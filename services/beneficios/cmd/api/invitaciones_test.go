package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func almacenInvitacionesPrueba(t *testing.T) *almacenMongo {
	t.Helper()
	uri := os.Getenv("BENEFICIOS_TEST_MONGODB_URI")
	if uri == "" {
		t.Skip("definí BENEFICIOS_TEST_MONGODB_URI con replica set de pruebas")
	}
	c, e := mongo.Connect(options.Client().ApplyURI(uri))
	if e != nil {
		t.Fatal(e)
	}
	db := fmt.Sprintf("melt_inv_test_%d", time.Now().UnixNano())
	t.Cleanup(func() { _ = c.Database(db).Drop(context.Background()); _ = c.Disconnect(context.Background()) })
	a := nuevoAlmacen(c, db)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if e = a.preparar(ctx); e != nil {
		t.Fatal(e)
	}
	return a
}
func servidorInvitacionesPrueba(a *almacenMongo) *Servidor {
	s := servidorPrueba(a)
	s.publicURL = "http://localhost:8080"
	s.invitacionSecret = []byte("secret-invitaciones-de-prueba-al-menos-32-caracteres")
	s.claveInterna = "internal-test"
	s.resolverEmail = func(_ context.Context, email string) (string, error) {
		if email == "existente@example.com" {
			return "existente", nil
		}
		if email == "no-verificado@example.com" {
			return "no-verificado", errEmailSinVerificar
		}
		return "", errUsuario
	}
	s.verificarEmail = func(context.Context, string, string) error { return nil }
	return s
}
func jsonCall(r http.Handler, path, body, partner, bearer, internal string) *httptest.ResponseRecorder {
	q := httptest.NewRequest("POST", path, strings.NewReader(body))
	q.Header.Set("Content-Type", "application/json")
	q.Header.Set("X-Partner-Key", partner)
	q.Header.Set("Authorization", bearer)
	q.Header.Set("X-Melt-Internal-Key", internal)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, q)
	return w
}
func invitationToken(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	if w.Code != 202 {
		t.Fatalf("pendiente: %d %s", w.Code, w.Body.String())
	}
	var v struct {
		URL    string `json:"registroUrl"`
		Estado string `json:"estado"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &v); e != nil {
		t.Fatal(e)
	}
	u, e := url.Parse(v.URL)
	if e != nil {
		t.Fatal(e)
	}
	q, e := url.ParseQuery(u.Fragment)
	if e != nil {
		t.Fatal(e)
	}
	token := q.Get("invitacion")
	if len(token) != 43 || v.Estado != "pendiente" || u.RawQuery != "" {
		t.Fatal("enlace inválido")
	}
	return token
}
func TestInvitacionesMongoFlujoYSeguridad(t *testing.T) {
	a := almacenInvitacionesPrueba(t)
	s := servidorInvitacionesPrueba(a)
	r := crearRouter(s)
	ctx := context.Background()
	body := `{"referencia":"pendiente-001","email":" Nuevo@Example.com ","puntos":500,"descripcion":"Beneficio externo"}`
	first := jsonCall(r, "/acreditaciones", body, "test", "", "")
	token := invitationToken(t, first)
	again := jsonCall(r, "/acreditar-puntos", body, "test", "", "")
	if again.Code != 202 || again.Body.String() != first.Body.String() {
		t.Fatal("no recuperó el mismo enlace", again.Code)
	}
	if saldo, movs, e := a.consultar(ctx, "u"); e != nil || saldo != 0 || len(movs) != 0 {
		t.Fatal("acreditó antes de crear/vincular cuenta", saldo, len(movs), e)
	}
	var raw bson.M
	if e := a.movimientos.FindOne(ctx, bson.M{"_id": "pendiente-001"}).Decode(&raw); e != nil {
		t.Fatal(e)
	}
	encoded, _ := json.Marshal(raw)
	if strings.Contains(string(encoded), token) {
		t.Fatal("token en claro persistido")
	}
	for _, b := range []string{strings.Replace(body, "500", "501", 1), strings.Replace(body, "Nuevo@Example.com", "otro@example.com", 1), strings.Replace(body, "Beneficio externo", "otro motivo", 1), `{"solicitudId":"pendiente-001","usuarioId":"u","puntos":500}`} {
		if w := jsonCall(r, "/acreditar-puntos", b, "test", "", ""); w.Code != 409 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	verify := fmt.Sprintf(`{"token":%q,"email":"otro@example.com"}`, token)
	if w := jsonCall(r, "/internal/invitaciones/verificar", verify, "", "", "internal-test"); w.Code != 403 {
		t.Fatal(w.Code)
	}
	if w := jsonCall(r, "/internal/invitaciones/verificar", verify, "", "", ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	verify = fmt.Sprintf(`{"token":%q,"email":"nuevo@example.com"}`, token)
	if w := jsonCall(r, "/internal/invitaciones/verificar", verify, "", "", "internal-test"); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	tokenBody := fmt.Sprintf(`{"token":%q}`, token)
	if w := jsonCall(r, "/invitaciones/confirmar", tokenBody, "", "", ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	s.verificarSesion = func(context.Context, string) (identidadCliente, error) {
		return identidadCliente{ID: "wrong", Email: "otro@example.com", Roles: []string{"cliente"}}, nil
	}
	if w := jsonCall(r, "/invitaciones/confirmar", tokenBody, "", "Bearer real", ""); w.Code != 403 {
		t.Fatal(w.Code)
	}
	s.verificarSesion = func(context.Context, string) (identidadCliente, error) {
		return identidadCliente{ID: "u", Email: "nuevo@example.com", Roles: []string{"cliente"}}, nil
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := jsonCall(r, "/invitaciones/confirmar", tokenBody, "", "Bearer real", "")
			if w.Code != 200 && w.Code != 201 {
				t.Errorf("confirmación concurrente: %d %s", w.Code, w.Body.String())
			}
		}()
	}
	wg.Wait()
	saldo, movs, e := a.consultar(ctx, "u")
	if e != nil || saldo != 500 || len(movs) != 1 || movs[0].Descripcion != "Beneficio externo" {
		t.Fatal(saldo, len(movs), e)
	}
	if w := jsonCall(r, "/internal/invitaciones/verificar", verify, "", "", "internal-test"); w.Code != 409 {
		t.Fatal("permitió nuevo registro con enlace usado", w.Code)
	}
	if w := jsonCall(r, "/invitaciones/confirmar", tokenBody, "", "Bearer real", ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := jsonCall(r, "/acreditaciones", body, "test", "", ""); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	// Mismo email pero una cuenta distinta nunca recupera una invitación consumida.
	s.verificarSesion = func(context.Context, string) (identidadCliente, error) {
		return identidadCliente{ID: "otro-id", Email: "nuevo@example.com", Roles: []string{"cliente"}}, nil
	}
	if w := jsonCall(r, "/invitaciones/confirmar", tokenBody, "", "Bearer real", ""); w.Code != 403 {
		t.Fatal(w.Code)
	}
	// Persistencia desde otro cliente, no estado local del handler.
	c2, e := mongo.Connect(options.Client().ApplyURI(os.Getenv("BENEFICIOS_TEST_MONGODB_URI")))
	if e != nil {
		t.Fatal(e)
	}
	defer c2.Disconnect(ctx)
	op, e := nuevoAlmacen(c2, a.movimientos.Database().Name()).obtenerOperacion(ctx, "pendiente-001")
	if e != nil || op.UsuarioID != "u" || op.Estado != "acreditada" {
		t.Fatal(op, e)
	}
}
func TestInvitacionesMongoVencimientoRenovacionYRollback(t *testing.T) {
	a := almacenInvitacionesPrueba(t)
	s := servidorInvitacionesPrueba(a)
	r := crearRouter(s)
	ctx := context.Background()
	body := `{"solicitudId":"vence","email":"nuevo@example.com","puntos":100}`
	token := invitationToken(t, jsonCall(r, "/acreditar-puntos", body, "test", "", ""))
	_, e := a.movimientos.UpdateOne(ctx, bson.M{"_id": "vence"}, bson.M{"$set": bson.M{"venceEn": time.Now().Add(-time.Hour)}})
	if e != nil {
		t.Fatal(e)
	}
	if w := jsonCall(r, "/invitaciones/consultar", fmt.Sprintf(`{"token":%q}`, token), "", "", ""); w.Code != 410 {
		t.Fatal(w.Code)
	}
	renewed := invitationToken(t, jsonCall(r, "/acreditar-puntos", body, "test", "", ""))
	if token == renewed {
		t.Fatal("no renovó token vencido")
	}
	if _, e := s.operacionToken(ctx, token); !errors.Is(e, errInvitacion) {
		t.Fatal("token viejo sigue válido", e)
	}
	_, e = a.registros.InsertOne(ctx, bson.M{"usuarioId": "u", "saldoPuntos": maxPuntos})
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = a.confirmarPendiente(ctx, "vence", "u", "nuevo@example.com"); !errors.Is(e, errLimite) {
		t.Fatal(e)
	}
	op, e := a.obtenerOperacion(ctx, "vence")
	if e != nil || op.Estado != "pendiente" || op.UsuarioID != "" {
		t.Fatal("confirmación parcial", op, e)
	}
	if saldo, _, e := a.consultar(ctx, "u"); e != nil || saldo != maxPuntos {
		t.Fatal(saldo, e)
	}
	for _, amount := range []int{100, 500, 137} {
		b := fmt.Sprintf(`{"referencia":"cantidad-%d","email":"existente@example.com","puntos":%d,"descripcion":"prueba"}`, amount, amount)
		w := jsonCall(r, "/acreditaciones", b, "test", "", "")
		if w.Code != 201 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if saldo, movs, e := a.consultar(ctx, "existente"); e != nil || saldo != 737 || len(movs) != 3 {
		t.Fatal(saldo, len(movs), e)
	}
	// Una cuenta antigua sin email verificado recibe enlace, no crédito por coincidencia de email.
	b := `{"referencia":"legacy","email":"no-verificado@example.com","puntos":500}`
	invitationToken(t, jsonCall(r, "/acreditaciones", b, "test", "", ""))
	if saldo, _, e := a.consultar(ctx, "no-verificado"); e != nil || saldo != 0 {
		t.Fatal(saldo, e)
	}
}

func TestContratoAcreditacionExtendido(t *testing.T) {
	for _, body := range []string{
		`{"referencia":"op","email":"a@example.com","puntos":100,"descripcion":null}`,
		`{"referencia":"op","solicitudId":"op","email":"a@example.com","puntos":100}`,
		`{"referencia":"op","email":"a@example.com","usuarioId":"123","puntos":100}`,
		`{"referencia":"op","email":"a@example.com","PUNTOS":100}`,
	} {
		if _, err := decodificarAcreditacion(strings.NewReader(body)); err == nil {
			t.Errorf("aceptó %s", body)
		}
	}
	req, err := decodificarAcreditacion(strings.NewReader(`{"referencia":"op","email":"a@example.com","puntos":137,"descripcion":"Prueba"}`))
	if err != nil || req.SolicitudID != "op" || req.Puntos != 137 {
		t.Fatalf("contrato válido: %+v %v", req, err)
	}
}
