package main

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestHealthEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := crearRouter(nil, func(context.Context) error { return nil })

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("se esperaba status %d y se obtuvo %d", http.StatusOK, recorder.Code)
	}
}

func TestSearchIndexFreshnessFailsClosedAfterTarget(t *testing.T) {
	s := &Servidor{}
	if s.searchIndexFresh() {
		t.Fatal("index without successful build must be stale")
	}
	s.lastIndexSuccess.Store(time.Now().Unix())
	if !s.searchIndexFresh() {
		t.Fatal("recent successful generation must be searchable")
	}
	s.lastIndexSuccess.Store(time.Now().Add(-61 * time.Second).Unix())
	if s.searchIndexFresh() {
		t.Fatal("stale generation must not be served")
	}
}

func TestMetricsEndpointReportsCatalogCounters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &Servidor{}
	s.searchFailures.Store(2)
	s.indexFailures.Store(3)
	router := crearRouter(s, func(context.Context) error { return nil })
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "catalog_search_failures_total 2") || !strings.Contains(recorder.Body.String(), "catalog_index_rebuild_failures_total 3") {
		t.Fatalf("metrics endpoint did not expose expected counters: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestHealthEndpointDegradado(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := crearRouter(nil, func(context.Context) error { return errors.New("mongo no disponible") })

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("se esperaba status %d y se obtuvo %d", http.StatusServiceUnavailable, recorder.Code)
	}
}

func TestParsePagination(t *testing.T) {
	tests := []struct {
		name, page, limit   string
		wantPage, wantLimit int
		wantErr             bool
	}{
		{"defaults", "", "", 1, 10, false},
		{"custom", "3", "50", 3, 50, false},
		{"page invalid", "0", "20", 0, 0, true},
		{"limit too high", "1", "51", 0, 0, true},
		{"not a number", "abc", "20", 0, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page, limit, err := parsePagination(tt.page, tt.limit)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && (page != tt.wantPage || limit != tt.wantLimit) {
				t.Fatalf("obtuvo page=%d limit=%d; esperaba page=%d limit=%d", page, limit, tt.wantPage, tt.wantLimit)
			}
		})
	}
}

func TestEscapeRegex(t *testing.T) {
	got := escapeRegex("limpieza.facial+")
	want := `limpieza\.facial\+`
	if got != want {
		t.Fatalf("escapeRegex() = %q; esperado %q", got, want)
	}
}

func TestTotalPages(t *testing.T) {
	if got := totalPages(0, 20); got != 0 {
		t.Fatalf("totalPages(0, 20) = %d, esperado 0", got)
	}
	if got := totalPages(41, 20); got != 3 {
		t.Fatalf("totalPages(41, 20) = %d, esperado 3", got)
	}

}

func TestRutaObtenerTratamientoRegistrada(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := crearRouter(nil, func(context.Context) error { return nil })

	for _, route := range router.Routes() {
		if route.Method == http.MethodGet && route.Path == "/tratamientos/:id" {
			return
		}
	}
	t.Fatal("la ruta GET /tratamientos/:id no está registrada")
}

func TestPrecioValido(t *testing.T) {
	tests := []struct {
		name  string
		value float64
		want  bool
	}{
		{"cero", 0, true},
		{"positivo", 12500.50, true},
		{"negativo", -1, false},
		{"NaN", math.NaN(), false},
		{"positivo infinito", math.Inf(1), false},
		{"negativo infinito", math.Inf(-1), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := precioValido(tt.value); got != tt.want {
				t.Fatalf("precioValido(%v) = %v; esperado %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestTratamientoIncluyeProfesionalesAsociados(t *testing.T) {
	tratamiento := Tratamiento{ID: "t1", ProfesionalesIDs: []string{"p1", "p2"}}
	if len(tratamiento.ProfesionalesIDs) != 2 || tratamiento.ProfesionalesIDs[0] != "p1" {
		t.Fatalf("no se conservaron los profesionales asociados: %#v", tratamiento.ProfesionalesIDs)
	}
}

func TestProfesionalesSinAlmacenRespondeNoDisponible(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := crearRouter(&Servidor{}, func(context.Context) error { return nil })
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/profesionales?tratamientoId=t1", nil))
	if recorder.Code != http.StatusServiceUnavailable || !strings.Contains(recorder.Body.String(), "CATALOGO_NO_DISPONIBLE") {
		t.Fatalf("respuesta inesperada: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestAltaCatalogoVerificaRolConAuthInterno(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/verify-admin" {
			t.Errorf("ruta de Auth inesperada: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") == "Bearer administrador" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusForbidden)
	}))
	defer auth.Close()
	s := &Servidor{authVerifyAdminURL: auth.URL + "/verify-admin"}
	router := gin.New()
	router.POST("/admin", s.requiereAdmin(), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	cases := []struct {
		name, bearer string
		want         int
	}{{"sin sesión", "", http.StatusUnauthorized}, {"cliente", "Bearer cliente", http.StatusForbidden}, {"administrador", "Bearer administrador", http.StatusNoContent}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/admin", nil)
			if tc.bearer != "" {
				req.Header.Set("Authorization", tc.bearer)
			}
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)
			if recorder.Code != tc.want {
				t.Fatalf("status=%d body=%s; esperado %d", recorder.Code, recorder.Body.String(), tc.want)
			}
		})
	}
}
