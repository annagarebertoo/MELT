package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// almacenPostgres mantiene las mismas reglas de dominio que almacenMongo,
// pero las implementa con transacciones y restricciones de PostgreSQL.
type almacenPostgres struct{ db *sql.DB }

func (a *almacenPostgres) preparar(ctx context.Context) error {
	var missing string
	err := a.db.QueryRowContext(ctx, `select string_agg(required.name, ', ' order by required.name)
		from (values ('registros_externos'), ('movimientos_puntos'), ('recompensas'),
			('reglas_puntos_tratamientos'), ('canjes'), ('mensajes_fallidos')) as required(name)
		where to_regclass('public.' || required.name) is null`).Scan(&missing)
	if err != nil {
		return err
	}
	if missing != "" {
		return fmt.Errorf("faltan tablas de Beneficios en PostgreSQL: %s (aplicá infra/supabase/beneficios.sql)", missing)
	}
	return nil
}

func pgNoRows(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return mongo.ErrNoDocuments
	}
	return err
}

func pgDuplicado(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func esDuplicado(err error) bool {
	return mongo.IsDuplicateKeyError(err) || pgDuplicado(err)
}

const movimientoColumns = `solicitud_id, coalesce(usuario_id, ''), puntos, saldo_puntos,
	tipo, creado_en, coalesce(email_destino, ''), coalesce(descripcion, ''),
	coalesce(estado, ''), coalesce(nonce, ''), coalesce(token_hash, ''), vence_en`

func scanMovimiento(row interface{ Scan(...any) error }) (operacionPuntos, error) {
	var op operacionPuntos
	var vence sql.NullTime
	err := row.Scan(&op.SolicitudID, &op.UsuarioID, &op.Puntos, &op.SaldoPuntos,
		&op.Tipo, &op.CreadoEn, &op.EmailDestino, &op.Descripcion, &op.Estado,
		&op.Nonce, &op.TokenHash, &vence)
	if err != nil {
		return op, pgNoRows(err)
	}
	if vence.Valid {
		op.VenceEn = vence.Time
	}
	return op, nil
}

func (a *almacenPostgres) buscarSolicitud(ctx context.Context, id string) (Movimiento, error) {
	op, err := scanMovimiento(a.db.QueryRowContext(ctx, `select `+movimientoColumns+` from movimientos_puntos where solicitud_id=$1`, id))
	return op.Movimiento, err
}

func (a *almacenPostgres) acreditar(ctx context.Context, req AcreditarRequest) (Movimiento, bool, error) {
	return a.acreditarDetalle(ctx, req, "", "")
}

func (a *almacenPostgres) acreditarDetalle(ctx context.Context, req AcreditarRequest, email, descripcion string) (Movimiento, bool, error) {
	for intento := 0; intento < 4; intento++ {
		tx, err := a.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
		if err != nil {
			return Movimiento{}, false, err
		}
		previo, readErr := scanMovimiento(tx.QueryRowContext(ctx, `select `+movimientoColumns+` from movimientos_puntos where solicitud_id=$1`, req.SolicitudID))
		if readErr == nil {
			_ = tx.Rollback()
			if email != "" && previo.UsuarioID == "" && previo.EmailDestino == email && previo.Puntos == req.Puntos && previo.Descripcion == descripcion {
				return Movimiento{}, false, errOperacionPendiente
			}
			return resolverDetalle(previo.Movimiento, req, email, descripcion)
		}
		if !errors.Is(readErr, mongo.ErrNoDocuments) {
			_ = tx.Rollback()
			return Movimiento{}, false, readErr
		}
		resultado := Movimiento{SolicitudID: req.SolicitudID, UsuarioID: req.UsuarioID, Puntos: req.Puntos, Tipo: "acreditacion_externa", CreadoEn: time.Now().UTC().Truncate(time.Millisecond), EmailDestino: email, Descripcion: descripcion, Estado: "acreditada"}
		_, err = tx.ExecContext(ctx, `insert into movimientos_puntos
			(solicitud_id, usuario_id, puntos, saldo_puntos, tipo, creado_en, email_destino, descripcion, estado)
			values ($1, nullif($2,''), $3, 0, $4, $5, nullif($6,''), nullif($7,''), $8)`,
			resultado.SolicitudID, resultado.UsuarioID, resultado.Puntos, resultado.Tipo, resultado.CreadoEn, resultado.EmailDestino, resultado.Descripcion, resultado.Estado)
		if err != nil {
			_ = tx.Rollback()
			if pgDuplicado(err) {
				continue
			}
			return Movimiento{}, false, err
		}
		_, err = tx.ExecContext(ctx, `insert into registros_externos (id, usuario_id, saldo_puntos, creado_en)
			values ($1,$1,0,$2) on conflict (usuario_id) do nothing`, req.UsuarioID, resultado.CreadoEn)
		if err != nil {
			_ = tx.Rollback()
			return Movimiento{}, false, err
		}
		err = tx.QueryRowContext(ctx, `update registros_externos set saldo_puntos=saldo_puntos+$1
			where usuario_id=$2 and saldo_puntos <= $3 returning saldo_puntos`, req.Puntos, req.UsuarioID, maxPuntos-req.Puntos).Scan(&resultado.SaldoPuntos)
		if errors.Is(err, sql.ErrNoRows) {
			_ = tx.Rollback()
			return Movimiento{}, false, errLimite
		}
		if err != nil {
			_ = tx.Rollback()
			return Movimiento{}, false, err
		}
		if _, err = tx.ExecContext(ctx, `update movimientos_puntos set saldo_puntos=$1 where solicitud_id=$2`, resultado.SaldoPuntos, resultado.SolicitudID); err != nil {
			_ = tx.Rollback()
			return Movimiento{}, false, err
		}
		if err = tx.Commit(); err == nil {
			return resultado, false, nil
		}
		if !pgDuplicado(err) {
			return Movimiento{}, false, err
		}
	}
	previo, err := a.buscarSolicitud(ctx, req.SolicitudID)
	if err == nil {
		return resolverDetalle(previo, req, email, descripcion)
	}
	return Movimiento{}, false, errors.New("contención; reintentar solicitud")
}

func (a *almacenPostgres) consultar(ctx context.Context, id string) (int64, []Movimiento, error) {
	tx, err := a.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return 0, nil, err
	}
	defer tx.Rollback()
	var cuenta Cuenta
	err = tx.QueryRowContext(ctx, `select usuario_id, saldo_puntos, puntos_regalo, creado_en from registros_externos where usuario_id=$1`, id).Scan(&cuenta.UsuarioID, &cuenta.SaldoPuntos, &cuenta.PuntosRegalo, &cuenta.CreadoEn)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, nil, err
	}
	rows, err := tx.QueryContext(ctx, `select solicitud_id, coalesce(usuario_id,''), puntos, saldo_puntos, tipo, creado_en,
		coalesce(email_destino,''), coalesce(descripcion,''), coalesce(estado,'') from movimientos_puntos
		where usuario_id=$1 order by creado_en, solicitud_id`, id)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()
	movimientos := []Movimiento{}
	for rows.Next() {
		var m Movimiento
		if err := rows.Scan(&m.SolicitudID, &m.UsuarioID, &m.Puntos, &m.SaldoPuntos, &m.Tipo, &m.CreadoEn, &m.EmailDestino, &m.Descripcion, &m.Estado); err != nil {
			return 0, nil, err
		}
		movimientos = append(movimientos, m)
	}
	if err := rows.Err(); err != nil {
		return 0, nil, err
	}
	if cuenta.PuntosRegalo > 0 {
		movimientos = append([]Movimiento{{SolicitudID: "historico:" + id, UsuarioID: id, Puntos: cuenta.PuntosRegalo, Tipo: "acreditacion_historica", CreadoEn: cuenta.CreadoEn}}, movimientos...)
	}
	return cuenta.SaldoPuntos, movimientos, nil
}

