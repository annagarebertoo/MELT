package main

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFirmaDelWebhook(t *testing.T) {
	valida := firmar("ABC123", "req-1")
	casos := []struct {
		nombre                       string
		firma, requestID, id, secret string
		esperado                     bool
	}{
		{"válida (id en minúsculas en el manifiesto)", valida, "req-1", "ABC123", secretoWebhookPrueba, true},
		{"otro request-id", valida, "req-2", "ABC123", secretoWebhookPrueba, false},
		{"otro recurso", valida, "req-1", "OTRO", secretoWebhookPrueba, false},
		{"otro secreto", valida, "req-1", "ABC123", "otro", false},
		{"sin ts", strings.Replace(valida, "ts=1700000000,", "", 1), "req-1", "ABC123", secretoWebhookPrueba, false},
		{"sin v1", "ts=1700000000", "req-1", "ABC123", secretoWebhookPrueba, false},
		{"partes mal formadas se ignoran", "basura, " + valida, "req-1", "ABC123", secretoWebhookPrueba, true},
		{"vacía", "", "req-1", "ABC123", secretoWebhookPrueba, false},
		{"sin secreto configurado", valida, "req-1", "ABC123", "", false},
	}
	for _, c := range casos {
		if got := validWebhookSignature(c.firma, c.requestID, c.id, c.secret); got != c.esperado {
			t.Errorf("%s: obtuvo %v", c.nombre, got)
		}
	}
}

func TestReglasDeImporteEIdentificadores(t *testing.T) {
	for _, v := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		if validAmount(v) {
			t.Errorf("importe %v no debe aceptarse", v)
		}
	}
	if !validAmount(0.01) {
		t.Error("un importe positivo es válido")
	}
	for _, id := range []string{"", "con espacio", "acento-á", "a/b", strings.Repeat("x", 129)} {
		if validID(id) {
			t.Errorf("id %q no debe aceptarse", id)
		}
	}
	for _, id := range []string{"chk-1", "ORDTST_9", strings.Repeat("x", 128)} {
		if !validID(id) {
			t.Errorf("id %q debe aceptarse", id)
		}
	}
}

func TestEstadosDeOrdenYPagoDeMercadoPago(t *testing.T) {
	casos := map[[2]string]string{
		{"processed", "accredited"}: "approved",
		{"processed", "waiting"}:    "processed",
		{"failed", ""}:              "rejected",
		{"canceled", ""}:            "rejected",
		{"refunded", ""}:            "rejected",
		{"action_required", ""}:     "action_required",
	}
	for entrada, esperado := range casos {
		if got := mpOrderState(mpOrder{Status: entrada[0], StatusDetail: entrada[1]}); got != esperado {
			t.Errorf("%v → %s, esperado %s", entrada, got, esperado)
		}
	}
	var o mpOrder
	_ = json.Unmarshal([]byte(`{"transactions":{"payments":[{"id":""},{"id":"PAY-2"}]}}`), &o)
	if mpOrderPaymentID(o) != "PAY-2" {
		t.Errorf("debe tomar el primer id no vacío")
	}
	if mpOrderPaymentID(mpOrder{}) != "" {
		t.Errorf("sin transacciones no hay id")
	}
	if !mpOrderIsTest(mpOrder{ID: "ORDTST1", LiveMode: true}) || mpOrderIsTest(mpOrder{ID: "ORD1", LiveMode: true}) || !mpOrderIsTest(mpOrder{ID: "ORD1"}) {
		t.Errorf("detección de orden de prueba incorrecta")
	}
	for crudo, esperado := range map[string]string{`123`: "123", `"456"`: "456"} {
		if got := mpPaymentID(mpPayment{ID: json.RawMessage(crudo)}); got != esperado {
			t.Errorf("id %s → %s", crudo, got)
		}
	}
}

func TestSaludInformaProveedor(t *testing.T) {
	s := &server{}
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if !strings.Contains(rec.Body.String(), `"provider":"deshabilitado"`) {
		t.Fatalf("sin token el proveedor figura deshabilitado: %s", rec.Body.String())
	}
	s.accessToken = "TEST-x"
	rec = httptest.NewRecorder()
	s.handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if !strings.Contains(rec.Body.String(), `"provider":"mercadopago"`) {
		t.Fatalf("con token el proveedor es mercadopago: %s", rec.Body.String())
	}
}
