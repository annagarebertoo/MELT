package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Un 401 de Auth en la consulta interna significa claves internas distintas
// entre servicios, no una caída de Auth. El log debe poder distinguirlo.
func TestResolverEmailAuthDistingueClaveInternaRechazada(t *testing.T) {
	auth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Melt-Internal-Key") != "clave-de-auth" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer auth.Close()

	_, err := resolverEmailAuth(auth.URL, "otra-clave")(context.Background(), "a@example.com")
	if !errors.Is(err, errClaveInternaRechazada) || causaFalloAuth(err) != "clave_interna_rechazada" {
		t.Fatalf("esperaba clave interna rechazada, obtuve %v", err)
	}
	_, err = resolverEmailAuth(auth.URL, "clave-de-auth")(context.Background(), "a@example.com")
	if !errors.Is(err, errUsuario) {
		t.Fatalf("con la clave correcta un email sin cuenta debe ser errUsuario, obtuve %v", err)
	}
	if err := verificadorAuth(auth.URL, "otra-clave")(context.Background(), "id"); causaFalloAuth(err) != "clave_interna_rechazada" {
		t.Fatalf("verificadorAuth: %v", err)
	}
}

func TestResolverEmailAuthSinConfiguracion(t *testing.T) {
	for _, c := range [][2]string{{"", "k"}, {"http://auth", ""}} {
		_, err := resolverEmailAuth(c[0], c[1])(context.Background(), "a@example.com")
		if causaFalloAuth(err) != "auth_no_configurado" {
			t.Fatalf("%v: esperaba auth_no_configurado, obtuve %v", c, err)
		}
	}
	if causaFalloAuth(errorRespuestaAuth(500)) != "auth_no_disponible" {
		t.Fatal("un 500 de Auth debe quedar como auth_no_disponible")
	}
}
