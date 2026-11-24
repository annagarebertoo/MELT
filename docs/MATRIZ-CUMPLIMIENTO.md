# Matriz de cumplimiento contra el enunciado

**Revisión del 24/11/2026**, comparando literalmente el *Enunciado — Práctico Integrador 2026* con la rama `codex/supabase-postgres-migration` más los cambios de esta revisión. Estados: ✅ cumplido con evidencia, 🟡 en curso, ⏳ depende de la parte A1–A3.

## Requisitos de arquitectura y desarrollo (sección 2)

| § | Requisito | Estado | Implementación | Evidencia |
|---|---|---|---|---|
| 2.1 | ≥ 3 microservicios y API Gateway | ✅ | Auth, Catálogo, Reservas, Pagos, Beneficios + NGINX | [ARCHITECTURE](ARCHITECTURE.md) |
| 2.1 | Dos patrones de arquitectura interna distintos | ✅ | Capas (Catálogo) y hexagonal (Reservas, Beneficios) | [ADR-002](adr/ADR-002-arquitectura-interna.md) |
| 2.2 | Frontend que centraliza todas las funcionalidades | ✅ | `infra/nginx/web/` servido por el Gateway | [RESERVAS-WEB](RESERVAS-WEB.md), manuales |
| 2.3 | Balanceo sobre ≥ 2 instancias, reconoce instancia caída, distribución observable | ✅ | Upstream de Catálogo, `max_fails`/`fail_timeout`, `proxy_next_upstream`, connect timeout 500 ms; métricas por réplica | [ADR-012](adr/ADR-012-balanceo-carga.md), [POSTMORTEM](POSTMORTEM.md) ensayo A |
| 2.5 | Síncrona con timeout, errores, respuestas tardías y reintentos | ✅ | Clientes HTTP con timeout, errores tipados, reintento sólo en lecturas e idempotentes | [ADR-005](adr/ADR-005-comunicacion.md), [COMMUNICATION](COMMUNICATION.md) |
| 2.5 | Asíncrona: evento de dominio publicado y consumido, mensajes fallidos | ✅ | Outbox → RabbitMQ `tratamiento.realizado` / `turno.cancelado` → Beneficios, reintentos y DLQ | Pruebas L2, [ADR-004](adr/ADR-004-consistencia-concurrencia.md) |
| 2.6 | Búsqueda con motor, paginación, filtros, orden, índice sincronizado, retraso máximo definido | ✅ | OpenSearch; reconstrucción por generaciones; 503 si el índice supera 60 s | [ADR-006](adr/ADR-006-busqueda.md), pruebas L3 |
| 2.7 | Caché en lectura relevante con métricas de impacto | ✅ | Memcached cache-aside en el listado; hits/misses en `/metrics` | [ADR-007](adr/ADR-007-cache.md), [ADR-013](adr/ADR-013-capacidad-costos.md) (×2,4 RPS) |
| 2.8 | Relacional y no relacional | ✅ | PostgreSQL (Reservas, Beneficios) y MongoDB (Auth, Catálogo, Pagos) | [ADR-003](adr/ADR-003-persistencia.md) |
| 2.8 | Operación que no puede duplicarse ni perderse | ✅ | Reserva y checkout; acreditación y canje de puntos | [ADR-004](adr/ADR-004-consistencia-concurrencia.md), pruebas L1/L2 |
| 2.9 | Dependencias identificadas, ≥ 1 mecanismo de protección, caída provocada en POSTMORTEM | ✅ | Failover de réplica, outbox con retroceso, DLQ, fallbacks de caché y búsqueda | [POSTMORTEM](POSTMORTEM.md) (tabla por dependencia), [ADR-010](adr/ADR-010-resiliencia.md) |
| 2.10 | Logs estructurados, métricas y tablero | ✅ | JSON con `request_id`/`trace_id` en todos los servicios; Prometheus; dos tableros Grafana; alertas | [OBSERVABILITY](OBSERVABILITY.md), [ADR-011](adr/ADR-011-observabilidad.md) |
| 2.11 | Unitarios sobre reglas de negocio con porcentaje de calidad | ✅ | Cobertura por servicio, mínimo 60 % exigido en CI | Workflow **Cobertura**: Auth 69 %, Beneficios 67 %, Catálogo 73 %, Pagos 66 %, Reservas 64 % |
| 2.11 | Integración por servicio contra dependencia real | ✅ | PostgreSQL, RabbitMQ, MongoDB, Memcached y OpenSearch descartables (`compose.test.yaml`) | Pruebas L1–L3, `scripts/cobertura.sh` |
| 2.11 | Carga con resultados documentados y analizados | ✅ | `scripts/carga-capacidad.py`, `benchmark-s3-resilience.py` | [ADR-013](adr/ADR-013-capacidad-costos.md), `docs/evidence/2026-11-24/` |
| 2.12 | Repositorio público, versión final en la rama principal | ⏳ | Se integra a `main` cuando cierre A1–A3 (decisión del equipo) | — |
| 2.12 | Estrategia de ramas, revisiones e integración | ✅ | Ramas por tarea, PR con CI | Historial de PR |
| 2.12 | Puesta en marcha local única y automatizada, sin configuraciones manuales | ⏳ | Parte A3 | [LOCAL-DEV](LOCAL-DEV.md) |
| 2.12 | Secretos fuera del repositorio | ✅ | `.env` ignorado, `.env.example`; secretos en Render | `.gitignore` |

