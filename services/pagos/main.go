package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type payment struct {
	ID              string    `bson:"_id" json:"id"`
	CheckoutID      string    `bson:"checkoutId" json:"checkoutId"`
	IdempotencyKey  string    `bson:"idempotencyKey" json:"-"`
	Amount          float64   `bson:"importe" json:"importe"`
	Currency        string    `bson:"moneda" json:"moneda"`
	Status          string    `bson:"estado" json:"estado"`
	CreatedAt       time.Time `bson:"creadoEn" json:"creadoEn"`
	Refunded        float64   `bson:"devuelto" json:"devuelto"`
	PreferenceID    string    `bson:"preferenceId,omitempty" json:"preferenceId,omitempty"`
	InitPoint       string    `bson:"initPoint,omitempty" json:"initPoint,omitempty"`
	ProviderID      string    `bson:"providerPaymentId,omitempty" json:"providerPaymentId,omitempty"`
	ProviderOrderID string    `bson:"providerOrderId,omitempty" json:"providerOrderId,omitempty"`
}

type server struct {
	payments            *mongo.Collection
	refunds             *mongo.Collection
	internalKey         string
	accessToken         string
	publicURL           string
	webhookSecret       string
	reservasCallbackURL string
	httpClient          *http.Client
}

type verifyRequest struct {
	PaymentID string  `json:"pagoId"`
	Amount    float64 `json:"importe"`
	Currency  string  `json:"moneda"`
	Reference string  `json:"referencia"`
}

type refundRequest struct {
	PaymentID string  `json:"pagoId"`
	Amount    float64 `json:"importe"`
	Currency  string  `json:"moneda"`
	Reason    string  `json:"motivo"`
}

type preferenceRequest struct {
	CheckoutID string  `json:"checkoutId"`
	Amount     float64 `json:"importe"`
	Currency   string  `json:"moneda"`
	Title      string  `json:"titulo"`
}

type preferenceResult struct {
	ID         string  `json:"id"`
	CheckoutID string  `json:"checkoutId"`
	Amount     float64 `json:"importe"`
	Currency   string  `json:"moneda"`
	Status     string  `json:"estado"`
	InitPoint  string  `json:"urlPago"`
}

type mpPayment struct {
	ID                json.RawMessage `json:"id"`
	Status            string          `json:"status"`
	ExternalReference string          `json:"external_reference"`
	TransactionAmount float64         `json:"transaction_amount"`
	CurrencyID        string          `json:"currency_id"`
	LiveMode          bool            `json:"live_mode"`
}

func mpPaymentID(item mpPayment) string {
	var id string
	if json.Unmarshal(item.ID, &id) == nil {
		return id
	}
	return strings.Trim(string(item.ID), `"`)
}

type mpOrder struct {
	ID                string `json:"id"`
	Status            string `json:"status"`
	StatusDetail      string `json:"status_detail"`
	ExternalReference string `json:"external_reference"`
	TotalAmount       string `json:"total_amount"`
	TotalPaidAmount   string `json:"total_paid_amount"`
	Currency          string `json:"currency"`
	LiveMode          bool   `json:"live_mode"`
	Transactions      struct {
		Payments []struct {
			ID string `json:"id"`
		} `json:"payments"`
	} `json:"transactions"`
}

func (s *server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		provider := "mercadopago"
		if s.accessToken == "" {
			provider = "deshabilitado"
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "provider": provider, "mode": "test"})
	})
	mux.Handle("POST /payments/preferences", s.internal(http.HandlerFunc(s.createPreference)))
	mux.Handle("POST /payments/verify", s.internal(http.HandlerFunc(s.verifyMercadoPago)))
	mux.HandleFunc("POST /webhooks/mercadopago", s.mercadoPagoWebhook)
	mux.Handle("GET /payments/{id}", s.internal(http.HandlerFunc(s.getPayment)))
	mux.Handle("POST /payments/refunds", s.internal(http.HandlerFunc(s.refund)))
	return mux
}

func (s *server) mpRequest(ctx context.Context, method, path string, payload any, out any) error {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, "https://api.mercadopago.com"+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.accessToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Mercado Pago devolvió HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
}

