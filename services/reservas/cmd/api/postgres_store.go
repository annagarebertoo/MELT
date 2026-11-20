package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// postgresStore is the persistence boundary for Reservas. Business IDs that
// belong to Auth, Catalog, Benefits and Payments remain external references.
type postgresStore struct{ db *sql.DB }

func (p *postgresStore) conflictoUnico(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && (pgErr.Code == "23505" || pgErr.Code == "23P01")
}

func abrirReservasPostgres(ctx context.Context) (*postgresStore, error) {
	url := strings.TrimSpace(os.Getenv("RESERVAS_DATABASE_URL"))
	if url == "" {
		return nil, errors.New("falta RESERVAS_DATABASE_URL con la conexión privada del proyecto MELT-Reservas")
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		return nil, fmt.Errorf("no se pudo configurar PostgreSQL de Reservas: %w", err)
	}
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(30 * time.Minute)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("no se pudo conectar con PostgreSQL de Reservas: %w", err)
	}
	store := &postgresStore{db: db}
	if err := store.preparar(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

type sqlRunner interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type reservasTxKey struct{}

func convertirBloqueos(values []interface{}) []bloqueoTurno {
	result := make([]bloqueoTurno, 0, len(values))
	for _, value := range values {
		if item, ok := value.(bloqueoTurno); ok {
			result = append(result, item)
		}
	}
	return result
}

func (p *postgresStore) runner(ctx context.Context) sqlRunner {
	if tx, ok := ctx.Value(reservasTxKey{}).(*sql.Tx); ok {
		return tx
	}
	return p.db
}

func enTransaccionReservas(ctx context.Context) bool {
	_, ok := ctx.Value(reservasTxKey{}).(*sql.Tx)
	return ok
}

func (p *postgresStore) transaction(ctx context.Context, fn func(context.Context) error) error {
	for intento := 0; intento < 3; intento++ {
		tx, err := p.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
		if err != nil {
			return err
		}
		txCtx := context.WithValue(ctx, reservasTxKey{}, tx)
		err = fn(txCtx)
		if err == nil {
			err = tx.Commit()
		} else {
			_ = tx.Rollback()
		}
		if !errorTransitorioTransaccion(err) || intento == 2 {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(intento+1) * 25 * time.Millisecond):
		}
	}
	return errors.New("no se pudo completar la transacción de Reservas")
}

func errorTransitorioTransaccion(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && (pgErr.Code == "40001" || pgErr.Code == "40P01")
}

func decodeSQLJSON[T any](row *sql.Row) (T, error) {
	var value T
	var raw []byte
	if err := row.Scan(&raw); err != nil {
		return value, err
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return value, err
	}
	return value, nil
}

