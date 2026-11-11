package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type falloInvitacion struct {
	status        int
	code, message string
}

func (e *falloInvitacion) Error() string { return e.code }
func responderErrorInvitacion(c *gin.Context, e error) {
	var f *falloInvitacion
	if errors.As(e, &f) {
		c.JSON(f.status, gin.H{"error": gin.H{"code": f.code, "message": f.message}})
		return
	}
	c.JSON(503, gin.H{"error": gin.H{"code": "BENEFICIOS_NO_DISPONIBLE", "message": "No se pudo verificar la invitación. Reintentá sin cambiar el enlace."}})
}
func emailInvitacion(v string) (string, error) {
	v = strings.ToLower(strings.TrimSpace(v))
	for _, r := range v {
		if r > 127 {
			return "", errors.New("email debe ser ASCII")
		}
	}
	a, e := mail.ParseAddress(v)
	if e != nil || a.Address != v || len(v) > 254 {
		return "", errors.New("email inválido")
	}
	return v, nil
}
func (s *authServer) llamarBeneficios(ctx context.Context, path string, payload any) (int, []byte, error) {
	if s.beneficiosURL == "" || s.claveInterna == "" {
		return 0, nil, errors.New("Beneficios no configurado")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	body, e := json.Marshal(payload)
	if e != nil {
		return 0, nil, e
	}
	q, e := http.NewRequestWithContext(ctx, "POST", s.beneficiosURL+path, strings.NewReader(string(body)))
	if e != nil {
		return 0, nil, e
	}
	q.Header.Set("Content-Type", "application/json")
	q.Header.Set("X-Melt-Internal-Key", s.claveInterna)
	client := &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	r, e := client.Do(q)
	if e != nil {
		return 0, nil, e
	}
	defer r.Body.Close()
	b, e := io.ReadAll(io.LimitReader(r.Body, 8192))
	return r.StatusCode, b, e
}
func (s *authServer) validarInvitacion(ctx context.Context, token, email string) (string, error) {
	if len(token) != 43 {
		return "", &falloInvitacion{400, "INVITACION_INVALIDA", "El enlace no es válido."}
	}
	email, e := emailInvitacion(email)
	if e != nil {
		return "", &falloInvitacion{400, "DATOS_INVALIDOS", "Enviá un email válido."}
	}
	status, b, e := s.llamarBeneficios(ctx, "/internal/invitaciones/verificar", map[string]string{"token": token, "email": email})
	if e != nil {
		return "", e
	}
	switch status {
	case 200:
		var v struct {
			SolicitudID string `json:"solicitudId"`
		}
		if json.Unmarshal(b, &v) != nil || v.SolicitudID == "" {
			return "", errors.New("respuesta inválida")
		}
		return v.SolicitudID, nil
	case 403:
		return "", &falloInvitacion{403, "DESTINATARIO_INCORRECTO", "Registrate con el email al que corresponde la invitación."}
	case 404:
		return "", &falloInvitacion{400, "INVITACION_INVALIDA", "El enlace no es válido."}
	case 409:
		return "", &falloInvitacion{409, "INVITACION_UTILIZADA", "El enlace ya fue utilizado. Iniciá sesión con tu cuenta."}
	case 410:
		return "", &falloInvitacion{410, "INVITACION_VENCIDA", "Solicitá un nuevo enlace al emisor; tus puntos continúan pendientes."}
	default:
		return "", errors.New("no se pudo verificar la invitación")
	}
}

// El ID validado se inserta junto con la cuenta. No se persiste el token.
// Si el receptor o la respuesta fallan, el worker recupera esta entrega durable.
func (s *authServer) entregarAcreditacion(ctx context.Context, u *Usuario) (deliveryErr error) {
	if u == nil || u.AcreditacionPendiente == "" {
		return nil
	}
	defer func() {
		if deliveryErr != nil {
			retryCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_, _ = s.users.UpdateOne(retryCtx, bson.M{"id": u.ID, "acreditacionPendiente": u.AcreditacionPendiente}, bson.M{"$set": bson.M{"acreditacionIntentoEn": time.Now().UTC()}})
		}
	}()
	status, _, e := s.llamarBeneficios(ctx, "/internal/invitaciones/confirmar", map[string]string{"solicitudId": u.AcreditacionPendiente, "usuarioId": u.ID, "email": u.Email})
	if e != nil {
		return e
	}
	if status != 200 && status != 201 {
		return errors.New("acreditación aún pendiente")
	}
	_, e = s.users.UpdateOne(ctx, bson.M{"id": u.ID, "acreditacionPendiente": u.AcreditacionPendiente}, bson.M{"$unset": bson.M{"acreditacionPendiente": "", "acreditacionIntentoEn": ""}})
	return e
}
func (s *authServer) procesarAcreditaciones(ctx context.Context) {
	if s.users == nil || s.beneficiosURL == "" || s.claveInterna == "" {
		return
	}
	batchCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	cursor, e := s.users.Find(batchCtx, bson.M{"acreditacionPendiente": bson.M{"$exists": true, "$ne": ""}, "activo": true}, options.Find().SetSort(bson.D{{Key: "acreditacionIntentoEn", Value: 1}, {Key: "creadoEn", Value: 1}}).SetLimit(20).SetProjection(bson.M{"id": 1, "email": 1, "acreditacionPendiente": 1}))
	if e != nil {
		cancel()
		return
	}
	var usuarios []Usuario
	e = cursor.All(batchCtx, &usuarios)
	cancel()
	if e != nil {
		return
	}
	for i := range usuarios {
		if ctx.Err() != nil {
			return
		}
		deliveryCtx, stop := context.WithTimeout(ctx, 4*time.Second)
		_ = s.entregarAcreditacion(deliveryCtx, &usuarios[i])
		stop()
	}
}
func (s *authServer) recuperarAcreditaciones(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		s.procesarAcreditaciones(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (s *authServer) requiereClaveInterna(c *gin.Context) bool {
	if s.claveInterna == "" {
		c.Status(503)
		return false
	}
	if subtle.ConstantTimeCompare([]byte(c.GetHeader("X-Melt-Internal-Key")), []byte(s.claveInterna)) != 1 {
		c.Status(401)
		return false
	}
	return true
}
func (s *authServer) buscarUsuarioInterno(c *gin.Context) {
	if !s.requiereClaveInterna(c) {
		return
	}
	var req struct {
		Email string `json:"email"`
	}
	d := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
	if d.Decode(&req) != nil {
		c.Status(400)
		return
	}
	email, e := emailInvitacion(req.Email)
	if e != nil {
		c.Status(400)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()
	var u Usuario
	e = s.users.FindOne(ctx, bson.M{"email": email}, options.FindOne().SetProjection(bson.M{"id": 1, "emailVerificado": 1, "activo": 1})).Decode(&u)
	if errors.Is(e, mongo.ErrNoDocuments) {
		c.Status(404)
		return
	}
	if e != nil {
		c.Status(503)
		return
	}
	if !u.Activo {
		c.Status(409)
		return
	}
	c.JSON(200, gin.H{"id": u.ID, "emailVerificado": u.EmailVerificado})
}

// Solo Beneficios, después de validar un enlace y una sesión del mismo email,
// puede confirmar esta prueba de identidad. El frontend no controla el flag.
func (s *authServer) verificarEmailInterno(c *gin.Context) {
	if !s.requiereClaveInterna(c) {
		return
	}
	var req struct {
		UsuarioID string `json:"usuarioId"`
		Email     string `json:"email"`
	}
	if json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 4096)).Decode(&req) != nil || req.UsuarioID == "" {
		c.Status(400)
		return
	}
	email, e := emailInvitacion(req.Email)
	if e != nil {
		c.Status(400)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()
	result, e := s.users.UpdateOne(ctx, bson.M{"id": req.UsuarioID, "email": email, "activo": true}, bson.M{"$set": bson.M{"emailVerificado": true}})
	if e != nil {
		c.Status(503)
		return
	}
	if result.MatchedCount == 0 {
		c.Status(404)
		return
	}
	c.Status(204)
}
