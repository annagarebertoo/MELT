package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestSimulatorResultOnlyAllowsBackendConfiguredOutcomes(t *testing.T) {
	for _, result := range []string{"approved", "rejected"} {
		if !validSimulatorResult(result) {
			t.Fatalf("unexpected disallowed result %q", result)
		}
	}
	if validSimulatorResult("approved-from-client") || validID("../checkout") || validID("") || !validID("checkout-1") {
		t.Fatal("checkout identifiers must be bounded safe identifiers")
	}
}

func TestInternalPaymentRoutesRejectClientSelectedApproval(t *testing.T) {
	s := &server{internalKey: "local-internal-key", result: "approved"}
	for _, tc := range []struct {
		body, key string
		want      int
	}{
		{`{"checkoutId":"checkout-1","importe":300,"moneda":"ARS","resultado":"approved"}`, "local-internal-key", http.StatusBadRequest},
		{`{"checkoutId":"checkout-1","importe":300,"moneda":"ARS"}`, "", http.StatusUnauthorized},
	} {
		req := httptest.NewRequest(http.MethodPost, "/payments/simulate", strings.NewReader(tc.body))
		if tc.key != "" {
			req.Header.Set("X-Melt-Internal-Key", tc.key)
		}
		req.Header.Set("Idempotency-Key", "checkout-1")
		rec := httptest.NewRecorder()
		s.handler().ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Fatalf("got status %d, want %d: %s", rec.Code, tc.want, rec.Body.String())
		}
	}
}

func TestMongoSimulatorIdempotencyStatusRefundAndReverse(t *testing.T) {
	uri := strings.TrimSpace(os.Getenv("PAGOS_TEST_MONGODB_URI"))
	if uri == "" {
		t.Skip("definí PAGOS_TEST_MONGODB_URI para probar Pagos con MongoDB aislado")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatal("no se pudo configurar MongoDB de prueba")
	}
	defer client.Disconnect(context.Background())
	if err := client.Ping(ctx, nil); err != nil {
		t.Fatal("no se pudo conectar MongoDB de prueba")
	}
	db := client.Database(fmt.Sprintf("pagos_test_%d", time.Now().UnixNano()))
	defer db.Drop(context.Background())
	payments, refunds := db.Collection("pagos_simulados"), db.Collection("devoluciones_simuladas")
	for _, idx := range []mongo.IndexModel{
		{Keys: bson.D{{Key: "idempotencyKey", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "checkoutId", Value: 1}}, Options: options.Index().SetUnique(true)},
	} {
		if _, err := payments.Indexes().CreateOne(ctx, idx); err != nil {
			t.Fatal("no se pudo crear índice de prueba")
		}
	}
	s := &server{payments: payments, refunds: refunds, internalKey: "test-internal", result: "approved"}
	call := func(method, path, body, idem string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("X-Melt-Internal-Key", "test-internal")
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if idem != "" {
			req.Header.Set("Idempotency-Key", idem)
		}
		rec := httptest.NewRecorder()
		s.handler().ServeHTTP(rec, req)
		return rec
	}
	request := `{"checkoutId":"checkout-approved","importe":3000,"moneda":"ARS"}`
	created := call(http.MethodPost, "/payments/simulate", request, "checkout-approved")
	if created.Code != http.StatusCreated {
		t.Fatalf("simulación: status=%d body=%s", created.Code, created.Body.String())
	}
	var first payment
	if json.Unmarshal(created.Body.Bytes(), &first) != nil || first.Status != "approved" {
		t.Fatalf("respuesta de aprobación inválida: %s", created.Body.String())
	}
	retry := call(http.MethodPost, "/payments/simulate", request, "checkout-approved")
	var repeated payment
	if retry.Code != http.StatusOK || json.Unmarshal(retry.Body.Bytes(), &repeated) != nil || repeated.ID != first.ID {
		t.Fatalf("la repetición no devolvió la operación original: status=%d body=%s", retry.Code, retry.Body.String())
	}
	conflict := call(http.MethodPost, "/payments/simulate", `{"checkoutId":"checkout-approved","importe":3100,"moneda":"ARS"}`, "another-key")
	if conflict.Code != http.StatusConflict {
		t.Fatalf("checkout con importe alterado: status=%d", conflict.Code)
	}
	lookup := call(http.MethodGet, "/payments/"+first.ID, "", "")
	var lookedUp payment
	if lookup.Code != http.StatusOK || json.Unmarshal(lookup.Body.Bytes(), &lookedUp) != nil || lookedUp.ID != first.ID {
		t.Fatalf("consulta de operación: status=%d body=%s", lookup.Code, lookup.Body.String())
	}
	verifyBody := `{"pagoId":"` + first.ID + `","importe":3000,"moneda":"ARS","referencia":"checkout-approved"}`
	verify := call(http.MethodPost, "/payments/verify", verifyBody, "checkout-approved")
	var verification struct {
		Approved bool `json:"aprobado"`
	}
	if verify.Code != http.StatusOK || json.Unmarshal(verify.Body.Bytes(), &verification) != nil || !verification.Approved {
		t.Fatalf("verificación no coincide: status=%d body=%s", verify.Code, verify.Body.String())
	}
	reversed := call(http.MethodPost, "/payments/"+first.ID+"/reverse", `{}`, "")
	if reversed.Code != http.StatusOK {
		t.Fatalf("reversión: status=%d body=%s", reversed.Code, reversed.Body.String())
	}
	s.result = "rejected"
	rejected := call(http.MethodPost, "/payments/simulate", `{"checkoutId":"checkout-rejected","importe":300,"moneda":"ARS"}`, "checkout-rejected")
	var rejectedPayment payment
	if rejected.Code != http.StatusCreated || json.Unmarshal(rejected.Body.Bytes(), &rejectedPayment) != nil || rejectedPayment.Status != "rejected" {
		t.Fatalf("resultado rechazado: status=%d body=%s", rejected.Code, rejected.Body.String())
	}
	s.result = "approved"
	refundPayment := call(http.MethodPost, "/payments/simulate", `{"checkoutId":"checkout-refund","importe":1000,"moneda":"ARS"}`, "checkout-refund")
	var refundable payment
	if refundPayment.Code != http.StatusCreated || json.Unmarshal(refundPayment.Body.Bytes(), &refundable) != nil {
		t.Fatalf("pago de devolución: %s", refundPayment.Body.String())
	}
	refundBody := `{"pagoId":"` + refundable.ID + `","importe":300,"moneda":"ARS","motivo":"cancelacion_turno"}`
	refund := call(http.MethodPost, "/payments/refunds", refundBody, "refund-turno-test")
	if refund.Code != http.StatusCreated {
		t.Fatalf("devolución simulada: status=%d body=%s", refund.Code, refund.Body.String())
	}
	if repeatedRefund := call(http.MethodPost, "/payments/refunds", refundBody, "refund-turno-test"); repeatedRefund.Code != http.StatusOK {
		t.Fatalf("reintento de devolución: status=%d", repeatedRefund.Code)
	}
	if _, err := payments.UpdateOne(ctx, bson.M{"_id": refundable.ID}, bson.M{"$set": bson.M{"devuelto": 700}}); err != nil {
		t.Fatal(err)
	}
	excess := call(http.MethodPost, "/payments/refunds", `{"pagoId":"`+refundable.ID+`","importe":400,"moneda":"ARS","motivo":"test"}`, "refund-excess")
	if excess.Code != http.StatusConflict {
		t.Fatalf("no se limitó la devolución al saldo disponible: status=%d", excess.Code)
	}
}
