package main

// Pruebas del flujo de Pagos contra un Mercado Pago simulado (httptest) y una
// MongoDB descartable. Cubren reglas de negocio: solo vendedores de prueba,
// idempotencia por checkout, importes y moneda exactos, firma HMAC del Webhook,
// estados de órdenes y devoluciones parciales/idempotentes.
//
// Usan PAGOS_TEST_MONGODB_URI (o AUTH_TEST_MONGODB_URI, que ya existe en CI).
// Sin esa variable se omiten; las reglas puras se prueban igual en reglas_test.go.

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const claveInternaPrueba = "clave-interna-de-prueba"
const secretoWebhookPrueba = "secreto-webhook-de-prueba"

// mpFalso simula los endpoints de Mercado Pago que usa Pagos.
type mpFalso struct {
	mu          sync.Mutex
	vendedor    []string
	pagos       map[string]map[string]any
	ordenes     map[string]map[string]any
	devolucion  int
	falla       map[string]int // ruta → status HTTP forzado
	solicitudes []string
	cuerpos     map[string][]byte
	idempotency []string
}

func nuevoMPFalso() *mpFalso {
	return &mpFalso{vendedor: []string{"test_user"}, pagos: map[string]map[string]any{}, ordenes: map[string]map[string]any{}, falla: map[string]int{}, cuerpos: map[string][]byte{}}
}

