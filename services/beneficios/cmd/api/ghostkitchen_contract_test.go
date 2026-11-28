package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestGhostKitchenAgainstPublishedContract runs against Prism started from the
// provider's published OpenAPI file, not a hand-written httptest response.
// Set GHOSTKITCHEN_CONTRACT_URL to http://localhost:4010/v1 to enable it.
func TestGhostKitchenAgainstPublishedContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("GHOSTKITCHEN_CONTRACT_URL")), "/")
	if baseURL == "" {
		t.Skip("set GHOSTKITCHEN_CONTRACT_URL to the GhostKitchen OpenAPI Prism mock")
	}
	t.Setenv("GHOSTKITCHEN_API_KEY", "consumer-contract-test-key")
	t.Setenv("GHOSTKITCHEN_BASE_URL", baseURL)

	router := crearRouter(servidorPrueba(nuevaMemoria()))
	cases := []struct {
		name, method, path, body string
		check                    func(*testing.T, map[string]any)
	}{
		{
			name: "listar sedes", method: http.MethodGet, path: "/ghostkitchen/sedes",
			check: func(t *testing.T, got map[string]any) {
				items, ok := got["items"].([]any)
				if !ok || len(items) == 0 {
					t.Fatalf("ListaSedes no cumple el contrato: %#v", got)
				}
			},
		},
		{
			name: "sede cercana", method: http.MethodGet,
			path: "/ghostkitchen/sedes/cercana?lat=-31.4262&lng=-64.1883",
			check: func(t *testing.T, got map[string]any) {
				if _, ok := got["distanciaKm"].(float64); !ok {
					t.Fatalf("SedeCercana.distanciaKm inválido: %#v", got["distanciaKm"])
				}
				if _, ok := got["sede"].(map[string]any); !ok {
					t.Fatalf("SedeCercana.sede inválida: %#v", got["sede"])
				}
			},
		},
		{
			name: "buscar platos", method: http.MethodGet,
			path: "/ghostkitchen/sedes/nueva-cordoba/platos?etiqueta=Vegano&page=1&pageSize=5",
			check: func(t *testing.T, got map[string]any) {
				for _, key := range []string{"sede", "items", "page", "pageSize", "total", "totalPages"} {
					if _, ok := got[key]; !ok {
						t.Fatalf("PaginaPlatos no incluye %q: %#v", key, got)
					}
				}
				items, ok := got["items"].([]any)
				if !ok || len(items) == 0 {
					t.Fatalf("PaginaPlatos.items inválido: %#v", got["items"])
				}
				plato, ok := items[0].(map[string]any)
				if !ok {
					t.Fatalf("Plato inválido: %#v", items[0])
				}
				for _, key := range []string{"id", "nombre", "categoria", "etiquetas", "precio", "moneda", "disponible", "restaurante", "sede", "actualizadoEn"} {
					if _, ok := plato[key]; !ok {
						t.Fatalf("Plato no incluye %q: %#v", key, plato)
					}
				}
			},
		},
		{
			name: "detalle de plato", method: http.MethodGet, path: "/ghostkitchen/platos/p1",
			check: func(t *testing.T, got map[string]any) {
				for _, key := range []string{"id", "nombre", "categoria", "etiquetas", "precio", "moneda", "disponible", "restaurante", "sede", "actualizadoEn"} {
					if _, ok := got[key]; !ok {
						t.Fatalf("Plato no incluye %q: %#v", key, got)
					}
				}
			},
		},
		{
			name: "comparar proximidad", method: http.MethodPost, path: "/ghostkitchen/proximidad",
			body: `{"ubicacionCliente":{"lat":-31.4262,"lng":-64.1883},"ubicacionSede":{"lat":-31.4201,"lng":-64.1888},"radioKm":5,"incluirDarkKitchen":true}`,
			check: func(t *testing.T, got map[string]any) {
				for _, key := range []string{"radioKm", "sedeExterna", "darkKitchen"} {
					if _, ok := got[key]; !ok {
						t.Fatalf("ResultadoProximidad no incluye %q: %#v", key, got)
					}
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Authorization", "Bearer cliente-test")
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
			}
			var got map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatalf("respuesta no JSON: %v: %s", err, w.Body.String())
			}
			tc.check(t, got)
		})
	}
}