## Documentación (sección 3)

| Archivo | Estado | Nota |
|---|---|---|
| README.md | ✅ | Revisar el enlace al despliegue al cerrar A2 |
| SPEC.md | ✅ | |
| docs/ARCHITECTURE.md | ✅ | Incluye limitaciones conocidas y deuda técnica |
| docs/adr/ADR-001 … ADR-015 | ✅ | D1–D13 cubiertas; ver tabla siguiente |
| docs/contracts/README.md | ⏳ | Contratos presentes; la versión final de la capacidad publicada es parte A2 |
| docs/POSTMORTEM.md | ✅ | Dos caídas provocadas con impacto, línea temporal, detección, respuesta, causa y acciones |

## Decisiones obligatorias (sección 5)

| D | Decisión | ADR | Estado |
|---|---|---|---|
| D1 | Límites de los servicios | [001](adr/ADR-001-limites-servicios.md) | ✅ |
| D2 | Arquitectura interna | [002](adr/ADR-002-arquitectura-interna.md) | ✅ |
| D3 | Persistencia | [003](adr/ADR-003-persistencia.md) | ✅ |
| D4 | Consistencia y concurrencia | [004](adr/ADR-004-consistencia-concurrencia.md) | ✅ |
| D5 | Comunicación | [005](adr/ADR-005-comunicacion.md) | ✅ |
| D6 | Búsqueda | [006](adr/ADR-006-busqueda.md) | ✅ |
| D7 | Caché | [007](adr/ADR-007-cache.md) | ✅ |
| D8 | Contrato propio | [008](adr/ADR-008-contrato-propio.md) | ⏳ A2 |
| D9 | Consumo del proveedor | [014](adr/ADR-014-consumo-proveedor.md) | ⏳ A1 |
| D10 | Resiliencia | [010](adr/ADR-010-resiliencia.md) | ✅ |
| D11 | Observabilidad, objetivo de servicio y alertas | [011](adr/ADR-011-observabilidad.md) | ✅ |
| D12 | Balanceo de carga | [012](adr/ADR-012-balanceo-carga.md) | ✅ |
| D13 | Capacidad y costos | [013](adr/ADR-013-capacidad-costos.md) | ✅ |

## Integración entre grupos (sección 4) — parte A1–A3

