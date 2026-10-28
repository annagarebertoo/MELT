package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
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
	ID           string    `bson:"id" json:"id"`
	Nombre       string    `bson:"nombre" json:"nombre"`
	Email        string    `bson:"email" json:"email"`
	PasswordHash string    `bson:"passwordHash" json:"-"`
	Roles        []string  `bson:"roles" json:"roles"`
	Activo       bool      `bson:"activo" json:"activo"`
	CreadoEn     time.Time `bson:"creadoEn" json:"creadoEn"`
}

type registroRequest struct {
	Nombre   string `json:"nombre" binding:"required"`
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=8"`
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
	client    *mongo.Client
	users     *mongo.Collection
	jwtSecret []byte
	tokenTTL  time.Duration
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
	users := client.Database("usuarios_db").Collection("usuarios")
	_, err = users.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "email", Value: 1}},
		Options: options.Index().SetUnique(true).SetName("uq_usuarios_email"),
	})
	if err != nil {
		log.Fatal("no se pudo crear el índice de email")
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
	s := &authServer{client: client, users: users, jwtSecret: []byte(secret), tokenTTL: ttl}
	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" { port = "8084" }
	if err := s.router().Run(":" + port); err != nil { log.Fatal("no se pudo iniciar autenticación") }
}

func (s *authServer) router() *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())
	r.GET("/health", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if s == nil || s.client == nil || s.client.Ping(ctx, nil) != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"servicio":"auth","estado":"degradado"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"servicio":"auth","estado":"ok"})
	})
	r.POST("/registro", s.registrarCliente)
	r.POST("/login", s.login)
	r.GET("/me", s.requiereAuth(), s.me)
	r.GET("/verify", s.verificarSesion)
	r.POST("/usuarios", s.requiereRol("administrador", "superadministrador"), s.crearUsuario)
	return r
}

func (s *authServer) registrarCliente(c *gin.Context) {
	var req registroRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code":"DATOS_INVALIDOS","message":"Completá nombre, email válido y contraseña de al menos 8 caracteres."}})
		return
	}
	if !passwordValida(req.Password) {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code":"PASSWORD_INVALIDA","message":"La contraseña debe tener entre 8 y 72 bytes."}})
		return
	}
	usuario, err := s.nuevoUsuario(c, req.Nombre, req.Email, req.Password, []string{"cliente"})
	if err != nil { return }
	c.JSON(http.StatusCreated, gin.H{"usuario": usuario})
}

func (s *authServer) crearUsuario(c *gin.Context) {
	var req crearUsuarioRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code":"DATOS_INVALIDOS","message":"Completá nombre, email, contraseña y al menos un rol válido."}})
		return
	}
	if !passwordValida(req.Password) {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code":"PASSWORD_INVALIDA","message":"La contraseña debe tener entre 8 y 72 bytes."}})
		return
	}
	for _, rol := range req.Roles {
		if !rolesValidos[rol] {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code":"ROL_INVALIDO","message":"Uno o más roles no son válidos."}})
			return
		}
	}
	cl, ok := c.Get("claims")
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code":"TOKEN_INVALIDO","message":"Iniciá sesión para continuar."}})
		return
	}
	claimsActuales, ok := cl.(*claims)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code":"TOKEN_INVALIDO","message":"Iniciá sesión para continuar."}})
		return
	}
	esSuperadmin := false
	for _, rol := range claimsActuales.Roles {
		if rol == "superadministrador" { esSuperadmin = true }
	}
	if !esSuperadmin {
		for _, rol := range req.Roles {
			if rol == "administrador" || rol == "superadministrador" {
				c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"code":"PERMISOS_INSUFICIENTES","message":"Solo un superadministrador puede asignar roles administrativos."}})
				return
			}
		}
	}
	usuario, err := s.nuevoUsuario(c, req.Nombre, req.Email, req.Password, req.Roles)
	if err != nil { return }
	c.JSON(http.StatusCreated, gin.H{"usuario": usuario})
}

func passwordValida(password string) bool {
	return len([]byte(password)) >= 8 && len([]byte(password)) <= 72
}

func (s *authServer) nuevoUsuario(c *gin.Context, nombre, email, password string, roles []string) (*Usuario, error) {
	nombre = strings.TrimSpace(nombre)
	email = strings.ToLower(strings.TrimSpace(email))
	if nombre == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code":"DATOS_INVALIDOS","message":"El nombre no puede estar vacío."}})
		return nil, errors.New("nombre vacío")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code":"ERROR_INTERNO","message":"No se pudo crear la cuenta."}})
		return nil, err
	}
	usuario := &Usuario{ID: bson.NewObjectID().Hex(), Nombre: nombre, Email: email, PasswordHash: string(hash), Roles: roles, Activo: true, CreadoEn: time.Now().UTC()}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	if _, err := s.users.InsertOne(ctx, usuario); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code":"EMAIL_EXISTENTE","message":"Ya existe una cuenta con ese email."}})
			return nil, err
		}
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code":"USUARIOS_NO_DISPONIBLE","message":"No se pudo guardar la cuenta."}})
		return nil, err
	}
	return usuario, nil
}

