package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTelemetriaPagosMideRutasDelMux(t *testing.T) {
	telMetricas = telNuevoRegistro()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /payments/{id}", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotFound) })
	mux.HandleFunc("GET /panic", func(w http.ResponseWriter, r *http.Request) { panic("x") })
	mux.HandleFunc("GET /metrics", telManejadorMetricas)
	h := telEnvolver(mux)

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/payments/123", nil))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/panic", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("panic debe responder 500, obtuvo %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	salida := rec.Body.String()
	for _, esperado := range []string{
		`service="pagos"`,
		`route="GET /payments/{id}",status="404"} 1`,
		`route="GET /panic",status="500"} 1`,
	} {
		if !strings.Contains(salida, esperado) {
			t.Fatalf("falta %q:\n%s", esperado, salida)
		}
	}
	if rec.Header().Get("X-Request-ID") == "" {
		t.Fatal("falta X-Request-ID")
	}
}
