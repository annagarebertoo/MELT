package main

import (
	"bytes"
	"context"
	"encoding/json"
	"go.mongodb.org/mongo-driver/v2/bson"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestIntegracionRegistroLoginYMeConMongo(t *testing.T) {
	uri := os.Getenv("AUTH_TEST_MONGODB_URI")
	if uri == "" {
		t.Skip("definí AUTH_TEST_MONGODB_URI para ejecutar la prueba de integración con MongoDB")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("no se pudo configurar MongoDB de prueba: %v", err)
	}
	defer client.Disconnect(context.Background())
	if err := client.Ping(ctx, nil); err != nil {
		t.Fatalf("MongoDB de prueba no responde: %v", err)
	}

	db := client.Database("melt_auth_integration_test")
	users := db.Collection("usuarios")
	if err := users.Drop(ctx); err != nil {
		t.Fatalf("no se pudo limpiar la colección de prueba: %v", err)
	}
	t.Cleanup(func() { _ = users.Drop(context.Background()) })
	_, err = users.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "email", Value: 1}},
		Options: options.Index().SetUnique(true).SetName("uq_usuarios_email_test"),
	})
	if err != nil {
		t.Fatalf("no se pudo crear índice de prueba: %v", err)
	}

	s := &authServer{client: client, users: users, jwtSecret: []byte("clave-de-prueba-de-al-menos-32-caracteres"), tokenTTL: time.Minute, claveInterna: "test-internal"}
	gin.SetMode(gin.TestMode)
	router := s.router()

	body := []byte(`{"nombre":"Cliente Integración","email":"Cliente.Integracion@example.com","password":"clave-segura-123"}`)
	recorder := httptest.NewRecorder()
	registroReq := httptest.NewRequest(http.MethodPost, "/registro", bytes.NewReader(body))
	registroReq.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, registroReq)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("registro: se esperaba 201 y se obtuvo %d: %s", recorder.Code, recorder.Body.String())
	}

	if bytes.Contains(recorder.Body.Bytes(), []byte("beneficioRegistro")) {
		t.Fatal("el registro conserva el beneficio antiguo")
	}
	var alta struct {
		Usuario Usuario `json:"usuario"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &alta); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		id, key string
		status  int
	}{{alta.Usuario.ID, "", 401}, {alta.Usuario.ID, "wrong", 401}, {alta.Usuario.ID, "test-internal", 204}, {"missing", "test-internal", 404}} {
		q := httptest.NewRequest("GET", "/internal/usuarios/"+tc.id, nil)
		q.Header.Set("X-Melt-Internal-Key", tc.key)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, q)
		if w.Code != tc.status {
			t.Fatalf("consulta interna: %d", w.Code)
		}
	}
	recorder = httptest.NewRecorder()
	loginBody := []byte(`{"email":"cliente.integracion@example.com","password":"clave-segura-123"}`)
	loginReq := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, loginReq)
	if recorder.Code != http.StatusOK {
		t.Fatalf("login: se esperaba 200 y se obtuvo %d: %s", recorder.Code, recorder.Body.String())
	}
	var login map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &login); err != nil {
		t.Fatal(err)
	}
	token, ok := login["token"].(string)
	if !ok || token == "" {
		t.Fatal("el login no devolvió un token")
	}

	recorder = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("me: se esperaba 200 y se obtuvo %d: %s", recorder.Code, recorder.Body.String())
	}
	if bytes.Contains(recorder.Body.Bytes(), []byte("passwordHash")) {
		t.Fatal("la respuesta no debe exponer passwordHash")
	}

	recorder = httptest.NewRecorder()
	registroDuplicadoReq := httptest.NewRequest(http.MethodPost, "/registro", bytes.NewReader(body))
	registroDuplicadoReq.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, registroDuplicadoReq)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("email duplicado: se esperaba 409 y se obtuvo %d", recorder.Code)
	}
}

func TestAdministracionUsuariosYRoles(t *testing.T) {
	uri := os.Getenv("AUTH_TEST_MONGODB_URI")
	if uri == "" {
		t.Skip("definí AUTH_TEST_MONGODB_URI para ejecutar la prueba de integración con MongoDB")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Disconnect(context.Background())
	if err := client.Ping(ctx, nil); err != nil {
		t.Fatal(err)
	}
	db := client.Database("melt_auth_roles_test_" + strconv.FormatInt(time.Now().UnixNano(), 10))
	t.Cleanup(func() { _ = db.Drop(context.Background()) })
	users := db.Collection("usuarios")
	fixtures := []Usuario{
		{ID: "root", Nombre: "Root", Email: "root@test.local", PasswordHash: "sensitive-root-hash", Roles: []string{"superadministrador"}, Activo: true},
		{ID: "admin", Nombre: "Admin", Email: "admin@test.local", PasswordHash: "sensitive-admin-hash", Roles: []string{"administrador"}, Activo: true},
		{ID: "client", Nombre: "Cliente", Email: "client@test.local", PasswordHash: "sensitive-client-hash", Roles: []string{"cliente"}, Activo: true},
		{ID: "linked-pro", Nombre: "Profesional vinculado", Email: "linked@test.local", Roles: []string{"profesional"}, Activo: true},
		{ID: "free-pro", Nombre: "Profesional libre", Email: "free@test.local", Roles: []string{"profesional"}, Activo: true},
	}
	docs := make([]any, len(fixtures))
	for i := range fixtures {
		docs[i] = fixtures[i]
	}
	if _, err := users.InsertMany(ctx, docs); err != nil {
		t.Fatal(err)
	}
	catalog := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Melt-Internal-Key") != "test-internal" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/internal/profesionales/por-cuenta/linked-pro" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer catalog.Close()
	s := &authServer{client: client, users: users, roleControl: db.Collection("control_roles"), jwtSecret: []byte("clave-de-prueba-de-al-menos-32-caracteres"), tokenTTL: time.Minute, claveInterna: "test-internal", catalogoURL: catalog.URL}
	tokens := map[string]string{}
	for _, user := range fixtures {
		token, err := s.emitirToken(user)
		if err != nil {
			t.Fatal(err)
		}
		tokens[user.ID] = token
	}
	router := s.router()
	call := func(method, path, body, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	listed := call(http.MethodGet, "/usuarios", "", tokens["root"])
	if listed.Code != http.StatusOK || bytes.Contains(listed.Body.Bytes(), []byte("sensitive-")) || bytes.Contains(listed.Body.Bytes(), []byte("passwordHash")) {
		t.Fatalf("listado inseguro o fallido: %d %s", listed.Code, listed.Body.String())
	}
	for _, role := range []string{"admin", "client"} {
		if got := call(http.MethodGet, "/usuarios", "", tokens[role]).Code; got != http.StatusForbidden {
			t.Fatalf("%s listó usuarios: %d", role, got)
		}
		if got := call(http.MethodPut, "/usuarios/free-pro/rol", `{"rol":"cliente"}`, tokens[role]).Code; got != http.StatusForbidden {
			t.Fatalf("%s cambió roles: %d", role, got)
		}
	}
	if got := call(http.MethodPut, "/usuarios/linked-pro/rol", `{"rol":"cliente"}`, tokens["root"]).Code; got != http.StatusConflict {
		t.Fatalf("se permitió desactualizar un vínculo activo: %d", got)
	}
	if got := call(http.MethodPut, "/usuarios/free-pro/rol", `{"rol":"cliente"}`, tokens["root"]).Code; got != http.StatusOK {
		t.Fatalf("cambio de rol válido falló: %d %s", got, call(http.MethodPut, "/usuarios/free-pro/rol", `{"rol":"cliente"}`, tokens["root"]).Body.String())
	}
	if got := call(http.MethodPut, "/usuarios/client/rol", `{"rol":"recepcion"}`, tokens["root"]).Code; got != http.StatusOK {
		t.Fatalf("se rechazó el rol recepcion existente: %d", got)
	}
	if got := call(http.MethodPut, "/usuarios/root/rol", `{"rol":"administrador"}`, tokens["root"]).Code; got != http.StatusConflict {
		t.Fatalf("se permitió autodegradación: %d", got)
	}
	if err := validarConservacionSuperadmin(ctx, users, fixtures[0], "administrador"); err == nil || err.Error() != "ULTIMO_SUPERADMINISTRADOR" {
		t.Fatalf("no se protegió al último superadministrador: %v", err)
	}
	if got := call(http.MethodPut, "/usuarios/client/rol", `{"rol":"no-es-un-rol"}`, tokens["root"]).Code; got != http.StatusBadRequest {
		t.Fatalf("se aceptó un rol inválido: %d", got)
	}
	created := call(http.MethodPost, "/usuarios", `{"nombre":"Profesional nuevo","email":"nuevo-prof@test.local","password":"clave-segura-123","roles":["profesional"]}`, tokens["root"])
	if created.Code != http.StatusCreated || bytes.Contains(created.Body.Bytes(), []byte("passwordHash")) {
		t.Fatalf("creación de profesional falló o expuso hash: %d %s", created.Code, created.Body.String())
	}
	if got := call(http.MethodPut, "/usuarios/root/rol", `{"rol":"administrador"}`, tokens["root"]).Code; got == http.StatusOK {
		t.Fatal("el único superadministrador fue degradado")
	}
}

func TestListadoProfesionalesYValidacionInternaDeRol(t *testing.T) {
	uri := os.Getenv("AUTH_TEST_MONGODB_URI")
	if uri == "" {
		t.Skip("definí AUTH_TEST_MONGODB_URI para ejecutar la prueba de integración con MongoDB")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Disconnect(context.Background())
	db := client.Database("melt_auth_profesionales_test_" + time.Now().Format("150405000"))
	users := db.Collection("usuarios")
	t.Cleanup(func() { _ = db.Drop(context.Background()) })
	fixtures := []Usuario{
		{ID: "admin", Nombre: "Admin", Email: "admin@test.local", Roles: []string{"administrador"}, Activo: true},
		{ID: "professional", Nombre: "Profesional", Email: "prof@test.local", PasswordHash: "sensitive-hash", Roles: []string{"profesional"}, Activo: true},
		{ID: "client", Nombre: "Cliente", Email: "client@test.local", Roles: []string{"cliente"}, Activo: true},
		{ID: "inactive-professional", Nombre: "Inactivo", Email: "inactive@test.local", Roles: []string{"profesional"}, Activo: false},
	}
	docs := make([]any, len(fixtures))
	for i := range fixtures {
		docs[i] = fixtures[i]
	}
	if _, err := users.InsertMany(ctx, docs); err != nil {
		t.Fatal(err)
	}
	s := &authServer{client: client, users: users, jwtSecret: []byte("clave-de-prueba-de-al-menos-32-caracteres"), tokenTTL: time.Minute, claveInterna: "test-internal"}
	adminToken, err := s.emitirToken(fixtures[0])
	if err != nil {
		t.Fatal(err)
	}
	clientToken, err := s.emitirToken(fixtures[2])
	if err != nil {
		t.Fatal(err)
	}
	router := s.router()
	request := httptest.NewRequest(http.MethodGet, "/usuarios/profesionales", nil)
	request.Header.Set("Authorization", "Bearer "+adminToken)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte("prof@test.local")) || bytes.Contains(response.Body.Bytes(), []byte("client@test.local")) || bytes.Contains(response.Body.Bytes(), []byte("inactive@test.local")) || bytes.Contains(response.Body.Bytes(), []byte("sensitive-hash")) {
		t.Fatalf("professional account list did not filter/sanitize correctly: status=%d body=%s", response.Code, response.Body.String())
	}
	request = httptest.NewRequest(http.MethodGet, "/usuarios/profesionales", nil)
	request.Header.Set("Authorization", "Bearer "+clientToken)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("client accessed professional account list: %d", response.Code)
	}
	request = httptest.NewRequest(http.MethodGet, "/internal/usuarios/professional", nil)
	request.Header.Set("X-Melt-Internal-Key", "test-internal")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || response.Header().Get("X-Auth-User-Roles") != "profesional" {
		t.Fatalf("internal role validation failed: status=%d roles=%q", response.Code, response.Header().Get("X-Auth-User-Roles"))
	}
}
