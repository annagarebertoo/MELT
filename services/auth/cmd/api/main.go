package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/mail"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"golang.org/x/crypto/bcrypt"
)

var rolesValidos = map[string]bool{
	"cliente": true, "profesional": true, "recepcion": true,
	"administrador": true, "superadministrador": true,
}

type Usuario struct {
	ID                    string    `bson:"id" json:"id"`
	Nombre                string    `bson:"nombre" json:"nombre"`
	Email                 string    `bson:"email" json:"email"`
	PasswordHash          string    `bson:"passwordHash" json:"-"`
	Roles                 []string  `bson:"roles" json:"roles"`
	Activo                bool      `bson:"activo" json:"activo"`
	CreadoEn              time.Time `bson:"creadoEn" json:"creadoEn"`
	EmailVerificado       bool      `bson:"emailVerificado,omitempty" json:"emailVerificado"`
	AcreditacionPendiente string    `bson:"acreditacionPendiente,omitempty" json:"-"`
}

type registroRequest struct {
	Nombre          string `json:"nombre" binding:"required"`
	Email           string `json:"email" binding:"required,email"`
	Password        string `json:"password" binding:"required,min=8"`
	InvitacionToken string `json:"invitacionToken,omitempty"`
}

type crearUsuarioRequest struct {
	Nombre   string   `json:"nombre" binding:"required"`
	Email    string   `json:"email" binding:"required,email"`
	Password string   `json:"password" binding:"required,min=8"`
	Roles    []string `json:"roles" binding:"required,min=1"`
}

type loginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

type authServer struct {
	client        *mongo.Client
	users         *mongo.Collection
	jwtSecret     []byte
	tokenTTL      time.Duration
	claveInterna  string
	beneficiosURL string
	catalogoURL   string
	roleControl   *mongo.Collection
}

type claims struct {
	Subject string   `json:"sub"`
	Email   string   `json:"email"`
	Roles   []string `json:"roles"`
	Expires int64    `json:"exp"`
	Issued  int64    `json:"iat"`
}

func main() {
	secret := strings.TrimSpace(os.Getenv("AUTH_JWT_SECRET"))
	if len(secret) < 32 {
		log.Fatal("AUTH_JWT_SECRET debe tener al menos 32 caracteres")
	}
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
	dbName := strings.TrimSpace(os.Getenv("AUTH_MONGODB_DATABASE"))
	if dbName == "" {
		dbName = "usuarios_db"
	}
	users := client.Database(dbName).Collection("usuarios")
	_, err = users.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "email", Value: 1}},
		Options: options.Index().SetUnique(true).SetName("uq_usuarios_email"),
	})
	if err != nil {
		log.Fatal("no se pudo crear el índice de email")
	}
	if _, err := users.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "acreditacionPendiente", Value: 1}, {Key: "acreditacionIntentoEn", Value: 1}}, Options: options.Index().SetSparse(true).SetName("ix_acreditaciones_pendientes")}); err != nil {
		log.Fatal("no se pudo preparar la entrega de acreditaciones")
	}
	if err := crearAdminInicial(ctx, users); err != nil {
		log.Fatal("no se pudo configurar el administrador inicial")
	}
	ttl := 15 * time.Minute
	if raw := strings.TrimSpace(os.Getenv("AUTH_TOKEN_TTL_MINUTES")); raw != "" {
		minutes, parseErr := time.ParseDuration(raw + "m")
		if parseErr != nil || minutes < time.Minute || minutes > 24*time.Hour {
			log.Fatal("AUTH_TOKEN_TTL_MINUTES debe estar entre 1 y 1440")
		}
		ttl = minutes
	}
	s := &authServer{client: client, users: users, jwtSecret: []byte(secret), tokenTTL: ttl, claveInterna: strings.TrimSpace(os.Getenv("MELT_INTERNAL_KEY")), beneficiosURL: strings.TrimRight(strings.TrimSpace(os.Getenv("BENEFICIOS_URL")), "/"), catalogoURL: strings.TrimRight(strings.TrimSpace(os.Getenv("CATALOGO_URL")), "/"), roleControl: client.Database(dbName).Collection("control_roles")}
	if s.catalogoURL == "" {
		s.catalogoURL = "http://catalogo-profesionales:8081"
	}
	workerCtx, stopWorker := context.WithCancel(context.Background())
	defer stopWorker()
	go s.recuperarAcreditaciones(workerCtx)
	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "8084"
	}
	if err := s.router().Run(":" + port); err != nil {
		log.Fatal("no se pudo iniciar autenticación")
	}
}

