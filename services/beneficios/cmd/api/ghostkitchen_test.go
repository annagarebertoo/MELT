package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestGhostKitchenClientEnvioAutenticadoYFiltros(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "clave-de-prueba" {
			t.Errorf("X-API-Key=%q", r.Header.Get("X-API-Key"))
		}
		if r.Header.Get("X-Correlation-ID") != "req-123" {
			t.Errorf("X-Correlation-ID=%q", r.Header.Get("X-Correlation-ID"))
		}
		if r.URL.Path != "/public/v1/sedes/demo/platos" {
			t.Errorf("path=%s", r.URL.Path)
		}
		if r.URL.Query().Get("etiqueta") != "Sin TACC" || r.URL.Query().Get("pageSize") != "5" {
			t.Errorf("query=%v", r.URL.Query())
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Correlation-ID", "corr-externo")
		io.WriteString(w, `{"items":[{"id":"plato-1","disponible":true}],"page":1,"pageSize":5,"total":1,"totalPages":1}`)
	}))
	defer server.Close()
	client := newGhostKitchenClient("clave-de-prueba", server.Client().Transport, server.URL+"/public/v1")
	query := url.Values{"etiqueta": {"Sin TACC"}, "pageSize": {"5"}}
	got, err := client.request(context.Background(), http.MethodGet, "sedes/demo/platos", query, nil, "req-123")
	if err != nil {
		t.Fatal(err)
	}
	if got.status != 200 || got.correlationID != "corr-externo" || !strings.Contains(string(got.body), "plato-1") {
		t.Fatalf("respuesta inesperada: %+v %s", got, got.body)
	}
	if client.http.Timeout != 3*time.Second {
		t.Fatalf("timeout=%s", client.http.Timeout)
	}
}

func TestGhostKitchenPropagaOperacionesDesdeEndpointCliente(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var seen atomic.Int32
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.Add(1)
		if r.Header.Get("X-API-Key") != "clave-mock" {
			t.Errorf("faltó X-API-Key")
		}
		switch r.URL.Path {
		case "/sedes":
		case "/sedes/cercana":
			if r.URL.Query().Get("lat") != "-31.4" || r.URL.Query().Get("lng") != "-64.2" {
				t.Errorf("coords=%v", r.URL.Query())
			}
		case "/sedes/sede-1/platos":
			if r.URL.Query().Get("q") != "burger" || r.URL.Query().Get("restauranteId") != "r1" {
				t.Errorf("filtros=%v", r.URL.Query())
			}
		case "/platos/p1":
		case "/proximidad":
			if r.Method != http.MethodPost {
				t.Errorf("método=%s", r.Method)
			}
		default:
			t.Errorf("ruta externa=%s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, `{"ok":true}`)
	}))
	defer mock.Close()
	t.Setenv("GHOSTKITCHEN_API_KEY", "clave-mock")
	t.Setenv("GHOSTKITCHEN_BASE_URL", mock.URL)
	router := crearRouter(servidorPrueba(nuevaMemoria()))
	cases := []struct{ method, path, body string }{
		{http.MethodGet, "/ghostkitchen/sedes", ""},
		{http.MethodGet, "/ghostkitchen/sedes/cercana?lat=-31.4&lng=-64.2", ""},
		{http.MethodGet, "/ghostkitchen/sedes/sede-1/platos?q=burger&restauranteId=r1&ignorado=x", ""},
		{http.MethodGet, "/ghostkitchen/platos/p1", ""},
		{http.MethodPost, "/ghostkitchen/proximidad", `{"radioKm":5}`},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		req.Header.Set("Authorization", "Bearer cliente-test")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s %s: HTTP %d %s", tc.method, tc.path, w.Code, w.Body.String())
		}
	}
	if seen.Load() != int32(len(cases)) {
		t.Fatalf("se enviaron %d solicitudes, esperado %d", seen.Load(), len(cases))
	}
}

