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
	return mux
}

func (s *server) verify(w http.ResponseWriter, r *http.Request) {
	var req verifyRequest
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.PaymentID) == "" ||
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
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.PaymentID) == "" ||
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
	writeJSON(w, http.StatusAccepted, map[string]any{
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
		token: strings.TrimSpace(os.Getenv("MP_ACCESS_TOKEN")),
		baseURL: baseURL,
		client: &http.Client{Timeout: 8 * time.Second},
	}
	if os.Getenv("MP_ENV") != "test" {
		log.Fatal("MP_ENV debe ser 'test'; este servicio está limitado al entorno de pruebas")
	}
	log.Printf("Servicio pagos Mercado Pago (modo pruebas) escuchando en :%s", port)
	if err := http.ListenAndServe(":"+port, s.handler()); err != nil {
		log.Fatal(err)
	}
}
