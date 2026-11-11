package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readconcern"
	"go.mongodb.org/mongo-driver/v2/mongo/writeconcern"
)

var (
	errRecompensaNoExiste = errors.New("recompensa inexistente")
	errCanjeNoDisponible  = errors.New("canje no disponible")
	errReglaNoExiste      = errors.New("regla de puntos inexistente")
)

type Recompensa struct {
	ID            string    `bson:"_id" json:"id"`
	Nombre        string    `bson:"nombre" json:"nombre"`
	Descripcion   string    `bson:"descripcion" json:"descripcion"`
	Tipo          string    `bson:"tipo" json:"tipo"` // descuento_porcentaje | tratamiento_gratis
	CostoPuntos   int64     `bson:"costoPuntos" json:"costoPuntos"`
	Porcentaje    int       `bson:"porcentaje,omitempty" json:"porcentaje,omitempty"`
	TratamientoID string    `bson:"tratamientoId,omitempty" json:"tratamientoId,omitempty"`
	Activa        bool      `bson:"activa" json:"activa"`
	ActualizadaEn time.Time `bson:"actualizadaEn" json:"actualizadaEn"`
}

type ReglaTratamiento struct {
	TratamientoID string    `bson:"_id" json:"tratamientoId"`
	Puntos        int64     `bson:"puntos" json:"puntos"`
	ActualizadaEn time.Time `bson:"actualizadaEn" json:"actualizadaEn"`
}

type Canje struct {
	ID             string     `bson:"_id" json:"id"`
	SolicitudID    string     `bson:"solicitudId" json:"solicitudId"`
	UsuarioID      string     `bson:"usuarioId" json:"usuarioId"`
	Recompensa     Recompensa `bson:"recompensa" json:"recompensa"`
	Estado         string     `bson:"estado" json:"estado"` // disponible | reservado | aplicado
	ReservaID      string     `bson:"reservaId,omitempty" json:"reservaId,omitempty"`
	PrecioOriginal float64    `bson:"precioOriginal,omitempty" json:"precioOriginal,omitempty"`
	Descuento      float64    `bson:"descuento,omitempty" json:"descuento,omitempty"`
	PrecioFinal    float64    `bson:"precioFinal,omitempty" json:"precioFinal,omitempty"`
	CreadoEn       time.Time  `bson:"creadoEn" json:"creadoEn"`
	AplicadoEn     *time.Time `bson:"aplicadoEn,omitempty" json:"aplicadoEn,omitempty"`
}

type programaStore interface {
	listarRecompensas(context.Context, bool) ([]Recompensa, error)
	guardarRecompensa(context.Context, Recompensa) error
	guardarRegla(context.Context, ReglaTratamiento) error
	listarReglas(context.Context) ([]ReglaTratamiento, error)
	canjear(context.Context, string, string, string) (Canje, bool, error)
	listarCanjes(context.Context, string) ([]Canje, error)
	reservarCanje(context.Context, string, string, string, string, float64) (Canje, error)
	actualizarCanje(context.Context, string, string, string, string) (Canje, error)
	procesarEventoTratamiento(context.Context, string, string, string) error
}

