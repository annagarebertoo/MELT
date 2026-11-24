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

Después de reconstruir Catálogo en un entorno local seguro, consultá `http://localhost:8080/api/v1/catalogo/metrics`, generá tráfico de lectura desde el Gateway y abrí Grafana → Dashboards → MELT. Prometheus necesita al menos dos scrapes (30 segundos con el intervalo actual) para calcular tasas. Verificá que `/targets` muestre dos targets Catálogo en estado `UP`.

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

Para comprobar el entorno ya iniciado, consultá el endpoint `/metrics`, confirmá dos targets `UP` en Prometheus y observá actividad en el dashboard después de solicitudes de lectura. La configuración de `compose.yaml` publica Grafana y Prometheus solo en `127.0.0.1`.
