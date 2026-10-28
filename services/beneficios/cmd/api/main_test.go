package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCargarPuntos(t *testing.T) {
	got, err := cargarPuntos(`{"basico|mensual":100,"premium|anual":500}`)
	if err != nil {
		t.Fatalf("no esperaba error: %v", err)
	}
	if got["basico|mensual"] != 100 || got["premium|anual"] != 500 {
		t.Fatalf("configuración inesperada: %#v", got)
	}
}

func TestCargarPuntosRechazaJSONInvalidoYNegativos(t *testing.T) {
	if _, err := cargarPuntos("no es json"); err == nil {
		t.Fatal("se esperaba error para JSON inválido")
	}
	if _, err := cargarPuntos(`{"basico|mensual":-1}`); err == nil {
		t.Fatal("se esperaba error para puntos negativos")
	}
	if _, err := cargarPuntos(`{"Basico|Mensual":100,"basico|mensual":200}`); err == nil {
		t.Fatal("se esperaba error para claves repetidas")
	}
}

func TestRutaRegistroExternoRegistrada(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := crearRouter(nil)
	for _, route := range router.Routes() {
		if route.Method == http.MethodPost && route.Path == "/registro-externo" {
			return
		}
	}
	t.Fatal("la ruta POST /registro-externo no está registrada")
}

func TestHealthSinServidorDevuelve503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := crearRouter(nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("se esperaba %d y se obtuvo %d", http.StatusServiceUnavailable, recorder.Code)
	}
}

func TestMisPuntosRequiereIdentidadYRolCliente(t *testing.T) {
	gin.SetMode(gin.TestMode)
	servidor := &Servidor{}
	router := crearRouter(servidor)

	sinSesion := httptest.NewRecorder()
	router.ServeHTTP(sinSesion, httptest.NewRequest(http.MethodGet, "/mis-puntos", nil))
	if sinSesion.Code != http.StatusUnauthorized {
		t.Fatalf("sin identidad: esperado 401, obtenido %d", sinSesion.Code)
	}

	noCliente := httptest.NewRequest(http.MethodGet, "/mis-puntos", nil)
	noCliente.Header.Set("X-Auth-User-ID", "u-1")
	noCliente.Header.Set("X-Auth-User-Email", "admin@example.com")
	noCliente.Header.Set("X-Auth-User-Roles", "administrador")
	prohibido := httptest.NewRecorder()
	router.ServeHTTP(prohibido, noCliente)
	if prohibido.Code != http.StatusForbidden {
		t.Fatalf("rol no cliente: esperado 403, obtenido %d", prohibido.Code)
	}
}

func TestRutaMisPuntosRegistrada(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := crearRouter(nil)
	for _, route := range router.Routes() {
		if route.Method == http.MethodGet && route.Path == "/mis-puntos" {
			return
		}
	}
	t.Fatal("la ruta GET /mis-puntos no está registrada")
}
