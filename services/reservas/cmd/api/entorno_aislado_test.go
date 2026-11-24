package main

import (
	"context"
	"database/sql"
	"fmt"
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
