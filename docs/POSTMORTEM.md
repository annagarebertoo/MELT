# Postmortem — jornada de fallas controladas

> **Alcance:** dos caídas provocadas el 24/11/2026 en un entorno local aislado (Docker, bases descartables, sin Atlas, Supabase ni Render), sobre la rama `claude/project-thread-dirwx9` derivada de `codex/supabase-postgres-migration`. Al final se conserva sin cambios el ensayo parcial del 23/10/2026, que motivó las acciones de este informe. Horarios en UTC (Argentina: UTC−3).

## Resumen

| Ensayo | Dependencia que cae | Impacto para el usuario | Pérdida o duplicación | Acción derivada |
|---|---|---|---|---|
| A | Una de dos réplicas de Catálogo | Ninguna respuesta con error; una solicitud tardó 0,5 s (antes 2 s) | No aplica (lecturas) | `proxy_connect_timeout 500ms` en las rutas de Catálogo |
| B | RabbitMQ (60 s y 150 s) | Ninguno en línea: completar turnos siguió respondiendo 202. Los puntos llegaron con demora | 0 eventos perdidos, 0 acreditaciones duplicadas | Retroceso exponencial y drenado inmediato en el publicador del outbox; métricas y alertas del flujo |
| C | GhostKitchen (503 simulado en la prueba automatizada) | Beneficios devuelve 503; la interfaz muestra que no pudo cargar la búsqueda y permite reintentar | El test confirma que el saldo y los movimientos de puntos no cambian; el endpoint es de consulta y no llama a Reservas | Respetar `Retry-After`, limitar a 3 intentos, mostrar la falla y no usar datos anteriores como vigentes |

## Ensayo A — caída de una réplica de Catálogo

**Escenario.** Gateway NGINX con dos réplicas de Catálogo (`compose.s1.yaml`), balanceo round-robin, detección pasiva (`max_fails=1 fail_timeout=10s`) y reintento de lecturas en la otra réplica (`proxy_next_upstream error timeout http_502 http_503 http_504`, 2 intentos). Herramientas: `scripts/verify-s1-failover.py` (30 solicitudes por fase) y `scripts/benchmark-s3-resilience.py` (200 solicitudes por fase, concurrencia 4).

### Línea temporal (primera corrida, timeout de conexión 2 s)

| Hora UTC | Hecho |
|---|---|
| 08:27:44 | Línea base: 30/30 HTTP 200, 15 por réplica. |
| 08:27:48 | Se detiene `catalogo-profesionales-1`. |
| 08:27:50 | El Gateway registra `upstream timed out while connecting` contra la réplica detenida, la marca como caída y reintenta en la réplica 2. Esa solicitud responde **200 en 2.002 ms**. |
| 08:27:50–52 | Las 29 solicitudes siguientes van sólo a la réplica 2, en 1–2 ms. |
| 08:27:52 | Se reinicia la réplica 1; healthy a los 5,3 s. |
| 08:28:02 | Vuelve a recibir tráfico 9,9 s después del reinicio (vence `fail_timeout`). Recuperación: 18/12. |

[Evidencia](evidence/2026-11-24/s1-failover-connect-timeout-2s.json).

### Impacto

Ninguna solicitud falló (90/90 en S1 y 600/600 en S3 respondieron 200). El costo de la caída fue **una** solicitud lenta por cada vez que el Gateway descubre que la réplica no está: con el valor anterior, 2 s; en S3 se ve como el p99 de la fase de fallo (590 ms con el ajuste ya aplicado, frente a 148 ms en normal). El p95 no cambió (123 → 121 ms).

### Detección

- Log de error del Gateway con el upstream y el `request_id`; el log de acceso JSON muestra `upstream` con los dos intentos.
- En el stack completo, Prometheus descubre cada réplica por DNS, `up{job="catalogo-profesionales"}` baja a 0 y la regla `CatalogReplicaUnavailable` dispara si dura más de 1 minuto; el tablero **MELT · Observabilidad S2** muestra el RPS por réplica. El ensayo A duró menos de ese minuto y corrió sobre `compose.s1.yaml`, así que la detección registrada es la de los logs.