func (a *almacenMongo) listarRecompensas(ctx context.Context, activas bool) ([]Recompensa, error) {
	filtro := bson.M{}
	if activas {
		filtro["activa"] = true
	}
	cur, err := a.recompensas.Find(ctx, filtro, options.Find().SetSort(bson.D{{Key: "nombre", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	items := []Recompensa{}
	return items, cur.All(ctx, &items)
}

func recompensaValida(r Recompensa) bool {
	r.ID, r.Nombre, r.Descripcion, r.Tipo = strings.TrimSpace(r.ID), strings.TrimSpace(r.Nombre), strings.TrimSpace(r.Descripcion), strings.TrimSpace(r.Tipo)
	if r.ID == "" || utf8.RuneCountInString(r.ID) > 128 || r.Nombre == "" || utf8.RuneCountInString(r.Nombre) > 120 || utf8.RuneCountInString(r.Descripcion) > 500 || r.CostoPuntos <= 0 || r.CostoPuntos > maxPuntos {
		return false
	}
	switch r.Tipo {
	case "descuento_porcentaje":
		return r.Porcentaje >= 1 && r.Porcentaje <= 100 && r.TratamientoID == ""
	case "tratamiento_gratis":
		return strings.TrimSpace(r.TratamientoID) != "" && r.Porcentaje == 0
	default:
		return false
	}
}

func (a *almacenMongo) guardarRecompensa(ctx context.Context, r Recompensa) error {
	r.ID, r.Nombre, r.Descripcion, r.TratamientoID = strings.TrimSpace(r.ID), strings.TrimSpace(r.Nombre), strings.TrimSpace(r.Descripcion), strings.TrimSpace(r.TratamientoID)
	r.ActualizadaEn = time.Now().UTC().Truncate(time.Millisecond)
	_, err := a.recompensas.ReplaceOne(ctx, bson.M{"_id": r.ID}, r, options.Replace().SetUpsert(true))
	return err
}

func (a *almacenMongo) guardarRegla(ctx context.Context, r ReglaTratamiento) error {
	r.TratamientoID = strings.TrimSpace(r.TratamientoID)
	if r.TratamientoID == "" || r.Puntos < 0 || r.Puntos > maxPuntos {
		return errors.New("regla inválida")
	}
	r.ActualizadaEn = time.Now().UTC().Truncate(time.Millisecond)
	_, err := a.reglasTratamientos.ReplaceOne(ctx, bson.M{"_id": r.TratamientoID}, r, options.Replace().SetUpsert(true))
	return err
}

func (a *almacenMongo) listarReglas(ctx context.Context) ([]ReglaTratamiento, error) {
	cur, err := a.reglasTratamientos.Find(ctx, bson.M{}, options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	items := []ReglaTratamiento{}
	return items, cur.All(ctx, &items)
}

func (a *almacenMongo) canjear(ctx context.Context, userID, rewardID, requestID string) (Canje, bool, error) {
	userID, rewardID, requestID = strings.TrimSpace(userID), strings.TrimSpace(rewardID), strings.TrimSpace(requestID)
	if userID == "" || rewardID == "" || requestID == "" || len(requestID) > 128 {
		return Canje{}, false, errors.New("solicitud inválida")
	}
	ledgerID := "canje:" + requestID
	var previo Movimiento
	if err := a.movimientos.FindOne(ctx, bson.M{"_id": ledgerID}).Decode(&previo); err == nil {
		if previo.UsuarioID != userID || previo.Descripcion != rewardID {
			return Canje{}, false, errConflicto
		}
		var existente Canje
		err = a.canjes.FindOne(ctx, bson.M{"solicitudId": requestID}).Decode(&existente)
		return existente, true, err
	} else if !errors.Is(err, mongo.ErrNoDocuments) {
		return Canje{}, false, err
	}
	var result Canje
	session, err := a.cliente.StartSession()
	if err != nil {
		return result, false, err
	}
	defer session.EndSession(ctx)
	_, err = session.WithTransaction(ctx, func(tx context.Context) (any, error) {
		var reward Recompensa
		if e := a.recompensas.FindOne(tx, bson.M{"_id": rewardID, "activa": true}).Decode(&reward); e != nil {
			if errors.Is(e, mongo.ErrNoDocuments) {
				return nil, errRecompensaNoExiste
			}
			return nil, e
		}
		now := time.Now().UTC().Truncate(time.Millisecond)
		result = Canje{ID: bson.NewObjectID().Hex(), SolicitudID: requestID, UsuarioID: userID, Recompensa: reward, Estado: "disponible", CreadoEn: now}
		if _, e := a.movimientos.InsertOne(tx, Movimiento{SolicitudID: ledgerID, UsuarioID: userID, Puntos: -reward.CostoPuntos, Tipo: "canje", CreadoEn: now, Descripcion: rewardID, Estado: "confirmado"}); e != nil {
			return nil, e
		}
		decrement, e := a.registros.UpdateOne(tx, bson.M{"usuarioId": userID, "saldoPuntos": bson.M{"$gte": reward.CostoPuntos}}, bson.M{"$inc": bson.M{"saldoPuntos": -reward.CostoPuntos}})
		if e != nil {
			return nil, e
		}
		if decrement.MatchedCount != 1 {
			return nil, errSaldoInsuficiente
		}
		var cuenta Cuenta
		if e = a.registros.FindOne(tx, bson.M{"usuarioId": userID}).Decode(&cuenta); e != nil {
			return nil, e
		}
		if cuenta.SaldoPuntos < 0 {
			return nil, errors.New("saldo insuficiente")
		}
		if _, e = a.canjes.InsertOne(tx, result); e != nil {
			return nil, e
		}
		_, e = a.movimientos.UpdateOne(tx, bson.M{"_id": ledgerID}, bson.M{"$set": bson.M{"saldoPuntos": cuenta.SaldoPuntos}})
		return nil, e
	}, options.Transaction().SetReadConcern(readconcern.Snapshot()).SetWriteConcern(writeconcern.Majority()))
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			var previo Movimiento
			if e := a.movimientos.FindOne(ctx, bson.M{"_id": ledgerID}).Decode(&previo); e == nil {
				if previo.UsuarioID != userID || previo.Descripcion != rewardID {
					return Canje{}, false, errConflicto
				}
				if e = a.canjes.FindOne(ctx, bson.M{"solicitudId": requestID}).Decode(&result); e == nil {
					return result, true, nil
				}
			}
		}
		if errors.Is(err, errSaldoInsuficiente) {
			return Canje{}, false, errSaldoInsuficiente
		}
		return Canje{}, false, err
	}
	return result, false, nil
}

func (a *almacenMongo) listarCanjes(ctx context.Context, userID string) ([]Canje, error) {
	cur, err := a.canjes.Find(ctx, bson.M{"usuarioId": userID}, options.Find().SetSort(bson.D{{Key: "creadoEn", Value: -1}}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	items := []Canje{}
	return items, cur.All(ctx, &items)
}

func (a *almacenMongo) reservarCanje(ctx context.Context, canjeID, userID, reservaID, tratamientoID string, precio float64) (Canje, error) {
	var item Canje
	if err := a.canjes.FindOne(ctx, bson.M{"_id": canjeID, "usuarioId": userID}).Decode(&item); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return item, errCanjeNoDisponible
		}
		return item, err
	}
	if item.Estado == "reservado" || item.Estado == "aplicado" {
		if item.ReservaID != reservaID {
			return item, errCanjeNoDisponible
		}
		return item, nil
	}
	if item.Estado != "disponible" {
		return item, errCanjeNoDisponible
	}
	if item.Recompensa.Tipo == "tratamiento_gratis" && item.Recompensa.TratamientoID != tratamientoID {
		return item, errors.New("la recompensa no aplica a este tratamiento")
	}
	if precio < 0 || math.IsNaN(precio) || math.IsInf(precio, 0) {
		return item, errors.New("precio inválido")
	}
	descuento := 0.0
	if item.Recompensa.Tipo == "tratamiento_gratis" {
		descuento = precio
	} else {
		descuento = math.Round(precio*float64(item.Recompensa.Porcentaje)) / 100
	}
	item.Estado, item.ReservaID = "reservado", reservaID
	item.PrecioOriginal, item.Descuento, item.PrecioFinal = precio, descuento, math.Max(0, precio-descuento)
	res, err := a.canjes.UpdateOne(ctx, bson.M{"_id": canjeID, "usuarioId": userID, "estado": "disponible"}, bson.M{"$set": bson.M{"estado": item.Estado, "reservaId": reservaID, "precioOriginal": item.PrecioOriginal, "descuento": item.Descuento, "precioFinal": item.PrecioFinal}})
	if err != nil {
		return item, err
	}
	if res.MatchedCount != 1 {
		return item, errCanjeNoDisponible
	}
	if err = a.canjes.FindOne(ctx, bson.M{"_id": canjeID, "usuarioId": userID, "reservaId": reservaID}).Decode(&item); err != nil {
		return item, err
	}
	return item, nil
}

func (a *almacenMongo) actualizarCanje(ctx context.Context, canjeID, userID, reservaID, accion string) (Canje, error) {
	var item Canje
	if err := a.canjes.FindOne(ctx, bson.M{"_id": canjeID, "usuarioId": userID}).Decode(&item); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return item, errCanjeNoDisponible
		}
		return item, err
	}
	if item.ReservaID != reservaID {
		return item, errCanjeNoDisponible
	}
	if accion == "restituir" {
		session, err := a.cliente.StartSession()
		if err != nil {
			return item, err
		}
		defer session.EndSession(ctx)
		_, err = session.WithTransaction(ctx, func(tx context.Context) (any, error) {
			var actual Canje
			if e := a.canjes.FindOne(tx, bson.M{"_id": canjeID, "usuarioId": userID}).Decode(&actual); e != nil {
				if errors.Is(e, mongo.ErrNoDocuments) {
					return nil, errCanjeNoDisponible
				}
				return nil, e
			}
			if actual.Estado == "disponible" {
				item = actual
				return nil, nil
			}
			if actual.Estado != "aplicado" || actual.ReservaID != reservaID {
				return nil, errCanjeNoDisponible
			}
			filter := bson.M{"_id": canjeID, "usuarioId": userID, "reservaId": reservaID, "estado": "aplicado"}
			set := bson.M{"estado": "disponible", "reservaId": "", "precioOriginal": 0, "descuento": 0, "precioFinal": 0}
			updated, e := a.canjes.UpdateOne(tx, filter, bson.M{"$set": set})
			if e != nil {
				return nil, e
			}
			if updated.MatchedCount != 1 {
				return nil, errCanjeNoDisponible
			}
			movementID := "restitucion:beneficio:" + reservaID
			_, e = a.movimientos.UpdateOne(tx, bson.M{"_id": movementID}, bson.M{"$setOnInsert": Movimiento{SolicitudID: movementID, UsuarioID: userID, Puntos: 0, Tipo: "restitucion_beneficio", CreadoEn: time.Now().UTC().Truncate(time.Millisecond), Descripcion: actual.Recompensa.Nombre, Estado: "confirmado"}}, options.UpdateOne().SetUpsert(true))
			if e != nil {
				return nil, e
			}
			return nil, a.canjes.FindOne(tx, bson.M{"_id": canjeID, "usuarioId": userID}).Decode(&item)
		}, options.Transaction().SetReadConcern(readconcern.Snapshot()).SetWriteConcern(writeconcern.Majority()))
		return item, err
	}
	set := bson.M{}
	switch accion {
	case "aplicar":
		if item.Estado == "aplicado" {
			return item, nil
		}
		if item.Estado != "reservado" {
			return item, errCanjeNoDisponible
		}
		now := time.Now().UTC().Truncate(time.Millisecond)
		set = bson.M{"estado": "aplicado", "aplicadoEn": now}
	case "liberar":
		if item.Estado == "disponible" {
			return item, nil
		}
		if item.Estado != "reservado" {
			return item, errCanjeNoDisponible
		}
		set = bson.M{"estado": "disponible", "reservaId": "", "precioOriginal": 0, "descuento": 0, "precioFinal": 0}
	default:
		return item, errors.New("acción desconocida")
	}
	filter := bson.M{"_id": canjeID, "usuarioId": userID, "reservaId": reservaID, "estado": item.Estado}
	res, err := a.canjes.UpdateOne(ctx, filter, bson.M{"$set": set})
	if err != nil {
		return item, err
	}
	if res.MatchedCount != 1 {
		return item, errCanjeNoDisponible
	}
	if err := a.canjes.FindOne(ctx, bson.M{"_id": canjeID, "usuarioId": userID}).Decode(&item); err != nil {
		return item, err
	}
	return item, nil
}

func (a *almacenMongo) procesarEventoTratamiento(ctx context.Context, eventID, userID, treatmentID string) error {
	if strings.TrimSpace(eventID) == "" || strings.TrimSpace(userID) == "" || strings.TrimSpace(treatmentID) == "" {
		return errors.New("evento de tratamiento inválido")
	}
	id := "evento:tratamiento-realizado:" + eventID
	var previo Movimiento
	if err := a.movimientos.FindOne(ctx, bson.M{"_id": id}).Decode(&previo); err == nil {
		return nil
	} else if !errors.Is(err, mongo.ErrNoDocuments) {
		return err
	}
	var rule ReglaTratamiento
	if err := a.reglasTratamientos.FindOne(ctx, bson.M{"_id": treatmentID}).Decode(&rule); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return errReglaNoExiste
		}
		return err
	}
	if rule.Puntos == 0 {
		return nil
	}
	_, _, err := a.acreditarDetalle(ctx, AcreditarRequest{SolicitudID: id, UsuarioID: userID, Puntos: rule.Puntos}, "", "Tratamiento realizado: "+treatmentID)
	return err
}