func (s *authServer) login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code":"DATOS_INVALIDOS","message":"Ingresá un email válido y la contraseña."}})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	var usuario Usuario
	err := s.users.FindOne(ctx, bson.D{{Key:"email", Value:strings.ToLower(strings.TrimSpace(req.Email))}}).Decode(&usuario)
	if !passwordValida(req.Password) || err != nil || bcrypt.CompareHashAndPassword([]byte(usuario.PasswordHash), []byte(req.Password)) != nil || !usuario.Activo {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code":"CREDENCIALES_INVALIDAS","message":"Email o contraseña incorrectos."}})
		return
	}
	token, err := s.emitirToken(usuario)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code":"ERROR_INTERNO","message":"No se pudo iniciar sesión."}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"token":token,"tipo":"Bearer","expiraEnSegundos":int(s.tokenTTL.Seconds()),"usuario":usuario})
}

func (s *authServer) emitirToken(u Usuario) (string, error) {
	header, _ := json.Marshal(map[string]string{"alg":"HS256","typ":"JWT"})
	now := time.Now()
	payload, err := json.Marshal(claims{Subject:u.ID,Email:u.Email,Roles:u.Roles,Issued:now.Unix(),Expires:now.Add(s.tokenTTL).Unix()})
	if err != nil { return "", err }
	unsigned := base64.RawURLEncoding.EncodeToString(header)+"."+base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, s.jwtSecret)
	_, _ = mac.Write([]byte(unsigned))
	return unsigned+"."+base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (s *authServer) validarToken(token string) (*claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 { return nil, errors.New("token inválido") }
	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, errors.New("header inválido")
	}
	var header map[string]string
	if err := json.Unmarshal(headerJSON, &header); err != nil || header["alg"] != "HS256" || header["typ"] != "JWT" {
		return nil, errors.New("algoritmo de token inválido")
	}
	unsigned := parts[0]+"."+parts[1]
	mac := hmac.New(sha256.New, s.jwtSecret)
	_, _ = mac.Write([]byte(unsigned))
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(signature, mac.Sum(nil)) { return nil, errors.New("firma inválida") }
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil { return nil, err }
	var cl claims
	if err := json.Unmarshal(payload, &cl); err != nil { return nil, err }
	if cl.Subject == "" || cl.Expires <= time.Now().Unix() { return nil, errors.New("token vencido") }
	return &cl, nil
}

func (s *authServer) requiereAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := strings.TrimSpace(c.GetHeader("Authorization"))
		if !strings.HasPrefix(header, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error":gin.H{"code":"TOKEN_REQUERIDO","message":"Iniciá sesión para continuar."}})
			return
		}
		cl, err := s.validarToken(strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error":gin.H{"code":"TOKEN_INVALIDO","message":"La sesión venció o el token no es válido."}})
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
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error":gin.H{"code":"TOKEN_REQUERIDO","message":"Iniciá sesión para continuar."}})
			return
		}
		cl, err := s.validarToken(strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error":gin.H{"code":"TOKEN_INVALIDO","message":"La sesión venció o el token no es válido."}})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
		defer cancel()
		var usuario Usuario
		if err := s.users.FindOne(ctx, bson.D{{Key: "id", Value: cl.Subject}}).Decode(&usuario); err != nil || !usuario.Activo {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code":"CUENTA_NO_DISPONIBLE","message":"La cuenta no está disponible."}})
			return
		}
		cl.Roles = usuario.Roles
		cl.Email = usuario.Email
		for _, actual := range usuario.Roles {
			for _, permitido := range roles {
				if actual == permitido { c.Set("claims",cl); c.Next(); return }
			}
		}
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error":gin.H{"code":"PERMISOS_INSUFICIENTES","message":"No tenés permisos para realizar esta acción."}})
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
	c.Status(http.StatusNoContent)
}

func (s *authServer) me(c *gin.Context) {
	cl, ok := c.Get("claims")
	if !ok { c.JSON(http.StatusUnauthorized, gin.H{"error":"token inválido"}); return }
	tokenClaims, ok := cl.(*claims)
	if !ok { c.JSON(http.StatusUnauthorized, gin.H{"error":"token inválido"}); return }
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	var usuario Usuario
	if err := s.users.FindOne(ctx, bson.D{{Key:"id",Value:tokenClaims.Subject}}).Decode(&usuario); err != nil || !usuario.Activo {
		c.JSON(http.StatusUnauthorized, gin.H{"error":gin.H{"code":"CUENTA_NO_DISPONIBLE","message":"La cuenta no está disponible."}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"usuario":usuario})
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