### Causa

Con la réplica detenida, Docker sigue resolviendo su IP durante unos segundos y la conexión TCP no se rechaza: queda sin respuesta hasta que vence `proxy_connect_timeout`. Ese timeout era de 2 s, demasiado para un servicio que en condiciones normales responde en 1–2 ms dentro de la misma red.

### Respuesta y acción

Se bajó `proxy_connect_timeout` a **500 ms** sólo en las rutas de Catálogo (`infra/nginx/default.conf`); el timeout de lectura no cambió. Se repitió el ensayo a las 08:29:02: la solicitud que detecta la caída respondió **200 en 502 ms** y las demás siguieron en 1–2 ms ([evidencia](evidence/2026-11-24/s1-failover-connect-timeout-500ms.json)). 500 ms deja margen amplio sobre la latencia de conexión observada y evita falsos positivos por pausas breves.

### Lectura honesta de los verificadores

Ambos scripts terminan en `overall_passed: false` por dos comprobaciones estrictas que no indican falla del usuario:

- `no_relevant_errors`: el log de error del Gateway contiene la línea `upstream timed out while connecting`, que es justamente la detección pasiva esperada (el script lee el log completo del contenedor, por eso acumula las líneas de corridas anteriores).
- `recovery_distribution_correct`: espera 15/15 en la recuperación, pero la réplica restaurada recién vuelve a recibir tráfico cuando vence `fail_timeout=10s`, así que la reparte 18/12 o 19/11.

No se cambió el criterio de los scripts para volverlos verdes.

## Ensayo B — caída de RabbitMQ

**Escenario.** Reservas, Beneficios, PostgreSQL, RabbitMQ, Jaeger y Prometheus locales. Un profesional completa turnos mientras el broker está detenido; cada turno completado debe generar puntos en Beneficios a través del outbox.

### Línea temporal (primera corrida)

| Hora UTC | Hecho |
|---|---|
| 08:47:37 | Se detiene RabbitMQ. |
| 08:47:39–40 | Se completan 3 turnos: **202** en 2–12 ms. Los 3 eventos quedan en `outbox_eventos` sin publicar. |
| 08:47:40 → | El publicador reintenta cada 1,2 s; log JSON `reservas_outbox_publish_failed` con el error de conexión. |
| 08:48:01 | `OutboxPublishFailing` pasa a *pending* (≥3 fallos en 5 min). |
| 08:50:04 | `OutboxPublishFailing` en *firing* (sostenida 2 min). `melt_reservas_outbox_publicaciones_total{resultado="fallido"}` = 121. |
| 08:50:11 | Se inicia RabbitMQ. |
| 08:50:18–21 | Se publican los 3 eventos (uno por ciclo de 1,2 s). |
| 08:50:22 | Beneficios acredita 100 puntos por evento: saldo 300, **3 movimientos, ninguno repetido**. |

[Evidencia](evidence/2026-11-24/caida-rabbitmq-timeline.txt). En Jaeger, la traza de un turno completado durante la caída une el `POST /turnos/:id/completar`, 133 intentos de `rabbitmq publish` y el `rabbitmq consume` de Beneficios 159 s después, con el mismo `trace_id`.

### Impacto

- Para el usuario, ninguno visible: la operación de negocio (completar el turno) no depende del broker.
- Los puntos llegaron 2 a 3 minutos tarde, el tiempo de la caída más la espera del ciclo de publicación.
- Sin pérdida (los eventos estaban en PostgreSQL) y sin duplicados (deduplicación por `eventId`).

### Causa y problemas encontrados

1. **Reintento a ritmo fijo.** El publicador reintentaba cada 1,2 s sin importar el error: 133 intentos para un solo evento y un log por intento.
2. **Drenado lento.** Al volver el broker publicaba un evento por ciclo: con N eventos pendientes, N × 1,2 s.
3. **Alerta sin primer evento.** La alerta de DLQ no disparaba ante el primer mensaje porque la serie nacía en 1 e `increase()` la ignoraba.

