package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readconcern"
	"go.mongodb.org/mongo-driver/v2/mongo/writeconcern"
)

type Categoria struct {
	ID          string `bson:"id" json:"id"`
	Nombre      string `bson:"nombre" json:"nombre"`
	Descripcion string `bson:"descripcion" json:"descripcion"`
	Clave       string `bson:"clave" json:"-"`
	Eliminada   bool   `bson:"eliminada,omitempty" json:"-"`
}

type catalogFailure struct {
	status        int
	code, message string
}

func (e *catalogFailure) Error() string           { return e.code }
func fail(status int, code, message string) error { return &catalogFailure{status, code, message} }
func catalogError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}
func databaseName(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// IDs are opaque strings in the existing contract, not ASCII slugs.
// Keep dots, accents and interior spaces accepted by the former API.
func idValido(s string) bool {
	if !utf8.ValidString(s) || strings.TrimSpace(s) != s || s == "" || s == "." || s == ".." || utf8.RuneCountInString(s) > 128 {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) || strings.ContainsRune("/\\?#", r) {
			return false
		}
	}
	return true
}
func normalizarLecturaTratamiento(t *Tratamiento) {
	if t.Imagenes == nil {
		t.Imagenes = []string{}
	}
	if t.Keywords == nil {
		t.Keywords = []string{}
	}
	if t.ProfesionalesIDs == nil {
		t.ProfesionalesIDs = []string{}
	}
}
func normalizarLecturaProfesional(p *Profesional) {
	if p.TratamientosIDs == nil {
		p.TratamientosIDs = []string{}
	}
}

func textoValido(s string, max int) bool {
	return strings.TrimSpace(s) != "" && utf8.ValidString(s) && utf8.RuneCountInString(s) <= max
}

