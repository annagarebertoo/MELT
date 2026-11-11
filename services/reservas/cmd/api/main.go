package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type identidad struct {
	ID    string   `json:"id"`
	Email string   `json:"email"`
	Roles []string `json:"roles"`
}

type agendaSemanal struct {
	ID            string         `bson:"_id" json:"id"`
	ProfesionalID string         `bson:"profesionalId" json:"profesionalId"`
	DiaSemana     int            `bson:"diaSemana" json:"diaSemana"`
	HoraInicio    string         `bson:"horaInicio" json:"horaInicio"`
	HoraFin       string         `bson:"horaFin" json:"horaFin"`
	Bloques       []bloqueAgenda `bson:"bloques,omitempty" json:"bloques"`
	Activo        bool           `bson:"activo" json:"activo"`
}

type bloqueAgenda struct {
	Inicio string `bson:"inicio" json:"inicio"`
	Fin    string `bson:"fin" json:"fin"`
}

type turno struct {
	ID                      string     `bson:"_id" json:"id"`
	ClienteID               string     `bson:"clienteId" json:"clienteId"`
	ProfesionalID           string     `bson:"profesionalId" json:"profesionalId"`
	TratamientoID           string     `bson:"tratamientoId" json:"tratamientoId"`
	CategoriaID             string     `bson:"categoriaId,omitempty" json:"categoriaId,omitempty"`
	ClienteNombre           string     `bson:"-" json:"clienteNombre,omitempty"`
	Inicio                  time.Time  `bson:"inicio" json:"inicio"`
	Fin                     time.Time  `bson:"fin" json:"fin"`
	Estado                  string     `bson:"estado" json:"estado"`
	CreadoEn                time.Time  `bson:"creadoEn" json:"creadoEn"`
	PrecioTratamiento       float64    `bson:"precioTratamiento" json:"precioTratamiento"`
	Sena                    float64    `bson:"sena" json:"sena"`
	PagoID                  string     `bson:"pagoId,omitempty" json:"pagoId,omitempty"`
	EstadoPago              string     `bson:"estadoPago,omitempty" json:"estadoPago,omitempty"`
	ImporteDevolucion       float64    `bson:"importeDevolucion,omitempty" json:"importeDevolucion,omitempty"`
	EstadoDevolucion        string     `bson:"estadoDevolucion,omitempty" json:"estadoDevolucion,omitempty"`
	TratamientoRealizadoEn  *time.Time `bson:"tratamientoRealizadoEn,omitempty" json:"tratamientoRealizadoEn,omitempty"`
	TratamientoRealizadoPor string     `bson:"tratamientoRealizadoPor,omitempty" json:"tratamientoRealizadoPor,omitempty"`
	NoAsistioEn             *time.Time `bson:"noAsistioEn,omitempty" json:"noAsistioEn,omitempty"`
	NoAsistioPor            string     `bson:"noAsistioPor,omitempty" json:"noAsistioPor,omitempty"`
	BeneficioCanjeID        string     `bson:"beneficioCanjeId,omitempty" json:"beneficioCanjeId,omitempty"`
	BeneficioReservaID      string     `bson:"beneficioReservaId,omitempty" json:"beneficioReservaId,omitempty"`
	DescuentoBeneficio      float64    `bson:"descuentoBeneficio,omitempty" json:"descuentoBeneficio,omitempty"`
	PrecioFinal             float64    `bson:"precioFinal,omitempty" json:"precioFinal,omitempty"`
}

type servidorReservas struct {
	mongo         *mongo.Client
	agendas       *mongo.Collection
	turnos        *mongo.Collection
	bloqueos      *mongo.Collection
	carritos      *mongo.Collection
	checkouts     *mongo.Collection
	outbox        *mongo.Collection
	catalogoURL   string
	pagosURL      string
	beneficiosURL string
	internalKey   string
	authURL       string
	controlAgenda *mongo.Collection
	rabbitURL     string
	httpClient    *http.Client
}

type tratamientoCatalogo struct {
	ID               string   `json:"id"`
	Nombre           string   `json:"nombre"`
	CategoriaID      string   `json:"categoriaId"`
	DuracionMinutos  int      `json:"duracionMinutos"`
	ProfesionalesIDs []string `json:"profesionalesIds"`
	Precio           float64  `json:"precio"`
	Moneda           string   `json:"moneda"`
}

func tratamientoCompatible(t tratamientoCatalogo, profesionalID string) bool {
	profesionalID = strings.TrimSpace(profesionalID)
	if profesionalID == "" {
		return false
	}
	for _, id := range t.ProfesionalesIDs {
		if strings.TrimSpace(id) == profesionalID {
			return true
		}
	}
	return false
}

type bloqueoTurno struct {
	ID            string     `bson:"_id,omitempty"`
	ProfesionalID string     `bson:"profesionalId"`
	ClienteID     string     `bson:"clienteId,omitempty"`
	CategoriaID   string     `bson:"categoriaId,omitempty"`
	Minuto        int64      `bson:"minuto"`
	TurnoID       string     `bson:"turnoId"`
	ExpiraEn      *time.Time `bson:"expiraEn,omitempty"`
}

func (s *servidorReservas) obtenerTratamiento(ctx context.Context, id string) (tratamientoCatalogo, int, error) {
	var t tratamientoCatalogo
	if strings.TrimSpace(id) == "" {
		return t, http.StatusBadRequest, errors.New("tratamientoId es obligatorio")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(s.catalogoURL, "/")+"/tratamientos/"+url.PathEscape(id), nil)
	if err != nil {
		return t, http.StatusServiceUnavailable, err
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return t, http.StatusServiceUnavailable, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return t, http.StatusUnprocessableEntity, errors.New("el tratamiento no existe en Catálogo")
	}
	if resp.StatusCode != http.StatusOK {
		return t, http.StatusServiceUnavailable, errors.New("Catálogo no está disponible")
	}
	if err := json.NewDecoder(resp.Body).Decode(&t); err != nil || t.ID == "" || t.DuracionMinutos < 5 || t.DuracionMinutos > 480 || strings.TrimSpace(t.CategoriaID) == "" {
		return t, http.StatusServiceUnavailable, errors.New("Catálogo devolvió un tratamiento inválido")
	}
	return t, http.StatusOK, nil
}

var permisosPorOperacion = map[string]map[string]bool{
	"reservas.consultar-disponibilidad":  {"cliente": true, "profesional": true, "recepcion": true, "administrador": true, "superadministrador": true},
	"reservas.consultar-turnos":          {"cliente": true, "profesional": true, "recepcion": true, "administrador": true, "superadministrador": true},
	"reservas.cancelar-turno":            {"cliente": true, "profesional": true, "recepcion": true, "administrador": true, "superadministrador": true},
	"reservas.crear":                     {"cliente": true},
	"reservas.gestionar-agenda":          {"profesional": true, "recepcion": true, "administrador": true, "superadministrador": true},
	"reservas.crear-bloqueo-carrito":     {"cliente": true},
	"reservas.confirmar-bloqueo-carrito": {"cliente": true},
	"reservas.liberar-bloqueo-carrito":   {"cliente": true},
	"reservas.reintentar-devolucion":     {"cliente": true, "recepcion": true, "administrador": true, "superadministrador": true},
	"reservas.completar-tratamiento":     {"profesional": true},
	"reservas.registrar-ausencia":        {"profesional": true},
}

