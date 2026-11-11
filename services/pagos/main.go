package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type payment struct {
	ID             string    `bson:"_id" json:"id"`
	CheckoutID     string    `bson:"checkoutId" json:"checkoutId"`
	IdempotencyKey string    `bson:"idempotencyKey" json:"-"`
	Amount         float64   `bson:"importe" json:"importe"`
	Currency       string    `bson:"moneda" json:"moneda"`
	Status         string    `bson:"estado" json:"estado"`
	CreatedAt      time.Time `bson:"creadoEn" json:"creadoEn"`
	Refunded       float64   `bson:"devuelto" json:"devuelto"`
}

type server struct {
	payments    *mongo.Collection
	refunds     *mongo.Collection
	internalKey string
	result      string
}

type simulateRequest struct {
	CheckoutID string  `json:"checkoutId"`
	Amount     float64 `json:"importe"`
	Currency   string  `json:"moneda"`
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

func (s *server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "provider": "simulador", "mode": s.result})
	})
	mux.Handle("POST /payments/simulate", s.internal(http.HandlerFunc(s.simulate)))
	mux.Handle("GET /payments/{id}", s.internal(http.HandlerFunc(s.getPayment)))
	mux.Handle("POST /payments/verify", s.internal(http.HandlerFunc(s.verify)))
	mux.Handle("POST /payments/{id}/reverse", s.internal(http.HandlerFunc(s.reverse)))
	mux.Handle("POST /payments/refunds", s.internal(http.HandlerFunc(s.refund)))
	return mux
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

func (s *server) simulate(w http.ResponseWriter, r *http.Request) {
	var req simulateRequest
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if decodeJSON(r, &req) != nil || key == "" || !validAmount(req.Amount) || req.Currency != "ARS" || !validID(req.CheckoutID) {
		writeError(w, http.StatusBadRequest, "Solicitud de simulación inválida")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()
	var existing payment
	err := s.payments.FindOne(ctx, bson.M{"checkoutId": req.CheckoutID}).Decode(&existing)
	if err == nil {
		if existing.CheckoutID != req.CheckoutID || existing.Amount != req.Amount || existing.Currency != req.Currency {
			writeError(w, http.StatusConflict, "La clave de idempotencia ya se usó con otros datos")
			return
		}
		writeJSON(w, http.StatusOK, existing)
		return
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		writeError(w, http.StatusServiceUnavailable, "No se pudo consultar la operación")
		return
	}
	doc := payment{ID: bson.NewObjectID().Hex(), CheckoutID: req.CheckoutID, IdempotencyKey: key, Amount: req.Amount, Currency: req.Currency, Status: s.result, CreatedAt: time.Now().UTC()}
	_, err = s.payments.InsertOne(ctx, doc)
	if mongo.IsDuplicateKeyError(err) {
		if s.payments.FindOne(ctx, bson.M{"checkoutId": req.CheckoutID}).Decode(&existing) == nil && existing.CheckoutID == req.CheckoutID && existing.Amount == req.Amount && existing.Currency == req.Currency {
			writeJSON(w, http.StatusOK, existing)
			return
		}
		writeError(w, http.StatusConflict, "La operación ya fue procesada con otros datos")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "No se pudo registrar el pago simulado")
		return
	}
	writeJSON(w, http.StatusCreated, doc)
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

func (s *server) reverse(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	res, err := s.payments.UpdateOne(r.Context(), bson.M{"_id": id, "estado": "approved"}, bson.M{"$set": bson.M{"estado": "reversed"}})
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "No se pudo revertir la simulación")
		return
	}
	var item payment
	if err = s.payments.FindOne(r.Context(), bson.M{"_id": id}).Decode(&item); err != nil {
		writeError(w, http.StatusNotFound, "Operación no encontrada")
		return
	}
	if item.Status != "reversed" {
		writeError(w, http.StatusConflict, "La operación no puede revertirse en su estado actual")
		return
	}
	_ = res
	writeJSON(w, http.StatusOK, item)
}

func (s *server) refund(w http.ResponseWriter, r *http.Request) {
	var req refundRequest
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if decodeJSON(r, &req) != nil || key == "" || !validID(req.PaymentID) || !validAmount(req.Amount) || req.Currency != "ARS" {
		writeError(w, http.StatusBadRequest, "Solicitud de devolución inválida")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()
	var existing bson.M
	if err := s.refunds.FindOne(ctx, bson.M{"_id": key}).Decode(&existing); err == nil {
		writeJSON(w, http.StatusOK, existing)
		return
	}
	var item payment
	if err := s.payments.FindOne(ctx, bson.M{"_id": req.PaymentID}).Decode(&item); err != nil {
		writeError(w, http.StatusNotFound, "Operación aprobada no encontrada")
		return
	}
	session, err := s.payments.Database().Client().StartSession()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "No se pudo iniciar la devolución simulada")
		return
	}
	defer session.EndSession(ctx)
	var result any
	created := false
	_, err = session.WithTransaction(ctx, func(tx context.Context) (any, error) {
		var existing bson.M
		if e := s.refunds.FindOne(tx, bson.M{"_id": key}).Decode(&existing); e == nil {
			result = existing
			created = false
			return existing, nil
		} else if !errors.Is(e, mongo.ErrNoDocuments) {
			return nil, e
		}
		updated, e := s.payments.UpdateOne(tx, bson.M{"_id": req.PaymentID, "estado": "approved", "devuelto": bson.M{"$lte": item.Amount - req.Amount}}, bson.M{"$inc": bson.M{"devuelto": req.Amount}})
		if e != nil {
			return nil, e
		}
		if updated.MatchedCount != 1 {
			return nil, errors.New("el importe excede el saldo disponible")
		}
		refund := bson.M{"_id": key, "pagoId": req.PaymentID, "importe": req.Amount, "moneda": req.Currency, "estado": "refunded", "motivo": req.Reason, "creadoEn": time.Now().UTC()}
		if _, e = s.refunds.InsertOne(tx, refund); e != nil {
			return nil, e
		}
		result = refund
		created = true
		return refund, nil
	})
	if err != nil {
		writeError(w, http.StatusConflict, "No se pudo aplicar la devolución simulada")
		return
	}
	status := http.StatusCreated
	if !created {
		status = http.StatusOK
	}
	writeJSON(w, status, result)
}

func validAmount(amount float64) bool {
	return amount > 0 && !math.IsNaN(amount) && !math.IsInf(amount, 0)
}
func validSimulatorResult(value string) bool { return value == "approved" || value == "rejected" }
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
		uri = "mongodb://localhost:27017"
	}
	dbName := strings.TrimSpace(os.Getenv("PAGOS_MONGODB_DATABASE"))
	if dbName == "" {
		dbName = "pagos_db"
	}
	result := strings.ToLower(strings.TrimSpace(os.Getenv("PAYMENT_SIMULATOR_RESULT")))
	if result == "" {
		result = "approved"
	}
	if !validSimulatorResult(result) {
		log.Fatal("PAYMENT_SIMULATOR_RESULT debe ser approved o rejected")
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
	s := &server{payments: db.Collection("pagos_simulados"), refunds: db.Collection("devoluciones_simuladas"), internalKey: strings.TrimSpace(os.Getenv("MELT_INTERNAL_KEY")), result: result}
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
