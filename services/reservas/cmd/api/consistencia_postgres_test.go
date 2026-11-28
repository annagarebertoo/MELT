package main

// L1 — Consistencia de reservas y pagos contra PostgreSQL descartable.
// Ejecutar con scripts/test-consistencia-integracion.sh l1 (levanta compose.test.yaml).

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

type pagosFalso struct {
	mu            sync.Mutex
	estado        string
	preferencias  int
	verificacion  int
	devoluciones  map[string]int
	demoraVerify  time.Duration
	importesSenas map[string]float64
}

func (p *pagosFalso) totalDevoluciones() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	total := 0
	for _, n := range p.devoluciones {
		total += n
	}
	return total
}

type catalogoFalso struct {
	mu      sync.Mutex
	precios map[string]float64
}

func (c *catalogoFalso) cambiarPrecio(id string, precio float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.precios[id] = precio
}

type fixtureConsistencia struct {
	s        *servidorReservas
	router   http.Handler
	db       *sql.DB
	pagos    *pagosFalso
	catalogo *catalogoFalso
	dia      time.Time
}

const (
	profesionalL1  = "prof-l1"
	profesionalL1b = "prof-l1b"
	facialL1       = "facial-l1"
	masajeL1       = "masaje-l1"
)

func nuevoFixtureConsistencia(t *testing.T) *fixtureConsistencia {
	t.Helper()
	db := postgresReservasAislado(t)
	f := &fixtureConsistencia{db: db, pagos: &pagosFalso{estado: "approved", devoluciones: map[string]int{}, importesSenas: map[string]float64{}}, catalogo: &catalogoFalso{precios: map[string]float64{facialL1: 10000, masajeL1: 8000}}}
	duraciones := map[string]int{facialL1: 60, masajeL1: 45}
	categorias := map[string]string{facialL1: "cat-facial", masajeL1: "cat-masaje"}
	catalogo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/profesionales/") {
			id := strings.TrimPrefix(r.URL.Path, "/profesionales/")
			_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "activo": true, "tratamientosIds": []string{facialL1, masajeL1}})
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/tratamientos/")
		f.catalogo.mu.Lock()
		precio, ok := f.catalogo.precios[id]
		f.catalogo.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(tratamientoCatalogo{ID: id, Nombre: id, CategoriaID: categorias[id], DuracionMinutos: duraciones[id], ProfesionalesIDs: []string{profesionalL1, profesionalL1b}, Precio: precio, Moneda: "ARS"})
	}))
	t.Cleanup(catalogo.Close)
	pagos := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Melt-Internal-Key") != "l1-internal" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/payments/preferences":
			var req struct {
				CheckoutID string  `json:"checkoutId"`
				Amount     float64 `json:"importe"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			f.pagos.mu.Lock()
			f.pagos.preferencias++
			f.pagos.importesSenas[req.CheckoutID] = req.Amount
			f.pagos.mu.Unlock()
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(checkoutPreference{ID: "pref-" + req.CheckoutID, CheckoutID: req.CheckoutID, Amount: req.Amount, Currency: "ARS", InitPoint: "https://sandbox.mercadopago.test/" + req.CheckoutID})
		case "/payments/verify":
			var req struct {
				PaymentID string  `json:"pagoId"`
				Amount    float64 `json:"importe"`
				Reference string  `json:"referencia"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			f.pagos.mu.Lock()
			f.pagos.verificacion++
			estado, demora := f.pagos.estado, f.pagos.demoraVerify
			f.pagos.mu.Unlock()
			time.Sleep(demora)
			_ = json.NewEncoder(w).Encode(paymentVerification{CheckoutID: req.Reference, PaymentID: req.PaymentID, Amount: req.Amount, Currency: "ARS", Status: estado, Approved: estado == "approved"})
		case "/payments/refunds":
			f.pagos.mu.Lock()
			f.pagos.devoluciones[r.Header.Get("Idempotency-Key")]++
			f.pagos.mu.Unlock()
			w.WriteHeader(http.StatusCreated)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(pagos.Close)
	beneficios := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	t.Cleanup(beneficios.Close)

	f.s = &servidorReservas{pg: &postgresStore{db: db}, catalogoURL: catalogo.URL, pagosURL: pagos.URL, beneficiosURL: beneficios.URL, internalKey: "l1-internal", httpClient: &http.Client{Timeout: 10 * time.Second}}
	gin.SetMode(gin.TestMode)
	f.router = f.s.router()
	ctx := context.Background()
	for _, prof := range []string{profesionalL1, profesionalL1b} {
		for dia := 0; dia < 7; dia++ {
			if err := f.s.pg.guardarAgenda(ctx, agendaSemanal{ID: agendaControlID(prof, dia), ProfesionalID: prof, DiaSemana: dia, Bloques: []bloqueAgenda{{Inicio: "08:00", Fin: "20:00"}}, Activo: true}); err != nil {
				t.Fatal(err)
			}
		}
	}
	loc := zonaHorariaReservas()
	fecha := time.Now().In(loc).AddDate(0, 0, 7)
	f.dia = time.Date(fecha.Year(), fecha.Month(), fecha.Day(), 0, 0, 0, 0, loc)
	return f
}

