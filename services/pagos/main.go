package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const defaultMPBaseURL = "https://api.mercadopago.com"

type server struct {
	token   string
	baseURL string
	client  *http.Client
}

type verifyRequest struct {
	PaymentID string  `json:"pagoId"`
	Amount    float64 `json:"importe"`
	Currency  string  `json:"moneda"`
	Reference string  `json:"referencia"`
}

type verifyResponse struct {
	Approved  bool    `json:"aprobado"`
	PaymentID string  `json:"pagoId"`
	Amount    float64 `json:"importe"`
	Currency  string  `json:"moneda"`
}

type refundRequest struct {
	PaymentID string  `json:"pagoId"`
	Amount    float64 `json:"importe"`
	Currency  string  `json:"moneda"`
	Reason    string  `json:"motivo"`
}

type preferenceRequest struct {
	Amount    float64 `json:"importe"`
	Currency  string  `json:"moneda"`
	Reference string  `json:"referencia"`
	Title     string  `json:"titulo"`
}

type preferenceResponse struct {
	ID               string `json:"id"`
	InitPoint        string `json:"init_point"`
	SandboxInitPoint string `json:"sandbox_init_point"`
}

type mpPayment struct {
	ID                json.RawMessage `json:"id"`
	Status            string          `json:"status"`
	TransactionAmount float64         `json:"transaction_amount"`
	CurrencyID        string          `json:"currency_id"`
	ExternalReference string          `json:"external_reference"`
}

func (s *server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "provider": "mercadopago", "mode": "test", "configured": s.token != ""})
	})
	mux.HandleFunc("POST /payments/verify", s.verify)
	mux.HandleFunc("POST /payments/refunds", s.refund)
	mux.HandleFunc("POST /payments/preferences", s.createPreference)
	return mux
}

func (s *server) createPreference(w http.ResponseWriter, r *http.Request) {
	var req preferenceRequest
	if err := decodeJSON(r, &req); err != nil || !validAmount(req.Amount) || req.Currency != "ARS" ||
		!validReference(req.Reference) || strings.TrimSpace(req.Title) == "" || len(req.Title) > 120 {
		writeError(w, http.StatusBadRequest, "Solicitud de pago inválida")
		return
	}
	body := map[string]any{
		"items": []map[string]any{{
			"title": strings.TrimSpace(req.Title), "quantity": 1,
			"currency_id": req.Currency, "unit_price": req.Amount,
		}},
		"external_reference": req.Reference,
	}
	var returns map[string]string
	if returnURL := strings.TrimSpace(os.Getenv("MP_RETURN_URL")); returnURL != "" {
		parsed, err := url.Parse(returnURL)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
			writeError(w, http.StatusServiceUnavailable, "MP_RETURN_URL debe ser una URL HTTPS pública")
			return
		}
		returns = map[string]string{
			"success": returnURL + "?pago=aprobado&referencia=" + url.QueryEscape(req.Reference),
			"failure": returnURL + "?pago=rechazado&referencia=" + url.QueryEscape(req.Reference),
			"pending": returnURL + "?pago=pendiente&referencia=" + url.QueryEscape(req.Reference),
		}
		body["back_urls"] = returns
		body["auto_return"] = "approved"
	}
	encoded, _ := json.Marshal(body)
	var result preferenceResponse
	if err := s.request(r, http.MethodPost, "/checkout/preferences", encoded, "cart-"+req.Reference, &result); err != nil {
		writeError(w, http.StatusBadGateway, "Mercado Pago no pudo iniciar el pago")
		return
	}
	checkoutURL := result.InitPoint
	if strings.EqualFold(strings.TrimSpace(os.Getenv("MP_ENV")), "test") && result.SandboxInitPoint != "" {
		checkoutURL = result.SandboxInitPoint
	}
	if result.ID == "" || checkoutURL == "" {
		writeError(w, http.StatusBadGateway, "Mercado Pago no devolvió un enlace de pago")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"preferenciaId": result.ID, "urlPago": checkoutURL})
}