func TestGhostKitchenErrorEnEndpointConservaCodigoYCorrelationID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.Header().Set("X-Correlation-ID", "corr-sin-cobertura")
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `{"status":404,"code":"SIN_COBERTURA","detail":"No hay sedes cercanas","correlationId":"corr-sin-cobertura"}`)
	}))
	defer mock.Close()
	t.Setenv("GHOSTKITCHEN_API_KEY", "clave-mock")
	t.Setenv("GHOSTKITCHEN_BASE_URL", mock.URL)
	req := httptest.NewRequest(http.MethodGet, "/ghostkitchen/sedes/cercana?lat=-31.4&lng=-64.2", nil)
	req.Header.Set("Authorization", "Bearer cliente-test")
	w := httptest.NewRecorder()
	crearRouter(servidorPrueba(nuevaMemoria())).ServeHTTP(w, req)
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "SIN_COBERTURA") {
		t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
	}
	if w.Header().Get("X-Correlation-ID") != "corr-sin-cobertura" {
		t.Fatalf("X-Correlation-ID=%q", w.Header().Get("X-Correlation-ID"))
	}
}
func TestGhostKitchenErroresYReintentosAcotados(t *testing.T) {
	for _, tc := range []struct {
		name             string
		status, attempts int
	}{{"clave inválida", 401, 1}, {"sin cobertura", 404, 1}, {"límite excedido", 429, 3}, {"no disponible", 503, 3}} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Retry-After", "0")
				w.Header().Set("Content-Type", "application/problem+json")
				w.WriteHeader(tc.status)
				io.WriteString(w, `{"code":"PROBLEMA","correlationId":"corr-test"}`)
			}))
			defer server.Close()
			client := newGhostKitchenClient("key", server.Client().Transport, server.URL)
			got, err := client.request(context.Background(), http.MethodGet, "sedes/cercana", nil, nil, "")
			if err != nil {
				t.Fatal(err)
			}
			if got.status != tc.status || got.correlationID != "corr-test" {
				t.Fatalf("respuesta=%+v", got)
			}
			if calls.Load() != int32(tc.attempts) {
				t.Fatalf("llamadas=%d, esperadas=%d", calls.Load(), tc.attempts)
			}
		})
	}
}

func TestGhostKitchenRespetaRetryAfter(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, `{"ok":true}`)
	}))
	defer server.Close()
	client := newGhostKitchenClient("key", server.Client().Transport, server.URL)
	started := time.Now()
	got, err := client.request(context.Background(), http.MethodGet, "sedes", nil, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.status != http.StatusOK || calls.Load() != 2 {
		t.Fatalf("respuesta=%+v llamadas=%d", got, calls.Load())
	}
	if time.Since(started) < 950*time.Millisecond {
		t.Fatal("se reintentó antes del Retry-After")
	}
}
func TestGhostKitchenTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(100 * time.Millisecond); w.WriteHeader(200) }))
	defer server.Close()
	client := newGhostKitchenClient("key", server.Client().Transport, server.URL)
	client.http.Timeout = 20 * time.Millisecond
	if _, err := client.request(context.Background(), http.MethodGet, "sedes", nil, nil, ""); err == nil {
		t.Fatal("se esperaba timeout")
	}
}

func TestGhostKitchenSinClaveNoLlamaAlProveedor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldKey, hadKey := os.LookupEnv("GHOSTKITCHEN_API_KEY")
	oldBase, hadBase := os.LookupEnv("GHOSTKITCHEN_BASE_URL")
	t.Setenv("GHOSTKITCHEN_API_KEY", "")
	t.Setenv("GHOSTKITCHEN_BASE_URL", "")
	defer func() {
		if hadKey {
			_ = os.Setenv("GHOSTKITCHEN_API_KEY", oldKey)
		}
		if hadBase {
			_ = os.Setenv("GHOSTKITCHEN_BASE_URL", oldBase)
		}
	}()
	req := httptest.NewRequest(http.MethodGet, "/ghostkitchen/sedes", nil)
	req.Header.Set("Authorization", "Bearer cliente-test")
	w := httptest.NewRecorder()
	crearRouter(servidorPrueba(nuevaMemoria())).ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "GHOSTKITCHEN_NO_CONFIGURADO") {
		t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
	}
}