func decodeSQLRows[T any](rows *sql.Rows) ([]T, error) {
	defer rows.Close()
	values := make([]T, 0)
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var value T
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (p *postgresStore) preparar(ctx context.Context) error {
	var missing string
	err := p.db.QueryRowContext(ctx, `select coalesce(string_agg(t.name, ', ' order by t.name), '')
		from (values ('agendas_semanales'), ('turnos'), ('bloqueos_turnos'),
		('carritos_bloqueos'), ('checkouts'), ('control_agendas'), ('outbox_eventos')) t(name)
		where to_regclass('public.' || t.name) is null`).Scan(&missing)
	if err != nil {
		return err
	}
	if missing != "" {
		return fmt.Errorf("faltan tablas de Reservas en PostgreSQL: %s (aplicá infra/supabase/reservas.sql)", missing)
	}
	return nil
}

var errRegistroNoEncontrado = sql.ErrNoRows

func sqlNotFound(err error) bool { return errors.Is(err, sql.ErrNoRows) }

func (p *postgresStore) noEncontrado(err error) bool { return errors.Is(err, sql.ErrNoRows) }

const agendaJSON = `select to_jsonb(x) from (
	select id, profesional_id as "profesionalId", dia_semana as "diaSemana",
	 hora_inicio as "horaInicio", hora_fin as "horaFin", bloques, activo
	from agendas_semanales`

func (p *postgresStore) agenda(ctx context.Context, profesionalID string, dia int, soloActiva bool) (agendaSemanal, error) {
	query := agendaJSON + ` where profesional_id=$1 and dia_semana=$2`
	if soloActiva {
		query += ` and activo=true`
	}
	query += ` limit 1`
	if enTransaccionReservas(ctx) {
		query += ` for update`
	}
	query += `) x`
	return decodeSQLJSON[agendaSemanal](p.runner(ctx).QueryRowContext(ctx, query, profesionalID, dia))
}

func (p *postgresStore) agendas(ctx context.Context, profesionalID string) ([]agendaSemanal, error) {
	rows, err := p.runner(ctx).QueryContext(ctx, agendaJSON+` where profesional_id=$1 order by dia_semana) x`, profesionalID)
	if err != nil {
		return nil, err
	}
	return decodeSQLRows[agendaSemanal](rows)
}

func (p *postgresStore) guardarAgenda(ctx context.Context, item agendaSemanal) error {
	_, err := p.runner(ctx).ExecContext(ctx, `insert into agendas_semanales
		(id,profesional_id,dia_semana,hora_inicio,hora_fin,bloques,activo)
		values ($1,$2,$3,nullif($4,'')::time,nullif($5,'')::time,$6::jsonb,$7)
		on conflict (profesional_id,dia_semana) do update set id=excluded.id,
		hora_inicio=excluded.hora_inicio,hora_fin=excluded.hora_fin,bloques=excluded.bloques,activo=excluded.activo`,
		item.ID, item.ProfesionalID, item.DiaSemana, item.HoraInicio, item.HoraFin, mustJSON(item.Bloques), item.Activo)
	return err
}

func mustJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(raw)
}

func (p *postgresStore) asegurarAgendaControl(ctx context.Context, profesionalID string, dia int) error {
	_, err := p.runner(ctx).ExecContext(ctx, `insert into control_agendas (id,profesional_id,dia_semana)
		values ($1,$2,$3) on conflict (profesional_id,dia_semana) do nothing`, agendaControlID(profesionalID, dia), profesionalID, dia)
	return err
}

func (p *postgresStore) modificarVersionAgenda(ctx context.Context, profesionalID string, dia int) error {
	_, err := p.runner(ctx).ExecContext(ctx, `update control_agendas set version=version+1 where profesional_id=$1 and dia_semana=$2`, profesionalID, dia)
	return err
}

const turnoJSON = `select to_jsonb(x) from (
	select id, cliente_id as "clienteId", profesional_id as "profesionalId", tratamiento_id as "tratamientoId",
	 categoria_id as "categoriaId", inicio, fin, estado, creado_en as "creadoEn",
	 precio_tratamiento as "precioTratamiento", sena, pago_id as "pagoId", estado_pago as "estadoPago",
	 importe_devolucion as "importeDevolucion", estado_devolucion as "estadoDevolucion",
	 tratamiento_realizado_en as "tratamientoRealizadoEn", tratamiento_realizado_por as "tratamientoRealizadoPor",
	 no_asistio_en as "noAsistioEn", no_asistio_por as "noAsistioPor", beneficio_canje_id as "beneficioCanjeId",
	 beneficio_reserva_id as "beneficioReservaId", descuento_beneficio as "descuentoBeneficio", precio_final as "precioFinal"
	from turnos`

func (p *postgresStore) turno(ctx context.Context, id string) (turno, error) {
	return decodeSQLJSON[turno](p.runner(ctx).QueryRowContext(ctx, turnoJSON+` where id=$1) x`, id))
}

func (p *postgresStore) turnoForUpdate(ctx context.Context, id string) (turno, error) {
	return decodeSQLJSON[turno](p.runner(ctx).QueryRowContext(ctx, turnoJSON+` where id=$1 for update) x`, id))
}

func (p *postgresStore) turnoSuperpuesto(ctx context.Context, profesionalID string, inicio, fin any) (turno, error) {
	return decodeSQLJSON[turno](p.runner(ctx).QueryRowContext(ctx, turnoJSON+`
		where profesional_id=$1 and estado='confirmado' and inicio<$3 and fin>$2 limit 1) x`, profesionalID, inicio, fin))
}

