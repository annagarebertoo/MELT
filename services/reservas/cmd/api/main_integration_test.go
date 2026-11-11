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

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestReservasMongoIntegrationFlujoCompleto(t *testing.T) {
	uri := strings.TrimSpace(os.Getenv("RESERVAS_TEST_MONGODB_URI"))
	if uri == "" {
		t.Skip("definí RESERVAS_TEST_MONGODB_URI para ejecutar la integración con MongoDB")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatal("no se pudo configurar MongoDB de prueba")
	}
	defer client.Disconnect(context.Background())
	if err := client.Ping(ctx, nil); err != nil {
		t.Fatal("no se pudo conectar con MongoDB de prueba")
	}
	db := client.Database(fmt.Sprintf("reservas_test_%d", time.Now().UnixNano()))
	defer db.Drop(context.Background())

	agendas := db.Collection("agendas_semanales")
	turnos := db.Collection("turnos")
	bloqueos := db.Collection("bloqueos_turnos")
	carritos := db.Collection("carritos_bloqueos")
	checkouts := db.Collection("checkouts")
	if _, err := checkouts.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "clienteId", Value: 1}, {Key: "idempotencyKey", Value: 1}}, Options: options.Index().SetUnique(true)}); err != nil {
		t.Fatal("no se pudo crear el índice de idempotencia de checkout")
	}
	if _, err := bloqueos.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "profesionalId", Value: 1}, {Key: "minuto", Value: 1}}, Options: options.Index().SetUnique(true)}); err != nil {
		t.Fatal("no se pudo crear el índice de concurrencia")
	}
	if _, err := bloqueos.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "clienteId", Value: 1}, {Key: "categoriaId", Value: 1}, {Key: "minuto", Value: 1}}, Options: options.Index().SetUnique(true).SetPartialFilterExpression(bson.M{"clienteId": bson.M{"$exists": true}, "categoriaId": bson.M{"$exists": true}})}); err != nil {
		t.Fatal("no se pudo crear el índice de categoría/cliente")
	}
	if _, err := bloqueos.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "expiraEn", Value: 1}}, Options: options.Index().SetExpireAfterSeconds(0)}); err != nil {
		t.Fatal("no se pudo crear el vencimiento automático de bloqueos")
	}
	if _, err := carritos.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "expiraEn", Value: 1}}, Options: options.Index().SetExpireAfterSeconds(0)}); err != nil {
		t.Fatal("no se pudo crear el vencimiento automático de carritos")
	}
	if _, err := turnos.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "profesionalId", Value: 1}, {Key: "inicio", Value: 1}}}); err != nil {
		t.Fatal("no se pudo crear el índice de turnos")
	}

	loc := zonaHorariaReservas()
	fecha := time.Now().In(loc).AddDate(0, 0, 7)
	fechaStr := fecha.Format("2006-01-02")
	inicioLocal := time.Date(fecha.Year(), fecha.Month(), fecha.Day(), 14, 0, 0, 0, loc)
	if _, err := agendas.InsertOne(ctx, agendaSemanal{ID: agendaControlID("prof-1", int(inicioLocal.Weekday())), ProfesionalID: "prof-1", DiaSemana: int(inicioLocal.Weekday()), HoraInicio: "08:00", HoraFin: "20:00", Bloques: []bloqueAgenda{{Inicio: "08:00", Fin: "12:00"}, {Inicio: "13:00", Fin: "20:00"}}, Activo: true}); err != nil {
		t.Fatal("no se pudo crear la agenda de prueba")
	}
	if _, err := agendas.InsertOne(ctx, agendaSemanal{ID: agendaControlID("prof-2", int(inicioLocal.Weekday())), ProfesionalID: "prof-2", DiaSemana: int(inicioLocal.Weekday()), HoraInicio: "08:00", HoraFin: "20:00", Activo: true}); err != nil {
		t.Fatal("no se pudo crear la agenda del segundo profesional")
	}

	tratamiento := tratamientoCatalogo{ID: "facial-01", Nombre: "Higiene facial", CategoriaID: "categoria-facial", DuracionMinutos: 60, ProfesionalesIDs: []string{"prof-1", "prof-2"}, Precio: 10000, Moneda: "ARS"}
	tratamientoDos := tratamientoCatalogo{ID: "masaje-02", Nombre: "Masaje descontracturante", CategoriaID: "categoria-masaje", DuracionMinutos: 45, ProfesionalesIDs: []string{"prof-2"}, Precio: 10000, Moneda: "ARS"}
	catalogo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/internal/profesionales/por-cuenta/") {
			if r.Header.Get("X-Melt-Internal-Key") != "integration-internal" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			accountID := strings.TrimPrefix(r.URL.Path, "/internal/profesionales/por-cuenta/")
			profileID := map[string]string{"auth-prof-1": "prof-1", "auth-prof-2": "prof-2", "otro-profesional": "prof-2"}[accountID]
			if profileID == "" {
				http.NotFound(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"profesionalId": profileID})
			return
		}
		if strings.HasPrefix(r.URL.Path, "/profesionales/") {
			id := strings.TrimPrefix(r.URL.Path, "/profesionales/")
			_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "activo": true, "tratamientosIds": []string{"facial-01", "masaje-02"}})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/masaje-02") {
			_ = json.NewEncoder(w).Encode(tratamientoDos)
			return
		}
		_ = json.NewEncoder(w).Encode(tratamiento)
	}))
	defer catalogo.Close()
	beneficios := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/reservar") {
			var req struct {
				Precio float64 `json:"precio"`
			}
			if json.NewDecoder(r.Body).Decode(&req) != nil || req.Precio <= 0 {
				http.Error(w, "invalid benefit", http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"canje": map[string]any{"id": strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/internal/canjes/"), "/reservar"), "descuento": 1000, "precioFinal": req.Precio - 1000}})
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer beneficios.Close()
	var refundCalls, preferenceCalls int
	simulateResult := "approved"
	expireHoldID := ""
	pagos := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/payments/simulate":
			preferenceCalls++
			var req simulatePaymentRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Amount <= 0 || req.Currency != "ARS" || req.CheckoutID == "" || r.Header.Get("X-Melt-Internal-Key") != "integration-internal" {
				http.Error(w, "invalid simulation", http.StatusBadRequest)
				return
			}
			payment := simulatedPayment{ID: "pago-" + req.CheckoutID, CheckoutID: req.CheckoutID, Amount: req.Amount, Currency: req.Currency, Status: simulateResult}
			if expireHoldID != "" {
				_, _ = carritos.UpdateOne(ctx, bson.M{"_id": expireHoldID}, bson.M{"$set": bson.M{"expiraEn": time.Now().UTC().Add(-time.Minute)}})
				expireHoldID = ""
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(payment)
		case "/payments/refunds":
			refundCalls++
			w.WriteHeader(http.StatusCreated)
		default:
			if strings.HasSuffix(r.URL.Path, "/reverse") {
				w.WriteHeader(http.StatusOK)
			} else {
				http.NotFound(w, r)
			}
		}
	}))
	defer pagos.Close()
	outbox := db.Collection("outbox_eventos")
	s := &servidorReservas{mongo: client, agendas: agendas, turnos: turnos, bloqueos: bloqueos, carritos: carritos, checkouts: checkouts, outbox: outbox, catalogoURL: catalogo.URL, pagosURL: pagos.URL, beneficiosURL: beneficios.URL, internalKey: "integration-internal", httpClient: &http.Client{Timeout: 3 * time.Second}}
	gin.SetMode(gin.TestMode)
	router := s.router()
	turnoProfesionalUno := turno{ID: "turno-profesional-uno", ClienteID: "cliente-test", ProfesionalID: "prof-1", TratamientoID: "facial-01", Inicio: inicioLocal, Fin: inicioLocal.Add(time.Hour), Estado: "completado"}
	turnoProfesionalDos := turno{ID: "turno-profesional-dos", ClienteID: "cliente-test-2", ProfesionalID: "prof-2", TratamientoID: "facial-01", Inicio: inicioLocal, Fin: inicioLocal.Add(time.Hour), Estado: "completado"}
	if _, err := turnos.InsertMany(ctx, []any{turnoProfesionalUno, turnoProfesionalDos}); err != nil {
		t.Fatal("no se pudieron preparar turnos de aislamiento profesional")
	}
	for _, tc := range []struct {
		accountID, wantProfessional string
	}{{"auth-prof-1", "prof-1"}, {"auth-prof-2", "prof-2"}} {
		listed := integrationRequestAs(router, http.MethodGet, "/turnos", "", tc.accountID, "profesional")
		if listed.Code != http.StatusOK {
			t.Fatalf("consulta profesional %s: status=%d body=%s", tc.accountID, listed.Code, listed.Body.String())
		}
		var result struct {
			Turnos []turno `json:"turnos"`
		}
		if err := json.Unmarshal(listed.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if len(result.Turnos) != 1 || result.Turnos[0].ProfesionalID != tc.wantProfessional {
			t.Fatalf("%s recibió turnos ajenos: %+v", tc.accountID, result.Turnos)
		}
	}
	unknownProfessional := integrationRequestAs(router, http.MethodGet, "/turnos", "", "unlinked-professional", "profesional")
	if unknownProfessional.Code != http.StatusForbidden {
		t.Fatalf("cuenta profesional sin vínculo: status=%d", unknownProfessional.Code)
	}
	agendaDia := (int(inicioLocal.Weekday()) + 1) % 7
	forgedAgenda := integrationRequestAs(router, http.MethodPut, "/agenda", fmt.Sprintf(`{"profesionalId":"prof-2","diaSemana":%d,"horaInicio":"09:00","horaFin":"10:00"}`, agendaDia), "auth-prof-1", "profesional")
	if forgedAgenda.Code != http.StatusOK {
		t.Fatalf("agenda profesional: status=%d body=%s", forgedAgenda.Code, forgedAgenda.Body.String())
	}
	var ownAgenda agendaSemanal
	if err := agendas.FindOne(ctx, bson.M{"profesionalId": "prof-1", "diaSemana": agendaDia}).Decode(&ownAgenda); err != nil {
		t.Fatalf("agenda se guardó bajo el perfil resuelto: %v", err)
	}
	if err := agendas.FindOne(ctx, bson.M{"profesionalId": "prof-2", "diaSemana": agendaDia}).Err(); err == nil {
		t.Fatal("el ID manipulado desde el cuerpo alteró agenda de otro profesional")
	}
	ownAgendaResponse := integrationRequestAs(router, http.MethodGet, "/agenda?profesionalId=prof-2", "", "auth-prof-1", "profesional")
	var ownAgendaResult struct {
		ProfesionalID string          `json:"profesionalId"`
		Dias          []agendaSemanal `json:"dias"`
	}
	if ownAgendaResponse.Code != http.StatusOK || json.Unmarshal(ownAgendaResponse.Body.Bytes(), &ownAgendaResult) != nil || ownAgendaResult.ProfesionalID != "prof-1" || len(ownAgendaResult.Dias) != 7 {
		t.Fatalf("consulta profesional permitió agenda ajena o incompleta: status=%d body=%s", ownAgendaResponse.Code, ownAgendaResponse.Body.String())
	}
	invalidAgenda := integrationRequestAs(router, http.MethodPut, "/agenda", fmt.Sprintf(`{"diaSemana":%d,"bloques":[{"inicio":"07:00","fin":"09:00"}]}`, agendaDia), "auth-prof-1", "profesional")
	if invalidAgenda.Code != http.StatusBadRequest {
		t.Fatalf("agenda antes de las 08:00: status=%d", invalidAgenda.Code)
	}
	query := fmt.Sprintf("/disponibilidad?profesionalId=prof-1&tratamientoId=facial-01&fecha=%s", fechaStr)
	antes := integrationRequest(router, http.MethodGet, query, "")
	if antes.Code != http.StatusOK {
		t.Fatalf("consulta inicial: status=%d body=%s", antes.Code, antes.Body.String())
	}
	var initial struct {
		Horarios []string `json:"horarios"`
	}
	if err := json.Unmarshal(antes.Body.Bytes(), &initial); err != nil {
		t.Fatal(err)
	}
	if !contieneHorario(initial.Horarios, "14:00") {
		t.Fatalf("el horario inicial no incluye 14:00: %v", initial.Horarios)
	}
	if preferenceCalls != 0 {
		t.Fatalf("consultar disponibilidad inició %d pagos", preferenceCalls)
	}
	if contieneHorario(initial.Horarios, "12:00") || contieneHorario(initial.Horarios, "12:15") {
		t.Fatalf("el horario de descanso no debería estar disponible: %v", initial.Horarios)
	}

	payload := fmt.Sprintf(`{"profesionalId":"prof-1","tratamientoId":"facial-01","inicio":%q,"diferirPago":true}`, inicioLocal.Format(time.RFC3339))
	results := make(chan *httptest.ResponseRecorder, 2)
	for range 2 {
		go func() { results <- integrationRequest(router, http.MethodPost, "/carrito/bloqueos", payload) }()
	}
	var bloqueoID string
	created, conflicts := 0, 0
	for range 2 {
		response := <-results
		switch response.Code {
		case http.StatusCreated:
			created++
			var result struct {
				Bloqueo bloqueoCarrito `json:"bloqueo"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			bloqueoID = result.Bloqueo.ID
			if result.Bloqueo.Sena != 3000 || result.Bloqueo.URLPago != "" || result.Bloqueo.PreferenciaID != "" || preferenceCalls != 0 {
				t.Fatalf("agregar al carrito debe retener sin iniciar el pago: %+v; preferencias=%d", result.Bloqueo, preferenceCalls)
			}
			restante := time.Until(result.Bloqueo.ExpiraEn)
			if restante < 9*time.Minute || restante > 10*time.Minute {
				t.Fatalf("duración del bloqueo=%s, se esperaban 10 minutos", restante)
			}
		case http.StatusConflict:
			conflicts++
		default:
			t.Fatalf("respuesta inesperada al reservar en paralelo: status=%d body=%s", response.Code, response.Body.String())
		}
	}
	if created != 1 || conflicts != 1 {
		t.Fatalf("concurrencia: creados=%d conflictos=%d; se esperaba 1 y 1", created, conflicts)
	}
	cartResponse := integrationRequest(router, http.MethodGet, "/carrito", "")
	var listedCart struct {
		Items []bloqueoCarrito `json:"items"`
	}
	if cartResponse.Code != http.StatusOK || json.Unmarshal(cartResponse.Body.Bytes(), &listedCart) != nil || len(listedCart.Items) != 1 {
		t.Fatalf("carrito inicial: status=%d body=%s", cartResponse.Code, cartResponse.Body.String())
	}
	secondStart := time.Date(fecha.Year(), fecha.Month(), fecha.Day()+7, 16, 0, 0, 0, loc)
	secondDateStr := secondStart.Format("2006-01-02")
	secondPayload := fmt.Sprintf(`{"profesionalId":"prof-2","tratamientoId":"masaje-02","inicio":%q,"diferirPago":true}`, secondStart.Format(time.RFC3339))
	secondAdded := integrationRequest(router, http.MethodPost, "/carrito/bloqueos", secondPayload)
	if secondAdded.Code != http.StatusCreated {
		t.Fatalf("agregar segundo tratamiento: status=%d body=%s", secondAdded.Code, secondAdded.Body.String())
	}
	cartResponse = integrationRequest(router, http.MethodGet, "/carrito", "")
	if cartResponse.Code != http.StatusOK || json.Unmarshal(cartResponse.Body.Bytes(), &listedCart) != nil || len(listedCart.Items) != 2 {
		t.Fatalf("carrito con dos tratamientos: status=%d body=%s", cartResponse.Code, cartResponse.Body.String())
	}
	var secondItem struct {
		Bloqueo bloqueoCarrito `json:"bloqueo"`
	}
	if err := json.Unmarshal(secondAdded.Body.Bytes(), &secondItem); err != nil {
		t.Fatal(err)
	}
	deletedSecond := integrationRequest(router, http.MethodDelete, "/carrito/bloqueos/"+secondItem.Bloqueo.ID, "")
	if deletedSecond.Code != http.StatusNoContent {
		t.Fatalf("eliminar segundo tratamiento: status=%d", deletedSecond.Code)
	}
	secondAvailability := integrationRequest(router, http.MethodGet, fmt.Sprintf("/disponibilidad?profesionalId=prof-2&tratamientoId=masaje-02&fecha=%s", secondDateStr), "")
	var secondSlots struct {
		Horarios []string `json:"horarios"`
	}
	if secondAvailability.Code != http.StatusOK || json.Unmarshal(secondAvailability.Body.Bytes(), &secondSlots) != nil || !contieneHorario(secondSlots.Horarios, "16:00") {
		t.Fatalf("eliminar el elemento no liberó el horario: status=%d body=%s", secondAvailability.Code, secondAvailability.Body.String())
	}
	startPayment := integrationRequest(router, http.MethodPost, "/carrito/bloqueos/"+bloqueoID+"/pago", "{}")
	if startPayment.Code != http.StatusConflict || preferenceCalls != 0 {
		t.Fatalf("no se debe permitir un pago por elemento: status=%d body=%s simulaciones=%d", startPayment.Code, startPayment.Body.String(), preferenceCalls)
	}
	repeatedStartPayment := integrationRequest(router, http.MethodPost, "/carrito/bloqueos/"+bloqueoID+"/pago", "{}")
	if repeatedStartPayment.Code != http.StatusConflict || preferenceCalls != 0 {
		t.Fatalf("el endpoint individual debe seguir bloqueado: status=%d simulaciones=%d", repeatedStartPayment.Code, preferenceCalls)
	}
	failedPayment := integrationRequest(router, http.MethodPost, "/carrito/bloqueos/"+bloqueoID+"/confirmar", `{"pagoId":"pago-fallido"}`)
	if failedPayment.Code != http.StatusConflict {
		t.Fatalf("la confirmación por elemento debe requerir checkout: status=%d body=%s", failedPayment.Code, failedPayment.Body.String())
	}
	failedTurns, _ := turnos.CountDocuments(ctx, bson.M{"pagoId": "pago-fallido"})
	if failedTurns != 0 {
		t.Fatalf("el pago fallido creó %d reservas", failedTurns)
	}
	cartResponse = integrationRequest(router, http.MethodGet, "/carrito", "")
	if cartResponse.Code != http.StatusOK || json.Unmarshal(cartResponse.Body.Bytes(), &listedCart) != nil || len(listedCart.Items) != 1 || listedCart.Items[0].ID != bloqueoID {
		t.Fatalf("pago fallido dejó un carrito inconsistente: status=%d body=%s", cartResponse.Code, cartResponse.Body.String())
	}

	durante := integrationRequest(router, http.MethodGet, query, "")
	var held struct {
		Horarios []string `json:"horarios"`
	}
	if err := json.Unmarshal(durante.Body.Bytes(), &held); err != nil {
		t.Fatal(err)
	}
	if contieneHorario(held.Horarios, "14:00") || contieneHorario(held.Horarios, "14:15") {
		t.Fatalf("la disponibilidad expuso horarios retenidos: %v", held.Horarios)
	}

	confirm := integrationRequest(router, http.MethodPost, "/carrito/checkout", `{"idempotencyKey":"checkout-single-test-001"}`)
	if confirm.Code != http.StatusCreated {
		t.Fatalf("checkout de un elemento: status=%d body=%s", confirm.Code, confirm.Body.String())
	}
	var checkoutResult struct {
		Checkout struct {
			PagoID string   `json:"pagoId"`
			Turnos []string `json:"turnos"`
		} `json:"checkout"`
	}
	if err := json.Unmarshal(confirm.Body.Bytes(), &checkoutResult); err != nil || len(checkoutResult.Checkout.Turnos) != 1 {
		t.Fatalf("respuesta de checkout: body=%s err=%v", confirm.Body.String(), err)
	}
	var confirmed struct {
		Turno turno `json:"turno"`
	}
	if err := turnos.FindOne(ctx, bson.M{"_id": checkoutResult.Checkout.Turnos[0]}).Decode(&confirmed.Turno); err != nil {
		t.Fatal("el turno no quedó confirmado en la transacción")
	}
	if confirmed.Turno.Sena != 3000 || confirmed.Turno.EstadoPago != "aprobado" {
		t.Fatalf("turno confirmado con pago/seña incorrectos: %+v", confirmed.Turno)
	}
	var cart bson.M
	if err := carritos.FindOne(ctx, bson.D{{Key: "_id", Value: bloqueoID}}).Decode(&cart); err != nil {
		t.Fatal("no se pudo conservar el carrito idempotente después de la confirmación")
	}
	if _, exists := cart["expiraEn"]; exists {
		t.Fatal("un carrito confirmado conservaría su TTL y perdería la idempotencia al vencer")
	}
	duplicate := integrationRequest(router, http.MethodPost, "/carrito/checkout", `{"idempotencyKey":"checkout-single-test-001"}`)
	if duplicate.Code != http.StatusOK {
		t.Fatalf("reintento de confirmación duplicó o falló: status=%d", duplicate.Code)
	}
	if preferenceCalls != 1 {
		t.Fatalf("se esperaba una simulación única sin verificación externa: simulaciones=%d", preferenceCalls)
	}

	cancellation := integrationRequest(router, http.MethodDelete, "/turnos/"+confirmed.Turno.ID, "")
	if cancellation.Code != http.StatusOK {
		t.Fatalf("cancelación: status=%d body=%s", cancellation.Code, cancellation.Body.String())
	}
	var canceled struct {
		Devolucion struct {
			Importe float64 `json:"importe"`
			Estado  string  `json:"estado"`
		} `json:"devolucion"`
	}
	if err := json.Unmarshal(cancellation.Body.Bytes(), &canceled); err != nil {
		t.Fatal(err)
	}
	if canceled.Devolucion.Importe != 3000 || canceled.Devolucion.Estado != "devuelta" || refundCalls != 1 {
		t.Fatalf("devolución inesperada: %+v, llamadas=%d", canceled.Devolucion, refundCalls)
	}

	despues := integrationRequest(router, http.MethodGet, query, "")
	var availableAgain struct {
		Horarios []string `json:"horarios"`
	}
	if err := json.Unmarshal(despues.Body.Bytes(), &availableAgain); err != nil {
		t.Fatal(err)
	}
	if !contieneHorario(availableAgain.Horarios, "14:00") {
		t.Fatalf("la cancelación no liberó el horario: %v", availableAgain.Horarios)
	}
	otherProfessionalSlot := integrationRequestAs(router, http.MethodPost, "/carrito/bloqueos", payload, "cliente-test", "cliente")
	if otherProfessionalSlot.Code != http.StatusCreated {
		t.Fatalf("reserva de control para categoría: status=%d body=%s", otherProfessionalSlot.Code, otherProfessionalSlot.Body.String())
	}
	otherProfessionalPayload := fmt.Sprintf(`{"profesionalId":"prof-2","tratamientoId":"facial-01","inicio":%q}`, inicioLocal.Format(time.RFC3339))
	categoryOverlap := integrationRequestAs(router, http.MethodPost, "/carrito/bloqueos", otherProfessionalPayload, "cliente-test", "cliente")
	if categoryOverlap.Code != http.StatusConflict {
		t.Fatalf("se permitió superponer misma categoría en otro profesional: status=%d body=%s", categoryOverlap.Code, categoryOverlap.Body.String())
	}
	differentTimePayload := fmt.Sprintf(`{"profesionalId":"prof-2","tratamientoId":"facial-01","inicio":%q}`, time.Date(fecha.Year(), fecha.Month(), fecha.Day(), 16, 0, 0, 0, loc).Format(time.RFC3339))
	differentTime := integrationRequestAs(router, http.MethodPost, "/carrito/bloqueos", differentTimePayload, "cliente-test", "cliente")
	if differentTime.Code != http.StatusCreated {
		t.Fatalf("el mismo cliente no pudo reservar la categoría en otro horario: status=%d body=%s", differentTime.Code, differentTime.Body.String())
	}
	var differentTimeHold struct {
		Bloqueo bloqueoCarrito `json:"bloqueo"`
	}
	if err := json.Unmarshal(differentTime.Body.Bytes(), &differentTimeHold); err != nil {
		t.Fatal(err)
	}
	_ = integrationRequestAs(router, http.MethodDelete, "/carrito/bloqueos/"+differentTimeHold.Bloqueo.ID, "", "cliente-test", "cliente")
	var categoryHold struct {
		Bloqueo bloqueoCarrito `json:"bloqueo"`
	}
	if err := json.Unmarshal(otherProfessionalSlot.Body.Bytes(), &categoryHold); err != nil {
		t.Fatal(err)
	}
	_ = integrationRequestAs(router, http.MethodDelete, "/carrito/bloqueos/"+categoryHold.Bloqueo.ID, "", "cliente-test", "cliente")

	otroInicio := time.Date(fecha.Year(), fecha.Month(), fecha.Day(), 16, 0, 0, 0, loc)
	otroPayload := fmt.Sprintf(`{"profesionalId":"prof-1","tratamientoId":"facial-01","inicio":%q}`, otroInicio.Format(time.RFC3339))
	manualRelease := integrationRequest(router, http.MethodPost, "/carrito/bloqueos", otroPayload)
	if manualRelease.Code != http.StatusCreated {
		t.Fatalf("bloqueo para liberación manual: status=%d body=%s", manualRelease.Code, manualRelease.Body.String())
	}
	otherClientPayload := fmt.Sprintf(`{"profesionalId":"prof-1","tratamientoId":"facial-01","inicio":%q}`, time.Date(fecha.Year(), fecha.Month(), fecha.Day(), 18, 0, 0, 0, loc).Format(time.RFC3339))
	otherClientNonOverlapping := integrationRequestAs(router, http.MethodPost, "/carrito/bloqueos", otherClientPayload, "cliente-test-2", "cliente")
	if otherClientNonOverlapping.Code != http.StatusCreated {
		t.Fatalf("otro cliente no pudo reservar al mismo profesional en horario no superpuesto: status=%d body=%s", otherClientNonOverlapping.Code, otherClientNonOverlapping.Body.String())
	}
	sameSlotDifferentClient := integrationRequestAs(router, http.MethodPost, "/carrito/bloqueos", otherClientPayload, "cliente-test-3", "cliente")
	if sameSlotDifferentClient.Code != http.StatusConflict {
		t.Fatalf("dos clientes pudieron tomar el mismo horario: status=%d body=%s", sameSlotDifferentClient.Code, sameSlotDifferentClient.Body.String())
	}
	var pending struct {
		Bloqueo bloqueoCarrito `json:"bloqueo"`
	}
	if err := json.Unmarshal(manualRelease.Body.Bytes(), &pending); err != nil {
		t.Fatal(err)
	}
	released := integrationRequest(router, http.MethodDelete, "/carrito/bloqueos/"+pending.Bloqueo.ID, "")
	if released.Code != http.StatusNoContent {
		t.Fatalf("liberación manual: status=%d body=%s", released.Code, released.Body.String())
	}
	var otherPending struct {
		Bloqueo bloqueoCarrito `json:"bloqueo"`
	}
	if err := json.Unmarshal(otherClientNonOverlapping.Body.Bytes(), &otherPending); err != nil {
		t.Fatal(err)
	}
	_ = integrationRequestAs(router, http.MethodDelete, "/carrito/bloqueos/"+otherPending.Bloqueo.ID, "", "cliente-test-2", "cliente")
	availableAfterRelease := integrationRequest(router, http.MethodGet, query, "")
	var reopened struct {
		Horarios []string `json:"horarios"`
	}
	if err := json.Unmarshal(availableAfterRelease.Body.Bytes(), &reopened); err != nil {
		t.Fatal(err)
	}
	if !contieneHorario(reopened.Horarios, "16:00") {
		t.Fatalf("la liberación manual no devolvió el horario: %v", reopened.Horarios)
	}

	completable := turno{ID: "turno-realizado-test", ClienteID: "cliente-test", ProfesionalID: "prof-1", TratamientoID: "facial-01", Inicio: inicioLocal, Fin: inicioLocal.Add(time.Hour), Estado: "confirmado", CreadoEn: time.Now().UTC(), PrecioTratamiento: 10000, Sena: 3000, BeneficioCanjeID: "canje-test", BeneficioReservaID: "carrito-test"}
	if _, err := turnos.InsertOne(ctx, completable); err != nil {
		t.Fatal("no se pudo preparar el turno para probar la confirmación profesional")
	}
	completed := integrationRequestAs(router, http.MethodPost, "/turnos/"+completable.ID+"/completar", "", "auth-prof-1", "profesional")
	if completed.Code != http.StatusAccepted {
		t.Fatalf("confirmación profesional: status=%d body=%s", completed.Code, completed.Body.String())
	}
	repeatedCompletion := integrationRequestAs(router, http.MethodPost, "/turnos/"+completable.ID+"/completar", "", "auth-prof-1", "profesional")
	if repeatedCompletion.Code != http.StatusOK {
		t.Fatalf("la confirmación repetida no fue idempotente: status=%d body=%s", repeatedCompletion.Code, repeatedCompletion.Body.String())
	}
	var eventCount int64
	if eventCount, err = outbox.CountDocuments(ctx, bson.M{"_id": "tratamiento-realizado:" + completable.ID}); err != nil || eventCount != 1 {
		t.Fatalf("se esperaba un único evento outbox, cantidad=%d error=%v", eventCount, err)
	}
	var event eventoOutbox
	if err := outbox.FindOne(ctx, bson.M{"_id": "tratamiento-realizado:" + completable.ID}).Decode(&event); err != nil || event.Payload["beneficioReservaId"] != "carrito-test" {
		t.Fatalf("el evento debe incluir el id del carrito usado por el canje: payload=%v error=%v", event.Payload, err)
	}
	wrongProfessional := integrationRequestAs(router, http.MethodPost, "/turnos/"+completable.ID+"/completar", "", "auth-prof-2", "profesional")
	if wrongProfessional.Code != http.StatusForbidden {
		t.Fatalf("un profesional no asignado pudo confirmar el turno: status=%d", wrongProfessional.Code)
	}
	missed := turno{ID: "turno-no-asistio-test", ClienteID: "cliente-test", ProfesionalID: "prof-1", TratamientoID: "facial-01", Inicio: time.Now().UTC().Add(-3 * time.Hour), Fin: time.Now().UTC().Add(-2 * time.Hour), Estado: "confirmado"}
	if _, err := turnos.InsertOne(ctx, missed); err != nil {
		t.Fatal("no se pudo preparar el turno de inasistencia")
	}
	noShow := integrationRequestAs(router, http.MethodPost, "/turnos/"+missed.ID+"/no-asistio", "{}", "auth-prof-1", "profesional")
	if noShow.Code != http.StatusOK {
		t.Fatalf("registro de inasistencia: status=%d body=%s", noShow.Code, noShow.Body.String())
	}
	var marked turno
	if err := turnos.FindOne(ctx, bson.M{"_id": missed.ID}).Decode(&marked); err != nil || marked.Estado != "no_asistio" {
		t.Fatalf("estado de inasistencia no guardado: %+v error=%v", marked, err)
	}
	repeatedNoShow := integrationRequestAs(router, http.MethodPost, "/turnos/"+missed.ID+"/no-asistio", "{}", "auth-prof-1", "profesional")
	if repeatedNoShow.Code != http.StatusOK {
		t.Fatalf("reintento de inasistencia no fue idempotente: status=%d", repeatedNoShow.Code)
	}
	if eventCount, err = outbox.CountDocuments(ctx, bson.M{"_id": "tratamiento-realizado:" + missed.ID}); err != nil || eventCount != 0 {
		t.Fatalf("inasistencia generó un evento de puntos: count=%d error=%v", eventCount, err)
	}
	wrongNoShow := integrationRequestAs(router, http.MethodPost, "/turnos/"+missed.ID+"/no-asistio", "{}", "auth-prof-2", "profesional")
	if wrongNoShow.Code != http.StatusForbidden {
		t.Fatalf("otro profesional pudo marcar inasistencia: status=%d", wrongNoShow.Code)
	}

	addHold := func(professionalID, treatmentID string, start time.Time, benefitID string) string {
		payload := map[string]any{"profesionalId": professionalID, "tratamientoId": treatmentID, "inicio": start.Format(time.RFC3339), "diferirPago": true}
		if benefitID != "" {
			payload["beneficioCanjeId"] = benefitID
		}
		body, _ := json.Marshal(payload)
		response := integrationRequest(router, http.MethodPost, "/carrito/bloqueos", string(body))
		if response.Code != http.StatusCreated {
			t.Fatalf("agregar elemento para checkout: status=%d body=%s", response.Code, response.Body.String())
		}
		var result struct {
			Bloqueo bloqueoCarrito `json:"bloqueo"`
		}
		if json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Bloqueo.ID == "" {
			t.Fatalf("no se guardó el elemento de checkout: %s", response.Body.String())
		}
		return result.Bloqueo.ID
	}

	checkoutHoldOne := addHold("prof-1", "facial-01", inicioLocal, "canje-checkout")
	checkoutHoldTwo := addHold("prof-2", "masaje-02", secondStart, "")
	otherCustomerCart := integrationRequestAs(router, http.MethodGet, "/carrito", "", "cliente-aislado", "cliente")
	var otherCart struct {
		Items []bloqueoCarrito `json:"items"`
	}
	if otherCustomerCart.Code != http.StatusOK || json.Unmarshal(otherCustomerCart.Body.Bytes(), &otherCart) != nil || len(otherCart.Items) != 0 {
		t.Fatalf("se mezclaron carritos de clientes: %s", otherCustomerCart.Body.String())
	}
	myCart := integrationRequest(router, http.MethodGet, "/carrito", "")
	var ownCart struct {
		Items   []bloqueoCarrito                                   `json:"items"`
		Resumen struct{ Subtotal, Descuento, Total, Sena float64 } `json:"resumen"`
	}
	if myCart.Code != http.StatusOK || json.Unmarshal(myCart.Body.Bytes(), &ownCart) != nil || len(ownCart.Items) != 2 || ownCart.Resumen.Subtotal != 20000 || ownCart.Resumen.Descuento != 1000 || ownCart.Resumen.Total != 19000 || ownCart.Resumen.Sena != 5700 {
		t.Fatalf("resumen del carrito único incorrecto: status=%d body=%s", myCart.Code, myCart.Body.String())
	}
	forgedCheckout := integrationRequest(router, http.MethodPost, "/carrito/checkout", `{"idempotencyKey":"checkout-approved-001","importe":1,"resultado":"approved"}`)
	if forgedCheckout.Code != http.StatusBadRequest {
		t.Fatalf("el cliente pudo elegir el resultado o importe: status=%d body=%s", forgedCheckout.Code, forgedCheckout.Body.String())
	}
	if _, err := carritos.UpdateOne(ctx, bson.M{"_id": checkoutHoldOne}, bson.M{"$set": bson.M{"precio": 1, "precioFinal": 1, "sena": 0.3}}); err != nil {
		t.Fatal("no se pudo preparar la comprobación de precio autoritativo")
	}
	checkoutBody := `{"idempotencyKey":"checkout-approved-001"}`
	approvedCheckout := integrationRequest(router, http.MethodPost, "/carrito/checkout", checkoutBody)
	if approvedCheckout.Code != http.StatusCreated {
		t.Fatalf("checkout aprobado: status=%d body=%s", approvedCheckout.Code, approvedCheckout.Body.String())
	}
	var approvedResult struct {
		Checkout struct {
			ID, Estado, PagoID string
			Total, Sena        float64
			Turnos             []string
		} `json:"checkout"`
	}
	if json.Unmarshal(approvedCheckout.Body.Bytes(), &approvedResult) != nil || approvedResult.Checkout.Estado != "confirmed" || approvedResult.Checkout.Total != 19000 || approvedResult.Checkout.Sena != 5700 || len(approvedResult.Checkout.Turnos) != 2 {
		t.Fatalf("checkout no confirmó todos sus turnos: %s", approvedCheckout.Body.String())
	}
	confirmedCount, _ := turnos.CountDocuments(ctx, bson.M{"pagoId": approvedResult.Checkout.PagoID, "estado": "confirmado"})
	if confirmedCount != 2 {
		t.Fatalf("la aprobación no confirmó todos los turnos en bloque: count=%d", confirmedCount)
	}
	if preferenceCalls != 2 {
		t.Fatalf("varios tratamientos deben crear una única operación de pago; operaciones=%d", preferenceCalls)
	}
	duplicateCheckout := integrationRequest(router, http.MethodPost, "/carrito/checkout", checkoutBody)
	if duplicateCheckout.Code != http.StatusOK {
		t.Fatalf("reintento idempotente del checkout: status=%d body=%s", duplicateCheckout.Code, duplicateCheckout.Body.String())
	}
	confirmedCount, _ = turnos.CountDocuments(ctx, bson.M{"pagoId": approvedResult.Checkout.PagoID})
	if confirmedCount != 2 || preferenceCalls != 2 {
		t.Fatalf("reintentar checkout duplicó pagos o turnos: turnos=%d pagos=%d", confirmedCount, preferenceCalls)
	}
	for _, turnID := range approvedResult.Checkout.Turnos {
		count, _ := outbox.CountDocuments(ctx, bson.M{"_id": "tratamiento-realizado:" + turnID})
		if count != 0 {
			t.Fatal("pagar el checkout generó puntos antes de realizar el tratamiento")
		}
	}
	clientBookings := integrationRequest(router, http.MethodGet, "/turnos", "")
	if clientBookings.Code != http.StatusOK || !strings.Contains(clientBookings.Body.String(), approvedResult.Checkout.Turnos[0]) || !strings.Contains(clientBookings.Body.String(), approvedResult.Checkout.Turnos[1]) {
		t.Fatalf("los turnos no aparecen en las reservas del cliente: %s", clientBookings.Body.String())
	}
	professionalBookings := integrationRequestAs(router, http.MethodGet, "/turnos", "", "auth-prof-1", "profesional")
	if professionalBookings.Code != http.StatusOK || !strings.Contains(professionalBookings.Body.String(), approvedResult.Checkout.Turnos[0]) || strings.Contains(professionalBookings.Body.String(), approvedResult.Checkout.Turnos[1]) {
		t.Fatalf("la agenda profesional no quedó actualizada y aislada: %s", professionalBookings.Body.String())
	}
	_ = checkoutHoldOne
	_ = checkoutHoldTwo

	rejectedHold := addHold("prof-1", "facial-01", inicioLocal.Add(4*time.Hour), "")
	simulateResult = "rejected"
	rejectedCheckout := integrationRequest(router, http.MethodPost, "/carrito/checkout", `{"idempotencyKey":"checkout-rejected-001"}`)
	simulateResult = "approved"
	if rejectedCheckout.Code != http.StatusOK {
		t.Fatalf("checkout rechazado: status=%d body=%s", rejectedCheckout.Code, rejectedCheckout.Body.String())
	}
	var rejectedResult struct {
		Checkout struct{ ID, Estado, PagoID string } `json:"checkout"`
	}
	if json.Unmarshal(rejectedCheckout.Body.Bytes(), &rejectedResult) != nil || rejectedResult.Checkout.Estado != "rejected" {
		t.Fatalf("rechazo de simulación no fue reflejado: %s", rejectedCheckout.Body.String())
	}
	noRejectedTurns, _ := turnos.CountDocuments(ctx, bson.M{"pagoId": rejectedResult.Checkout.PagoID})
	if noRejectedTurns != 0 {
		t.Fatalf("el pago rechazado confirmó %d turnos", noRejectedTurns)
	}
	var rejectedHoldDoc bloqueoCarrito
	if err := carritos.FindOne(ctx, bson.M{"_id": rejectedHold}).Decode(&rejectedHoldDoc); err != nil || rejectedHoldDoc.Estado != "pendiente_pago" || rejectedHoldDoc.CheckoutID != "" {
		t.Fatalf("el rechazo no dejó la retención reintentable: %+v error=%v", rejectedHoldDoc, err)
	}

	expireFirst := addHold("prof-1", "facial-01", inicioLocal.AddDate(0, 0, 14), "")
	expireSecond := addHold("prof-2", "masaje-02", secondStart.AddDate(0, 0, 14), "")
	expireHoldID = expireSecond
	partialFailure := integrationRequest(router, http.MethodPost, "/carrito/checkout", `{"idempotencyKey":"checkout-expiry-001"}`)
	if partialFailure.Code != http.StatusConflict {
		t.Fatalf("checkout con una retención vencida debía rechazarse entero: status=%d body=%s", partialFailure.Code, partialFailure.Body.String())
	}
	var partialOrder checkoutOperation
	if err := checkouts.FindOne(ctx, bson.M{"clienteId": "cliente-test", "idempotencyKey": "checkout-expiry-001"}).Decode(&partialOrder); err != nil {
		t.Fatalf("faltó registrar la compra fallida para recuperación: %v", err)
	}
	partialTurns, _ := turnos.CountDocuments(ctx, bson.M{"pagoId": partialOrder.PagoID})
	if partialOrder.Estado != "reverted" || partialTurns != 0 {
		t.Fatalf("quedó una compra parcialmente confirmada o no se revirtió el simulador: estado=%s turnos=%d", partialOrder.Estado, partialTurns)
	}
	var retainedFirst bloqueoCarrito
	if err := carritos.FindOne(ctx, bson.M{"_id": expireFirst}).Decode(&retainedFirst); err != nil || retainedFirst.Estado != "pendiente_pago" || retainedFirst.CheckoutID != "" {
		t.Fatalf("la retención todavía vigente se eliminó o quedó bloqueada: %+v error=%v", retainedFirst, err)
	}
}

func integrationRequest(router http.Handler, method, path, body string) *httptest.ResponseRecorder {
	return integrationRequestAs(router, method, path, body, "cliente-test", "cliente")
}

func integrationRequestAs(router http.Handler, method, path, body, userID, role string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("X-Auth-User-ID", userID)
	request.Header.Set("X-Auth-User-Email", "cliente@example.com")
	request.Header.Set("X-Auth-User-Roles", role)
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	router.ServeHTTP(recorder, request)
	return recorder
}

func contieneHorario(horarios []string, esperado string) bool {
	for _, horario := range horarios {
		if horario == esperado {
			return true
		}
	}
	return false
}
