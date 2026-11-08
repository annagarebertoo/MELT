package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVerifyChecksApprovedAmountCurrencyAndReference(t *testing.T) {
	mp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("missing bearer token")
		}
		if r.URL.Path != "/v1/payments/123" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": 123, "status": "approved", "transaction_amount": 3000.0,
			"currency_id": "ARS", "external_reference": "cart-1",
		})
	}))
	defer mp.Close()
	s := &server{token: "test-token", baseURL: mp.URL, client: mp.Client()}
	req := httptest.NewRequest(http.MethodPost, "/payments/verify", strings.NewReader(
		`{"pagoId":"123","importe":3000,"moneda":"ARS","referencia":"cart-1"}`))
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d: %s", rec.Code, rec.Body.String())
	}
	var result verifyResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Approved || result.PaymentID != "123" {
		t.Fatalf("unexpected verification result: %+v", result)
	}
}

func TestVerifyRejectsWrongReference(t *testing.T) {
	mp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": 123, "status": "approved", "transaction_amount": 3000.0,
			"currency_id": "ARS", "external_reference": "different-cart",
		})
	}))
	defer mp.Close()
	s := &server{token: "test-token", baseURL: mp.URL, client: mp.Client()}
	req := httptest.NewRequest(http.MethodPost, "/payments/verify", strings.NewReader(
		`{"pagoId":"123","importe":3000,"moneda":"ARS","referencia":"cart-1"}`))
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, req)
	var result verifyResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Approved {
		t.Fatal("payment with mismatched external reference must not be approved")
	}
}

func TestRefundSendsIdempotencyKeyAndPartialAmount(t *testing.T) {
	mp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/payments/123/refunds" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("X-Idempotency-Key") != "refund-turno-1" {
			t.Errorf("idempotency key missing")
		}
		var body map[string]float64
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["amount"] != 900 {
			t.Errorf("expected partial refund 900, got %v", body["amount"])
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":123,"amount":900,"status":"approved"}`))
	}))
	defer mp.Close()
	s := &server{token: "test-token", baseURL: mp.URL, client: mp.Client()}
	req := httptest.NewRequest(http.MethodPost, "/payments/refunds", strings.NewReader(
		`{"pagoId":"123","importe":900,"moneda":"ARS","motivo":"cancelacion_turno"}`))
	req.Header.Set("Idempotency-Key", "refund-turno-1")
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("got status %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreatePreferenceUsesCartReferenceAndTestCheckoutURL(t *testing.T) {
	t.Setenv("MP_ENV", "test")
	t.Setenv("MP_RETURN_URL", "")
	mp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/checkout/preferences" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("X-Idempotency-Key") != "cart-cart-123" {
			t.Errorf("idempotency key missing: %q", r.Header.Get("X-Idempotency-Key"))
		}
		var body struct {
			ExternalReference string `json:"external_reference"`
			Items             []struct {
				UnitPrice float64 `json:"unit_price"`
				Currency  string  `json:"currency_id"`
			} `json:"items"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.ExternalReference != "cart-123" || len(body.Items) != 1 || body.Items[0].UnitPrice != 3000 || body.Items[0].Currency != "ARS" {
			t.Errorf("unexpected preference: %+v", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "pref-1", "init_point": "https://mercadopago.test/live", "sandbox_init_point": "https://mercadopago.test/sandbox"})
	}))
	defer mp.Close()
	s := &server{token: "test-token", baseURL: mp.URL, client: mp.Client()}
	req := httptest.NewRequest(http.MethodPost, "/payments/preferences", strings.NewReader(`{"importe":3000,"moneda":"ARS","referencia":"cart-123","titulo":"Seña de tratamiento"}`))
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("got status %d: %s", rec.Code, rec.Body.String())
	}
	var result map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["preferenciaId"] != "pref-1" || result["urlPago"] != "https://mercadopago.test/sandbox" {
		t.Fatalf("unexpected checkout response: %+v", result)
	}
}

func TestCreatePreferenceRejectsNonHTTPSReturnURL(t *testing.T) {
	t.Setenv("MP_RETURN_URL", "http://localhost:8080/")
	s := &server{token: "test-token", baseURL: "http://127.0.0.1:1", client: http.DefaultClient}
	req := httptest.NewRequest(http.MethodPost, "/payments/preferences", strings.NewReader(`{"importe":3000,"moneda":"ARS","referencia":"cart-123","titulo":"Seña"}`))
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("got status %d; want %d", rec.Code, http.StatusServiceUnavailable)
	}
}
