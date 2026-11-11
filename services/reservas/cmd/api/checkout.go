package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

var errCheckoutEnCurso = errors.New("checkout en curso")

type checkoutItem struct {
	HoldID           string    `bson:"holdId" json:"holdId"`
	TurnID           string    `bson:"turnId" json:"turnId"`
	TratamientoID    string    `bson:"tratamientoId" json:"tratamientoId"`
	ProfesionalID    string    `bson:"profesionalId" json:"profesionalId"`
	Inicio           time.Time `bson:"inicio" json:"inicio"`
	Fin              time.Time `bson:"fin" json:"fin"`
	Precio           float64   `bson:"precio" json:"precio"`
	Descuento        float64   `bson:"descuento" json:"descuento"`
	PrecioFinal      float64   `bson:"precioFinal" json:"precioFinal"`
	Sena             float64   `bson:"sena" json:"sena"`
	BeneficioCanjeID string    `bson:"beneficioCanjeId,omitempty" json:"beneficioCanjeId,omitempty"`
}

type checkoutOperation struct {
	ID             string         `bson:"_id" json:"id"`
	ClienteID      string         `bson:"clienteId" json:"clienteId"`
	IdempotencyKey string         `bson:"idempotencyKey" json:"-"`
	ItemIDs        []string       `bson:"itemIds" json:"itemIds"`
	Items          []checkoutItem `bson:"items" json:"items"`
	Subtotal       float64        `bson:"subtotal" json:"subtotal"`
	Descuento      float64        `bson:"descuento" json:"descuento"`
	Total          float64        `bson:"total" json:"total"`
	Sena           float64        `bson:"sena" json:"sena"`
	Estado         string         `bson:"estado" json:"estado"`
	PagoID         string         `bson:"pagoId,omitempty" json:"pagoId,omitempty"`
	TurnoIDs       []string       `bson:"turnoIds,omitempty" json:"turnoIds,omitempty"`
	CreadoEn       time.Time      `bson:"creadoEn" json:"creadoEn"`
	ActualizadoEn  time.Time      `bson:"actualizadoEn" json:"actualizadoEn"`
}

type simulatePaymentRequest struct {
	CheckoutID string  `json:"checkoutId"`
	Amount     float64 `json:"importe"`
	Currency   string  `json:"moneda"`
}

type simulatedPayment struct {
	ID         string  `json:"id"`
	CheckoutID string  `json:"checkoutId"`
	Amount     float64 `json:"importe"`
	Currency   string  `json:"moneda"`
	Status     string  `json:"estado"`
}

type profesionalCatalogo struct {
	ID              string   `json:"id"`
	Activo          bool     `json:"activo"`
	TratamientosIDs []string `json:"tratamientosIds"`
}

func (s *servidorReservas) profesionalCompatible(ctx context.Context, professionalID, treatmentID string) error {
	if strings.TrimSpace(professionalID) == "" {
		return errors.New("perfil profesional inválido")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(s.catalogoURL, "/")+"/profesionales/"+professionalID, nil)
	if err != nil {
		return err
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New("el profesional no está activo en Catálogo")
	}
	var professional profesionalCatalogo
	if json.NewDecoder(resp.Body).Decode(&professional) != nil || professional.ID != professionalID || !professional.Activo {
		return errors.New("Catálogo devolvió un perfil profesional no válido")
	}
	for _, id := range professional.TratamientosIDs {
		if id == treatmentID {
			return nil
		}
	}
	return errors.New("el profesional ya no está asociado al tratamiento")
}

func (s *servidorReservas) simularPago(ctx context.Context, checkoutID string, amount float64) (simulatedPayment, error) {
	var payment simulatedPayment
	body, _ := json.Marshal(simulatePaymentRequest{CheckoutID: checkoutID, Amount: amount, Currency: "ARS"})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(s.pagosURL, "/")+"/payments/simulate", strings.NewReader(string(body)))
	if err != nil {
		return payment, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Melt-Internal-Key", s.internalKey)
	req.Header.Set("Idempotency-Key", "checkout-"+checkoutID)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return payment, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return payment, errors.New("Pagos no pudo registrar la simulación")
	}
	if err := json.NewDecoder(resp.Body).Decode(&payment); err != nil {
		return payment, err
	}
	if payment.ID == "" || payment.CheckoutID != checkoutID || payment.Currency != "ARS" || math.Abs(payment.Amount-amount) > 0.009 || (payment.Status != "approved" && payment.Status != "rejected") {
		return payment, errors.New("Pagos devolvió una operación simulada inválida")
	}
	return payment, nil
}

