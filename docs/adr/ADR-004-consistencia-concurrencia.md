# ADR-004 — Consistencia y concurrencia

**Estado:** Aceptado  
**Fecha:** 2026-11-24  
**Decisores:** Equipo MELT  
**Requisito:** D4 y sección 2.8 del enunciado («al menos una operación que requiera garantías de consistencia, de manera que no pueda duplicarse ni perderse información»).

## Contexto y problema

La acción principal de MELT es **reservar y pagar turnos de estética**. Dos clientes pueden pedir el mismo horario a la vez, un mismo cliente puede reenviar el checkout (doble clic, reintento del navegador), Mercado Pago puede entregar el mismo Webhook varias veces y RabbitMQ puede reentregar un evento. Si cualquiera de esos casos se resolviera mal, aparecerían turnos superpuestos, cobros duplicados, puntos acreditados dos veces o saldos negativos.

Los datos que exigen estas garantías viven en PostgreSQL (Reservas y Beneficios, ver [ADR-003](ADR-003-persistencia.md)). Pagos usa MongoDB, donde la idempotencia se apoya en claves únicas.

## Operaciones que requieren garantías

| Operación | Riesgo | Garantía elegida | Dónde |
|---|---|---|---|
| Retener y confirmar un horario | Dos clientes con el mismo profesional y minuto; un cliente con dos tratamientos de la misma categoría a la vez | Restricción `exclude using gist (profesional_id with =, tstzrange(inicio, fin) with &&)` sobre `turnos`; índices únicos sobre `bloqueos_turnos (profesional_id, minuto)` y por cliente/categoría/minuto. La base rechaza el segundo intento aunque ambos lleguen en el mismo milisegundo. | `infra/supabase/reservas.sql`, `services/reservas/cmd/api/postgres_store.go` |
| Checkout agrupado | Doble envío crea dos cobros | `unique (cliente_id, idempotency_key)` en `checkouts`; la repetición devuelve la misma operación. Transacción `SERIALIZABLE` con reintento acotado ante `40001`/`40P01`. | `checkout.go`, `postgres_store.go` |
| Confirmación del pago | Webhook y retorno llegan juntos; pago con importe distinto | Bloqueo `for update` del checkout; la confirmación de todos los turnos y el outbox van en **una** transacción. Si algo falla, se revierte todo y se pide **una** devolución con `Idempotency-Key`. Pagos verifica la firma HMAC del Webhook y que el importe coincida antes de notificar. | `services/reservas`, `services/pagos/main.go` |
| Publicar «tratamiento realizado» | El turno se completa pero el evento se pierde si el broker está caído | **Outbox transaccional**: el cambio de estado y el evento se escriben juntos; un publicador entrega luego con retroceso exponencial (1,2 s → 10 s) y drenado inmediato. Entrega *al menos una vez*. | `services/reservas/cmd/api/events.go` |
| Acreditar puntos | Reentrega del evento acredita dos veces | `movimientos_puntos.solicitud_id` es clave primaria derivada del `eventId`; la inserción repetida no suma. Mensajes no procesables van a la DLQ y a `mensajes_fallidos`. | `services/beneficios/cmd/api/consumer.go`, `postgres_store.go` |
| Canjear recompensa | Dos canjes simultáneos dejan el saldo negativo | Transacción `SERIALIZABLE`, `for update` sobre la recompensa, `check (saldo_puntos >= 0)` y `canjes.solicitud_id unique`. | `services/beneficios/cmd/api/postgres_store.go` |
| Devolución | Reintento devuelve dos veces o más que lo cobrado | Colección de devoluciones con `_id = Idempotency-Key`; se rechaza con 409 si `devuelto + importe > cobrado`. | `services/pagos/main.go` |

## Alternativas consideradas

1. **Bloqueo distribuido (Redis/Memcached) antes de escribir.** Agrega una dependencia más y no protege si el lock expira a mitad de la operación. Descartado: la base ya ofrece la garantía.
2. **Verificar-y-luego-insertar en la aplicación.** Tiene carrera entre la lectura y la escritura. Descartado; se usa sólo como mensaje amable antes de que la restricción decida.
3. **Saga con compensaciones para todo el checkout.** Útil entre servicios, pero la confirmación de turnos ocurre dentro de un solo servicio: una transacción local es más simple y fuerte. Se conserva una única compensación (devolución) cuando el cobro ya ocurrió y la confirmación no puede completarse.
4. **Publicar directamente a RabbitMQ tras el commit.** Si el proceso cae entre ambos pasos, el evento se pierde. Descartado a favor del outbox.

## Decisión

- La base de datos es la autoridad sobre los invariantes: restricciones de exclusión, unicidad y `check`, no validaciones en memoria.
- Las operaciones de escritura críticas usan transacciones `SERIALIZABLE` con hasta tres intentos ante conflicto de serialización o deadlock.
- Toda operación que un cliente, el proveedor o el broker puedan repetir lleva una clave de idempotencia persistida (checkout, Webhook, eventos, canjes, devoluciones).
- La mensajería es *al menos una vez* con consumidor idempotente; no se promete *exactamente una vez*.

## Consecuencias

- Bajo contención, algunas solicitudes reciben 409 o se reintentan internamente; es preferible a duplicar.
- `SERIALIZABLE` tiene costo en throughput; se acota a las operaciones de la tabla, no a las lecturas.
- El outbox introduce demora: en la caída controlada de RabbitMQ del 24/11 los 10 eventos retenidos se acreditaron 8,5 s después de que el broker volvió (ver [POSTMORTEM](../POSTMORTEM.md)).
- El evento puede llegar más de una vez y desordenado entre tipos; Beneficios debe seguir deduplicando por `eventId`.

## Verificación

Pruebas de integración contra PostgreSQL y RabbitMQ reales y descartables (`compose.test.yaml`, job **Cobertura** y **Pruebas L1–L3** en CI):

- `TestL1RetencionesConcurrentesNoDuplicanHorario`, `TestL1MismoClienteNoSuperponeCategoriaEnParalelo`, `TestL1ControlDeAgendaSoportaPrimerasReservasSimultaneas`.
- `TestL1CheckoutIdempotenteBajoConcurrencia`, `TestL1ConfirmacionDePagoConcurrenteNoDuplicaTurnosNiDevuelve`, `TestL1FalloDeConfirmacionRevierteTodoYDevuelveUnaVez`, `TestL1RetencionVencidaNoSeConvierteEnTurno`, `TestL1PagoRechazadoLiberaCheckoutSinTurnos`, `TestL1CancelacionConcurrenteEsIdempotente`.
- `TestL2OutboxRetieneEventoSiElBrokerNoEstaYLoRecupera`, `TestL2ReplayDelOutboxConservaIdentidadParaDeduplicar`, `TestL2ConsumidorAcreditaUnaVezConReentregasYReplicas`, `TestL2EventosInvalidosVanALaDLQSinBloquearElConsumidor`.
- `TestProgramaCanjesConcurrentesNoSobregiranElSaldo` (8 canjes simultáneos sobre un saldo que alcanza para uno: exactamente uno se aprueba).
- Pagos: `flujo_test.go` cubre preferencia idempotente, Webhook con firma o importe inválido, devolución idempotente y devolución que supera lo cobrado (409).
- Ensayo manual: caída de RabbitMQ con turnos completados durante la caída; 10/10 acreditados, 0 duplicados ([evidencia](../evidence/2026-11-24/caida-rabbitmq-timeline.txt)).
