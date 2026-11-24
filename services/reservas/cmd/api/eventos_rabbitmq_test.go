package main

// L2 — Publicación del outbox de Reservas contra RabbitMQ y PostgreSQL descartables.

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/rabbitmq/amqp091-go"
)

// colaObservadora declara el exchange de dominio y una cola exclusiva que
// recibe todos sus eventos, como lo haría cualquier consumidor suscripto.
func colaObservadora(t *testing.T, rabbitURL string) (*amqp091.Channel, string) {
	t.Helper()
	conn, err := amqp091.Dial(rabbitURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	ch, err := conn.Channel()
	if err != nil {
		t.Fatal(err)
	}
	if err := ch.ExchangeDeclare(exchangeEventos, "topic", true, false, false, false, nil); err != nil {
		t.Fatal(err)
	}
	q, err := ch.QueueDeclare("", false, true, true, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ch.QueueBind(q.Name, "#", exchangeEventos, false, nil); err != nil {
		t.Fatal(err)
	}
	return ch, q.Name
}

func recibirMensajes(t *testing.T, ch *amqp091.Channel, cola string, esperados int) []amqp091.Delivery {
	t.Helper()
	var out []amqp091.Delivery
	limite := time.Now().Add(5 * time.Second)
	for len(out) < esperados && time.Now().Before(limite) {
		msg, ok, err := ch.Get(cola, true)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		out = append(out, msg)
	}
	if len(out) != esperados {
		t.Fatalf("mensajes recibidos=%d, esperado %d", len(out), esperados)
	}
	// Ningún mensaje extra debe llegar.
	time.Sleep(200 * time.Millisecond)
	if _, ok, _ := ch.Get(cola, true); ok {
		t.Fatal("llegó un mensaje duplicado o inesperado")
	}
	return out
}

func vaciarOutbox(t *testing.T, s *servidorReservas) int {
	t.Helper()
	publicados := 0
	for i := 0; i < 50; i++ {
		ok, err := s.publicarSiguienteEvento(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			return publicados
		}
		publicados++
	}
	t.Fatal("el outbox no se vació")
	return publicados
}

func TestL2OutboxPublicaEventoDeNegocioConfirmadoYEnOrden(t *testing.T) {
	f := nuevoFixtureConsistencia(t)
	f.s.rabbitURL = rabbitMQAislado(t)
	ch, cola := colaObservadora(t, f.s.rabbitURL)
	ctx := context.Background()
	ahora := time.Now().UTC()
	ids := []string{}
	for i, offset := range []time.Duration{-5 * time.Hour, -3 * time.Hour} {
		id := []string{"turno-l2-a", "turno-l2-b"}[i]
		if _, err := f.db.Exec(`insert into turnos (id,cliente_id,profesional_id,tratamiento_id,inicio,fin,estado) values ($1,'cliente-l2',$2,$3,$4,$5,'confirmado')`, id, profesionalL1, facialL1, ahora.Add(offset), ahora.Add(offset+time.Hour)); err != nil {
			t.Fatal(err)
		}
		if _, _, err := f.s.registrarRealizacion(ctx, id, profesionalL1); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, "tratamiento-realizado:"+id)
		time.Sleep(5 * time.Millisecond)
	}
	if n := vaciarOutbox(t, f.s); n != 2 {
		t.Fatalf("eventos publicados=%d", n)
	}
	mensajes := recibirMensajes(t, ch, cola, 2)
	for i, msg := range mensajes {
		if msg.MessageId != ids[i] || msg.Type != "tratamiento.realizado" || msg.RoutingKey != "tratamiento.realizado" || msg.DeliveryMode != amqp091.Persistent {
			t.Fatalf("mensaje %d inválido o fuera de orden: id=%s type=%s key=%s mode=%d", i, msg.MessageId, msg.Type, msg.RoutingKey, msg.DeliveryMode)
		}
		var payload map[string]any
		if err := json.Unmarshal(msg.Body, &payload); err != nil || payload["eventId"] != ids[i] || payload["usuarioId"] != "cliente-l2" || payload["tratamientoId"] != facialL1 {
			t.Fatalf("payload inválido: %s", msg.Body)
		}
	}
	if n := f.contar(t, `select count(*) from outbox_eventos where publicado_en is null`); n != 0 {
		t.Fatalf("quedaron %d eventos sin marcar como publicados", n)
	}
	// Repetir la realización no genera otro evento.
	if _, repetido, err := f.s.registrarRealizacion(ctx, "turno-l2-a", profesionalL1); err != nil || !repetido {
		t.Fatalf("realización repetida: repetido=%v err=%v", repetido, err)
	}
	if n := vaciarOutbox(t, f.s); n != 0 {
		t.Fatalf("una realización repetida publicó %d eventos", n)
	}
}

func TestL2OutboxRetieneEventoSiElBrokerNoEstaYLoRecupera(t *testing.T) {
	f := nuevoFixtureConsistencia(t)
	rabbitURL := rabbitMQAislado(t)
	ch, cola := colaObservadora(t, rabbitURL)
	if err := f.s.pg.insertarEvento(context.Background(), eventoOutbox{ID: "turno-cancelado:l2-caida", Tipo: "turno.cancelado", RoutingKey: "turno.cancelado", Payload: map[string]any{"eventId": "turno-cancelado:l2-caida", "turnoId": "l2-caida"}, CreadoEn: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	f.s.rabbitURL = "amqp://guest:guest@127.0.0.1:1/" // broker caído
	for i := 0; i < 3; i++ {
		if ok, err := f.s.publicarSiguienteEvento(context.Background()); err == nil || ok {
			t.Fatalf("intento %d sin broker debía fallar: ok=%v err=%v", i, ok, err)
		}
	}
	var intentos int
	var ultimoError string
	if err := f.db.QueryRow(`select intentos, coalesce(ultimo_error,'') from outbox_eventos where id='turno-cancelado:l2-caida' and publicado_en is null`).Scan(&intentos, &ultimoError); err != nil {
		t.Fatalf("el evento se perdió o se marcó como publicado sin broker: %v", err)
	}
	if intentos != 3 || ultimoError == "" {
		t.Fatalf("la falla no quedó registrada: intentos=%d error=%q", intentos, ultimoError)
	}
	f.s.rabbitURL = rabbitURL // el broker vuelve
	if n := vaciarOutbox(t, f.s); n != 1 {
		t.Fatalf("eventos publicados tras la recuperación=%d", n)
	}
	mensajes := recibirMensajes(t, ch, cola, 1)
	if mensajes[0].MessageId != "turno-cancelado:l2-caida" {
		t.Fatalf("mensaje recuperado inesperado: %s", mensajes[0].MessageId)
	}
}

func TestL2ReplayDelOutboxConservaIdentidadParaDeduplicar(t *testing.T) {
	f := nuevoFixtureConsistencia(t)
	f.s.rabbitURL = rabbitMQAislado(t)
	ch, cola := colaObservadora(t, f.s.rabbitURL)
	id := "tratamiento-realizado:l2-replay"
	if err := f.s.pg.insertarEvento(context.Background(), eventoOutbox{ID: id, Tipo: "tratamiento.realizado", RoutingKey: "tratamiento.realizado", Payload: map[string]any{"eventId": id, "turnoId": "l2-replay"}, CreadoEn: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	vaciarOutbox(t, f.s)
	// Caída simulada entre la confirmación del broker y la marca en PostgreSQL.
	if _, err := f.db.Exec(`update outbox_eventos set publicado_en=null where id=$1`, id); err != nil {
		t.Fatal(err)
	}
	vaciarOutbox(t, f.s)
	mensajes := recibirMensajes(t, ch, cola, 2)
	for _, msg := range mensajes {
		var payload map[string]any
		_ = json.Unmarshal(msg.Body, &payload)
		if msg.MessageId != id || payload["eventId"] != id {
			t.Fatalf("el replay cambió la identidad del evento: messageId=%s body=%s", msg.MessageId, msg.Body)
		}
	}
}