func (a *almacenPostgres) obtenerOperacion(ctx context.Context, id string) (operacionPuntos, error) {
	return scanMovimiento(a.db.QueryRowContext(ctx, `select `+movimientoColumns+` from movimientos_puntos where solicitud_id=$1`, id))
}

func (a *almacenPostgres) guardarPendiente(ctx context.Context, op operacionPuntos) error {
	_, err := a.db.ExecContext(ctx, `insert into movimientos_puntos
		(solicitud_id, usuario_id, puntos, saldo_puntos, tipo, creado_en, email_destino, descripcion, estado, nonce, token_hash, vence_en)
		values ($1,nullif($2,''),$3,0,$4,$5,nullif($6,''),nullif($7,''),$8,$9,$10,$11)`,
		op.SolicitudID, op.UsuarioID, op.Puntos, op.Tipo, op.CreadoEn, op.EmailDestino, op.Descripcion, op.Estado, op.Nonce, op.TokenHash, op.VenceEn)
	return err
}

func (a *almacenPostgres) obtenerPorToken(ctx context.Context, hash string) (operacionPuntos, error) {
	return scanMovimiento(a.db.QueryRowContext(ctx, `select `+movimientoColumns+` from movimientos_puntos where token_hash=$1`, hash))
}

func (a *almacenPostgres) renovarPendiente(ctx context.Context, old, next operacionPuntos) (operacionPuntos, error) {
	op, err := scanMovimiento(a.db.QueryRowContext(ctx, `update movimientos_puntos set nonce=$1, token_hash=$2, vence_en=$3
		where solicitud_id=$4 and estado='pendiente' and token_hash=$5 returning `+movementColumnsQualified("movimientos_puntos"), next.Nonce, next.TokenHash, next.VenceEn, old.SolicitudID, old.TokenHash))
	if errors.Is(err, mongo.ErrNoDocuments) {
		return a.obtenerOperacion(ctx, old.SolicitudID)
	}
	return op, err
}