| Requisito | Estado | Observación |
|---|---|---|
| Capacidad propia publicada, contrato formal versionado, operativa en la nube | ⏳ A2 | Endpoints de acreditación de puntos para el grupo consumidor |
| Consumo de un proveedor incorporado a un flujo importante, con consecuencias de negocio | ⏳ A1 | Hoy GhostKitchen es sólo consulta; el enunciado no acepta una integración «sin consecuencias sobre el negocio» |
| Llamada desde el microservicio responsable, no desde el frontend ni el Gateway | ⏳ A1 | |
| Test de contrato sobre la capacidad consumida | ⏳ A1 | |
| Timeouts, errores y comportamiento visible ante caída del proveedor, demostrado en la jornada de fallas | ⏳ A1 | Agregar al POSTMORTEM cuando esté |

## Defensa individual (sección 6)

| Ítem | Dónde mostrarlo |
|---|---|
| Operación crítica con garantías de consistencia y concurrencia | ADR-004 y `TestL1*`, `TestProgramaCanjesConcurrentesNoSobregiranElSaldo` |
| Unitarios, integración contra dependencia real | Workflow **Cobertura** y **Pruebas L1–L3** |
| Test de contrato sobre la capacidad consumida | ⏳ A1 |
| Balanceo operativo con distribución observable | `compose.s1.yaml`, `scripts/verify-s1-failover.py`, tablero S2 |
| Caída de una dependencia propia y del proveedor | Ensayos A y B del POSTMORTEM; proveedor ⏳ A1 |
| Carga con análisis y límite principal | ADR-013 |
| Logs, métricas, trazas, tablero y alerta | Jaeger, **MELT · Flujo principal**, `OutboxPublishFailing` |

## Historial: revisión documental anterior

**Alcance:** revisión documental del árbol `develop` en `ea1ead7e85f533ba54aff85bd95958bb749897a0`; no se ejecutaron pruebas para esta actualización. La fuente oficial en el repositorio es `referencias/Enunciado TP Final (2).pdf`, pero no se pudo extraer su texto en esta sesión sin instalar dependencias (prohibido por la solicitud). Por ello esta tabla relaciona requisitos técnicos registrados en `SPEC.md`, ADR y evidencia disponible; no equivale a una auditoría literal de todas las páginas del PDF.