// Preserve public field names and reject misspelled fields instead of silently discarding them.
func decodeCatalog(c *gin.Context, target any) bool {
	media, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || media != "application/json" {
		catalogError(c, 415, "CONTENT_TYPE_INVALIDO", "Usá Content-Type: application/json.")
		return false
	}
	raw, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 64*1024))
	if err != nil {
		catalogError(c, 413, "CUERPO_DEMASIADO_GRANDE", "El cuerpo no puede superar 64 KiB.")
		return false
	}
	if !utf8.Valid(raw) {
		catalogError(c, 400, "DATOS_INVALIDOS", "El JSON debe estar codificado en UTF-8 válido.")
		return false
	}
	// Decode the object token by token to detect duplicate keys and enforce exact case.
	fields := map[string]json.RawMessage{}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	token, err := dec.Token()
	if err != nil || token != json.Delim('{') {
		catalogError(c, 400, "DATOS_INVALIDOS", "Enviá un objeto JSON válido.")
		return false
	}
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			catalogError(c, 400, "DATOS_INVALIDOS", "JSON inválido.")
			return false
		}
		name, ok := key.(string)
		if !ok {
			catalogError(c, 400, "DATOS_INVALIDOS", "JSON inválido.")
			return false
		}
		if _, exists := fields[name]; exists {
			catalogError(c, 400, "DATOS_INVALIDOS", "No repitas propiedades.")
			return false
		}
		var value json.RawMessage
		if dec.Decode(&value) != nil || string(value) == "null" {
			catalogError(c, 400, "DATOS_INVALIDOS", "Las propiedades no admiten null.")
			return false
		}
		fields[name] = value
	}
	if _, err = dec.Token(); err != nil {
		catalogError(c, 400, "DATOS_INVALIDOS", "JSON inválido.")
		return false
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		catalogError(c, 400, "DATOS_INVALIDOS", "Enviá un único objeto JSON.")
		return false
	}
	allowed := map[string]bool{}
	required := []string{"nombre"}
	switch target.(type) {
	case *Tratamiento:
		for _, k := range []string{"id", "nombre", "descripcion", "categoria", "categoriaId", "precio", "moneda", "duracionMinutos", "imagenes", "keywords", "profesionalesIds"} {
			allowed[k] = true
		}
		required = append(required, "descripcion", "categoria", "precio", "moneda", "duracionMinutos")
	case *Profesional:
		for _, k := range []string{"id", "nombre", "descripcion", "tratamientosIds", "activo"} {
			allowed[k] = true
		}
	case *Categoria:
		for _, k := range []string{"id", "nombre", "descripcion"} {
			allowed[k] = true
		}
	}
	for k := range fields {
		if !allowed[k] {
			catalogError(c, 400, "DATOS_INVALIDOS", "Propiedad desconocida: "+k)
			return false
		}
	}
	for _, k := range required {
		if _, ok := fields[k]; !ok {
			catalogError(c, 400, "DATOS_INVALIDOS", "Falta la propiedad: "+k)
			return false
		}
	}
	if json.Unmarshal(raw, target) != nil {
		catalogError(c, 400, "DATOS_INVALIDOS", "Tipos de datos inválidos.")
		return false
	}
	return true
}
func uniqueIDs(ids []string) ([]string, bool) {
	result := []string{}
	seen := map[string]bool{}
	if len(ids) > 200 {
		return nil, false
	}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if !idValido(id) {
			return nil, false
		}
		if !seen[id] {
			result = append(result, id)
			seen[id] = true
		}
	}
	return result, true
}
func listasTratamientoValidas(t *Tratamiento) bool {
	var ok bool
	t.ProfesionalesIDs, ok = uniqueIDs(t.ProfesionalesIDs)
	if !ok || len(t.Keywords) > 100 || len(t.Imagenes) > 20 {
		return false
	}
	for _, s := range t.Keywords {
		if !textoValido(s, 100) {
			return false
		}
	}
	for _, s := range t.Imagenes {
		u, err := url.Parse(s)
		if err != nil || len(s) > 2048 || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return false
		}
	}
	return true
}
func pathID(c *gin.Context, id *string, create bool) bool {
	*id = strings.TrimSpace(*id)
	if !create {
		if *id == "" {
			*id = c.Param("id")
		}
		if *id != c.Param("id") {
			catalogError(c, 400, "ID_INCONSISTENTE", "El ID del cuerpo debe coincidir con la ruta.")
			return false
		}
	}
	if !idValido(*id) {
		catalogError(c, 400, "DATOS_INVALIDOS", "Usá un ID de 1 a 128 caracteres, sin controles, separadores de ruta ni espacios extremos.")
		return false
	}
	return true
}

func (s *Servidor) inicializarCatalogo(ctx context.Context) error {
	for _, col := range []*mongo.Collection{s.coleccion, s.profesionales, s.categorias} {
		if _, err := col.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "id", Value: 1}}, Options: options.Index().SetUnique(true).SetName("uq_" + col.Name() + "_id")}); err != nil {
			return err
		}
	}
	if _, err := s.profesionales.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "authUserId", Value: 1}}, Options: options.Index().SetUnique(true).SetSparse(true).SetName("uq_profesionales_auth_user")}); err != nil {
		return err
	}
	if _, err := s.categorias.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "clave", Value: 1}}, Options: options.Index().SetUnique(true).SetName("uq_categorias_clave")}); err != nil {
		return err
	}
	if _, err := s.control.UpdateOne(ctx, bson.M{"_id": "escrituras"}, bson.M{"$setOnInsert": bson.M{"version": 0}}, options.UpdateOne().SetUpsert(true)); err != nil && !mongo.IsDuplicateKeyError(err) {
		return err
	}
	// Insert only: restarts never restore deleted categories or overwrite admin edits.
	for id, name := range map[string]string{"facial": "Facial", "corporal": "Corporal", "depilacion": "Depilación", "manos-unas": "Manos y uñas"} {
		if _, err := s.categorias.UpdateOne(ctx, bson.M{"id": id}, bson.M{"$setOnInsert": Categoria{ID: id, Nombre: name, Clave: strings.ToLower(name)}}, options.UpdateOne().SetUpsert(true)); err != nil && !mongo.IsDuplicateKeyError(err) {
			return err
		}
	}
	return nil
}

