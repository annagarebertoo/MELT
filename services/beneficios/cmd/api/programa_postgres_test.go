package main

// Programa de recompensas contra PostgreSQL descartable (MELT_TEST_POSTGRES_URL):
// administración, canje idempotente, saldo insuficiente, concurrencia y ciclo
// de vida del beneficio aplicado a una reserva (reservar → aplicar/liberar/restituir).

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
)

const claveInternaBeneficios = "clave-interna-beneficios-prueba"

type fixturePrograma struct {
	srv     *Servidor
	almacen *almacenPostgres
	router  *gin.Engine
}

func nuevoFixturePrograma(t *testing.T) *fixturePrograma {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db := postgresBeneficiosAislado(t)
	almacen := &almacenPostgres{db: db}
	sesiones := map[string]identidadCliente{
		"Bearer token-cliente":  {ID: "cli-1", Email: "cli1@example.com", Roles: []string{"cliente"}},
		"Bearer token-cliente2": {ID: "cli-2", Email: "cli2@example.com", Roles: []string{"cliente"}},
		"Bearer token-admin":    {ID: "adm-1", Email: "adm@example.com", Roles: []string{"administrador"}},
		"Bearer token-prof":     {ID: "pro-1", Email: "pro@example.com", Roles: []string{"profesional"}},
	}
	srv := &Servidor{database: db, almacen: almacen, programa: almacen, claveInterna: claveInternaBeneficios,
		verificarSesion: func(_ context.Context, bearer string) (identidadCliente, error) {
			if id, ok := sesiones[bearer]; ok {
				return id, nil
			}
			if bearer == "Bearer auth-caido" {
				return identidadCliente{}, errors.New("auth no responde")
			}
			return identidadCliente{}, errSesionInvalida
		}}
	return &fixturePrograma{srv: srv, almacen: almacen, router: crearRouter(srv)}
}

func (f *fixturePrograma) pedir(t *testing.T, metodo, ruta, token string, cuerpo any) (int, map[string]any) {
	t.Helper()
	var lector *bytes.Reader
	if cuerpo != nil {
		datos, _ := json.Marshal(cuerpo)
		lector = bytes.NewReader(datos)
	} else {
		lector = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(metodo, ruta, lector)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", token)
	}
	if strings.HasPrefix(ruta, "/internal/") {
		req.Header.Set("X-Melt-Internal-Key", claveInternaBeneficios)
	}
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	return rec.Code, resp
}

func (f *fixturePrograma) acreditar(t *testing.T, usuario string, puntos int64, solicitud string) {
	t.Helper()
	if _, _, err := f.almacen.acreditarDetalle(context.Background(), AcreditarRequest{SolicitudID: solicitud, UsuarioID: usuario, Puntos: puntos}, "", "prueba"); err != nil {
		t.Fatal(err)
	}
}

func (f *fixturePrograma) saldo(t *testing.T, usuario string) int64 {
	t.Helper()
	saldo, _, err := f.almacen.consultar(context.Background(), usuario)
	if err != nil {
		t.Fatal(err)
	}
	return saldo
}

