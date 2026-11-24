package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/rabbitmq/amqp091-go"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const exchangeEventos = "melt.dominio"

type eventoOutbox struct {
	ID          string    `bson:"_id" json:"id"`
	Tipo        string    `bson:"tipo" json:"tipo"`
	RoutingKey  string    `bson:"routingKey" json:"routingKey"`
	Payload     bson.M    `bson:"payload" json:"payload"`
	CreadoEn    time.Time `bson:"creadoEn" json:"creadoEn"`
	PublicadoEn time.Time `bson:"publicadoEn,omitempty" json:"publicadoEn,omitempty"`
	Intentos    int       `bson:"intentos" json:"intentos"`
}

// publicarEventos confirma el envío al broker antes de marcar el evento como
// publicado. Si el proceso cae entre esos pasos, el consumidor deduplica el replay.
func (s *servidorReservas) publicarEventos(ctx context.Context) {
	if strings.TrimSpace(s.rabbitURL) == "" {
		return
	}
	ticker := time.NewTicker(1200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if _, err := s.publicarSiguienteEvento(ctx); err != nil {
			log.Printf("%v", err)
		}
	}
}

// publicarSiguienteEvento publica el evento pendiente más antiguo del outbox.
// Devuelve false sin error cuando no hay pendientes.
func (s *servidorReservas) publicarSiguienteEvento(ctx context.Context) (bool, error) {
	var item eventoOutbox
	findCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	var err error
	if s.pg != nil {
		item, err = s.pg.eventoPendiente(findCtx)
	} else {
		err = s.outbox.FindOne(findCtx, bson.M{"publicadoEn": bson.M{"$exists": false}}, options.FindOne().SetSort(bson.D{{Key: "creadoEn", Value: 1}})).Decode(&item)
	}
	cancel()
	if errors.Is(err, mongo.ErrNoDocuments) || sqlNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("reservas_outbox_read_failed: %w", err)
	}
	body, err := json.Marshal(item.Payload)
	if err != nil {
		return false, fmt.Errorf("reservas_outbox_encode_failed event=%s", item.ID)
	}
	publishCtx, stop := context.WithTimeout(ctx, 5*time.Second)
	conn, err := amqp091.DialConfig(s.rabbitURL, amqp091.Config{Heartbeat: 10 * time.Second, Locale: "en_US"})
	if err == nil {
		var ch *amqp091.Channel
		ch, err = conn.Channel()
		if err == nil {
			err = ch.ExchangeDeclare(exchangeEventos, "topic", true, false, false, false, nil)
			if err == nil {
				err = ch.Confirm(false)
			}
			if err == nil {
				confirms := ch.NotifyPublish(make(chan amqp091.Confirmation, 1))
				err = ch.PublishWithContext(publishCtx, exchangeEventos, item.RoutingKey, false, false, amqp091.Publishing{ContentType: "application/json", DeliveryMode: amqp091.Persistent, MessageId: item.ID, Type: item.Tipo, Timestamp: item.CreadoEn, Body: body})
				if err == nil {
					select {
					case confirm := <-confirms:
						if !confirm.Ack {
							err = errors.New("rabbitmq no confirmó el evento")
						}
					case <-publishCtx.Done():
						err = publishCtx.Err()
					}
				}
			}
			_ = ch.Close()
		}
		_ = conn.Close()
	}
	stop()
	if err != nil {
		if s.pg != nil {
			failCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			_ = s.pg.registrarFalloEvento(failCtx, item.ID, err.Error())
			cancel()
		}
		return false, fmt.Errorf("reservas_outbox_publish_failed event=%s: %w", item.ID, err)
	}
	markCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	if s.pg != nil {
		err = s.pg.publicarEvento(markCtx, item.ID, time.Now().UTC())
	} else {
		_, err = s.outbox.UpdateOne(markCtx, bson.M{"_id": item.ID, "publicadoEn": bson.M{"$exists": false}}, bson.M{"$set": bson.M{"publicadoEn": time.Now().UTC()}, "$inc": bson.M{"intentos": 1}})
	}
	cancel()
	if err != nil {
		return true, fmt.Errorf("reservas_outbox_mark_failed event=%s: %w", item.ID, err)
	}
	return true, nil
}
