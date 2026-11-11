package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readconcern"
	"go.mongodb.org/mongo-driver/v2/mongo/writeconcern"
)

// Limita los enteros al rango exacto interoperable con clientes JavaScript.
const maxPuntos int64 = 9007199254740991

var errConflicto = errors.New("solicitud reutilizada con otros datos")
var errLimite = errors.New("saldo fuera del rango admitido")
var errUsuario = errors.New("usuario inexistente o inactivo")
var errSaldoInsuficiente = errors.New("saldo insuficiente")

type AcreditarRequest struct {
	SolicitudID string `json:"solicitudId"`
	UsuarioID   string `json:"usuarioId"`
	Puntos      int64  `json:"puntos"`
}
type Movimiento struct {
	SolicitudID  string    `bson:"_id" json:"solicitudId"`
	UsuarioID    string    `bson:"usuarioId" json:"usuarioId"`
	Puntos       int64     `bson:"puntos" json:"puntos"`
	SaldoPuntos  int64     `bson:"saldoPuntos" json:"saldoPuntos"`
	Tipo         string    `bson:"tipo" json:"tipo"`
	CreadoEn     time.Time `bson:"creadoEn" json:"creadoEn"`
	EmailDestino string    `bson:"emailDestino,omitempty" json:"-"`
	Descripcion  string    `bson:"descripcion,omitempty" json:"descripcion,omitempty"`
	Estado       string    `bson:"estado,omitempty" json:"estado,omitempty"`
}
type Cuenta struct {
	UsuarioID   string `bson:"usuarioId"`
	SaldoPuntos int64  `bson:"saldoPuntos"`
	// Campos históricos preservados para mostrar el movimiento original.
	PuntosRegalo int64     `bson:"puntosRegalo"`
	CreadoEn     time.Time `bson:"creadoEn"`
}
type almacenPuntos interface {
	buscarSolicitud(context.Context, string) (Movimiento, error)
	acreditar(context.Context, AcreditarRequest) (Movimiento, bool, error)
	consultar(context.Context, string) (int64, []Movimiento, error)
}
type Servidor struct {
	cliente          *mongo.Client
	almacen          almacenPuntos
	programa         programaStore
	rabbitURL        string
	claveAPI         string
	verificarUsuario func(context.Context, string) error
	verificarSesion  func(context.Context, string) (identidadCliente, error)
	resolverEmail    func(context.Context, string) (string, error)
	claveInterna     string
	invitacionSecret []byte
	publicURL        string
	verificarEmail   func(context.Context, string, string) error
}
type almacenMongo struct {
	cliente                                           *mongo.Client
	registros, movimientos                            *mongo.Collection
	recompensas, reglasTratamientos, canjes, fallidos *mongo.Collection
}