func main() {
	uri := strings.TrimSpace(os.Getenv("MONGODB_URI"))
	if uri == "" {
		uri = "mongodb://localhost:27017"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		log.Fatal("no se pudo configurar MongoDB")
	}
	defer client.Disconnect(context.Background())
	if err := client.Ping(ctx, nil); err != nil {
		log.Fatal("no se pudo conectar con MongoDB")
	}
	db := client.Database("reservas_db")
	catalogoURL := strings.TrimSpace(os.Getenv("CATALOGO_URL"))
	if catalogoURL == "" {
		catalogoURL = "http://catalogo-profesionales:8081"
	}
	beneficiosURL := strings.TrimRight(strings.TrimSpace(os.Getenv("BENEFICIOS_URL")), "/")
	if beneficiosURL == "" {
		beneficiosURL = "http://beneficios:8083"
	}
	authURL := strings.TrimRight(strings.TrimSpace(os.Getenv("AUTH_URL")), "/")
	if authURL == "" {
		authURL = "http://auth:8084"
	}
	rabbitURL := strings.TrimSpace(os.Getenv("RABBITMQ_URL"))
	s := &servidorReservas{mongo: client, agendas: db.Collection("agendas_semanales"), turnos: db.Collection("turnos"), bloqueos: db.Collection("bloqueos_turnos"), carritos: db.Collection("carritos_bloqueos"), checkouts: db.Collection("checkouts"), outbox: db.Collection("outbox_eventos"), controlAgenda: db.Collection("control_agendas"), catalogoURL: catalogoURL, authURL: authURL, pagosURL: strings.TrimRight(strings.TrimSpace(os.Getenv("PAGOS_URL")), "/"), beneficiosURL: beneficiosURL, internalKey: strings.TrimSpace(os.Getenv("MELT_INTERNAL_KEY")), rabbitURL: rabbitURL, httpClient: &http.Client{Timeout: 7 * time.Second}}
	_, err = s.agendas.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "profesionalId", Value: 1}, {Key: "diaSemana", Value: 1}}, Options: options.Index().SetUnique(true).SetName("uq_agenda_profesional_dia")})
	if err != nil {
		log.Fatal("no se pudo crear índice de agenda")
	}
	_, err = s.turnos.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "profesionalId", Value: 1}, {Key: "inicio", Value: 1}}, Options: options.Index().SetName("ix_turnos_profesional_inicio")})
	if err != nil {
		log.Fatal("no se pudo crear índice de turnos")
	}
	_, err = s.bloqueos.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "profesionalId", Value: 1}, {Key: "minuto", Value: 1}}, Options: options.Index().SetUnique(true).SetName("uq_bloqueo_profesional_minuto")})
	if err != nil {
		log.Fatal("no se pudo crear índice de bloqueos de turnos")
	}
	_, err = s.bloqueos.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "clienteId", Value: 1}, {Key: "categoriaId", Value: 1}, {Key: "minuto", Value: 1}}, Options: options.Index().SetUnique(true).SetName("uq_bloqueo_cliente_categoria_minuto").SetPartialFilterExpression(bson.M{"clienteId": bson.M{"$exists": true}, "categoriaId": bson.M{"$exists": true}})})
	if err != nil {
		log.Fatal("no se pudo crear índice de categoría por cliente")
	}
	_, err = s.bloqueos.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "expiraEn", Value: 1}}, Options: options.Index().SetExpireAfterSeconds(0).SetName("ttl_bloqueos_carrito")})
	if err != nil {
		log.Fatal("no se pudo crear índice TTL de bloqueos")
	}
	_, err = s.carritos.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "expiraEn", Value: 1}}, Options: options.Index().SetExpireAfterSeconds(0).SetName("ttl_carritos")})
	if err != nil {
		log.Fatal("no se pudo crear índice TTL de carritos")
	}
	_, err = s.checkouts.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "clienteId", Value: 1}, {Key: "idempotencyKey", Value: 1}}, Options: options.Index().SetUnique(true).SetName("uq_checkout_cliente_idempotencia")})
	if err != nil {
		log.Fatal("no se pudo crear índice de idempotencia de checkout")
	}
	_, err = s.outbox.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "publicadoEn", Value: 1}, {Key: "creadoEn", Value: 1}}})
	if err != nil {
		log.Fatal("no se pudo crear índice de eventos pendientes")
	}
	publisherCtx, cancelPublisher := context.WithCancel(context.Background())
	defer cancelPublisher()
	go s.publicarEventos(publisherCtx)
	if err := s.router().Run(":8082"); err != nil {
		log.Fatal("no se pudo iniciar Reservas")
	}
}

func (s *servidorReservas) router() *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())
	r.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"servicio": "reservas", "estado": "ok"}) })
	r.GET("/me", requiereIdentidad(), func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"usuario": identidadDesdeHeaders(c)}) })
	r.PUT("/agenda", requiereIdentidad(), requierePermiso("reservas.gestionar-agenda"), s.guardarAgenda)
	r.GET("/agenda", requiereIdentidad(), requierePermiso("reservas.gestionar-agenda"), s.consultarAgenda)
	r.GET("/disponibilidad", requiereIdentidad(), requierePermiso("reservas.consultar-disponibilidad"), s.disponibilidad)
	r.GET("/turnos", requiereIdentidad(), requierePermiso("reservas.consultar-turnos"), s.listarTurnos)
	r.GET("/carrito", requiereIdentidad(), requierePermiso("reservas.crear-bloqueo-carrito"), s.listarCarrito)
	r.POST("/carrito/checkout", requiereIdentidad(), requierePermiso("reservas.confirmar-bloqueo-carrito"), s.checkoutCarrito)
	r.POST("/carrito/bloqueos", requiereIdentidad(), requierePermiso("reservas.crear-bloqueo-carrito"), s.crearBloqueoCarrito)
	r.POST("/carrito/bloqueos/:id/pago", requiereIdentidad(), requierePermiso("reservas.crear-bloqueo-carrito"), s.iniciarPagoBloqueo)
	r.POST("/carrito/bloqueos/:id/confirmar", requiereIdentidad(), requierePermiso("reservas.confirmar-bloqueo-carrito"), s.confirmarBloqueoCarrito)
	r.DELETE("/carrito/bloqueos/:id", requiereIdentidad(), requierePermiso("reservas.liberar-bloqueo-carrito"), s.liberarBloqueoCarrito)
	r.POST("/turnos", requiereIdentidad(), requierePermiso("reservas.crear"), s.confirmarTurnoDesdeCarrito)
	r.DELETE("/turnos/:id", requiereIdentidad(), requierePermiso("reservas.cancelar-turno"), s.cancelarTurno)
	r.POST("/turnos/:id/devolucion/reintentar", requiereIdentidad(), requierePermiso("reservas.reintentar-devolucion"), s.reintentarDevolucion)
	r.POST("/turnos/:id/completar", requiereIdentidad(), requierePermiso("reservas.completar-tratamiento"), s.completarTratamiento)
	r.POST("/turnos/:id/no-asistio", requiereIdentidad(), requierePermiso("reservas.registrar-ausencia"), s.registrarNoAsistencia)
	return r
}

func identidadDesdeHeaders(c *gin.Context) identidad {
	roles := []string{}
	for _, rol := range strings.Split(c.GetHeader("X-Auth-User-Roles"), ",") {
		if rol = strings.TrimSpace(rol); rol != "" {
			roles = append(roles, rol)
		}
	}
	return identidad{ID: strings.TrimSpace(c.GetHeader("X-Auth-User-ID")), Email: strings.ToLower(strings.TrimSpace(c.GetHeader("X-Auth-User-Email"))), Roles: roles}
}

func tieneRol(u identidad, roles ...string) bool {
	for _, actual := range u.Roles {
		for _, esperado := range roles {
			if actual == esperado {
				return true
			}
		}
	}
	return false
}

func requiereIdentidad() gin.HandlerFunc {
	return func(c *gin.Context) {
		u := identidadDesdeHeaders(c)
		if u.ID == "" || u.Email == "" || len(u.Roles) == 0 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "IDENTIDAD_REQUERIDA", "message": "Iniciá sesión para continuar."}})
			return
		}
		c.Set("identidad", u)
		c.Next()
	}
}

func requierePermiso(operacion string) gin.HandlerFunc {
	return func(c *gin.Context) {
		v, ok := c.Get("identidad")
		u, valid := v.(identidad)
		if !ok || !valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "IDENTIDAD_REQUERIDA", "message": "Iniciá sesión para continuar."}})
			return
		}
		permitidos, exists := permisosPorOperacion[operacion]
		if !exists || !tieneRol(u, mapKeys(permitidos)...) {
			code := "PERMISOS_INSUFICIENTES"
			if !exists {
				code = "OPERACION_SIN_PERMISO"
			}
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": gin.H{"code": code, "message": "No tenés permisos para realizar esta acción."}})
			return
		}
		c.Next()
	}
}

func mapKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		if m[k] {
			out = append(out, k)
		}
	}
	return out
}

type agendaRequest struct {
	ProfesionalID string         `json:"profesionalId"`
	DiaSemana     int            `json:"diaSemana"`
	HoraInicio    string         `json:"horaInicio"`
	HoraFin       string         `json:"horaFin"`
	Bloques       []bloqueAgenda `json:"bloques"`
	Activo        *bool          `json:"activo"`
}

func parseHora(s string) (int, error) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, err
	}
	return t.Hour()*60 + t.Minute(), nil
}
func agendaValida(dia int, inicio, fin string) bool {
	if dia < 0 || dia > 6 {
		return false
	}
	a, errA := parseHora(inicio)
	b, errB := parseHora(fin)
	return errA == nil && errB == nil && a >= 8*60 && b <= 20*60 && a < b
}

func validarBloquesAgenda(bloques []bloqueAgenda) ([]bloqueAgenda, bool) {
	if len(bloques) > 8 {
		return nil, false
	}
	ordenados := append([]bloqueAgenda(nil), bloques...)
	sort.Slice(ordenados, func(i, j int) bool { return ordenados[i].Inicio < ordenados[j].Inicio })
	ultimoFin := -1
	for _, bloque := range ordenados {
		inicio, errInicio := parseHora(bloque.Inicio)
		fin, errFin := parseHora(bloque.Fin)
		if errInicio != nil || errFin != nil || inicio < 8*60 || fin > 20*60 || inicio >= fin || inicio < ultimoFin {
			return nil, false
		}
		ultimoFin = fin
	}
	return ordenados, true
}

func bloquesAgendaDe(agenda agendaSemanal) []bloqueAgenda {
	if agenda.Bloques != nil {
		return agenda.Bloques
	}
	if agenda.Activo && agendaValida(agenda.DiaSemana, agenda.HoraInicio, agenda.HoraFin) {
		return []bloqueAgenda{{Inicio: agenda.HoraInicio, Fin: agenda.HoraFin}}
	}
	return []bloqueAgenda{}
}

func intervaloEnBloques(inicio, fin time.Time, bloques []bloqueAgenda, loc *time.Location) bool {
	localInicio, localFin := inicio.In(loc), fin.In(loc)
	if localInicio.Year() != localFin.Year() || localInicio.YearDay() != localFin.YearDay() {
		return false
	}
	startMinute, endMinute := localInicio.Hour()*60+localInicio.Minute(), localFin.Hour()*60+localFin.Minute()
	for _, bloque := range bloques {
		from, errFrom := parseHora(bloque.Inicio)
		to, errTo := parseHora(bloque.Fin)
		if errFrom == nil && errTo == nil && startMinute >= from && endMinute <= to {
			return true
		}
	}
	return false
}

