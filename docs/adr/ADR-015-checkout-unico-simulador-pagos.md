# ADR-015 — Checkout único con Mercado Pago

**Estado:** aplicado para Checkout Pro en modo de prueba. La versión anterior usaba un simulador y queda reemplazada por esta enmienda. Ver [contratos de Reservas](../contracts/reservas-checkout-v1.openapi.yaml) y [Pagos](../contracts/pagos-checkout-pro-v1.openapi.yaml), además de los manuales de [Reservas web](../RESERVAS-WEB.md) y [Pagos](../servicios/PAGOS.md).

**Estado:** Aceptado e implementado localmente

**Fecha:** 2026-11-11
**Decisores:** Equipo MELT

## Contexto

La compra agrupa horarios retenidos y cobra una sola seña. Para esta entrega se usa Checkout Pro con credenciales de prueba; no deben configurarse credenciales de producción.

## Decisión

1. Reservas sigue siendo dueño de agendas, turnos, retenciones, carrito lógico y checkout. Los elementos del único carrito activo de una cuenta cliente son sus retenciones `pendiente_pago` vigentes en la tabla `carritos_bloqueos` de PostgreSQL Supabase MELT-Reservas.
2. Reservas crea un registro `checkouts` con la identidad autenticada, los IDs de retención, una instantánea de importes, totales, estado y clave de idempotencia. El identificador de checkout es la referencia única de la operación de pago.
3. Los precios y duraciones se vuelven a consultar en Catálogo; los canjes se vuelven a reservar en Beneficios. El navegador no determina importes ni resultado.
4. Pagos continúa como microservicio independiente. Crea preferencias de Checkout Pro, consulta el estado del pago en Mercado Pago y procesa devoluciones. Pagos registra preferencia, referencia e IDs de proveedor en su base Mongo `pagos_db`; las llamadas internas requieren `X-Melt-Internal-Key`. Sólo se usa el Access Token copiado desde el entorno de prueba y el enlace `sandbox_init_point`.
5. Ante aprobación verificada (por retorno o Webhook firmado), Reservas vuelve a validar todas las retenciones y los registros de bloqueo y confirma todos los turnos dentro de una transacción PostgreSQL. Un error no deja turnos parcialmente confirmados.
6. Rechazo no confirma turnos y conserva retenciones vigentes para reintentar. La expiración sigue siendo individual por elemento y de 10 minutos. Quitar un elemento libera únicamente sus minutos y su canje asociado.
7. La seña es 30 % del total actualizado después de descuentos. El saldo restante se muestra en el resumen y queda fuera de este checkout. El pago por sí solo no genera eventos ni puntos.
8. La interfaz ofrece una sola acción «Confirmar compra». Los endpoints históricos por bloqueo se conservan para compatibilidad de clientes antiguos; la interfaz nueva no los utiliza.
9. Pagos redirige al checkout hospedado, valida retorno y firma de Webhook y consulta el pago en servidor. El Gateway sólo publica la ruta del Webhook; ninguna clave de Mercado Pago llega al navegador.

## Consistencia y recuperación

El registro en Pagos y la transacción de Reservas pertenecen a servicios distintos, por lo que no forman una transacción distribuida. Ambas operaciones usan identificadores estables e idempotencia. Si Mercado Pago aprueba pero la transacción de Reservas falla o vencen las retenciones, no se confirman turnos parcialmente y se requiere revisión operativa del pago y eventual devolución.

## Datos existentes y compatibilidad

La migración vigente define `carritos_bloqueos` y `checkouts` como tablas PostgreSQL en MELT-Reservas. No se importaron datos de la antigua base Mongo de Reservas; no hay migración automática de esos documentos. Pagos sigue registrando simulaciones en MongoDB `pagos_db`; los datos históricos de pagos externos no se transforman.

## Consecuencias

- La compra de varios horarios es una sola operación visible, con importe total autoritativo e idempotente.
- Pagos requiere acceso a MongoDB y su propio nombre de base (`PAGOS_MONGODB_DATABASE`); no depende de credenciales externas.
- En modo `test`, se usan usuarios y medios de pago de prueba de Mercado Pago; el servicio sólo redirige al enlace sandbox y rechaza respuestas de pago `live_mode=true`.
- La integración anterior descrita en [ADR-009](ADR-009-compatibilidad-carrito-sena.md) se conserva como historia de reglas y retenciones. Este ADR reemplaza únicamente la modalidad de pago por turno y define el checkout agrupado.
