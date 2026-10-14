# ADR-005 — Comunicación entre servicios (versión inicial)

**Estado:** Propuesto (versión inicial)  
**Fecha:** 2026-10-08  
**Entrega:** 1 — 9 de octubre de 2026  
**Decisores:** Equipo MELT

## Contexto y problema

Los tres microservicios (Catálogo y Profesionales, Reservas, Beneficios) no comparten bases de datos. Las operaciones de negocio requieren:

- Consultas y validaciones que necesitan respuesta inmediata (aptitud del profesional, datos del tratamiento, disponibilidad, validación de un beneficio al reservar).
- Efectos posteriores que no deben bloquear la operación principal (acreditación de puntos tras completar un tratamiento, restitución por cancelación, envío de correo de confirmación, actualización del índice de búsqueda).

Hay que definir modalidades de comunicación, cuándo usar cada una y qué problemas (fallas, duplicados, respuestas perdidas) se introducen y deberán mitigarse.

## Alternativas consideradas

1. **Solo HTTP/JSON síncrono**  
   Simple de razonar. Acopla la latencia y la disponibilidad de todos los participantes: si Beneficios o el proveedor de correo fallan, la reserva o la finalización pueden quedar bloqueadas o en estado inconsistente.

2. **Solo mensajería asíncrona**  
   Desacopla bien los efectos posteriores, pero no es adecuada para validaciones que el usuario espera en la misma solicitud (¿puedo reservar este horario? ¿el profesional atiende este tratamiento?).

3. **HTTP/JSON para consultas y validaciones inmediatas + RabbitMQ para eventos de dominio** (opción elegida)  
   Combina respuesta inmediata donde hace falta con desacoplamiento de efectos posteriores.

## Decisión (propuesta inicial)

### Modalidades

| Interacción | Modalidad | Propósito |
| :--- | :--- | :--- |
| Frontend → gateway → servicio | HTTP/JSON | Consultas y operaciones según el rol. |
| Reservas → Catálogo | Síncrona (HTTP/JSON) | Verificar tratamiento, aptitud del profesional y datos necesarios para reservar. |
| Reservas → Beneficios | Síncrona (HTTP/JSON), sujeta al diseño del flujo | Validar y coordinar la aplicación de un beneficio durante la reserva. |
| Reservas → RabbitMQ → Beneficios | Asíncrona | Acreditación por tratamiento completado y restitución por cancelación válida. |
| Reservas → RabbitMQ → procesamiento de correo | Asíncrona | Enviar la confirmación sin hacer depender el turno del proveedor de correo. |
| Catálogo → procesamiento de búsqueda | Asíncrona (candidata vía RabbitMQ) | Reflejar cambios del catálogo en el índice. |

- El **API Gateway (NGINX)** enruta las solicitudes y aplica políticas de entrada; no concentra reglas de negocio de reservas ni de saldo.
- Se proponen eventos de dominio tales como: turno confirmado, tratamiento completado, turno cancelado. Sus esquemas, reintentos y recuperación están pendientes.

### Principios iniciales

- **Síncrono** cuando el resultado es parte de la respuesta al usuario o de una validación que debe completarse antes de confirmar el hecho de negocio.
- **Asíncrono** cuando el efecto es posterior y puede tolerar un retraso (puntos, correo, indexación), de modo que una falla del consumidor no anule el hecho ya registrado.

## Justificación

- La reserva necesita datos de Catálogo y, opcionalmente, validación de Beneficios **antes** de confirmar. Esa necesidad es síncrona.
- La acreditación de puntos y el correo no deben impedir que el turno quede confirmado o completado. La mensajería permite procesarlos de forma independiente y recuperable.
- RabbitMQ es la tecnología propuesta para publicación/consumo de eventos; permite colas, reintentos y separación de productores y consumidores.

## Consecuencias

### Positivas

- El usuario obtiene respuesta inmediata en las operaciones que la requieren.
- Los efectos posteriores no bloquean el flujo principal ni dependen de la disponibilidad del correo o de Beneficios en el mismo instante.
- Se alinean las comunicaciones con las responsabilidades de cada servicio (ADR-001).

### Negativas / problemas que introduce

- **Fallas de comunicación síncrona:** timeouts, respuestas ausentes o tardías. Una respuesta perdida no prueba que el efecto no ocurrió; la repetición debe preservar una única operación de negocio (idempotencia).
- **Mensajes duplicados o reordenados:** el consumidor de Beneficios identifica cada evento y registra cada acreditación con una clave única y una operación transaccional; un mismo tratamiento completado no puede acreditar puntos dos veces. Los efectos de pago/devolución también deben usar una clave idempotente. Para correo se registra el estado del evento y se limita el reintento; no se promete ausencia absoluta de duplicados si el proveedor acepta el envío y se pierde su respuesta.
- **Consistencia eventual:** entre el registro del turno y la acreditación/restitución puede haber un intervalo en el que el estado visible no refleja aún todos los efectos.
- **Coordinación Reservas–Beneficios** en la reserva con beneficio: dos servicios, dos bases; hay que evitar consumir puntos sin resolver el turno (y viceversa). No se adopta aún una saga completa como decisión cerrada.

### Aspectos pendientes (a completar en el diseño e implementación)

- Tiempos de espera, políticas de reintento y tratamiento de errores en llamadas HTTP.
- Esquemas definitivos de eventos, nombres de colas/exchanges y configuración operativa de dead-letter.
- Idempotencia de productores y consumidores, con claves únicas para acreditaciones y operaciones de pago/devolución.
- Evaluación de **Transactional Outbox** como patrón candidato para no perder la publicación de eventos tras un commit.
- Estrategia de recuperación y posibles compensaciones en el flujo de reserva con beneficio.
- Instrumentación (trazas, métricas) para seguir una operación a través de servicios y colas.


## Enmienda de decisión — eventos y efectos posteriores

**Estado de esta enmienda:** Aceptada por el equipo para el diseño actualizado.

- Los cambios de estado de un turno realizados por el profesional (confirmación y cancelación) publican eventos que originan el envío de correo al cliente.
- La finalización registrada por el profesional publica el evento que habilita la acreditación de puntos. Los puntos no se acreditan por reservar ni por pagar una seña.
- Los efectos de correo y puntos son asíncronos y recuperables. Para correos se aplica un intento inicial y hasta 3 reintentos automáticos con esperas crecientes; si persiste el fallo, el evento pasa a una cola de errores para revisión y eventual reenvío manual. Se registra el estado de procesamiento. La falla del proveedor de correo no revierte la operación de agenda. Para puntos, el identificador único del tratamiento/evento y una restricción de unicidad en la base de datos impiden que reintentar la misma acreditación duplique el saldo.
- El bloqueo de horario por carrito durante 10 minutos es una operación síncrona de control de concurrencia en Reservas; su expiración libera el intervalo automáticamente. No debe modelarse como una confirmación definitiva del turno.
- El resultado de pago de la seña debe verificarse antes de confirmar la reserva. Las notificaciones de pago y devolución deben correlacionarse con la operación original y no duplicar efectos. La seña equivale al 30 % del precio del tratamiento; se devuelve el 100 % si se cancela con al menos 24 horas de anticipación y no se devuelve por cancelación más tardía o inasistencia. El proveedor/modalidad y el procedimiento operativo de devolución quedan pendientes.
