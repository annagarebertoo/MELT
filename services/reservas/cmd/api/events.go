package main

import (
	"context"
	"encoding/json"
	"errors"
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
	ID          string    `bson:"_id"`
	Tipo        string    `bson:"tipo"`
	RoutingKey  string    `bson:"routingKey"`
	Payload     bson.M    `bson:"payload"`
	CreadoEn    time.Time `bson:"creadoEn"`
	PublicadoEn time.Time `bson:"publicadoEn,omitempty"`
	Intentos    int       `bson:"intentos"`
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
		var item eventoOutbox
		findCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		err := s.outbox.FindOne(findCtx, bson.M{"publicadoEn": bson.M{"$exists": false}}, options.FindOne().SetSort(bson.D{{Key: "creadoEn", Value: 1}})).Decode(&item)
		cancel()
		if errors.Is(err, mongo.ErrNoDocuments) {
			continue
		}
		if err != nil {
			log.Printf("reservas_outbox_read_failed: %v", err)
			continue
		}
		body, err := json.Marshal(item.Payload)
		if err != nil {
			log.Printf("reservas_outbox_encode_failed event=%s", item.ID)
			continue
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
			log.Printf("reservas_outbox_publish_failed event=%s: %v", item.ID, err)
			continue
		}
		markCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		_, err = s.outbox.UpdateOne(markCtx, bson.M{"_id": item.ID, "publicadoEn": bson.M{"$exists": false}}, bson.M{"$set": bson.M{"publicadoEn": time.Now().UTC()}, "$inc": bson.M{"intentos": 1}})
		cancel()
		if err != nil {
			log.Printf("reservas_outbox_mark_failed event=%s: %v", item.ID, err)
		}
	}
}