func TestProgramaAdministracionDeRecompensasYReglas(t *testing.T) {
	f := nuevoFixturePrograma(t)
	descuento := map[string]any{"id": "desc-20", "nombre": "20 % off", "tipo": "descuento_porcentaje", "costoPuntos": 200, "porcentaje": 20, "activa": true}
	gratis := map[string]any{"id": "masaje-gratis", "nombre": "Masaje gratis", "tipo": "tratamiento_gratis", "costoPuntos": 500, "tratamientoId": "trat-masaje", "activa": true}
	inactiva := map[string]any{"id": "vieja", "nombre": "Vieja", "tipo": "descuento_porcentaje", "costoPuntos": 10, "porcentaje": 5, "activa": false}

	if status, _ := f.pedir(t, "POST", "/admin/recompensas", "Bearer token-cliente", descuento); status != http.StatusForbidden {
		t.Fatalf("un cliente no administra recompensas: %d", status)
	}
	if status, _ := f.pedir(t, "POST", "/admin/recompensas", "", descuento); status != http.StatusUnauthorized {
		t.Fatalf("sin sesión: %d", status)
	}
	for _, r := range []map[string]any{descuento, gratis, inactiva} {
		if status, resp := f.pedir(t, "POST", "/admin/recompensas", "Bearer token-admin", r); status != http.StatusCreated && status != http.StatusOK {
			t.Fatalf("alta de %v: %d %v", r["id"], status, resp)
		}
	}
	invalida := map[string]any{"id": "mala", "nombre": "Mala", "tipo": "descuento_porcentaje", "costoPuntos": 10, "porcentaje": 150}
	if status, _ := f.pedir(t, "POST", "/admin/recompensas", "Bearer token-admin", invalida); status != http.StatusBadRequest {
		t.Fatalf("porcentaje > 100 debe rechazarse: %d", status)
	}
	descuento["porcentaje"] = 25
	if status, _ := f.pedir(t, "PUT", "/admin/recompensas/otro-id", "Bearer token-admin", descuento); status != http.StatusBadRequest {
		t.Fatalf("el ID de la URL debe coincidir: %d", status)
	}
	if status, _ := f.pedir(t, "PUT", "/admin/recompensas/desc-20", "Bearer token-admin", descuento); status != http.StatusOK {
		t.Fatalf("modificación: %d", status)
	}

	_, publicas := f.pedir(t, "GET", "/recompensas", "", nil)
	// beneficios.sql agrega una recompensa demo; se verifican los IDs propios.
	if p := mustJSONTest(publicas); !strings.Contains(p, `"desc-20"`) || !strings.Contains(p, `"masaje-gratis"`) || strings.Contains(p, `"vieja"`) {
		t.Fatalf("el catálogo público muestra solo las activas: %s", p)
	}
	_, todas := f.pedir(t, "GET", "/admin/recompensas", "Bearer token-admin", nil)
	if !strings.Contains(mustJSONTest(todas), `"vieja"`) {
		t.Fatalf("la administración ve también las inactivas: %v", todas)
	}

	if status, _ := f.pedir(t, "PUT", "/admin/reglas-tratamientos/trat-masaje", "Bearer token-admin", map[string]any{"puntos": 120}); status != http.StatusOK {
		t.Fatalf("regla: %d", status)
	}
	if status, _ := f.pedir(t, "PUT", "/admin/reglas-tratamientos/trat-masaje", "Bearer token-admin", map[string]any{"puntos": -1}); status != http.StatusBadRequest {
		t.Fatalf("puntos negativos: %d", status)
	}
	_, reglas := f.pedir(t, "GET", "/admin/reglas-tratamientos", "Bearer token-admin", nil)
	if !strings.Contains(mustJSONTest(reglas), `"puntos":120`) {
		t.Fatalf("reglas: %v", reglas)
	}
	// La regla define los puntos del evento de tratamiento realizado (una sola vez por evento).
	for i := 0; i < 2; i++ {
		if err := f.almacen.procesarEventoTratamiento(context.Background(), "ev-1", "cli-1", "trat-masaje"); err != nil {
			t.Fatal(err)
		}
	}
	if s := f.saldo(t, "cli-1"); s != 120 {
		t.Fatalf("un evento repetido acredita una vez: saldo %d", s)
	}
	if err := f.almacen.procesarEventoTratamiento(context.Background(), "ev-2", "cli-1", "sin-regla"); !errors.Is(err, errReglaNoExiste) {
		t.Fatalf("sin regla debe informar errReglaNoExiste: %v", err)
	}
}

