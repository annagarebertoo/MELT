package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readconcern"
	"go.mongodb.org/mongo-driver/v2/mongo/writeconcern"
)

var errOperacionPendiente = errors.New("operación pendiente")
var errInvitacion = errors.New("invitación inválida")
var errInvitacionVencida = errors.New("invitación vencida")
var errDestinatario = errors.New("destinatario distinto")
var errEmailSinVerificar = errors.New("email sin verificar")
var errCuentaInactiva = errors.New("cuenta inactiva")

// Pendientes y movimientos comparten colección y _id: la referencia es global,
// incluso entre solicitudes por ID y solicitudes por email.
type operacionPuntos struct {
	Movimiento `bson:",inline"`
	Nonce      string    `bson:"nonce,omitempty"`
	TokenHash  string    `bson:"tokenHash,omitempty"`
	VenceEn    time.Time `bson:"venceEn,omitempty"`
}
type almacenInvitaciones interface {
	obtenerOperacion(context.Context, string) (operacionPuntos, error)
	guardarPendiente(context.Context, operacionPuntos) error
	renovarPendiente(context.Context, operacionPuntos, operacionPuntos) (operacionPuntos, error)
	obtenerPorToken(context.Context, string) (operacionPuntos, error)
	confirmarPendiente(context.Context, string, string, string) (Movimiento, bool, error)
	acreditarDetalle(context.Context, AcreditarRequest, string, string) (Movimiento, bool, error)
}