func (s *servidorReservas) resolverPerfilProfesional(ctx context.Context, authUserID string) (string, int, error) {
	if s == nil || strings.TrimSpace(s.catalogoURL) == "" || s.internalKey == "" {
		return "", http.StatusServiceUnavailable, errors.New("Catálogo interno no configurado")
	}
	endpoint := strings.TrimRight(s.catalogoURL, "/") + "/internal/profesionales/por-cuenta/" + url.PathEscape(authUserID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", http.StatusServiceUnavailable, err
	}
	req.Header.Set("X-Melt-Internal-Key", s.internalKey)
	client := s.httpClient
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", http.StatusServiceUnavailable, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", http.StatusForbidden, errors.New("La cuenta profesional no tiene un perfil vinculado")
	}
	if resp.StatusCode != http.StatusOK {
		return "", http.StatusServiceUnavailable, errors.New("No se pudo resolver el perfil profesional")
	}
	var result struct {
		ProfesionalID string `json:"profesionalId"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&result); err != nil || strings.TrimSpace(result.ProfesionalID) == "" {
		return "", http.StatusServiceUnavailable, errors.New("Catálogo devolvió un perfil profesional inválido")
	}
	return result.ProfesionalID, http.StatusOK, nil
}

func (s *servidorReservas) nombreClienteInterno(ctx context.Context, userID string) string {
	if s.authURL == "" || s.internalKey == "" {
		return ""
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(s.authURL, "/")+"/internal/usuarios/"+url.PathEscape(userID), nil)
	if err != nil {
		return ""
	}
	req.Header.Set("X-Melt-Internal-Key", s.internalKey)
	client := s.httpClient
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return ""
	}
	return strings.TrimSpace(resp.Header.Get("X-Auth-User-Name"))
}

var errAgendaIncompatible = errors.New("AGENDA_INCOMPATIBLE_CON_RESERVAS")

func (s *servidorReservas) agendaControl() *mongo.Collection {
	if s.controlAgenda != nil {
		return s.controlAgenda
	}
	return s.agendas.Database().Collection("control_agendas")
}

func agendaControlID(profesionalID string, dia int) string {
	return profesionalID + "-" + strconv.Itoa(dia)
}

func (s *servidorReservas) asegurarAgendaControl(ctx context.Context, profesionalID string, dia int) error {
	_, err := s.agendaControl().UpdateOne(ctx, bson.M{"_id": agendaControlID(profesionalID, dia)}, bson.M{"$setOnInsert": bson.M{"version": 0}}, options.UpdateOne().SetUpsert(true))
	if mongo.IsDuplicateKeyError(err) {
		return nil
	}
	return err
}

func (s *servidorReservas) agendaRespetaReservas(ctx context.Context, profesionalID string, dia int, bloques []bloqueAgenda) (bool, error) {
	now := time.Now().UTC()
	loc := zonaHorariaReservas()
	turns, err := s.turnos.Find(ctx, bson.M{"profesionalId": profesionalID, "estado": "confirmado", "inicio": bson.M{"$gte": now}})
	if err != nil {
		return false, err
	}
	defer turns.Close(ctx)
	var turnos []turno
	if err := turns.All(ctx, &turnos); err != nil {
		return false, err
	}
	for _, item := range turnos {
		if int(item.Inicio.In(loc).Weekday()) == dia && !intervaloEnBloques(item.Inicio, item.Fin, bloques, loc) {
			return false, nil
		}
	}
	carts, err := s.carritos.Find(ctx, bson.M{"profesionalId": profesionalID, "estado": "pendiente_pago", "expiraEn": bson.M{"$gt": now}, "inicio": bson.M{"$gte": now}})
	if err != nil {
		return false, err
	}
	defer carts.Close(ctx)
	var pendientes []bloqueoCarrito
	if err := carts.All(ctx, &pendientes); err != nil {
		return false, err
	}
	for _, item := range pendientes {
		if int(item.Inicio.In(loc).Weekday()) == dia && !intervaloEnBloques(item.Inicio, item.Fin, bloques, loc) {
			return false, nil
		}
	}
	return true, nil
}

func (s *servidorReservas) consultarAgenda(c *gin.Context) {
	u := c.MustGet("identidad").(identidad)
	profesionalID := strings.TrimSpace(c.Query("profesionalId"))
	if tieneRol(u, "profesional") && !tieneRol(u, "recepcion", "administrador", "superadministrador") {
		resolvedID, status, err := s.resolverPerfilProfesional(c.Request.Context(), u.ID)
		if err != nil {
			c.JSON(status, gin.H{"error": gin.H{"code": "PERFIL_PROFESIONAL_NO_DISPONIBLE", "message": err.Error()}})
			return
		}
		profesionalID = resolvedID
	}
	if profesionalID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "PROFESIONAL_REQUERIDO", "message": "Indicá un perfil profesional."}})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	cur, err := s.agendas.Find(ctx, bson.M{"profesionalId": profesionalID}, options.Find().SetSort(bson.D{{Key: "diaSemana", Value: 1}}))
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no se pudo consultar la agenda"})
		return
	}
	defer cur.Close(ctx)
	var guardadas []agendaSemanal
	if err := cur.All(ctx, &guardadas); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no se pudo leer la agenda"})
		return
	}
	porDia := make(map[int]agendaSemanal, len(guardadas))
	for _, item := range guardadas {
		porDia[item.DiaSemana] = item
	}
	dias := make([]agendaSemanal, 0, 7)
	for dia := 0; dia < 7; dia++ {
		item, ok := porDia[dia]
		if !ok {
			item = agendaSemanal{ID: agendaControlID(profesionalID, dia), ProfesionalID: profesionalID, DiaSemana: dia, Bloques: []bloqueAgenda{}, Activo: false}
		}
		if item.Bloques == nil {
			item.Bloques = bloquesAgendaDe(item)
		}
		dias = append(dias, item)
	}
	c.JSON(http.StatusOK, gin.H{"profesionalId": profesionalID, "dias": dias})
}

func (s *servidorReservas) guardarAgenda(c *gin.Context) {
	u := c.MustGet("identidad").(identidad)
	var req agendaRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "JSON inválido"})
		return
	}
	profesionalID := strings.TrimSpace(req.ProfesionalID)
	if tieneRol(u, "profesional") && !tieneRol(u, "recepcion", "administrador", "superadministrador") {
		resolvedID, status, err := s.resolverPerfilProfesional(c.Request.Context(), u.ID)
		if err != nil {
			c.JSON(status, gin.H{"error": gin.H{"code": "PERFIL_PROFESIONAL_NO_DISPONIBLE", "message": err.Error()}})
			return
		}
		profesionalID = resolvedID
	}
	if req.Bloques == nil {
		if req.Activo != nil && !*req.Activo {
			req.Bloques = []bloqueAgenda{}
		} else if req.HoraInicio != "" || req.HoraFin != "" {
			req.Bloques = []bloqueAgenda{{Inicio: req.HoraInicio, Fin: req.HoraFin}}
		} else {
			req.Bloques = []bloqueAgenda{}
		}
	}
	bloques, valid := validarBloquesAgenda(req.Bloques)
	if profesionalID == "" || req.DiaSemana < 0 || req.DiaSemana > 6 || !valid {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "AGENDA_INVALIDA", "message": "Usá bloques no superpuestos entre las 08:00 y las 20:00."}})
		return
	}
	doc := agendaSemanal{ID: agendaControlID(profesionalID, req.DiaSemana), ProfesionalID: profesionalID, DiaSemana: req.DiaSemana, Bloques: bloques, Activo: len(bloques) > 0}
	if len(bloques) > 0 {
		doc.HoraInicio, doc.HoraFin = bloques[0].Inicio, bloques[len(bloques)-1].Fin
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	if err := s.asegurarAgendaControl(ctx, profesionalID, req.DiaSemana); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no se pudo bloquear la modificación de agenda"})
		return
	}
	session, err := s.mongo.StartSession()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no se pudo guardar la agenda"})
		return
	}
	defer session.EndSession(ctx)
	_, err = session.WithTransaction(ctx, func(tx context.Context) (any, error) {
		if _, e := s.agendaControl().UpdateOne(tx, bson.M{"_id": agendaControlID(profesionalID, req.DiaSemana)}, bson.M{"$inc": bson.M{"version": 1}}); e != nil {
			return nil, e
		}
		compatible, e := s.agendaRespetaReservas(tx, profesionalID, req.DiaSemana, bloques)
		if e != nil {
			return nil, e
		}
		if !compatible {
			return nil, errAgendaIncompatible
		}
		_, e = s.agendas.UpdateOne(tx, bson.M{"profesionalId": profesionalID, "diaSemana": req.DiaSemana}, bson.M{
			"$set":         bson.M{"profesionalId": doc.ProfesionalID, "diaSemana": doc.DiaSemana, "horaInicio": doc.HoraInicio, "horaFin": doc.HoraFin, "bloques": doc.Bloques, "activo": doc.Activo},
			"$setOnInsert": bson.M{"_id": doc.ID},
		}, options.UpdateOne().SetUpsert(true))
		return doc, e
	})
	if err != nil {
		if errors.Is(err, errAgendaIncompatible) {
			c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code": err.Error(), "message": "La nueva disponibilidad dejaría fuera un turno confirmado o un horario retenido."}})
			return
		}
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no se pudo guardar la agenda"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"agenda": doc})
}

func (s *servidorReservas) disponibilidad(c *gin.Context) {
	prof := strings.TrimSpace(c.Query("profesionalId"))
	fecha, err := time.Parse("2006-01-02", c.Query("fecha"))
	if prof == "" || err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "PARAMETROS_INVALIDOS", "message": "Usá profesionalId y fecha (YYYY-MM-DD)."}})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	tratamientoID := strings.TrimSpace(c.Query("tratamientoId"))
	if tratamientoID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "PARAMETROS_INVALIDOS", "message": "Enviá tratamientoId para consultar la duración real del tratamiento."}})
		return
	}
	tratamiento, status, catErr := s.obtenerTratamiento(ctx, tratamientoID)
	if catErr != nil {
		c.JSON(status, gin.H{"error": gin.H{"code": "TRATAMIENTO_NO_VALIDO", "message": catErr.Error()}})
		return
	}
	if !tratamientoCompatible(tratamiento, prof) {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": gin.H{"code": "PROFESIONAL_NO_APTO", "message": "El profesional seleccionado no está habilitado para realizar este tratamiento."}})
		return
	}
	dur := tratamiento.DuracionMinutos
	loc := zonaHorariaReservas()
	base := time.Date(fecha.Year(), fecha.Month(), fecha.Day(), 0, 0, 0, 0, loc)
	var agenda agendaSemanal
	if err := s.agendas.FindOne(ctx, bson.D{{Key: "profesionalId", Value: prof}, {Key: "diaSemana", Value: int(base.Weekday())}, {Key: "activo", Value: true}}).Decode(&agenda); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			c.JSON(http.StatusOK, gin.H{"profesionalId": prof, "fecha": c.Query("fecha"), "horarios": []string{}})
			return
		}
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no se pudo consultar la agenda"})
		return
	}
	if !agendaValida(agenda.DiaSemana, agenda.HoraInicio, agenda.HoraFin) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "AGENDA_INVALIDA", "message": "La agenda del profesional contiene un rango horario inválido."}})
		return
	}
	if !agenda.Activo {
		c.JSON(http.StatusOK, gin.H{"profesionalId": prof, "fecha": c.Query("fecha"), "horarios": []string{}})
		return
	}
	bloques, agendaOK := validarBloquesAgenda(bloquesAgendaDe(agenda))
	if !agendaOK {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "AGENDA_INVALIDA", "message": "La agenda del profesional contiene bloques inválidos."}})
		return
	}
	var slots []string
	u := c.MustGet("identidad").(identidad)
	clienteID := ""
	if tieneRol(u, "cliente") {
		clienteID = u.ID
	}
	for _, bloque := range bloques {
		startMin, _ := parseHora(bloque.Inicio)
		endMin, _ := parseHora(bloque.Fin)
		for m := primerIntervaloDe15Minutos(startMin); m+dur <= endMin; m += 15 {
			start := base.Add(time.Duration(m) * time.Minute)
			end := start.Add(time.Duration(dur) * time.Minute)
			ocupado, busyErr := s.horarioOcupado(ctx, prof, clienteID, tratamiento.CategoriaID, start, end)
			if busyErr != nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no se pudo consultar disponibilidad"})
				return
			}
			if !ocupado && start.After(time.Now()) {
				slots = append(slots, start.Format("15:04"))
			}
		}
	}
	if slots == nil {
		slots = []string{}
	}
	c.JSON(http.StatusOK, gin.H{"profesionalId": prof, "fecha": c.Query("fecha"), "duracionMinutos": dur, "horarios": slots})
}

func primerIntervaloDe15Minutos(minuto int) int { return ((minuto + 14) / 15) * 15 }

func zonaHorariaReservas() *time.Location {
	nombre := strings.TrimSpace(os.Getenv("MELT_TIMEZONE"))
	if nombre == "" {
		nombre = "America/Argentina/Buenos_Aires"
	}
	loc, err := time.LoadLocation(nombre)
	if err != nil {
		log.Printf("zona horaria MELT_TIMEZONE=%q inválida; se usa America/Argentina/Buenos_Aires", nombre)
		return time.FixedZone("America/Argentina/Buenos_Aires", -3*60*60)
	}
	return loc
}

func (s *servidorReservas) horarioOcupado(ctx context.Context, profesionalID, clienteID, categoriaID string, inicio, fin time.Time) (bool, error) {
	turnos, err := s.turnos.CountDocuments(ctx, bson.D{{Key: "profesionalId", Value: profesionalID}, {Key: "estado", Value: "confirmado"}, {Key: "inicio", Value: bson.D{{Key: "$lt", Value: fin}}}, {Key: "fin", Value: bson.D{{Key: "$gt", Value: inicio}}}})
	if err != nil {
		return false, err
	}
	if turnos > 0 {
		return true, nil
	}
	minutoInicio := inicio.UTC().Unix() / 60
	minutoFin := fin.UTC().Unix() / 60
	filter := bson.D{
		{Key: "profesionalId", Value: profesionalID},
		{Key: "minuto", Value: bson.D{{Key: "$gte", Value: minutoInicio}, {Key: "$lt", Value: minutoFin}}},
		{Key: "$or", Value: bson.A{
			bson.D{{Key: "expiraEn", Value: bson.D{{Key: "$exists", Value: false}}}},
			bson.D{{Key: "expiraEn", Value: bson.D{{Key: "$gt", Value: time.Now().UTC()}}}},
		}},
	}
	reservasTemporales, err := s.bloqueos.CountDocuments(ctx, filter)
	if err != nil || reservasTemporales > 0 {
		return reservasTemporales > 0, err
	}
	if clienteID != "" && categoriaID != "" {
		conflicto, err := s.categoriaOcupadaCliente(ctx, clienteID, categoriaID, inicio, fin)
		return conflicto, err
	}
	return false, nil
}

func (s *servidorReservas) categoriaOcupadaCliente(ctx context.Context, clienteID, categoriaID string, inicio, fin time.Time) (bool, error) {
	minutoInicio, minutoFin := inicio.UTC().Unix()/60, fin.UTC().Unix()/60
	filter := bson.M{"clienteId": clienteID, "categoriaId": categoriaID, "minuto": bson.M{"$gte": minutoInicio, "$lt": minutoFin}, "$or": bson.A{bson.M{"expiraEn": bson.M{"$exists": false}}, bson.M{"expiraEn": bson.M{"$gt": time.Now().UTC()}}}}
	count, err := s.bloqueos.CountDocuments(ctx, filter)
	if err != nil || count > 0 {
		return count > 0, err
	}
	turns, err := s.turnos.Find(ctx, bson.M{"clienteId": clienteID, "estado": "confirmado", "inicio": bson.M{"$lt": fin}, "fin": bson.M{"$gt": inicio}})
	if err != nil {
		return false, err
	}
	defer turns.Close(ctx)
	for turns.Next(ctx) {
		var item turno
		if err := turns.Decode(&item); err != nil {
			return false, err
		}
		id := item.CategoriaID
		if id == "" {
			t, _, e := s.obtenerTratamiento(ctx, item.TratamientoID)
			if e != nil {
				return false, e
			}
			id = t.CategoriaID
		}
		if id == categoriaID {
			return true, nil
		}
	}
	if err := turns.Err(); err != nil {
		return false, err
	}
	carts, err := s.carritos.Find(ctx, bson.M{"clienteId": clienteID, "estado": "pendiente_pago", "expiraEn": bson.M{"$gt": time.Now().UTC()}, "inicio": bson.M{"$lt": fin}, "fin": bson.M{"$gt": inicio}})
	if err != nil {
		return false, err
	}
	defer carts.Close(ctx)
	for carts.Next(ctx) {
		var item bloqueoCarrito
		if err := carts.Decode(&item); err != nil {
			return false, err
		}
		id := item.CategoriaID
		if id == "" {
			t, _, e := s.obtenerTratamiento(ctx, item.TratamientoID)
			if e != nil {
				return false, e
			}
			id = t.CategoriaID
		}
		if id == categoriaID {
			return true, nil
		}
	}
	return false, carts.Err()
}

func (s *servidorReservas) crearTurno(c *gin.Context) {
	u := c.MustGet("identidad").(identidad)
	var req struct {
		ProfesionalID string `json:"profesionalId" binding:"required"`
		TratamientoID string `json:"tratamientoId" binding:"required"`
		Inicio        string `json:"inicio" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "TURNO_INVALIDO", "message": "Indicá profesionalId, tratamientoId e inicio RFC3339."}})
		return
	}
	req.ProfesionalID = strings.TrimSpace(req.ProfesionalID)
	req.TratamientoID = strings.TrimSpace(req.TratamientoID)
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	tratamiento, status, catErr := s.obtenerTratamiento(ctx, req.TratamientoID)
	if catErr != nil {
		c.JSON(status, gin.H{"error": gin.H{"code": "TRATAMIENTO_NO_VALIDO", "message": catErr.Error()}})
		return
	}
	if !tratamientoCompatible(tratamiento, req.ProfesionalID) {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": gin.H{"code": "PROFESIONAL_NO_APTO", "message": "El profesional seleccionado no está habilitado para realizar este tratamiento."}})
		return
	}
	start, err := time.Parse(time.RFC3339, req.Inicio)
	if err != nil || !start.After(time.Now()) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "el inicio debe ser una fecha futura RFC3339"})
		return
	}
	localStart := start.In(zonaHorariaReservas())
	duracionMinutos := tratamiento.DuracionMinutos
	end := start.Add(time.Duration(duracionMinutos) * time.Minute)
	if localStart.Second() != 0 || localStart.Nanosecond() != 0 || localStart.Minute()%15 != 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INTERVALO_INVALIDO", "message": "El inicio debe coincidir con un intervalo de 15 minutos."}})
		return
	}
	var agenda agendaSemanal
	if err := s.agendas.FindOne(ctx, bson.D{{Key: "profesionalId", Value: req.ProfesionalID}, {Key: "diaSemana", Value: int(localStart.Weekday())}, {Key: "activo", Value: true}}).Decode(&agenda); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code": "FUERA_DE_AGENDA", "message": "El profesional no atiende ese día."}})
		return
	}
	if !agendaValida(agenda.DiaSemana, agenda.HoraInicio, agenda.HoraFin) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "AGENDA_INVALIDA", "message": "La agenda del profesional contiene un rango horario inválido."}})
		return
	}
	from, _ := parseHora(agenda.HoraInicio)
	to, _ := parseHora(agenda.HoraFin)
	minute := localStart.Hour()*60 + localStart.Minute()
	if minute < from || minute+duracionMinutos > to {
		c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code": "FUERA_DE_AGENDA", "message": "El horario queda fuera de la agenda del profesional."}})
		return
	}
	var overlap turno
	err = s.turnos.FindOne(ctx, bson.D{{Key: "profesionalId", Value: req.ProfesionalID}, {Key: "estado", Value: "confirmado"}, {Key: "inicio", Value: bson.D{{Key: "$lt", Value: end}}}, {Key: "fin", Value: bson.D{{Key: "$gt", Value: start}}}}).Decode(&overlap)
	if err == nil {
		c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code": "HORARIO_OCUPADO", "message": "Ese horario ya está reservado."}})
		return
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no se pudo verificar el horario"})
		return
	}
	doc := turno{ID: bson.NewObjectID().Hex(), ClienteID: u.ID, ProfesionalID: req.ProfesionalID, TratamientoID: tratamiento.ID, CategoriaID: tratamiento.CategoriaID, Inicio: start.UTC(), Fin: end.UTC(), Estado: "confirmado", CreadoEn: time.Now().UTC()}
	// El índice único en profesionalId + minuto coordina todas las réplicas.
	bloqueos := make([]interface{}, 0, duracionMinutos)
	for minuto := start.UTC().Unix() / 60; minuto < end.UTC().Unix()/60; minuto++ {
		bloqueos = append(bloqueos, bloqueoTurno{ProfesionalID: req.ProfesionalID, ClienteID: u.ID, CategoriaID: tratamiento.CategoriaID, Minuto: minuto, TurnoID: doc.ID})
	}
	if len(bloqueos) > 0 {
		if _, err := s.bloqueos.InsertMany(ctx, bloqueos); err != nil {
			_, _ = s.bloqueos.DeleteMany(context.Background(), bson.D{{Key: "turnoId", Value: doc.ID}})
			if mongo.IsDuplicateKeyError(err) {
				c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code": "HORARIO_OCUPADO", "message": "Ese horario ya está reservado o fue tomado por otra solicitud."}})
				return
			}
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no se pudo bloquear el horario de forma segura"})
			return
		}
	}
	if _, err := s.turnos.InsertOne(ctx, doc); err != nil {
		_, _ = s.bloqueos.DeleteMany(context.Background(), bson.D{{Key: "turnoId", Value: doc.ID}})
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no se pudo crear el turno"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"turno": doc})
}