// The shared write document serializes administrative mutations across replicas.
// This prevents category deletion / relationship updates from racing with inserts.
// No Auth/Reservas/Beneficios data is accessed by these transactions.
func (s *Servidor) mutate(c *gin.Context, change func(context.Context) error) bool {
	if s == nil || s.coleccion == nil || s.profesionales == nil || s.categorias == nil || s.control == nil {
		catalogError(c, 503, "CATALOGO_NO_DISPONIBLE", "El catálogo no está disponible temporalmente.")
		return false
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	session, err := s.coleccion.Database().Client().StartSession()
	if err == nil {
		defer session.EndSession(ctx)
		_, err = session.WithTransaction(ctx, func(tx context.Context) (any, error) {
			if _, e := s.control.UpdateOne(tx, bson.M{"_id": "escrituras"}, bson.M{"$inc": bson.M{"version": 1}}, options.UpdateOne().SetUpsert(true)); e != nil {
				return nil, e
			}
			return nil, change(tx)
		}, options.Transaction().SetReadConcern(readconcern.Snapshot()).SetWriteConcern(writeconcern.Majority()))
	}
	if err == nil {
		if s.cache != nil {
			if cacheErr := s.cache.bumpGeneration(); cacheErr != nil {
				slogWarn("catalog_cache_invalidation_failed", cacheErr)
			}
		}
		return true
	}
	var failure *catalogFailure
	if errors.As(err, &failure) {
		catalogError(c, failure.status, failure.code, failure.message)
	} else if mongo.IsDuplicateKeyError(err) {
		catalogError(c, 409, "RECURSO_EXISTENTE", "El identificador o nombre único ya está registrado, incluso si fue dado de baja.")
	} else {
		responderErrorDB(c, err)
	}
	return false
}
func existing(ctx context.Context, col *mongo.Collection, filter bson.M, target any, kind string) error {
	err := col.FindOne(ctx, filter).Decode(target)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return fail(404, kind+"_NOT_FOUND", "No existe el recurso activo solicitado.")
	}
	return err
}
func (s *Servidor) requireRefs(ctx context.Context, col *mongo.Collection, ids []string, active bson.M) error {
	if len(ids) == 0 {
		return nil
	}
	active["id"] = bson.M{"$in": ids}
	count, err := col.CountDocuments(ctx, active)
	if err != nil {
		return err
	}
	if count != int64(len(ids)) {
		return fail(400, "ASOCIACION_INVALIDA", "Todos los recursos asociados deben existir y estar activos.")
	}
	return nil
}
func (s *Servidor) guardarTratamiento(c *gin.Context, create bool) {
	var t Tratamiento
	if !decodeCatalog(c, &t) || !pathID(c, &t.ID, create) || !normalizarTratamiento(c, &t) {
		return
	}
	if !s.mutate(c, func(ctx context.Context) error {
		if !create {
			var old Tratamiento
			if e := existing(ctx, s.coleccion, bson.M{"id": t.ID, "eliminado": bson.M{"$ne": true}}, &old, "TRATAMIENTO"); e != nil {
				return e
			}
		}
		var category Categoria
		if e := existing(ctx, s.categorias, bson.M{"nombre": t.Categoria, "eliminada": bson.M{"$ne": true}}, &category, "CATEGORIA"); e != nil {
			return e
		}
		t.CategoriaID = category.ID
		if e := s.requireRefs(ctx, s.profesionales, t.ProfesionalesIDs, bson.M{"activo": true}); e != nil {
			return e
		}
		var err error
		if create {
			_, err = s.coleccion.InsertOne(ctx, t)
		} else {
			_, err = s.coleccion.ReplaceOne(ctx, bson.M{"id": t.ID}, t)
		}
		if err != nil {
			return err
		}
		if _, err = s.profesionales.UpdateMany(ctx, bson.M{"tratamientosIds": t.ID}, bson.M{"$pull": bson.M{"tratamientosIds": t.ID}}); err != nil {
			return err
		}
		if len(t.ProfesionalesIDs) > 0 {
			_, err = s.profesionales.UpdateMany(ctx, bson.M{"id": bson.M{"$in": t.ProfesionalesIDs}}, bson.M{"$addToSet": bson.M{"tratamientosIds": t.ID}})
			if err == nil {
				err = associationCapacity(ctx, s.profesionales, t.ProfesionalesIDs, "tratamientosIds")
			}
		}
		return err
	}) {
		return
	}
	status := 200
	if create {
		status = 201
	}
	c.JSON(status, t)
}
func (s *Servidor) actualizarProfesional(c *gin.Context) { s.guardarProfesional(c, false) }
func (s *Servidor) guardarProfesional(c *gin.Context, create bool) {
	p := Profesional{Activo: true}
	if !decodeCatalog(c, &p) || !pathID(c, &p.ID, create) {
		return
	}
	p.Nombre = strings.TrimSpace(p.Nombre)
	p.Descripcion = strings.TrimSpace(p.Descripcion)
	ids, ok := uniqueIDs(p.TratamientosIDs)
	p.TratamientosIDs = ids
	if !textoValido(p.Nombre, 200) || utf8.RuneCountInString(p.Descripcion) > 5000 || !ok || (!p.Activo && len(ids) > 0) {
		catalogError(c, 400, "DATOS_INVALIDOS", "Nombre, descripción o asociaciones inválidas; un profesional inactivo no puede tener aptitudes vigentes.")
		return
	}
	if !s.mutate(c, func(ctx context.Context) error {
		if !create {
			var old Profesional
			if e := existing(ctx, s.profesionales, bson.M{"id": p.ID}, &old, "PROFESIONAL"); e != nil {
				return e
			}
			p.AuthUserID = old.AuthUserID
		}
		if e := s.requireRefs(ctx, s.coleccion, p.TratamientosIDs, bson.M{"eliminado": bson.M{"$ne": true}}); e != nil {
			return e
		}
		var err error
		if create {
			_, err = s.profesionales.InsertOne(ctx, p)
		} else {
			_, err = s.profesionales.ReplaceOne(ctx, bson.M{"id": p.ID}, p)
		}
		if err != nil {
			return err
		}
		if _, err = s.coleccion.UpdateMany(ctx, bson.M{"eliminado": bson.M{"$ne": true}, "profesionalesIds": p.ID}, bson.M{"$pull": bson.M{"profesionalesIds": p.ID}}); err != nil {
			return err
		}
		if len(ids) > 0 {
			_, err = s.coleccion.UpdateMany(ctx, bson.M{"id": bson.M{"$in": ids}}, bson.M{"$addToSet": bson.M{"profesionalesIds": p.ID}})
			if err == nil {
				err = associationCapacity(ctx, s.coleccion, ids, "profesionalesIds")
			}
		}
		return err
	}) {
		return
	}
	status := 200
	if create {
		status = 201
	}
	c.JSON(status, p)
}

