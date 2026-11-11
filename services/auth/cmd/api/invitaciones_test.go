package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func authInvitacionesPrueba(t *testing.T) *authServer {
	t.Helper()
	uri := os.Getenv("AUTH_TEST_MONGODB_URI")
	if uri == "" {
		t.Skip("definí AUTH_TEST_MONGODB_URI con MongoDB de pruebas")
	}
	c, e := mongo.Connect(options.Client().ApplyURI(uri))
	if e != nil {
		t.Fatal(e)
	}
	db := c.Database(fmt.Sprintf("melt_auth_inv_test_%d", time.Now().UnixNano()))
	users := db.Collection("usuarios")
	t.Cleanup(func() { _ = db.Drop(context.Background()); _ = c.Disconnect(context.Background()) })
	_, e = users.Indexes().CreateOne(context.Background(), mongo.IndexModel{Keys: bson.D{{Key: "email", Value: 1}}, Options: options.Index().SetUnique(true)})
	if e != nil {
		t.Fatal(e)
	}
	return &authServer{client: c, users: users, jwtSecret: []byte("secret-de-pruebas-de-al-menos-32-caracteres"), tokenTTL: time.Minute, claveInterna: "internal-test"}
}
func authJSON(r http.Handler, path string, body any, key string) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	q := httptest.NewRequest("POST", path, bytes.NewReader(b))
	q.Header.Set("Content-Type", "application/json")
	q.Header.Set("X-Melt-Internal-Key", key)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, q)
	return w
}
func TestRegistroInvitacionOutboxDurableYRecuperacion(t *testing.T) {
	s := authInvitacionesPrueba(t)
	var confirmado atomic.Bool
	var entregas atomic.Int32
	b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Melt-Internal-Key") != "internal-test" {
			t.Error("falta clave")
		}
		switch r.URL.Path {
		case "/internal/invitaciones/verificar":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"solicitudId":"op-001"}`))
		case "/internal/invitaciones/confirmar":
			entregas.Add(1)
			if !confirmado.Load() {
				w.WriteHeader(503)
				return
			}
			w.WriteHeader(201)
		default:
			w.WriteHeader(404)
		}
	}))
	defer b.Close()
	s.beneficiosURL = b.URL
	r := s.router()
	body := map[string]any{"nombre": "Persona Invitada", "email": "invitada@example.com", "password": "clave-segura-123", "invitacionToken": strings.Repeat("t", 43)}
	w := authJSON(r, "/registro", body, "")
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var response struct {
		Usuario      Usuario `json:"usuario"`
		Acreditacion struct {
			Estado string `json:"estado"`
		} `json:"acreditacion"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &response); e != nil {
		t.Fatal(e)
	}
	if response.Acreditacion.Estado != "pendiente_reintento" || !response.Usuario.EmailVerificado {
		t.Fatal(w.Body.String())
	}
	var persisted bson.M
	if e := s.users.FindOne(context.Background(), bson.M{"id": response.Usuario.ID}).Decode(&persisted); e != nil {
		t.Fatal(e)
	}
	if persisted["acreditacionPendiente"] != "op-001" {
		t.Fatal(persisted)
	}
	raw, _ := json.Marshal(persisted)
	if bytes.Contains(raw, []byte(strings.Repeat("t", 43))) || bytes.Contains(w.Body.Bytes(), []byte("acreditacionPendiente")) {
		t.Fatal("expuso credenciales internas")
	}
	// Otra instancia de Auth recupera la entrega desde MongoDB, sin depender de memoria.
	confirmado.Store(true)
	s2 := *s
	s2.procesarAcreditaciones(context.Background())
	s2.procesarAcreditaciones(context.Background())
	if e := s.users.FindOne(context.Background(), bson.M{"id": response.Usuario.ID, "acreditacionPendiente": bson.M{"$exists": false}}).Err(); e != nil {
		t.Fatal("no limpió outbox", e)
	}
	if entregas.Load() != 2 {
		t.Fatal("no recuperó exactamente una entrega", entregas.Load())
	}
	if w := authJSON(r, "/login", map[string]string{"email": "invitada@example.com", "password": "clave-segura-123"}, ""); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	// La verificación de email está protegida y no se controla desde el registro público.
	lookup := map[string]string{"email": "invitada@example.com"}
	if w := authJSON(r, "/internal/usuarios/buscar", lookup, ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := authJSON(r, "/internal/usuarios/buscar", lookup, "internal-test"); w.Code != 200 || !strings.Contains(w.Body.String(), `"emailVerificado":true`) {
		t.Fatal(w.Code, w.Body.String())
	}
	ordinary := map[string]any{"nombre": "Normal", "email": "normal@example.com", "password": "clave-segura-123", "emailVerificado": true}
	w = authJSON(r, "/registro", ordinary, "")
	if w.Code != 201 {
		t.Fatal(w.Code)
	}
	if e := s.users.FindOne(context.Background(), bson.M{"email": "normal@example.com", "emailVerificado": true}).Err(); e != mongo.ErrNoDocuments {
		t.Fatal("el usuario controló emailVerificado", e)
	}
}
func TestRegistroRechazaInvitacionAntesDeCrearCuenta(t *testing.T) {
	for _, status := range []int{403, 404, 409, 410, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			s := authInvitacionesPrueba(t)
			b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
			defer b.Close()
			s.beneficiosURL = b.URL
			w := authJSON(s.router(), "/registro", map[string]string{"nombre": "Invitada", "email": "persona@example.com", "password": "clave-segura-123", "invitacionToken": strings.Repeat("t", 43)}, "")
			expected := status
			if status == 404 {
				expected = 400
			}
			if w.Code != expected {
				t.Fatal(w.Code, w.Body.String())
			}
			count, e := s.users.CountDocuments(context.Background(), bson.M{})
			if e != nil || count != 0 {
				t.Fatal("creó cuenta sin validar invitación", count, e)
			}
		})
	}
}
