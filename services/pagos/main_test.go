package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMPOrderTestEnvironmentDetection(t *testing.T) {
	for _, tc := range []struct {
		name  string
		order mpOrder
		want  bool
	}{
		{name: "sandbox order marked live by webhook", order: mpOrder{ID: "ORDTST01M7W22Q1FD49CY7R1TKX5WG3K", LiveMode: true}, want: true},
		{name: "sandbox order marked as test", order: mpOrder{ID: "ORD01K9SN1Q5959CGN8QGW00EH7V5", LiveMode: false}, want: true},
		{name: "production order", order: mpOrder{ID: "ORD01K9SN1Q5959CGN8QGW00EH7V5", LiveMode: true}, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := mpOrderIsTest(tc.order); got != tc.want {
				t.Fatalf("mpOrderIsTest() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCheckoutProRoutesRequireInternalKeyAndExposeNoSimulator(t *testing.T) {
	s := &server{internalKey: "local-internal-key"}
	for _, tc := range []struct {
		method, path, body, key string
		want                    int
	}{
		{http.MethodPost, "/payments/preferences", `{"checkoutId":"checkout-1","importe":300,"moneda":"ARS"}`, "", http.StatusUnauthorized},
		{http.MethodPost, "/payments/preferences", `{"checkoutId":"checkout-1","importe":300,"moneda":"ARS"}`, "local-internal-key", http.StatusBadRequest},
		{http.MethodPost, "/payments/simulate", `{}`, "local-internal-key", http.StatusMethodNotAllowed},
		{http.MethodPost, "/payments/abc/reverse", `{}`, "local-internal-key", http.StatusNotFound},
	} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		if tc.key != "" {
			req.Header.Set("X-Melt-Internal-Key", tc.key)
		}
		rec := httptest.NewRecorder()
		s.handler().ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Fatalf("%s %s: got status %d, want %d: %s", tc.method, tc.path, rec.Code, tc.want, rec.Body.String())
		}
	}
}
