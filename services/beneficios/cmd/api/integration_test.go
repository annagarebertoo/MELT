package main

import (
	"context"
	"errors"
	"fmt"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"os"
	"sync"
	"testing"
	"time"
)

func TestMongoPersistenciaConcurrenciaYRollback(t *testing.T) {
	uri := os.Getenv("BENEFICIOS_TEST_MONGODB_URI")
	if uri == "" {
		t.Skip("definí BENEFICIOS_TEST_MONGODB_URI con un replica set de pruebas")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Disconnect(context.Background())
	if err = c.Ping(ctx, nil); err != nil {
		t.Fatal(err)
	}
	db := fmt.Sprintf("melt_beneficios_test_%d", time.Now().UnixNano())
	defer c.Database(db).Drop(context.Background())
	// Simular el índice histórico antes de preparar la versión nueva.
	legacy := c.Database(db).Collection("registros_externos")
	if _, err := legacy.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "email", Value: 1}}, Options: options.Index().SetUnique(true).SetName("uq_registro_externo_email")}); err != nil {
		t.Fatal(err)
	}
	a := nuevoAlmacen(c, db)
	if err = a.preparar(ctx); err != nil {
		t.Fatal(err)
	}
	// Preservar saldo y movimiento histórico al retirar la integración anterior.
	_, err = a.registros.InsertOne(ctx, bson.M{"usuarioId": "u", "saldoPuntos": int64(50), "puntosRegalo": int64(50), "creadoEn": time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	req := AcreditarRequest{"primera", "u", 200}
	m, dup, err := a.acreditar(ctx, req)
	if err != nil || dup || m.SaldoPuntos != 250 {
		t.Fatal(m, dup, err)
	}
	m2, dup, err := a.acreditar(ctx, req)
	if err != nil || !dup || m2 != m {
		t.Fatal(m2, dup, err)
	}
	for _, r := range []AcreditarRequest{{"primera", "u", 201}, {"primera", "otro", 200}} {
		if _, _, err := a.acreditar(ctx, r); !errors.Is(err, errConflicto) {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _, e := a.acreditar(ctx, AcreditarRequest{fmt.Sprintf("concurrente-%d", i%10), "u", 2})
			if e != nil {
				t.Errorf("concurrencia: %v", e)
			}
		}(i)
	}
	wg.Wait()
	// Carreras sobre una cuenta que aún no existe en Beneficios.
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _, e := a.acreditar(ctx, AcreditarRequest{fmt.Sprintf("nueva-%d", i%4), "nueva", 3})
			if e != nil {
				t.Errorf("cuenta nueva: %v", e)
			}
		}(i)
	}
	wg.Wait()
	if saldo, _, e := a.consultar(ctx, "nueva"); e != nil || saldo != 12 {
		t.Fatal("saldo cuenta nueva", saldo, e)
	}
	// Una conexión nueva comprueba persistencia, no estado en memoria.
	c2, e := mongo.Connect(options.Client().ApplyURI(uri))
	if e != nil {
		t.Fatal(e)
	}
	defer c2.Disconnect(context.Background())
	saldo, movs, err := nuevoAlmacen(c2, db).consultar(ctx, "u")
	if err != nil || saldo != 270 || len(movs) != 12 {
		t.Fatalf("saldo=%d movimientos=%d err=%v", saldo, len(movs), err)
	}
	// Fallo después de insertar el movimiento: debe revertirse la transacción.
	_, _, err = a.acreditar(ctx, AcreditarRequest{"overflow", "u", maxPuntos})
	if !errors.Is(err, errLimite) {
		t.Fatal(err)
	}
	if err = a.movimientos.FindOne(ctx, bson.M{"_id": "overflow"}).Err(); !errors.Is(err, mongo.ErrNoDocuments) {
		t.Fatal("movimiento parcial", err)
	}
	saldo, _, err = a.consultar(ctx, "u")
	if err != nil || saldo != 270 {
		t.Fatal("saldo alterado tras rollback", saldo, err)
	}
	if err := a.guardarRecompensa(ctx, Recompensa{ID: "desc-10", Nombre: "10%", Tipo: "descuento_porcentaje", CostoPuntos: 10, Porcentaje: 10, Activa: true}); err != nil {
		t.Fatal("guardar recompensa", err)
	}
	canje, repetido, err := a.canjear(ctx, "u", "desc-10", "canje-uno")
	if err != nil || repetido || canje.Estado != "disponible" {
		t.Fatalf("canje inicial: %+v repetido=%v err=%v", canje, repetido, err)
	}
	canjeRepetido, repetido, err := a.canjear(ctx, "u", "desc-10", "canje-uno")
	if err != nil || !repetido || canjeRepetido.ID != canje.ID {
		t.Fatalf("canje repetido: %+v repetido=%v err=%v", canjeRepetido, repetido, err)
	}
	if _, _, err := a.canjear(ctx, "otro-usuario", "desc-10", "canje-uno"); !errors.Is(err, errConflicto) {
		t.Fatalf("la clave de idempotencia debe detectar otro usuario: %v", err)
	}
	if _, _, err := a.canjear(ctx, "sin-saldo", "desc-10", "canje-sin-saldo"); !errors.Is(err, errSaldoInsuficiente) {
		t.Fatalf("se aceptó un canje sin saldo: %v", err)
	}
	if saldo, _, err := a.consultar(ctx, "u"); err != nil || saldo != 260 {
		t.Fatalf("el canje debe descontar puntos una sola vez: saldo=%d err=%v", saldo, err)
	}
	if err := a.guardarRegla(ctx, ReglaTratamiento{TratamientoID: "facial", Puntos: 5}); err != nil {
		t.Fatal("guardar regla de tratamiento", err)
	}
	if err := a.procesarEventoTratamiento(ctx, "evento-facial-1", "u", "facial"); err != nil {
		t.Fatal("procesar tratamiento realizado", err)
	}
	if err := a.procesarEventoTratamiento(ctx, "evento-facial-1", "u", "facial"); err != nil {
		t.Fatal("repetir evento de tratamiento", err)
	}
	if count, err := a.movimientos.CountDocuments(ctx, bson.M{"_id": "evento:tratamiento-realizado:evento-facial-1"}); err != nil || count != 1 {
		t.Fatalf("acreditación de tratamiento duplicada: count=%d err=%v", count, err)
	}
	if saldo, _, err := a.consultar(ctx, "u"); err != nil || saldo != 265 {
		t.Fatalf("el tratamiento debe acreditar puntos una sola vez: saldo=%d err=%v", saldo, err)
	}
	if _, err := a.reservarCanje(ctx, canje.ID, "u", "turno-test", "facial", 1000); err != nil {
		t.Fatal("reservar canje", err)
	}
	if _, err := a.canjes.UpdateOne(ctx, bson.M{"_id": canje.ID}, bson.M{"$set": bson.M{"reservaExpiraEn": time.Now().Add(-time.Minute)}}); err != nil {
		t.Fatal("vencer la reserva de prueba", err)
	}
	if _, err := a.reservarCanje(ctx, canje.ID, "u", "turno-test", "facial", 1000); err != nil {
		t.Fatal("renovar la misma reserva antes de confirmar el pago", err)
	}
	var canjeConReserva Canje
	if err := a.canjes.FindOne(ctx, bson.M{"_id": canje.ID}).Decode(&canjeConReserva); err != nil || canjeConReserva.ReservaExpiraEn == nil || !canjeConReserva.ReservaExpiraEn.After(time.Now()) {
		t.Fatalf("la reserva del mismo carrito debe renovarse: %+v err=%v", canjeConReserva.ReservaExpiraEn, err)
	}
	if _, err := a.actualizarCanje(ctx, canje.ID, "u", "turno-test", "aplicar"); err != nil {
		t.Fatal("aplicar canje", err)
	}
	if _, err := a.actualizarCanje(ctx, canje.ID, "u", "turno-test", "restituir"); err != nil {
		t.Fatal("restituir canje", err)
	}
	if _, err := a.actualizarCanje(ctx, canje.ID, "u", "turno-test", "restituir"); err != nil {
		t.Fatal("repetir restitución de canje", err)
	}
	if count, err := a.movimientos.CountDocuments(ctx, bson.M{"_id": "restitucion:beneficio:turno-test", "tipo": "restitucion_beneficio"}); err != nil || count != 1 {
		t.Fatalf("movimiento de restitución: count=%d err=%v", count, err)
	}
	if _, err := a.reservarCanje(ctx, canje.ID, "u", "turno-expirado", "facial", 1000); err != nil {
		t.Fatal("reservar canje antes de vencimiento", err)
	}
	if _, err := a.canjes.UpdateOne(ctx, bson.M{"_id": canje.ID}, bson.M{"$set": bson.M{"reservaExpiraEn": time.Now().Add(-time.Minute)}}); err != nil {
		t.Fatal("vencer reserva de canje", err)
	}
	if _, err := a.reservarCanje(ctx, canje.ID, "u", "turno-nuevo", "facial", 1000); err != nil {
		t.Fatal("reutilizar beneficio con reserva vencida", err)
	}
	// Otro usuario sin email: no debe heredar la vieja restricción de regalo.
	if _, _, err = a.acreditar(ctx, AcreditarRequest{"otro-usuario", "otro", 10}); err != nil {
		t.Fatal(err)
	}
}
