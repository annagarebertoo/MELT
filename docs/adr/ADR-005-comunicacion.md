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
- **Mensajes duplicados o reordenados:** el consumidor (Beneficios, correo) debe tratar eventos de forma idempotente (no acreditar dos veces el mismo tratamiento completado, no restituir dos veces).
- **Consistencia eventual:** entre el registro del turno y la acreditación/restitución puede haber un intervalo en el que el estado visible no refleja aún todos los efectos.
- **Coordinación Reservas–Beneficios** en la reserva con beneficio: dos servicios, dos bases; hay que evitar consumir puntos sin resolver el turno (y viceversa). No se adopta aún una saga completa como decisión cerrada.

### Aspectos pendientes (a completar en el diseño e implementación)

- Tiempos de espera, políticas de reintento y tratamiento de errores en llamadas HTTP.
- Esquemas de eventos, nombres de colas/exchanges y estrategia de dead-letter.
- Idempotencia de productores y consumidores.
- Evaluación de **Transactional Outbox** como patrón candidato para no perder la publicación de eventos tras un commit.
- Estrategia de recuperación y posibles compensaciones en el flujo de reserva con beneficio.
- Instrumentación (trazas, métricas) para seguir una operación a través de servicios y colas.