func (s *server) createPreference(w http.ResponseWriter, r *http.Request) {
	var req preferenceRequest
	if decodeJSON(r, &req) != nil || s.accessToken == "" || !validID(req.CheckoutID) || !validAmount(req.Amount) || req.Currency != "ARS" {
		writeError(w, http.StatusBadRequest, "No se pudo crear el pago de prueba")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	var existing payment
	err := s.payments.FindOne(ctx, bson.M{"checkoutId": req.CheckoutID}).Decode(&existing)
	if err == nil && existing.InitPoint != "" && existing.Amount == req.Amount && existing.Currency == req.Currency {
		writeJSON(w, http.StatusOK, preferenceResult{ID: existing.PreferenceID, CheckoutID: existing.CheckoutID, Amount: existing.Amount, Currency: existing.Currency, Status: existing.Status, InitPoint: existing.InitPoint})
		return
	}
	if err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
		writeError(w, http.StatusServiceUnavailable, "No se pudo consultar el pago")
		return
	}
	returnURL := strings.TrimRight(s.publicURL, "/") + "/?checkout_return=1&checkout_id=" + req.CheckoutID
	if !strings.HasPrefix(returnURL, "https://") {
		writeError(w, http.StatusServiceUnavailable, "MELT_PUBLIC_URL debe ser una URL HTTPS pública")
		return
	}
	var mpResp struct {
		ID               string `json:"id"`
		InitPoint        string `json:"init_point"`
		SandboxInitPoint string `json:"sandbox_init_point"`
	}
	payload := map[string]any{"items": []any{map[string]any{"title": req.Title, "quantity": 1, "unit_price": req.Amount, "currency_id": req.Currency}}, "external_reference": req.CheckoutID, "back_urls": map[string]string{"success": returnURL, "failure": returnURL, "pending": returnURL}, "auto_return": "approved", "notification_url": strings.TrimRight(s.publicURL, "/") + "/api/v1/pagos/webhooks/mercadopago"}
	if err := s.mpRequest(ctx, http.MethodPost, "/checkout/preferences", payload, &mpResp); err != nil || mpResp.ID == "" {
		writeError(w, http.StatusBadGateway, "Mercado Pago no pudo iniciar el checkout de prueba")
		return
	}
	url := mpResp.SandboxInitPoint
	if !strings.HasPrefix(url, "https://") {
		writeError(w, http.StatusBadGateway, "Mercado Pago no devolvió una URL de checkout de prueba")
		return
	}
	doc := payment{ID: "mp-" + req.CheckoutID, CheckoutID: req.CheckoutID, IdempotencyKey: "checkout-" + req.CheckoutID, Amount: req.Amount, Currency: req.Currency, Status: "pending", CreatedAt: time.Now().UTC(), PreferenceID: mpResp.ID, InitPoint: url}
	if existing.ID != "" {
		_, err = s.payments.ReplaceOne(ctx, bson.M{"checkoutId": req.CheckoutID}, doc)
	} else {
		_, err = s.payments.InsertOne(ctx, doc)
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "No se pudo guardar el pago")
		return
	}
	writeJSON(w, http.StatusCreated, preferenceResult{ID: doc.PreferenceID, CheckoutID: doc.CheckoutID, Amount: doc.Amount, Currency: doc.Currency, Status: doc.Status, InitPoint: doc.InitPoint})
}

func validWebhookSignature(signature, requestID, dataID, secret string) bool {
	if signature == "" || requestID == "" || dataID == "" || secret == "" {
		return false
	}
	var ts, received string
	for _, part := range strings.Split(signature, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		switch key {
		case "ts":
			ts = value
		case "v1":
			received = strings.ToLower(value)
		}
	}
	if ts == "" || received == "" {
		return false
	}
	manifest := "id:" + strings.ToLower(dataID) + ";request-id:" + requestID + ";ts:" + ts + ";"
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(manifest))
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(received))
}