### Respuesta y acciones

- Retroceso exponencial en el publicador (1,2 s, duplicando hasta 10 s) y drenado sin espera mientras haya eventos (`events.go`, probado en `TestProximaEsperaOutboxDrenaRetrocedeYSeAcota`).
- Contadores inicializados en 0 para que `increase()` vea el primer evento.
- **Repetición con el cambio (08:51:42):** 60 s de caída y 10 turnos completados (todos 202). Al iniciar RabbitMQ a las 08:52:43, los **10/10 eventos se acreditaron en 8,5 s**, con **18 intentos en total** (antes, 133 para uno solo), 10 movimientos distintos y saldo 1.000.

### Detección disponible

- Alertas `OutboxPublishFailing` y `BeneficiosEventsDeadLettered` ([estado firing](evidence/2026-11-24/alerta-dlq-disparada.json)).
- Panel «Outbox» del tablero **MELT · Flujo principal**: publicaciones por resultado y p95 de demora.
- Logs JSON con `service`, `instance`, `trace_id` y `request_id` en todos los servicios.

## Comportamiento ante la caída de cada dependencia

| Servicio | Dependencia | Qué pasa si falla o se pone lenta | Protección |
|---|---|---|---|
| Gateway | Réplica de Catálogo | El Gateway la aparta 10 s y reintenta lecturas en la otra | Detección pasiva, `proxy_next_upstream`, connect timeout 500 ms (ensayo A) |
| Catálogo | Memcached | Lee de MongoDB; más latencia, mismo resultado | Caché opcional (`TestL3FallbackSinMemcachedYRecuperacion`) |
| Catálogo | OpenSearch | La búsqueda cae a MongoDB y no cachea el resultado degradado | Timeout y fallback (`TestL3FallbackSinOpenSearchUsaMongoYNoCacheaResultadosDegradados`) |
| Reservas | RabbitMQ | Completar/cancelar sigue respondiendo; eventos esperan en el outbox | Outbox + retroceso (ensayo B) |
| Reservas | Pagos / Mercado Pago | El checkout informa que el pago no está disponible; no confirma turnos | Timeout HTTP, idempotencia, sin confirmar sin pago aprobado |
| Reservas | Catálogo / Auth | La operación falla con error claro; no se escriben datos a medias | Timeouts HTTP, transacción |
| Beneficios | PostgreSQL | El mensaje no se confirma y se reintenta; tras agotar intentos va a la DLQ | Reintentos acotados, DLQ, `mensajes_fallidos` |
| Beneficios | Proveedor externo (GhostKitchen) | El endpoint devuelve el 503 del proveedor; el cliente ve un aviso de indisponibilidad, se limpian los resultados anteriores y puede volver a intentar. No se altera el saldo ni se crea una reserva | Timeout de 3 s por llamada; hasta 3 intentos para 429/503 según `Retry-After`; [prueba de caída](../services/beneficios/cmd/api/ghostkitchen_test.go) |

## Ensayo C — indisponibilidad de GhostKitchen

**Escenario reproducible.** `TestGhostKitchen503NoModificaSaldoNiMovimientos` usa un servidor HTTP de prueba que responde 503 con `Retry-After: 0` en cada intento. Beneficios consulta `GET /ghostkitchen/sedes`, realiza los tres intentos configurados y devuelve 503 al cliente. La prueba prepara 25 puntos, consulta saldo y movimientos antes y después, y confirma que ambos permanecen iguales.

**Impacto esperado.** La sección GhostKitchen de la interfaz informa el error recibido, elimina las sedes/platos mostrados previamente para no presentarlos como actuales y permite repetir la búsqueda. El flujo no crea reservas, pedidos ni operaciones de puntos. La prueba automatizada comprueba el estado de puntos; no ejecuta una compra ni una reserva, porque GhostKitchen no ofrece esas operaciones.