func main() {
	uri := strings.TrimSpace(os.Getenv("MONGODB_URI"))
	if uri == "" {
		uri = "mongodb://localhost:27017"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cliente, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		log.Fatal(err)
	}
	defer cliente.Disconnect(context.Background())
	if err = cliente.Ping(ctx, nil); err != nil {
		log.Fatal("no se pudo conectar con MongoDB")
	}
	dbName := strings.TrimSpace(os.Getenv("BENEFICIOS_MONGODB_DATABASE"))
	if dbName == "" {
		dbName = "beneficios_db"
	}
	a := nuevoAlmacen(cliente, dbName)
	if err = a.preparar(ctx); err != nil {
		log.Fatalf("no se pudo preparar Beneficios: %v", err)
	}
	authURL := strings.TrimRight(strings.TrimSpace(os.Getenv("AUTH_URL")), "/")
	claveInterna := strings.TrimSpace(os.Getenv("MELT_INTERNAL_KEY"))
	rabbitURL := strings.TrimSpace(os.Getenv("RABBITMQ_URL"))
	s := &Servidor{cliente: cliente, almacen: a, programa: a, rabbitURL: rabbitURL, claveAPI: strings.TrimSpace(os.Getenv("BENEFICIOS_PARTNER_KEY")), verificarUsuario: verificadorAuth(authURL, claveInterna), verificarSesion: verificadorSesionAuth(authURL), resolverEmail: resolverEmailAuth(authURL, claveInterna), verificarEmail: verificadorEmailAuth(authURL, claveInterna), claveInterna: claveInterna, invitacionSecret: []byte(os.Getenv("BENEFICIOS_INVITATION_SECRET")), publicURL: strings.TrimRight(strings.TrimSpace(os.Getenv("MELT_PUBLIC_URL")), "/")}
	if rabbitURL != "" {
		go (&consumidorPrograma{url: rabbitURL, almacen: a}).run(context.Background())
	}
	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "8083"
	}
	if err := crearRouter(s).Run(":" + port); err != nil {
		log.Fatal(err)
	}
}
func nuevoAlmacen(c *mongo.Client, db string) *almacenMongo {
	database := c.Database(db)
	return &almacenMongo{cliente: c, registros: database.Collection("registros_externos"), movimientos: database.Collection("movimientos_puntos"), recompensas: database.Collection("recompensas"), reglasTratamientos: database.Collection("reglas_puntos_tratamientos"), canjes: database.Collection("canjes"), fallidos: database.Collection("mensajes_fallidos")}
}
func (a *almacenMongo) preparar(ctx context.Context) error {
	// El índice email era una regla de regalo único, no una regla del saldo.
	// Se retira sin borrar documentos ni saldos preexistentes.
	indices, err := a.registros.Indexes().List(ctx)
	if err != nil {
		return err
	}
	var encontrados []bson.M
	if err = indices.All(ctx, &encontrados); err != nil {
		return err
	}
	for _, indice := range encontrados {
		if indice["name"] == "uq_registro_externo_email" {
			if err := a.registros.Indexes().DropOne(ctx, "uq_registro_externo_email"); err != nil {
				return err
			}
		}
	}
	if _, err := a.registros.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "usuarioId", Value: 1}}, Options: options.Index().SetUnique(true).SetSparse(true).SetName("uq_registro_gym_usuario")}); err != nil {
		return err
	}
	if _, err = a.movimientos.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "tokenHash", Value: 1}}, Options: options.Index().SetUnique(true).SetSparse(true)}); err != nil {
		return err
	}
	_, err = a.movimientos.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "usuarioId", Value: 1}, {Key: "creadoEn", Value: 1}}})
	if err != nil {
		return err
	}
	if _, err = a.canjes.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "solicitudId", Value: 1}}, Options: options.Index().SetUnique(true).SetName("uq_canje_solicitud")}); err != nil {
		return err
	}
	if _, err = a.canjes.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "usuarioId", Value: 1}, {Key: "estado", Value: 1}, {Key: "creadoEn", Value: -1}}}); err != nil {
		return err
	}
	if _, err = a.fallidos.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "estado", Value: 1}, {Key: "creadoEn", Value: -1}}}); err != nil {
		return err
	}
	return nil
}
func crearRouter(s *Servidor) *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())
	r.GET("/health", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if s == nil || s.cliente == nil || s.cliente.Ping(ctx, nil) != nil {
			c.JSON(503, gin.H{"estado": "degradado"})
			return
		}
		c.JSON(200, gin.H{"servicio": "beneficios", "estado": "ok"})
	})
	r.POST("/acreditar-puntos", s.acreditarPuntos)
	r.POST("/acreditaciones", s.acreditarPuntos)
	r.POST("/invitaciones/consultar", s.consultarInvitacion)
	r.POST("/invitaciones/confirmar", s.requiereCliente(), s.confirmarInvitacionPublica)
	r.POST("/internal/invitaciones/verificar", s.verificarInvitacionInterna)
	r.POST("/internal/invitaciones/confirmar", s.confirmarInvitacionInterna)
	r.GET("/mis-puntos", s.requiereCliente(), s.misPuntos)
	s.agregarRutasPrograma(r)
	return r
}
func errorJSON(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}
func (s *Servidor) acreditarPuntos(c *gin.Context) {
	if s == nil || s.claveAPI == "" {
		errorJSON(c, 503, "BENEFICIOS_NO_DISPONIBLE", "La acreditación no está configurada.")
		return
	}
	if subtle.ConstantTimeCompare([]byte(c.GetHeader("X-Partner-Key")), []byte(s.claveAPI)) != 1 {
		errorJSON(c, 401, "PARTNER_NO_AUTORIZADO", "Consumidor no autorizado.")
		return
	}
	mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || mediaType != "application/json" {
		errorJSON(c, 415, "CONTENT_TYPE_INVALIDO", "Usá Content-Type: application/json.")
		return
	}
	req, err := decodificarAcreditacion(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
	if err != nil || req.SolicitudID != strings.TrimSpace(req.SolicitudID) || req.UsuarioID != strings.TrimSpace(req.UsuarioID) || utf8.RuneCountInString(req.SolicitudID) == 0 || utf8.RuneCountInString(req.SolicitudID) > 128 || (req.Email == "" && (utf8.RuneCountInString(req.UsuarioID) == 0 || utf8.RuneCountInString(req.UsuarioID) > 128)) || utf8.RuneCountInString(req.Descripcion) > 500 || req.Puntos <= 0 || req.Puntos > maxPuntos {
		errorJSON(c, 400, "DATOS_INVALIDOS", "Enviá solicitudId o referencia, usuarioId o email, puntos enteros positivos y descripcion opcional de hasta 500 caracteres.")
		return
	}
	if req.Email != "" {
		s.acreditarPorEmail(c, req)
		return
	}
	if s.almacen == nil || s.verificarUsuario == nil {
		errorJSON(c, 503, "BENEFICIOS_NO_DISPONIBLE", "Servicio no disponible.")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 4*time.Second)
	defer cancel()
	// Resolver antes de consultar Auth permite recuperar una confirmación aun
	// si Auth está caído, y garantiza conflicto por ID global con cualquier payload.
	previo, err := s.almacen.buscarSolicitud(ctx, req.SolicitudID)
	if err == nil {
		movimiento, _, e := resolverDetalle(previo, req.AcreditarRequest, "", req.Descripcion)
		if e != nil {
			errorJSON(c, 409, "SOLICITUD_EN_CONFLICTO", "solicitudId ya se utilizó con otros datos.")
			return
		}
		c.JSON(http.StatusOK, movimiento)
		return
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		errorJSON(c, 503, "BENEFICIOS_NO_DISPONIBLE", "No se pudo consultar la solicitud; reintentá con el mismo solicitudId.")
		return
	}
	if err := s.verificarUsuario(ctx, req.UsuarioID); err != nil {
		if errors.Is(err, errUsuario) {
			errorJSON(c, 404, "USUARIO_NO_EXISTE", "Usuario inexistente o inactivo.")
		} else {
			errorJSON(c, 503, "AUTH_NO_DISPONIBLE", "No se pudo verificar el usuario.")
		}
		return
	}
	movimiento, repetida, err := acreditarEntrada(ctx, s.almacen, req.AcreditarRequest, "", req.Descripcion)
	switch {
	case errors.Is(err, errConflicto):
		errorJSON(c, 409, "SOLICITUD_EN_CONFLICTO", "solicitudId ya se utilizó con otros datos.")
	case errors.Is(err, errLimite):
		errorJSON(c, 422, "SALDO_FUERA_DE_RANGO", "La operación supera el saldo máximo admitido.")
	case err != nil:
		errorJSON(c, 503, "BENEFICIOS_NO_DISPONIBLE", "No se pudo confirmar la operación; reintentá con el mismo solicitudId.")
	default:
		status := http.StatusCreated
		if repetida {
			status = http.StatusOK
		}
		c.JSON(status, movimiento)
	}
}
func (s *Servidor) misPuntos(c *gin.Context) {
	if s == nil || s.almacen == nil {
		errorJSON(c, 503, "BENEFICIOS_NO_DISPONIBLE", "No se pudo consultar el saldo.")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 4*time.Second)
	defer cancel()
	saldo, movimientos, err := s.almacen.consultar(ctx, c.GetString("authUserID"))
	if err != nil {
		errorJSON(c, 503, "BENEFICIOS_NO_DISPONIBLE", "No se pudo consultar el saldo.")
		return
	}
	c.JSON(200, gin.H{"usuarioId": c.GetString("authUserID"), "saldoPuntos": saldo, "movimientos": movimientos})
}
func verificadorAuth(base, clave string) func(context.Context, string) error {
	client := &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return func(ctx context.Context, id string) error {
		if base == "" || clave == "" {
			return errors.New("Auth no configurado")
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/internal/usuarios/"+url.PathEscape(id), nil)
		if err != nil {
			return err
		}
		req.Header.Set("X-Melt-Internal-Key", clave)
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode == 404 {
			return errUsuario
		}
		if resp.StatusCode != 204 {
			return errors.New("Auth no disponible")
		}
		return nil
	}
}
func resolverRepetida(m Movimiento, req AcreditarRequest) (Movimiento, bool, error) {
	return resolverDetalle(m, req, "", "")
}
func (a *almacenMongo) acreditar(ctx context.Context, req AcreditarRequest) (Movimiento, bool, error) {
	return a.acreditarDetalle(ctx, req, "", "")
}
func (a *almacenMongo) acreditarDetalle(ctx context.Context, req AcreditarRequest, email, descripcion string) (Movimiento, bool, error) {
	// El _id de movimientos es la clave global de idempotencia. Ante una carrera
	// de inserción se reconsulta luego del abort; jamás se incrementa fuera del commit.
	for intento := 0; intento < 4; intento++ {
		var previo Movimiento
		err := a.movimientos.FindOne(ctx, bson.M{"_id": req.SolicitudID}).Decode(&previo)
		if err == nil {
			if email != "" && previo.UsuarioID == "" && previo.EmailDestino == email && previo.Puntos == req.Puntos && previo.Descripcion == descripcion {
				return Movimiento{}, false, errOperacionPendiente
			}
			return resolverDetalle(previo, req, email, descripcion)
		}
		if !errors.Is(err, mongo.ErrNoDocuments) {
			return Movimiento{}, false, err
		}
		session, err := a.cliente.StartSession()
		if err != nil {
			return Movimiento{}, false, err
		}
		var resultado Movimiento
		_, err = session.WithTransaction(ctx, func(tx context.Context) (any, error) {
			resultado = Movimiento{SolicitudID: req.SolicitudID, UsuarioID: req.UsuarioID, Puntos: req.Puntos, Tipo: "acreditacion_externa", CreadoEn: time.Now().UTC().Truncate(time.Millisecond), EmailDestino: email, Descripcion: descripcion, Estado: "acreditada"}
			// Reservar primero la solicitud; el registro y el incremento se revierten juntos.
			if _, e := a.movimientos.InsertOne(tx, resultado); e != nil {
				return nil, e
			}
			_, e := a.registros.UpdateOne(tx, bson.M{"usuarioId": req.UsuarioID}, bson.M{"$setOnInsert": bson.M{"id": req.UsuarioID, "saldoPuntos": int64(0), "creadoEn": resultado.CreadoEn}}, options.UpdateOne().SetUpsert(true))
			if e != nil {
				return nil, e
			}
			var cuenta Cuenta
			e = a.registros.FindOneAndUpdate(tx, bson.M{"usuarioId": req.UsuarioID, "saldoPuntos": bson.M{"$lte": maxPuntos - req.Puntos}}, bson.M{"$inc": bson.M{"saldoPuntos": req.Puntos}}, options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&cuenta)
			if errors.Is(e, mongo.ErrNoDocuments) {
				return nil, errLimite
			}
			if e != nil {
				return nil, e
			}
			resultado.SaldoPuntos = cuenta.SaldoPuntos
			_, e = a.movimientos.UpdateOne(tx, bson.M{"_id": req.SolicitudID}, bson.M{"$set": bson.M{"saldoPuntos": cuenta.SaldoPuntos}})
			return nil, e
		}, options.Transaction().SetReadConcern(readconcern.Snapshot()).SetWriteConcern(writeconcern.Majority()))
		session.EndSession(ctx)
		if err == nil {
			return resultado, false, nil
		}
		if !mongo.IsDuplicateKeyError(err) {
			return Movimiento{}, false, err
		}
	}
	return Movimiento{}, false, errors.New("contención; reintentar solicitud")
}
func (a *almacenMongo) consultar(ctx context.Context, id string) (int64, []Movimiento, error) {
	session, err := a.cliente.StartSession()
	if err != nil {
		return 0, nil, err
	}
	defer session.EndSession(ctx)
	var cuenta Cuenta
	movimientos := []Movimiento{}
	_, err = session.WithTransaction(ctx, func(tx context.Context) (any, error) {
		cuenta = Cuenta{}
		movimientos = []Movimiento{}
		e := a.registros.FindOne(tx, bson.M{"usuarioId": id}).Decode(&cuenta)
		if e != nil && !errors.Is(e, mongo.ErrNoDocuments) {
			return nil, e
		}
		cursor, e := a.movimientos.Find(tx, bson.M{"usuarioId": id}, options.Find().SetSort(bson.D{{Key: "creadoEn", Value: 1}, {Key: "_id", Value: 1}}))
		if e != nil {
			return nil, e
		}
		if e = cursor.All(tx, &movimientos); e != nil {
			return nil, e
		}
		if cuenta.PuntosRegalo > 0 {
			movimientos = append([]Movimiento{{SolicitudID: "historico:" + id, UsuarioID: id, Puntos: cuenta.PuntosRegalo, Tipo: "acreditacion_historica", CreadoEn: cuenta.CreadoEn}}, movimientos...)
		}
		return nil, nil
	}, options.Transaction().SetReadConcern(readconcern.Snapshot()).SetWriteConcern(writeconcern.Majority()))
	return cuenta.SaldoPuntos, movimientos, err
}

func (a *almacenMongo) buscarSolicitud(ctx context.Context, id string) (Movimiento, error) {
	var m Movimiento
	err := a.movimientos.FindOne(ctx, bson.M{"_id": id}).Decode(&m)
	return m, err
}