func (s *servidorReservas) revertirPago(ctx context.Context, paymentID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(s.pagosURL, "/")+"/payments/"+paymentID+"/reverse", strings.NewReader(`{}`))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Melt-Internal-Key", s.internalKey)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New("Pagos no pudo revertir la operación simulada")
	}
	return nil
}

func (s *servidorReservas) checkoutCarrito(c *gin.Context) {
	u := c.MustGet("identidad").(identidad)
	var req struct {
		IdempotencyKey string `json:"idempotencyKey" binding:"required"`
	}
	decoder := json.NewDecoder(io.LimitReader(c.Request.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&req) != nil || !validCheckoutKey(req.IdempotencyKey) {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "CHECKOUT_INVALIDO", "message": "No se pudo iniciar la compra. Actualizá el carrito e intentá otra vez."}})
		return
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "CHECKOUT_INVALIDO", "message": "No se pudo iniciar la compra. Actualizá el carrito e intentá otra vez."}})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	filter := bson.M{"clienteId": u.ID, "idempotencyKey": req.IdempotencyKey}
	var order checkoutOperation
	err := s.checkouts.FindOne(ctx, filter).Decode(&order)
	if err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no se pudo recuperar la compra"})
		return
	}
	if errors.Is(err, mongo.ErrNoDocuments) {
		order, err = s.prepareCheckout(ctx, u.ID, req.IdempotencyKey)
		if err != nil {
			status := http.StatusConflict
			code := "CHECKOUT_NO_DISPONIBLE"
			if errors.Is(err, mongo.ErrNoDocuments) {
				status, code = http.StatusBadRequest, "CARRITO_VACIO"
			}
			c.JSON(status, gin.H{"error": gin.H{"code": code, "message": err.Error()}})
			return
		}
	}
	if order.Estado == "confirmed" || order.Estado == "rejected" || order.Estado == "reverted" {
		c.JSON(http.StatusOK, checkoutResponse(order))
		return
	}
	payment, err := s.simularPago(ctx, order.ID, order.Sena)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "PAGO_SIMULADO_NO_DISPONIBLE", "message": "No pudimos registrar la compra simulada; tus horarios siguen retenidos durante su vigencia."}})
		return
	}
	order.PagoID = payment.ID
	if payment.Status == "rejected" {
		_, _ = s.checkouts.UpdateOne(ctx, bson.M{"_id": order.ID, "estado": "processing"}, bson.M{"$set": bson.M{"estado": "rejected", "pagoId": payment.ID, "actualizadoEn": time.Now().UTC()}})
		_, _ = s.carritos.UpdateMany(ctx, bson.M{"_id": bson.M{"$in": order.ItemIDs}, "clienteId": u.ID, "checkoutId": order.ID}, bson.M{"$unset": bson.M{"checkoutId": ""}})
		order.Estado = "rejected"
		c.JSON(http.StatusOK, checkoutResponse(order))
		return
	}
	order.Estado = "processing"
	if err := s.confirmarCheckout(ctx, &order, payment.ID); err != nil {
		reverseErr := s.revertirPago(ctx, payment.ID)
		status := "reverted"
		if reverseErr != nil {
			status = "approval_needs_recovery"
		} else {
			_, _ = s.carritos.UpdateMany(context.Background(), bson.M{"_id": bson.M{"$in": order.ItemIDs}, "clienteId": order.ClienteID, "checkoutId": order.ID, "estado": "pendiente_pago"}, bson.M{"$unset": bson.M{"checkoutId": ""}})
		}
		_, _ = s.checkouts.UpdateOne(context.Background(), bson.M{"_id": order.ID, "estado": "processing"}, bson.M{"$set": bson.M{"estado": status, "pagoId": payment.ID, "actualizadoEn": time.Now().UTC(), "errorConfirmacion": safeError(err)}})
		c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code": "CHECKOUT_NO_CONFIRMADO", "message": "No se confirmaron reservas parciales. La simulación fue revertida; volvé a revisar los horarios retenidos."}})
		return
	}
	c.JSON(http.StatusCreated, checkoutResponse(order))
}