func (p *postgresStore) contarTurnosSuperpuestos(ctx context.Context, profesionalID string, inicio, fin any) (int64, error) {
	var count int64
	err := p.runner(ctx).QueryRowContext(ctx, `select count(*) from turnos
		where profesional_id=$1 and estado='confirmado' and inicio<$3 and fin>$2`, profesionalID, inicio, fin).Scan(&count)
	return count, err
}

func (p *postgresStore) turnosDesde(ctx context.Context, profesionalID string, desde any) ([]turno, error) {
	rows, err := p.runner(ctx).QueryContext(ctx, turnoJSON+` where profesional_id=$1 and estado='confirmado' and inicio >= $2) x`, profesionalID, desde)
	if err != nil {
		return nil, err
	}
	return decodeSQLRows[turno](rows)
}

func (p *postgresStore) turnosClienteEnRango(ctx context.Context, clienteID string, inicio, fin any) ([]turno, error) {
	rows, err := p.runner(ctx).QueryContext(ctx, turnoJSON+` where cliente_id=$1 and estado='confirmado' and inicio<$3 and fin>$2) x`, clienteID, inicio, fin)
	if err != nil {
		return nil, err
	}
	return decodeSQLRows[turno](rows)
}

func (p *postgresStore) turnosCliente(ctx context.Context, clienteID string, desde any) ([]turno, error) {
	rows, err := p.runner(ctx).QueryContext(ctx, turnoJSON+` where cliente_id=$1 and estado='confirmado' and inicio >= $2) x`, clienteID, desde)
	if err != nil {
		return nil, err
	}
	return decodeSQLRows[turno](rows)
}

func (p *postgresStore) listarTurnos(ctx context.Context, clienteID, profesionalID string) ([]turno, error) {
	query := turnoJSON + ` where ($1='' or cliente_id=$1) and ($2='' or profesional_id=$2) order by inicio) x`
	rows, err := p.runner(ctx).QueryContext(ctx, query, clienteID, profesionalID)
	if err != nil {
		return nil, err
	}
	return decodeSQLRows[turno](rows)
}

