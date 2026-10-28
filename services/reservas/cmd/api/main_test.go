package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestMeRequiereIdentidadVerificada(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/me", requiereIdentidad(), func(c *gin.Context) { c.Status(http.StatusOK) })

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/me", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("se esperaba 401 sin identidad y se obtuvo %d", recorder.Code)
	}
}

func TestRequiereIdentidadRechazaRolIncorrecto(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/profesional", requiereIdentidad("profesional"), func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/profesional", nil)
	req.Header.Set("X-Auth-User-ID", "cliente-1")
	req.Header.Set("X-Auth-User-Email", "cliente@example.com")
	req.Header.Set("X-Auth-User-Roles", "cliente")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("se esperaba 403 para rol incorrecto y se obtuvo %d", recorder.Code)
	}
}

func TestMeDevuelveIdentidadDelGateway(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/me", requiereIdentidad(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"usuario": identidadDesdeHeaders(c)})
	})

	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set("X-Auth-User-ID", "cliente-1")
	req.Header.Set("X-Auth-User-Email", "CLIENTE@example.com")
	req.Header.Set("X-Auth-User-Roles", "cliente")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("se esperaba 200 y se obtuvo %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestPermisosPorOperacion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name       string
		operacion  string
		rol        string
		wantStatus int
	}{
		{"cliente crea reserva", "reservas.crear", "cliente", http.StatusOK},
		{"profesional no crea reserva de cliente", "reservas.crear", "profesional", http.StatusForbidden},
		{"recepcion consulta todas", "reservas.consultar-todas", "recepcion", http.StatusOK},
		{"cliente no consulta todas", "reservas.consultar-todas", "cliente", http.StatusForbidden},
		{"operación sin política denegada", "reservas.operacion-desconocida", "administrador", http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.GET("/operacion", requiereIdentidad(), requierePermiso(tt.operacion), func(c *gin.Context) {
				c.Status(http.StatusOK)
			})
			req := httptest.NewRequest(http.MethodGet, "/operacion", nil)
			req.Header.Set("X-Auth-User-ID", "user-1")
			req.Header.Set("X-Auth-User-Email", "user@example.com")
			req.Header.Set("X-Auth-User-Roles", tt.rol)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)
			if recorder.Code != tt.wantStatus {
				t.Fatalf("se esperaba %d y se obtuvo %d: %s", tt.wantStatus, recorder.Code, recorder.Body.String())
			}
		})
	}
}