func (s *authServer) router() *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())
	r.GET("/health", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if s == nil || s.client == nil || s.client.Ping(ctx, nil) != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"servicio": "auth", "estado": "degradado"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"servicio": "auth", "estado": "ok"})
	})
	r.GET("/internal/usuarios/:id", s.usuarioInterno)
	r.POST("/internal/usuarios/buscar", s.buscarUsuarioInterno)
	r.POST("/internal/usuarios/verificar-email", s.verificarEmailInterno)
	r.POST("/registro", s.registrarCliente)
	r.POST("/login", s.login)
	r.GET("/me", s.requiereAuth(), s.me)
	r.GET("/verify", s.verificarSesion)
	r.GET("/verify-admin", s.requiereRol("administrador", "superadministrador"), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	r.POST("/usuarios", s.requiereRol("administrador", "superadministrador"), s.crearUsuario)
	r.GET("/usuarios", s.requiereRol("superadministrador"), s.listarUsuarios)
	r.PUT("/usuarios/:id/rol", s.requiereRol("superadministrador"), s.cambiarRolUsuario)
	r.GET("/usuarios/profesionales", s.requiereRol("administrador", "superadministrador"), s.listarCuentasProfesionales)
	return r
}

type cambiarRolRequest struct {
	Rol string `json:"rol" binding:"required"`
}

func (s *authServer) listarUsuarios(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	cur, err := s.users.Find(ctx, bson.M{}, options.Find().SetProjection(bson.M{"_id": 0, "id": 1, "nombre": 1, "email": 1, "roles": 1, "activo": 1, "creadoEn": 1, "emailVerificado": 1}).SetSort(bson.D{{Key: "creadoEn", Value: -1}, {Key: "email", Value: 1}}))
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "USUARIOS_NO_DISPONIBLES", "message": "No se pudieron consultar las cuentas."}})
		return
	}
	defer cur.Close(ctx)
	usuarios := make([]Usuario, 0)
	for cur.Next(ctx) {
		var usuario Usuario
		if cur.Decode(&usuario) != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "USUARIOS_NO_DISPONIBLES", "message": "No se pudieron leer las cuentas."}})
			return
		}
		usuarios = append(usuarios, usuario)
	}
	if cur.Err() != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "USUARIOS_NO_DISPONIBLES", "message": "No se pudieron leer las cuentas."}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"usuarios": usuarios})
}

