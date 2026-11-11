package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestDecodeCatalogValidations(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, body, content string
		status              int
	}{
		{"valid", `{"nombre":"Facial"}`, "application/json; charset=utf-8", 200},
		{"wrong case", `{"Nombre":"Facial"}`, "application/json", 400},
		{"unknown field", `{"nombre":"Facial","admin":true}`, "application/json", 400},
		{"duplicate", `{"nombre":"Facial","nombre":"Otro"}`, "application/json", 400},
		{"invalid UTF8", "{\"nombre\":\"" + string([]byte{0xff}) + "\"}", "application/json", 400},
		{"null", `{"nombre":null}`, "application/json", 400},
		{"array", `[]`, "application/json", 400},
		{"trailing", `{"nombre":"Facial"} {}`, "application/json", 400},
		{"broken", `{"nombre":"Facial"`, "application/json", 400},
		{"missing", `{}`, "application/json", 400},
		{"wrong type", `{"nombre":4}`, "application/json", 400},
		{"wrong content type", `{"nombre":"Facial"}`, "text/plain", 415},
		{"absent content type", `{"nombre":"Facial"}`, "", 415},
		{"oversize", `{"nombre":"` + strings.Repeat("a", 65536) + `"}`, "application/json", 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := gin.New()
			router.POST("/", func(c *gin.Context) {
				var item Categoria
				if decodeCatalog(c, &item) {
					c.Status(200)
				}
			})
			req := httptest.NewRequest("POST", "/", strings.NewReader(tc.body))
			if tc.content != "" {
				req.Header.Set("Content-Type", tc.content)
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != tc.status {
				t.Fatalf("got %d %s", w.Code, w.Body.String())
			}
		})
	}
}
func TestMutationPermissionsAllResources(t *testing.T) {
	auth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("Authorization") {
		case "Bearer admin":
			w.WriteHeader(204)
		case "Bearer unavailable":
			w.WriteHeader(500)
		default:
			w.WriteHeader(403)
		}
	}))
	defer auth.Close()
	router := crearRouter(&Servidor{authVerifyAdminURL: auth.URL}, func(context.Context) error { return nil })
	for _, resource := range []string{"tratamientos", "categorias", "profesionales"} {
		for _, method := range []string{"POST", "PUT", "DELETE"} {
			path := "/" + resource
			if method != "POST" {
				path += "/id"
			}
			for _, tc := range []struct {
				bearer string
				status int
			}{{"", 401}, {"Bearer cliente", 403}, {"Bearer unavailable", 503}} {
				req := httptest.NewRequest(method, path, strings.NewReader(`{}`))
				req.Header.Set("Authorization", tc.bearer)
				req.Header.Set("X-Auth-User-Role", "administrador")
				req.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)
				if w.Code != tc.status {
					t.Fatalf("%s %s: %d", method, path, w.Code)
				}
			}
		}
	}
}
func TestAuthRedirectFailsClosed(t *testing.T) {
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("redirect must not be followed")
		w.WriteHeader(204)
	}))
	defer destination.Close()
	auth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, 302) }))
	defer auth.Close()
	router := crearRouter(&Servidor{authVerifyAdminURL: auth.URL}, func(context.Context) error { return nil })
	req := httptest.NewRequest("DELETE", "/categorias/id", nil)
	req.Header.Set("Authorization", "Bearer admin")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != 503 {
		t.Fatal(w.Code)
	}
}
func TestTreatmentValidationBounds(t *testing.T) {
	baseline := Tratamiento{ID: "t-1", Nombre: "Tratamiento", Descripcion: "Descripción", Categoria: "Nueva categoría", Precio: 10, Moneda: "ARS", DuracionMinutos: 30}
	for _, tc := range []struct {
		name   string
		change func(*Tratamiento)
		valid  bool
	}{
		{"dynamic category", func(*Tratamiento) {}, true},
		{"bad ID", func(v *Tratamiento) { v.ID = "a/b" }, false},
		{"duration", func(v *Tratamiento) { v.DuracionMinutos = 0 }, false},
		{"negative", func(v *Tratamiento) { v.Precio = -1 }, false},
		{"currency", func(v *Tratamiento) { v.Moneda = "USD" }, false},
		{"invalid image", func(v *Tratamiento) { v.Imagenes = []string{"javascript:alert(1)"} }, false},
		{"credential image", func(v *Tratamiento) { v.Imagenes = []string{"https://user:pass@example.com/a"} }, false},
		{"duplicate refs normalized", func(v *Tratamiento) { v.ProfesionalesIDs = []string{"p1", " p1 "} }, true},
		{"blank ref", func(v *Tratamiento) { v.ProfesionalesIDs = []string{""} }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := baseline
			tc.change(&v)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			if got := normalizarTratamiento(c, &v); got != tc.valid {
				t.Fatalf("valid=%v", got)
			}
		})
	}
}
func TestRequiredPriceNotSilentlyZero(t *testing.T) {
	r := gin.New()
	r.POST("/", func(c *gin.Context) {
		var v Tratamiento
		if decodeCatalog(c, &v) {
			c.Status(200)
		}
	})
	body := `{"id":"t1","nombre":"Uno","descripcion":"Uno","categoria":"Facial","moneda":"ARS","duracionMinutos":30}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	var result map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "precio") {
		t.Fatal(w.Code, result)
	}
}

func TestAuditOpaqueIdentifiers(t *testing.T) {
	for _, id := range []string{"abc-1", "trat.v1", "área facial", "prof_1"} {
		if !idValido(id) {
			t.Errorf("opaque ID rejected: %q", id)
		}
	}
	for _, id := range []string{"", ".", "..", "a/b", "a?b", "a#b", "a\\b", " a", "a ", "a\n", strings.Repeat("á", 129)} {
		if idValido(id) {
			t.Errorf("invalid ID accepted: %q", id)
		}
	}
}