func (s *servidorReservas) listarTurnos(c *gin.Context) {
	u := c.MustGet("identidad").(identidad)
	filter := bson.D{}
	if tieneRol(u, "recepcion", "administrador", "superadministrador") {
		// Personal autorizado puede consultar todos los turnos.
	} else if tieneRol(u, "cliente") {
		filter = append(filter, bson.E{Key: "clienteId", Value: u.ID})
	} else if tieneRol(u, "profesional") {
		profesionalID, status, err := s.resolverPerfilProfesional(c.Request.Context(), u.ID)
		if err != nil {
			c.JSON(status, gin.H{"error": gin.H{"code": "PERFIL_PROFESIONAL_NO_DISPONIBLE", "message": err.Error()}})
			return
		}
		filter = append(filter, bson.E{Key: "profesionalId", Value: profesionalID})
	} else {
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "PERMISOS_INSUFICIENTES", "message": "No tenés permisos para consultar turnos."}})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	cur, err := s.turnos.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "inicio", Value: 1}}))
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no se pudieron consultar los turnos"})
		return
	}
	defer cur.Close(ctx)
	out := []turno{}
	if err := cur.All(ctx, &out); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no se pudieron leer los turnos"})
		return
	}
	if tieneRol(u, "profesional") && !tieneRol(u, "recepcion", "administrador", "superadministrador") {
		for i := range out {
			out[i].ClienteNombre = s.nombreClienteInterno(ctx, out[i].ClienteID)
		}
	}
	c.JSON(http.StatusOK, gin.H{"turnos": out})
}

