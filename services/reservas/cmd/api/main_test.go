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