type profesionalAdministrativo struct {
	ID              string   `json:"id"`
	Nombre          string   `json:"nombre"`
	Descripcion     string   `json:"descripcion,omitempty"`
	TratamientosIDs []string `json:"tratamientosIds"`
	Activo          bool     `json:"activo"`
	AuthUserID      string   `json:"authUserId,omitempty"`
}

func (s *Servidor) listarProfesionalesAdmin(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	cur, err := s.profesionales.Find(ctx, bson.M{"activo": true}, options.Find().SetSort(bson.D{{Key: "nombre", Value: 1}, {Key: "id", Value: 1}}))
	if err != nil {
		responderErrorDB(c, err)
		return
	}
	defer cur.Close(ctx)
	items := make([]profesionalAdministrativo, 0)
	for cur.Next(ctx) {
		var p Profesional
		if err := cur.Decode(&p); err != nil {
			responderErrorDB(c, err)
			return
		}
		if p.TratamientosIDs == nil {
			p.TratamientosIDs = []string{}
		}
		items = append(items, profesionalAdministrativo{ID: p.ID, Nombre: p.Nombre, Descripcion: p.Descripcion, TratamientosIDs: p.TratamientosIDs, Activo: p.Activo, AuthUserID: p.AuthUserID})
	}
	if err := cur.Err(); err != nil {
		responderErrorDB(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"profesionales": items})
}

