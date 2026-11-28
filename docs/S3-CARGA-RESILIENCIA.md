# S3 — Carga local y resiliencia

El runner usa únicamente Python estándar y Docker (Docker Desktop en Windows/macOS o el socket local `unix:///var/run/docker.sock` en Linux). Reutiliza las comprobaciones de aislamiento, etiquetas, salud y restauración de `scripts/verify-s1-failover.py`; el endpoint es el GET de tratamientos del Gateway de `melt-s1`. No usa `.env`, bases compartidas ni rutas remotas. Las solicitudes no modifican datos.

## Requisitos

- Docker Desktop local con contexto verificable `desktop-linux`, o en Linux el daemon local (el verificador exige que el servidor informe `OSType linux` y rechaza contextos remotos).
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

## Ejecución del 24/11/2026 y prueba de capacidad

Resultados y análisis en el [POSTMORTEM](POSTMORTEM.md) (ensayo A) y en [ADR-013](adr/ADR-013-capacidad-costos.md). JSON en [`docs/evidence/2026-11-24/`](evidence/2026-11-24/). Resumen: 600/600 HTTP 200 en las tres fases; p95 normal 124 ms, con fallo 121 ms y en recuperación 123 ms (la latencia incluye el arranque de `docker exec`).

Este runner mide resiliencia con poca concurrencia. Para la capacidad máxima se agregó `scripts/carga-capacidad.py`, que corre dentro de la red Docker y sube la concurrencia por escalones:

```sh
docker run --rm --network melt-s1_s1 -v "$PWD/scripts:/s:ro" -v "$PWD/output:/out" python:3.13-alpine \
  python /s/carga-capacidad.py --url "http://gateway/api/v1/catalogo/tratamientos?page=1&limit=10" \
  --escalones 1,2,4,8,16,32,64 --segundos 10 --salida /out/capacidad/catalogo-listado.json
```

Sólo acepta los hosts `gateway`, `localhost` y `127.0.0.1`, nunca Render ni terceros, y no sobrescribe informes existentes.
