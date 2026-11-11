# ADR-015 — Checkout único con simulador de pagos

**Estado:** aplicado en el código de la referencia `develop` `ea1ead7e85f533ba54aff85bd95958bb749897a0`. Servicio Pagos interno, sin pasarela real ni cobros. El contrato actual es [reservas-checkout-v1](../contracts/reservas-checkout-v1.openapi.yaml) y [pagos-simulador-v1](../contracts/pagos-simulador-v1.openapi.yaml); ver además los manuales de [Reservas](../servicios/RESERVAS.md) y [Pagos](../servicios/PAGOS.md). El texto debajo documenta la decisión original; para nombres exactos de rutas y estados prevalece el código actual.

**Estado:** Aceptado e implementado localmente

**Fecha:** 2026-11-11
**Decisores:** Equipo MELT

## Contexto

La implementación anterior almacenaba cada retención por separado y creaba una operación de pago por turno. La demostración académica no dispone de una credencial externa de pago y necesita confirmar varios horarios en una única compra sin cobros reales.

## Decisión

1. Reservas sigue siendo dueño de agendas, turnos, retenciones, carrito lógico y checkout. Los elementos del único carrito activo de una cuenta cliente son sus retenciones `pendiente_pago` vigentes en `reservas_db.carritos_bloqueos`.
2. Reservas crea un registro `checkouts` con la identidad autenticada, los IDs de retención, una instantánea de importes, totales, estado y clave de idempotencia. El identificador de checkout es la referencia única de la operación de pago.
3. Los precios y duraciones se vuelven a consultar en Catálogo; los canjes se vuelven a reservar en Beneficios. El navegador no determina importes ni resultado.
4. Pagos continúa como microservicio independiente. Guarda una transacción simulada en su base propia `pagos_db`, con unicidad por checkout e idempotencia. Su resultado queda configurado en backend mediante `PAYMENT_SIMULATOR_RESULT` (`approved` por defecto, `rejected` para pruebas). Las rutas de operación exigen `X-Melt-Internal-Key`.
5. Ante aprobación, Reservas vuelve a validar todas las retenciones y los registros de bloqueo y confirma todos los turnos, actualiza los bloqueos y cierra el checkout dentro de una transacción MongoDB. Un error no deja turnos parcialmente confirmados; Reservas solicita la reversión de la simulación.
6. Rechazo no confirma turnos y conserva retenciones vigentes para reintentar. La expiración sigue siendo individual por elemento y de 10 minutos. Quitar un elemento libera únicamente sus minutos y su canje asociado.
7. La seña es 30 % del total actualizado después de descuentos. El saldo restante se muestra en el resumen y continúa fuera del cobro simulado. El pago por sí solo no genera eventos ni puntos.
8. La interfaz ofrece una sola acción «Confirmar compra». Los endpoints históricos por bloqueo se conservan para compatibilidad de clientes antiguos; la interfaz nueva no los utiliza.
9. Mercado Pago, sus SDK, preferencias, tokens, retornos y webhooks quedan fuera de la integración. No hay cobros reales.

## Consistencia y recuperación

El registro en Pagos y la transacción de Reservas pertenecen a servicios distintos, por lo que no forman una transacción distribuida. Ambas operaciones usan identificadores estables e idempotencia. Si Pagos aprueba pero la transacción de Reservas falla, se revierte la simulación y no se confirma ningún turno. Si la reversión falla, el checkout queda en `approval_needs_recovery` para recuperación operativa; sus retenciones permanecen vinculadas y no se permiten modificaciones silenciosas.

## Datos existentes y compatibilidad

No se elimina ni migra destructivamente `carritos_bloqueos`. Los documentos existentes siguen siendo visibles si están vigentes. La colección nueva `checkouts` conserva operaciones agrupadas sin TTL para soportar reintentos; Pagos crea `pagos_db.pagos_simulados` y `devoluciones_simuladas`. Los índices nuevos son aditivos. Los datos históricos de pagos externos no se transforman.

## Consecuencias

- La compra de varios horarios es una sola operación visible, con importe total autoritativo e idempotente.
- Pagos requiere acceso a MongoDB y su propio nombre de base (`PAGOS_MONGODB_DATABASE`); no depende de credenciales externas.
- La simulación sirve para la entrega académica y no representa una liquidación financiera real.
- La integración anterior descrita en [ADR-009](ADR-009-compatibilidad-carrito-sena.md) se conserva como historia de reglas y retenciones. Este ADR reemplaza únicamente la modalidad de pago por turno y define el checkout agrupado.