func TestProgramaCanjeIdempotenteYSaldoInsuficiente(t *testing.T) {
	f := nuevoFixturePrograma(t)
	ctx := context.Background()
	if err := f.almacen.guardarRecompensa(ctx, Recompensa{ID: "desc-20", Nombre: "20 %", Tipo: "descuento_porcentaje", CostoPuntos: 200, Porcentaje: 20, Activa: true}); err != nil {
		t.Fatal(err)
	}
	pedido := map[string]any{"solicitudId": "sol-1", "recompensaId": "desc-20"}
	if status, resp := f.pedir(t, "POST", "/canjes", "Bearer token-cliente", pedido); status != 422 {
		t.Fatalf("sin puntos debe ser 422: %d %v", status, resp)
	}
	f.acreditar(t, "cli-1", 250, "carga-1")
	status, resp := f.pedir(t, "POST", "/canjes", "Bearer token-cliente", pedido)
	if status != http.StatusCreated {
		t.Fatalf("canje: %d %v", status, resp)
	}
	if status, _ = f.pedir(t, "POST", "/canjes", "Bearer token-cliente", pedido); status != http.StatusOK {
		t.Fatalf("la misma solicitud devuelve el canje existente: %d", status)
	}
	if s := f.saldo(t, "cli-1"); s != 50 {
		t.Fatalf("el reintento no debe descontar dos veces: saldo %d", s)
	}
	if status, _ = f.pedir(t, "POST", "/canjes", "Bearer token-cliente", map[string]any{"solicitudId": "sol-x", "recompensaId": "no-existe"}); status != http.StatusNotFound {
		t.Fatalf("recompensa inexistente: %d", status)
	}
	if status, _ = f.pedir(t, "POST", "/canjes", "Bearer token-cliente", map[string]any{"solicitudId": ""}); status != http.StatusBadRequest {
		t.Fatalf("solicitud vacía: %d", status)
	}
	if status, _ = f.pedir(t, "POST", "/canjes", "Bearer token-prof", pedido); status != http.StatusForbidden {
		t.Fatalf("solo clientes canjean: %d", status)
	}
	if status, _ = f.pedir(t, "POST", "/canjes", "Bearer auth-caido", pedido); status != http.StatusServiceUnavailable {
		t.Fatalf("Auth caído debe ser 503: %d", status)
	}
	if status, _ = f.pedir(t, "POST", "/canjes", "Bearer token-falso", pedido); status != http.StatusUnauthorized {
		t.Fatalf("token inválido debe ser 401: %d", status)
	}
	_, mios := f.pedir(t, "GET", "/mis-canjes", "Bearer token-cliente", nil)
	if n := len(mios["canjes"].([]any)); n != 1 {
		t.Fatalf("mis canjes: %d", n)
	}
	_, ajenos := f.pedir(t, "GET", "/mis-canjes", "Bearer token-cliente2", nil)
	if n := len(ajenos["canjes"].([]any)); n != 0 {
		t.Fatalf("otro cliente no ve canjes ajenos: %d", n)
	}
	_, puntos := f.pedir(t, "GET", "/mis-puntos", "Bearer token-cliente", nil)
	if puntos["saldoPuntos"].(float64) != 50 {
		t.Fatalf("mis puntos: %v", puntos)
	}
}

// D4: con saldo para un solo canje, solicitudes concurrentes distintas no
// pueden dejar el saldo negativo ni crear dos canjes.
func TestProgramaCanjesConcurrentesNoSobregiranElSaldo(t *testing.T) {
	f := nuevoFixturePrograma(t)
	ctx := context.Background()
	if err := f.almacen.guardarRecompensa(ctx, Recompensa{ID: "desc-10", Nombre: "10 %", Tipo: "descuento_porcentaje", CostoPuntos: 100, Porcentaje: 10, Activa: true}); err != nil {
		t.Fatal(err)
	}
	f.acreditar(t, "cli-1", 150, "carga-c")
	var wg sync.WaitGroup
	estados := make([]int, 8)
	for i := range estados {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			estados[i], _ = f.pedir(t, "POST", "/canjes", "Bearer token-cliente", map[string]any{"solicitudId": "conc-" + string(rune('a'+i)), "recompensaId": "desc-10"})
		}(i)
	}
	wg.Wait()
	exitos := 0
	for _, s := range estados {
		if s == http.StatusCreated {
			exitos++
		}
	}
	if exitos != 1 {
		t.Fatalf("debe concretarse exactamente un canje, hubo %d (%v)", exitos, estados)
	}
	if s := f.saldo(t, "cli-1"); s != 50 {
		t.Fatalf("saldo final esperado 50, obtenido %d", s)
	}
}

