# MELT — Validación de D5: Comunicación

**Responsable:** Sofía. **Entrega:** 2 — 23 de octubre de 2026.

**Rama:** `feature/sofia-d5-comunicacion`. **Base D3:** `1d5394b08863f1facaded819f8008ba2d130fd4c`, que incluye D11 (`0273c0b`).

## Alcance y estado

Se valida incrementalmente D5 sobre el flujo existente **cliente HTTP → NGINX → Catálogo → MongoDB**, sin cambiar código de negocio, configuración ni contratos. El enunciado definitivo `Enunciado TP Final (3).pdf` exige validar D5 para la Entrega 2; el evento de dominio publicado y consumido, la idempotencia y el tratamiento de mensajes no procesables continúan siendo requisitos de la etapa posterior. Esta evidencia no acredita toda la comunicación final del sistema.

| Interacción | Estado real |
| :--- | :--- |
| Cliente HTTP → gateway → Catálogo | Implementada y comprobada con respuestas reales, errores y logs correlacionados. El cliente de prueba no es un frontend. |
| Catálogo → MongoDB | Integrada. La regresión D11 verifica spans del driver bajo la solicitud HTTP; D3 conserva su validación de persistencia. |
| Gateway → Reservas / Beneficios | Enrutamiento configurado; servicios con endpoint de salud. No se validaron operaciones de negocio. |
| Reservas → Catálogo / Beneficios | Diseñada en ADR-005; sin llamadas de negocio implementadas en el código inspeccionado. |
| Eventos mediante RabbitMQ | Solo diseño y declaración de infraestructura. No hay comunicación asíncrona de negocio validada. |
| Exportación de trazas a Jaeger | Implementada y verificada por D11. Es telemetría, no publicación y consumo de eventos de dominio. |

## Políticas existentes

- NGINX recibe `/api/v1/catalogo/`, elimina ese prefijo y envía la solicitud a `catalogo-profesionales:8081` mediante `catalogo_backend`. Las respuestas probadas registran un upstream real en el log del gateway.
- `X-Request-ID` válido se conserva; si falta o es inválido, D11 genera uno. NGINX y Catálogo admiten hasta 64 caracteres con el patrón `[A-Za-z0-9][A-Za-z0-9_-]{0,63}`. El contexto W3C `traceparent` se propaga y ambas capas registran la misma traza. El gateway devuelve los identificadores en las respuestas, incluidos los errores comprobados.
- Los errores de validación devuelven 400 con `INVALID_PARAMETER`; un tratamiento inexistente devuelve 404 con `TRATAMIENTO_NOT_FOUND`. Son errores de aplicación, no fallas de transporte. No se provocaron errores 502, 503 o 504 en el entorno.

| Límite | Configuración inspeccionada | Alcance de la comprobación |
| :--- | :--- | :--- |
| Conexión NGINX → upstream | `proxy_connect_timeout 2s` | Presencia y sintaxis, sin provocar vencimiento. |
| Envío NGINX → upstream | `proxy_send_timeout 5s` | Presencia y sintaxis, sin provocar vencimiento. |
| Lectura NGINX ← upstream | `proxy_read_timeout 5s` | Presencia y sintaxis, sin provocar vencimiento. |
| Operaciones de tratamientos → MongoDB | Contexto de 5 segundos en el código existente. | Inspección; no se ralentizó MongoDB. |
| Salud de Catálogo → MongoDB | Contexto de 2 segundos. | Inspección y respuesta saludable; sin caída provocada. |
| Recepción de encabezados en Catálogo | `ReadHeaderTimeout` de 5 segundos. | Inspección; no es un límite total de ejecución del handler. |

Los tiempos de envío y lectura del proxy limitan intervalos entre operaciones sucesivas, no garantizan una duración total de cinco segundos para cualquier solicitud. La comprobación `nginx -T` valida y muestra los archivos de configuración del contenedor; no recarga el proceso ni demuestra por sí sola el vencimiento efectivo de esos límites.

No hay directivas explícitas `proxy_next_upstream*` en la configuración inspeccionada del gateway. Esto no significa que NGINX jamás reintente: no se validó su comportamiento implícito ante fallas. Tampoco existen llamadas HTTP de negocio entre microservicios con una política de reintentos comprobable. Quedan pendientes la política explícita por operación, las respuestas tardías o ausentes y su relación con la idempotencia. No se considera seguro repetir una escritura por el solo hecho de no haber recibido respuesta.

