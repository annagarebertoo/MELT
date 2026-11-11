package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/rabbitmq/amqp091-go"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const (
	eventosExchange  = "melt.dominio"
	fallidosExchange = "melt.beneficios.fallidos"
	eventosQueue     = "melt.beneficios.eventos"
	fallidosQueue    = "melt.beneficios.eventos.fallidos"
)

type EventoDominio struct {
	EventID            string    `json:"eventId" bson:"eventId"`
	Tipo               string    `json:"tipo" bson:"tipo"`
	TurnoID            string    `json:"turnoId" bson:"turnoId"`
	UsuarioID          string    `json:"usuarioId" bson:"usuarioId"`
	ProfesionalID      string    `json:"profesionalId" bson:"profesionalId"`
	TratamientoID      string    `json:"tratamientoId" bson:"tratamientoId"`
	BeneficioID        string    `json:"beneficioCanjeId" bson:"beneficioCanjeId"`
	BeneficioReservaID string    `json:"beneficioReservaId" bson:"beneficioReservaId"`
	RealizadoEn        time.Time `json:"realizadoEn,omitempty" bson:"realizadoEn,omitempty"`
	CanceladoEn        time.Time `json:"canceladoEn,omitempty" bson:"canceladoEn,omitempty"`
}

type mensajeFallido struct {
	ID            string        `bson:"_id" json:"id"`
	Evento        EventoDominio `bson:"evento" json:"evento"`
	Error         string        `bson:"error" json:"error"`
	Intentos      int           `bson:"intentos" json:"intentos"`
	Estado        string        `bson:"estado" json:"estado"`
	CreadoEn      time.Time     `bson:"creadoEn" json:"creadoEn"`
	ReintentadoEn *time.Time    `bson:"reintentadoEn,omitempty" json:"reintentadoEn,omitempty"`
}

type consumidorPrograma struct {
	url     string
	almacen *almacenMongo
}

func (w *consumidorPrograma) run(ctx context.Context) {
	for ctx.Err() == nil {
		if err := w.consume(ctx); err != nil && ctx.Err() == nil {
			log.Printf("beneficios_rabbitmq_reconnect: %v", err)
			time.Sleep(2 * time.Second)
		}
	}
}

func (w *consumidorPrograma) consume(ctx context.Context) error {
	conn, err := amqp091.Dial(w.url)
	if err != nil {
		return err
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()
	if err = ch.ExchangeDeclare(eventosExchange, "topic", true, false, false, false, nil); err != nil {
		return err
	}
	if err = ch.ExchangeDeclare(fallidosExchange, "direct", true, false, false, false, nil); err != nil {
		return err
	}
	dead, err := ch.QueueDeclare(fallidosQueue, true, false, false, false, nil)
	if err != nil {
		return err
	}
	if err = ch.QueueBind(dead.Name, "event", fallidosExchange, false, nil); err != nil {
		return err
	}
	queue, err := ch.QueueDeclare(eventosQueue, true, false, false, false, amqp091.Table{"x-dead-letter-exchange": fallidosExchange, "x-dead-letter-routing-key": "event"})
	if err != nil {
		return err
	}
	for _, key := range []string{"tratamiento.realizado", "turno.cancelado"} {
		if err = ch.QueueBind(queue.Name, key, eventosExchange, false, nil); err != nil {
			return err
		}
	}
	if err = ch.Qos(1, 0, false); err != nil {
		return err
	}
	if err = ch.Confirm(false); err != nil {
		return err
	}
	deliveries, err := ch.Consume(queue.Name, "", false, false, false, false, nil)
	if err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case delivery, ok := <-deliveries:
			if !ok {
				return errors.New("rabbitmq cerró el canal de consumo")
			}
			w.handle(ctx, ch, delivery)
		}
	}
}

func (w *consumidorPrograma) handle(ctx context.Context, ch *amqp091.Channel, delivery amqp091.Delivery) {
	var event EventoDominio
	if err := json.Unmarshal(delivery.Body, &event); err != nil || strings.TrimSpace(event.EventID) == "" {
		_ = w.failPermanently(ctx, ch, delivery, event, "el evento no cumple el esquema esperado", 1)
		return
	}
	if event.Tipo == "" {
		event.Tipo = delivery.Type
	}
	processCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	err := w.process(processCtx, event)
	cancel()
	if err == nil {
		_ = delivery.Ack(false)
		return
	}
	attempts := retryCount(delivery.Headers) + 1
	if attempts >= 4 {
		_ = w.failPermanently(ctx, ch, delivery, event, err.Error(), attempts)
		return
	}
	delay := []time.Duration{time.Second, 3 * time.Second, 8 * time.Second}[attempts-1]
	select {
	case <-ctx.Done():
		_ = delivery.Nack(false, true)
		return
	case <-time.After(delay):
	}
	if err = publishConfirmed(ch, eventosExchange, delivery.RoutingKey, delivery.Type, delivery.MessageId, delivery.Body, amqp091.Table{"x-melt-intentos": int32(attempts)}); err != nil {
		_ = delivery.Nack(false, true)
		return
	}
	_ = delivery.Ack(false)
}