func (m *mpFalso) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ruta := r.Method + " " + r.URL.Path
	m.solicitudes = append(m.solicitudes, ruta)
	cuerpo, _ := io.ReadAll(r.Body)
	m.cuerpos[ruta] = cuerpo
	if k := r.Header.Get("X-Idempotency-Key"); k != "" {
		m.idempotency = append(m.idempotency, k)
	}
	if status, ok := m.falla[ruta]; ok {
		w.WriteHeader(status)
		return
	}
	responder := func(v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	switch {
	case ruta == "GET /users/me":
		responder(map[string]any{"tags": m.vendedor})
	case ruta == "POST /checkout/preferences":
		responder(map[string]any{"id": "pref-1", "init_point": "https://www.mercadopago.com.ar/checkout/v1/redirect?pref_id=pref-1"})
	case strings.HasPrefix(ruta, "GET /checkout/preferences/"):
		responder(map[string]any{"init_point": "https://www.mercadopago.com.ar/checkout/v1/redirect?pref_id=renovada"})
	case strings.HasPrefix(ruta, "GET /v1/payments/"):
		id := strings.TrimPrefix(r.URL.Path, "/v1/payments/")
		if p, ok := m.pagos[id]; ok {
			responder(p)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	case strings.HasPrefix(ruta, "GET /v1/orders/"):
		id := strings.TrimPrefix(r.URL.Path, "/v1/orders/")
		if o, ok := m.ordenes[id]; ok {
			responder(o)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	case strings.HasSuffix(r.URL.Path, "/refunds") || strings.HasSuffix(r.URL.Path, "/refund"):
		m.devolucion++
		responder(map[string]any{"id": 9000 + m.devolucion, "status": "approved"})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// redirigirMP envía a la API simulada lo que Pagos dirige a api.mercadopago.com.
type redirigirMP struct{ destino *url.URL }

func (r redirigirMP) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host == "api.mercadopago.com" {
		copia := req.Clone(req.Context())
		copia.URL.Scheme, copia.URL.Host = r.destino.Scheme, r.destino.Host
		copia.Host = r.destino.Host
		return http.DefaultTransport.RoundTrip(copia)
	}
	return http.DefaultTransport.RoundTrip(req)
}

type entornoPagos struct {
	srv          *server
	mp           *mpFalso
	reservas     *httptest.Server
	notificados  []map[string]string
	estadoReserv int
	mu           sync.Mutex
}

func uriMongoPrueba(t *testing.T) string {
	t.Helper()
	for _, clave := range []string{"PAGOS_TEST_MONGODB_URI", "AUTH_TEST_MONGODB_URI", "MONGODB_TEST_URI"} {
		if v := strings.TrimSpace(os.Getenv(clave)); v != "" {
			if strings.Contains(v, "mongodb.net") {
				t.Fatalf("%s apunta a Atlas; usar solo MongoDB local descartable", clave)
			}
			return v
		}
	}
	t.Skip("sin MongoDB de prueba (PAGOS_TEST_MONGODB_URI/AUTH_TEST_MONGODB_URI)")
	return ""
}

func nuevoEntornoPagos(t *testing.T) *entornoPagos {
	t.Helper()
	uri := uriMongoPrueba(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cliente, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatal(err)
	}
	if err := cliente.Ping(ctx, nil); err != nil {
		t.Fatal(err)
	}
	db := cliente.Database(fmt.Sprintf("pagos_test_%d", time.Now().UnixNano()))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = db.Drop(ctx)
		_ = cliente.Disconnect(ctx)
	})
	e := &entornoPagos{mp: nuevoMPFalso(), estadoReserv: http.StatusOK}
	apiMP := httptest.NewServer(e.mp)
	t.Cleanup(apiMP.Close)
	e.reservas = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Melt-Internal-Key") != claveInternaPrueba {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var cuerpo map[string]string
		_ = json.NewDecoder(r.Body).Decode(&cuerpo)
		e.mu.Lock()
		e.notificados = append(e.notificados, cuerpo)
		estado := e.estadoReserv
		e.mu.Unlock()
		w.WriteHeader(estado)
	}))
	t.Cleanup(e.reservas.Close)
	destino, _ := url.Parse(apiMP.URL)
	e.srv = &server{
		payments: db.Collection("pagos_mercadopago"), refunds: db.Collection("devoluciones_mercadopago"),
		internalKey: claveInternaPrueba, accessToken: "TEST-0000000000000000-000000-abcdef", publicURL: "https://melt.example.test",
		webhookSecret: secretoWebhookPrueba, reservasCallbackURL: e.reservas.URL + "/internal/pagos/mercadopago",
		httpClient: &http.Client{Timeout: 5 * time.Second, Transport: redirigirMP{destino: destino}},
	}
	if _, err := e.srv.payments.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "checkoutId", Value: 1}}, Options: options.Index().SetUnique(true)}); err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *entornoPagos) pedir(t *testing.T, metodo, ruta string, cuerpo any, cabeceras map[string]string) (int, map[string]any) {
	t.Helper()
	var lector io.Reader
	if cuerpo != nil {
		datos, _ := json.Marshal(cuerpo)
		lector = bytes.NewReader(datos)
	}
	req := httptest.NewRequest(metodo, ruta, lector)
	req.Header.Set("X-Melt-Internal-Key", claveInternaPrueba)
	for k, v := range cabeceras {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.srv.handler().ServeHTTP(rec, req)
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	return rec.Code, resp
}

func firmar(dataID, requestID string) string {
	ts := "1700000000"
	mac := hmac.New(sha256.New, []byte(secretoWebhookPrueba))
	mac.Write([]byte("id:" + strings.ToLower(dataID) + ";request-id:" + requestID + ";ts:" + ts + ";"))
	return "ts=" + ts + ",v1=" + hex.EncodeToString(mac.Sum(nil))
}

func TestPreferenciaEsIdempotentePorCheckoutYSoloConVendedorDePrueba(t *testing.T) {
	e := nuevoEntornoPagos(t)
	pedido := map[string]any{"checkoutId": "chk-1", "importe": 3000.0, "moneda": "ARS", "titulo": "Seña MELT"}

	status, resp := e.pedir(t, http.MethodPost, "/payments/preferences", pedido, nil)
	if status != http.StatusCreated || resp["urlPago"] == "" || resp["estado"] != "pending" {
		t.Fatalf("primera preferencia: %d %v", status, resp)
	}
	status, resp = e.pedir(t, http.MethodPost, "/payments/preferences", pedido, nil)
	if status != http.StatusOK || resp["id"] != "pref-1" {
		t.Fatalf("la repetición debe devolver la misma preferencia: %d %v", status, resp)
	}
	creadas := 0
	for _, s := range e.mp.solicitudes {
		if s == "POST /checkout/preferences" {
			creadas++
		}
	}
	if creadas != 1 {
		t.Fatalf("se crearon %d preferencias en Mercado Pago; debe ser 1", creadas)
	}
	var cuerpoMP map[string]any
	_ = json.Unmarshal(e.mp.cuerpos["POST /checkout/preferences"], &cuerpoMP)
	if cuerpoMP["external_reference"] != "chk-1" || !strings.HasPrefix(cuerpoMP["notification_url"].(string), "https://melt.example.test/") {
		t.Fatalf("preferencia sin referencia o webhook correctos: %v", cuerpoMP)
	}

	// Un cambio de importe para el mismo checkout no reutiliza la preferencia anterior.
	pedido["importe"] = 3500.0
	if status, _ = e.pedir(t, http.MethodPost, "/payments/preferences", pedido, nil); status != http.StatusCreated {
		t.Fatalf("un importe distinto debe regenerar la preferencia, obtuvo %d", status)
	}

	e.mp.vendedor = []string{"real_seller"}
	pedido["checkoutId"] = "chk-2"
	if status, _ = e.pedir(t, http.MethodPost, "/payments/preferences", pedido, nil); status != http.StatusServiceUnavailable {
		t.Fatalf("un vendedor real debe rechazarse, obtuvo %d", status)
	}
}

func TestPreferenciaRechazaEntradasInvalidasYErroresDelProveedor(t *testing.T) {
	e := nuevoEntornoPagos(t)
	casos := []map[string]any{
		{"checkoutId": "chk-1", "importe": 0, "moneda": "ARS"},
		{"checkoutId": "chk-1", "importe": 100, "moneda": "USD"},
		{"checkoutId": "id con espacios", "importe": 100, "moneda": "ARS"},
		{"checkoutId": "chk-1", "importe": 100, "moneda": "ARS", "campoExtra": true},
	}
	for _, c := range casos {
		if status, _ := e.pedir(t, http.MethodPost, "/payments/preferences", c, nil); status != http.StatusBadRequest {
			t.Fatalf("%v debe ser 400, obtuvo %d", c, status)
		}
	}
	e.mp.falla["POST /checkout/preferences"] = http.StatusInternalServerError
	if status, _ := e.pedir(t, http.MethodPost, "/payments/preferences", map[string]any{"checkoutId": "chk-9", "importe": 100.0, "moneda": "ARS"}, nil); status != http.StatusBadGateway {
		t.Fatalf("un 500 de Mercado Pago debe responder 502, obtuvo %d", status)
	}
	e.srv.publicURL = "http://inseguro.local"
	delete(e.mp.falla, "POST /checkout/preferences")
	if status, _ := e.pedir(t, http.MethodPost, "/payments/preferences", map[string]any{"checkoutId": "chk-10", "importe": 100.0, "moneda": "ARS"}, nil); status != http.StatusServiceUnavailable {
		t.Fatalf("sin URL pública HTTPS debe rechazar, obtuvo %d", status)
	}
}

func TestPreferenciaSandboxRenuevaElEnlace(t *testing.T) {
	e := nuevoEntornoPagos(t)
	ctx := context.Background()
	_, err := e.srv.payments.InsertOne(ctx, payment{ID: "mp-chk-s", CheckoutID: "chk-s", Amount: 100, Currency: "ARS", Status: "pending", PreferenceID: "pref-s", InitPoint: "https://sandbox.mercadopago.com.ar/x"})
	if err != nil {
		t.Fatal(err)
	}
	status, resp := e.pedir(t, http.MethodPost, "/payments/preferences", map[string]any{"checkoutId": "chk-s", "importe": 100.0, "moneda": "ARS"}, nil)
	if status != http.StatusOK || !strings.Contains(resp["urlPago"].(string), "renovada") {
		t.Fatalf("debe renovar el enlace sandbox: %d %v", status, resp)
	}
}

func TestWebhookDePagoValidaFirmaImporteYNotificaAReservas(t *testing.T) {
	e := nuevoEntornoPagos(t)
	ctx := context.Background()
	if _, err := e.srv.payments.InsertOne(ctx, payment{ID: "mp-chk-w", CheckoutID: "chk-w", Amount: 3000, Currency: "ARS", Status: "pending"}); err != nil {
		t.Fatal(err)
	}
	e.mp.pagos["123"] = map[string]any{"id": 123, "status": "approved", "external_reference": "chk-w", "transaction_amount": 3000, "currency_id": "ARS"}

	ruta := "/webhooks/mercadopago?type=payment&data.id=123"
	if status, _ := e.pedir(t, http.MethodPost, ruta, nil, map[string]string{"x-request-id": "r1", "x-signature": "ts=1,v1=deadbeef"}); status != http.StatusUnauthorized {
		t.Fatalf("firma inválida debe ser 401, obtuvo %d", status)
	}
	if status, _ := e.pedir(t, http.MethodPost, ruta, nil, map[string]string{"x-request-id": "r1", "x-signature": firmar("123", "r1")}); status != http.StatusOK {
		t.Fatalf("webhook válido debe ser 200, obtuvo %d", status)
	}
	if len(e.notificados) != 1 || e.notificados[0]["checkoutId"] != "chk-w" || e.notificados[0]["pagoId"] != "123" {
		t.Fatalf("Reservas no recibió la confirmación: %v", e.notificados)
	}
	var guardado payment
	_ = e.srv.payments.FindOne(ctx, bson.M{"_id": "mp-chk-w"}).Decode(&guardado)
	if guardado.Status != "approved" || guardado.ProviderID != "123" {
		t.Fatalf("el pago no quedó aprobado: %+v", guardado)
	}

	// Importe adulterado: no confirma la reserva.
	e.mp.pagos["124"] = map[string]any{"id": "124", "status": "approved", "external_reference": "chk-w", "transaction_amount": 1, "currency_id": "ARS"}
	if status, _ := e.pedir(t, http.MethodPost, "/webhooks/mercadopago?type=payment&data.id=124", nil, map[string]string{"x-request-id": "r2", "x-signature": firmar("124", "r2")}); status != http.StatusConflict {
		t.Fatalf("un importe distinto debe ser 409, obtuvo %d", status)
	}
	// Tópicos ajenos se ignoran sin consultar al proveedor.
	if status, resp := e.pedir(t, http.MethodPost, "/webhooks/mercadopago?type=merchant_order&data.id=1", nil, nil); status != http.StatusOK || resp["status"] != "ignored" {
		t.Fatalf("tópico ajeno: %d %v", status, resp)
	}
	// Si Reservas falla, Mercado Pago recibe un error y reintentará la notificación.
	e.estadoReserv = http.StatusServiceUnavailable
	if status, _ := e.pedir(t, http.MethodPost, ruta, nil, map[string]string{"x-request-id": "r3", "x-signature": firmar("123", "r3")}); status != http.StatusBadGateway {
		t.Fatalf("si Reservas falla el webhook debe devolver 502 para que MP reintente, obtuvo %d", status)
	}
	// Proveedor caído al consultar el pago.
	e.mp.falla["GET /v1/payments/123"] = http.StatusServiceUnavailable
	if status, _ := e.pedir(t, http.MethodPost, ruta, nil, map[string]string{"x-request-id": "r4", "x-signature": firmar("123", "r4")}); status != http.StatusBadGateway {
		t.Fatalf("proveedor caído debe dar 502, obtuvo %d", status)
	}
}

func TestWebhookDeOrdenAceptaSoloOrdenesDePruebaConImporteExacto(t *testing.T) {
	e := nuevoEntornoPagos(t)
	ctx := context.Background()
	if _, err := e.srv.payments.InsertOne(ctx, payment{ID: "mp-chk-o", CheckoutID: "chk-o", Amount: 1500, Currency: "ARS", Status: "pending"}); err != nil {
		t.Fatal(err)
	}
	orden := func(id string, vivo bool, total, pagado string) map[string]any {
		return map[string]any{"id": id, "status": "processed", "status_detail": "accredited", "external_reference": "chk-o", "total_amount": total, "total_paid_amount": pagado, "currency": "ARS", "live_mode": vivo,
			"transactions": map[string]any{"payments": []any{map[string]any{"id": "PAY-" + id}}}}
	}
	e.mp.ordenes["ORDTST1"] = orden("ORDTST1", true, "1500.00", "1500.00")
	e.mp.ordenes["ORDREAL"] = orden("ORDREAL", true, "1500.00", "1500.00")
	e.mp.ordenes["ORDTST2"] = orden("ORDTST2", false, "1500.00", "10.00")

	firmarOrden := func(id string) map[string]string { return map[string]string{"x-request-id": "r-" + id, "x-signature": firmar(id, "r-"+id)} }
	if status, _ := e.pedir(t, http.MethodPost, "/webhooks/mercadopago?type=order&data.id=ORDTST1", nil, firmarOrden("ORDTST1")); status != http.StatusOK {
		t.Fatalf("orden de prueba válida debe confirmar, obtuvo %d", status)
	}
	var guardado payment
	_ = e.srv.payments.FindOne(ctx, bson.M{"_id": "mp-chk-o"}).Decode(&guardado)
	if guardado.Status != "approved" || guardado.ProviderOrderID != "ORDTST1" || guardado.ProviderID != "PAY-ORDTST1" {
		t.Fatalf("orden no registrada: %+v", guardado)
	}
	if status, _ := e.pedir(t, http.MethodPost, "/webhooks/mercadopago?type=order&data.id=ORDREAL", nil, firmarOrden("ORDREAL")); status != http.StatusConflict {
		t.Fatalf("una orden real debe rechazarse, obtuvo %d", status)
	}
	if status, _ := e.pedir(t, http.MethodPost, "/webhooks/mercadopago?type=orders_v2&data.id=ORDTST2", nil, firmarOrden("ORDTST2")); status != http.StatusConflict {
		t.Fatalf("un pago parcial debe rechazarse, obtuvo %d", status)
	}
}

func TestVerificacionDePagoYOrdenContraElCheckout(t *testing.T) {
	e := nuevoEntornoPagos(t)
	e.mp.pagos["555"] = map[string]any{"id": 555, "status": "approved", "external_reference": "chk-v", "transaction_amount": 900, "currency_id": "ARS"}
	status, resp := e.pedir(t, http.MethodPost, "/payments/verify", map[string]any{"pagoId": "555", "importe": 900.0, "moneda": "ARS", "referencia": "chk-v"}, nil)
	if status != http.StatusOK || resp["aprobado"] != true {
		t.Fatalf("pago coincidente debe aprobarse: %d %v", status, resp)
	}
	if status, _ = e.pedir(t, http.MethodPost, "/payments/verify", map[string]any{"pagoId": "555", "importe": 900.0, "moneda": "ARS", "referencia": "otro-checkout"}, nil); status != http.StatusConflict {
		t.Fatalf("otra referencia debe ser 409, obtuvo %d", status)
	}
	if status, _ = e.pedir(t, http.MethodPost, "/payments/verify", map[string]any{"pagoId": "999", "importe": 900.0, "moneda": "ARS", "referencia": "chk-v"}, nil); status != http.StatusBadGateway {
		t.Fatalf("pago inexistente en MP debe ser 502, obtuvo %d", status)
	}
	e.mp.ordenes["ORDTST9"] = map[string]any{"id": "ORDTST9", "status": "action_required", "external_reference": "chk-v", "total_amount": "900.00", "total_paid_amount": "0.00", "currency": "ARS"}
	status, resp = e.pedir(t, http.MethodPost, "/payments/verify", map[string]any{"pagoId": "ORDTST9", "importe": 900.0, "moneda": "ARS", "referencia": "chk-v"}, nil)
	if status != http.StatusOK || resp["aprobado"] != false || resp["estado"] != "action_required" {
		t.Fatalf("orden pendiente no debe aprobarse: %d %v", status, resp)
	}
	if status, _ = e.pedir(t, http.MethodPost, "/payments/verify", map[string]any{"pagoId": "ORDTST9", "importe": 901.0, "moneda": "ARS", "referencia": "chk-v"}, nil); status != http.StatusConflict {
		t.Fatalf("importe distinto en orden debe ser 409, obtuvo %d", status)
	}
}

func TestDevolucionEsIdempotenteYNoSuperaElSaldo(t *testing.T) {
	e := nuevoEntornoPagos(t)
	ctx := context.Background()
	if _, err := e.srv.payments.InsertOne(ctx, payment{ID: "mp-chk-d", CheckoutID: "chk-d", Amount: 1000, Currency: "ARS", Status: "approved", ProviderID: "777"}); err != nil {
		t.Fatal(err)
	}
	pedido := map[string]any{"pagoId": "777", "importe": 600.0, "moneda": "ARS", "motivo": "cancelación"}
	if status, _ := e.pedir(t, http.MethodPost, "/payments/refunds", pedido, nil); status != http.StatusBadRequest {
		t.Fatalf("sin Idempotency-Key debe ser 400, obtuvo %d", status)
	}
	status, resp := e.pedir(t, http.MethodPost, "/payments/refunds", pedido, map[string]string{"Idempotency-Key": "dev-1"})
	if status != http.StatusCreated || resp["estado"] != "approved" {
		t.Fatalf("primera devolución: %d %v", status, resp)
	}
	if status, _ = e.pedir(t, http.MethodPost, "/payments/refunds", pedido, map[string]string{"Idempotency-Key": "dev-1"}); status != http.StatusOK {
		t.Fatalf("la misma clave debe devolver el resultado guardado, obtuvo %d", status)
	}
	if e.mp.devolucion != 1 {
		t.Fatalf("Mercado Pago recibió %d devoluciones; debe ser 1", e.mp.devolucion)
	}
	if status, _ = e.pedir(t, http.MethodPost, "/payments/refunds", pedido, map[string]string{"Idempotency-Key": "dev-2"}); status != http.StatusConflict {
		t.Fatalf("600 + 600 supera 1000: debe ser 409, obtuvo %d", status)
	}
	if status, _ = e.pedir(t, http.MethodPost, "/payments/refunds", map[string]any{"pagoId": "000", "importe": 1.0, "moneda": "ARS"}, map[string]string{"Idempotency-Key": "dev-3"}); status != http.StatusNotFound {
		t.Fatalf("pago no aprobado debe ser 404, obtuvo %d", status)
	}
	var guardado payment
	_ = e.srv.payments.FindOne(ctx, bson.M{"_id": "mp-chk-d"}).Decode(&guardado)
	if guardado.Refunded != 600 {
		t.Fatalf("saldo devuelto incorrecto: %v", guardado.Refunded)
	}
	if len(e.mp.idempotency) == 0 || e.mp.idempotency[0] != "dev-1" {
		t.Fatalf("la clave de idempotencia no se envió a Mercado Pago: %v", e.mp.idempotency)
	}
}

func TestDevolucionDeOrdenParcialYTotal(t *testing.T) {
	e := nuevoEntornoPagos(t)
	ctx := context.Background()
	if _, err := e.srv.payments.InsertOne(ctx, payment{ID: "mp-chk-od", CheckoutID: "chk-od", Amount: 1000, Currency: "ARS", Status: "approved", ProviderID: "PAY-1", ProviderOrderID: "ORDTST5"}); err != nil {
		t.Fatal(err)
	}
	if status, _ := e.pedir(t, http.MethodPost, "/payments/refunds", map[string]any{"pagoId": "ORDTST5", "importe": 300.0, "moneda": "ARS"}, map[string]string{"Idempotency-Key": "od-1"}); status != http.StatusCreated {
		t.Fatalf("devolución parcial de orden: %d", status)
	}
	var parcial map[string]any
	_ = json.Unmarshal(e.mp.cuerpos["POST /v1/orders/ORDTST5/refund"], &parcial)
	if parcial["transactions"] == nil {
		t.Fatalf("la devolución parcial debe indicar la transacción e importe: %s", e.mp.cuerpos["POST /v1/orders/ORDTST5/refund"])
	}
	if status, _ := e.pedir(t, http.MethodPost, "/payments/refunds", map[string]any{"pagoId": "ORDTST5", "importe": 700.0, "moneda": "ARS"}, map[string]string{"Idempotency-Key": "od-2"}); status != http.StatusCreated {
		t.Fatalf("devolución del saldo restante: %d", status)
	}
	if string(e.mp.cuerpos["POST /v1/orders/ORDTST5/refund"]) != "{}" {
		t.Fatalf("la devolución total de una orden se envía sin transacciones: %s", e.mp.cuerpos["POST /v1/orders/ORDTST5/refund"])
	}
	e.mp.falla["POST /v1/orders/ORDTST5/refund"] = http.StatusInternalServerError
	if status, _ := e.pedir(t, http.MethodPost, "/payments/refunds", map[string]any{"pagoId": "ORDTST5", "importe": 0.01, "moneda": "ARS"}, map[string]string{"Idempotency-Key": "od-3"}); status != http.StatusConflict && status != http.StatusBadGateway {
		t.Fatalf("sin saldo o con error del proveedor no debe aceptarse, obtuvo %d", status)
	}
}

func TestConsultaYVerificacionInternaDeOperaciones(t *testing.T) {
	e := nuevoEntornoPagos(t)
	ctx := context.Background()
	if _, err := e.srv.payments.InsertOne(ctx, payment{ID: "mp-chk-q", CheckoutID: "chk-q", Amount: 50, Currency: "ARS", Status: "approved"}); err != nil {
		t.Fatal(err)
	}
	if status, resp := e.pedir(t, http.MethodGet, "/payments/mp-chk-q", nil, nil); status != http.StatusOK || resp["checkoutId"] != "chk-q" {
		t.Fatalf("consulta: %d %v", status, resp)
	}
	if status, _ := e.pedir(t, http.MethodGet, "/payments/no-existe", nil, nil); status != http.StatusNotFound {
		t.Fatalf("inexistente debe ser 404, obtuvo %d", status)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/payments/verify", strings.NewReader(`{"pagoId":"mp-chk-q","importe":50,"moneda":"ARS","referencia":"chk-q"}`))
	e.srv.verify(rec, req)
	if !strings.Contains(rec.Body.String(), `"aprobado":true`) {
		t.Fatalf("verificación interna: %s", rec.Body.String())
	}
	rec = httptest.NewRecorder()
	e.srv.verify(rec, httptest.NewRequest(http.MethodPost, "/payments/verify", strings.NewReader(`{"pagoId":"otro","importe":50,"moneda":"ARS","referencia":"chk-q"}`)))
	if !strings.Contains(rec.Body.String(), `"aprobado":false`) {
		t.Fatalf("operación inexistente no se aprueba: %s", rec.Body.String())
	}
	rec = httptest.NewRecorder()
	e.srv.verify(rec, httptest.NewRequest(http.MethodPost, "/payments/verify", strings.NewReader(`{"pagoId":"x","importe":-1,"moneda":"ARS","referencia":"chk-q"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("importe negativo debe ser 400, obtuvo %d", rec.Code)
	}
}