func (p *postgresStore) actualizarTurnoCancelado(ctx context.Context, id string, importe float64, estado string) (int64, error) {
	res, err := p.runner(ctx).ExecContext(ctx, `update turnos set estado='cancelado', importe_devolucion=$2, estado_devolucion=$3 where id=$1 and estado='confirmado'`, id, importe, estado)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (p *postgresStore) actualizarDevolucion(ctx context.Context, id string, estadoActual, estadoNuevo string) error {
	_, err := p.runner(ctx).ExecContext(ctx, `update turnos set estado_devolucion=$3 where id=$1 and estado='cancelado' and estado_devolucion=$2`, id, estadoActual, estadoNuevo)
	return err
}

func (p *postgresStore) marcarTurno(ctx context.Context, id, profesionalID, estado string, cuando any) (int64, error) {
	var query string
	switch estado {
	case "completado":
		query = `update turnos set estado='completado', tratamiento_realizado_en=$3, tratamiento_realizado_por=$2 where id=$1 and profesional_id=$2 and estado='confirmado'`
	case "no_asistio":
		query = `update turnos set estado='no_asistio', no_asistio_en=$3, no_asistio_por=$2 where id=$1 and profesional_id=$2 and estado='confirmado'`
	default:
		return 0, fmt.Errorf("estado de turno no soportado: %s", estado)
	}
	res, err := p.runner(ctx).ExecContext(ctx, query, id, profesionalID, cuando)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (p *postgresStore) insertarTurno(ctx context.Context, t turno) error {
	_, err := p.runner(ctx).ExecContext(ctx, `insert into turnos
		(id,cliente_id,profesional_id,tratamiento_id,categoria_id,inicio,fin,estado,creado_en,precio_tratamiento,sena,pago_id,estado_pago,importe_devolucion,estado_devolucion,tratamiento_realizado_en,tratamiento_realizado_por,no_asistio_en,no_asistio_por,beneficio_canje_id,beneficio_reserva_id,descuento_beneficio,precio_final)
		values ($1,$2,$3,$4,nullif($5,''),$6,$7,$8,$9,$10,$11,nullif($12,''),nullif($13,''),$14,nullif($15,''),$16,nullif($17,''),$18,nullif($19,''),nullif($20,''),nullif($21,''),$22,$23)`,
		t.ID, t.ClienteID, t.ProfesionalID, t.TratamientoID, t.CategoriaID, t.Inicio, t.Fin, t.Estado, t.CreadoEn, t.PrecioTratamiento, t.Sena, t.PagoID, t.EstadoPago, t.ImporteDevolucion, t.EstadoDevolucion, t.TratamientoRealizadoEn, t.TratamientoRealizadoPor, t.NoAsistioEn, t.NoAsistioPor, t.BeneficioCanjeID, t.BeneficioReservaID, t.DescuentoBeneficio, t.PrecioFinal)
	return err
}

const carritoJSON = `select to_jsonb(x) from (
	select id, cliente_id as "clienteId", profesional_id as "profesionalId", tratamiento_id as "tratamientoId",
	 categoria_id as "categoriaId", inicio, fin, precio, moneda, sena, beneficio_canje_id as "beneficioCanjeId",
	 descuento_beneficio as "descuentoBeneficio", precio_final as "precioFinal", estado, expira_en as "expiraEn",
	 turno_id as "turnoId", pago_id as "pagoId", estado_pago as "estadoPago", preferencia_id as "preferenciaId",
	 url_pago as "urlPago", checkout_id as "checkoutId" from carritos_bloqueos`

func (p *postgresStore) carrito(ctx context.Context, id string) (bloqueoCarrito, error) {
	return decodeSQLJSON[bloqueoCarrito](p.runner(ctx).QueryRowContext(ctx, carritoJSON+` where id=$1) x`, id))
}

func (p *postgresStore) carritoForUpdate(ctx context.Context, id string) (bloqueoCarrito, error) {
	return decodeSQLJSON[bloqueoCarrito](p.runner(ctx).QueryRowContext(ctx, carritoJSON+` where id=$1 for update) x`, id))
}

func (p *postgresStore) insertarCarrito(ctx context.Context, c bloqueoCarrito) error {
	_, err := p.runner(ctx).ExecContext(ctx, `insert into carritos_bloqueos
		(id,cliente_id,profesional_id,tratamiento_id,categoria_id,inicio,fin,precio,moneda,sena,beneficio_canje_id,descuento_beneficio,precio_final,estado,expira_en,turno_id,pago_id,estado_pago,preferencia_id,url_pago,checkout_id)
		values ($1,$2,$3,$4,nullif($5,''),$6,$7,$8,$9,$10,nullif($11,''),$12,$13,$14,$15,nullif($16,''),nullif($17,''),nullif($18,''),nullif($19,''),nullif($20,''),nullif($21,''))`,
		c.ID, c.ClienteID, c.ProfesionalID, c.TratamientoID, c.CategoriaID, c.Inicio, c.Fin, c.Precio, c.Moneda, c.Sena, c.BeneficioCanjeID, c.DescuentoBeneficio, c.PrecioFinal, c.Estado, c.ExpiraEn, c.TurnoID, c.PagoID, c.EstadoPago, c.PreferenciaID, c.URLPago, c.CheckoutID)
	return err
}

func (p *postgresStore) carritosCliente(ctx context.Context, clienteID, estado string, ahora any) ([]bloqueoCarrito, error) {
	rows, err := p.runner(ctx).QueryContext(ctx, carritoJSON+` where cliente_id=$1 and estado=$2 and expira_en>$3 order by creado_en,inicio) x`, clienteID, estado, ahora)
	if err != nil {
		return nil, err
	}
	return decodeSQLRows[bloqueoCarrito](rows)
}

func (p *postgresStore) carritosCheckout(ctx context.Context, clienteID, estado string, ahora any) ([]bloqueoCarrito, error) {
	rows, err := p.runner(ctx).QueryContext(ctx, carritoJSON+` where cliente_id=$1 and estado=$2 and expira_en>$3 order by inicio) x`, clienteID, estado, ahora)
	if err != nil {
		return nil, err
	}
	return decodeSQLRows[bloqueoCarrito](rows)
}

func (p *postgresStore) carritosProfesionalFuturos(ctx context.Context, profesionalID, estado string, ahora any) ([]bloqueoCarrito, error) {
	rows, err := p.runner(ctx).QueryContext(ctx, carritoJSON+` where profesional_id=$1 and estado=$2 and expira_en>$3 and inicio>=$3) x`, profesionalID, estado, ahora)
	if err != nil {
		return nil, err
	}
	return decodeSQLRows[bloqueoCarrito](rows)
}

func (p *postgresStore) carritosClienteEnRango(ctx context.Context, clienteID, estado string, inicio, fin, ahora any) ([]bloqueoCarrito, error) {
	rows, err := p.runner(ctx).QueryContext(ctx, carritoJSON+` where cliente_id=$1 and estado=$2 and expira_en>$3 and inicio<$5 and fin>$4) x`, clienteID, estado, ahora, inicio, fin)
	if err != nil {
		return nil, err
	}
	return decodeSQLRows[bloqueoCarrito](rows)
}

func (p *postgresStore) actualizarPrecioCarrito(ctx context.Context, id, clienteID string, precio, final, descuento, sena float64) error {
	_, err := p.runner(ctx).ExecContext(ctx, `update carritos_bloqueos set precio=$3, precio_final=$4, descuento_beneficio=$5, sena=$6 where id=$1 and cliente_id=$2 and estado='pendiente_pago'`, id, clienteID, precio, final, descuento, sena)
	return err
}

func (p *postgresStore) asignarCheckoutCarrito(ctx context.Context, id, clienteID, checkoutID string, ahora any) (int64, error) {
	res, err := p.runner(ctx).ExecContext(ctx, `update carritos_bloqueos set checkout_id=$3 where id=$1 and cliente_id=$2 and estado='pendiente_pago' and checkout_id is null and expira_en>$4`, id, clienteID, checkoutID, ahora)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (p *postgresStore) confirmarCarrito(ctx context.Context, id, checkoutID, turnoID, pagoID string) (int64, error) {
	res, err := p.runner(ctx).ExecContext(ctx, `update carritos_bloqueos set estado='confirmado', turno_id=$3, pago_id=$4, expira_en=null, checkout_id=null where id=$1 and checkout_id=$2 and estado='pendiente_pago'`, id, checkoutID, turnoID, pagoID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (p *postgresStore) liberarCarrito(ctx context.Context, id, clienteID string, ahora any) (int64, error) {
	res, err := p.runner(ctx).ExecContext(ctx, `update carritos_bloqueos set estado='liberado', expira_en=$3 where id=$1 and cliente_id=$2 and estado='pendiente_pago' and checkout_id is null`, id, clienteID, ahora)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (p *postgresStore) borrarCarrito(ctx context.Context, id, clienteID string) error {
	_, err := p.runner(ctx).ExecContext(ctx, `delete from carritos_bloqueos where id=$1 and cliente_id=$2 and estado='pendiente_pago'`, id, clienteID)
	return err
}

func (p *postgresStore) borrarBloqueosTurno(ctx context.Context, turnoID string) error {
	_, err := p.runner(ctx).ExecContext(ctx, `delete from bloqueos_turnos where turno_id=$1`, turnoID)
	return err
}

func (p *postgresStore) borrarBloqueosVencidos(ctx context.Context, ahora any) error {
	_, err := p.runner(ctx).ExecContext(ctx, `delete from bloqueos_turnos where expira_en is not null and expira_en<=$1`, ahora)
	return err
}

func (p *postgresStore) insertarBloqueos(ctx context.Context, values []bloqueoTurno) error {
	tx, ownsTx := p.runner(ctx).(*sql.Tx)
	var txCtx context.Context
	if !ownsTx {
		var err error
		tx, err = p.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
		if err != nil {
			return err
		}
		defer tx.Rollback()
		txCtx = context.WithValue(ctx, reservasTxKey{}, tx)
		ctx = txCtx
	}
	runner := p.runner(ctx)
	for _, item := range values {
		_, err := runner.ExecContext(ctx, `insert into bloqueos_turnos (id,profesional_id,cliente_id,categoria_id,minuto,turno_id,expira_en)
			values ($1,$2,nullif($3,''),nullif($4,''),$5,$6,$7)`, fmt.Sprintf("%s:%d", item.ProfesionalID, item.Minuto), item.ProfesionalID, item.ClienteID, item.CategoriaID, item.Minuto, item.TurnoID, item.ExpiraEn)
		if err != nil {
			return err
		}
	}
	if ownsTx {
		return nil
	}
	return tx.Commit()
}

func (p *postgresStore) transferirBloqueos(ctx context.Context, origen, destino string) error {
	_, err := p.runner(ctx).ExecContext(ctx, `update bloqueos_turnos set turno_id=$2, expira_en=null where turno_id=$1`, origen, destino)
	return err
}

func (p *postgresStore) bloqueoMinutos(ctx context.Context, turnoID string, ahora any) (int64, error) {
	var count int64
	err := p.runner(ctx).QueryRowContext(ctx, `select count(*) from bloqueos_turnos where turno_id=$1 and (expira_en is null or expira_en>$2)`, turnoID, ahora).Scan(&count)
	return count, err
}

func (p *postgresStore) bloqueosProfesionalRango(ctx context.Context, profesionalID string, desde, hasta int64, ahora any) (int64, error) {
	var count int64
	err := p.runner(ctx).QueryRowContext(ctx, `select count(*) from bloqueos_turnos where profesional_id=$1 and minuto >= $2 and minuto < $3 and (expira_en is null or expira_en>$4)`, profesionalID, desde, hasta, ahora).Scan(&count)
	return count, err
}

func (p *postgresStore) bloqueosCategoriaClienteRango(ctx context.Context, clienteID, categoriaID string, desde, hasta int64, ahora any) (int64, error) {
	var count int64
	err := p.runner(ctx).QueryRowContext(ctx, `select count(*) from bloqueos_turnos where cliente_id=$1 and categoria_id=$2 and minuto >= $3 and minuto < $4 and (expira_en is null or expira_en>$5)`, clienteID, categoriaID, desde, hasta, ahora).Scan(&count)
	return count, err
}

func (p *postgresStore) carritosCategoriaClienteEnRango(ctx context.Context, clienteID, categoriaID string, inicio, fin, ahora any) ([]bloqueoCarrito, error) {
	rows, err := p.runner(ctx).QueryContext(ctx, carritoJSON+` where cliente_id=$1 and categoria_id=$2 and estado='pendiente_pago' and expira_en>$3 and inicio<$5 and fin>$4) x`, clienteID, categoriaID, ahora, inicio, fin)
	if err != nil {
		return nil, err
	}
	return decodeSQLRows[bloqueoCarrito](rows)
}

const checkoutJSON = `select to_jsonb(x) from (
	select id,cliente_id as "clienteId",idempotency_key as "idempotencyKey",item_ids as "itemIds",items,
	 subtotal,descuento,total,sena,estado,pago_id as "pagoId",turno_ids as "turnoIds",
	 creado_en as "creadoEn",actualizado_en as "actualizadoEn" from checkouts`

func (p *postgresStore) checkoutPorClienteClave(ctx context.Context, clienteID, clave string) (checkoutOperation, error) {
	return decodeSQLJSON[checkoutOperation](p.runner(ctx).QueryRowContext(ctx, checkoutJSON+` where cliente_id=$1 and idempotency_key=$2) x`, clienteID, clave))
}

func (p *postgresStore) checkoutPorIDCliente(ctx context.Context, id, clienteID string) (checkoutOperation, error) {
	return decodeSQLJSON[checkoutOperation](p.runner(ctx).QueryRowContext(ctx, checkoutJSON+` where id=$1 and cliente_id=$2) x`, id, clienteID))
}

func (p *postgresStore) checkoutPorID(ctx context.Context, id string) (checkoutOperation, error) {
	return decodeSQLJSON[checkoutOperation](p.runner(ctx).QueryRowContext(ctx, checkoutJSON+` where id=$1) x`, id))
}

func (p *postgresStore) checkout(ctx context.Context, id, clienteID string) (checkoutOperation, error) {
	return decodeSQLJSON[checkoutOperation](p.runner(ctx).QueryRowContext(ctx, checkoutJSON+` where id=$1 and cliente_id=$2 for update) x`, id, clienteID))
}

func (p *postgresStore) insertarCheckout(ctx context.Context, c checkoutOperation) error {
	_, err := p.runner(ctx).ExecContext(ctx, `insert into checkouts (id,cliente_id,idempotency_key,item_ids,items,subtotal,descuento,total,sena,estado,pago_id,turno_ids,creado_en,actualizado_en)
		values ($1,$2,$3,$4::jsonb,$5::jsonb,$6,$7,$8,$9,$10,nullif($11,''),$12::jsonb,$13,$14)`, c.ID, c.ClienteID, c.IdempotencyKey, mustJSON(c.ItemIDs), mustJSON(c.Items), c.Subtotal, c.Descuento, c.Total, c.Sena, c.Estado, c.PagoID, mustJSON(c.TurnoIDs), c.CreadoEn, c.ActualizadoEn)
	return err
}

func (p *postgresStore) actualizarCheckout(ctx context.Context, id, estado, pagoID string, actualizado any) (int64, error) {
	res, err := p.runner(ctx).ExecContext(ctx, `update checkouts set estado=$2,pago_id=nullif($3,''),actualizado_en=$4 where id=$1`, id, estado, pagoID, actualizado)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (p *postgresStore) limpiarCheckoutCarritos(ctx context.Context, itemIDs []string, clienteID, checkoutID string) error {
	for _, id := range itemIDs {
		if _, err := p.runner(ctx).ExecContext(ctx, `update carritos_bloqueos set checkout_id=null where id=$1 and cliente_id=$2 and checkout_id=$3`, id, clienteID, checkoutID); err != nil {
			return err
		}
	}
	return nil
}

func (p *postgresStore) marcarCheckoutResultado(ctx context.Context, id, estado, pagoID string, actualizado any) error {
	_, err := p.runner(ctx).ExecContext(ctx, `update checkouts set estado=$2,pago_id=nullif($3,''),actualizado_en=$4 where id=$1 and estado='processing'`, id, estado, pagoID, actualizado)
	return err
}

func (p *postgresStore) confirmarCheckoutEstado(ctx context.Context, id, pagoID string, actualizado any) (int64, error) {
	res, err := p.runner(ctx).ExecContext(ctx, `update checkouts set estado='confirmed',pago_id=$2,actualizado_en=$3 where id=$1 and estado='processing'`, id, pagoID, actualizado)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (p *postgresStore) insertarEvento(ctx context.Context, item eventoOutbox) error {
	_, err := p.runner(ctx).ExecContext(ctx, `insert into outbox_eventos (id,tipo,routing_key,payload,creado_en) values ($1,$2,$3,$4::jsonb,$5)`, item.ID, item.Tipo, item.RoutingKey, mustJSON(item.Payload), item.CreadoEn)
	return err
}

func (p *postgresStore) eventoPendiente(ctx context.Context) (eventoOutbox, error) {
	query := `select to_jsonb(x) from (select id,tipo,routing_key as "routingKey",payload,creado_en as "creadoEn",publicado_en as "publicadoEn",intentos from outbox_eventos where publicado_en is null order by creado_en limit 1) x`
	return decodeSQLJSON[eventoOutbox](p.runner(ctx).QueryRowContext(ctx, query))
}

func (p *postgresStore) publicarEvento(ctx context.Context, id string, ahora any) error {
	_, err := p.runner(ctx).ExecContext(ctx, `update outbox_eventos set publicado_en=$2,intentos=intentos+1 where id=$1 and publicado_en is null`, id, ahora)
	return err
}