func (f *fixtureConsistencia) horario(hora, minuto int) time.Time {
	return time.Date(f.dia.Year(), f.dia.Month(), f.dia.Day(), hora, minuto, 0, 0, f.dia.Location())
}

func (f *fixtureConsistencia) retener(t *testing.T, cliente, profesional, tratamiento string, inicio time.Time) *httptest.ResponseRecorder {
	t.Helper()
	body := fmt.Sprintf(`{"profesionalId":%q,"tratamientoId":%q,"inicio":%q}`, profesional, tratamiento, inicio.Format(time.RFC3339))
	return integrationRequestAs(f.router, http.MethodPost, "/carrito/bloqueos", body, cliente, "cliente")
}

func (f *fixtureConsistencia) retenerOK(t *testing.T, cliente, profesional, tratamiento string, inicio time.Time) string {
	t.Helper()
	res := f.retener(t, cliente, profesional, tratamiento, inicio)
	if res.Code != http.StatusCreated {
		t.Fatalf("retención %s %s: status=%d body=%s", profesional, inicio, res.Code, res.Body.String())
	}
	var out struct {
		Bloqueo bloqueoCarrito `json:"bloqueo"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil || out.Bloqueo.ID == "" {
		t.Fatalf("respuesta de retención inválida: %s", res.Body.String())
	}
	return out.Bloqueo.ID
}

func (f *fixtureConsistencia) checkout(cliente, clave string) (*httptest.ResponseRecorder, checkoutOperation) {
	res := integrationRequestAs(f.router, http.MethodPost, "/carrito/checkout", fmt.Sprintf(`{"idempotencyKey":%q}`, clave), cliente, "cliente")
	var out struct {
		Checkout checkoutOperation `json:"checkout"`
	}
	_ = json.Unmarshal(res.Body.Bytes(), &out)
	return res, out.Checkout
}

func (f *fixtureConsistencia) retorno(cliente, checkoutID, pagoID string) *httptest.ResponseRecorder {
	return integrationRequestAs(f.router, http.MethodPost, "/carrito/checkout/retorno", fmt.Sprintf(`{"checkoutId":%q,"pagoId":%q}`, checkoutID, pagoID), cliente, "cliente")
}

func (f *fixtureConsistencia) webhook(checkoutID, pagoID string) *httptest.ResponseRecorder {
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/pagos/mercadopago", strings.NewReader(fmt.Sprintf(`{"checkoutId":%q,"pagoId":%q}`, checkoutID, pagoID)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Melt-Internal-Key", "l1-internal")
	f.router.ServeHTTP(res, req)
	return res
}

func (f *fixtureConsistencia) contar(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := f.db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

func (f *fixtureConsistencia) estadoCheckout(t *testing.T, id string) string {
	t.Helper()
	var estado string
	if err := f.db.QueryRow(`select estado from checkouts where id=$1`, id).Scan(&estado); err != nil {
		t.Fatal(err)
	}
	return estado
}

// enParalelo lanza n goroutines que arrancan a la vez para maximizar la contención.
func enParalelo(n int, fn func(i int)) {
	var start, done sync.WaitGroup
	start.Add(1)
	done.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer done.Done()
			start.Wait()
			fn(i)
		}(i)
	}
	start.Done()
	done.Wait()
}

func TestL1RetencionesConcurrentesNoDuplicanHorario(t *testing.T) {
	f := nuevoFixtureConsistencia(t)
	// Una ronda por día de la semana: la primera reserva de cada día también
	// compite por crear la fila de control de agenda.
	const rondas, clientes = 7, 12
	for ronda := 0; ronda < rondas; ronda++ {
		dia := f.horario(14, 0).AddDate(0, 0, ronda)
		codigos := make([]int, clientes)
		cuerpos := make([]string, clientes)
		enParalelo(clientes, func(i int) {
			// Mitad compite por 14:00 y mitad por 14:15: todos se superponen.
			inicio := dia
			if i%2 == 1 {
				inicio = dia.Add(15 * time.Minute)
			}
			res := f.retener(t, fmt.Sprintf("cliente-%d-%02d", ronda, i), profesionalL1, facialL1, inicio)
			codigos[i], cuerpos[i] = res.Code, res.Body.String()
		})
		creadas, conflictos := 0, 0
		for i, code := range codigos {
			switch code {
			case http.StatusCreated:
				creadas++
			case http.StatusConflict:
				conflictos++
			default:
				t.Errorf("ronda %d: respuesta inesperada ante contención: %d %s (todas: %v)", ronda, code, cuerpos[i], codigos)
			}
		}
		if creadas != 1 || conflictos != clientes-1 {
			t.Fatalf("ronda %d: se esperaba 1 retención y %d conflictos; creadas=%d conflictos=%d", ronda, clientes-1, creadas, conflictos)
		}
	}
	if n := f.contar(t, `select count(*) from carritos_bloqueos where profesional_id=$1`, profesionalL1); n != rondas {
		t.Fatalf("carritos persistidos=%d, esperado %d", n, rondas)
	}
	if n := f.contar(t, `select count(*) from bloqueos_turnos where profesional_id=$1`, profesionalL1); n != 60*rondas {
		t.Fatalf("minutos bloqueados=%d, esperado %d (sin restos de transacciones fallidas)", n, 60*rondas)
	}
	if n := f.contar(t, `select count(distinct turno_id) from bloqueos_turnos`); n != rondas {
		t.Fatalf("bloqueos pertenecen a %d retenciones; esperado %d", n, rondas)
	}
}

func TestL1MismoClienteNoSuperponeCategoriaEnParalelo(t *testing.T) {
	f := nuevoFixtureConsistencia(t)
	codigos := make([]int, 6)
	enParalelo(len(codigos), func(i int) {
		// Distintos profesionales, misma categoría y horario para un único cliente.
		prof := profesionalL1
		if i%2 == 1 {
			prof = profesionalL1b
		}
		codigos[i] = f.retener(t, "cliente-unico", prof, facialL1, f.horario(10, 0)).Code
	})
	creadas := 0
	for _, code := range codigos {
		if code == http.StatusCreated {
			creadas++
		} else if code != http.StatusConflict {
			t.Fatalf("respuesta inesperada: %v", codigos)
		}
	}
	if creadas != 1 || f.contar(t, `select count(*) from carritos_bloqueos where cliente_id='cliente-unico'`) != 1 {
		t.Fatalf("el cliente quedó con %d retenciones superpuestas de la misma categoría (%v)", creadas, codigos)
	}
}

func TestL1CheckoutIdempotenteBajoConcurrencia(t *testing.T) {
	f := nuevoFixtureConsistencia(t)
	// Varias rondas con clientes distintos: la carrera entre la búsqueda por
	// clave y la asignación de retenciones sólo aparece con cierta frecuencia.
	const rondas, intentos = 8, 16
	for ronda := 0; ronda < rondas; ronda++ {
		cliente := fmt.Sprintf("cliente-a-%d", ronda)
		f.retenerOK(t, cliente, profesionalL1, facialL1, f.horario(8, ronda*90/15*15))
		f.retenerOK(t, cliente, profesionalL1b, masajeL1, f.horario(8, ronda*90/15*15))
		ids := make([]string, intentos)
		codigos := make([]int, intentos)
		enParalelo(intentos, func(i int) {
			res, order := f.checkout(cliente, "clave-idempotente-01")
			ids[i], codigos[i] = order.ID, res.Code
		})
		for i := range ids {
			if codigos[i] != http.StatusCreated || ids[i] == "" || ids[i] != ids[0] {
				t.Fatalf("ronda %d: checkout repetido no fue idempotente: códigos=%v ids=%v", ronda, codigos, ids)
			}
		}
		if n := f.contar(t, `select count(*) from checkouts where cliente_id=$1`, cliente); n != 1 {
			t.Fatalf("ronda %d: checkouts persistidos=%d, esperado 1", ronda, n)
		}
		if n := f.contar(t, `select count(*) from carritos_bloqueos where cliente_id=$1 and checkout_id=$2`, cliente, ids[0]); n != 2 {
			t.Fatalf("ronda %d: retenciones asociadas al checkout=%d, esperado 2", ronda, n)
		}
		var sena float64
		if err := f.db.QueryRow(`select sena from checkouts where id=$1`, ids[0]).Scan(&sena); err != nil || sena != importeSena(10000)+importeSena(8000) {
			t.Fatalf("seña persistida=%v error=%v", sena, err)
		}
		otra, _ := f.checkout(cliente, "clave-distinta-02")
		if otra.Code != http.StatusConflict {
			t.Fatalf("otra clave sobre el mismo carrito debe rechazarse: %d %s", otra.Code, otra.Body.String())
		}
		if n := f.contar(t, `select count(*) from checkouts where cliente_id=$1`, cliente); n != 1 {
			t.Fatalf("una segunda clave creó otro checkout: %d", n)
		}
	}
}

func TestL1ConfirmacionDePagoConcurrenteNoDuplicaTurnosNiDevuelve(t *testing.T) {
	f := nuevoFixtureConsistencia(t)
	f.retenerOK(t, "cliente-b", profesionalL1, facialL1, f.horario(15, 0))
	f.retenerOK(t, "cliente-b", profesionalL1, masajeL1, f.horario(17, 0))
	res, order := f.checkout("cliente-b", "clave-confirmacion-01")
	if res.Code != http.StatusCreated {
		t.Fatalf("checkout: %d %s", res.Code, res.Body.String())
	}
	// El retorno del navegador y varios webhooks reintentados llegan a la vez.
	const llamadas = 24
	codigos := make([]int, llamadas)
	enParalelo(llamadas, func(i int) {
		if i%2 == 0 {
			codigos[i] = f.retorno("cliente-b", order.ID, "pago-aprobado-01").Code
		} else {
			codigos[i] = f.webhook(order.ID, "pago-aprobado-01").Code
		}
	})
	for _, code := range codigos {
		if code != http.StatusOK {
			t.Fatalf("una notificación duplicada no fue idempotente: %v", codigos)
		}
	}
	if estado := f.estadoCheckout(t, order.ID); estado != "confirmed" {
		t.Fatalf("estado del checkout=%s", estado)
	}
	if n := f.contar(t, `select count(*) from turnos where cliente_id='cliente-b' and estado='confirmado' and pago_id='pago-aprobado-01'`); n != 2 {
		t.Fatalf("turnos confirmados=%d, esperado 2", n)
	}
	if n := f.contar(t, `select count(*) from carritos_bloqueos where cliente_id='cliente-b' and estado='confirmado' and turno_id is not null and expira_en is null`); n != 2 {
		t.Fatalf("retenciones confirmadas=%d, esperado 2", n)
	}
	if n := f.contar(t, `select count(*) from bloqueos_turnos b join turnos t on t.id=b.turno_id where t.cliente_id='cliente-b' and b.expira_en is null`); n != 60+45 {
		t.Fatalf("minutos transferidos a turnos=%d, esperado 105", n)
	}
	if n := f.pagos.totalDevoluciones(); n != 0 {
		t.Fatalf("un pago confirmado fue devuelto %d veces: %v", n, f.pagos.devoluciones)
	}
}

func TestL1FalloDeConfirmacionRevierteTodoYDevuelveUnaVez(t *testing.T) {
	f := nuevoFixtureConsistencia(t)
	primero := f.retenerOK(t, "cliente-c", profesionalL1, facialL1, f.horario(9, 0))
	segundo := f.retenerOK(t, "cliente-c", profesionalL1, masajeL1, f.horario(12, 0))
	res, order := f.checkout("cliente-c", "clave-rollback-01")
	if res.Code != http.StatusCreated {
		t.Fatalf("checkout: %d %s", res.Code, res.Body.String())
	}
	// El precio del segundo tratamiento cambia entre el checkout y el pago: el
	// primer turno ya se insertó dentro de la transacción cuando falla el segundo.
	f.catalogo.cambiarPrecio(masajeL1, 9500)
	const llamadas = 6
	codigos := make([]int, llamadas)
	enParalelo(llamadas, func(i int) {
		codigos[i] = f.webhook(order.ID, "pago-rollback-01").Code
	})
	if n := f.contar(t, `select count(*) from turnos where cliente_id='cliente-c'`); n != 0 {
		t.Fatalf("quedaron %d turnos parciales tras el rollback", n)
	}
	if estado := f.estadoCheckout(t, order.ID); estado != "reverted" {
		t.Fatalf("estado del checkout=%s, esperado reverted (códigos %v)", estado, codigos)
	}
	f.pagos.mu.Lock()
	devoluciones := f.pagos.devoluciones["refund-checkout-"+order.ID]
	f.pagos.mu.Unlock()
	if devoluciones < 1 {
		t.Fatal("no se pidió la devolución de la seña cobrada")
	}
	// Las retenciones vuelven a estar disponibles para un nuevo checkout.
	for _, id := range []string{primero, segundo} {
		cart, err := f.s.pg.carrito(context.Background(), id)
		if err != nil || cart.Estado != "pendiente_pago" || cart.CheckoutID != "" {
			t.Fatalf("retención %s no quedó liberada del checkout: %+v %v", id, cart, err)
		}
	}
	if n := f.contar(t, `select count(*) from bloqueos_turnos where turno_id in ($1,$2) and expira_en is not null`, primero, segundo); n != 60+45 {
		t.Fatalf("los minutos retenidos cambiaron durante el rollback: %d", n)
	}
	// Reentregas posteriores del mismo pago no generan otra devolución ni turnos.
	if res := f.webhook(order.ID, "pago-rollback-01"); res.Code != http.StatusOK {
		t.Fatalf("reentrega tras reversión: %d %s", res.Code, res.Body.String())
	}
	if n := f.pagos.totalDevoluciones(); n != devoluciones {
		t.Fatalf("la reentrega pidió otra devolución: %v", f.pagos.devoluciones)
	}
	f.catalogo.cambiarPrecio(masajeL1, 8000)
	res, nuevo := f.checkout("cliente-c", "clave-rollback-02")
	if res.Code != http.StatusCreated || nuevo.ID == order.ID {
		t.Fatalf("no se pudo reintentar la compra: %d %s", res.Code, res.Body.String())
	}
	if res := f.retorno("cliente-c", nuevo.ID, "pago-rollback-02"); res.Code != http.StatusOK {
		t.Fatalf("segundo pago: %d %s", res.Code, res.Body.String())
	}
	if n := f.contar(t, `select count(*) from turnos where cliente_id='cliente-c' and estado='confirmado'`); n != 2 {
		t.Fatalf("turnos tras reintento=%d", n)
	}
}

func TestL1RetencionVencidaNoSeConvierteEnTurno(t *testing.T) {
	f := nuevoFixtureConsistencia(t)
	id := f.retenerOK(t, "cliente-d", profesionalL1, facialL1, f.horario(16, 0))
	res, order := f.checkout("cliente-d", "clave-vencida-01")
	if res.Code != http.StatusCreated {
		t.Fatalf("checkout: %d %s", res.Code, res.Body.String())
	}
	vencido := time.Now().UTC().Add(-time.Minute)
	if _, err := f.db.Exec(`update carritos_bloqueos set expira_en=$2 where id=$1`, id, vencido); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`update bloqueos_turnos set expira_en=$2 where turno_id=$1`, id, vencido); err != nil {
		t.Fatal(err)
	}
	f.webhook(order.ID, "pago-vencido-01")
	if n := f.contar(t, `select count(*) from turnos where cliente_id='cliente-d'`); n != 0 {
		t.Fatalf("una retención vencida se convirtió en %d turnos", n)
	}
	if estado := f.estadoCheckout(t, order.ID); estado != "reverted" {
		t.Fatalf("estado=%s, esperado reverted", estado)
	}
	if f.pagos.totalDevoluciones() != 1 {
		t.Fatalf("devoluciones=%v", f.pagos.devoluciones)
	}
	// Otro cliente puede tomar el horario liberado.
	f.retenerOK(t, "cliente-e", profesionalL1, facialL1, f.horario(16, 0))
}

func TestL1PagoRechazadoLiberaCheckoutSinTurnos(t *testing.T) {
	f := nuevoFixtureConsistencia(t)
	f.retenerOK(t, "cliente-f", profesionalL1, facialL1, f.horario(13, 0))
	res, order := f.checkout("cliente-f", "clave-rechazo-01")
	if res.Code != http.StatusCreated {
		t.Fatal(res.Body.String())
	}
	f.pagos.mu.Lock()
	f.pagos.estado = "rejected"
	f.pagos.mu.Unlock()
	enParalelo(4, func(i int) { f.webhook(order.ID, "pago-rechazado-01") })
	if estado := f.estadoCheckout(t, order.ID); estado != "rejected" {
		t.Fatalf("estado=%s", estado)
	}
	if n := f.contar(t, `select count(*) from turnos where cliente_id='cliente-f'`); n != 0 {
		t.Fatalf("un pago rechazado creó %d turnos", n)
	}
	if n := f.contar(t, `select count(*) from carritos_bloqueos where cliente_id='cliente-f' and checkout_id is null and estado='pendiente_pago'`); n != 1 {
		t.Fatal("la retención no volvió a quedar disponible tras el rechazo")
	}
	f.pagos.mu.Lock()
	f.pagos.estado = "approved"
	f.pagos.mu.Unlock()
	res, nuevo := f.checkout("cliente-f", "clave-rechazo-02")
	if res.Code != http.StatusCreated {
		t.Fatal(res.Body.String())
	}
	if res := f.retorno("cliente-f", nuevo.ID, "pago-aprobado-02"); res.Code != http.StatusOK {
		t.Fatal(res.Body.String())
	}
	if n := f.contar(t, `select count(*) from turnos where cliente_id='cliente-f' and estado='confirmado'`); n != 1 {
		t.Fatalf("turnos=%d", n)
	}
	if f.pagos.totalDevoluciones() != 0 {
		t.Fatalf("devoluciones inesperadas: %v", f.pagos.devoluciones)
	}
}

func TestL1CancelacionConcurrenteEsIdempotente(t *testing.T) {
	f := nuevoFixtureConsistencia(t)
	f.retenerOK(t, "cliente-g", profesionalL1, facialL1, f.horario(18, 0))
	res, order := f.checkout("cliente-g", "clave-cancelar-01")
	if res.Code != http.StatusCreated {
		t.Fatal(res.Body.String())
	}
	if res := f.retorno("cliente-g", order.ID, "pago-cancelar-01"); res.Code != http.StatusOK {
		t.Fatal(res.Body.String())
	}
	var turnoID string
	if err := f.db.QueryRow(`select id from turnos where cliente_id='cliente-g'`).Scan(&turnoID); err != nil {
		t.Fatal(err)
	}
	codigos := make([]int, 8)
	enParalelo(len(codigos), func(i int) {
		codigos[i] = integrationRequestAs(f.router, http.MethodDelete, "/turnos/"+turnoID, "", "cliente-g", "cliente").Code
	})
	exitos := 0
	for _, code := range codigos {
		if code == http.StatusOK {
			exitos++
		} else if code != http.StatusConflict {
			t.Fatalf("respuesta inesperada: %v", codigos)
		}
	}
	if exitos != 1 {
		t.Fatalf("cancelaciones exitosas=%d (%v)", exitos, codigos)
	}
	if n := f.contar(t, `select count(*) from outbox_eventos where tipo='turno.cancelado' and payload->>'turnoId'=$1`, turnoID); n != 1 {
		t.Fatalf("eventos de cancelación=%d, esperado 1", n)
	}
	f.pagos.mu.Lock()
	devoluciones := f.pagos.devoluciones["refund-turno-"+turnoID]
	f.pagos.mu.Unlock()
	if devoluciones != 1 {
		t.Fatalf("devoluciones de la seña=%d, esperado 1", devoluciones)
	}
	if n := f.contar(t, `select count(*) from bloqueos_turnos where turno_id=$1`, turnoID); n != 0 {
		t.Fatalf("la cancelación dejó %d minutos bloqueados", n)
	}
	// El horario liberado puede volver a reservarse.
	f.retenerOK(t, "cliente-h", profesionalL1, facialL1, f.horario(18, 0))
}

func TestL1ControlDeAgendaSoportaPrimerasReservasSimultaneas(t *testing.T) {
	f := nuevoFixtureConsistencia(t)
	// La fila de control se crea con la primera reserva de cada profesional y
	// día; varias primeras reservas simultáneas no deben fallar con 503.
	for ronda := 0; ronda < 20; ronda++ {
		prof := fmt.Sprintf("prof-control-%d", ronda)
		errores := make([]error, 24)
		enParalelo(len(errores), func(i int) {
			errores[i] = f.s.pg.asegurarAgendaControl(context.Background(), prof, ronda%7)
		})
		for _, err := range errores {
			if err != nil {
				t.Fatalf("ronda %d: crear el control de agenda falló bajo concurrencia: %v", ronda, err)
			}
		}
		if n := f.contar(t, `select count(*) from control_agendas where profesional_id=$1`, prof); n != 1 {
			t.Fatalf("filas de control=%d", n)
		}
	}
}