func (s *authServer) cambiarRolUsuario(c *gin.Context) {
	var req cambiarRolRequest
	if err := c.ShouldBindJSON(&req); err != nil || !rolesValidos[req.Rol] {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "ROL_INVALIDO", "message": "Elegí un rol válido."}})
		return
	}
	cl, ok := c.Get("claims")
	actual, valid := cl.(*claims)
	if !ok || !valid {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "TOKEN_INVALIDO", "message": "Iniciá sesión para continuar."}})
		return
	}
	id := strings.TrimSpace(c.Param("id"))
	ctx, cancel := context.WithTimeout(c.Request.Context(), 8*time.Second)
	defer cancel()
	var objetivo Usuario
	if err := s.users.FindOne(ctx, bson.M{"id": id}).Decode(&objetivo); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"code": "USUARIO_NO_ENCONTRADO", "message": "No se encontró la cuenta."}})
		} else {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "USUARIOS_NO_DISPONIBLES", "message": "No se pudo consultar la cuenta."}})
		}
		return
	}
	if objetivo.ID == actual.Subject && req.Rol != "superadministrador" {
		c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code": "CAMBIO_PROPIO_NO_PERMITIDO", "message": "No podés quitarte el rol de superadministrador desde esta sesión."}})
		return
	}
	if objetivo.Activo && contieneRol(objetivo.Roles, "profesional") && req.Rol != "profesional" {
		linked, err := s.cuentaTienePerfilVinculado(ctx, id)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "CATALOGO_NO_DISPONIBLE", "message": "No se pudo verificar la vinculación con el catálogo. Intentá nuevamente."}})
			return
		}
		if linked {
			c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code": "PROFESIONAL_VINCULADO", "message": "Desvinculá primero esta cuenta de su perfil profesional en Catálogo."}})
			return
		}
	}
	control := s.roleControl
	if control == nil {
		control = s.users.Database().Collection("control_roles")
	}
	session, err := s.client.StartSession()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "ROLES_NO_DISPONIBLES", "message": "No se pudo actualizar el rol."}})
		return
	}
	defer session.EndSession(ctx)
	_, err = session.WithTransaction(ctx, func(tx context.Context) (any, error) {
		// Este documento serializa los cambios de rol y evita dos bajas simultáneas del último superadministrador.
		if _, e := control.UpdateOne(tx, bson.M{"_id": "role_management"}, bson.M{"$inc": bson.M{"version": 1}}, options.UpdateOne().SetUpsert(true)); e != nil {
			return nil, e
		}
		var current Usuario
		if e := s.users.FindOne(tx, bson.M{"id": id}).Decode(&current); e != nil {
			return nil, e
		}
		if current.ID == actual.Subject && req.Rol != "superadministrador" {
			return nil, errors.New("CAMBIO_PROPIO_NO_PERMITIDO")
		}
		if e := validarConservacionSuperadmin(tx, s.users, current, req.Rol); e != nil {
			return nil, e
		}
		_, e := s.users.UpdateOne(tx, bson.M{"id": id}, bson.M{"$set": bson.M{"roles": []string{req.Rol}}})
		return nil, e
	})
	if err != nil {
		switch err.Error() {
		case "CAMBIO_PROPIO_NO_PERMITIDO":
			c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code": err.Error(), "message": "No podés quitarte el rol de superadministrador desde esta sesión."}})
		case "ULTIMO_SUPERADMINISTRADOR":
			c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code": err.Error(), "message": "Debe quedar al menos un superadministrador activo."}})
		default:
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "ROLES_NO_DISPONIBLES", "message": "No se pudo actualizar el rol."}})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"usuario": gin.H{"id": objetivo.ID, "email": objetivo.Email, "rol": req.Rol}})
}

func validarConservacionSuperadmin(ctx context.Context, users *mongo.Collection, usuario Usuario, nuevoRol string) error {
	if !usuario.Activo || !contieneRol(usuario.Roles, "superadministrador") || nuevoRol == "superadministrador" {
		return nil
	}
	count, err := users.CountDocuments(ctx, bson.M{"activo": true, "roles": "superadministrador"})
	if err != nil {
		return err
	}
	if count <= 1 {
		return errors.New("ULTIMO_SUPERADMINISTRADOR")
	}
	return nil
}

func contieneRol(roles []string, role string) bool {
	for _, actual := range roles {
		if actual == role {
			return true
		}
	}
	return false
}

func (s *authServer) cuentaTienePerfilVinculado(ctx context.Context, id string) (bool, error) {
	if s.catalogoURL == "" || s.claveInterna == "" {
		return false, errors.New("catalogo interno no configurado")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.catalogoURL+"/internal/profesionales/por-cuenta/"+id, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("X-Melt-Internal-Key", s.claveInterna)
	resp, err := (&http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return true, nil
	}
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	return false, errors.New("respuesta interna inesperada de catálogo")
}

func (s *authServer) listarCuentasProfesionales(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 4*time.Second)
	defer cancel()
	cur, err := s.users.Find(ctx, bson.M{"activo": true, "roles": "profesional"}, options.Find().SetProjection(bson.M{"_id": 0, "id": 1, "nombre": 1, "email": 1}).SetSort(bson.D{{Key: "nombre", Value: 1}, {Key: "id", Value: 1}}))
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "USUARIOS_NO_DISPONIBLES", "message": "No se pudieron consultar las cuentas profesionales."}})
		return
	}
	defer cur.Close(ctx)
	cuentas := make([]gin.H, 0)
	for cur.Next(ctx) {
		var usuario struct {
			ID     string `bson:"id" json:"id"`
			Nombre string `bson:"nombre" json:"nombre"`
			Email  string `bson:"email" json:"email"`
		}
		if err := cur.Decode(&usuario); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "USUARIOS_NO_DISPONIBLES", "message": "No se pudieron leer las cuentas profesionales."}})
			return
		}
		cuentas = append(cuentas, gin.H{"id": usuario.ID, "nombre": usuario.Nombre, "email": usuario.Email})
	}
	if err := cur.Err(); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "USUARIOS_NO_DISPONIBLES", "message": "No se pudieron leer las cuentas profesionales."}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"usuarios": cuentas})
}

