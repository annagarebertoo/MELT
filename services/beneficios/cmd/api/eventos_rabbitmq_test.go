package main

// L2 — Consumidor de eventos de Beneficios contra RabbitMQ y PostgreSQL
// descartables: publicación, reentrega, deduplicación, DLQ y recuperación.
// Ejecutar con scripts/test-consistencia-integracion.sh l2.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rabbitmq/amqp091-go"
)

var hostsCompartidos = []string{"supabase.co", "supabase.com", "pooler.supabase", "mongodb.net", "render.com", "onrender.com", "cloudamqp.com"}

func exigirDestinoDescartable(t testing.TB, nombre, raw string) {
	t.Helper()
	host := strings.ToLower(raw)
	if parsed, err := url.Parse(raw); err == nil && parsed.Host != "" {
		host = strings.ToLower(parsed.Hostname())
	}
	for _, prohibido := range hostsCompartidos {
		if strings.Contains(host, prohibido) {
			t.Fatalf("%s apunta a un entorno compartido (%s); usá compose.test.yaml", nombre, prohibido)
		}
	}
}

// postgresBeneficiosAislado crea una base nueva con infra/supabase/beneficios.sql.
func postgresBeneficiosAislado(t *testing.T) *sql.DB {
	t.Helper()
	adminURL := strings.TrimSpace(os.Getenv("MELT_TEST_POSTGRES_URL"))
	if adminURL == "" {
		t.Skip("definí MELT_TEST_POSTGRES_URL (ver scripts/test-consistencia-integracion.sh)")
	}
	exigirDestinoDescartable(t, "MELT_TEST_POSTGRES_URL", adminURL)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := sql.Open("pgx", adminURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	nombre := fmt.Sprintf("beneficios_test_%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(ctx, "create database "+nombre); err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(adminURL)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Path = "/" + nombre
	db, err := sql.Open("pgx", parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(5)
	esquema, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "infra", "supabase", "beneficios.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, string(esquema)); err != nil {
		t.Fatalf("no se pudo aplicar beneficios.sql: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
		admin, err := sql.Open("pgx", adminURL)
		if err != nil {
			return
		}
		defer admin.Close()
		_, _ = admin.Exec("drop database if exists " + nombre + " with (force)")
	})
	return db
}

// rabbitMQAislado crea un vhost propio y lo elimina al terminar.
func rabbitMQAislado(t *testing.T) (amqpURL string, api func(method, path, body string) []byte) {
	t.Helper()
	base := strings.TrimSpace(os.Getenv("MELT_TEST_RABBITMQ_URL"))
	apiURL := strings.TrimRight(strings.TrimSpace(os.Getenv("MELT_TEST_RABBITMQ_API")), "/")
	if base == "" || apiURL == "" {
		t.Skip("definí MELT_TEST_RABBITMQ_URL y MELT_TEST_RABBITMQ_API (ver scripts/test-consistencia-integracion.sh)")
	}
	exigirDestinoDescartable(t, "MELT_TEST_RABBITMQ_URL", base)
	exigirDestinoDescartable(t, "MELT_TEST_RABBITMQ_API", apiURL)
	api = func(method, path, body string) []byte {
		t.Helper()
		req, err := http.NewRequest(method, apiURL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("API de RabbitMQ %s %s: %v", method, path, err)
		}
		defer resp.Body.Close()
		var raw json.RawMessage
		_ = json.NewDecoder(resp.Body).Decode(&raw)
		if resp.StatusCode >= 300 && !(method == http.MethodDelete && resp.StatusCode == http.StatusNotFound) {
			t.Fatalf("API de RabbitMQ %s %s: HTTP %d", method, path, resp.StatusCode)
		}
		return raw
	}
	vhost := fmt.Sprintf("beneficios-test-%d", time.Now().UnixNano())
	api(http.MethodPut, "/api/vhosts/"+vhost, "")
	parsed, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	user := "guest"
	if parsed.User != nil {
		user = parsed.User.Username()
	}
	api(http.MethodPut, "/api/permissions/"+vhost+"/"+user, `{"configure":".*","write":".*","read":".*"}`)
	t.Cleanup(func() { api(http.MethodDelete, "/api/vhosts/"+vhost, "") })
	parsed.Path = "/" + vhost
	return parsed.String(), api
}

type fixtureEventos struct {
	db        *sql.DB
	almacen   *almacenPostgres
	rabbitURL string
	api       func(method, path, body string) []byte
	vhost     string
	pub       *amqp091.Channel
}

func nuevoFixtureEventos(t *testing.T) *fixtureEventos {
	t.Helper()
	db := postgresBeneficiosAislado(t)
	rabbitURL, api := rabbitMQAislado(t)
	parsed, _ := url.Parse(rabbitURL)
	f := &fixtureEventos{db: db, almacen: &almacenPostgres{db: db}, rabbitURL: rabbitURL, api: api, vhost: strings.TrimPrefix(parsed.Path, "/")}
	conn, err := amqp091.Dial(rabbitURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if f.pub, err = conn.Channel(); err != nil {
		t.Fatal(err)
	}
	if err := f.pub.ExchangeDeclare(eventosExchange, "topic", true, false, false, false, nil); err != nil {
		t.Fatal(err)
	}
	return f
}

// iniciarConsumidor arranca una réplica del consumidor con reintentos cortos.
func (f *fixtureEventos) iniciarConsumidor(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		(&consumidorPrograma{url: f.rabbitURL, almacen: f.almacen, retrasos: []time.Duration{20 * time.Millisecond, 40 * time.Millisecond, 60 * time.Millisecond}}).run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("el consumidor no terminó: el canal de RabbitMQ quedó bloqueado")
		}
	})
	f.esperar(t, "la cola del consumidor", func() bool {
		_, err := f.cola(eventosQueue)
		return err == nil
	})
}

