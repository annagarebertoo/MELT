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
	if _, err := bloqueos.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "profesionalId", Value: 1}, {Key: "minuto", Value: 1}}, Options: options.Index().SetUnique(true)}); err != nil {
		t.Fatal("no se pudo crear el índice de concurrencia")
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
	if _, err := agendas.InsertOne(ctx, agendaSemanal{ID: "prof-1", ProfesionalID: "prof-1", DiaSemana: int(inicioLocal.Weekday()), HoraInicio: "08:00", HoraFin: "20:00", Activo: true}); err != nil {
		t.Fatal("no se pudo crear la agenda de prueba")
	}

	tratamiento := tratamientoCatalogo{ID: "facial-01", Nombre: "Higiene facial", DuracionMinutos: 60, ProfesionalesIDs: []string{"prof-1"}, Precio: 10000, Moneda: "ARS"}
	catalogo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _ = json.NewEncoder(w).Encode(tratamiento) }))
	defer catalogo.Close()
	var verifyCalls, refundCalls int
	pagos := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/payments/verify":
			verifyCalls++
			var req verificacionPagoRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "invalid request", http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(verificacionPagoResponse{Aprobado: true, PagoID: req.PagoID, Importe: req.Importe, Moneda: req.Moneda})
		case "/payments/refunds":
			refundCalls++
			w.WriteHeader(http.StatusCreated)
		default:
			http.NotFound(w, r)
		}
	}))
	defer pagos.Close()
	s := &servidorReservas{agendas: agendas, turnos: turnos, bloqueos: bloqueos, carritos: carritos, catalogoURL: catalogo.URL, pagosURL: pagos.URL, httpClient: &http.Client{Timeout: 3 * time.Second}}
	gin.SetMode(gin.TestMode)
	router := s.router()
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

	payload := fmt.Sprintf(`{"profesionalId":"prof-1","tratamientoId":"facial-01","inicio":%q}`, inicioLocal.Format(time.RFC3339))
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
				Sena    struct {
					Porcentaje int     `json:"porcentaje"`
					Importe    float64 `json:"importe"`
				} `json:"seña"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			bloqueoID = result.Bloqueo.ID
			if result.Sena.Porcentaje != 30 || result.Sena.Importe != 3000 {
				t.Fatalf("seña inesperada: %+v", result.Sena)
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

	confirm := integrationRequest(router, http.MethodPost, "/carrito/bloqueos/"+bloqueoID+"/confirmar", `{"pagoId":"pago-test-1"}`)
	if confirm.Code != http.StatusCreated {
		t.Fatalf("confirmación: status=%d body=%s", confirm.Code, confirm.Body.String())
	}
	var confirmed struct {
		Turno turno `json:"turno"`
	}
	if err := json.Unmarshal(confirm.Body.Bytes(), &confirmed); err != nil {
		t.Fatal(err)
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
	duplicate := integrationRequest(router, http.MethodPost, "/carrito/bloqueos/"+bloqueoID+"/confirmar", `{"pagoId":"pago-test-1"}`)
	if duplicate.Code != http.StatusOK {
		t.Fatalf("reintento de confirmación duplicó o falló: status=%d", duplicate.Code)
	}
	if verifyCalls != 1 {
		t.Fatalf("la repetición idempotente verificó el pago %d veces", verifyCalls)
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

	otroInicio := time.Date(fecha.Year(), fecha.Month(), fecha.Day(), 16, 0, 0, 0, loc)
	otroPayload := fmt.Sprintf(`{"profesionalId":"prof-1","tratamientoId":"facial-01","inicio":%q}`, otroInicio.Format(time.RFC3339))
	manualRelease := integrationRequest(router, http.MethodPost, "/carrito/bloqueos", otroPayload)
	if manualRelease.Code != http.StatusCreated {
		t.Fatalf("bloqueo para liberación manual: status=%d body=%s", manualRelease.Code, manualRelease.Body.String())
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
}

func integrationRequest(router http.Handler, method, path, body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("X-Auth-User-ID", "cliente-test")
	request.Header.Set("X-Auth-User-Email", "cliente@example.com")
	request.Header.Set("X-Auth-User-Roles", "cliente")
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
