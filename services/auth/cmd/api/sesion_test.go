package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// verificarSesion es el contrato con el Gateway (auth_request): 204 con la
// identidad verificada en encabezados, o 401 sin filtrar el motivo.
func TestVerificarSesionEsElContratoDelGateway(t *testing.T) {
	s := authInvitacionesPrueba(t)
	ctx := context.Background()
	activo := Usuario{ID: "u-activo", Nombre: "Activa", Email: "activa@example.com", Roles: []string{"cliente", "profesional"}, Activo: true, CreadoEn: time.Now()}
	inactivo := Usuario{ID: "u-inactivo", Nombre: "Inactiva", Email: "inactiva@example.com", Roles: []string{"cliente"}, Activo: false, CreadoEn: time.Now()}
	if _, err := s.users.InsertMany(ctx, []any{activo, inactivo}); err != nil {
		t.Fatal(err)
	}
	tokenActivo, _ := s.emitirToken(activo)
	tokenInactivo, _ := s.emitirToken(inactivo)
	tokenFantasma, _ := s.emitirToken(Usuario{ID: "u-borrado", Email: "x@example.com", Roles: []string{"cliente"}})
	r := s.router()

	pedir := func(autorizacion string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/verify", nil)
		if autorizacion != "" {
			req.Header.Set("Authorization", autorizacion)
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
	rec := pedir("Bearer " + tokenActivo)
	if rec.Code != http.StatusNoContent || rec.Header().Get("X-Auth-User-ID") != "u-activo" ||
		rec.Header().Get("X-Auth-User-Roles") != "cliente,profesional" || rec.Header().Get("X-Auth-User-Email") != "activa@example.com" {
		t.Fatalf("sesión válida: %d %v", rec.Code, rec.Header())
	}
	for nombre, aut := range map[string]string{
		"sin encabezado":   "",
		"esquema distinto": "Basic abc",
		"token adulterado": "Bearer " + tokenActivo + "x",
		"cuenta inactiva":  "Bearer " + tokenInactivo,
		"cuenta borrada":   "Bearer " + tokenFantasma,
	} {
		rec := pedir(aut)
		if rec.Code != http.StatusUnauthorized || rec.Header().Get("X-Auth-User-ID") != "" {
			t.Fatalf("%s: debe ser 401 sin identidad, obtuvo %d %v", nombre, rec.Code, rec.Header())
		}
	}

	// /me también rechaza una cuenta desactivada aunque el token siga vigente.
	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set("Authorization", "Bearer "+tokenInactivo)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("/me con cuenta inactiva: %d", rec.Code)
	}
}

func TestAdministradorInicialSeCreaUnaVezYValidaLaConfiguracion(t *testing.T) {
	s := authInvitacionesPrueba(t)
	ctx := context.Background()

	t.Setenv("AUTH_ADMIN_EMAIL", "")
	t.Setenv("AUTH_ADMIN_PASSWORD", "")
	if err := crearAdminInicial(ctx, s.users); err != nil {
		t.Fatalf("sin configuración no debe hacer nada: %v", err)
	}
	for _, c := range []struct{ email, clave string }{
		{"no-es-email", "una-clave-larga-123"},
		{"Admin@Example.com ", "corta"},
		{"admin@example.com", strings.Repeat("a", 73)},
		{"", "una-clave-larga-123"},
	} {
		t.Setenv("AUTH_ADMIN_EMAIL", c.email)
		t.Setenv("AUTH_ADMIN_PASSWORD", c.clave)
		if err := crearAdminInicial(ctx, s.users); err == nil {
			t.Fatalf("configuración inválida aceptada: %q", c.email)
		}
	}
	t.Setenv("AUTH_ADMIN_EMAIL", "Admin@Example.com")
	t.Setenv("AUTH_ADMIN_PASSWORD", "una-clave-larga-123")
	for i := 0; i < 2; i++ {
		if err := crearAdminInicial(ctx, s.users); err != nil {
			t.Fatalf("creación %d: %v", i, err)
		}
	}
	n, _ := s.users.CountDocuments(ctx, bson.M{"email": "admin@example.com", "roles": "superadministrador"})
	if n != 1 {
		t.Fatalf("debe existir exactamente un superadministrador, hay %d", n)
	}
	// El administrador creado puede iniciar sesión.
	rec := authJSON(s.router(), "/login", map[string]string{"email": "admin@example.com", "password": "una-clave-larga-123"}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("login del administrador inicial: %d %s", rec.Code, rec.Body.String())
	}
}

func TestEndpointsInternosDeUsuariosExigenClaveYValidanEntrada(t *testing.T) {
	s := authInvitacionesPrueba(t)
	ctx := context.Background()
	if _, err := s.users.InsertMany(ctx, []any{
		Usuario{ID: "u-1", Nombre: "Uno", Email: "uno@example.com", Roles: []string{"cliente"}, Activo: true, CreadoEn: time.Now()},
		Usuario{ID: "u-2", Nombre: "Dos", Email: "dos@example.com", Roles: []string{"cliente"}, Activo: false, CreadoEn: time.Now()},
	}); err != nil {
		t.Fatal(err)
	}
	r := s.router()
	casos := []struct {
		ruta   string
		cuerpo any
		clave  string
		status int
	}{
		{"/internal/usuarios/buscar", map[string]string{"email": "uno@example.com"}, "", 401},
		{"/internal/usuarios/buscar", map[string]string{"email": "uno@example.com"}, "internal-test", 200},
		{"/internal/usuarios/buscar", map[string]string{"email": "DOS@example.com"}, "internal-test", 409},
		{"/internal/usuarios/buscar", map[string]string{"email": "nadie@example.com"}, "internal-test", 404},
		{"/internal/usuarios/buscar", map[string]string{"email": "no-es-email"}, "internal-test", 400},
		{"/internal/usuarios/verificar-email", map[string]string{"usuarioId": "u-1", "email": "uno@example.com"}, "mala", 401},
		{"/internal/usuarios/verificar-email", map[string]string{"usuarioId": "u-1", "email": "otro@example.com"}, "internal-test", 404},
		{"/internal/usuarios/verificar-email", map[string]string{"usuarioId": "", "email": "uno@example.com"}, "internal-test", 400},
		{"/internal/usuarios/verificar-email", map[string]string{"usuarioId": "u-2", "email": "dos@example.com"}, "internal-test", 404},
		{"/internal/usuarios/verificar-email", map[string]string{"usuarioId": "u-1", "email": "Uno@Example.com"}, "internal-test", 204},
	}
	for _, c := range casos {
		if rec := authJSON(r, c.ruta, c.cuerpo, c.clave); rec.Code != c.status {
			t.Fatalf("%s %v (clave %q): esperado %d, obtuvo %d", c.ruta, c.cuerpo, c.clave, c.status, rec.Code)
		}
	}
	var u Usuario
	_ = s.users.FindOne(ctx, bson.M{"id": "u-1"}).Decode(&u)
	if !u.EmailVerificado {
		t.Fatal("el email debió quedar verificado")
	}
	sinClave := &authServer{users: s.users}
	if rec := authJSON(sinClave.router(), "/internal/usuarios/buscar", map[string]string{"email": "uno@example.com"}, "x"); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("sin clave interna configurada debe ser 503, obtuvo %d", rec.Code)
	}
}
