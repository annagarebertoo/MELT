package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

func TestTelemetriaExponeMetricasPorRutaYConservaRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	telMetricas = telNuevoRegistro()
	r := gin.New()
	r.Use(telMiddleware(), gin.Recovery())
	r.GET("/metrics", telMetricasGin)
	r.GET("/turnos/:id", func(c *gin.Context) { c.Status(http.StatusConflict) })

	req := httptest.NewRequest(http.MethodGet, "/turnos/abc", nil)
	req.Header.Set("X-Request-ID", "req-123")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Header().Get("X-Request-ID") != "req-123" {
		t.Fatalf("request ID no preservado: %q", rec.Header().Get("X-Request-ID"))
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	cuerpo := rec.Body.String()
	for _, esperado := range []string{
		`melt_http_requests_total{service="beneficios"`,
		`route="/turnos/:id",status="409"} 1`,
		`melt_http_request_duration_seconds_bucket{`,
	} {
		if !strings.Contains(cuerpo, esperado) {
			t.Fatalf("falta %q en /metrics:\n%s", esperado, cuerpo)
		}
	}
	if strings.Contains(cuerpo, `route="/turnos/abc"`) {
		t.Fatal("la métrica no debe usar la URL concreta (cardinalidad)")
	}
}

func TestTelemetriaRequestIDInvalidoSeReemplaza(t *testing.T) {
	if id := telRequestID("con espacios y \n saltos"); id == "con espacios y \n saltos" || len(id) != 32 {
		t.Fatalf("request ID inválido aceptado: %q", id)
	}
	if id := telRequestID("ok-ID_1"); id != "ok-ID_1" {
		t.Fatalf("request ID válido reemplazado: %q", id)
	}
}

func TestTelemetriaPropagaTraceparentYRequestIDEnLlamadasSalientes(t *testing.T) {
	otel.SetTextMapPropagator(propagation.TraceContext{})
	var recibido http.Header
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recibido = r.Header.Clone()
	}))
	defer upstream.Close()

	telMetricas = telNuevoRegistro()
	cliente := &http.Client{Transport: telTransporte{base: http.DefaultTransport}}
	// Traza entrante simulada desde el Gateway.
	entrante := propagation.HeaderCarrier{"Traceparent": []string{"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"}}
	ctx := otel.GetTextMapPropagator().Extract(context.Background(), entrante)
	ctx = context.WithValue(ctx, telRequestIDKey{}, "req-xyz")
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, upstream.URL, nil)
	resp, err := cliente.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got := recibido.Get("X-Request-ID"); got != "req-xyz" {
		t.Fatalf("X-Request-ID no propagado: %q", got)
	}
	if got := recibido.Get("Traceparent"); !strings.Contains(got, "4bf92f3577b34da6a3ce929d0e0e4736") {
		t.Fatalf("traceparent no propagado: %q", got)
	}
	var b strings.Builder
	telMetricas.escribir(&b)
	if !strings.Contains(b.String(), `melt_http_client_requests_total{service="beneficios"`) {
		t.Fatalf("llamada saliente no medida:\n%s", b.String())
	}
}
