# Registro de decisiones arquitectónicas

Los ADR preservan la evolución de MELT. Las secciones “propuesta inicial”, “pendiente” y las fechas de Entrega 2 son históricas; no describen automáticamente `develop` actual. La arquitectura vigente se resume en [ARCHITECTURE.md](../ARCHITECTURE.md). Las decisiones se mantienen sin borrar para que la cátedra pueda seguir el razonamiento.

| ADR | Decisión y situación en la referencia actual |
|---|---|
| [001 — Límites de servicios](ADR-001-limites-servicios.md) | Límites/ownership; revisar contra la tabla de ownership actual en arquitectura. El texto de validación 2026-10-23 es anterior a la expansión de Reservas/Beneficios. |
| [002 — Arquitectura interna](ADR-002-arquitectura-interna.md) | Capas/estilos propuestos; descripción antigua de handlers aún no implementados es histórica. No usar como inventario actual. |
| [003 — Persistencia](ADR-003-persistencia.md) | Propuesta documental/relacional de etapas anteriores; el código actual usa MongoDB por servicio y Compose también declara PostgreSQL que no consumen servicios de negocio. |
| [005 — Comunicación](ADR-005-comunicacion.md) | Estado actual mixto: HTTP síncrono y eventos RabbitMQ/outbox; el texto inicial es histórico. |
| [006 — Búsqueda](ADR-006-busqueda.md) | OpenSearch integrado en Catálogo; validaciones fechadas y estado de índice en manual actual. |
| [007 — Caché](ADR-007-cache.md) | Memcached integrado para consultas de listado Catálogo; medir con cautela, evidencia histórica no es garantía de producción. |
| [008 — Contrato propio](ADR-008-contrato-propio.md) | OpenAPI v1 de consulta pública; limita lo que el contrato propio garantiza. |
| [009 — Compatibilidad, carrito y seña](ADR-009-compatibilidad-carrito-sena.md) | Reglas de dominio conservadas; texto de pago de proveedor fue reemplazado por ADR-015 (simulador interno). |
| [010 — Resiliencia](ADR-010-resiliencia.md) | Política/ensayos parciales; no asumir failover transparente. Consultar postmortem. |
| [011 — Observabilidad](ADR-011-observabilidad.md) | Instrumentación parcial de Gateway/Catálogo; no generalizar a todos los servicios. |
| [012 — Balanceo](ADR-012-balanceo-carga.md) | Configuración multi-réplica/upstream; el ensayo dejó un 504 observado, no prueba tolerancia sin errores. |
| [013 — Capacidad y costos](ADR-013-capacidad-costos.md) | Supuestos y estimaciones académicas; no son presupuesto ni consumo medido en cloud actual. |
| [014 — Consumo proveedor](ADR-014-consumo-proveedor.md) | Actualización: existe proxy GhostKitchen, pero grupo/proveedor oficialmente asignado a D9 y contrato exigido no están confirmados. |
| [015 — Checkout único/simulador](ADR-015-checkout-unico-simulador-pagos.md) | Aplicado en referencia actual. Pagos no integra Mercado Pago ni cobra dinero. |

Las ediciones de estado actuales se agregan como nota/enmienda; las secciones originales conservan su fecha y contexto.