func (s *Servidor) requiereAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		if s == nil || s.verificarSesion == nil {
			errorJSON(c, 503, "AUTH_NO_DISPONIBLE", "No se pudo validar la sesión.")
			c.Abort()
			return
		}
		identity, err := s.verificarSesion(c.Request.Context(), c.GetHeader("Authorization"))
		if err != nil {
			errorJSON(c, 401, "SESION_INVALIDA", "Iniciá sesión para continuar.")
			c.Abort()
			return
		}
		if !tieneRolEnLista(identity.Roles, "administrador", "superadministrador") {
			errorJSON(c, 403, "PERMISOS_INSUFICIENTES", "Esta acción requiere permisos de administración.")
			c.Abort()
			return
		}
		c.Set("authUserID", identity.ID)
		c.Set("authRoles", identity.Roles)
		c.Next()
	}
}

func tieneRolEnLista(roles []string, permitidos ...string) bool {
	for _, role := range roles {
		for _, allowed := range permitidos {
			if role == allowed {
				return true
			}
		}
	}
	return false
}

func (s *Servidor) listarRecompensas(c *gin.Context) {
	if s.programa == nil {
		errorJSON(c, 503, "BENEFICIOS_NO_DISPONIBLE", "El programa de beneficios no está disponible.")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 4*time.Second)
	defer cancel()
	items, err := s.programa.listarRecompensas(ctx, true)
	if err != nil {
		errorJSON(c, 503, "BENEFICIOS_NO_DISPONIBLE", "No se pudo consultar el catálogo de recompensas.")
		return
	}
	c.JSON(200, gin.H{"recompensas": items})
}