func (s *authServer) registrarCliente(c *gin.Context) {
	var req registroRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "DATOS_INVALIDOS", "message": "Completá nombre, email válido y contraseña de al menos 8 caracteres."}})
		return
	}
	if !passwordValida(req.Password) {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "PASSWORD_INVALIDA", "message": "La contraseña debe tener entre 8 y 72 bytes."}})
		return
	}
	solicitud := ""
	if req.InvitacionToken != "" {
		var e error
		solicitud, e = s.validarInvitacion(c.Request.Context(), req.InvitacionToken, req.Email)
		if e != nil {
			responderErrorInvitacion(c, e)
			return
		}
	}
	usuario, err := s.nuevoUsuario(c, req.Nombre, req.Email, req.Password, []string{"cliente"}, solicitud)
	if err != nil {
		return
	}
	respuesta := gin.H{"usuario": usuario}
	if solicitud != "" {
		estado := "pendiente_reintento"
		if s.entregarAcreditacion(c.Request.Context(), usuario) == nil {
			estado = "acreditada"
		}
		respuesta["acreditacion"] = gin.H{"solicitudId": solicitud, "estado": estado}
	}
	c.JSON(http.StatusCreated, respuesta)
}

func (s *authServer) crearUsuario(c *gin.Context) {
	var req crearUsuarioRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "DATOS_INVALIDOS", "message": "Completá nombre, email, contraseña y al menos un rol válido."}})
		return
	}
	if !passwordValida(req.Password) {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "PASSWORD_INVALIDA", "message": "La contraseña debe tener entre 8 y 72 bytes."}})
		return
	}
	for _, rol := range req.Roles {
		if !rolesValidos[rol] {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "ROL_INVALIDO", "message": "Uno o más roles no son válidos."}})
			return
		}
	}
	cl, ok := c.Get("claims")
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "TOKEN_INVALIDO", "message": "Iniciá sesión para continuar."}})
		return
	}
	claimsActuales, ok := cl.(*claims)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "TOKEN_INVALIDO", "message": "Iniciá sesión para continuar."}})
		return
	}
	esSuperadmin := false
	for _, rol := range claimsActuales.Roles {
		if rol == "superadministrador" {
			esSuperadmin = true
		}
	}
	if !esSuperadmin {
		for _, rol := range req.Roles {
			if rol == "administrador" || rol == "superadministrador" {
				c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "PERMISOS_INSUFICIENTES", "message": "Solo un superadministrador puede asignar roles administrativos."}})
				return
			}
		}
	}
	usuario, err := s.nuevoUsuario(c, req.Nombre, req.Email, req.Password, req.Roles)
	if err != nil {
		return
	}
	c.JSON(http.StatusCreated, gin.H{"usuario": usuario})
}

func passwordValida(password string) bool {
	return len([]byte(password)) >= 8 && len([]byte(password)) <= 72
}