**Contrato.** Los endpoints y errores se contrastan además con Prism iniciado desde la especificación oficial publicada por el Grupo 3, versión 1.1.0 ([copia OpenAPI](contracts/ghostkitchen-v1.1.0.openapi.yaml), [workflow de contrato](../.github/workflows/ghostkitchen-contract.yml)). Este mock se deriva del contrato; no es una caída del servidor real.

**Límite de la evidencia.** Es una simulación controlada y automatizada, no se detuvo ni se sometió a carga el proveedor en la nube. Tampoco se probó la interfaz en un navegador durante la caída. No se hicieron llamadas de falla a Render ni a datos compartidos.

## Pendiente

- Para la jornada de defensa, repetir la simulación y mostrar el aviso visible en la interfaz junto con los logs correlacionados. No provocar una caída real en producción.
- Las caídas se ensayaron en local. No se provocaron fallas en Render ni en bases compartidas.

## Historial

### Ensayo autorizado — 23/10/2026

**Base:** `cc230fa`, rama `feature/sofia-adr-entrega-2`. **Evidencia:** [fallo-replica-2026-10-23.json](evidence/fallo-replica-2026-10-23.json). Verificador: `scripts/verify-replica-failure.py`.

El usuario autorizó detener únicamente `melt-catalogo-profesionales-2`, consultar el listado público y restaurar ese mismo contenedor incluso si fallaba una comprobación. No se modificó configuración ni se detuvieron MongoDB, gateway, Jaeger o todas las réplicas.

| Momento UTC (Argentina: UTC−3) | Hecho registrado |
| :--- | :--- |
| 17:45:09.574 | Inicio del ensayo y referencia de estado previo. |
| 17:45:09.971 | Se confirmó detenida la réplica 2. Se ejecutaron doce GET a `/api/v1/tratamientos`. |
| Durante esa fase | Once respuestas 200 y una 504; máximo observado 2.004,042 ms. Los handlers que respondieron pertenecían solo a la réplica 1. |
| 17:45:12.570 | Restauración solicitada desde `finally`; después se comprobó salud de la réplica 2 y se esperaron seis segundos antes de consultar recuperación. No se midió el instante exacto de recuperación. |
| Fase posterior | Doce respuestas 200; máximo observado 26,478 ms; se observaron las dos instancias. |

**Impacto:** una de doce solicitudes de la fase interrumpida no obtuvo el catálogo aunque había una réplica saludable. La solicitud `sofia-fallo-1b98f1fbf44f46828b9e7445e7e56064` devolvió 504 y su log registró únicamente el destino `172.19.0.4:8081`. No llegó a registrarse un handler de Catálogo para esa solicitud. No se afirma que se reintentara contra la otra instancia.

**Detección y respuesta:** las respuestas HTTP y los logs correlacionados hicieron visible el fallo. La restauración automática recuperó el mismo contenedor. Las instantáneas de documentos e índices de MongoDB coincidieron antes/después; los identificadores y horas de inicio de gateway, MongoDB y Jaeger no cambiaron.

**Causa comprobada y límite del análisis:** hubo un intento registrado contra la réplica detenida y la respuesta 504 tardó aproximadamente el timeout de conexión configurado. El mecanismo exacto por el cual esa solicitud no pasó a otro destino no quedó demostrado; la interacción entre resolución DNS, destinos disponibles y comportamiento de reintento es una hipótesis a investigar, no una causa raíz confirmada. No se midió un período exacto de exclusión pasiva.

**Acciones pendientes:** acordar política explícita de intentos y exclusión/recuperación de destinos, revisar el camino de errores del proxy y repetir el ensayo con autorización después de cualquier cambio. No se cambió infraestructura en este cierre. La comprobación estricta de cero errores del verificador devolvió código 1 y se conserva como resultado no aprobado; no se transformó en éxito por haber restaurado la réplica.

**Validado:** interrupción real de una réplica, continuidad parcial, error acotado observado, restauración y reincorporación de ambas instancias. **No validado:** failover sin errores, caída total, caída de MongoDB o del proveedor, agotamiento del timeout de lectura, carga, duplicación o recuperación transaccional. Las filas originales que corresponden a estos escenarios continúan pendientes.