func (s *server) mercadoPagoWebhook(w http.ResponseWriter, r *http.Request) {
	dataID := strings.TrimSpace(r.URL.Query().Get("data.id"))
	if dataID == "" {
		dataID = strings.TrimSpace(r.URL.Query().Get("id"))
	}
	topic := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("type")))
	if topic == "" {
		topic = strings.ToLower(strings.TrimSpace(r.URL.Query().Get("topic")))
	}
	if topic != "payment" && topic != "order" && topic != "orders_v2" {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignored"})
		return
	}
	if !validWebhookSignature(r.Header.Get("x-signature"), r.Header.Get("x-request-id"), dataID, s.webhookSecret) {
		writeError(w, http.StatusUnauthorized, "Firma de notificación inválida")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if topic == "order" || topic == "orders_v2" {
		s.procesarWebhookOrder(w, ctx, dataID)
		return
	}
	var item mpPayment
	if err := s.mpRequest(ctx, http.MethodGet, "/v1/payments/"+dataID, nil, &item); err != nil {
		writeError(w, http.StatusBadGateway, "No se pudo consultar el pago notificado")
		return
	}
	// Pagos runs with MP_ENV=test and uses the configured test access token.
	// The resource is fetched from Mercado Pago and matched against the exact
	// checkout, amount, and currency below; live_mode in the webhook payload is
	// inconsistent for some sandbox Checkout Pro test-user transactions.
	var stored payment
	if err := s.payments.FindOne(ctx, bson.M{"checkoutId": item.ExternalReference}).Decode(&stored); err != nil || stored.Amount <= 0 || stored.Currency != item.CurrencyID || math.Abs(stored.Amount-item.TransactionAmount) >= 0.009 {
		writeError(w, http.StatusConflict, "El pago notificado no coincide con un checkout MELT")
		return
	}
	providerID := mpPaymentID(item)
	_, _ = s.payments.UpdateOne(ctx, bson.M{"_id": stored.ID}, bson.M{"$set": bson.M{"providerPaymentId": providerID, "estado": item.Status}})
	s.notificarReservas(w, ctx, item.ExternalReference, providerID)
}

func (s *server) procesarWebhookOrder(w http.ResponseWriter, ctx context.Context, orderID string) {
	var order mpOrder
	if err := s.mpRequest(ctx, http.MethodGet, "/v1/orders/"+orderID, nil, &order); err != nil {
		writeError(w, http.StatusBadGateway, "No se pudo consultar la orden notificada")
		return
	}
	if order.ID != orderID || !mpOrderIsTest(order) {
		writeError(w, http.StatusConflict, "La orden no pertenece al entorno de prueba")
		return
	}
	var stored payment
	if err := s.payments.FindOne(ctx, bson.M{"checkoutId": order.ExternalReference}).Decode(&stored); err != nil || stored.Amount <= 0 || stored.Currency != order.Currency {
		writeError(w, http.StatusConflict, "La orden no coincide con un checkout MELT")
		return
	}
	amount, amountErr := strconv.ParseFloat(order.TotalAmount, 64)
	paid, paidErr := strconv.ParseFloat(order.TotalPaidAmount, 64)
	if amountErr != nil || paidErr != nil || math.Abs(stored.Amount-amount) >= 0.009 || math.Abs(stored.Amount-paid) >= 0.009 {
		writeError(w, http.StatusConflict, "El importe acreditado no coincide con el checkout MELT")
		return
	}
	state := mpOrderState(order)
	transactionID := mpOrderPaymentID(order)
	_, _ = s.payments.UpdateOne(ctx, bson.M{"_id": stored.ID}, bson.M{"$set": bson.M{"providerOrderId": order.ID, "providerPaymentId": transactionID, "estado": state}})
	s.notificarReservas(w, ctx, order.ExternalReference, order.ID)
}