func (s *servidorReservas) devolverSena(ctx context.Context, pagoID string, importe float64, idempotencyKey string) error {
	if s.pagosURL == "" {
		return errors.New("proveedor de pagos no configurado")
	}
	payload, _ := json.Marshal(gin.H{"pagoId": pagoID, "importe": importe, "moneda": "ARS", "motivo": "cancelacion_turno"})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.pagosURL+"/payments/refunds", strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", idempotencyKey)
	req.Header.Set("X-Melt-Internal-Key", s.internalKey)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusAccepted {
		return errors.New("el proveedor no confirmó la devolución")
	}
	return nil
}

func (s *servidorReservas) cancelarTurno(c *gin.Context) {
	u := c.MustGet("identidad").(identidad)
	id := strings.TrimSpace(c.Param("id"))
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	var t turno
	if err := s.turnos.FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Decode(&t); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			c.JSON(http.StatusNotFound, gin.H{"error": "turno no encontrado"})
			return
		}
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no se pudo consultar el turno"})
		return
	}
	own := t.ClienteID == u.ID
	staff := tieneRol(u, "recepcion", "administrador", "superadministrador")
	profesionalPropio := false
	if tieneRol(u, "profesional") {
		profesionalID, status, err := s.resolverPerfilProfesional(ctx, u.ID)
		if err != nil {
			c.JSON(status, gin.H{"error": gin.H{"code": "PERFIL_PROFESIONAL_NO_DISPONIBLE", "message": err.Error()}})
			return
		}
		profesionalPropio = t.ProfesionalID == profesionalID
	}
	if !own && !staff && !profesionalPropio {
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "PERMISOS_INSUFICIENTES", "message": "Solo podés cancelar tus propios turnos."}})
		return
	}
	if t.Estado != "confirmado" {
		c.JSON(http.StatusConflict, gin.H{"error": "el turno ya no está confirmado"})
		return
	}
	ahora := time.Now().UTC()
	if t.Inicio.Before(ahora) {
		c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code": "TURNO_INICIADO", "message": "No se puede cancelar un turno que ya comenzó."}})
		return
	}
	devolucion := 0.0
	estadoDevolucion := "no_corresponde"
	if t.PagoID != "" {
		devolucion = importeDevolucion(t.Sena, t.Inicio, ahora)
		if devolucion > 0 {
			estadoDevolucion = "pendiente"
		}
	}
	if err := s.cancelarConEvento(ctx, t, devolucion, estadoDevolucion); err != nil {
		if errors.Is(err, errTurnoNoConfirmado) {
			c.JSON(http.StatusConflict, gin.H{"error": "el turno ya no está confirmado"})
			return
		}
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "CANCELACION_NO_REGISTRADA", "message": "No se pudo guardar la cancelación de forma segura."}})
		return
	}
	if devolucion > 0 {
		if err := s.devolverSena(ctx, t.PagoID, devolucion, "refund-turno-"+id); err != nil {
			c.JSON(http.StatusAccepted, gin.H{"turnoId": id, "estado": "cancelado", "devolucion": gin.H{"importe": devolucion, "moneda": "ARS", "estado": "pendiente", "mensaje": "La cancelación quedó registrada; la devolución requiere reintento operativo."}})
			return
		}
		_, _ = s.turnos.UpdateOne(ctx, bson.D{{Key: "_id", Value: id}, {Key: "estado", Value: "cancelado"}, {Key: "estadoDevolucion", Value: "pendiente"}}, bson.D{{Key: "$set", Value: bson.D{{Key: "estadoDevolucion", Value: "devuelta"}}}})
		estadoDevolucion = "devuelta"
	}
	c.JSON(http.StatusOK, gin.H{"turnoId": id, "estado": "cancelado", "devolucion": gin.H{"importe": devolucion, "moneda": "ARS", "estado": estadoDevolucion, "regla": "100% si se cancela con 24 horas o más; sin devolución con menos de 24 horas"}})
}

type bloqueoCarrito struct {
	ID                 string    `bson:"_id" json:"id"`
	ClienteID          string    `bson:"clienteId" json:"clienteId"`
	ProfesionalID      string    `bson:"profesionalId" json:"profesionalId"`
	TratamientoID      string    `bson:"tratamientoId" json:"tratamientoId"`
	CategoriaID        string    `bson:"categoriaId,omitempty" json:"categoriaId,omitempty"`
	Inicio             time.Time `bson:"inicio" json:"inicio"`
	Fin                time.Time `bson:"fin" json:"fin"`
	Precio             float64   `bson:"precio" json:"precio"`
	Moneda             string    `bson:"moneda" json:"moneda"`
	Sena               float64   `bson:"sena" json:"sena"`
	BeneficioCanjeID   string    `bson:"beneficioCanjeId,omitempty" json:"beneficioCanjeId,omitempty"`
	DescuentoBeneficio float64   `bson:"descuentoBeneficio,omitempty" json:"descuentoBeneficio,omitempty"`
	PrecioFinal        float64   `bson:"precioFinal,omitempty" json:"precioFinal,omitempty"`
	Estado             string    `bson:"estado" json:"estado"`
	ExpiraEn           time.Time `bson:"expiraEn" json:"expiraEn"`
	TurnoID            string    `bson:"turnoId,omitempty" json:"turnoId,omitempty"`
	PagoID             string    `bson:"pagoId,omitempty" json:"pagoId,omitempty"`
	EstadoPago         string    `bson:"estadoPago,omitempty" json:"estadoPago,omitempty"`
	PreferenciaID      string    `bson:"preferenciaId,omitempty" json:"preferenciaId,omitempty"`
	URLPago            string    `bson:"urlPago,omitempty" json:"urlPago,omitempty"`
	CheckoutID         string    `bson:"checkoutId,omitempty" json:"checkoutId,omitempty"`
}

