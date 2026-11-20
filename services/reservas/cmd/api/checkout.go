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

type checkoutPreference struct {
	ID         string  `json:"id"`
	CheckoutID string  `json:"checkoutId"`
	Amount     float64 `json:"importe"`
	Currency   string  `json:"moneda"`
	Status     string  `json:"estado"`
	InitPoint  string  `json:"urlPago"`
}

type paymentVerification struct {
	CheckoutID string  `json:"checkoutId"`
	PaymentID  string  `json:"pagoId"`
	Status     string  `json:"estado"`
	Approved   bool    `json:"aprobado"`
	Amount     float64 `json:"importe"`
	Currency   string  `json:"moneda"`
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

func (s *servidorReservas) crearPreferenciaMP(ctx context.Context, order checkoutOperation) (checkoutPreference, error) {
	var result checkoutPreference
	body, _ := json.Marshal(map[string]any{"checkoutId": order.ID, "importe": order.Sena, "moneda": "ARS", "titulo": "Seña de reserva MELT"})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(s.pagosURL, "/")+"/payments/preferences", strings.NewReader(string(body)))
	if err != nil {
		return result, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Melt-Internal-Key", s.internalKey)
	req.Header.Set("Idempotency-Key", "checkout-"+order.ID)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return result, errors.New("Pagos no pudo crear la preferencia de Mercado Pago")
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return result, err
	}
	if result.ID == "" || result.CheckoutID != order.ID || result.Currency != "ARS" || math.Abs(result.Amount-order.Sena) > 0.009 || !strings.HasPrefix(result.InitPoint, "https://") {
		return result, errors.New("Pagos devolvió una preferencia inválida")
	}
	return result, nil
}

func (s *servidorReservas) verificarPagoMP(ctx context.Context, checkoutID, paymentID string, amount float64) (paymentVerification, error) {
	var result paymentVerification
	body, _ := json.Marshal(map[string]any{"pagoId": paymentID, "importe": amount, "moneda": "ARS", "referencia": checkoutID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(s.pagosURL, "/")+"/payments/verify", strings.NewReader(string(body)))
	if err != nil {
		return result, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Melt-Internal-Key", s.internalKey)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return result, errors.New("Pagos no pudo verificar la operación en Mercado Pago")
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return result, err
	}
	if result.CheckoutID != checkoutID || result.PaymentID != paymentID || result.Currency != "ARS" || math.Abs(result.Amount-amount) > 0.009 {
		return result, errors.New("la operación verificada no coincide con el checkout")
	}
	return result, nil
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
	var err error
	if s.pg != nil {
		order, err = s.pg.checkoutPorClienteClave(ctx, u.ID, req.IdempotencyKey)
	} else {
		err = s.checkouts.FindOne(ctx, filter).Decode(&order)
	}
	noOrder := errors.Is(err, mongo.ErrNoDocuments) || sqlNotFound(err)
	if err != nil && !noOrder {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no se pudo recuperar la compra"})
		return
	}
	if noOrder {
		order, err = s.prepareCheckout(ctx, u.ID, req.IdempotencyKey)
		if err != nil {
			status := http.StatusConflict
			code := "CHECKOUT_NO_DISPONIBLE"
			if errors.Is(err, mongo.ErrNoDocuments) || sqlNotFound(err) {
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
	preference, err := s.crearPreferenciaMP(ctx, order)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "PAGO_NO_DISPONIBLE", "message": "No pudimos iniciar el pago de prueba; tus horarios siguen retenidos durante su vigencia."}})
		return
	}
	order.PagoID = preference.ID
	order.Estado = "processing"
	if s.pg != nil {
		_, _ = s.pg.actualizarCheckout(ctx, order.ID, "processing", preference.ID, time.Now().UTC())
	}
	response := checkoutResponse(order)
	response["checkout"].(gin.H)["urlPago"] = preference.InitPoint
	c.JSON(http.StatusCreated, response)
}

func (s *servidorReservas) verificarRetornoPago(c *gin.Context) {
	u := c.MustGet("identidad").(identidad)
	var req struct {
		CheckoutID string `json:"checkoutId"`
		PaymentID  string `json:"pagoId"`
	}
	decoder := json.NewDecoder(io.LimitReader(c.Request.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&req) != nil || !validCheckoutKey(req.CheckoutID) || !validCheckoutKey(req.PaymentID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Retorno de pago inválido"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	var order checkoutOperation
	var err error
	if s.pg != nil {
		order, err = s.pg.checkoutPorIDCliente(ctx, req.CheckoutID, u.ID)
	} else {
		err = s.checkouts.FindOne(ctx, bson.M{"_id": req.CheckoutID, "clienteId": u.ID}).Decode(&order)
	}
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Checkout no encontrado"})
		return
	}
	if order.Estado == "confirmed" {
		c.JSON(http.StatusOK, checkoutResponse(order))
		return
	}
	if order.Estado != "processing" {
		c.JSON(http.StatusConflict, gin.H{"error": "Este checkout ya no está pendiente"})
		return
	}
	verified, err := s.procesarResultadoPago(ctx, &order, req.PaymentID)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "No se pudo comprobar o procesar el pago. Actualizá el estado en unos segundos."})
		return
	}
	response := checkoutResponse(order)
	response["checkout"].(gin.H)["pagoEstado"] = verified.Status
	c.JSON(http.StatusOK, response)
}