func (s *servidorReservas) prepareCheckout(ctx context.Context, clientID, key string) (checkoutOperation, error) {
	var prior checkoutOperation
	if err := s.checkouts.FindOne(ctx, bson.M{"clienteId": clientID, "idempotencyKey": key}).Decode(&prior); err == nil {
		return prior, nil
	} else if !errors.Is(err, mongo.ErrNoDocuments) {
		return checkoutOperation{}, err
	}
	now := time.Now().UTC()
	cur, err := s.carritos.Find(ctx, bson.M{"clienteId": clientID, "estado": "pendiente_pago", "expiraEn": bson.M{"$gt": now}}, options.Find().SetSort(bson.D{{Key: "inicio", Value: 1}}))
	if err != nil {
		return checkoutOperation{}, err
	}
	defer cur.Close(ctx)
	var holds []bloqueoCarrito
	if err := cur.All(ctx, &holds); err != nil {
		return checkoutOperation{}, err
	}
	if len(holds) == 0 {
		return checkoutOperation{}, mongo.ErrNoDocuments
	}
	id := bson.NewObjectID().Hex()
	order := checkoutOperation{ID: id, ClienteID: clientID, IdempotencyKey: key, Estado: "processing", CreadoEn: now, ActualizadoEn: now, Items: []checkoutItem{}, ItemIDs: []string{}, TurnoIDs: []string{}}
	for i := range holds {
		h := &holds[i]
		if h.CheckoutID != "" {
			return checkoutOperation{}, errors.New("un elemento del carrito ya está participando en otra compra")
		}
		if !h.ExpiraEn.After(now) || !h.Inicio.After(now) {
			return checkoutOperation{}, errors.New("un horario venció; actualizá el carrito. Los demás horarios retenidos siguen disponibles")
		}
		t, _, err := s.obtenerTratamiento(ctx, h.TratamientoID)
		if err != nil || !tratamientoCompatible(t, h.ProfesionalID) || t.Moneda != "" && t.Moneda != "ARS" || t.Precio <= 0 {
			return checkoutOperation{}, errors.New("un tratamiento dejó de estar disponible o su precio no es válido")
		}
		if err := s.profesionalCompatible(ctx, h.ProfesionalID, t.ID); err != nil {
			return checkoutOperation{}, errors.New("un profesional dejó de estar activo o habilitado para este tratamiento")
		}
		if t.DuracionMinutos <= 0 || !h.Inicio.Add(time.Duration(t.DuracionMinutos)*time.Minute).Equal(h.Fin) {
			return checkoutOperation{}, errors.New("la duración o el horario de un tratamiento cambió; volvé a seleccionarlo")
		}
		locks, lockErr := s.bloqueos.CountDocuments(ctx, bson.M{"turnoId": h.ID, "expiraEn": bson.M{"$gt": now}})
		expected := int(h.Fin.Sub(h.Inicio) / time.Minute)
		if lockErr != nil || locks < int64(expected) {
			return checkoutOperation{}, errors.New("un horario ya no conserva todas sus retenciones; actualizá el carrito")
		}
		h.Precio, h.PrecioFinal, h.DescuentoBeneficio = t.Precio, t.Precio, 0
		if h.BeneficioCanjeID != "" {
			benefit, benefitErr := s.reservarBeneficio(ctx, h.BeneficioCanjeID, clientID, h.ID, h.TratamientoID, t.Precio)
			if benefitErr != nil {
				return checkoutOperation{}, errors.New("un beneficio ya no está disponible; quitá ese elemento o actualizá el carrito")
			}
			h.DescuentoBeneficio, h.PrecioFinal = benefit.Descuento, benefit.PrecioFinal
		}
		h.Sena = importeSena(h.PrecioFinal)
		if _, err := s.carritos.UpdateOne(ctx, bson.M{"_id": h.ID, "clienteId": clientID, "estado": "pendiente_pago", "expiraEn": bson.M{"$gt": now}}, bson.M{"$set": bson.M{"precio": h.Precio, "precioFinal": h.PrecioFinal, "descuentoBeneficio": h.DescuentoBeneficio, "sena": h.Sena}}); err != nil {
			return checkoutOperation{}, err
		}
		turnID := bson.NewObjectID().Hex()
		order.Items = append(order.Items, checkoutItem{HoldID: h.ID, TurnID: turnID, TratamientoID: h.TratamientoID, ProfesionalID: h.ProfesionalID, Inicio: h.Inicio, Fin: h.Fin, Precio: h.Precio, Descuento: h.DescuentoBeneficio, PrecioFinal: h.PrecioFinal, Sena: h.Sena, BeneficioCanjeID: h.BeneficioCanjeID})
		order.ItemIDs = append(order.ItemIDs, h.ID)
		order.TurnoIDs = append(order.TurnoIDs, turnID)
		order.Subtotal += h.Precio
		order.Descuento += h.DescuentoBeneficio
		order.Total += h.PrecioFinal
		order.Sena += h.Sena
	}
	session, err := s.mongo.StartSession()
	if err != nil {
		return checkoutOperation{}, err
	}
	defer session.EndSession(ctx)
	_, err = session.WithTransaction(ctx, func(tx context.Context) (any, error) {
		for _, holdID := range order.ItemIDs {
			res, e := s.carritos.UpdateOne(tx, bson.M{"_id": holdID, "clienteId": clientID, "estado": "pendiente_pago", "checkoutId": bson.M{"$in": bson.A{"", nil}}, "expiraEn": bson.M{"$gt": now}}, bson.M{"$set": bson.M{"checkoutId": id}})
			if e != nil {
				return nil, e
			}
			if res.MatchedCount != 1 {
				return nil, errors.New("un horario venció o cambió mientras se preparaba la compra")
			}
		}
		if _, e := s.checkouts.InsertOne(tx, order); e != nil {
			return nil, e
		}
		return nil, nil
	})
	if err != nil {
		var existing checkoutOperation
		if s.checkouts.FindOne(ctx, bson.M{"clienteId": clientID, "idempotencyKey": key}).Decode(&existing) == nil {
			return existing, nil
		}
		return checkoutOperation{}, err
	}
	return order, nil
}

