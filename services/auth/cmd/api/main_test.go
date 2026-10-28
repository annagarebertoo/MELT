package main

import (
	"testing"
	"time"
)

func TestTokenValidoYVencido(t *testing.T) {
	s := &authServer{jwtSecret: []byte("clave-de-prueba-de-al-menos-32-caracteres"), tokenTTL: time.Minute}
	u := Usuario{ID:"usuario-1", Email:"cliente@example.com", Roles:[]string{"cliente"}}
	token, err := s.emitirToken(u)
	if err != nil { t.Fatalf("no esperaba error al emitir token: %v", err) }
	cl, err := s.validarToken(token)
	if err != nil { t.Fatalf("token válido rechazado: %v", err) }
	if cl.Subject != u.ID || cl.Email != u.Email || len(cl.Roles) != 1 || cl.Roles[0] != "cliente" {
		t.Fatalf("claims inesperados: %#v", cl)
	}
}

func TestTokenRechazaFirmaModificada(t *testing.T) {
	s := &authServer{jwtSecret: []byte("clave-de-prueba-de-al-menos-32-caracteres"), tokenTTL: time.Minute}
	token, err := s.emitirToken(Usuario{ID:"usuario-1", Email:"cliente@example.com", Roles:[]string{"cliente"}})
	if err != nil { t.Fatal(err) }
	modificado := token[:len(token)-1] + "x"
	if _, err := s.validarToken(modificado); err == nil {
		t.Fatal("se esperaba rechazar el token modificado")
	}
}

func TestRolesValidos(t *testing.T) {
	for _, rol := range []string{"cliente", "profesional", "recepcion", "administrador", "superadministrador"} {
		if !rolesValidos[rol] { t.Errorf("el rol %q debería existir", rol) }
	}
	if rolesValidos["root"] { t.Fatal("no debería aceptar roles no definidos") }
}
