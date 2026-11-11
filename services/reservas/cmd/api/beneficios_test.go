package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestContratoInternoBeneficiosParaAplicarCanje(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.Header.Get("X-Melt-Internal-Key") != "clave-prueba" {
			t.Errorf("la llamada interna no envió su clave")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("cuerpo JSON inválido: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.URL.Path == "/internal/canjes/canje-1/reservar" {
			if body["usuarioId"] != "cliente-1" || body["reservaId"] != "carrito-1" || body["tratamientoId"] != "facial" || body["precio"] != float64(1000) {
				t.Errorf("datos de reserva inesperados: %#v", body)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"canje":{"id":"canje-1","descuento":100,"precioFinal":900}}`))
			return
		}
		if body["usuarioId"] != "cliente-1" || body["reservaId"] != "carrito-1" {
			t.Errorf("datos de aplicación inesperados: %#v", body)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	s := &servidorReservas{beneficiosURL: server.URL, internalKey: "clave-prueba", httpClient: server.Client()}
	canje, err := s.reservarBeneficio(context.Background(), "canje-1", "cliente-1", "carrito-1", "facial", 1000)
	if err != nil || canje.Descuento != 100 || canje.PrecioFinal != 900 {
		t.Fatalf("resultado inesperado: %+v, error=%v", canje, err)
	}
	if err := s.actualizarBeneficio(context.Background(), canje.ID, "cliente-1", "carrito-1", "aplicar"); err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 || paths[0] != "/internal/canjes/canje-1/reservar" || paths[1] != "/internal/canjes/canje-1/aplicar" {
		t.Fatalf("rutas llamadas: %v", paths)
	}
}