func (s *authServer) nuevoUsuario(c *gin.Context, nombre, email, password string, roles []string, invitacion ...string) (*Usuario, error) {
	nombre = strings.TrimSpace(nombre)
	email = strings.ToLower(strings.TrimSpace(email))
	if nombre == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "DATOS_INVALIDOS", "message": "El nombre no puede estar vacío."}})
		return nil, errors.New("nombre vacío")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "ERROR_INTERNO", "message": "No se pudo crear la cuenta."}})
		return nil, err
	}
	usuario := &Usuario{ID: bson.NewObjectID().Hex(), Nombre: nombre, Email: email, PasswordHash: string(hash), Roles: roles, Activo: true, CreadoEn: time.Now().UTC()}
	if len(invitacion) > 0 && invitacion[0] != "" {
		usuario.AcreditacionPendiente = invitacion[0]
		usuario.EmailVerificado = true
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	if _, err := s.users.InsertOne(ctx, usuario); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code": "EMAIL_EXISTENTE", "message": "Ya existe una cuenta con ese email."}})
			return nil, err
		}
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "USUARIOS_NO_DISPONIBLE", "message": "No se pudo guardar la cuenta."}})
		return nil, err
	}
	return usuario, nil
}

func (s *authServer) login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "DATOS_INVALIDOS", "message": "Ingresá un email válido y la contraseña."}})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	var usuario Usuario
	err := s.users.FindOne(ctx, bson.D{{Key: "email", Value: strings.ToLower(strings.TrimSpace(req.Email))}}).Decode(&usuario)
	if !passwordValida(req.Password) || err != nil || bcrypt.CompareHashAndPassword([]byte(usuario.PasswordHash), []byte(req.Password)) != nil || !usuario.Activo {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "CREDENCIALES_INVALIDAS", "message": "Email o contraseña incorrectos."}})
		return
	}
	token, err := s.emitirToken(usuario)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "ERROR_INTERNO", "message": "No se pudo iniciar sesión."}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": token, "tipo": "Bearer", "expiraEnSegundos": int(s.tokenTTL.Seconds()), "usuario": usuario})
}

func (s *authServer) emitirToken(u Usuario) (string, error) {
	header, _ := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	now := time.Now()
	payload, err := json.Marshal(claims{Subject: u.ID, Email: u.Email, Roles: u.Roles, Issued: now.Unix(), Expires: now.Add(s.tokenTTL).Unix()})
	if err != nil {
		return "", err
	}
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, s.jwtSecret)
	_, _ = mac.Write([]byte(unsigned))
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (s *authServer) validarToken(token string) (*claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("token inválido")
	}
	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, errors.New("header inválido")
	}
	var header map[string]string
	if err := json.Unmarshal(headerJSON, &header); err != nil || header["alg"] != "HS256" || header["typ"] != "JWT" {
		return nil, errors.New("algoritmo de token inválido")
	}
	unsigned := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, s.jwtSecret)
	_, _ = mac.Write([]byte(unsigned))
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(signature, mac.Sum(nil)) {
		return nil, errors.New("firma inválida")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}
	var cl claims
	if err := json.Unmarshal(payload, &cl); err != nil {
		return nil, err
	}
	if cl.Subject == "" || cl.Expires <= time.Now().Unix() {
		return nil, errors.New("token vencido")
	}
	return &cl, nil
}

func (s *authServer) requiereAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := strings.TrimSpace(c.GetHeader("Authorization"))
		if !strings.HasPrefix(header, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "TOKEN_REQUERIDO", "message": "Iniciá sesión para continuar."}})
			return
		}
		cl, err := s.validarToken(strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "TOKEN_INVALIDO", "message": "La sesión venció o el token no es válido."}})
			return
		}
		c.Set("claims", cl)
		c.Next()
	}
}

func (s *authServer) requiereRol(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := strings.TrimSpace(c.GetHeader("Authorization"))
		if !strings.HasPrefix(header, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "TOKEN_REQUERIDO", "message": "Iniciá sesión para continuar."}})
			return
		}
		cl, err := s.validarToken(strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "TOKEN_INVALIDO", "message": "La sesión venció o el token no es válido."}})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
		defer cancel()
		var usuario Usuario
		if err := s.users.FindOne(ctx, bson.D{{Key: "id", Value: cl.Subject}}).Decode(&usuario); err != nil || !usuario.Activo {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "CUENTA_NO_DISPONIBLE", "message": "La cuenta no está disponible."}})
			return
		}
		cl.Roles = usuario.Roles
		cl.Email = usuario.Email
		for _, actual := range usuario.Roles {
			for _, permitido := range roles {
				if actual == permitido {
					c.Set("claims", cl)
					c.Next()
					return
				}
			}
		}
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "PERMISOS_INSUFICIENTES", "message": "No tenés permisos para realizar esta acción."}})
	}
}

