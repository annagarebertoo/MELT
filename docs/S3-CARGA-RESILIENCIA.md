# S3 — Carga local y resiliencia

El runner usa únicamente Python estándar y Docker Desktop. Reutiliza las comprobaciones de aislamiento, etiquetas, salud y restauración de `scripts/verify-s1-failover.py`; el endpoint es el GET de tratamientos del Gateway de `melt-s1`. No usa `.env`, bases compartidas ni rutas remotas. Las solicitudes no modifican datos.

## Requisitos

- Docker Desktop local con contexto verificable `desktop-linux`.
- El entorno aislado `compose.s1.yaml` iniciado, con Gateway y exactamente dos réplicas de Catálogo saludables.
- Python 3.10 o posterior.

Si el entorno `melt-s1` todavía no está iniciado, desde la raíz del repo:

```powershell
$env:COMPOSE_DISABLE_ENV_FILE = '1'
docker --context desktop-linux compose -f compose.s1.yaml up -d --build --scale catalogo-profesionales=2
```

Este compose usa MongoDB local y la red interna S1. El runner valida esos límites de nuevo antes de generar tráfico.

El runner valida el proyecto, red interna, URI de Mongo local, labels y estado antes de enviar solicitudes. Para inyectar un fallo, primero completa una fase normal con HTTP 200, evidencia correlacionada y tráfico en ambas réplicas. Solo entonces detiene una réplica `catalogo-profesionales` de `melt-s1`; la intenta restaurar desde `finally` ante errores o interrupciones.

## Ejecución

Carga normal de 40 solicitudes, concurrencia 2:

```powershell
python scripts/benchmark-s3-resilience.py --execute --requests 40 --concurrency 2
```

Comparación normal, con una réplica detenida temporalmente, y recuperación:

```powershell
python scripts/benchmark-s3-resilience.py --execute --inject-failure --requests 40 --concurrency 2
```

Modo acotado por duración (hasta el tope de solicitudes indicado):

```powershell
python scripts/benchmark-s3-resilience.py --execute --inject-failure --duration-seconds 20 --max-requests 1000 --concurrency 4
```

Límites: 1–10 workers, 1–10.000 solicitudes o 1–300 segundos, timeout HTTP de 1–30 segundos y tope de 1–10.000 solicitudes en modo duración. `--inject-failure` es independiente y explícito; sin esa opción no detiene contenedores. Cada ejecución escribe un JSON nuevo bajo `output/s3/` y rechaza sobrescribir archivos existentes. No cambia ni reemplaza reportes de `output/s1/`.

## Resultados

Cada fase (`normal`, `failure`, `recovery`) incluye total, éxitos 2xx, fallos, códigos HTTP, tipo de error, latencias p50/p95/p99 y throughput. El JSON conserva cada solicitud, su `request_id`, latencia, estado, error y réplica asociada a los logs. La comparación informa el cambio de p95, throughput y solicitudes fallidas entre normal y fallo.

La latencia medida desde el runner incluye el inicio del proceso `docker exec` más el recorrido HTTP desde el Gateway. Por eso, conviene comparar las fases del mismo informe; no interpretar estos valores como una medición pura de la latencia interna de Catálogo. Un `overall_passed: false` conserva los errores observados y no fuerza el resultado: para el escenario con fallo exige continuidad HTTP 200, uso exclusivo de la réplica sana durante el fallo, restauración saludable y tráfico posterior en ambas réplicas.
