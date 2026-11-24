package main

import (
	"encoding/json"
	"errors"
	"time"

	"context"
	"go.mongodb.org/mongo-driver/v2/bson"
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
		`melt_http_requests_total{service="reservas"`,
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

func TestTelemetriaContadoresDeNegocio(t *testing.T) {
	telMetricas = telNuevoRegistro()
	telMetricas.contar("melt_reservas_outbox_publicaciones_total", "ayuda", 1, "tipo", "tratamiento.realizado", "resultado", "publicado")
	telMetricas.contar("melt_reservas_outbox_publicaciones_total", "ayuda", 1, "tipo", "tratamiento.realizado", "resultado", "publicado")
	telMetricas.observar("melt_reservas_outbox_demora_segundos", "ayuda", 0.2, "tipo", "tratamiento.realizado")
	var b strings.Builder
	telMetricas.escribir(&b)
	salida := b.String()
	if !strings.Contains(salida, `tipo="tratamiento.realizado",resultado="publicado"} 2`) {
		t.Fatalf("contador no acumulado:\n%s", salida)
	}
	if !strings.Contains(salida, "# TYPE melt_reservas_outbox_demora_segundos histogram") ||
		!strings.Contains(salida, `melt_reservas_outbox_demora_segundos_bucket{service="reservas"`) {
		t.Fatalf("histograma no expuesto:\n%s", salida)
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
	if !strings.Contains(b.String(), `melt_http_client_requests_total{service="reservas"`) {
		t.Fatalf("llamada saliente no medida:\n%s", b.String())
	}
}

func TestOutboxConservaLaTrazaSinPublicarlaEnElCuerpo(t *testing.T) {
	otel.SetTextMapPropagator(propagation.TraceContext{})
	entrante := propagation.HeaderCarrier{"Traceparent": []string{"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"}}
	ctx := otel.GetTextMapPropagator().Extract(context.Background(), entrante)
	original := bson.M{"eventId": "e1"}
	guardado := conTrazaOutbox(ctx, original)
	if _, ok := original[claveTrazaOutbox]; ok {
		t.Fatal("no debe modificar el payload original")
	}
	// Simula el viaje por PostgreSQL (jsonb → map[string]any).
	ida, _ := json.Marshal(guardado)
	var leido bson.M
	_ = json.Unmarshal(ida, &leido)
	ctxPublicacion, cuerpo := trazaDesdeOutbox(context.Background(), leido)
	if _, ok := cuerpo[claveTrazaOutbox]; ok || cuerpo["eventId"] != "e1" {
		t.Fatalf("el cuerpo publicado no debe incluir la traza: %v", cuerpo)
	}
	cabeceras := map[string]any{}
	telInyectarCabeceras(ctxPublicacion, cabeceras)
	if tp, _ := cabeceras["traceparent"].(string); !strings.Contains(tp, "4bf92f3577b34da6a3ce929d0e0e4736") {
		t.Fatalf("la publicación no continúa la traza: %v", cabeceras)
	}
	if _, cuerpoSinTraza := trazaDesdeOutbox(context.Background(), bson.M{"eventId": "e2"}); cuerpoSinTraza["eventId"] != "e2" {
		t.Fatal("un evento sin traza debe quedar igual")
	}
}

func TestProximaEsperaOutboxDrenaRetrocedeYSeAcota(t *testing.T) {
	if got := proximaEsperaOutbox(intervaloOutbox, true, nil); got != 0 {
		t.Fatalf("tras publicar debe drenar sin esperar, obtuvo %v", got)
	}
	if got := proximaEsperaOutbox(0, false, nil); got != intervaloOutbox {
		t.Fatalf("sin pendientes debe volver al intervalo normal, obtuvo %v", got)
	}
	espera := time.Duration(0)
	vistas := []time.Duration{}
	for i := 0; i < 6; i++ {
		espera = proximaEsperaOutbox(espera, false, errors.New("broker caído"))
		vistas = append(vistas, espera)
	}
	esperado := []time.Duration{1200 * time.Millisecond, 2400 * time.Millisecond, 4800 * time.Millisecond, 9600 * time.Millisecond, 10 * time.Second, 10 * time.Second}
	for i := range esperado {
		if vistas[i] != esperado[i] {
			t.Fatalf("retroceso inesperado: %v", vistas)
		}
	}
}
