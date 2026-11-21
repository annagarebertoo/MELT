package main

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestAsistenciaPostgres(t *testing.T) {
	dsn := os.Getenv("RESERVAS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requiere PostgreSQL de prueba con el esquema de Reservas")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	s := &servidorReservas{pg: &postgresStore{db: db}}
	prof := bson.NewObjectID().Hex()
	now := time.Now().UTC()
	makeTurn := func(offset time.Duration) string {
		t.Helper()
		id := bson.NewObjectID().Hex()
		_, err := db.ExecContext(ctx, `insert into turnos
			(id,cliente_id,profesional_id,tratamiento_id,inicio,fin,estado)
			values ($1,'cliente-ci',$2,'tratamiento-ci',$3,$4,'confirmado')`,
			id, prof, now.Add(offset), now.Add(offset+time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	absence := makeTurn(-6*time.Hour)
	done := makeTurn(-3*time.Hour)
	future := makeTurn(time.Hour)
	if _, _, err := s.registrarInasistencia(ctx, absence, "otro"); !errors.Is(err, errInasistenciaTurnoAjeno) {
		t.Fatalf("profesional ajeno: %v", err)
	}
	if _, _, err := s.registrarInasistencia(ctx, future, prof); !errors.Is(err, errInasistenciaFutura) {
		t.Fatalf("turno futuro: %v", err)
	}
	for i := 0; i < 2; i++ {
		_, repeated, err := s.registrarInasistencia(ctx, absence, prof)
		if err != nil || repeated != (i == 1) {
			t.Fatalf("inasistencia intento %d: repetido=%v error=%v", i, repeated, err)
		}
		_, repeated, err = s.registrarRealizacion(ctx, done, prof)
		if err != nil || repeated != (i == 1) {
			t.Fatalf("realización intento %d: repetido=%v error=%v", i, repeated, err)
		}
	}
	for id, expected := range map[string]string{absence:"no_asistio", done:"completado", future:"confirmado"} {
		saved, err := s.pg.turno(ctx, id)
		if err != nil || saved.Estado != expected {
			t.Fatalf("estado persistido: obtenido=%q esperado=%q error=%v", saved.Estado, expected, err)
		}
		var count int
		if err := db.QueryRowContext(ctx, `select count(*) from outbox_eventos where payload->>'turnoId'=$1`, id).Scan(&count); err != nil {
			t.Fatal(err)
		}
		want := 0
		if id == done {
			want = 1
		}
		if count != want {
			t.Fatalf("eventos de acreditación para %s: %d, esperado %d", expected, count, want)
		}
	}
}