func (s *server) notificarReservas(w http.ResponseWriter, ctx context.Context, checkoutID, providerID string) {
	payload, _ := json.Marshal(map[string]string{"checkoutId": checkoutID, "pagoId": providerID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.reservasCallbackURL, bytes.NewReader(payload))
	if err != nil {
		writeError(w, http.StatusBadGateway, "No se pudo notificar a Reservas")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Melt-Internal-Key", s.internalKey)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "No se pudo notificar a Reservas")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		writeError(w, http.StatusBadGateway, "Reservas no pudo procesar el pago notificado")
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *server) verifyMercadoPago(w http.ResponseWriter, r *http.Request) {
	var req verifyRequest
	if decodeJSON(r, &req) != nil || s.accessToken == "" || !validID(req.PaymentID) || !validAmount(req.Amount) || req.Currency != "ARS" || !validID(req.Reference) {
		writeError(w, http.StatusBadRequest, "Solicitud de verificación inválida")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	if strings.HasPrefix(strings.ToUpper(req.PaymentID), "ORD") {
		s.verificarOrder(w, ctx, req)
		return
	}
	var item mpPayment
	if err := s.mpRequest(ctx, http.MethodGet, "/v1/payments/"+req.PaymentID, nil, &item); err != nil {
		writeError(w, http.StatusBadGateway, "No se pudo consultar el pago en Mercado Pago")
		return
	}
	providerID := mpPaymentID(item)
	valid := providerID != "" && item.ExternalReference == req.Reference && item.CurrencyID == req.Currency && math.Abs(item.TransactionAmount-req.Amount) < 0.009
	if !valid {
		writeError(w, http.StatusConflict, "El pago no coincide con el checkout")
		return
	}
	_, _ = s.payments.UpdateOne(ctx, bson.M{"checkoutId": req.Reference}, bson.M{"$set": bson.M{"providerPaymentId": providerID, "estado": item.Status}})
	writeJSON(w, http.StatusOK, map[string]any{"checkoutId": item.ExternalReference, "pagoId": providerID, "estado": item.Status, "aprobado": item.Status == "approved", "importe": item.TransactionAmount, "moneda": item.CurrencyID})
}

func (s *server) verificarOrder(w http.ResponseWriter, ctx context.Context, req verifyRequest) {
	var order mpOrder
	if err := s.mpRequest(ctx, http.MethodGet, "/v1/orders/"+req.PaymentID, nil, &order); err != nil {
		writeError(w, http.StatusBadGateway, "No se pudo consultar la orden en Mercado Pago")
		return
	}
	amount, amountErr := strconv.ParseFloat(order.TotalAmount, 64)
	paid, paidErr := strconv.ParseFloat(order.TotalPaidAmount, 64)
	if order.ID != req.PaymentID || !mpOrderIsTest(order) || order.ExternalReference != req.Reference || order.Currency != req.Currency || amountErr != nil || paidErr != nil || math.Abs(amount-req.Amount) >= 0.009 {
		writeError(w, http.StatusConflict, "La orden no coincide con el checkout MELT")
		return
	}
	approved := order.Status == "processed" && order.StatusDetail == "accredited" && math.Abs(paid-req.Amount) < 0.009
	state := mpOrderState(order)
	_, _ = s.payments.UpdateOne(ctx, bson.M{"checkoutId": req.Reference}, bson.M{"$set": bson.M{"providerOrderId": order.ID, "providerPaymentId": mpOrderPaymentID(order), "estado": state}})
	writeJSON(w, http.StatusOK, map[string]any{"checkoutId": order.ExternalReference, "pagoId": req.PaymentID, "estado": state, "aprobado": approved, "importe": amount, "moneda": order.Currency})
}

// Mercado Pago's sandbox Checkout Pro orders use the ORDTST identifier even
// though some webhook deliveries mark live_mode=true. The resource is still
// fetched with the configured test access token and independently matched to
// the pending MELT checkout before it can confirm a reservation.
func mpOrderIsTest(order mpOrder) bool {
	return !order.LiveMode || strings.HasPrefix(strings.ToUpper(order.ID), "ORDTST")
}

func mpOrderState(order mpOrder) string {
	if order.Status == "processed" && order.StatusDetail == "accredited" {
		return "approved"
	}
	if order.Status == "failed" || order.Status == "canceled" || order.Status == "refunded" {
		return "rejected"
	}
	return order.Status
}

func mpOrderPaymentID(order mpOrder) string {
	for _, payment := range order.Transactions.Payments {
		if payment.ID != "" {
			return payment.ID
		}
	}
	return ""
}

func (s *server) internal(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.internalKey == "" || r.Header.Get("X-Melt-Internal-Key") != s.internalKey {
			writeError(w, http.StatusUnauthorized, "Operación interna no autorizada")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) getPayment(w http.ResponseWriter, r *http.Request) {
	var item payment
	err := s.payments.FindOne(r.Context(), bson.M{"_id": r.PathValue("id")}).Decode(&item)
	if errors.Is(err, mongo.ErrNoDocuments) {
		writeError(w, http.StatusNotFound, "Operación no encontrada")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "No se pudo consultar la operación")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *server) verify(w http.ResponseWriter, r *http.Request) {
	var req verifyRequest
	if decodeJSON(r, &req) != nil || !validID(req.PaymentID) || !validAmount(req.Amount) || req.Currency != "ARS" || req.Reference == "" {
		writeError(w, http.StatusBadRequest, "Solicitud de verificación inválida")
		return
	}
	var item payment
	err := s.payments.FindOne(r.Context(), bson.M{"_id": req.PaymentID}).Decode(&item)
	if errors.Is(err, mongo.ErrNoDocuments) {
		writeJSON(w, http.StatusOK, map[string]any{"aprobado": false, "pagoId": req.PaymentID, "importe": 0, "moneda": req.Currency})
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "No se pudo verificar la operación")
		return
	}
	approved := item.Status == "approved" && item.CheckoutID == req.Reference && item.Currency == req.Currency && math.Abs(item.Amount-req.Amount) < 0.009
	writeJSON(w, http.StatusOK, map[string]any{"aprobado": approved, "pagoId": item.ID, "importe": item.Amount, "moneda": item.Currency})
}

func (s *server) refund(w http.ResponseWriter, r *http.Request) {
	var req refundRequest
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if decodeJSON(r, &req) != nil || key == "" || !validID(req.PaymentID) || !validAmount(req.Amount) || req.Currency != "ARS" || s.accessToken == "" {
		writeError(w, http.StatusBadRequest, "Solicitud de devolución inválida")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	var existing bson.M
	if err := s.refunds.FindOne(ctx, bson.M{"_id": key}).Decode(&existing); err == nil {
		writeJSON(w, http.StatusOK, existing)
		return
	} else if !errors.Is(err, mongo.ErrNoDocuments) {
		writeError(w, http.StatusServiceUnavailable, "No se pudo consultar la devolución")
		return
	}
	var item payment
	if err := s.payments.FindOne(ctx, bson.M{"estado": "approved", "$or": bson.A{bson.M{"providerPaymentId": req.PaymentID}, bson.M{"providerOrderId": req.PaymentID}}}).Decode(&item); err != nil {
		writeError(w, http.StatusNotFound, "No se encontró un pago aprobado para devolver")
		return
	}
	if item.Refunded+req.Amount > item.Amount+0.009 {
		writeError(w, http.StatusConflict, "El importe supera el saldo disponible para devolución")
		return
	}
	var mpResult struct {
		ID     json.RawMessage `json:"id"`
		Status string          `json:"status"`
	}
	path := "/v1/payments/" + req.PaymentID + "/refunds"
	var body []byte
	if item.ProviderOrderID != "" {
		path = "/v1/orders/" + item.ProviderOrderID + "/refund"
		if item.Refunded+req.Amount < item.Amount-0.009 {
			body, _ = json.Marshal(map[string]any{"transactions": []any{map[string]string{"id": item.ProviderID, "amount": fmt.Sprintf("%.2f", req.Amount)}}})
		} else {
			body = []byte(`{}`)
		}
	} else {
		body, _ = json.Marshal(map[string]float64{"amount": req.Amount})
	}
	mpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.mercadopago.com"+path, bytes.NewReader(body))
	if err != nil {
		writeError(w, http.StatusBadGateway, "No se pudo iniciar la devolución en Mercado Pago")
		return
	}
	mpReq.Header.Set("Authorization", "Bearer "+s.accessToken)
	mpReq.Header.Set("Content-Type", "application/json")
	mpReq.Header.Set("X-Idempotency-Key", key)
	resp, err := s.httpClient.Do(mpReq)
	if err != nil {
		writeError(w, http.StatusBadGateway, "No se pudo confirmar la devolución en Mercado Pago")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&mpResult) != nil || len(mpResult.ID) == 0 || string(mpResult.ID) == "null" {
		writeError(w, http.StatusBadGateway, "Mercado Pago no confirmó la devolución")
		return
	}
	refundID := strings.Trim(string(mpResult.ID), `"`)
	refund := bson.M{"_id": key, "pagoId": req.PaymentID, "devolucionId": refundID, "importe": req.Amount, "moneda": req.Currency, "estado": mpResult.Status, "motivo": req.Reason, "creadoEn": time.Now().UTC()}
	if _, err := s.refunds.InsertOne(ctx, refund); err != nil {
		if s.refunds.FindOne(ctx, bson.M{"_id": key}).Decode(&existing) == nil {
			writeJSON(w, http.StatusOK, existing)
			return
		}
		writeError(w, http.StatusServiceUnavailable, "La devolución fue aceptada pero no se pudo registrar el resultado")
		return
	}
	_, _ = s.payments.UpdateOne(ctx, bson.M{"_id": item.ID}, bson.M{"$inc": bson.M{"devuelto": req.Amount}})
	writeJSON(w, http.StatusCreated, refund)
}

func validAmount(amount float64) bool {
	return amount > 0 && !math.IsNaN(amount) && !math.IsInf(amount, 0)
}
func validID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, ch := range id {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_') {
			return false
		}
	}
	return true
}

func decodeJSON(r *http.Request, out any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	return dec.Decode(out)
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func main() {
	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "8085"
	}
	uri := strings.TrimSpace(os.Getenv("MONGODB_URI"))
	if uri == "" {
		log.Fatal("MONGODB_URI es obligatorio; configurar el clúster compartido de MongoDB Atlas")
	}
	dbName := strings.TrimSpace(os.Getenv("PAGOS_MONGODB_DATABASE"))
	if dbName == "" {
		dbName = "pagos_db"
	}
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("MP_ENV")))
	if mode == "" {
		mode = "test"
	}
	if mode != "test" {
		log.Fatal("Pagos sólo permite MP_ENV=test")
	}
	token := strings.TrimSpace(os.Getenv("MP_TEST_ACCESS_TOKEN"))
	if token != "" && !strings.HasPrefix(token, "TEST-") && !strings.HasPrefix(token, "APP_USR-") {
		log.Fatal("MP_TEST_ACCESS_TOKEN no tiene un formato reconocido")
	}
	publicURL := strings.TrimSpace(os.Getenv("MELT_PUBLIC_URL"))
	if token != "" && publicURL == "" {
		log.Fatal("MELT_PUBLIC_URL es obligatorio para el retorno de Mercado Pago")
	}
	webhookSecret := strings.TrimSpace(os.Getenv("MP_WEBHOOK_SECRET"))
	callbackURL := strings.TrimSpace(os.Getenv("RESERVAS_CALLBACK_URL"))
	if callbackURL == "" && publicURL != "" {
		callbackURL = strings.TrimRight(publicURL, "/") + "/api/v1/internal/reservas/pago-confirmado"
	}
	if token != "" && (webhookSecret == "" || callbackURL == "") {
		log.Fatal("MP_WEBHOOK_SECRET y la ruta de retorno interno de Reservas son obligatorios para Mercado Pago")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		log.Fatal("no se pudo configurar MongoDB para Pagos")
	}
	defer client.Disconnect(context.Background())
	if err := client.Ping(ctx, nil); err != nil {
		log.Fatal("no se pudo conectar Pagos con MongoDB")
	}
	db := client.Database(dbName)
	s := &server{payments: db.Collection("pagos_mercadopago"), refunds: db.Collection("devoluciones_mercadopago"), internalKey: strings.TrimSpace(os.Getenv("MELT_INTERNAL_KEY")), accessToken: token, publicURL: publicURL, webhookSecret: webhookSecret, reservasCallbackURL: callbackURL, httpClient: &http.Client{Timeout: 10 * time.Second}}
	if s.internalKey == "" {
		log.Fatal("MELT_INTERNAL_KEY es necesario para operaciones internas")
	}
	_, err = s.payments.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "idempotencyKey", Value: 1}}, Options: options.Index().SetUnique(true).SetName("uq_pago_idempotencia")})
	if err != nil {
		log.Fatal("no se pudo crear índice de idempotencia")
	}
	_, err = s.payments.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "checkoutId", Value: 1}}, Options: options.Index().SetUnique(true).SetName("uq_pago_checkout")})
	if err != nil {
		log.Fatal("no se pudo crear índice de checkout")
	}
	if err := http.ListenAndServe(":"+port, s.handler()); err != nil {
		log.Fatal(err)
	}
}
