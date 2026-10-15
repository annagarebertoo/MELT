package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

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
		name, page, limit string
		wantPage, wantLimit int
		wantErr bool
	}{
		{"defaults", "", "", 1, 20, false},
		{"custom", "3", "50", 3, 50, false},
		{"page invalid", "0", "20", 0, 0, true},
		{"limit too high", "1", "101", 0, 0, true},
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
