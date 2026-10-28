package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
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
	segments := strings.Split(token, ".")
	segments[1] = "e30"
	modificado := strings.Join(segments, ".")
	if _, err := s.validarToken(modificado); err == nil {
		t.Fatal("se esperaba rechazar el token modificado")
	}
}

func TestPasswordValida(t *testing.T) {
	if !passwordValida("12345678") { t.Fatal("debería aceptar 8 bytes") }
	if passwordValida("corta") { t.Fatal("no debería aceptar menos de 8 bytes") }
	if passwordValida(strings.Repeat("a", 73)) { t.Fatal("no debería aceptar más de 72 bytes para bcrypt") }
}

func TestTokenVencidoSeRechaza(t *testing.T) {
	s := &authServer{jwtSecret: []byte("clave-de-prueba-de-al-menos-32-caracteres"), tokenTTL: time.Minute}
	u := Usuario{ID:"usuario-1", Email:"cliente@example.com", Roles:[]string{"cliente"}}
	token, err := s.emitirToken(u)
	if err != nil { t.Fatal(err) }
	parts := strings.Split(token, ".")
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil { t.Fatal(err) }
	var cl claims
	if err := json.Unmarshal(payload, &cl); err != nil { t.Fatal(err) }
	cl.Expires = time.Now().Add(-time.Minute).Unix()
	payload, err = json.Marshal(cl)
	if err != nil { t.Fatal(err) }
	parts[1] = base64.RawURLEncoding.EncodeToString(payload)
	unsigned := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, s.jwtSecret)
	_, _ = mac.Write([]byte(unsigned))
	parts[2] = base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if _, err := s.validarToken(strings.Join(parts, ".")); err == nil {
		t.Fatal("se esperaba rechazar el token vencido")
	}
}

func TestRolesValidos(t *testing.T) {
	for _, rol := range []string{"cliente", "profesional", "recepcion", "administrador", "superadministrador"} {
		if !rolesValidos[rol] { t.Errorf("el rol %q debería existir", rol) }
	}
	if rolesValidos["root"] { t.Fatal("no debería aceptar roles no definidos") }
}