func (s *Servidor) guardarRecompensa(c *gin.Context) {
	var item Recompensa
	if err := c.ShouldBindJSON(&item); err != nil || !recompensaValida(item) {
		errorJSON(c, 400, "RECOMPENSA_INVALIDA", "Completá nombre, ID, costo en puntos y una modalidad de descuento válida.")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 4*time.Second)
	defer cancel()
	if err := s.programa.guardarRecompensa(ctx, item); err != nil {
		errorJSON(c, 503, "BENEFICIOS_NO_DISPONIBLE", "No se pudo guardar la recompensa.")
		return
	}
	c.JSON(200, gin.H{"recompensa": item})
}

func (s *Servidor) listarReglas(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 4*time.Second)
	defer cancel()
	items, err := s.programa.listarReglas(ctx)
	if err != nil {
		errorJSON(c, 503, "BENEFICIOS_NO_DISPONIBLE", "No se pudieron consultar las reglas.")
		return
	}
	c.JSON(200, gin.H{"reglas": items})
}

func (s *Servidor) guardarRegla(c *gin.Context) {
	var item ReglaTratamiento
	if err := c.ShouldBindJSON(&item); err != nil || strings.TrimSpace(item.TratamientoID) == "" || item.Puntos < 0 || item.Puntos > maxPuntos {
		errorJSON(c, 400, "REGLA_INVALIDA", "Indicá el tratamiento y una cantidad de puntos entera igual o mayor que cero.")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 4*time.Second)
	defer cancel()
	if err := s.programa.guardarRegla(ctx, item); err != nil {
		errorJSON(c, 503, "BENEFICIOS_NO_DISPONIBLE", "No se pudo guardar la regla.")
		return
	}
	c.JSON(200, gin.H{"regla": item})
}

