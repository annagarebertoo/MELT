# Inventario documental de MELT

Inventario del árbol de `develop` de referencia y del conjunto de documentación actualizado en esta tarea. Los documentos históricos y la evidencia se conservan; el estado/caveat indicado evita tratarlos como verdad operativa actual.

## Documentos de entrada en la raíz

| Archivo | Propósito / estado |
|---|---|
| `README.md` | Entrada general y enlaces a las guías vigentes. |
| `SPEC.md` | Requisitos de producto originales y reglas; mantiene contenido histórico con aviso de diferencias respecto de la implementación actual. |
| `docs/README.md` | Índice general y guía sobre qué documentos son vigentes/históricos. |
| `docs/INVENTARIO.md` | Este inventario. |

## Arquitectura, operación y manuales

| Archivo | Propósito / estado |
|---|---|
| `docs/ARCHITECTURE.md` | Arquitectura actual, ownership y diagramas Mermaid. |
| `docs/OPERACIONES.md` | Requisitos, variables, ejecución, pruebas y estado remoto no verificado. |
| `docs/manuales/USUARIO.md` | Recorridos por rol. |
| `docs/manuales/DESARROLLO.md` | Organización, contratos, pruebas y seguridad para desarrolladores. |
| `docs/servicios/README.md` | Índice de manuales de microservicios y límites de rutas Gateway. |
| `docs/servicios/AUTH.md` | Auth, roles, endpoints, integraciones y pruebas. |
| `docs/servicios/CATALOGO-PROFESIONALES.md` | Catálogo, CRUD, búsqueda, caché, asociaciones y configuración. |
| `docs/servicios/RESERVAS.md` | Agenda, disponibilidad, reglas de reserva, carrito, checkout y eventos. |
| `docs/servicios/PAGOS.md` | Simulador interno y operaciones server-to-server. |
| `docs/servicios/BENEFICIOS.md` | Puntos, canjes, eventos, partner y GhostKitchen. |
| `docs/MATRIZ-CUMPLIMIENTO.md` | Requisito, referencia de código, documento/evidencia, estado y limitación del PDF. |
| `docs/TROUBLESHOOTING.md` | Síntoma, diagnóstico y resolución segura. |
| `docs/GLOSARIO.md` | Terminología de dominio y arquitectura. |

## Documentos de capacidades y guías anteriores

| Archivo | Propósito / estado |
|---|---|
| `docs/LOCAL-DEV.md` | Guía heredada complementaria; no sustituye OPERACIONES para configuración actual. |
| `docs/RESERVAS-WEB.md` | Recorrido web y preparación manual de datos demo. |
| `docs/CATALOGO-PROFESIONALES-ETAPA-1.md` | CRUD, límites, OpenSearch/Memcached, pruebas y benchmark histórico. |
| `docs/FLUJO-BENEFICIOS.md` | Flujo de acreditación, canje y eventos. |
| `docs/ACREDITACION-PUNTOS.md` | Configuración y operación del API de puntos externo e invitaciones. |
| `docs/IMPLEMENTACION-ACREDITACION-PUNTOS.md` | Historial de implementación y validación de esa integración. |
| `docs/INTEGRACION-GIMNASIO.md` | Guía del consumidor Grupo 1; URL publicada en el documento requiere verificación actual. |
| `docs/GHOSTKITCHEN.md` | Proxy de proveedor, endpoints, configuración y pruebas mock. |
| `docs/PRUEBAS-RENDER-ACREDITACION.md` | Evidencia histórica de Render con rama/commit/URLs fechados. |
| `docs/ENTREGA-2.md` | Informe histórico de Entrega 2; sus estados no describen el código actual. |
| `docs/COMMUNICATION.md` | Ensayo histórico D5 del 23/10; se añadió aviso que lo distingue del flujo actual de RabbitMQ. |
| `docs/PERSISTENCE.md` | Validación histórica D3 del 23/10; se añadió aviso del estado MongoDB posterior. |
| `docs/OBSERVABILITY.md` | Evidencia/guía histórica D11; no acredita instrumentación completa de todos los servicios. |
| `docs/POSTMORTEM.md` | Resultado parcial del ensayo de resiliencia y acciones pendientes. |

## ADR (historial de decisiones)

