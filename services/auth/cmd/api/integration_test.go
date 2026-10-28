package main

import (
	"bytes"
	"go.mongodb.org/mongo-driver/v2/bson"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
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
	if err != nil { t.Fatalf("no se pudo configurar MongoDB de prueba: %v", err) }
	defer client.Disconnect(context.Background())
	if err := client.Ping(ctx, nil); err != nil { t.Fatalf("MongoDB de prueba no responde: %v", err) }

	db := client.Database("melt_auth_integration_test")
	users := db.Collection("usuarios")
	if err := users.Drop(ctx); err != nil { t.Fatalf("no se pudo limpiar la colección de prueba: %v", err) }
	t.Cleanup(func() { _ = users.Drop(context.Background()) })
	_, err = users.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "email", Value: 1}},
		Options: options.Index().SetUnique(true).SetName("uq_usuarios_email_test"),
	})
	if err != nil { t.Fatalf("no se pudo crear índice de prueba: %v", err) }

	s := &authServer{client: client, users: users, jwtSecret: []byte("clave-de-prueba-de-al-menos-32-caracteres"), tokenTTL: time.Minute}
	gin.SetMode(gin.TestMode)
	router := s.router()

	body := []byte(`{"nombre":"Cliente Integración","email":"Cliente.Integracion@example.com","password":"clave-segura-123"}`)
	recorder := httptest.NewRecorder()
	registroReq := httptest.NewRequest(http.MethodPost, "/registro", bytes.NewReader(body))
	registroReq.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, registroReq)
	if recorder.Code != http.StatusCreated { t.Fatalf("registro: se esperaba 201 y se obtuvo %d: %s", recorder.Code, recorder.Body.String()) }

	recorder = httptest.NewRecorder()
	loginBody := []byte(`{"email":"cliente.integracion@example.com","password":"clave-segura-123"}`)
	loginReq := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, loginReq)
	if recorder.Code != http.StatusOK { t.Fatalf("login: se esperaba 200 y se obtuvo %d: %s", recorder.Code, recorder.Body.String()) }
	var login map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &login); err != nil { t.Fatal(err) }
	token, ok := login["token"].(string)
	if !ok || token == "" { t.Fatal("el login no devolvió un token") }

	recorder = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK { t.Fatalf("me: se esperaba 200 y se obtuvo %d: %s", recorder.Code, recorder.Body.String()) }
	if bytes.Contains(recorder.Body.Bytes(), []byte("passwordHash")) { t.Fatal("la respuesta no debe exponer passwordHash") }

	recorder = httptest.NewRecorder()
	registroDuplicadoReq := httptest.NewRequest(http.MethodPost, "/registro", bytes.NewReader(body))
	registroDuplicadoReq.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, registroDuplicadoReq)
	if recorder.Code != http.StatusConflict { t.Fatalf("email duplicado: se esperaba 409 y se obtuvo %d", recorder.Code) }
}