func validAmount(amount float64) bool {
	return amount > 0 && !math.IsNaN(amount) && !math.IsInf(amount, 0)
}

func validReference(reference string) bool {
	if reference == "" || len(reference) > 128 {
		return false
	}
	for _, char := range reference {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '-' || char == '_') {
			return false
		}
	}
	return true
}

func (s *server) verify(w http.ResponseWriter, r *http.Request) {
	var req verifyRequest
	if err := decodeJSON(r, &req); err != nil || !validPaymentID(req.PaymentID) ||
		strings.TrimSpace(req.Reference) == "" || req.Amount <= 0 || req.Currency != "ARS" {
		writeError(w, http.StatusBadRequest, "Solicitud de verificación inválida")
		return
	}
	var payment mpPayment
	if err := s.request(r, http.MethodGet, "/v1/payments/"+req.PaymentID, nil, "", &payment); err != nil {
		writeError(w, http.StatusBadGateway, "Mercado Pago no pudo verificar el pago")
		return
	}
	paymentID := strings.Trim(strings.TrimSpace(string(payment.ID)), "\"")
	approved := payment.Status == "approved" &&
		paymentID == req.PaymentID &&
		payment.CurrencyID == req.Currency &&
		payment.ExternalReference == req.Reference &&
		math.Abs(payment.TransactionAmount-req.Amount) < 0.009
	writeJSON(w, http.StatusOK, verifyResponse{
		Approved: approved, PaymentID: paymentID,
		Amount: payment.TransactionAmount, Currency: payment.CurrencyID,
	})
}

func (s *server) refund(w http.ResponseWriter, r *http.Request) {
	var req refundRequest
	if err := decodeJSON(r, &req); err != nil || !validPaymentID(req.PaymentID) ||
		req.Amount <= 0 || req.Currency != "ARS" {
		writeError(w, http.StatusBadRequest, "Solicitud de devolución inválida")
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		writeError(w, http.StatusBadRequest, "Falta el encabezado Idempotency-Key")
		return
	}
	body, _ := json.Marshal(map[string]float64{"amount": req.Amount})
	var result map[string]any
	path := "/v1/payments/" + req.PaymentID + "/refunds"
	if err := s.request(r, http.MethodPost, path, body, key, &result); err != nil {
		writeError(w, http.StatusBadGateway, "Mercado Pago no confirmó la devolución")
		return
	}
	if result["status"] != "approved" {
		writeError(w, http.StatusBadGateway, "Mercado Pago recibió la solicitud, pero la devolución todavía no figura aprobada")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"estado": "aceptada", "pagoId": req.PaymentID, "importe": req.Amount,
		"moneda": req.Currency, "resultado": result,
	})
}

func (s *server) request(r *http.Request, method, path string, body []byte, idempotencyKey string, out any) error {
	if s.token == "" {
		return errors.New("MP_ACCESS_TOKEN no está configurado")
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	ctx, cancel := context.WithTimeout(r.Context(), 7*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, s.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if idempotencyKey != "" {
		req.Header.Set("X-Idempotency-Key", idempotencyKey)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Mercado Pago respondió HTTP %d", resp.StatusCode)
	}
	if out == nil {
		_, err = io.Copy(io.Discard, resp.Body)
		return err
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func validPaymentID(id string) bool {
	if id == "" {
		return false
	}
	for _, ch := range id {
		if ch < 48 || ch > 57 {
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
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("MP_BASE_URL")), "/")
	if baseURL == "" {
		baseURL = defaultMPBaseURL
	}
	s := &server{
		token:   strings.TrimSpace(os.Getenv("MP_ACCESS_TOKEN")),
		baseURL: baseURL,
		client:  &http.Client{Timeout: 8 * time.Second},
	}
	if os.Getenv("MP_ENV") != "test" {
		log.Fatal("MP_ENV debe ser 'test'; este servicio está limitado al entorno de pruebas")
	}
	log.Printf("Servicio pagos Mercado Pago (modo pruebas) escuchando en :%s", port)
	if err := http.ListenAndServe(":"+port, s.handler()); err != nil {
		log.Fatal(err)
	}
}