func (s *Servidor) verificarCuentaProfesional(ctx context.Context, authUserID string) error {
	if s == nil || s.authInternalURL == "" || s.internalKey == "" {
		return fail(http.StatusServiceUnavailable, "AUTH_NO_DISPONIBLE", "No se pudo validar la cuenta profesional.")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.authInternalURL+url.PathEscape(authUserID), nil)
	if err != nil {
		return fail(http.StatusServiceUnavailable, "AUTH_NO_DISPONIBLE", "No se pudo validar la cuenta profesional.")
	}
	req.Header.Set("X-Melt-Internal-Key", s.internalKey)
	resp, err := (&http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
	if err != nil {
		return fail(http.StatusServiceUnavailable, "AUTH_NO_DISPONIBLE", "No se pudo validar la cuenta profesional.")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return fail(http.StatusUnprocessableEntity, "CUENTA_NO_PROFESIONAL", "La cuenta no existe o no está activa.")
	}
	if resp.StatusCode != http.StatusNoContent {
		return fail(http.StatusServiceUnavailable, "AUTH_NO_DISPONIBLE", "No se pudo validar la cuenta profesional.")
	}
	for _, role := range strings.Split(resp.Header.Get("X-Auth-User-Roles"), ",") {
		if strings.TrimSpace(role) == "profesional" {
			return nil
		}
	}
	return fail(http.StatusUnprocessableEntity, "CUENTA_NO_PROFESIONAL", "La cuenta debe tener el rol profesional.")
}

func (s *Servidor) vincularCuentaProfesional(c *gin.Context) {
	id := c.Param("id")
	if !pathID(c, &id, false) {
		return
	}
	var req struct {
		AuthUserID *string `json:"authUserId"`
	}
	media, _, mediaErr := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if mediaErr != nil || media != "application/json" {
		catalogError(c, http.StatusUnsupportedMediaType, "CONTENT_TYPE_INVALIDO", "Usá Content-Type: application/json.")
		return
	}
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&req) != nil || req.AuthUserID == nil {
		catalogError(c, http.StatusBadRequest, "DATOS_INVALIDOS", "Indicá authUserId con el ID de cuenta profesional o una cadena vacía para desvincular.")
		return
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		catalogError(c, http.StatusBadRequest, "DATOS_INVALIDOS", "Enviá un único objeto JSON.")
		return
	}
	authUserID := strings.TrimSpace(*req.AuthUserID)
	if authUserID != "" {
		if !idValido(authUserID) {
			catalogError(c, http.StatusBadRequest, "DATOS_INVALIDOS", "El ID de cuenta no es válido.")
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 4*time.Second)
		err := s.verificarCuentaProfesional(ctx, authUserID)
		cancel()
		if err != nil {
			readFailure(c, err)
			return
		}
	}
	if !s.mutate(c, func(ctx context.Context) error {
		var profile Profesional
		if err := existing(ctx, s.profesionales, bson.M{"id": id, "activo": true}, &profile, "PROFESIONAL"); err != nil {
			return err
		}
		if authUserID != "" {
			count, err := s.profesionales.CountDocuments(ctx, bson.M{"authUserId": authUserID, "id": bson.M{"$ne": id}})
			if err != nil {
				return err
			}
			if count != 0 {
				return fail(http.StatusConflict, "CUENTA_YA_VINCULADA", "La cuenta ya está vinculada a otro perfil profesional.")
			}
			_, err = s.profesionales.UpdateOne(ctx, bson.M{"id": id, "activo": true}, bson.M{"$set": bson.M{"authUserId": authUserID}})
			return err
		}
		_, err := s.profesionales.UpdateOne(ctx, bson.M{"id": id, "activo": true}, bson.M{"$unset": bson.M{"authUserId": ""}})
		return err
	}) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"profesionalId": id, "authUserId": authUserID, "vinculado": authUserID != ""})
}

