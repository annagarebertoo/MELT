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
	errInasistenciaTurnoNoEncontrado = errors.New("TURNO_NO_ENCONTRADO")
	errInasistenciaTurnoAjeno        = errors.New("TURNO_AJENO")
	errInasistenciaNoConfirmado      = errors.New("TURNO_NO_CONFIRMADO")
	errInasistenciaFutura            = errors.New("TURNO_AUN_NO_FINALIZADO")
)

func (s *servidorReservas) registrarNoAsistencia(c *gin.Context) {
	identidad := c.MustGet("identidad").(identidad)
	profesionalID, status, err := s.resolverPerfilProfesional(c.Request.Context(), identidad.ID)
	if err != nil {
		c.JSON(status, gin.H{"error": gin.H{"code": "PERFIL_PROFESIONAL_NO_DISPONIBLE", "message": err.Error()}})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 8*time.Second)
	defer cancel()
	item, repetido, err := s.registrarInasistencia(ctx, strings.TrimSpace(c.Param("id")), profesionalID)
	switch {
	case errors.Is(err, errInasistenciaTurnoNoEncontrado):
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"code": err.Error(), "message": "No encontramos ese turno."}})
	case errors.Is(err, errInasistenciaTurnoAjeno):
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"code": err.Error(), "message": "Solo el profesional asignado puede registrar la inasistencia."}})
	case errors.Is(err, errInasistenciaNoConfirmado):
		c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code": err.Error(), "message": "Solo se puede registrar la inasistencia de un turno confirmado."}})
	case errors.Is(err, errInasistenciaFutura):
		c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code": err.Error(), "message": "La inasistencia se puede registrar cuando termine el horario del turno."}})
	case err != nil:
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "INASISTENCIA_NO_REGISTRADA", "message": "No se pudo guardar la inasistencia; podés reintentar."}})
	default:
		message := "Inasistencia registrada. No se generaron puntos por tratamiento realizado."
		if repetido {
			message = "La inasistencia ya estaba registrada."
		}
		c.JSON(http.StatusOK, gin.H{"turno": item, "mensaje": message})
	}
}

func (s *servidorReservas) registrarInasistencia(ctx context.Context, turnoID, profesionalID string) (turno, bool, error) {
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
				return nil, errInasistenciaTurnoNoEncontrado
			}
			return nil, e
		}
		if item.ProfesionalID != profesionalID {
			return nil, errInasistenciaTurnoAjeno
		}
		if item.Estado == "no_asistio" {
			resultado, repetido = item, true
			return item, nil
		}
		if item.Estado != "confirmado" {
			return nil, errInasistenciaNoConfirmado
		}
		now := time.Now().UTC().Truncate(time.Millisecond)
		if item.Fin.After(now) {
			return nil, errInasistenciaFutura
		}
		res, e := s.turnos.UpdateOne(tx, bson.M{"_id": item.ID, "profesionalId": profesionalID, "estado": "confirmado"}, bson.M{"$set": bson.M{"estado": "no_asistio", "noAsistioEn": now, "noAsistioPor": profesionalID}})
		if e != nil {
			return nil, e
		}
		if res.MatchedCount != 1 {
			return nil, errInasistenciaNoConfirmado
		}
		if _, e = s.bloqueos.DeleteMany(tx, bson.M{"turnoId": item.ID}); e != nil {
			return nil, e
		}
		item.Estado, item.NoAsistioPor, item.NoAsistioEn = "no_asistio", profesionalID, &now
		resultado = item
		return item, nil
	}, options.Transaction().SetReadConcern(readconcern.Snapshot()).SetWriteConcern(writeconcern.Majority()))
	if err != nil {
		return turno{}, false, err
	}
	return resultado, repetido, nil
}