func (s *servidorReservas) webhookPagoMP(c *gin.Context) {
	if s.internalKey == "" || c.GetHeader("X-Melt-Internal-Key") != s.internalKey {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Operación interna no autorizada"})
		return
	}
	var req struct {
		CheckoutID string `json:"checkoutId"`
		PaymentID  string `json:"pagoId"`
	}
	if c.ShouldBindJSON(&req) != nil || !validCheckoutKey(req.CheckoutID) || !validCheckoutKey(req.PaymentID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Notificación interna inválida"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	var order checkoutOperation
	var err error
	if s.pg != nil {
		order, err = s.pg.checkoutPorID(ctx, req.CheckoutID)
	} else {
		err = s.checkouts.FindOne(ctx, bson.M{"_id": req.CheckoutID}).Decode(&order)
	}
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Checkout no encontrado"})
		return
	}
	if order.Estado == "confirmed" {
		c.JSON(http.StatusOK, checkoutResponse(order))
		return
	}
	if order.Estado != "processing" {
		c.JSON(http.StatusOK, checkoutResponse(order))
		return
	}
	if _, err := s.procesarResultadoPago(ctx, &order, req.PaymentID); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "No se pudo procesar el pago notificado"})
		return
	}
	c.JSON(http.StatusOK, checkoutResponse(order))
}

func (s *servidorReservas) procesarResultadoPago(ctx context.Context, order *checkoutOperation, paymentID string) (paymentVerification, error) {
	verified, err := s.verificarPagoMP(ctx, order.ID, paymentID, order.Sena)
	if err != nil {
		return verified, err
	}
	order.PagoID = paymentID
	if verified.Approved {
		if err := s.confirmarCheckout(ctx, order, paymentID); err != nil {
			return verified, err
		}
	} else if verified.Status == "rejected" || verified.Status == "cancelled" {
		order.Estado = "rejected"
		if s.pg != nil {
			_, _ = s.pg.actualizarCheckout(ctx, order.ID, "rejected", paymentID, time.Now().UTC())
			_ = s.pg.limpiarCheckoutCarritos(ctx, order.ItemIDs, order.ClienteID, order.ID)
		} else {
			_, _ = s.checkouts.UpdateOne(ctx, bson.M{"_id": order.ID, "estado": "processing"}, bson.M{"$set": bson.M{"estado": "rejected", "pagoId": paymentID, "actualizadoEn": time.Now().UTC()}})
			_, _ = s.carritos.UpdateMany(ctx, bson.M{"_id": bson.M{"$in": order.ItemIDs}, "clienteId": order.ClienteID, "checkoutId": order.ID}, bson.M{"$unset": bson.M{"checkoutId": ""}})
		}
	}
	return verified, nil
}