func movementColumnsQualified(table string) string {
	cols := []string{"solicitud_id", "coalesce(usuario_id, '')", "puntos", "saldo_puntos", "tipo", "creado_en", "coalesce(email_destino, '')", "coalesce(descripcion, '')", "coalesce(estado, '')", "coalesce(nonce, '')", "coalesce(token_hash, '')", "vence_en"}
	for i, col := range cols {
		if strings.HasPrefix(col, "coalesce(") {
			parts := strings.SplitN(strings.TrimSuffix(strings.TrimPrefix(col, "coalesce("), ")"), ",", 2)
			cols[i] = "coalesce(" + table + "." + parts[0] + "," + parts[1] + ")"
		} else {
			cols[i] = table + "." + col
		}
	}
	return strings.Join(cols, ",")
}

func (a *almacenPostgres) confirmarPendiente(ctx context.Context, id, user, email string) (Movimiento, bool, error) {
	tx, err := a.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Movimiento{}, false, err
	}
	defer tx.Rollback()
	op, err := scanMovimiento(tx.QueryRowContext(ctx, `select `+movementColumnsQualified("movimientos_puntos")+` from movimientos_puntos where solicitud_id=$1 for update`, id))
	if err != nil {
		return Movimiento{}, false, err
	}
	if op.EmailDestino != email {
		return Movimiento{}, false, errDestinatario
	}
	if op.UsuarioID != "" {
		if op.UsuarioID != user {
			return Movimiento{}, false, errDestinatario
		}
		return op.Movimiento, true, nil
	}
	if op.Estado != "pendiente" {
		return Movimiento{}, false, errInvitacion
	}
	if _, err = tx.ExecContext(ctx, `insert into registros_externos (id, usuario_id, saldo_puntos, creado_en) values ($1,$1,0,$2) on conflict (usuario_id) do nothing`, user, time.Now().UTC()); err != nil {
		return Movimiento{}, false, err
	}
	var saldo int64
	err = tx.QueryRowContext(ctx, `update registros_externos set saldo_puntos=saldo_puntos+$1 where usuario_id=$2 and saldo_puntos <= $3 returning saldo_puntos`, op.Puntos, user, maxPuntos-op.Puntos).Scan(&saldo)
	if errors.Is(err, sql.ErrNoRows) {
		return Movimiento{}, false, errLimite
	}
	if err != nil {
		return Movimiento{}, false, err
	}
	result := op.Movimiento
	result.UsuarioID, result.SaldoPuntos, result.Estado = user, saldo, "acreditada"
	result.CreadoEn = time.Now().UTC().Truncate(time.Millisecond)
	_, err = tx.ExecContext(ctx, `update movimientos_puntos set usuario_id=$1, saldo_puntos=$2, estado=$3, creado_en=$4 where solicitud_id=$5 and estado='pendiente'`, user, saldo, result.Estado, result.CreadoEn, id)
	if err != nil {
		return Movimiento{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return Movimiento{}, false, err
	}
	return result, false, nil
}

