package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestContratoEstricto(t *testing.T) {
	for _, body := range []string{
		`{"solicitudId":"op","usuarioId":"u","PUNTOS":7}`,
		`{"solicitudId":"op","usuarioId":"u","puntos":7,"PUNTOS":8}`,
		`{"solicitudId":"op","usuarioId":"u","puntos":7,"puntos":8}`,
		`{"SolicitudId":"op","usuarioId":"u","puntos":7}`,
		`{"solicitudId":"op","UsuarioId":"u","puntos":7}`,
	} {
		t.Run(body, func(t *testing.T) {
			a := nuevaMemoria()
			w := request(crearRouter(servidorPrueba(a)), body, "test")
			if w.Code != 400 || len(a.ops) != 0 {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
	for _, ct := range []string{"", "text/plain", "application/xml", "application/json; invalid"} {
		t.Run("contenttype="+ct, func(t *testing.T) {
			a := nuevaMemoria()
			r := crearRouter(servidorPrueba(a))
			q := httptest.NewRequest("POST", "/acreditar-puntos", strings.NewReader(`{"solicitudId":"op","usuarioId":"u","puntos":7}`))
			q.Header.Set("X-Partner-Key", "test")
			if ct != "" {
				q.Header.Set("Content-Type", ct)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, q)
			if w.Code != 415 || len(a.ops) != 0 {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
	for _, ct := range []string{"application/json", "application/json; charset=utf-8"} {
		t.Run(ct, func(t *testing.T) {
			r := crearRouter(servidorPrueba(nuevaMemoria()))
			q := httptest.NewRequest("POST", "/acreditar-puntos", strings.NewReader(`{"solicitudId":"op","usuarioId":"u","puntos":7}`))
			q.Header.Set("X-Partner-Key", "test")
			q.Header.Set("Content-Type", ct)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, q)
			if w.Code != 201 {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
	for _, field := range []string{"solicitudId", "usuarioId"} {
		for _, char := range []string{"a", "é", "😀"} {
			for _, n := range []int{1, 100, 128, 129} {
				t.Run(fmt.Sprintf("%s-%s-%d", field, char, n), func(t *testing.T) {
					body := map[string]any{"solicitudId": "op", "usuarioId": "u", "puntos": 7}
					body[field] = strings.Repeat(char, n)
					raw, _ := json.Marshal(body)
					a := nuevaMemoria()
					w := request(crearRouter(servidorPrueba(a)), string(raw), "test")
					expected := 201
					if n > 128 {
						expected = 400
					}
					if w.Code != expected {
						t.Fatal(w.Code, w.Body.String())
					}
				})
			}
		}
	}
	w := request(crearRouter(servidorPrueba(nuevaMemoria())), `{"solicitudId":"op","usuarioId":"u","puntos":7}`+strings.Repeat(" ", 4096), "test")
	if w.Code != 400 {
		t.Fatal("límite de cuerpo", w.Code)
	}
}

func TestMisPuntosNoConfiaEnHeaders(t *testing.T) {
	a := nuevaMemoria()
	a.saldo["u"] = 10
	a.saldo["victima"] = 999
	r := crearRouter(servidorPrueba(a))
	for _, token := range []string{"", "Bearer falso", "Basic cliente-test", "Bearer cliente-test extra"} {
		q := httptest.NewRequest("GET", "/mis-puntos", nil)
		q.Header.Set("Authorization", token)
		q.Header.Set("X-Auth-User-ID", "victima")
		q.Header.Set("X-Auth-User-Email", "v@example.com")
		q.Header.Set("X-Auth-User-Roles", "cliente")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, q)
		if w.Code != 401 {
			t.Fatal(token, w.Code)
		}
	}
	q := httptest.NewRequest("GET", "/mis-puntos", nil)
	q.Header.Set("Authorization", "Bearer cliente-test")
	q.Header.Set("X-Auth-User-ID", "victima")
	q.Header.Set("X-Auth-User-Email", "v@example.com")
	q.Header.Set("X-Auth-User-Roles", "superadministrador")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, q)
	var body struct {
		UsuarioID string `json:"usuarioId"`
		Saldo     int64  `json:"saldoPuntos"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || body.UsuarioID != "u" || body.Saldo != 10 {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestVerificacionRealConAuthHTTP(t *testing.T) {
	for _, tc := range []struct {
		name             string
		status           int
		id, email, roles string
		expected         int
	}{
		{"cliente", 204, "u", "u@example.com", "cliente", 200},
		{"roles actuales", 204, "u", "u@example.com", "administrador", 403},
		{"token inválido o cuenta inactiva", 401, "", "", "", 401},
		{"auth caído", 503, "", "", "", 503},
		{"redirect rechazado", 302, "", "", "", 503},
		{"sin identidad", 204, "", "", "cliente", 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			auth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/verify" || r.Header.Get("Authorization") != "Bearer real" {
					t.Error("verificación incorrecta")
				}
				if r.Header.Get("X-Auth-User-ID") != "" {
					t.Error("se reenviaron headers no confiables")
				}
				w.Header().Set("X-Auth-User-ID", tc.id)
				w.Header().Set("X-Auth-User-Email", tc.email)
				w.Header().Set("X-Auth-User-Roles", tc.roles)
				w.WriteHeader(tc.status)
			}))
			defer auth.Close()
			s := servidorPrueba(nuevaMemoria())
			s.verificarSesion = verificadorSesionAuth(auth.URL)
			q := httptest.NewRequest("GET", "/mis-puntos", nil)
			q.Header.Set("Authorization", "Bearer real")
			q.Header.Set("X-Auth-User-ID", "victima")
			q.Header.Set("X-Auth-User-Roles", "cliente")
			w := httptest.NewRecorder()
			crearRouter(s).ServeHTTP(w, q)
			if w.Code != tc.expected {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
	t.Run("sin configuración", func(t *testing.T) {
		_, err := verificadorSesionAuth("")(context.Background(), "Bearer real")
		if err == nil || errors.Is(err, errSesionInvalida) {
			t.Fatal(err)
		}
	})
	t.Run("sin conexión", func(t *testing.T) {
		auth := httptest.NewServer(http.NotFoundHandler())
		base := auth.URL
		auth.Close()
		_, err := verificadorSesionAuth(base)(context.Background(), "Bearer real")
		if err == nil {
			t.Fatal("se aceptó sesión sin verificar")
		}
	})
}