func (s *Servidor) canjearRecompensa(c *gin.Context) {
	userID := c.GetString("authUserID")
	var req struct {
		SolicitudID  string `json:"solicitudId"`
		RecompensaID string `json:"recompensaId"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.SolicitudID) == "" || strings.TrimSpace(req.RecompensaID) == "" {
		errorJSON(c, 400, "CANJE_INVALIDO", "Indicá solicitudId único y recompensaId.")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	item, repeated, err := s.programa.canjear(ctx, userID, req.RecompensaID, req.SolicitudID)
	switch {
	case errors.Is(err, errRecompensaNoExiste):
		errorJSON(c, 404, "RECOMPENSA_NO_ENCONTRADA", "La recompensa no está disponible.")
	case errors.Is(err, errConflicto):
		errorJSON(c, 409, "SOLICITUD_EN_CONFLICTO", "solicitudId ya se utilizó con otros datos.")
	case strings.Contains(fmt.Sprint(err), "saldo insuficiente"):
		errorJSON(c, 422, "SALDO_INSUFICIENTE", "No tenés puntos suficientes para este canje.")
	case err != nil:
		errorJSON(c, 503, "BENEFICIOS_NO_DISPONIBLE", "No se pudo completar el canje; podés reintentar con el mismo solicitudId.")
	default:
		status := http.StatusCreated
		if repeated {
			status = http.StatusOK
		}
		c.JSON(status, gin.H{"canje": item})
	}
}

func (s *Servidor) misCanjes(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 4*time.Second)
	defer cancel()
	items, err := s.programa.listarCanjes(ctx, c.GetString("authUserID"))
	if err != nil {
		errorJSON(c, 503, "BENEFICIOS_NO_DISPONIBLE", "No se pudieron consultar tus canjes.")
		return
	}
	c.JSON(200, gin.H{"canjes": items})
}

func (s *Servidor) canjeInterno(accion string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !s.autorizaInterno(c) {
			return
		}
		var req struct {
			UsuarioID     string  `json:"usuarioId"`
			ReservaID     string  `json:"reservaId"`
			TratamientoID string  `json:"tratamientoId"`
			Precio        float64 `json:"precio"`
		}
		if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.UsuarioID) == "" || strings.TrimSpace(req.ReservaID) == "" {
			errorJSON(c, 400, "CANJE_INVALIDO", "Solicitud de canje inválida.")
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 4*time.Second)
		defer cancel()
		var item Canje
		var err error
		if accion == "reservar" {
			item, err = s.programa.reservarCanje(ctx, c.Param("id"), req.UsuarioID, req.ReservaID, req.TratamientoID, req.Precio)
		} else {
			item, err = s.programa.actualizarCanje(ctx, c.Param("id"), req.UsuarioID, req.ReservaID, accion)
		}
		if errors.Is(err, errCanjeNoDisponible) {
			errorJSON(c, 409, "CANJE_NO_DISPONIBLE", "El beneficio ya se usó o está reservado en otra operación.")
			return
		}
		if err != nil {
			errorJSON(c, 503, "BENEFICIOS_NO_DISPONIBLE", "No se pudo actualizar el beneficio.")
			return
		}
		c.JSON(200, gin.H{"canje": item})
	}
}

func (s *Servidor) agregarRutasPrograma(r *gin.Engine) {
	r.GET("/recompensas", s.listarRecompensas)
	r.GET("/mis-canjes", s.requiereCliente(), s.misCanjes)
	r.POST("/canjes", s.requiereCliente(), s.canjearRecompensa)
	r.GET("/admin/recompensas", s.requiereAdmin(), func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 4*time.Second)
		defer cancel()
		items, err := s.programa.listarRecompensas(ctx, false)
		if err != nil {
			errorJSON(c, 503, "BENEFICIOS_NO_DISPONIBLE", "No se pudieron consultar las recompensas.")
			return
		}
		c.JSON(200, gin.H{"recompensas": items})
	})
	r.POST("/admin/recompensas", s.requiereAdmin(), s.guardarRecompensa)
	r.PUT("/admin/recompensas/:id", s.requiereAdmin(), func(c *gin.Context) {
		var item Recompensa
		if err := c.ShouldBindJSON(&item); err != nil || item.ID != c.Param("id") || !recompensaValida(item) {
			errorJSON(c, 400, "RECOMPENSA_INVALIDA", "El ID o los datos de recompensa no son válidos.")
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 4*time.Second)
		defer cancel()
		if err := s.programa.guardarRecompensa(ctx, item); err != nil {
			errorJSON(c, 503, "BENEFICIOS_NO_DISPONIBLE", "No se pudo guardar la recompensa.")
			return
		}
		c.JSON(200, gin.H{"recompensa": item})
	})
	r.GET("/admin/reglas-tratamientos", s.requiereAdmin(), s.listarReglas)
	r.GET("/admin/mensajes-fallidos", s.requiereAdmin(), s.listarMensajesFallidos)
	r.POST("/admin/mensajes-fallidos/:id/reintentar", s.requiereAdmin(), s.reintentarMensajeFallido)
	r.PUT("/admin/reglas-tratamientos/:id", s.requiereAdmin(), func(c *gin.Context) {
		var req struct {
			Puntos int64 `json:"puntos"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			errorJSON(c, 400, "REGLA_INVALIDA", "Datos de regla inválidos.")
			return
		}
		s.guardarReglaConID(c, c.Param("id"), req.Puntos)
	})
	r.POST("/internal/canjes/:id/reservar", s.canjeInterno("reservar"))
	r.POST("/internal/canjes/:id/aplicar", s.canjeInterno("aplicar"))
	r.POST("/internal/canjes/:id/liberar", s.canjeInterno("liberar"))
	r.POST("/internal/canjes/:id/restituir", s.canjeInterno("restituir"))
}