| Requisito/capacidad identificada | Evidencia en código/configuración | Documento/evidencia | Estado documental |
|---|---|---|---|
| API Gateway y frontend integrado | `infra/nginx/default.conf`, `infra/nginx/web/`, `compose.yaml` | [Arquitectura](ARCHITECTURE.md), [OPERACIONES](OPERACIONES.md) | Implementado localmente; remoto no verificado aquí. |
| Persistencia por servicio | Auth, Catálogo/Profesionales y Pagos usan MongoDB; Reservas y Beneficios usan PostgreSQL privado en proyectos Supabase separados (`*_DATABASE_URL`) | [Arquitectura](ARCHITECTURE.md), [ADR-003](adr/ADR-003-persistencia.md), [Supabase](../infra/supabase/README.md) | Arquitectura y conexión verificadas localmente en la rama actual; el registro histórico de `PERSISTENCE.md` corresponde a otra versión. |
| Roles y controles de Auth | `services/auth/cmd/api/main.go`; middlewares de Reservas, Catálogo, Beneficios | Manual [Auth](servicios/AUTH.md), [Reservas](servicios/RESERVAS.md) | Rutas y controles presentes; no se repitieron pruebas en esta tarea. |
| CRUD de catálogo, perfiles y asociaciones | `services/catalogo-profesionales/cmd/api/`; transacciones administrativas | [Catálogo](servicios/CATALOGO-PROFESIONALES.md), contrato admin | Implementado según código revisado. |
| Búsqueda OpenSearch y caché Memcached | `search_cache.go`, configuración Compose, pruebas con build tags | [Etapa Catálogo](CATALOGO-PROFESIONALES-ETAPA-1.md), ADR-006/007 | Implementado; resultados de benchmarks son muestras locales históricas. |
| Disponibilidad, agenda, turnos y concurrencia | `services/reservas/cmd/api/`; índices únicos/transacciones | [Reservas](servicios/RESERVAS.md), [web](RESERVAS-WEB.md) | Código presente. Reglas validan conflictos por profesional/minuto y por cliente/categoría/minuto. |
| Retenciones y checkout agrupado, seña del 30 % | `checkout.go`, rutas de carrito, transacción de confirmación | [Reservas](RESERVAS-WEB.md), ADR-015, OpenAPI checkout | Implementado; sólo confirma tras verificar aprobación. |
| Checkout Pro de Mercado Pago en modo de prueba | Preferencias, retorno, consulta server-side y Webhook HMAC en `services/pagos` | [Pagos](servicios/PAGOS.md), ADR-015 | Implementado; requiere credenciales de prueba y secreto Webhook configurados en Render. |
| Puntos por tratamiento realizado / idempotencia | outbox Reservas, RabbitMQ, consumidor Beneficios, deduplicación | [Beneficios](servicios/BENEFICIOS.md), [flujo](FLUJO-BENEFICIOS.md) | Implementado por código; extremo a extremo en producción no verificado. |
| Canje de recompensas | rutas del programa en Beneficios y frontend | [Beneficios](servicios/BENEFICIOS.md), README | Implementado en código. |
| Acreditación externa para Gimnasio | `/api/v1/beneficios/acreditar-puntos` y `/acreditaciones`, partner key, invitaciones | [contrato](contracts/acreditar-puntos-v1.openapi.yaml), [integración](INTEGRACION-GIMNASIO.md) | Implementada; verificar disponibilidad/URL remota al momento de consumir. |
| Consumo externo GhostKitchen | cliente HTTP y rutas proxy en Beneficios; tests `httptest` | [GhostKitchen](GHOSTKITCHEN.md), [ADR-014](adr/ADR-014-consumo-proveedor.md) | Adaptador implementado; operación real requiere clave/proveedor. La disponibilidad remota no se comprobó. |
| Mensajería asíncrona | RabbitMQ, outbox y consumidor con retry/fallidos | [Comunicación](COMMUNICATION.md), ADR-005, evidencia D5 | Implementación presente; evidencia D5 señala límites de inspección de broker. |
| Resiliencia/failover | `compose.yaml`, upstream y timeouts Gateway | [Postmortem](POSTMORTEM.md), evidencia de fallo de réplica | Parcial: ensayo observó 1 HTTP 504 de 12 con una réplica caída; failover sin errores no demostrado. |
| Observabilidad | logs/traces de Gateway/Catálogo, Prometheus scrape config | [Observabilidad](OBSERVABILITY.md), ADR-011 | Instrumentación parcial. No declarar tablero completo ni métricas de negocio sin evidencia. |
| Manuales/contratos exhaustivos | contratos OpenAPI existentes y rutas en código | [Contratos](contracts/README.md), manuales por servicio | Parcial: hay capacidades sin esquema OpenAPI completo; esta documentación registra las rutas revisadas. |
| Requisitos oficiales exactos del TP | PDF de referencia no extraído en esta sesión | `referencias/Enunciado TP Final (2).pdf` | Pendiente de cotejo literal por la limitación del lector PDF. |

### Verificaciones anteriores citadas

Los documentos de Catálogo, D3/D5/D8/D11 y `docs/evidence/*.json` conservan pruebas de fechas/commits anteriores. No se reejecutaron aquí. Para presentar cumplimiento, adjuntar evidencia de la versión entregada, indicar su commit y no reutilizar las marcas históricas como resultado de la revisión documental actual.

### Actualización de evidencia — 20/11/2026

Esta matriz conserva el alcance y las limitaciones de su revisión documental original. Para el estado más reciente de GitHub Actions, despliegues de Render, el botón «No asistió» y las comprobaciones pendientes, consultar [Estado de verificación de noviembre de 2026](ESTADO-VERIFICACION-2026-11-20.md). No extrapolar el resultado de CI a pruebas funcionales autenticadas en Render.
