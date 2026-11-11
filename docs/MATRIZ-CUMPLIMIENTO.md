# Matriz de requisitos, implementación y evidencia

**Alcance:** revisión documental del árbol `develop` en `ea1ead7e85f533ba54aff85bd95958bb749897a0`; no se ejecutaron pruebas para esta actualización. La fuente oficial en el repositorio es `referencias/Enunciado TP Final (2).pdf`, pero no se pudo extraer su texto en esta sesión sin instalar dependencias (prohibido por la solicitud). Por ello esta tabla relaciona requisitos técnicos registrados en `SPEC.md`, ADR y evidencia disponible; no equivale a una auditoría literal de todas las páginas del PDF.

| Requisito/capacidad identificada | Evidencia en código/configuración | Documento/evidencia | Estado documental |
|---|---|---|---|
| API Gateway y frontend integrado | `infra/nginx/default.conf`, `infra/nginx/web/`, `compose.yaml` | [Arquitectura](ARCHITECTURE.md), [OPERACIONES](OPERACIONES.md) | Implementado localmente; remoto no verificado aquí. |
| Servicios con ownership y persistencia MongoDB | `services/*`, bases separadas y colecciones en `main.go` | [Arquitectura](ARCHITECTURE.md), [Persistencia](PERSISTENCE.md) | Implementado; PostgreSQL está declarado pero no usado por servicios de negocio. |
| Roles y controles de Auth | `services/auth/cmd/api/main.go`; middlewares de Reservas, Catálogo, Beneficios | Manual [Auth](servicios/AUTH.md), [Reservas](servicios/RESERVAS.md) | Rutas y controles presentes; no se repitieron pruebas en esta tarea. |
| CRUD de catálogo, perfiles y asociaciones | `services/catalogo-profesionales/cmd/api/`; transacciones administrativas | [Catálogo](servicios/CATALOGO-PROFESIONALES.md), contrato admin | Implementado según código revisado. |
| Búsqueda OpenSearch y caché Memcached | `search_cache.go`, configuración Compose, pruebas con build tags | [Etapa Catálogo](CATALOGO-PROFESIONALES-ETAPA-1.md), ADR-006/007 | Implementado; resultados de benchmarks son muestras locales históricas. |
| Disponibilidad, agenda, turnos y concurrencia | `services/reservas/cmd/api/`; índices únicos/transacciones | [Reservas](servicios/RESERVAS.md), [web](RESERVAS-WEB.md) | Código presente. Reglas validan conflictos por profesional/minuto y por cliente/categoría/minuto. |
| Retenciones y checkout agrupado, seña del 30 % | `checkout.go`, rutas de carrito, transacción de confirmación | [Reservas](servicios/RESERVAS.md), ADR-015, OpenAPI checkout | Implementado en código; Pago es simulado. |
| Pago real externo | No se encuentra integración de pasarela en `services/pagos` | [Pagos](servicios/PAGOS.md), ADR-015 | No implementado; simulador académico no cobra. |
| Puntos por tratamiento realizado / idempotencia | outbox Reservas, RabbitMQ, consumidor Beneficios, deduplicación | [Beneficios](servicios/BENEFICIOS.md), [flujo](FLUJO-BENEFICIOS.md) | Implementado por código; extremo a extremo en producción no verificado. |
| Canje de recompensas | rutas del programa en Beneficios y frontend | [Beneficios](servicios/BENEFICIOS.md), README | Implementado en código. |
| Acreditación externa para Gimnasio | `/api/v1/beneficios/acreditar-puntos` y `/acreditaciones`, partner key, invitaciones | [contrato](contracts/acreditar-puntos-v1.openapi.yaml), [integración](INTEGRACION-GIMNASIO.md) | Implementada; verificar disponibilidad/URL remota al momento de consumir. |
| Consumo externo GhostKitchen | cliente HTTP y rutas proxy en Beneficios; tests `httptest` | [GhostKitchen](GHOSTKITCHEN.md), [ADR-014](adr/ADR-014-consumo-proveedor.md) | Adaptador implementado; operación real requiere clave/proveedor. La disponibilidad remota no se comprobó. |
| Mensajería asíncrona | RabbitMQ, outbox y consumidor con retry/fallidos | [Comunicación](COMMUNICATION.md), ADR-005, evidencia D5 | Implementación presente; evidencia D5 señala límites de inspección de broker. |
| Resiliencia/failover | `compose.yaml`, upstream y timeouts Gateway | [Postmortem](POSTMORTEM.md), evidencia de fallo de réplica | Parcial: ensayo observó 1 HTTP 504 de 12 con una réplica caída; failover sin errores no demostrado. |
| Observabilidad | logs/traces de Gateway/Catálogo, Prometheus scrape config | [Observabilidad](OBSERVABILITY.md), ADR-011 | Instrumentación parcial. No declarar tablero completo ni métricas de negocio sin evidencia. |
| Manuales/contratos exhaustivos | contratos OpenAPI existentes y rutas en código | [Contratos](contracts/README.md), manuales por servicio | Parcial: hay capacidades sin esquema OpenAPI completo; esta documentación registra las rutas revisadas. |
| Requisitos oficiales exactos del TP | PDF de referencia no extraído en esta sesión | `referencias/Enunciado TP Final (2).pdf` | Pendiente de cotejo literal por la limitación del lector PDF. |

## Verificaciones anteriores citadas

Los documentos de Catálogo, D3/D5/D8/D11 y `docs/evidence/*.json` conservan pruebas de fechas/commits anteriores. No se reejecutaron aquí. Para presentar cumplimiento, adjuntar evidencia de la versión entregada, indicar su commit y no reutilizar las marcas históricas como resultado de la revisión documental actual.