func (s *Servidor) listarMensajesFallidos(c *gin.Context) {
	almacen, ok := s.almacen.(*almacenMongo)
	if !ok {
		errorJSON(c, 503, "BENEFICIOS_NO_DISPONIBLE", "No se pudieron consultar los mensajes fallidos.")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 4*time.Second)
	defer cancel()
	cur, err := almacen.fallidos.Find(ctx, bson.M{"estado": "pendiente"}, options.Find().SetSort(bson.D{{Key: "creadoEn", Value: -1}}).SetLimit(50))
	if err != nil {
		errorJSON(c, 503, "BENEFICIOS_NO_DISPONIBLE", "No se pudieron consultar los mensajes fallidos.")
		return
	}
	defer cur.Close(ctx)
	items := []mensajeFallido{}
	if err = cur.All(ctx, &items); err != nil {
		errorJSON(c, 503, "BENEFICIOS_NO_DISPONIBLE", "No se pudieron leer los mensajes fallidos.")
		return
	}
	c.JSON(200, gin.H{"mensajes": items})
}

func (s *Servidor) reintentarMensajeFallido(c *gin.Context) {
	if s.rabbitURL == "" {
		errorJSON(c, 503, "RABBITMQ_NO_CONFIGURADO", "La mensajería no está configurada.")
		return
	}
	almacen, ok := s.almacen.(*almacenMongo)
	if !ok {
		errorJSON(c, 503, "BENEFICIOS_NO_DISPONIBLE", "No se pudo consultar el mensaje.")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 8*time.Second)
	defer cancel()
	id := strings.TrimSpace(c.Param("id"))
	var item mensajeFallido
	if err := almacen.fallidos.FindOne(ctx, bson.M{"_id": id, "estado": "pendiente"}).Decode(&item); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			errorJSON(c, 404, "MENSAJE_NO_ENCONTRADO", "No hay un mensaje pendiente con ese identificador.")
			return
		}
		errorJSON(c, 503, "BENEFICIOS_NO_DISPONIBLE", "No se pudo consultar el mensaje.")
		return
	}
	claim, err := almacen.fallidos.UpdateOne(ctx, bson.M{"_id": id, "estado": "pendiente"}, bson.M{"$set": bson.M{"estado": "reencolando"}})
	if err != nil || claim.MatchedCount != 1 {
		errorJSON(c, 409, "MENSAJE_EN_PROCESO", "El mensaje ya está siendo recuperado.")
		return
	}
	if err = reintentarEvento(ctx, s.rabbitURL, item.Evento); err != nil {
		_, _ = almacen.fallidos.UpdateOne(context.Background(), bson.M{"_id": id, "estado": "reencolando"}, bson.M{"$set": bson.M{"estado": "pendiente"}})
		errorJSON(c, 503, "RABBITMQ_NO_DISPONIBLE", "No se pudo reenviar el mensaje; sigue pendiente.")
		return
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	_, err = almacen.fallidos.UpdateOne(ctx, bson.M{"_id": id, "estado": "reencolando"}, bson.M{"$set": bson.M{"estado": "reintentado", "reintentadoEn": now}})
	if err != nil {
		errorJSON(c, 503, "BENEFICIOS_NO_DISPONIBLE", "El mensaje se reenvío, pero no se pudo actualizar su estado.")
		return
	}
	c.JSON(202, gin.H{"id": id, "estado": "reintentado"})
}

func (s *Servidor) guardarReglaConID(c *gin.Context, id string, puntos int64) {
	item := ReglaTratamiento{TratamientoID: strings.TrimSpace(id), Puntos: puntos}
	if item.TratamientoID == "" || puntos < 0 || puntos > maxPuntos {
		errorJSON(c, 400, "REGLA_INVALIDA", "La cantidad de puntos debe ser un entero igual o mayor que cero.")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 4*time.Second)
	defer cancel()
	if err := s.programa.guardarRegla(ctx, item); err != nil {
		errorJSON(c, 503, "BENEFICIOS_NO_DISPONIBLE", "No se pudo guardar la regla.")
		return
	}
	c.JSON(200, gin.H{"regla": item})
}
