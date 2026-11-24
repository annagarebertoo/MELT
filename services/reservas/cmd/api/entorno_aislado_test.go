package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// hostsCompartidos lista destinos que las pruebas nunca deben tocar: las bases
// de Atlas/Supabase y los servicios publicados en Render son entornos reales.
var hostsCompartidos = []string{"supabase.co", "supabase.com", "pooler.supabase", "mongodb.net", "render.com", "onrender.com", "cloudamqp.com"}

// exigirDestinoDescartable aborta la prueba si la URL apunta a un entorno compartido.
func exigirDestinoDescartable(t testing.TB, nombre, raw string) {
	t.Helper()
	parsed, err := url.Parse(raw)
	host := strings.ToLower(raw)
	if err == nil && parsed.Host != "" {
		host = strings.ToLower(parsed.Hostname())
	}
	for _, prohibido := range hostsCompartidos {
		if strings.Contains(host, prohibido) {
			t.Fatalf("%s apunta a un entorno compartido (%s); usá compose.test.yaml", nombre, prohibido)
		}
	}
}

// postgresReservasAislado crea una base PostgreSQL nueva con el esquema de
// infra/supabase/reservas.sql y la elimina al terminar la prueba.
func postgresReservasAislado(t *testing.T) *sql.DB {
	t.Helper()
	adminURL := strings.TrimSpace(os.Getenv("MELT_TEST_POSTGRES_URL"))
	if adminURL == "" {
		t.Skip("definí MELT_TEST_POSTGRES_URL (ver scripts/test-consistencia-integracion.sh)")
	}
	exigirDestinoDescartable(t, "MELT_TEST_POSTGRES_URL", adminURL)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := sql.Open("pgx", adminURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	nombre := fmt.Sprintf("reservas_test_%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(ctx, "create database "+nombre); err != nil {
		t.Fatalf("no se pudo crear la base descartable: %v", err)
	}
	parsed, err := url.Parse(adminURL)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Path = "/" + nombre
	db, err := sql.Open("pgx", parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	// Mismo límite de conexiones que abrirReservasPostgres en producción.
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	esquema, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "infra", "supabase", "reservas.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, string(esquema)); err != nil {
		t.Fatalf("no se pudo aplicar reservas.sql: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		admin, err := sql.Open("pgx", adminURL)
		if err != nil {
			return
		}
		defer admin.Close()
		_, _ = admin.ExecContext(ctx, "drop database if exists "+nombre+" with (force)")
	})
	return db
}

// rabbitMQAislado crea un vhost propio en el RabbitMQ descartable y devuelve la
// URL AMQP hacia ese vhost; el vhost (colas, exchanges y mensajes) se borra al final.
func rabbitMQAislado(t *testing.T) string {
	t.Helper()
	amqpURL := strings.TrimSpace(os.Getenv("MELT_TEST_RABBITMQ_URL"))
	apiURL := strings.TrimRight(strings.TrimSpace(os.Getenv("MELT_TEST_RABBITMQ_API")), "/")
	if amqpURL == "" || apiURL == "" {
		t.Skip("definí MELT_TEST_RABBITMQ_URL y MELT_TEST_RABBITMQ_API (ver scripts/test-consistencia-integracion.sh)")
	}
	exigirDestinoDescartable(t, "MELT_TEST_RABBITMQ_URL", amqpURL)
	exigirDestinoDescartable(t, "MELT_TEST_RABBITMQ_API", apiURL)
	vhost := fmt.Sprintf("reservas-test-%d", time.Now().UnixNano())
	api := func(method, path, body string) {
		t.Helper()
		req, err := http.NewRequest(method, apiURL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("API de RabbitMQ %s %s: %v", method, path, err)
		}
		resp.Body.Close()
		if resp.StatusCode >= 300 && !(method == http.MethodDelete && resp.StatusCode == http.StatusNotFound) {
			t.Fatalf("API de RabbitMQ %s %s: HTTP %d", method, path, resp.StatusCode)
		}
	}
	api(http.MethodPut, "/api/vhosts/"+vhost, "")
	user := "guest"
	if parsed, err := url.Parse(amqpURL); err == nil && parsed.User != nil {
		user = parsed.User.Username()
	}
	api(http.MethodPut, "/api/permissions/"+vhost+"/"+user, `{"configure":".*","write":".*","read":".*"}`)
	t.Cleanup(func() { api(http.MethodDelete, "/api/vhosts/"+vhost, "") })
	parsed, err := url.Parse(amqpURL)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Path = "/" + vhost
	return parsed.String()
}