type solicitudCarrito struct {
	ProfesionalID    string `json:"profesionalId" binding:"required"`
	TratamientoID    string `json:"tratamientoId" binding:"required"`
	Inicio           string `json:"inicio" binding:"required"`
	BeneficioCanjeID string `json:"beneficioCanjeId,omitempty"`
	DiferirPago      bool   `json:"diferirPago,omitempty"`
}

func importeSena(precio float64) float64 {
	return math.Round(precio*0.30*100) / 100
}

func importeDevolucion(sena float64, inicio, ahora time.Time) float64 {
	if sena > 0 && !inicio.Before(ahora.Add(24*time.Hour)) {
		return sena
	}
	return 0
}

func (s *servidorReservas) descartarBloqueoPendiente(id, clienteID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var cart bloqueoCarrito
	if s.carritos.FindOne(ctx, bson.D{{Key: "_id", Value: id}, {Key: "clienteId", Value: clienteID}}).Decode(&cart) == nil {
		s.liberarBeneficio(ctx, cart.BeneficioCanjeID, clienteID, id)
	}
	_, _ = s.bloqueos.DeleteMany(ctx, bson.D{{Key: "turnoId", Value: id}})
	_, _ = s.carritos.DeleteOne(ctx, bson.D{{Key: "_id", Value: id}, {Key: "clienteId", Value: clienteID}, {Key: "estado", Value: "pendiente_pago"}})
}

func (s *servidorReservas) listarCarrito(c *gin.Context) {
	u := c.MustGet("identidad").(identidad)
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	cur, err := s.carritos.Find(ctx, bson.M{"clienteId": u.ID, "estado": "pendiente_pago", "expiraEn": bson.M{"$gt": time.Now().UTC()}}, options.Find().SetSort(bson.D{{Key: "creadoEn", Value: 1}, {Key: "inicio", Value: 1}}))
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no se pudo consultar el carrito"})
		return
	}
	defer cur.Close(ctx)
	items := []bloqueoCarrito{}
	if err := cur.All(ctx, &items); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no se pudo leer el carrito"})
		return
	}
	resumen := gin.H{"subtotal": float64(0), "descuento": float64(0), "total": float64(0), "sena": float64(0), "restante": float64(0)}
	var subtotal, descuento, total, sena float64
	for _, item := range items {
		subtotal += item.Precio
		descuento += item.DescuentoBeneficio
		total += item.PrecioFinal
		sena += item.Sena
	}
	if total == 0 {
		total = subtotal - descuento
	}
	resumen["subtotal"], resumen["descuento"], resumen["total"], resumen["sena"], resumen["restante"] = subtotal, descuento, total, sena, math.Max(0, total-sena)
	c.JSON(http.StatusOK, gin.H{"items": items, "resumen": resumen})
}

func (s *servidorReservas) iniciarPagoBloqueo(c *gin.Context) {
	c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code": "CHECKOUT_UNICO_REQUERIDO", "message": "Los pagos por elemento ya no están disponibles. Confirmá el carrito completo desde POST /carrito/checkout."}})
}

func (s *servidorReservas) crearBloqueoCarrito(c *gin.Context) {
	u := c.MustGet("identidad").(identidad)
	var req solicitudCarrito
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "CARRITO_INVALIDO", "message": "Indicá profesionalId, tratamientoId e inicio RFC3339."}})
		return
	}
	req.ProfesionalID = strings.TrimSpace(req.ProfesionalID)
	req.TratamientoID = strings.TrimSpace(req.TratamientoID)
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	t, status, err := s.obtenerTratamiento(ctx, req.TratamientoID)
	if err != nil {
		c.JSON(status, gin.H{"error": gin.H{"code": "TRATAMIENTO_NO_VALIDO", "message": err.Error()}})
		return
	}
	if !tratamientoCompatible(t, req.ProfesionalID) {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": gin.H{"code": "PROFESIONAL_NO_APTO", "message": "El profesional no está habilitado para este tratamiento."}})
		return
	}
	if err := s.profesionalCompatible(ctx, req.ProfesionalID, t.ID); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": gin.H{"code": "PROFESIONAL_NO_APTO", "message": "El perfil profesional está inactivo o ya no está asociado al tratamiento."}})
		return
	}
	if t.Precio < 0 || (t.Moneda != "" && t.Moneda != "ARS") {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": gin.H{"code": "PRECIO_INVALIDO", "message": "El tratamiento no tiene un precio válido en ARS."}})
		return
	}
	start, err := time.Parse(time.RFC3339, req.Inicio)
	if err != nil || !start.After(time.Now()) || start.Second() != 0 || start.Nanosecond() != 0 || start.Minute()%15 != 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INTERVALO_INVALIDO", "message": "El inicio debe ser futuro, RFC3339 y coincidir con un intervalo de 15 minutos."}})
		return
	}
	localStart := start.In(zonaHorariaReservas())
	end := start.Add(time.Duration(t.DuracionMinutos) * time.Minute)
	var agenda agendaSemanal
	if err := s.agendas.FindOne(ctx, bson.D{{Key: "profesionalId", Value: req.ProfesionalID}, {Key: "diaSemana", Value: int(localStart.Weekday())}, {Key: "activo", Value: true}}).Decode(&agenda); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code": "FUERA_DE_AGENDA", "message": "El profesional no atiende ese día."}})
		return
	}
	bloquesAgenda, blocksValid := validarBloquesAgenda(bloquesAgendaDe(agenda))
	if !agenda.Activo || !blocksValid {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "AGENDA_INVALIDA", "message": "La agenda del profesional contiene un rango horario inválido."}})
		return
	}
	if !intervaloEnBloques(start, end, bloquesAgenda, zonaHorariaReservas()) {
		c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code": "FUERA_DE_AGENDA", "message": "El horario queda fuera de la agenda del profesional."}})
		return
	}
	now := time.Now().UTC()
	_, _ = s.bloqueos.DeleteMany(ctx, bson.D{{Key: "expiraEn", Value: bson.D{{Key: "$lte", Value: now}}}})
	var overlap turno
	err = s.turnos.FindOne(ctx, bson.D{{Key: "profesionalId", Value: req.ProfesionalID}, {Key: "estado", Value: "confirmado"}, {Key: "inicio", Value: bson.D{{Key: "$lt", Value: end}}}, {Key: "fin", Value: bson.D{{Key: "$gt", Value: start}}}}).Decode(&overlap)
	if err == nil {
		c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code": "HORARIO_OCUPADO", "message": "Ese horario ya está reservado."}})
		return
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no se pudo verificar el horario"})
		return
	}
	if categoriaBusy, categoryErr := s.categoriaOcupadaCliente(ctx, u.ID, t.CategoriaID, start, end); categoryErr != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "DISPONIBILIDAD_NO_DISPONIBLE", "message": "No se pudo verificar si ya tenés un tratamiento de esta categoría en ese horario."}})
		return
	} else if categoriaBusy {
		c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code": "CATEGORIA_SUPERPUESTA", "message": "Ya tenés un tratamiento de esta categoría en un horario superpuesto."}})
		return
	}
	id := bson.NewObjectID().Hex()
	expira := now.Add(10 * time.Minute)
	doc := bloqueoCarrito{ID: id, ClienteID: u.ID, ProfesionalID: req.ProfesionalID, TratamientoID: t.ID, CategoriaID: t.CategoriaID, Inicio: start.UTC(), Fin: end.UTC(), Precio: t.Precio, PrecioFinal: t.Precio, Moneda: "ARS", Sena: importeSena(t.Precio), Estado: "pendiente_pago", ExpiraEn: expira}
	if req.BeneficioCanjeID != "" {
		canje, reserveErr := s.reservarBeneficio(ctx, strings.TrimSpace(req.BeneficioCanjeID), u.ID, id, t.ID, t.Precio)
		if reserveErr != nil {
			c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code": "BENEFICIO_NO_DISPONIBLE", "message": "No pudimos reservar ese beneficio para este tratamiento."}})
			return
		}
		doc.BeneficioCanjeID = canje.ID
		doc.DescuentoBeneficio = canje.Descuento
		doc.PrecioFinal = canje.PrecioFinal
		doc.Sena = importeSena(canje.PrecioFinal)
	}
	bloqueos := make([]interface{}, 0, t.DuracionMinutos)
	for minuto := start.UTC().Unix() / 60; minuto < end.UTC().Unix()/60; minuto++ {
		bloqueos = append(bloqueos, bloqueoTurno{ProfesionalID: req.ProfesionalID, ClienteID: u.ID, CategoriaID: t.CategoriaID, Minuto: minuto, TurnoID: id, ExpiraEn: &expira})
	}
	if err := s.asegurarAgendaControl(ctx, req.ProfesionalID, int(localStart.Weekday())); err != nil {
		s.liberarBeneficio(context.Background(), doc.BeneficioCanjeID, u.ID, id)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no se pudo proteger la agenda durante la reserva"})
		return
	}
	session, sessionErr := s.mongo.StartSession()
	if sessionErr != nil {
		s.liberarBeneficio(context.Background(), doc.BeneficioCanjeID, u.ID, id)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no se pudo bloquear el horario"})
		return
	}
	defer session.EndSession(ctx)
	_, err = session.WithTransaction(ctx, func(tx context.Context) (any, error) {
		if _, e := s.agendaControl().UpdateOne(tx, bson.M{"_id": agendaControlID(req.ProfesionalID, int(localStart.Weekday()))}, bson.M{"$inc": bson.M{"version": 1}}); e != nil {
			return nil, e
		}
		var agendaActual agendaSemanal
		if e := s.agendas.FindOne(tx, bson.M{"profesionalId": req.ProfesionalID, "diaSemana": int(localStart.Weekday()), "activo": true}).Decode(&agendaActual); e != nil {
			return nil, errAgendaIncompatible
		}
		bloquesActuales, ok := validarBloquesAgenda(bloquesAgendaDe(agendaActual))
		if !ok || !intervaloEnBloques(start, end, bloquesActuales, zonaHorariaReservas()) {
			return nil, errAgendaIncompatible
		}
		if _, e := s.bloqueos.InsertMany(tx, bloqueos); e != nil {
			return nil, e
		}
		if _, e := s.carritos.InsertOne(tx, doc); e != nil {
			return nil, e
		}
		return doc, nil
	})
	if err != nil {
		s.liberarBeneficio(context.Background(), doc.BeneficioCanjeID, u.ID, id)
		if mongo.IsDuplicateKeyError(err) {
			c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code": "HORARIO_OCUPADO", "message": "Ese horario ya está reservado o bloqueado por otro carrito."}})
			return
		}
		if errors.Is(err, errAgendaIncompatible) {
			c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code": "FUERA_DE_AGENDA", "message": "La disponibilidad cambió. Volvé a consultar los horarios."}})
			return
		}
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no se pudo bloquear el horario de forma segura"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"bloqueo": doc, "seña": gin.H{"porcentaje": 30, "importe": doc.Sena, "moneda": doc.Moneda}, "mensaje": "Tratamiento agregado al carrito. El horario queda retenido durante diez minutos; el pago se simula una sola vez desde el checkout."})
}

