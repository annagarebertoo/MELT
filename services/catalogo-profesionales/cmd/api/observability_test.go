package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/event"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func testTraceProvider(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		_ = provider.Shutdown(context.Background())
	})
	return exporter
}

func TestRequestIDValidation(t *testing.T) {
	for _, input := range []string{"sofia-d11_01", "a", strings.Repeat("b", 64)} {
		if got := requestID(input); got != input {
			t.Fatalf("valid ID was replaced: %q", input)
		}
	}
	for _, input := range []string{"", "has spaces", "bad\r\nheader", strings.Repeat("a", 65), "token=secret"} {
		got := requestID(input)
		if got == input || !validRequestID.MatchString(got) || len(got) != 32 {
			t.Fatalf("invalid ID was not replaced safely: %q", got)
		}
	}
}

func TestHTTPLogAndRemoteTraceContext(t *testing.T) {
	exporter := testTraceProvider(t)
	var output bytes.Buffer
	router := gin.New()
	router.Use(observabilityMiddleware(newJSONLogger(&output)))
	router.GET("/tratamientos/:id", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	req := httptest.NewRequest(http.MethodGet, "/tratamientos/private-id?token=private-token", nil)
	req.Header.Set("Authorization", "Bearer private-token")
	req.Header.Set("X-Request-ID", "sofia-d11-test")
	req.Header.Set("traceparent", "00-11111111111111111111111111111111-2222222222222222-01")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	if response.Code != http.StatusNoContent || response.Header().Get("X-Request-ID") != "sofia-d11-test" {
		t.Fatalf("HTTP behavior or correlation changed: %d", response.Code)
	}
	if response.Header().Get("X-Trace-ID") != "11111111111111111111111111111111" {
		t.Fatal("remote trace context was not preserved")
	}
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("invalid JSON log: %v", err)
	}
	for _, key := range []string{"timestamp", "level", "service", "method", "route", "status", "duration_ms", "request_id", "trace_id", "span_id"} {
		if _, ok := record[key]; !ok {
			t.Errorf("missing log field: %s", key)
		}
	}
	if record["route"] != "/tratamientos/:id" || strings.Contains(output.String(), "private-") {
		t.Fatal("resource ID, query or credentials leaked into access log")
	}
	spans := exporter.GetSpans()
	if len(spans) != 1 || spans[0].Parent.SpanID().String() != "2222222222222222" || !spans[0].Parent.IsRemote() {
		t.Fatal("HTTP span has no correct remote parent")
	}
}

func TestGeneratedIDAndSafePanicLog(t *testing.T) {
	testTraceProvider(t)
	var output bytes.Buffer
	router := gin.New()
	router.Use(observabilityMiddleware(newJSONLogger(&output)))
	router.GET("/panic", func(c *gin.Context) { panic("password=never-log-this") })
	for _, id := range []string{"", "invalid id"} {
		output.Reset()
		req := httptest.NewRequest(http.MethodGet, "/panic", nil)
		req.Header.Set("X-Request-ID", id)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != 500 || !validRequestID.MatchString(response.Header().Get("X-Request-ID")) {
			t.Fatal("panic recovery or generated correlation failed")
		}
		var record map[string]any
		if json.Unmarshal(output.Bytes(), &record) != nil || record["level"] != "ERROR" || strings.Contains(output.String(), "never-log-this") {
			t.Fatal("panic log is not a safe structured error")
		}
	}
}

func TestMongoMonitorParentAndPrivacy(t *testing.T) {
	exporter := testTraceProvider(t)
	ctx, parent := otel.Tracer("test").Start(context.Background(), "HTTP catalogo")
	monitor := mongoMonitor()
	raw, err := bson.Marshal(bson.D{
		{Key: "find", Value: "tratamientos"},
		{Key: "filter", Value: bson.D{{Key: "secret", Value: "private-value"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	monitor.Started(ctx, &event.CommandStartedEvent{
		Command: raw, DatabaseName: "catalogo_db", CommandName: "find",
		RequestID: 7, ConnectionID: "mongodb:27017[-1]",
	})
	monitor.Failed(ctx, &event.CommandFailedEvent{
		CommandFinishedEvent: event.CommandFinishedEvent{
			CommandName: "find", RequestID: 7, ConnectionID: "mongodb:27017[-1]",
		},
		Failure: errors.New("password=private-value"),
	})
	parent.End()
	spans := exporter.GetSpans()
	if len(spans) != 2 || spans[0].SpanKind != trace.SpanKindClient || spans[0].Parent.SpanID() != parent.SpanContext().SpanID() {
		t.Fatal("MongoDB span was not attached to its HTTP parent")
	}
	encoded, _ := json.Marshal(spans)
	if strings.Contains(string(encoded), "private-value") || strings.Contains(string(encoded), "db.query.text") {
		t.Fatal("MongoDB instrumentation exported a query value or raw error")
	}
}