func (s *authServer) verificarSesion(c *gin.Context) {
	header := strings.TrimSpace(c.GetHeader("Authorization"))
	if !strings.HasPrefix(header, "Bearer ") {
		c.Status(http.StatusUnauthorized)
		return
	}
	cl, err := s.validarToken(strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")))
	if err != nil {
		c.Status(http.StatusUnauthorized)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()
	var usuario Usuario
	if err := s.users.FindOne(ctx, bson.D{{Key: "id", Value: cl.Subject}}).Decode(&usuario); err != nil || !usuario.Activo {
		c.Status(http.StatusUnauthorized)
		return
	}
	// NGINX usa estos headers de la subsolicitud interna para propagar
	// identidad verificada. El gateway sobrescribe cualquier valor enviado
	// por el cliente antes de reenviar la petición al servicio de negocio.
	c.Header("X-Auth-User-ID", usuario.ID)
	c.Header("X-Auth-User-Roles", strings.Join(usuario.Roles, ","))
	c.Header("X-Auth-User-Email", usuario.Email)
	c.Status(http.StatusNoContent)
}

func (s *authServer) me(c *gin.Context) {
	cl, ok := c.Get("claims")
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "token inválido"})
		return
	}
	tokenClaims, ok := cl.(*claims)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "token inválido"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	var usuario Usuario
	if err := s.users.FindOne(ctx, bson.D{{Key: "id", Value: tokenClaims.Subject}}).Decode(&usuario); err != nil || !usuario.Activo {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "CUENTA_NO_DISPONIBLE", "message": "La cuenta no está disponible."}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"usuario": usuario})
}

func crearAdminInicial(ctx context.Context, users *mongo.Collection) error {
	email := strings.ToLower(strings.TrimSpace(os.Getenv("AUTH_ADMIN_EMAIL")))
	password := os.Getenv("AUTH_ADMIN_PASSWORD")
	if email == "" && password == "" {
		return nil
	}
	parsedEmail, emailErr := mail.ParseAddress(email)
	if email == "" || emailErr != nil || parsedEmail.Address != email || len(password) < 12 || len([]byte(password)) > 72 {
		return errors.New("configurá un email válido y una contraseña inicial de entre 12 y 72 bytes")
	}
	var existente Usuario
	err := users.FindOne(ctx, bson.D{{Key: "email", Value: email}}).Decode(&existente)
	if err == nil {
		return nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	admin := Usuario{
		ID: bson.NewObjectID().Hex(), Nombre: "Administrador inicial", Email: email,
		PasswordHash: string(hash), Roles: []string{"superadministrador"},
		Activo: true, CreadoEn: time.Now().UTC(),
	}
	_, err = users.InsertOne(ctx, admin)
	if mongo.IsDuplicateKeyError(err) {
		return nil
	}
	return err
}

// Solo consulta existencia: no publica datos personales ni roles.
func (s *authServer) usuarioInterno(c *gin.Context) {
	if s.claveInterna == "" {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	if subtle.ConstantTimeCompare([]byte(c.GetHeader("X-Melt-Internal-Key")), []byte(s.claveInterna)) != 1 {
		c.Status(http.StatusUnauthorized)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()
	var usuario Usuario
	err := s.users.FindOne(ctx, bson.M{"id": c.Param("id"), "activo": true}, options.FindOne().SetProjection(bson.M{"_id": 0, "id": 1, "roles": 1, "nombre": 1})).Decode(&usuario)
	if errors.Is(err, mongo.ErrNoDocuments) {
		c.Status(http.StatusNotFound)
		return
	}
	if err != nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	c.Header("X-Auth-User-ID", usuario.ID)
	c.Header("X-Auth-User-Roles", strings.Join(usuario.Roles, ","))
	c.Header("X-Auth-User-Name", usuario.Nombre)
	c.Status(http.StatusNoContent)
}