func resolverDetalle(m Movimiento, req AcreditarRequest, email, descripcion string) (Movimiento, bool, error) {
	if m.UsuarioID != req.UsuarioID || m.Puntos != req.Puntos || m.EmailDestino != email || m.Descripcion != descripcion {
		return Movimiento{}, false, errConflicto
	}
	return m, true, nil
}
func acreditarEntrada(ctx context.Context, a almacenPuntos, r AcreditarRequest, email, descripcion string) (Movimiento, bool, error) {
	if d, ok := a.(interface {
		acreditarDetalle(context.Context, AcreditarRequest, string, string) (Movimiento, bool, error)
	}); ok {
		return d.acreditarDetalle(ctx, r, email, descripcion)
	}
	if email != "" || descripcion != "" {
		return Movimiento{}, false, errors.New("almacén no disponible")
	}
	return a.acreditar(ctx, r)
}
func normalizarEmail(email string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	for _, r := range email {
		if r > 127 {
			return "", errors.New("email debe ser ASCII")
		}
	}
	v, err := mail.ParseAddress(email)
	if err != nil || v.Address != email || len(email) > 254 {
		return "", errDestinatario
	}
	return email, nil
}
func (s *Servidor) acreditarPorEmail(c *gin.Context, r entradaAcreditacion) {
	email, err := normalizarEmail(r.Email)
	if err != nil {
		errorJSON(c, 400, "DATOS_INVALIDOS", "Enviá un email válido.")
		return
	}
	r.Email = email
	a, ok := s.almacen.(almacenInvitaciones)
	if !ok || s.resolverEmail == nil {
		errorJSON(c, 503, "BENEFICIOS_NO_DISPONIBLE", "Flujo por email no disponible.")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	// La consulta de idempotencia precede a Auth y no cambia el destinatario.
	op, err := a.obtenerOperacion(ctx, r.SolicitudID)
	if err == nil {
		s.responderOperacionEmail(c, ctx, a, op, r)
		return
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		errorInvitacionJSON(c, err)
		return
	}
	id, err := s.resolverEmail(ctx, email)
	if err == nil {
		m, dup, e := a.acreditarDetalle(ctx, AcreditarRequest{r.SolicitudID, id, r.Puntos}, email, r.Descripcion)
		if errors.Is(e, errOperacionPendiente) {
			op, e = a.obtenerOperacion(ctx, r.SolicitudID)
			if e == nil {
				s.responderOperacionEmail(c, ctx, a, op, r)
				return
			}
		}
		if e != nil {
			errorInvitacionJSON(c, e)
			return
		}
		status := 201
		if dup {
			status = 200
		}
		c.JSON(status, m)
		return
	}
	if !errors.Is(err, errUsuario) && !errors.Is(err, errEmailSinVerificar) {
		if errors.Is(err, errCuentaInactiva) {
			errorJSON(c, 409, "CUENTA_NO_DISPONIBLE", "La cuenta destinataria está inactiva.")
		} else {
			errorJSON(c, 503, "AUTH_NO_DISPONIBLE", "No se pudo verificar el destinatario.")
		}
		return
	}
	if !s.invitacionesConfiguradas() {
		errorJSON(c, 503, "INVITACIONES_NO_CONFIGURADAS", "No se pudo crear el enlace de registro.")
		return
	}
	op = operacionPuntos{Movimiento: Movimiento{SolicitudID: r.SolicitudID, EmailDestino: email, Puntos: r.Puntos, Descripcion: r.Descripcion, Estado: "pendiente", Tipo: "acreditacion_externa", CreadoEn: time.Now().UTC().Truncate(time.Millisecond)}}
	if err = s.prepararToken(&op); err != nil {
		errorInvitacionJSON(c, err)
		return
	}
	if err = a.guardarPendiente(ctx, op); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			op, err = a.obtenerOperacion(ctx, r.SolicitudID)
			if err == nil {
				s.responderOperacionEmail(c, ctx, a, op, r)
				return
			}
		}
		errorInvitacionJSON(c, err)
		return
	}
	s.responderOperacionEmail(c, ctx, a, op, r)
}
func (s *Servidor) invitacionesConfiguradas() bool {
	u, e := url.Parse(s.publicURL)
	return len(s.invitacionSecret) >= 32 && e == nil && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && (u.Scheme == "https" || (u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1")))
}
func (s *Servidor) tokenPara(op operacionPuntos) string {
	payload, _ := json.Marshal([]string{op.SolicitudID, op.EmailDestino, strconv.FormatInt(op.Puntos, 10), op.Nonce})
	mac := hmac.New(sha256.New, s.invitacionSecret)
	mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
func (s *Servidor) prepararToken(op *operacionPuntos) error {
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	op.Nonce = base64.RawURLEncoding.EncodeToString(nonce)
	op.VenceEn = time.Now().UTC().Add(7 * 24 * time.Hour).Truncate(time.Millisecond)
	op.TokenHash = hashToken(s.tokenPara(*op))
	return nil
}
func (s *Servidor) responderOperacionEmail(c *gin.Context, ctx context.Context, a almacenInvitaciones, op operacionPuntos, r entradaAcreditacion) {
	if op.EmailDestino != r.Email || op.Puntos != r.Puntos || op.Descripcion != r.Descripcion {
		errorInvitacionJSON(c, errConflicto)
		return
	}
	if op.Estado == "acreditada" || op.UsuarioID != "" {
		c.JSON(200, op.Movimiento)
		return
	}
	if op.Estado != "pendiente" || !s.invitacionesConfiguradas() {
		errorJSON(c, 503, "INVITACIONES_NO_CONFIGURADAS", "No se pudo recuperar el enlace de registro.")
		return
	}
	if !time.Now().Before(op.VenceEn) {
		nuevo := op
		if e := s.prepararToken(&nuevo); e != nil {
			errorInvitacionJSON(c, e)
			return
		}
		var e error
		op, e = a.renovarPendiente(ctx, op, nuevo)
		if e != nil {
			errorInvitacionJSON(c, e)
			return
		}
		if op.Estado == "acreditada" {
			c.JSON(200, op.Movimiento)
			return
		}
	}
	token := s.tokenPara(op)
	if subtle.ConstantTimeCompare([]byte(hashToken(token)), []byte(op.TokenHash)) != 1 {
		errorJSON(c, 503, "INVITACIONES_NO_CONFIGURADAS", "La clave de invitaciones no coincide con la configuración persistida.")
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(202, gin.H{"solicitudId": op.SolicitudID, "estado": "pendiente", "puntos": op.Puntos, "puntosAcreditados": 0, "registroUrl": s.publicURL + "/#invitacion=" + url.QueryEscape(token), "venceEn": op.VenceEn})
}
func leerJSON(c *gin.Context, v any) error {
	media, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || media != "application/json" {
		return errors.New("Content-Type inválido")
	}
	d := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
	d.DisallowUnknownFields()
	if err = d.Decode(v); err != nil {
		return err
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("JSON adicional")
	}
	return nil
}
func (s *Servidor) operacionToken(ctx context.Context, token string) (operacionPuntos, error) {
	a, ok := s.almacen.(almacenInvitaciones)
	if !ok {
		return operacionPuntos{}, errors.New("almacén no disponible")
	}
	raw, e := base64.RawURLEncoding.DecodeString(token)
	if e != nil || len(raw) != 32 {
		return operacionPuntos{}, errInvitacion
	}
	op, e := a.obtenerPorToken(ctx, hashToken(token))
	if errors.Is(e, mongo.ErrNoDocuments) {
		return op, errInvitacion
	}
	if e != nil {
		return op, e
	}
	if op.Estado == "pendiente" && !time.Now().Before(op.VenceEn) {
		return op, errInvitacionVencida
	}
	return op, nil
}
func (s *Servidor) consultarInvitacion(c *gin.Context) {
	var req struct {
		Token string `json:"token"`
	}
	if leerJSON(c, &req) != nil {
		errorJSON(c, 400, "DATOS_INVALIDOS", "Enviá el token de invitación.")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()
	op, err := s.operacionToken(ctx, req.Token)
	if err != nil {
		errorInvitacionJSON(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(200, gin.H{"email": op.EmailDestino, "puntos": op.Puntos, "estado": op.Estado, "venceEn": op.VenceEn})
}
func (s *Servidor) autorizaInterno(c *gin.Context) bool {
	if s.claveInterna == "" {
		errorJSON(c, 503, "INTERNAL_NO_CONFIGURADO", "Integración interna no disponible.")
		return false
	}
	if subtle.ConstantTimeCompare([]byte(c.GetHeader("X-Melt-Internal-Key")), []byte(s.claveInterna)) != 1 {
		errorJSON(c, 401, "INTERNAL_NO_AUTORIZADO", "Servicio no autorizado.")
		return false
	}
	return true
}
func (s *Servidor) verificarInvitacionInterna(c *gin.Context) {
	if !s.autorizaInterno(c) {
		return
	}
	var req struct {
		Token string `json:"token"`
		Email string `json:"email"`
	}
	if leerJSON(c, &req) != nil {
		errorJSON(c, 400, "DATOS_INVALIDOS", "Solicitud inválida.")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()
	op, e := s.operacionToken(ctx, req.Token)
	if e != nil {
		errorInvitacionJSON(c, e)
		return
	}
	email, e := normalizarEmail(req.Email)
	if e != nil || op.EmailDestino != email {
		errorInvitacionJSON(c, errDestinatario)
		return
	}
	if op.Estado != "pendiente" {
		errorJSON(c, 409, "INVITACION_UTILIZADA", "El enlace ya fue utilizado.")
		return
	}
	// El registro solo acepta invitaciones pendientes. Los reintentos de una
	// entrega ya validada usan el endpoint interno de confirmación.
	c.Header("Cache-Control", "no-store")
	c.JSON(200, gin.H{"solicitudId": op.SolicitudID})
}
func (s *Servidor) confirmarInvitacionInterna(c *gin.Context) {
	if !s.autorizaInterno(c) {
		return
	}
	var req struct {
		SolicitudID string `json:"solicitudId"`
		UsuarioID   string `json:"usuarioId"`
		Email       string `json:"email"`
	}
	if leerJSON(c, &req) != nil || req.SolicitudID == "" || req.UsuarioID == "" {
		errorJSON(c, 400, "DATOS_INVALIDOS", "Solicitud inválida.")
		return
	}
	a, ok := s.almacen.(almacenInvitaciones)
	if !ok || s.resolverEmail == nil {
		errorInvitacionJSON(c, errors.New("servicio no disponible"))
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 4*time.Second)
	defer cancel()
	email, e := normalizarEmail(req.Email)
	if e != nil {
		errorInvitacionJSON(c, e)
		return
	}
	id, e := s.resolverEmail(ctx, email)
	if e != nil && !errors.Is(e, errEmailSinVerificar) {
		errorInvitacionJSON(c, e)
		return
	}
	if id != req.UsuarioID {
		errorInvitacionJSON(c, errDestinatario)
		return
	}
	// Auth ya validó el enlace antes de crear y persistir la cuenta. Su outbox
	// puede completar una entrega tardía aun si el enlace vence durante la caída.
	m, dup, e := a.confirmarPendiente(ctx, req.SolicitudID, id, email)
	if e != nil {
		errorInvitacionJSON(c, e)
		return
	}
	status := 201
	if dup {
		status = 200
	}
	c.JSON(status, m)
}
func (s *Servidor) confirmarInvitacionPublica(c *gin.Context) {
	var req struct {
		Token string `json:"token"`
	}
	if leerJSON(c, &req) != nil {
		errorJSON(c, 400, "DATOS_INVALIDOS", "Enviá el token de invitación.")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 4*time.Second)
	defer cancel()
	op, e := s.operacionToken(ctx, req.Token)
	if e != nil {
		errorInvitacionJSON(c, e)
		return
	}
	email := strings.ToLower(strings.TrimSpace(c.GetString("authUserEmail")))
	if email != op.EmailDestino {
		errorInvitacionJSON(c, errDestinatario)
		return
	}
	if op.UsuarioID != "" && op.UsuarioID != c.GetString("authUserID") {
		errorInvitacionJSON(c, errDestinatario)
		return
	}
	if s.verificarEmail == nil {
		errorInvitacionJSON(c, errors.New("Auth no configurado"))
		return
	}
	if e = s.verificarEmail(ctx, c.GetString("authUserID"), email); e != nil {
		errorInvitacionJSON(c, e)
		return
	}
	a, ok := s.almacen.(almacenInvitaciones)
	if !ok {
		errorInvitacionJSON(c, errors.New("almacén no disponible"))
		return
	}
	m, dup, e := a.confirmarPendiente(ctx, op.SolicitudID, c.GetString("authUserID"), email)
	if e != nil {
		errorInvitacionJSON(c, e)
		return
	}
	status := 201
	if dup {
		status = 200
	}
	c.JSON(status, m)
}
func errorInvitacionJSON(c *gin.Context, e error) {
	switch {
	case errors.Is(e, errConflicto):
		errorJSON(c, 409, "SOLICITUD_EN_CONFLICTO", "La referencia ya se utilizó con otros datos.")
	case errors.Is(e, errInvitacion):
		errorJSON(c, 404, "INVITACION_INVALIDA", "El enlace no es válido.")
	case errors.Is(e, errInvitacionVencida):
		errorJSON(c, 410, "INVITACION_VENCIDA", "Solicitá un nuevo enlace al emisor; tus puntos continúan pendientes.")
	case errors.Is(e, errDestinatario):
		errorJSON(c, 403, "DESTINATARIO_INCORRECTO", "La invitación corresponde a otro email o cuenta.")
	case errors.Is(e, errLimite):
		errorJSON(c, 422, "SALDO_FUERA_DE_RANGO", "La operación supera el saldo máximo.")
	default:
		errorJSON(c, 503, "BENEFICIOS_NO_DISPONIBLE", "No se pudo confirmar la operación. Reintentá sin cambiar su referencia.")
	}
}
func resolverEmailAuth(base, clave string) func(context.Context, string) (string, error) {
	client := &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return func(ctx context.Context, email string) (string, error) {
		if base == "" || clave == "" {
			return "", errors.New("Auth no configurado")
		}
		body, _ := json.Marshal(map[string]string{"email": email})
		q, e := http.NewRequestWithContext(ctx, "POST", base+"/internal/usuarios/buscar", strings.NewReader(string(body)))
		if e != nil {
			return "", e
		}
		q.Header.Set("Content-Type", "application/json")
		q.Header.Set("X-Melt-Internal-Key", clave)
		resp, e := client.Do(q)
		if e != nil {
			return "", e
		}
		defer resp.Body.Close()
		if resp.StatusCode == 404 {
			return "", errUsuario
		}
		if resp.StatusCode == 409 {
			return "", errCuentaInactiva
		}
		if resp.StatusCode != 200 {
			return "", errors.New("Auth no disponible")
		}
		var u struct {
			ID              string `json:"id"`
			EmailVerificado bool   `json:"emailVerificado"`
		}
		if e = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&u); e != nil || u.ID == "" {
			return "", errors.New("respuesta inválida de Auth")
		}
		if !u.EmailVerificado {
			return u.ID, errEmailSinVerificar
		}
		return u.ID, nil
	}
}
func (a *almacenMongo) obtenerOperacion(ctx context.Context, id string) (operacionPuntos, error) {
	var o operacionPuntos
	e := a.movimientos.FindOne(ctx, bson.M{"_id": id}).Decode(&o)
	return o, e
}
func (a *almacenMongo) guardarPendiente(ctx context.Context, o operacionPuntos) error {
	_, e := a.movimientos.InsertOne(ctx, o)
	return e
}
func (a *almacenMongo) obtenerPorToken(ctx context.Context, hash string) (operacionPuntos, error) {
	var o operacionPuntos
	e := a.movimientos.FindOne(ctx, bson.M{"tokenHash": hash}).Decode(&o)
	return o, e
}
func (a *almacenMongo) renovarPendiente(ctx context.Context, old, next operacionPuntos) (operacionPuntos, error) {
	var o operacionPuntos
	e := a.movimientos.FindOneAndUpdate(ctx, bson.M{"_id": old.SolicitudID, "estado": "pendiente", "tokenHash": old.TokenHash}, bson.M{"$set": bson.M{"nonce": next.Nonce, "tokenHash": next.TokenHash, "venceEn": next.VenceEn}}, options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&o)
	if errors.Is(e, mongo.ErrNoDocuments) {
		return a.obtenerOperacion(ctx, old.SolicitudID)
	}
	return o, e
}
func (a *almacenMongo) confirmarPendiente(ctx context.Context, id, user, email string) (Movimiento, bool, error) {
	session, e := a.cliente.StartSession()
	if e != nil {
		return Movimiento{}, false, e
	}
	defer session.EndSession(ctx)
	var result Movimiento
	duplicate := false
	_, e = session.WithTransaction(ctx, func(tx context.Context) (any, error) {
		duplicate = false
		o, e := a.obtenerOperacion(tx, id)
		if e != nil {
			return nil, e
		}
		if o.EmailDestino != email {
			return nil, errDestinatario
		}
		if o.UsuarioID != "" {
			if o.UsuarioID != user {
				return nil, errDestinatario
			}
			result = o.Movimiento
			duplicate = true
			return nil, nil
		}
		if o.Estado != "pendiente" {
			return nil, errInvitacion
		}
		_, e = a.registros.UpdateOne(tx, bson.M{"usuarioId": user}, bson.M{"$setOnInsert": bson.M{"id": user, "saldoPuntos": int64(0), "creadoEn": time.Now().UTC()}}, options.UpdateOne().SetUpsert(true))
		if e != nil {
			return nil, e
		}
		var cuenta Cuenta
		e = a.registros.FindOneAndUpdate(tx, bson.M{"usuarioId": user, "saldoPuntos": bson.M{"$lte": maxPuntos - o.Puntos}}, bson.M{"$inc": bson.M{"saldoPuntos": o.Puntos}}, options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&cuenta)
		if errors.Is(e, mongo.ErrNoDocuments) {
			return nil, errLimite
		}
		if e != nil {
			return nil, e
		}
		result = o.Movimiento
		result.UsuarioID = user
		result.SaldoPuntos = cuenta.SaldoPuntos
		result.Estado = "acreditada"
		result.CreadoEn = time.Now().UTC().Truncate(time.Millisecond)
		_, e = a.movimientos.UpdateOne(tx, bson.M{"_id": id, "estado": "pendiente"}, bson.M{"$set": bson.M{"usuarioId": user, "saldoPuntos": cuenta.SaldoPuntos, "estado": "acreditada", "creadoEn": result.CreadoEn}})
		return nil, e
	}, options.Transaction().SetReadConcern(readconcern.Snapshot()).SetWriteConcern(writeconcern.Majority()))
	return result, duplicate, e
}

func verificadorEmailAuth(base, clave string) func(context.Context, string, string) error {
	client := &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return func(ctx context.Context, id, email string) error {
		if base == "" || clave == "" {
			return errors.New("Auth no configurado")
		}
		b, _ := json.Marshal(map[string]string{"usuarioId": id, "email": email})
		q, e := http.NewRequestWithContext(ctx, "POST", base+"/internal/usuarios/verificar-email", strings.NewReader(string(b)))
		if e != nil {
			return e
		}
		q.Header.Set("Content-Type", "application/json")
		q.Header.Set("X-Melt-Internal-Key", clave)
		r, e := client.Do(q)
		if e != nil {
			return e
		}
		defer r.Body.Close()
		if r.StatusCode != 204 {
			return errors.New("no se pudo verificar el email")
		}
		return nil
	}
}