func (s *servidorReservas) confirmarCheckout(ctx context.Context, order *checkoutOperation, paymentID string) error {
	aplicados := make([]checkoutItem, 0, len(order.Items))
	for _, item := range order.Items {
		if item.BeneficioCanjeID == "" {
			continue
		}
		if err := s.actualizarBeneficio(ctx, item.BeneficioCanjeID, order.ClienteID, item.HoldID, "aplicar"); err != nil {
			for _, done := range aplicados {
				_ = s.actualizarBeneficio(context.Background(), done.BeneficioCanjeID, order.ClienteID, done.HoldID, "restituir")
			}
			return errors.New("no se pudo aplicar un beneficio del checkout")
		}
		aplicados = append(aplicados, item)
	}
	now := time.Now().UTC()
	session, err := s.mongo.StartSession()
	if err != nil {
		for _, item := range aplicados {
			_ = s.actualizarBeneficio(context.Background(), item.BeneficioCanjeID, order.ClienteID, item.HoldID, "restituir")
		}
		return err
	}
	defer session.EndSession(ctx)
	_, err = session.WithTransaction(ctx, func(tx context.Context) (any, error) {
		var current checkoutOperation
		if e := s.checkouts.FindOne(tx, bson.M{"_id": order.ID, "clienteId": order.ClienteID}).Decode(&current); e != nil {
			return nil, e
		}
		if current.Estado == "confirmed" {
			*order = current
			return nil, nil
		}
		if current.Estado != "processing" {
			return nil, errors.New("checkout ya procesado")
		}
		for _, item := range current.Items {
			var hold bloqueoCarrito
			if e := s.carritos.FindOne(tx, bson.M{"_id": item.HoldID, "clienteId": order.ClienteID, "estado": "pendiente_pago", "checkoutId": current.ID, "expiraEn": bson.M{"$gt": now}}).Decode(&hold); e != nil {
				return nil, errors.New("una retención venció o dejó de pertenecer a la compra")
			}
			t, status, e := s.obtenerTratamiento(tx, hold.TratamientoID)
			if e != nil || status != http.StatusOK || !tratamientoCompatible(t, hold.ProfesionalID) || math.Abs(t.Precio-hold.Precio) > 0.009 {
				return nil, errors.New("un tratamiento cambió o dejó de estar disponible")
			}
			if e := s.profesionalCompatible(tx, hold.ProfesionalID, t.ID); e != nil {
				return nil, errors.New("un profesional cambió o dejó de estar habilitado")
			}
			lockCount, e := s.bloqueos.CountDocuments(tx, bson.M{"turnoId": hold.ID, "expiraEn": bson.M{"$gt": now}})
			if e != nil || lockCount < int64(hold.Fin.Sub(hold.Inicio)/time.Minute) {
				return nil, errors.New("una retención ya no protege todo el horario")
			}
			turn := turno{ID: item.TurnID, ClienteID: current.ClienteID, ProfesionalID: item.ProfesionalID, TratamientoID: item.TratamientoID, CategoriaID: hold.CategoriaID, Inicio: item.Inicio, Fin: item.Fin, Estado: "confirmado", CreadoEn: now, PrecioTratamiento: item.Precio, Sena: item.Sena, PagoID: paymentID, EstadoPago: "aprobado", BeneficioCanjeID: item.BeneficioCanjeID, BeneficioReservaID: item.HoldID, DescuentoBeneficio: item.Descuento, PrecioFinal: item.PrecioFinal}
			if _, e := s.turnos.InsertOne(tx, turn); e != nil {
				return nil, e
			}
			if _, e := s.carritos.UpdateOne(tx, bson.M{"_id": item.HoldID, "checkoutId": current.ID, "estado": "pendiente_pago"}, bson.M{"$set": bson.M{"estado": "confirmado", "turnoId": turn.ID, "pagoId": paymentID}, "$unset": bson.M{"expiraEn": "", "checkoutId": ""}}); e != nil {
				return nil, e
			}
			if _, e := s.bloqueos.UpdateMany(tx, bson.M{"turnoId": item.HoldID}, bson.M{"$set": bson.M{"turnoId": turn.ID}, "$unset": bson.M{"expiraEn": ""}}); e != nil {
				return nil, e
			}
		}
		updated, e := s.checkouts.UpdateOne(tx, bson.M{"_id": current.ID, "estado": "processing"}, bson.M{"$set": bson.M{"estado": "confirmed", "pagoId": paymentID, "actualizadoEn": now}})
		if e != nil {
			return nil, e
		}
		if updated.MatchedCount != 1 {
			return nil, errors.New("checkout cambió durante la confirmación")
		}
		current.Estado, current.PagoID, current.ActualizadoEn = "confirmed", paymentID, now
		*order = current
		return nil, nil
	})
	if err != nil {
		for _, item := range aplicados {
			_ = s.actualizarBeneficio(context.Background(), item.BeneficioCanjeID, order.ClienteID, item.HoldID, "restituir")
		}
		return err
	}
	return nil
}

func checkoutResponse(order checkoutOperation) gin.H {
	paymentStatus := "pending"
	if order.Estado == "confirmed" {
		paymentStatus = "approved"
	}
	if order.Estado == "rejected" {
		paymentStatus = "rejected"
	}
	if order.Estado == "reverted" {
		paymentStatus = "reverted"
	}
	return gin.H{"checkout": gin.H{"id": order.ID, "estado": order.Estado, "subtotal": order.Subtotal, "descuento": order.Descuento, "total": order.Total, "sena": order.Sena, "restante": math.Max(0, order.Total-order.Sena), "pagoId": order.PagoID, "pagoEstado": paymentStatus, "turnos": order.TurnoIDs, "items": order.Items}}
}

func validCheckoutKey(key string) bool {
	if len(key) < 8 || len(key) > 80 {
		return false
	}
	for _, r := range key {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func safeError(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
