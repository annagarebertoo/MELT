package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readconcern"
	"go.mongodb.org/mongo-driver/v2/mongo/writeconcern"
)

var (
	errTurnoNoEncontrado = errors.New("turno no encontrado")
	errProfesionalAjeno  = errors.New("el turno pertenece a otro profesional")
	errTurnoNoConfirmado = errors.New("el turno no está confirmado")
)

func (s *servidorReservas) completarTratamiento(c *gin.Context) {
	identidad := c.MustGet("identidad").(identidad)
	if !tieneRol(identidad, "profesional") {
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "PERMISOS_INSUFICIENTES", "message": "Solo el profesional asignado puede confirmar que realizó el tratamiento."}})
		return
	}
	profesionalID, status, err := s.resolverPerfilProfesional(c.Request.Context(), identidad.ID)
	if err != nil {
		c.JSON(status, gin.H{"error": gin.H{"code": "PERFIL_PROFESIONAL_NO_DISPONIBLE", "message": err.Error()}})
		return
	}
	if s.mongo == nil || s.outbox == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "EVENTOS_NO_DISPONIBLES", "message": "No se pudo registrar la realización de forma segura."}})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 8*time.Second)
	defer cancel()
	item, repetido, err := s.registrarRealizacion(ctx, strings.TrimSpace(c.Param("id")), profesionalID)
	switch {
	case errors.Is(err, errTurnoNoEncontrado):
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"code": "TURNO_NO_ENCONTRADO", "message": "No encontramos ese turno."}})
	case errors.Is(err, errProfesionalAjeno):
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "TURNO_AJENO", "message": "Solo el profesional asignado puede confirmar la realización."}})
	case errors.Is(err, errTurnoNoConfirmado):
		c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code": "TURNO_NO_CONFIRMADO", "message": "Solo se puede completar un turno confirmado."}})
	case err != nil:
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "REALIZACION_NO_REGISTRADA", "message": "No se pudo guardar la realización; podés reintentar."}})
	default:
		status := http.StatusAccepted
		message := "Realización registrada. La acreditación de puntos se procesará automáticamente."
		if repetido {
			status = http.StatusOK
			message = "La realización ya estaba registrada; no se duplicó el evento."
		}
		c.JSON(status, gin.H{"turno": item, "acreditacion": gin.H{"estado": "pendiente", "mensaje": message}})
	}
}

func (s *servidorReservas) registrarRealizacion(ctx context.Context, turnoID, profesionalID string) (turno, bool, error) {
	if turnoID == "" || profesionalID == "" {
		return turno{}, false, errors.New("identidad incompleta")
	}
	session, err := s.mongo.StartSession()
	if err != nil {
		return turno{}, false, err
	}
	defer session.EndSession(ctx)
	var resultado turno
	repetido := false
	_, err = session.WithTransaction(ctx, func(tx context.Context) (any, error) {
		var item turno
		if e := s.turnos.FindOne(tx, bson.M{"_id": turnoID}).Decode(&item); e != nil {
			if errors.Is(e, mongo.ErrNoDocuments) {
				return nil, errTurnoNoEncontrado
			}
			return nil, e
		}
		if item.ProfesionalID != profesionalID {
			return nil, errProfesionalAjeno
		}
		if item.Estado == "completado" {
			resultado, repetido = item, true
			return item, nil
		}
		if item.Estado != "confirmado" {
			return nil, errTurnoNoConfirmado
		}
		now := time.Now().UTC().Truncate(time.Millisecond)
		eventID := "tratamiento-realizado:" + item.ID
		res, e := s.turnos.UpdateOne(tx, bson.M{"_id": item.ID, "profesionalId": profesionalID, "estado": "confirmado"}, bson.M{"$set": bson.M{"estado": "completado", "tratamientoRealizadoEn": now, "tratamientoRealizadoPor": profesionalID}})
		if e != nil {
			return nil, e
		}
		if res.MatchedCount != 1 {
			return nil, errTurnoNoConfirmado
		}
		if _, e = s.bloqueos.DeleteMany(tx, bson.M{"turnoId": item.ID}); e != nil {
			return nil, e
		}
		item.Estado, item.TratamientoRealizadoPor = "completado", profesionalID
		item.TratamientoRealizadoEn = &now
		payload := bson.M{"eventId": eventID, "turnoId": item.ID, "usuarioId": item.ClienteID, "profesionalId": item.ProfesionalID, "tratamientoId": item.TratamientoID, "realizadoEn": now, "beneficioCanjeId": item.BeneficioCanjeID, "beneficioReservaId": item.BeneficioReservaID}
		if _, e = s.outbox.InsertOne(tx, eventoOutbox{ID: eventID, Tipo: "tratamiento.realizado", RoutingKey: "tratamiento.realizado", Payload: payload, CreadoEn: now}); e != nil {
			return nil, e
		}
		resultado = item
		return item, nil
	}, options.Transaction().SetReadConcern(readconcern.Snapshot()).SetWriteConcern(writeconcern.Majority()))
	if err != nil {
		return turno{}, false, err
	}
	return resultado, repetido, nil
}

func (s *servidorReservas) cancelarConEvento(ctx context.Context, t turno, devolucion float64, estadoDevolucion string) error {
	if s.mongo == nil || s.outbox == nil {
		return errors.New("outbox no configurado")
	}
	session, err := s.mongo.StartSession()
	if err != nil {
		return err
	}
	defer session.EndSession(ctx)
	_, err = session.WithTransaction(ctx, func(tx context.Context) (any, error) {
		res, e := s.turnos.UpdateOne(tx, bson.M{"_id": t.ID, "estado": "confirmado"}, bson.M{"$set": bson.M{"estado": "cancelado", "importeDevolucion": devolucion, "estadoDevolucion": estadoDevolucion}})
		if e != nil {
			return nil, e
		}
		if res.MatchedCount != 1 {
			return nil, errTurnoNoConfirmado
		}
		if _, e = s.bloqueos.DeleteMany(tx, bson.M{"turnoId": t.ID}); e != nil {
			return nil, e
		}
		now := time.Now().UTC().Truncate(time.Millisecond)
		eventID := "turno-cancelado:" + t.ID
		payload := bson.M{"eventId": eventID, "turnoId": t.ID, "usuarioId": t.ClienteID, "profesionalId": t.ProfesionalID, "tratamientoId": t.TratamientoID, "canceladoEn": now, "beneficioCanjeId": t.BeneficioCanjeID, "beneficioReservaId": t.BeneficioReservaID}
		_, e = s.outbox.InsertOne(tx, eventoOutbox{ID: eventID, Tipo: "turno.cancelado", RoutingKey: "turno.cancelado", Payload: payload, CreadoEn: now})
		return nil, e
	}, options.Transaction().SetReadConcern(readconcern.Snapshot()).SetWriteConcern(writeconcern.Majority()))
	return err
}