func (w *consumidorPrograma) process(ctx context.Context, event EventoDominio) error {
	switch event.Tipo {
	case "tratamiento.realizado":
		return w.almacen.procesarEventoTratamiento(ctx, event.EventID, event.UsuarioID, event.TratamientoID)
	case "turno.cancelado":
		if event.BeneficioID == "" {
			return nil
		}
		reservaID := event.BeneficioReservaID
		if reservaID == "" {
			reservaID = event.TurnoID
		}
		_, err := w.almacen.actualizarCanje(ctx, event.BeneficioID, event.UsuarioID, reservaID, "restituir")
		return err
	default:
		return fmt.Errorf("tipo de evento no admitido: %s", event.Tipo)
	}
}

func retryCount(headers amqp091.Table) int {
	if headers == nil {
		return 0
	}
	switch value := headers["x-melt-intentos"].(type) {
	case int32:
		return int(value)
	case int64:
		return int(value)
	case int:
		return value
	case string:
		n, _ := strconv.Atoi(value)
		return n
	default:
		return 0
	}
}

func (w *consumidorPrograma) failPermanently(ctx context.Context, ch *amqp091.Channel, delivery amqp091.Delivery, event EventoDominio, reason string, attempts int) error {
	if event.EventID == "" {
		event.EventID = delivery.MessageId
	}
	if event.EventID == "" {
		event.EventID = bson.NewObjectID().Hex()
	}
	if event.Tipo == "" {
		event.Tipo = delivery.Type
	}
	failure := mensajeFallido{ID: event.EventID, Evento: event, Error: reason, Intentos: attempts, Estado: "pendiente", CreadoEn: time.Now().UTC().Truncate(time.Millisecond)}
	_, err := w.almacen.fallidos.UpdateOne(ctx, bson.M{"_id": failure.ID}, bson.M{"$set": bson.M{"evento": failure.Evento, "error": failure.Error, "intentos": failure.Intentos, "estado": failure.Estado}, "$setOnInsert": bson.M{"creadoEn": failure.CreadoEn}}, options.UpdateOne().SetUpsert(true))
	if err != nil {
		_ = delivery.Nack(false, true)
		return err
	}
	body, err := json.Marshal(event)
	if err != nil {
		_ = delivery.Nack(false, true)
		return err
	}
	if err = publishConfirmed(ch, fallidosExchange, "event", event.Tipo, event.EventID, body, amqp091.Table{"x-melt-error": reason, "x-melt-intentos": int32(attempts)}); err != nil {
		_ = delivery.Nack(false, true)
		return err
	}
	_ = delivery.Ack(false)
	return nil
}

func publishConfirmed(ch *amqp091.Channel, exchange, key, typ, messageID string, body []byte, headers amqp091.Table) error {
	confirmations := ch.NotifyPublish(make(chan amqp091.Confirmation, 1))
	if err := ch.Publish(exchange, key, false, false, amqp091.Publishing{ContentType: "application/json", DeliveryMode: amqp091.Persistent, Type: typ, MessageId: messageID, Headers: headers, Timestamp: time.Now().UTC(), Body: body}); err != nil {
		return err
	}
	select {
	case confirm := <-confirmations:
		if !confirm.Ack {
			return errors.New("rabbitmq no confirmó la publicación")
		}
		return nil
	case <-time.After(4 * time.Second):
		return errors.New("tiempo agotado esperando confirmación de rabbitmq")
	}
}

func reintentarEvento(ctx context.Context, rabbitURL string, event EventoDominio) error {
	conn, err := amqp091.Dial(rabbitURL)
	if err != nil {
		return err
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()
	if err = ch.ExchangeDeclare(eventosExchange, "topic", true, false, false, false, nil); err != nil {
		return err
	}
	if err = ch.Confirm(false); err != nil {
		return err
	}
	body, err := json.Marshal(event)
	if err != nil {
		return err
	}
	key := event.Tipo
	return publishConfirmed(ch, eventosExchange, key, event.Tipo, event.EventID, body, amqp091.Table{})
}