func (s *servidorReservas) liberarBloqueoCarrito(c *gin.Context) {
	u := c.MustGet("identidad").(identidad)
	id := strings.TrimSpace(c.Param("id"))
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	var cart bloqueoCarrito
	if err := s.carritos.FindOne(ctx, bson.D{{Key: "_id", Value: id}, {Key: "clienteId", Value: u.ID}}).Decode(&cart); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"code": "CARRITO_NO_ENCONTRADO", "message": "No encontramos ese carrito."}})
		return
	}
	session, err := s.mongo.StartSession()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no se pudo liberar el horario"})
		return
	}
	defer session.EndSession(ctx)
	_, err = session.WithTransaction(ctx, func(tx context.Context) (any, error) {
		res, updateErr := s.carritos.UpdateOne(tx, bson.M{"_id": id, "clienteId": u.ID, "estado": "pendiente_pago", "checkoutId": bson.M{"$in": bson.A{"", nil}}}, bson.M{"$set": bson.M{"estado": "liberado", "expiraEn": time.Now().UTC()}})
		if updateErr != nil {
			return nil, updateErr
		}
		if res.MatchedCount == 0 {
			var locked bloqueoCarrito
			if e := s.carritos.FindOne(tx, bson.M{"_id": id, "clienteId": u.ID, "checkoutId": bson.M{"$nin": bson.A{"", nil}}}).Decode(&locked); e == nil {
				return nil, errCheckoutEnCurso
			}
			return nil, mongo.ErrNoDocuments
		}
		if _, deleteErr := s.bloqueos.DeleteMany(tx, bson.M{"turnoId": id}); deleteErr != nil {
			return nil, deleteErr
		}
		return nil, nil
	})
	if errors.Is(err, mongo.ErrNoDocuments) {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"code": "CARRITO_NO_ENCONTRADO", "message": "No existe un bloqueo pendiente de tu propiedad."}})
		return
	}
	if errors.Is(err, errCheckoutEnCurso) {
		c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code": "CHECKOUT_EN_CURSO", "message": "No se puede quitar un elemento mientras se confirma la compra."}})
		return
	}
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no se pudo liberar el carrito"})
		return
	}
	s.liberarBeneficio(ctx, cart.BeneficioCanjeID, u.ID, id)
	c.Status(http.StatusNoContent)
}

func (s *servidorReservas) confirmarBloqueoCarrito(c *gin.Context) {
	c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code": "CHECKOUT_UNICO_REQUERIDO", "message": "La confirmación por elemento ya no está disponible. Confirmá el carrito completo desde POST /carrito/checkout."}})
}

func (s *servidorReservas) confirmarTurnoDesdeCarrito(c *gin.Context) {
	c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "CARRITO_REQUERIDO", "message": "Primero bloqueá el horario con POST /carrito/bloqueos y luego confirmalo con el pago aprobado en POST /carrito/bloqueos/{id}/confirmar."}})
}

func (s *servidorReservas) reintentarDevolucion(c *gin.Context) {
	u := c.MustGet("identidad").(identidad)
	id := strings.TrimSpace(c.Param("id"))
	ctx, cancel := context.WithTimeout(c.Request.Context(), 8*time.Second)
	defer cancel()
	var t turno
	if err := s.turnos.FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Decode(&t); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "turno no encontrado"})
		return
	}
	if t.ClienteID != u.ID && !tieneRol(u, "recepcion", "administrador", "superadministrador") {
		c.JSON(http.StatusForbidden, gin.H{"error": "no tenés permiso para gestionar esta devolución"})
		return
	}
	if t.Estado != "cancelado" || t.EstadoDevolucion != "pendiente" || t.ImporteDevolucion <= 0 || t.PagoID == "" {
		c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code": "DEVOLUCION_NO_PENDIENTE", "message": "El turno no tiene una devolución pendiente."}})
		return
	}
	if err := s.devolverSena(ctx, t.PagoID, t.ImporteDevolucion, "refund-turno-"+id); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "DEVOLUCION_PENDIENTE", "message": "El proveedor todavía no confirmó la devolución; se puede reintentar."}})
		return
	}
	_, err := s.turnos.UpdateOne(ctx, bson.D{{Key: "_id", Value: id}, {Key: "estadoDevolucion", Value: "pendiente"}}, bson.D{{Key: "$set", Value: bson.D{{Key: "estadoDevolucion", Value: "devuelta"}}}})
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "la devolución fue enviada pero no se pudo actualizar su estado"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"turnoId": id, "devolucion": gin.H{"importe": t.ImporteDevolucion, "moneda": "ARS", "estado": "devuelta"}})
}

func slotNoSuperpuesto(start, end, otherStart, otherEnd time.Time) bool {
	return !start.Before(otherEnd) || !end.After(otherStart)
}