func (a *almacenPostgres) listarRecompensas(ctx context.Context, activas bool) ([]Recompensa, error) {
	query := `select id,nombre,descripcion,tipo,costo_puntos,coalesce(porcentaje,0),coalesce(tratamiento_id,''),activa,actualizada_en from recompensas`
	if activas {
		query += ` where activa=true`
	}
	query += ` order by nombre`
	rows, err := a.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Recompensa{}
	for rows.Next() {
		var r Recompensa
		if err := rows.Scan(&r.ID, &r.Nombre, &r.Descripcion, &r.Tipo, &r.CostoPuntos, &r.Porcentaje, &r.TratamientoID, &r.Activa, &r.ActualizadaEn); err != nil {
			return nil, err
		}
		items = append(items, r)
	}
	return items, rows.Err()
}

func (a *almacenPostgres) guardarRecompensa(ctx context.Context, r Recompensa) error {
	r.ID, r.Nombre, r.Descripcion, r.TratamientoID = strings.TrimSpace(r.ID), strings.TrimSpace(r.Nombre), strings.TrimSpace(r.Descripcion), strings.TrimSpace(r.TratamientoID)
	r.ActualizadaEn = time.Now().UTC().Truncate(time.Millisecond)
	_, err := a.db.ExecContext(ctx, `insert into recompensas (id,nombre,descripcion,tipo,costo_puntos,porcentaje,tratamiento_id,activa,actualizada_en)
		values ($1,$2,$3,$4,$5,$6,nullif($7,''),$8,$9) on conflict (id) do update set nombre=excluded.nombre,descripcion=excluded.descripcion,
		tipo=excluded.tipo,costo_puntos=excluded.costo_puntos,porcentaje=excluded.porcentaje,tratamiento_id=excluded.tratamiento_id,activa=excluded.activa,actualizada_en=excluded.actualizada_en`,
		r.ID, r.Nombre, r.Descripcion, r.Tipo, r.CostoPuntos, r.Porcentaje, r.TratamientoID, r.Activa, r.ActualizadaEn)
	return err
}

func (a *almacenPostgres) guardarRegla(ctx context.Context, r ReglaTratamiento) error {
	r.TratamientoID = strings.TrimSpace(r.TratamientoID)
	if r.TratamientoID == "" || r.Puntos < 0 || r.Puntos > maxPuntos {
		return errors.New("regla inválida")
	}
	r.ActualizadaEn = time.Now().UTC().Truncate(time.Millisecond)
	_, err := a.db.ExecContext(ctx, `insert into reglas_puntos_tratamientos (tratamiento_id,puntos,actualizada_en) values ($1,$2,$3)
		on conflict (tratamiento_id) do update set puntos=excluded.puntos,actualizada_en=excluded.actualizada_en`, r.TratamientoID, r.Puntos, r.ActualizadaEn)
	return err
}

