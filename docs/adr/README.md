# Registro de decisiones arquitectónicas

Los ADR preservan la evolución de MELT. Las secciones “propuesta inicial”, “pendiente” y las fechas de Entrega 2 son históricas; no describen automáticamente `develop` actual. La arquitectura vigente se resume en [ARCHITECTURE.md](../ARCHITECTURE.md). Las decisiones se mantienen sin borrar para que la cátedra pueda seguir el razonamiento.

| ADR | Decisión y situación en la referencia actual |
|---|---|
| [001 — Límites de servicios](ADR-001-limites-servicios.md) | Límites/ownership; revisar contra la tabla de ownership actual en arquitectura. El texto de validación 2026-10-23 es anterior a la expansión de Reservas/Beneficios. |
| [002 — Arquitectura interna](ADR-002-arquitectura-interna.md) | Capas/estilos propuestos; descripción antigua de handlers aún no implementados es histórica. No usar como inventario actual. |
| [003 — Persistencia](ADR-003-persistencia.md) | Decisión aplicada en la configuración actual: MongoDB para Catálogo/Auth/Pagos y bases PostgreSQL privadas independientes para Reservas y Beneficios. La aplicación del SQL y la conexión a los proyectos Supabase del equipo debe verificarse por entorno. |
| [004 — Consistencia y concurrencia](ADR-004-consistencia-concurrencia.md) | Invariantes en la base (exclusión, unicidad, `check`), transacciones serializables con reintento, idempotencia en checkout, Webhook, eventos, canjes y devoluciones. Verificado con pruebas de concurrencia. |
| [005 — Comunicación](ADR-005-comunicacion.md) | Estado actual mixto: HTTP síncrono y eventos RabbitMQ/outbox; el texto inicial es histórico. |
| [006 — Búsqueda](ADR-006-busqueda.md) | OpenSearch integrado en Catálogo; validaciones fechadas y estado de índice en manual actual. |
| [007 — Caché](ADR-007-cache.md) | Memcached integrado para consultas de listado Catálogo; medir con cautela, evidencia histórica no es garantía de producción. |
| [008 — Contrato propio](ADR-008-contrato-propio.md) | OpenAPI v1 de consulta pública; limita lo que el contrato propio garantiza. |
| [009 — Compatibilidad, carrito y seña](ADR-009-compatibilidad-carrito-sena.md) | Reglas de dominio conservadas; la modalidad de pago actual se define en ADR-015. |
| [010 — Resiliencia](ADR-010-resiliencia.md) | Enmienda 24/11: mecanismos por dependencia ensayados (réplica de Catálogo y RabbitMQ). Ver postmortem. |
| [011 — Observabilidad](ADR-011-observabilidad.md) | Enmienda 24/11: logs JSON, métricas, trazas, tablero y alertas también en Reservas, Pagos, Auth y Beneficios. |
| [012 — Balanceo](ADR-012-balanceo-carga.md) | Enmienda 24/11: ensayo sin errores con una réplica caída; connect timeout 500 ms. |
| [013 — Capacidad y costos](ADR-013-capacidad-costos.md) | Enmienda 24/11: carga medida (~1.100 RPS sin errores), límite en CPU de Catálogo, escalado y costos. |
| [014 — Consumo proveedor](ADR-014-consumo-proveedor.md) | Actualización: existe proxy GhostKitchen, pero grupo/proveedor oficialmente asignado a D9 y contrato exigido no están confirmados. |
| [015 — Checkout único/Mercado Pago](ADR-015-checkout-unico-simulador-pagos.md) | Checkout Pro con credenciales de prueba, retorno y Webhook firmado. |

Las ediciones de estado actuales se agregan como nota/enmienda; las secciones originales conservan su fecha y contexto.