func (s *servidorReservas) prepareCheckout(ctx context.Context, clientID, key string) (checkoutOperation, error) {
	var prior checkoutOperation
	var priorErr error
	if s.pg != nil {
		prior, priorErr = s.pg.checkoutPorClienteClave(ctx, clientID, key)
	} else {
		priorErr = s.checkouts.FindOne(ctx, bson.M{"clienteId": clientID, "idempotencyKey": key}).Decode(&prior)
	}
	if err := priorErr; err == nil {
		return prior, nil
	} else if !errors.Is(err, mongo.ErrNoDocuments) && !sqlNotFound(err) {
		return checkoutOperation{}, err
	}
	now := time.Now().UTC()
	if s.pg != nil {
		holds, err := s.pg.carritosCheckout(ctx, clientID, "pendiente_pago", now)
		if err != nil {
			return checkoutOperation{}, err
		}
		return s.prepararCheckoutDesdeRetenciones(ctx, clientID, key, now, holds)
	}
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

func (s *servidorReservas) prepararCheckoutDesdeRetenciones(ctx context.Context, clientID, key string, now time.Time, holds []bloqueoCarrito) (checkoutOperation, error) {
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
		if err != nil || !tratamientoCompatible(t, h.ProfesionalID) || (t.Moneda != "" && t.Moneda != "ARS") || t.Precio <= 0 {
			return checkoutOperation{}, errors.New("un tratamiento dejó de estar disponible o su precio no es válido")
		}
		if err := s.profesionalCompatible(ctx, h.ProfesionalID, t.ID); err != nil {
			return checkoutOperation{}, errors.New("un profesional dejó de estar activo o habilitado para este tratamiento")
		}
		if t.DuracionMinutos <= 0 || !h.Inicio.Add(time.Duration(t.DuracionMinutos)*time.Minute).Equal(h.Fin) {
			return checkoutOperation{}, errors.New("la duración o el horario de un tratamiento cambió; volvé a seleccionarlo")
		}
		locks, err := s.pg.bloqueoMinutos(ctx, h.ID, now)
		expected := int64(h.Fin.Sub(h.Inicio) / time.Minute)
		if err != nil || locks < expected {
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
		if err := s.pg.actualizarPrecioCarrito(ctx, h.ID, clientID, h.Precio, h.PrecioFinal, h.DescuentoBeneficio, h.Sena); err != nil {
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
	err := s.pg.transaction(ctx, func(tx context.Context) error {
		for _, holdID := range order.ItemIDs {
			matched, err := s.pg.asignarCheckoutCarrito(tx, holdID, clientID, id, now)
			if err != nil {
				return err
			}
			if matched != 1 {
				return errors.New("un horario venció o cambió mientras se preparaba la compra")
			}
		}
		return s.pg.insertarCheckout(tx, order)
	})
	if err != nil {
		if existing, lookupErr := s.pg.checkoutPorClienteClave(ctx, clientID, key); lookupErr == nil {
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
	if s.pg != nil {
		if err := s.confirmarCheckoutPostgres(ctx, order, paymentID, now); err != nil {
			for _, item := range aplicados {
				_ = s.actualizarBeneficio(context.Background(), item.BeneficioCanjeID, order.ClienteID, item.HoldID, "restituir")
			}
			return err
		}
		return nil
	}
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

func (s *servidorReservas) confirmarCheckoutPostgres(ctx context.Context, order *checkoutOperation, paymentID string, now time.Time) error {
	return s.pg.transaction(ctx, func(tx context.Context) error {
		current, err := s.pg.checkout(tx, order.ID, order.ClienteID)
		if err != nil {
			return err
		}
		if current.Estado == "confirmed" {
			*order = current
			return nil
		}
		if current.Estado != "processing" {
			return errors.New("checkout ya procesado")
		}
		for _, item := range current.Items {
			hold, err := s.pg.carritoForUpdate(tx, item.HoldID)
			if err != nil || hold.ClienteID != order.ClienteID || hold.Estado != "pendiente_pago" || hold.CheckoutID != current.ID || !hold.ExpiraEn.After(now) {
				return errors.New("una retención venció o dejó de pertenecer a la compra")
			}
			t, status, err := s.obtenerTratamiento(tx, hold.TratamientoID)
			if err != nil || status != http.StatusOK || !tratamientoCompatible(t, hold.ProfesionalID) || math.Abs(t.Precio-hold.Precio) > 0.009 {
				return errors.New("un tratamiento cambió o dejó de estar disponible")
			}
			if err := s.profesionalCompatible(tx, hold.ProfesionalID, t.ID); err != nil {
				return errors.New("un profesional cambió o dejó de estar habilitado")
			}
			lockCount, err := s.pg.bloqueoMinutos(tx, hold.ID, now)
			if err != nil || lockCount < int64(hold.Fin.Sub(hold.Inicio)/time.Minute) {
				return errors.New("una retención ya no protege todo el horario")
			}
			turn := turno{ID: item.TurnID, ClienteID: current.ClienteID, ProfesionalID: item.ProfesionalID, TratamientoID: item.TratamientoID, CategoriaID: hold.CategoriaID, Inicio: item.Inicio, Fin: item.Fin, Estado: "confirmado", CreadoEn: now, PrecioTratamiento: item.Precio, Sena: item.Sena, PagoID: paymentID, EstadoPago: "aprobado", BeneficioCanjeID: item.BeneficioCanjeID, BeneficioReservaID: item.HoldID, DescuentoBeneficio: item.Descuento, PrecioFinal: item.PrecioFinal}
			if err := s.pg.insertarTurno(tx, turn); err != nil {
				return err
			}
			matched, err := s.pg.confirmarCarrito(tx, item.HoldID, current.ID, turn.ID, paymentID)
			if err != nil {
				return err
			}
			if matched != 1 {
				return errors.New("una retención dejó de pertenecer al checkout")
			}
			if err := s.pg.transferirBloqueos(tx, item.HoldID, turn.ID); err != nil {
				return err
			}
		}
		matched, err := s.pg.confirmarCheckoutEstado(tx, current.ID, paymentID, now)
		if err != nil {
			return err
		}
		if matched != 1 {
			return errors.New("checkout cambió durante la confirmación")
		}
		current.Estado, current.PagoID, current.ActualizadoEn = "confirmed", paymentID, now
		*order = current
		return nil
	})
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