func TestProgramaBeneficioAplicadoAUnaReserva(t *testing.T) {
	f := nuevoFixturePrograma(t)
	ctx := context.Background()
	for _, r := range []Recompensa{
		{ID: "desc-15", Nombre: "15 %", Tipo: "descuento_porcentaje", CostoPuntos: 100, Porcentaje: 15, Activa: true},
		{ID: "facial-gratis", Nombre: "Facial gratis", Tipo: "tratamiento_gratis", CostoPuntos: 100, TratamientoID: "trat-facial", Activa: true},
	} {
		if err := f.almacen.guardarRecompensa(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	f.acreditar(t, "cli-1", 300, "carga-r")
	_, c1 := f.pedir(t, "POST", "/canjes", "Bearer token-cliente", map[string]any{"solicitudId": "r-1", "recompensaId": "desc-15"})
	_, c2 := f.pedir(t, "POST", "/canjes", "Bearer token-cliente", map[string]any{"solicitudId": "r-2", "recompensaId": "facial-gratis"})
	id1 := c1["canje"].(map[string]any)["id"].(string)
	id2 := c2["canje"].(map[string]any)["id"].(string)
	reserva := func(reservaID, tratamiento string, precio float64) map[string]any {
		return map[string]any{"usuarioId": "cli-1", "reservaId": reservaID, "tratamientoId": tratamiento, "precio": precio}
	}

	status, resp := f.pedir(t, "POST", "/internal/canjes/"+id1+"/reservar", "", reserva("res-A", "trat-x", 10001))
	canje := resp["canje"].(map[string]any)
	if status != 200 || canje["descuento"].(float64) != 1500.15 || canje["precioFinal"].(float64) != 8500.85 || canje["estado"] != "reservado" {
		t.Fatalf("descuento del 15 %% redondeado a centavos: %d %v", status, canje)
	}
	if status, _ = f.pedir(t, "POST", "/internal/canjes/"+id1+"/reservar", "", reserva("res-B", "trat-x", 5000)); status != 409 {
		t.Fatalf("un beneficio reservado no se usa en otra reserva: %d", status)
	}
	if status, _ = f.pedir(t, "POST", "/internal/canjes/"+id1+"/reservar", "", reserva("res-A", "trat-x", 10001)); status != 200 {
		t.Fatalf("la misma reserva renueva el bloqueo: %d", status)
	}
	if status, _ = f.pedir(t, "POST", "/internal/canjes/"+id1+"/liberar", "", reserva("res-A", "", 0)); status != 200 {
		t.Fatalf("liberar: %d", status)
	}
	if status, _ = f.pedir(t, "POST", "/internal/canjes/"+id1+"/reservar", "", reserva("res-B", "trat-x", 5000)); status != 200 {
		t.Fatalf("liberado puede reservarse en otra reserva: %d", status)
	}
	if status, _ = f.pedir(t, "POST", "/internal/canjes/"+id1+"/aplicar", "", reserva("res-B", "", 0)); status != 200 {
		t.Fatalf("aplicar: %d", status)
	}
	if status, _ = f.pedir(t, "POST", "/internal/canjes/"+id1+"/aplicar", "", reserva("res-B", "", 0)); status != 200 {
		t.Fatalf("aplicar es idempotente: %d", status)
	}
	if status, _ = f.pedir(t, "POST", "/internal/canjes/"+id1+"/reservar", "", reserva("res-C", "trat-x", 5000)); status != 409 {
		t.Fatalf("aplicado no se reutiliza: %d", status)
	}
	if status, _ = f.pedir(t, "POST", "/internal/canjes/"+id1+"/restituir", "", reserva("res-B", "", 0)); status != 200 {
		t.Fatalf("restituir tras cancelar el turno: %d", status)
	}
	if status, _ = f.pedir(t, "POST", "/internal/canjes/"+id1+"/restituir", "", reserva("res-B", "", 0)); status != 200 {
		t.Fatalf("restituir es idempotente (evento duplicado): %d", status)
	}

	// Tratamiento gratis: solo para su tratamiento y descuenta el precio completo.
	if status, _ = f.pedir(t, "POST", "/internal/canjes/"+id2+"/reservar", "", reserva("res-D", "trat-otro", 7000)); status != 503 && status != 409 {
		t.Fatalf("tratamiento gratis en otro tratamiento debe rechazarse: %d", status)
	}
	_, resp = f.pedir(t, "POST", "/internal/canjes/"+id2+"/reservar", "", reserva("res-D", "trat-facial", 7000))
	// precioFinal 0 se omite en JSON (omitempty).
	if c := resp["canje"].(map[string]any); c["precioFinal"] != nil || c["descuento"].(float64) != 7000 {
		t.Fatalf("tratamiento gratis: %v", c)
	}
	// Otro usuario no puede usar el canje.
	if status, _ = f.pedir(t, "POST", "/internal/canjes/"+id2+"/aplicar", "", map[string]any{"usuarioId": "cli-2", "reservaId": "res-D"}); status != 409 {
		t.Fatalf("un canje ajeno no se aplica: %d", status)
	}
	// Sin clave interna.
	req := httptest.NewRequest("POST", "/internal/canjes/"+id2+"/aplicar", strings.NewReader(`{"usuarioId":"cli-1","reservaId":"res-D"}`))
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("sin clave interna: %d", rec.Code)
	}
	if status, _ = f.pedir(t, "POST", "/internal/canjes/"+id2+"/aplicar", "", map[string]any{"usuarioId": ""}); status != 400 {
		t.Fatalf("datos incompletos: %d", status)
	}
}

func mustJSONTest(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