func (s *Servidor) resolverPerfilProfesional(c *gin.Context) {
	if s == nil || s.internalKey == "" || subtle.ConstantTimeCompare([]byte(c.GetHeader("X-Melt-Internal-Key")), []byte(s.internalKey)) != 1 {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "IDENTIDAD_INTERNA_INVALIDA", "message": "Se requiere una credencial de servicio válida."}})
		return
	}
	authUserID := strings.TrimSpace(c.Param("authUserId"))
	if !idValido(authUserID) {
		catalogError(c, http.StatusBadRequest, "DATOS_INVALIDOS", "El ID de cuenta no es válido.")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 4*time.Second)
	defer cancel()
	var p Profesional
	err := s.profesionales.FindOne(ctx, bson.M{"authUserId": authUserID, "activo": true}).Decode(&p)
	if errors.Is(err, mongo.ErrNoDocuments) {
		catalogError(c, http.StatusNotFound, "PERFIL_NO_VINCULADO", "La cuenta no tiene un perfil profesional activo vinculado.")
		return
	}
	if err != nil {
		responderErrorDB(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"profesionalId": p.ID})
}

func (s *Servidor) eliminarTratamiento(c *gin.Context) {
	id := c.Param("id")
	if !pathID(c, &id, false) {
		return
	}
	if s.mutate(c, func(ctx context.Context) error {
		var old Tratamiento
		if e := existing(ctx, s.coleccion, bson.M{"id": id, "eliminado": bson.M{"$ne": true}}, &old, "TRATAMIENTO"); e != nil {
			return e
		}
		if _, err := s.coleccion.UpdateOne(ctx, bson.M{"id": id}, bson.M{"$set": bson.M{"eliminado": true}}); err != nil {
			return err
		}
		_, err := s.profesionales.UpdateMany(ctx, bson.M{"tratamientosIds": id}, bson.M{"$pull": bson.M{"tratamientosIds": id}})
		return err
	}) {
		c.Status(204)
	}
}
func (s *Servidor) eliminarProfesional(c *gin.Context) {
	id := c.Param("id")
	if !pathID(c, &id, false) {
		return
	}
	if s.mutate(c, func(ctx context.Context) error {
		var old Profesional
		if e := existing(ctx, s.profesionales, bson.M{"id": id, "activo": true}, &old, "PROFESIONAL"); e != nil {
			return e
		}
		if _, err := s.profesionales.UpdateOne(ctx, bson.M{"id": id}, bson.M{"$set": bson.M{"activo": false, "tratamientosIds": []string{}}, "$unset": bson.M{"authUserId": ""}}); err != nil {
			return err
		}
		_, err := s.coleccion.UpdateMany(ctx, bson.M{"eliminado": bson.M{"$ne": true}, "profesionalesIds": id}, bson.M{"$pull": bson.M{"profesionalesIds": id}})
		return err
	}) {
		c.Status(204)
	}
}
func (s *Servidor) obtenerProfesional(c *gin.Context) {
	id := c.Param("id")
	if !pathID(c, &id, false) {
		return
	}
	if s.profesionales == nil {
		catalogError(c, 503, "CATALOGO_NO_DISPONIBLE", "Profesionales no disponible.")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	var item Profesional
	if err := existing(ctx, s.profesionales, bson.M{"id": id, "activo": true}, &item, "PROFESIONAL"); err != nil {
		readFailure(c, err)
		return
	}
	normalizarLecturaProfesional(&item)
	c.JSON(200, item)
}
func readFailure(c *gin.Context, err error) {
	var f *catalogFailure
	if errors.As(err, &f) {
		catalogError(c, f.status, f.code, f.message)
	} else {
		responderErrorDB(c, err)
	}
}
func (s *Servidor) crearCategoria(c *gin.Context)      { s.guardarCategoria(c, true) }
func (s *Servidor) actualizarCategoria(c *gin.Context) { s.guardarCategoria(c, false) }
func (s *Servidor) guardarCategoria(c *gin.Context, create bool) {
	var item Categoria
	if !decodeCatalog(c, &item) || !pathID(c, &item.ID, create) {
		return
	}
	item.Nombre = strings.TrimSpace(item.Nombre)
	item.Descripcion = strings.TrimSpace(item.Descripcion)
	item.Clave = strings.ToLower(item.Nombre)
	if !textoValido(item.Nombre, 100) || utf8.RuneCountInString(item.Descripcion) > 1000 {
		catalogError(c, 400, "DATOS_INVALIDOS", "Nombre o descripción de categoría inválidos.")
		return
	}
	if !s.mutate(c, func(ctx context.Context) error {
		if create {
			_, err := s.categorias.InsertOne(ctx, item)
			return err
		}
		var old Categoria
		if err := existing(ctx, s.categorias, bson.M{"id": item.ID, "eliminada": bson.M{"$ne": true}}, &old, "CATEGORIA"); err != nil {
			return err
		}
		if _, err := s.categorias.ReplaceOne(ctx, bson.M{"id": item.ID}, item); err != nil {
			return err
		}
		// Keep the legacy category-name field used by current clients in sync.
		_, err := s.coleccion.UpdateMany(ctx, bson.M{"categoria": old.Nombre, "eliminado": bson.M{"$ne": true}}, bson.M{"$set": bson.M{"categoria": item.Nombre}})
		return err
	}) {
		return
	}
	status := 200
	if create {
		status = 201
	}
	c.JSON(status, item)
}
func (s *Servidor) eliminarCategoria(c *gin.Context) {
	id := c.Param("id")
	if !pathID(c, &id, false) {
		return
	}
	if s.mutate(c, func(ctx context.Context) error {
		var old Categoria
		if err := existing(ctx, s.categorias, bson.M{"id": id, "eliminada": bson.M{"$ne": true}}, &old, "CATEGORIA"); err != nil {
			return err
		}
		n, err := s.coleccion.CountDocuments(ctx, bson.M{"categoria": old.Nombre, "eliminado": bson.M{"$ne": true}})
		if err != nil {
			return err
		}
		if n > 0 {
			return fail(409, "CATEGORIA_EN_USO", "Reasigná o da de baja los tratamientos de esta categoría primero.")
		}
		_, err = s.categorias.UpdateOne(ctx, bson.M{"id": id}, bson.M{"$set": bson.M{"eliminada": true}})
		return err
	}) {
		c.Status(204)
	}
}
func (s *Servidor) obtenerCategoria(c *gin.Context) {
	id := c.Param("id")
	if !pathID(c, &id, false) {
		return
	}
	if s.categorias == nil {
		catalogError(c, 503, "CATALOGO_NO_DISPONIBLE", "Categorías no disponible.")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	var item Categoria
	if err := existing(ctx, s.categorias, bson.M{"id": id, "eliminada": bson.M{"$ne": true}}, &item, "CATEGORIA"); err != nil {
		readFailure(c, err)
		return
	}
	c.JSON(200, item)
}
func (s *Servidor) listarCategorias(c *gin.Context) {
	if s.categorias == nil {
		catalogError(c, 503, "CATALOGO_NO_DISPONIBLE", "Categorías no disponible.")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	cur, err := s.categorias.Find(ctx, bson.M{"eliminada": bson.M{"$ne": true}}, options.Find().SetSort(bson.D{{Key: "nombre", Value: 1}, {Key: "id", Value: 1}}))
	if err != nil {
		responderErrorDB(c, err)
		return
	}
	defer cur.Close(ctx)
	items := []Categoria{}
	if err = cur.All(ctx, &items); err != nil {
		responderErrorDB(c, err)
		return
	}
	c.JSON(200, gin.H{"categorias": items})
}

func associationCapacity(ctx context.Context, col *mongo.Collection, ids []string, field string) error {
	n, err := col.CountDocuments(ctx, bson.M{"id": bson.M{"$in": ids}, field + ".200": bson.M{"$exists": true}})
	if err != nil {
		return err
	}
	if n > 0 {
		return fail(409, "LIMITE_ASOCIACIONES", "La operación superaría las 200 asociaciones de un recurso relacionado.")
	}
	return nil
}