func (a *almacenPostgres) listarReglas(ctx context.Context) ([]ReglaTratamiento, error) {
	rows, err := a.db.QueryContext(ctx, `select tratamiento_id,puntos,actualizada_en from reglas_puntos_tratamientos order by tratamiento_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ReglaTratamiento{}
	for rows.Next() {
		var r ReglaTratamiento
		if err := rows.Scan(&r.TratamientoID, &r.Puntos, &r.ActualizadaEn); err != nil {
			return nil, err
		}
		items = append(items, r)
	}
	return items, rows.Err()
}

func (a *almacenPostgres) canjear(ctx context.Context, userID, rewardID, requestID string) (Canje, bool, error) {
	userID, rewardID, requestID = strings.TrimSpace(userID), strings.TrimSpace(rewardID), strings.TrimSpace(requestID)
	if userID == "" || rewardID == "" || requestID == "" || len(requestID) > 128 {
		return Canje{}, false, errors.New("solicitud inválida")
	}
	ledgerID := "canje:" + requestID
	if prev, err := a.buscarSolicitud(ctx, ledgerID); err == nil {
		if prev.UsuarioID != userID || prev.Descripcion != rewardID {
			return Canje{}, false, errConflicto
		}
		item, err := a.obtenerCanjeSolicitud(ctx, requestID)
		return item, true, err
	} else if !errors.Is(err, mongo.ErrNoDocuments) {
		return Canje{}, false, err
	}
	tx, err := a.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Canje{}, false, err
	}
	defer tx.Rollback()
	var reward Recompensa
	err = tx.QueryRowContext(ctx, `select id,nombre,descripcion,tipo,costo_puntos,coalesce(porcentaje,0),coalesce(tratamiento_id,''),activa,actualizada_en from recompensas where id=$1 and activa=true for update`, rewardID).Scan(&reward.ID, &reward.Nombre, &reward.Descripcion, &reward.Tipo, &reward.CostoPuntos, &reward.Porcentaje, &reward.TratamientoID, &reward.Activa, &reward.ActualizadaEn)
	if errors.Is(err, sql.ErrNoRows) {
		return Canje{}, false, errRecompensaNoExiste
	}
	if err != nil {
		return Canje{}, false, err
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	var saldo int64
	err = tx.QueryRowContext(ctx, `update registros_externos set saldo_puntos=saldo_puntos-$1 where usuario_id=$2 and saldo_puntos >= $1 returning saldo_puntos`, reward.CostoPuntos, userID).Scan(&saldo)
	if errors.Is(err, sql.ErrNoRows) {
		return Canje{}, false, errSaldoInsuficiente
	}
	if err != nil {
		return Canje{}, false, err
	}
	_, err = tx.ExecContext(ctx, `insert into movimientos_puntos (solicitud_id,usuario_id,puntos,saldo_puntos,tipo,creado_en,descripcion,estado)
		values ($1,$2,$3,$4,'canje',$5,$6,'confirmado')`, ledgerID, userID, -reward.CostoPuntos, saldo, now, rewardID)
	if err != nil {
		return Canje{}, false, err
	}
	rewardJSON, err := json.Marshal(reward)
	if err != nil {
		return Canje{}, false, err
	}
	result := Canje{ID: bson.NewObjectID().Hex(), SolicitudID: requestID, UsuarioID: userID, Recompensa: reward, Estado: "disponible", CreadoEn: now}
	_, err = tx.ExecContext(ctx, `insert into canjes (id,solicitud_id,usuario_id,recompensa,estado,creado_en) values ($1,$2,$3,$4::jsonb,$5,$6)`, result.ID, requestID, userID, string(rewardJSON), result.Estado, now)
	if err != nil {
		return Canje{}, false, err
	}
	if err = tx.Commit(); err != nil {
		if pgDuplicado(err) {
			prev, lookupErr := a.buscarSolicitud(ctx, ledgerID)
			if lookupErr == nil && prev.UsuarioID == userID && prev.Descripcion == rewardID {
				item, getErr := a.obtenerCanjeSolicitud(ctx, requestID)
				return item, getErr == nil, getErr
			}
			return Canje{}, false, errConflicto
		}
		return Canje{}, false, err
	}
	return result, false, nil
}

func (a *almacenPostgres) obtenerCanjeSolicitud(ctx context.Context, solicitudID string) (Canje, error) {
	var item Canje
	var reward []byte
	var reservaExpira, aplicado sql.NullTime
	var reservaID sql.NullString
	err := a.db.QueryRowContext(ctx, `select id,solicitud_id,usuario_id,recompensa,estado,reserva_id,coalesce(precio_original,0),coalesce(descuento,0),coalesce(precio_final,0),reserva_expira_en,creado_en,aplicado_en from canjes where solicitud_id=$1`, solicitudID).
		Scan(&item.ID, &item.SolicitudID, &item.UsuarioID, &reward, &item.Estado, &reservaID, &item.PrecioOriginal, &item.Descuento, &item.PrecioFinal, &reservaExpira, &item.CreadoEn, &aplicado)
	if err != nil {
		return item, pgNoRows(err)
	}
	if err := json.Unmarshal(reward, &item.Recompensa); err != nil {
		return item, err
	}
	if reservaID.Valid {
		item.ReservaID = reservaID.String
	}
	if reservaExpira.Valid {
		item.ReservaExpiraEn = &reservaExpira.Time
	}
	if aplicado.Valid {
		item.AplicadoEn = &aplicado.Time
	}
	return item, nil
}

func (a *almacenPostgres) canjeByID(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id, user string, lock bool) (Canje, error) {
	query := `select id,solicitud_id,usuario_id,recompensa,estado,reserva_id,coalesce(precio_original,0),coalesce(descuento,0),coalesce(precio_final,0),reserva_expira_en,creado_en,aplicado_en from canjes where id=$1 and usuario_id=$2`
	if lock {
		query += ` for update`
	}
	var item Canje
	var reward []byte
	var rid sql.NullString
	var expiry, applied sql.NullTime
	err := q.QueryRowContext(ctx, query, id, user).Scan(&item.ID, &item.SolicitudID, &item.UsuarioID, &reward, &item.Estado, &rid, &item.PrecioOriginal, &item.Descuento, &item.PrecioFinal, &expiry, &item.CreadoEn, &applied)
	if err != nil {
		return item, pgNoRows(err)
	}
	if err = json.Unmarshal(reward, &item.Recompensa); err != nil {
		return item, err
	}
	if rid.Valid {
		item.ReservaID = rid.String
	}
	if expiry.Valid {
		item.ReservaExpiraEn = &expiry.Time
	}
	if applied.Valid {
		item.AplicadoEn = &applied.Time
	}
	return item, nil
}

func (a *almacenPostgres) listarCanjes(ctx context.Context, userID string) ([]Canje, error) {
	if _, err := a.db.ExecContext(ctx, `update canjes set estado='disponible',reserva_id=null,reserva_expira_en=null,precio_original=0,descuento=0,precio_final=0 where usuario_id=$1 and estado='reservado' and reserva_expira_en <= now()`, userID); err != nil {
		return nil, err
	}
	rows, err := a.db.QueryContext(ctx, `select id,solicitud_id,usuario_id,recompensa,estado,reserva_id,coalesce(precio_original,0),coalesce(descuento,0),coalesce(precio_final,0),reserva_expira_en,creado_en,aplicado_en from canjes where usuario_id=$1 order by creado_en desc`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Canje{}
	for rows.Next() {
		var item Canje
		var reward []byte
		var rid sql.NullString
		var expiry, applied sql.NullTime
		if err := rows.Scan(&item.ID, &item.SolicitudID, &item.UsuarioID, &reward, &item.Estado, &rid, &item.PrecioOriginal, &item.Descuento, &item.PrecioFinal, &expiry, &item.CreadoEn, &applied); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(reward, &item.Recompensa); err != nil {
			return nil, err
		}
		if rid.Valid {
			item.ReservaID = rid.String
		}
		if expiry.Valid {
			item.ReservaExpiraEn = &expiry.Time
		}
		if applied.Valid {
			item.AplicadoEn = &applied.Time
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (a *almacenPostgres) reservarCanje(ctx context.Context, id, user, reserva, tratamiento string, precio float64) (Canje, error) {
	if precio < 0 || math.IsNaN(precio) || math.IsInf(precio, 0) {
		return Canje{}, errors.New("precio inválido")
	}
	tx, err := a.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Canje{}, err
	}
	defer tx.Rollback()
	item, err := a.canjeByID(ctx, tx, id, user, true)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return item, errCanjeNoDisponible
	}
	if err != nil {
		return item, err
	}
	now := time.Now().UTC()
	if item.Estado == "aplicado" {
		if item.ReservaID == reserva {
			return item, nil
		}
		return item, errCanjeNoDisponible
	}
	if item.Estado == "reservado" && item.ReservaID == reserva {
		expires := now.Add(10 * time.Minute).Truncate(time.Millisecond)
		_, err = tx.ExecContext(ctx, `update canjes set reserva_expira_en=$1 where id=$2`, expires, id)
		if err != nil {
			return item, err
		}
		item.ReservaExpiraEn = &expires
		if err = tx.Commit(); err != nil {
			return item, err
		}
		return item, nil
	}
	if item.Estado == "reservado" && (item.ReservaExpiraEn == nil || item.ReservaExpiraEn.After(now)) {
		return item, errCanjeNoDisponible
	}
	if item.Estado != "disponible" && item.Estado != "reservado" {
		return item, errCanjeNoDisponible
	}
	if item.Estado == "reservado" && item.ReservaExpiraEn == nil {
		return item, errCanjeNoDisponible
	}
	if item.Recompensa.Tipo == "tratamiento_gratis" && item.Recompensa.TratamientoID != tratamiento {
		return item, errors.New("la recompensa no aplica a este tratamiento")
	}
	descuento := 0.0
	if item.Recompensa.Tipo == "tratamiento_gratis" {
		descuento = precio
	} else {
		descuento = math.Round(precio*float64(item.Recompensa.Porcentaje)) / 100
	}
	expires := now.Add(10 * time.Minute).Truncate(time.Millisecond)
	item.Estado, item.ReservaID, item.ReservaExpiraEn = "reservado", reserva, &expires
	item.PrecioOriginal, item.Descuento, item.PrecioFinal = precio, descuento, math.Max(0, precio-descuento)
	_, err = tx.ExecContext(ctx, `update canjes set estado='reservado',reserva_id=$1,reserva_expira_en=$2,precio_original=$3,descuento=$4,precio_final=$5 where id=$6`, reserva, expires, precio, descuento, item.PrecioFinal, id)
	if err != nil {
		return item, err
	}
	if err = tx.Commit(); err != nil {
		return item, err
	}
	return item, nil
}

func (a *almacenPostgres) actualizarCanje(ctx context.Context, id, user, reserva, accion string) (Canje, error) {
	tx, err := a.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Canje{}, err
	}
	defer tx.Rollback()
	item, err := a.canjeByID(ctx, tx, id, user, true)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return item, errCanjeNoDisponible
	}
	if err != nil {
		return item, err
	}
	if accion == "restituir" && item.Estado == "disponible" {
		return item, nil
	}
	if item.ReservaID != reserva {
		return item, errCanjeNoDisponible
	}
	switch accion {
	case "aplicar":
		if item.Estado == "aplicado" {
			return item, nil
		}
		if item.Estado != "reservado" {
			return item, errCanjeNoDisponible
		}
		now := time.Now().UTC().Truncate(time.Millisecond)
		_, err = tx.ExecContext(ctx, `update canjes set estado='aplicado',aplicado_en=$1,reserva_expira_en=null where id=$2`, now, id)
	case "liberar":
		if item.Estado == "disponible" {
			return item, nil
		}
		if item.Estado != "reservado" {
			return item, errCanjeNoDisponible
		}
		_, err = tx.ExecContext(ctx, `update canjes set estado='disponible',reserva_id=null,reserva_expira_en=null,precio_original=0,descuento=0,precio_final=0 where id=$1`, id)
	case "restituir":
		if item.Estado != "aplicado" {
			return item, errCanjeNoDisponible
		}
		_, err = tx.ExecContext(ctx, `update canjes set estado='disponible',reserva_id=null,reserva_expira_en=null,precio_original=0,descuento=0,precio_final=0 where id=$1`, id)
		if err == nil {
			movementID := "restitucion:beneficio:" + reserva
			_, err = tx.ExecContext(ctx, `insert into movimientos_puntos (solicitud_id,usuario_id,puntos,saldo_puntos,tipo,creado_en,descripcion,estado) values ($1,$2,0,0,'restitucion_beneficio',$3,$4,'confirmado') on conflict (solicitud_id) do nothing`, movementID, user, time.Now().UTC().Truncate(time.Millisecond), item.Recompensa.Nombre)
		}
	default:
		return item, errors.New("acción desconocida")
	}
	if err != nil {
		return item, err
	}
	if err = tx.Commit(); err != nil {
		return item, err
	}
	return a.canjeByID(ctx, a.db, id, user, false)
}

func (a *almacenPostgres) procesarEventoTratamiento(ctx context.Context, eventID, userID, treatmentID string) error {
	if strings.TrimSpace(eventID) == "" || strings.TrimSpace(userID) == "" || strings.TrimSpace(treatmentID) == "" {
		return errors.New("evento de tratamiento inválido")
	}
	id := "evento:tratamiento-realizado:" + eventID
	if _, err := a.buscarSolicitud(ctx, id); err == nil {
		return nil
	} else if !errors.Is(err, mongo.ErrNoDocuments) {
		return err
	}
	var rule ReglaTratamiento
	err := a.db.QueryRowContext(ctx, `select tratamiento_id,puntos,actualizada_en from reglas_puntos_tratamientos where tratamiento_id=$1`, treatmentID).Scan(&rule.TratamientoID, &rule.Puntos, &rule.ActualizadaEn)
	if errors.Is(err, sql.ErrNoRows) {
		return errReglaNoExiste
	}
	if err != nil {
		return err
	}
	if rule.Puntos == 0 {
		return nil
	}
	_, _, err = a.acreditarDetalle(ctx, AcreditarRequest{SolicitudID: id, UsuarioID: userID, Puntos: rule.Puntos}, "", "Tratamiento realizado: "+treatmentID)
	return err
}

// Fallidos almacenados para inspección y reintento administrativo.
func (a *almacenPostgres) guardarMensajeFallido(ctx context.Context, m mensajeFallido) error {
	evento, err := json.Marshal(m.Evento)
	if err != nil {
		return err
	}
	_, err = a.db.ExecContext(ctx, `insert into mensajes_fallidos(id,evento,error,intentos,estado,creado_en,reintentado_en) values($1,$2::jsonb,$3,$4,$5,$6,$7)
		on conflict(id) do update set evento=excluded.evento,error=excluded.error,intentos=excluded.intentos,estado=excluded.estado`, m.ID, string(evento), m.Error, m.Intentos, m.Estado, m.CreadoEn, m.ReintentadoEn)
	return err
}

func (a *almacenPostgres) listarMensajesFallidos(ctx context.Context) ([]mensajeFallido, error) {
	rows, err := a.db.QueryContext(ctx, `select id,evento,error,intentos,estado,creado_en,reintentado_en from mensajes_fallidos where estado='pendiente' order by creado_en desc limit 50`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []mensajeFallido{}
	for rows.Next() {
		var m mensajeFallido
		var event []byte
		var retried sql.NullTime
		if err := rows.Scan(&m.ID, &event, &m.Error, &m.Intentos, &m.Estado, &m.CreadoEn, &retried); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(event, &m.Evento); err != nil {
			return nil, err
		}
		if retried.Valid {
			m.ReintentadoEn = &retried.Time
		}
		items = append(items, m)
	}
	return items, rows.Err()
}

func (a *almacenPostgres) obtenerMensajeFallido(ctx context.Context, id string) (mensajeFallido, error) {
	var m mensajeFallido
	var event []byte
	var retried sql.NullTime
	err := a.db.QueryRowContext(ctx, `select id,evento,error,intentos,estado,creado_en,reintentado_en from mensajes_fallidos where id=$1 and estado='pendiente'`, id).Scan(&m.ID, &event, &m.Error, &m.Intentos, &m.Estado, &m.CreadoEn, &retried)
	if err != nil {
		return m, pgNoRows(err)
	}
	if err = json.Unmarshal(event, &m.Evento); err != nil {
		return m, err
	}
	if retried.Valid {
		m.ReintentadoEn = &retried.Time
	}
	return m, nil
}

func (a *almacenPostgres) cambiarEstadoFallido(ctx context.Context, id, desde, hacia string, cuando *time.Time) (bool, error) {
	res, err := a.db.ExecContext(ctx, `update mensajes_fallidos set estado=$1,reintentado_en=coalesce($2,reintentado_en) where id=$3 and estado=$4`, hacia, cuando, id, desde)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

var _ almacenPuntos = (*almacenPostgres)(nil)
var _ almacenInvitaciones = (*almacenPostgres)(nil)
var _ programaStore = (*almacenPostgres)(nil)