func (f *fixtureEventos) cola(nombre string) (amqp091.Queue, error) {
	conn, err := amqp091.Dial(f.rabbitURL)
	if err != nil {
		return amqp091.Queue{}, err
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		return amqp091.Queue{}, err
	}
	args := amqp091.Table(nil)
	if nombre == eventosQueue {
		args = amqp091.Table{"x-dead-letter-exchange": fallidosExchange, "x-dead-letter-routing-key": "event"}
	}
	return ch.QueueDeclarePassive(nombre, true, false, false, false, args)
}

func (f *fixtureEventos) publicar(t *testing.T, routingKey, messageID string, body []byte) {
	t.Helper()
	if err := f.pub.Publish(eventosExchange, routingKey, false, false, amqp091.Publishing{ContentType: "application/json", DeliveryMode: amqp091.Persistent, MessageId: messageID, Type: routingKey, Body: body}); err != nil {
		t.Fatal(err)
	}
}

func (f *fixtureEventos) esperar(t *testing.T, que string, cond func() bool) {
	t.Helper()
	limite := time.Now().Add(15 * time.Second)
	for time.Now().Before(limite) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("tiempo agotado esperando %s", que)
}

func (f *fixtureEventos) saldo(t *testing.T, usuario string) int64 {
	t.Helper()
	var saldo int64
	err := f.db.QueryRow(`select saldo_puntos from registros_externos where usuario_id=$1`, usuario).Scan(&saldo)
	if err == sql.ErrNoRows {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return saldo
}

func (f *fixtureEventos) contar(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := f.db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (f *fixtureEventos) colaVacia(t *testing.T, nombre string) bool {
	t.Helper()
	q, err := f.cola(nombre)
	if err != nil {
		t.Fatal(err)
	}
	return q.Messages == 0
}

func eventoTratamiento(id, usuario, tratamiento string) []byte {
	body, _ := json.Marshal(EventoDominio{EventID: id, Tipo: "tratamiento.realizado", TurnoID: "turno-" + id, UsuarioID: usuario, TratamientoID: tratamiento, RealizadoEn: time.Now().UTC()})
	return body
}

func TestL2ConsumidorAcreditaUnaVezConReentregasYReplicas(t *testing.T) {
	f := nuevoFixtureEventos(t)
	if _, err := f.db.Exec(`insert into reglas_puntos_tratamientos (tratamiento_id,puntos) values ('facial-l2',150)`); err != nil {
		t.Fatal(err)
	}
	// Dos réplicas de Beneficios consumen la misma cola.
	f.iniciarConsumidor(t)
	f.iniciarConsumidor(t)
	body := eventoTratamiento("tratamiento-realizado:turno-dup", "usuario-dup", "facial-l2")
	for i := 0; i < 10; i++ {
		f.publicar(t, "tratamiento.realizado", "tratamiento-realizado:turno-dup", body)
	}
	f.publicar(t, "tratamiento.realizado", "tratamiento-realizado:turno-otro", eventoTratamiento("tratamiento-realizado:turno-otro", "usuario-dup", "facial-l2"))
	f.esperar(t, "el procesamiento de todas las copias", func() bool {
		return f.colaVacia(t, eventosQueue) && f.saldo(t, "usuario-dup") == 300
	})
	time.Sleep(300 * time.Millisecond)
	if saldo := f.saldo(t, "usuario-dup"); saldo != 300 {
		t.Fatalf("saldo=%d, esperado 300 (dos eventos distintos, sin duplicar copias)", saldo)
	}
	if n := f.contar(t, `select count(*) from movimientos_puntos where usuario_id='usuario-dup'`); n != 2 {
		t.Fatalf("movimientos=%d, esperado 2", n)
	}
	if n := f.contar(t, `select count(*) from mensajes_fallidos`); n != 0 {
		t.Fatalf("la deduplicación mandó %d copias a la DLQ", n)
	}
}

func TestL2EventosInvalidosVanALaDLQSinBloquearElConsumidor(t *testing.T) {
	f := nuevoFixtureEventos(t)
	if _, err := f.db.Exec(`insert into reglas_puntos_tratamientos (tratamiento_id,puntos) values ('facial-dlq',25)`); err != nil {
		t.Fatal(err)
	}
	f.iniciarConsumidor(t)
	// Varias publicaciones confirmadas seguidas sobre el mismo canal del consumidor.
	for i := 1; i <= 4; i++ {
		f.publicar(t, "tratamiento.realizado", fmt.Sprintf("mensaje-corrupto-%02d", i), []byte(`{no es json`))
	}
	f.publicar(t, "tratamiento.realizado", "evento-valido-tras-dlq", eventoTratamiento("evento-valido-tras-dlq", "usuario-tras-dlq", "facial-dlq"))
	f.esperar(t, "los mensajes en la DLQ y el evento válido procesado", func() bool {
		q, err := f.cola(fallidosQueue)
		return err == nil && q.Messages == 4 && f.saldo(t, "usuario-tras-dlq") == 25
	})
	for i := 1; i <= 4; i++ {
		var intentos int
		var estado string
		if err := f.db.QueryRow(`select intentos, estado from mensajes_fallidos where id=$1`, fmt.Sprintf("mensaje-corrupto-%02d", i)).Scan(&intentos, &estado); err != nil {
			t.Fatalf("el mensaje inválido %d no quedó registrado: %v", i, err)
		}
		if intentos != 1 || estado != "pendiente" {
			t.Fatalf("un mensaje inválido no debe reintentarse: intentos=%d estado=%s", intentos, estado)
		}
	}
	if !f.colaVacia(t, eventosQueue) {
		t.Fatal("quedaron mensajes en la cola principal")
	}
}

func TestL2FalloPersistenteReintentaSinDuplicarSuscriptoresYTerminaEnDLQ(t *testing.T) {
	f := nuevoFixtureEventos(t)
	// Otro servicio suscripto al mismo evento de dominio.
	obs, err := f.pub.QueueDeclare("observador-l2", true, false, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.pub.QueueBind(obs.Name, "tratamiento.realizado", eventosExchange, false, nil); err != nil {
		t.Fatal(err)
	}
	f.iniciarConsumidor(t)
	// No hay regla de puntos para el tratamiento: el proceso falla siempre.
	id := "tratamiento-realizado:turno-sin-regla"
	f.publicar(t, "tratamiento.realizado", id, eventoTratamiento(id, "usuario-dlq", "tratamiento-sin-regla"))
	f.esperar(t, "el mensaje en la DLQ", func() bool {
		q, err := f.cola(fallidosQueue)
		return err == nil && q.Messages == 1
	})
	var intentos int
	if err := f.db.QueryRow(`select intentos from mensajes_fallidos where id=$1 and estado='pendiente'`, id).Scan(&intentos); err != nil || intentos != 4 {
		t.Fatalf("se esperaban 4 intentos registrados antes de la DLQ: intentos=%d err=%v", intentos, err)
	}
	if !f.colaVacia(t, eventosQueue) {
		t.Fatal("el mensaje siguió circulando por la cola principal")
	}
	if q, _ := f.cola(obs.Name); q.Messages != 1 {
		t.Fatalf("los reintentos internos de Beneficios llegaron %d veces a otro suscriptor (esperado 1)", q.Messages)
	}

	// Recuperación verificable: se corrige la causa y un administrador reintenta.
	if _, err := f.db.Exec(`insert into reglas_puntos_tratamientos (tratamiento_id,puntos) values ('tratamiento-sin-regla',80)`); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	s := &Servidor{almacen: f.almacen, programa: f.almacen, rabbitURL: f.rabbitURL, verificarSesion: func(_ context.Context, auth string) (identidadCliente, error) {
		if auth != "Bearer admin-l2" {
			return identidadCliente{}, fmt.Errorf("sesión inválida")
		}
		return identidadCliente{ID: "admin-l2", Email: "admin@melt.test", Roles: []string{"administrador"}}, nil
	}}
	router := crearRouter(s)
	listar := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/mensajes-fallidos", nil)
	req.Header.Set("Authorization", "Bearer admin-l2")
	router.ServeHTTP(listar, req)
	if listar.Code != http.StatusOK || !strings.Contains(listar.Body.String(), id) {
		t.Fatalf("el mensaje fallido no aparece para el administrador: %d %s", listar.Code, listar.Body.String())
	}
	reintentar := func() *httptest.ResponseRecorder {
		res := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/admin/mensajes-fallidos/"+url.PathEscape(id)+"/reintentar", nil)
		req.Header.Set("Authorization", "Bearer admin-l2")
		router.ServeHTTP(res, req)
		return res
	}
	// Dos administradores reintentan a la vez: sólo uno reencola.
	codigos := make([]int, 2)
	var wg sync.WaitGroup
	for i := range codigos {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codigos[i] = reintentar().Code
		}(i)
	}
	wg.Wait()
	aceptados := 0
	for _, code := range codigos {
		if code == http.StatusAccepted {
			aceptados++
		}
	}
	if aceptados != 1 {
		t.Fatalf("reintentos manuales aceptados=%d (%v)", aceptados, codigos)
	}
	f.esperar(t, "la acreditación recuperada", func() bool { return f.saldo(t, "usuario-dlq") == 80 })
	var estado string
	if err := f.db.QueryRow(`select estado from mensajes_fallidos where id=$1`, id).Scan(&estado); err != nil || estado != "reintentado" {
		t.Fatalf("estado del mensaje fallido=%s err=%v", estado, err)
	}
	if res := reintentar(); res.Code != http.StatusNotFound {
		t.Fatalf("un mensaje ya recuperado no debe reintentarse otra vez: %d", res.Code)
	}
	if q, _ := f.cola(obs.Name); q.Messages != 1 {
		t.Fatalf("la recuperación manual se reenvió a otros suscriptores: %d mensajes", q.Messages)
	}
	time.Sleep(200 * time.Millisecond)
	if saldo := f.saldo(t, "usuario-dlq"); saldo != 80 {
		t.Fatalf("saldo tras la recuperación=%d", saldo)
	}
}

func TestL2ConsumidorSeReconectaYNoPierdeMensajes(t *testing.T) {
	f := nuevoFixtureEventos(t)
	if _, err := f.db.Exec(`insert into reglas_puntos_tratamientos (tratamiento_id,puntos) values ('facial-reconexion',40)`); err != nil {
		t.Fatal(err)
	}
	f.iniciarConsumidor(t)
	f.publicar(t, "tratamiento.realizado", "evento-antes", eventoTratamiento("evento-antes", "usuario-reconexion", "facial-reconexion"))
	f.esperar(t, "el primer evento", func() bool { return f.saldo(t, "usuario-reconexion") == 40 })

	// El broker corta las conexiones del consumidor (reinicio, failover de red).
	// La API de administración publica las conexiones con algunos segundos de demora.
	var conexiones []struct {
		Name string `json:"name"`
	}
	f.esperar(t, "las conexiones del vhost en la API de administración", func() bool {
		conexiones = nil
		_ = json.Unmarshal(f.api(http.MethodGet, "/api/vhosts/"+url.PathEscape(f.vhost)+"/connections", ""), &conexiones)
		return len(conexiones) >= 2 // publicador de la prueba + consumidor
	})
	for _, c := range conexiones {
		f.api(http.MethodDelete, "/api/connections/"+url.PathEscape(c.Name), "")
	}
	// El publicador de la prueba también se cayó: se reabre.
	conn, err := amqp091.Dial(f.rabbitURL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if f.pub, err = conn.Channel(); err != nil {
		t.Fatal(err)
	}
	// Publicado mientras el consumidor está desconectado: la cola durable lo retiene.
	f.publicar(t, "tratamiento.realizado", "evento-durante-corte", eventoTratamiento("evento-durante-corte", "usuario-reconexion", "facial-reconexion"))
	f.esperar(t, "el evento publicado durante el corte", func() bool { return f.saldo(t, "usuario-reconexion") == 80 })
	if n := f.contar(t, `select count(*) from movimientos_puntos where usuario_id='usuario-reconexion'`); n != 2 {
		t.Fatalf("movimientos=%d", n)
	}
}

func TestL2CancelacionDuplicadaRestituyeElBeneficioUnaVez(t *testing.T) {
	f := nuevoFixtureEventos(t)
	if _, err := f.db.Exec(`insert into canjes (id,solicitud_id,usuario_id,recompensa,estado,reserva_id,precio_original,descuento,precio_final,aplicado_en)
		values ('canje-l2','solicitud-canje-l2','usuario-canje','{"id":"r-l2","nombre":"Descuento 10%","tipo":"descuento_porcentaje","costoPuntos":100,"porcentaje":10}','aplicado','carrito-l2',1000,100,900,now())`); err != nil {
		t.Fatal(err)
	}
	f.iniciarConsumidor(t)
	f.iniciarConsumidor(t)
	body, _ := json.Marshal(EventoDominio{EventID: "turno-cancelado:turno-l2", Tipo: "turno.cancelado", TurnoID: "turno-l2", UsuarioID: "usuario-canje", BeneficioID: "canje-l2", BeneficioReservaID: "carrito-l2", CanceladoEn: time.Now().UTC()})
	for i := 0; i < 6; i++ {
		f.publicar(t, "turno.cancelado", "turno-cancelado:turno-l2", body)
	}
	f.esperar(t, "la restitución del beneficio", func() bool {
		var estado string
		_ = f.db.QueryRow(`select estado from canjes where id='canje-l2'`).Scan(&estado)
		return estado == "disponible" && f.colaVacia(t, eventosQueue)
	})
	time.Sleep(300 * time.Millisecond)
	if n := f.contar(t, `select count(*) from movimientos_puntos where solicitud_id='restitucion:beneficio:carrito-l2'`); n != 1 {
		t.Fatalf("movimientos de restitución=%d, esperado 1", n)
	}
	if n := f.contar(t, `select count(*) from mensajes_fallidos`); n != 0 {
		t.Fatalf("copias duplicadas terminaron en la DLQ: %d", n)
	}
}
