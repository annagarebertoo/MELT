# MELT — Observabilidad

## Arquitectura

Prometheus recolecta `/metrics` de cada réplica de Catálogo mediante DNS service discovery del nombre `catalogo-profesionales`. Grafana usa Prometheus como fuente provisionada y carga el tablero **MELT · Observabilidad S2** al iniciar. Jaeger recibe las trazas OTLP ya emitidas por el módulo OpenTelemetry de NGINX y el SDK de Catálogo.

El Gateway mantiene logs JSON de acceso con `request_id`, `trace_id`, ruta, estado, duración y upstream. Catálogo registra logs JSON con esos identificadores, `instance`, estado y duración. `request_id` correlaciona logs; `trace_id` permite seguir los spans distribuidos. Catálogo instrumenta spans HTTP y spans cliente para comandos MongoDB, solicitudes OpenSearch y operaciones Memcached, sin exportar URLs de búsqueda ni consultas. Jaeger conserva trazas en memoria; la configuración actual es local y no ofrece persistencia ni autenticación.

## Métricas

El endpoint `GET /metrics` de Catálogo conserva las métricas existentes de caché, búsqueda e indexación y expone:

| Métrica | Uso |
| --- | --- |
| `catalog_http_requests_total` | Contador HTTP por método, ruta normalizada, estado y réplica. |
| `catalog_http_request_duration_seconds` | Histograma para calcular p95 y otros percentiles. |
| `catalog_replica_info` | Identidad del contenedor Catálogo que expone las métricas. |
| `up{job="catalogo-profesionales"}` | Disponibilidad individual observada por Prometheus. |

El endpoint `/metrics` no cuenta sus propias solicitudes. Las rutas se toman del patrón de Gin y los métodos desconocidos se agrupan como `OTHER`, para evitar etiquetas de alta cardinalidad.

Prometheus descubre las direcciones A del servicio y crea un target por réplica. El label `replica` identifica el hostname de cada contenedor; `instance` identifica la dirección scrapeada. La tasa se calcula con `rate(...)`; el p95, con `histogram_quantile(0.95, ...)` sobre el histograma.

## Dashboard y acceso local

El dashboard incluye RPS por réplica, latencia p95, tasa de 5xx, solicitudes por estado HTTP y disponibilidad por instancia.

- Grafana: <http://localhost:3000> — usuario `admin`; contraseña `GRAFANA_PASSWORD` del entorno o `admin_dev_only` si no se configuró.
- Prometheus targets: <http://localhost:9090/targets>.
- Alertas Prometheus: <http://localhost:9090/alerts>.
- Jaeger: <http://localhost:16686>.

En un entorno local seguro, con las dependencias de Catálogo ya iniciadas:

```powershell
docker compose up -d --build --no-deps catalogo-profesionales
docker compose up -d --no-deps prometheus grafana
```

Después de reconstruir Catálogo en un entorno local seguro, consultá las métricas desde la red interna (`docker compose exec prometheus wget -qO- http://catalogo-profesionales:8081/metrics`; el Gateway responde 404 a `/api/v1/*/metrics` para no exponerlas), generá tráfico de lectura desde el Gateway y abrí Grafana → Dashboards → MELT. Prometheus necesita al menos dos scrapes (30 segundos con el intervalo actual) para calcular tasas. Verificá que `/targets` muestre dos targets Catálogo en estado `UP`.

El Compose principal configura `MONGODB_URI` mediante el `.env` local y puede apuntar a Atlas. No ejecutes esos comandos contra una base compartida para esta verificación; usá un entorno local aislado. Prometheus/Grafana solo scrapean la ruta de métricas y no necesitan consultar una base de negocio.

## Trazas y correlación

Para inspeccionar una solicitud, consultá el `X-Trace-ID` devuelto por el Gateway en Jaeger y buscá el mismo `request_id` en los logs de Gateway y Catálogo. La traza muestra el span NGINX, el span HTTP hijo de Catálogo y spans cliente de MongoDB, OpenSearch y Memcached según el camino ejecutado. La evidencia previa de D11 está en [`docs/evidence/d11-2026-10-23.json`](evidence/d11-2026-10-23.json); la decisión está en [ADR-011](adr/ADR-011-observabilidad.md).

NGINX puede conservar diagnósticos nativos de arranque en su log de error. Los logs de acceso HTTP y los de Catálogo son JSON. El procesador de Jaeger elimina atributos sensibles como URL sin normalizar, user-agent y texto de consulta antes de guardar spans.

## Alertas y criterios

El SLI de disponibilidad es la fracción de muestras Prometheus en que ambas réplicas están `UP`. El objetivo inicial para la entrega es **≥ 99% durante los últimos 7 días**. Es un objetivo local/académico, no un SLO de producción; el panel y la alerta necesitan acumular muestras para tener ventana suficiente. No se fija un SLO numérico de latencia antes de medir una carga representativa; el dashboard muestra el p95 observado.

Las reglas de `infra/prometheus/alerts.yml` generan:

- `CatalogReplicaUnavailable`: menos de dos targets saludables durante 1 minuto, incluso si DNS deja de publicar todos los targets.
- `CatalogHttp5xxBurst`: tres o más respuestas 5xx en una ventana de 5 minutos, sostenidas durante 1 minuto.
- `CatalogReplicaAvailabilitySLOBreach`: disponibilidad de réplicas menor al 99% en una ventana móvil de 7 días, sostenida durante 5 minutos.

Las alertas quedan visibles en Prometheus. No hay Alertmanager configurado para enviar notificaciones externas.

## Comprobación

Verificá la sintaxis de la aplicación sin iniciar servicios ni consultar bases compartidas:

```powershell
Push-Location services/catalogo-profesionales
go test -mod=readonly ./cmd/api
Pop-Location
```

Para comprobar el entorno ya iniciado, consultá `/metrics` desde la red interna, confirmá dos targets `UP` en Prometheus y observá actividad en el dashboard después de solicitudes de lectura. La configuración de `compose.yaml` publica Grafana y Prometheus solo en `127.0.0.1`.

## Flujo principal: Reservas, Pagos, Auth y Beneficios

Desde el 24/11/2026 los cuatro servicios del flujo principal tienen la misma instrumentación que Catálogo.

### Logs

JSON por `slog` con `timestamp`, `level`, `msg`, `service`, `instance`, `request_id` y `trace_id`. El `X-Request-ID` que pone el Gateway se propaga en las llamadas HTTP internas, así que un mismo `request_id` aparece en Gateway, Reservas, Catálogo y Pagos.

### Trazas

Con `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` definido (en `compose.yaml` apunta a `http://jaeger:4318/v1/traces`), cada servicio exporta spans OTLP/HTTP. Sin esa variable sólo propaga `traceparent`, sin exportar. Spans:

- servidor HTTP por ruta (`POST /turnos/:id/completar`, `POST /carrito/checkout`, …);
- cliente HTTP por destino (`HTTP GET catalogo-profesionales`, `HTTP POST pagos`, …);
- `rabbitmq publish <routing key>` en Reservas y `rabbitmq consume <routing key>` en Beneficios.

El contexto de la traza se guarda junto al evento en el outbox (`_traza`, que se quita antes de publicar) y viaja en las cabeceras AMQP. Por eso la traza de un turno completado llega hasta la acreditación de puntos aunque el broker haya estado caído entre medio.

### Métricas

| Métrica | Servicio | Uso |
|---|---|---|
| `melt_http_requests_total{service,instance,method,route,status}` | todos | RPS y tasa de 5xx por ruta |
| `melt_http_request_duration_seconds` | todos | p95 por ruta (checkout, Webhook, completar) |
| `melt_http_client_requests_total{destino,status}` y `melt_http_client_request_duration_seconds` | todos | Salud y latencia de dependencias (Catálogo, Auth, Pagos, Mercado Pago) |
| `melt_reservas_outbox_publicaciones_total{tipo,resultado}` | Reservas | Eventos publicados o fallidos |
| `melt_reservas_outbox_demora_segundos` | Reservas | Tiempo entre el commit y la publicación |
| `melt_beneficios_eventos_consumidos_total{tipo,resultado}` | Beneficios | Eventos procesados, reintentados o enviados a la DLQ |
| `melt_beneficios_evento_procesamiento_segundos` | Beneficios | Duración de la acreditación |
| `melt_service_info{service,instance}` | todos | Identidad de la instancia |

Los contadores de negocio arrancan en 0 para que `increase()` vea el primer evento. Prometheus descubre cada servicio por DNS (`reservas:8082`, `beneficios:8083`, `auth:8084`, `pagos:8085`).

### Tablero

**MELT · Flujo principal** (`infra/grafana/dashboards/flujo-principal.json`): disponibilidad por servicio, RPS, tasa de 5xx, p95 por servicio, checkout y Webhook, turnos completados/no asistió/cancelados, outbox (resultados y demora), eventos consumidos por resultado, llamadas salientes y p95 de dependencias.

### Objetivo de servicio y alertas

Objetivo del flujo principal: **al menos 95 % de las solicitudes de Reservas sin 5xx** y **p95 del checkout menor a 2 s**. Reglas en `infra/prometheus/alerts.yml`, grupo `melt-flujo-principal`:

| Alerta | Condición | Severidad |
|---|---|---|
| `MeltServiceDown` | Un servicio del flujo sin scrape durante 1 min | critical |
| `ReservasErrorRateHigh` | Más de 5 % de 5xx en Reservas durante 5 min | warning |
| `CheckoutLatencyP95High` | p95 del checkout > 2 s durante 5 min | warning |
| `OutboxPublishFailing` | ≥ 3 publicaciones fallidas en 5 min, sostenido 2 min | warning |
| `BeneficiosEventsDeadLettered` | Algún evento llegó a la DLQ en los últimos 15 min | warning |
| `PaymentProviderErrors` | ≥ 3 errores 5xx o de red de Pagos hacia Mercado Pago en 5 min | warning |

Las reglas tienen pruebas unitarias: `promtool test rules infra/prometheus/tests/flujo-principal-alerts.test.yml`. Durante la caída controlada de RabbitMQ del 24/11/2026, `OutboxPublishFailing` y `BeneficiosEventsDeadLettered` pasaron a *firing* ([POSTMORTEM](POSTMORTEM.md)).