| Archivo | Decisión |
|---|---|
| `docs/adr/README.md` | Índice y estado vigente/histórico por ADR. |
| `docs/adr/ADR-001-limites-servicios.md` | Límites de microservicios. |
| `docs/adr/ADR-002-arquitectura-interna.md` | Organización interna/estilos. |
| `docs/adr/ADR-003-persistencia.md` | Persistencia y ownership. |
| `docs/adr/ADR-005-comunicacion.md` | Comunicación síncrona/asíncrona; addenda vigente. |
| `docs/adr/ADR-006-busqueda.md` | OpenSearch. |
| `docs/adr/ADR-007-cache.md` | Memcached. |
| `docs/adr/ADR-008-contrato-propio.md` | API propia de catálogo. |
| `docs/adr/ADR-009-compatibilidad-carrito-sena.md` | Reglas de compatibilidad, retención y seña; referencias a pasarela son históricas. |
| `docs/adr/ADR-010-resiliencia.md` | Resiliencia. |
| `docs/adr/ADR-011-observabilidad.md` | Observabilidad. |
| `docs/adr/ADR-012-balanceo-carga.md` | Balanceo. |
| `docs/adr/ADR-013-capacidad-costos.md` | Capacidad/costos estimados, no presupuesto cloud vigente. |
| `docs/adr/ADR-014-consumo-proveedor.md` | Investigación histórica y actualización GhostKitchen; asignación oficial D9 no confirmada. |
| `docs/adr/ADR-015-checkout-unico-simulador-pagos.md` | Checkout agrupado y simulador actual. |

## Contratos OpenAPI y mock

| Archivo | Propósito / estado |
|---|---|
| `docs/contracts/README.md` | Índice de contratos y lagunas OpenAPI. |
| `docs/contracts/tratamientos-v1.openapi.yaml` | Consulta pública de tratamientos. |
| `docs/contracts/catalogo-admin-v1.openapi.json` | CRUD Catálogo y vinculación de cuenta profesional. |
| `docs/contracts/autenticacion-v1.openapi.yaml` | Auth y administración de usuarios/roles. |
| `docs/contracts/reservas-checkout-v1.openapi.yaml` | Carrito y checkout agrupado; no cubre todo el servicio Reservas. |
| `docs/contracts/pagos-simulador-v1.openapi.yaml` | API interna del simulador. |
| `docs/contracts/acreditar-puntos-v1.openapi.yaml` | API externa de acreditación. |
| `docs/contracts/registro-externo-v1.openapi.yaml` | Contrato histórico retirado; conservado por trazabilidad. |
| `docs/contracts/mock/README.md` | Instrucciones del mock de catálogo. |
| `docs/contracts/mock/go.mod` | Módulo Go del mock. |
| `docs/contracts/mock/main.go` | Implementación del mock con datos fijos. |

## Evidencia histórica

| Archivo | Registro |
|---|---|
| `docs/evidence/cierre-final-d3-2026-10-23.json` | Cierre D3 fechado. |
| `docs/evidence/cierre-final-d5-2026-10-23.json` | Cierre D5 fechado. |
| `docs/evidence/cierre-final-d8-2026-10-23.json` | Cierre D8 fechado. |
| `docs/evidence/d11-2026-10-23.json` | Traza/observabilidad D11. |
| `docs/evidence/d3-2026-10-23.json` | Persistencia D3. |
| `docs/evidence/d5-2026-10-23.json` | Comunicación D5. |
| `docs/evidence/d8-2026-10-23.json` | Contrato D8. |
| `docs/evidence/entrega-2-2026-10-23.json` | Captura agregada de Entrega 2. |
| `docs/evidence/fallo-replica-2026-10-23.json` | Ensayo parcial de fallo de réplica. |

## Referencias académicas en `referencias/`

- `Enunciado TP Final (2).pdf` — enunciado oficial identificado; su texto no pudo extraerse durante la actualización sin instalar un lector.
- `api-gateway-load-balancing-y-service-discovery.pdf`
- `cache.pdf`
- `clase01-monolito-y-microservicios.pdf`
- `comunicacion-asincrona.pdf`
- `datos-en-microservicios.pdf`
- `estilos-de-arquitectura-de-backend.pdf`
- `motores-de-busqueda.pdf`
- `resiliencia-ante-fallos-parciales.pdf`

Estos PDFs son material de referencia, no documentación normativa del comportamiento implementado.