La exportación OTLP de Catálogo tiene timeout de 2 segundos y reintentos deshabilitados en el código de D11. Esa política de telemetría no debe presentarse como política de las futuras operaciones de negocio.

## Comunicación asíncrona

Compose declara `rabbitmq:3-management`, puertos AMQP y de administración, healthcheck y volumen. `docker compose ps --all` no mostró ningún contenedor de RabbitMQ en esta ejecución. No se inició el broker y no se consultaron colas, conexiones ni consumidores en él.

La inspección de los servicios y la búsqueda reproducible sobre archivos Go y `go.mod` no encontraron referencias AMQP/RabbitMQ ni productores o consumidores implementados. La búsqueda es evidencia acotada al repositorio inspeccionado; no acredita el estado de sistemas externos.

Se mantienen como diseño los eventos de confirmación, cancelación y finalización; los efectos de correo, puntos e indexación; y el tratamiento de duplicados y mensajes fallidos. La política de correo documentada contempla un intento y hasta tres reintentos, luego una cola de errores. No está implementada ni probada. Los esquemas, exchanges, colas, confirmaciones, recuperación e idempotencia siguen pendientes; Outbox conserva su estado de patrón candidato.

## Reproducción y resultados

Requisitos: Python 3, Docker Compose y el entorno existente con gateway, dos réplicas de Catálogo, MongoDB y Jaeger funcionando, con `test-001` preexistente. Desde la raíz:

```powershell
python -X utf8 scripts/verify-communication.py
```

Para guardar otra captura sin reemplazar la evidencia de la entrega:

```powershell
python -X utf8 scripts/verify-communication.py --output "$env:TEMP\melt-d5-nueva-evidencia.json"
```

El verificador solo ejecuta GET e inspecciones de Docker; reutiliza funciones y la regresión de D11 sin modificar sus archivos. No inicia, detiene o reinicia servicios, ni publica mensajes o escribe datos de negocio. Solo escribe el JSON si se solicita `--output`. Si falta una dependencia o el registro, falla y requiere revisar el entorno; no prepara datos automáticamente.

| Prueba ejecutada el 23/10/2026 | Resultado |
| :--- | :--- |
| `nginx -T` y comparación del archivo del contenedor con el repositorio | Correctos; timeouts 2s/5s/5s presentes. |
| Salud, listado y detalle de `test-001` vía gateway | HTTP 200. |
| `limit=0` y ordenamiento inválido | HTTP 400 y código `INVALID_PARAMETER`. |
| Identificador aleatorio inexistente | HTTP 404 y código `TRATAMIENTO_NOT_FOUND`. |
| Correlación de las seis solicitudes | Request ID y trace ID conservados en respuesta y logs de ambas capas; upstream registrado. |
| Regresión D11 | Cuatro casos aprobados, relaciones de spans correctas, privacidad y dos réplicas saludables. |
| Tests existentes y `go vet` de Catálogo | Aprobados, sin modificar los tests anteriores. |
| Estado RabbitMQ y código de mensajería | Sin contenedor RabbitMQ; sin coincidencias AMQP/RabbitMQ en el código inspeccionado. No representa una prueba de mensajería funcional. |

La [evidencia JSON](evidence/d5-2026-10-23.json) conserva identificadores, estados, logs correlacionados, configuración comprobada, estado del broker y salida de la regresión D11. No incluye credenciales ni cuerpos de tratamientos. Las trazas de Jaeger son temporales; el JSON conserva los resultados de esta ejecución.

## Límites para la etapa posterior

D5 queda validada parcialmente para el flujo síncrono disponible en la Entrega 2 y documenta explícitamente lo aún no implementado. Permanecen pendientes las llamadas de negocio entre microservicios, política explícita de reintentos, vencimientos y respuestas tardías probados mediante fallas controladas autorizadas, idempotencia de escrituras, eventos reales y procesamiento de mensajes fallidos. No se acreditan comunicación asíncrona funcional, cumplimiento integral de D5 final ni integración externa.

La diferencia existente entre la ruta real y la del contrato sigue fuera de esta modificación. Tampoco se valida aquí el frontend, autenticación, pagos o funcionalidades de Reservas y Beneficios.
