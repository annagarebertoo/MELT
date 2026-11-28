package main

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestHTTPMetricRegistryExportsCountersHistogramAndReplica(t *testing.T) {
	registry := newHTTPMetricRegistry()
	registry.record("GET", "/tratamientos", 200, 25*time.Millisecond)
	registry.record("POST", "/tratamientos", 500, 120*time.Millisecond)
	registry.record("TRACE", "unmatched", 404, 2*time.Millisecond)

	var output bytes.Buffer
	registry.writePrometheus(&output)
	metrics := output.String()
	for _, expected := range []string{
		`catalog_replica_info{replica="` + registry.replica + `"} 1`,
		`catalog_http_requests_total{method="GET",route="/tratamientos",status="200",replica="` + registry.replica + `"} 1`,
		`catalog_http_requests_total{method="POST",route="/tratamientos",status="500",replica="` + registry.replica + `"} 1`,
		`catalog_http_requests_total{method="OTHER",route="unmatched",status="404",replica="` + registry.replica + `"} 1`,
		`catalog_http_request_duration_seconds_bucket{method="GET",route="/tratamientos",status="200",replica="` + registry.replica + `",le="0.025"} 1`,
		`catalog_http_request_duration_seconds_bucket{method="GET",route="/tratamientos",status="200",replica="` + registry.replica + `",le="+Inf"} 1`,
		`catalog_http_request_duration_seconds_sum{method="POST",route="/tratamientos",status="500",replica="` + registry.replica + `"} 0.12`,
		`catalog_http_request_duration_seconds_count{method="POST",route="/tratamientos",status="500",replica="` + registry.replica + `"} 1`,
	} {
		if !strings.Contains(metrics, expected) {
			t.Errorf("metrics output missing %q:\n%s", expected, metrics)
		}
	}
}

func TestObservabilityMiddlewareRecordsHTTPMetricsButNotScrapes(t *testing.T) {
	testTraceProvider(t)
	router := gin.New()
	router.Use(observabilityMiddleware(slog.New(slog.NewJSONHandler(io.Discard, nil))))
	router.GET("/s2-metrics-check", func(c *gin.Context) { c.Status(http.StatusCreated) })
	router.GET("/metrics", func(c *gin.Context) { c.Status(http.StatusOK) })

	for _, path := range []string{"/s2-metrics-check", "/metrics"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
	}

	var output bytes.Buffer
	catalogHTTPMetrics.writePrometheus(&output)
	metrics := output.String()
	if !strings.Contains(metrics, `catalog_http_requests_total{method="GET",route="/s2-metrics-check",status="201",replica="`+catalogHTTPMetrics.replica+`"} 1`) {
		t.Fatalf("middleware did not record the HTTP response metric:\n%s", metrics)
	}
	if strings.Contains(metrics, `route="/metrics"`) {
		t.Fatal("Prometheus scrape endpoint counted itself")
	}
}
