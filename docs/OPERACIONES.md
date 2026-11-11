# Operaciones, configuración y despliegue

Los valores siguientes salen de `compose.yaml`, `.env.example` y los `os.Getenv` de cada servicio. No copiar secretos a documentos, issues o capturas. `.env.example` contiene marcadores/valores exclusivamente de desarrollo; para uso local se puede crear `.env` ignorado por Git.

## Requisitos y arranque local

Docker Desktop con Compose v2; Go 1.26.1 y Node.js sólo para trabajo fuera de contenedor/verificaciones frontend.

1. En PowerShell, desde la raíz, preparar secretos locales efímeros y obligatorios:

```powershell
$env:AUTH_JWT_SECRET = [Convert]::ToBase64String([Security.Cryptography.RandomNumberGenerator]::GetBytes(48))
$env:MELT_INTERNAL_KEY = [Convert]::ToBase64String([Security.Cryptography.RandomNumberGenerator]::GetBytes(48))
```

2. Validar y levantar:

```powershell
docker compose config --quiet
docker compose up --build -d
docker compose ps
Invoke-RestMethod http://localhost:8080/health
```

Para logs: `docker compose logs --tail 100 gateway auth catalogo-profesionales reservas pagos beneficios`. Reinicio acotado: `docker compose restart <servicio>`; rebuild acotado: `docker compose up --build -d <servicio>`. No reiniciar MongoDB ni usar `down -v` para un problema de código. Apagado sin borrar volúmenes: `docker compose down`.

## Puertos del Compose

| Servicio | Puerto host | Uso |
|---|---:|---|
| Gateway | 8080 | Aplicación y API |
| MongoDB | 27017 | Base de desarrollo; replica set `rs0` |
| PostgreSQL | 5432 | Servicio declarado; no consumido por los servicios de negocio en este commit |
| RabbitMQ | 5672 / 15672 | AMQP / Management UI |
| OpenSearch | 9200 | Búsqueda local, seguridad deshabilitada para desarrollo |
| Memcached | 11211 | Cache de catálogo |
| Prometheus | 9090 | Métricas |
| Grafana | 3000 | Tableros |
| Jaeger | 127.0.0.1:16686 | Consulta local de trazas |

URLs de UI y endpoints locales se listan en [LOCAL-DEV.md](LOCAL-DEV.md). Prometheus scrapea Prometheus y Catálogo; el archivo versionado no provisiona un dashboard funcional completo. No exponer puertos de desarrollo a Internet.

## Variables de entorno

| Variable | Servicio | Req. | Ejemplo no secreto / default | Si falta |
|---|---|---:|---|---|
| `AUTH_JWT_SECRET` | Auth | Sí en Compose | generar localmente, no persistir | Compose no valida; Auth no arranca sin una clave válida. |
| `MELT_INTERNAL_KEY` | Auth, Catálogo, Reservas, Pagos, Beneficios | Sí | aleatoria por instalación, compartida sólo entre servicios del mismo entorno | `docker compose config` falla; llamadas internas no autorizan. |
| `MONGODB_URI` | Todos los backends | Sí operativamente | `mongodb://mongodb:27017` en Compose | fallback del proceso a localhost, no adecuado dentro de contenedor. |
| `AUTH_MONGODB_DATABASE` | Auth | No | `usuarios_db` | default Auth. |
| `CATALOGO_MONGODB_DATABASE` | Catálogo | No | `catalogo_db` | default Catálogo. |
| `PROFESIONALES_MONGODB_DATABASE` | Catálogo | No | vacío | usa la base de Catálogo. Cambiar con datos exige migración. |
| `RESERVAS` database | Reservas | No | fija en código: `reservas_db` | no tiene variable de nombre en este commit. |
| `PAGOS_MONGODB_DATABASE` | Pagos | No | `pagos_db` | default `pagos_db`. |
| `BENEFICIOS_MONGODB_DATABASE` | Beneficios | No | `beneficios_db` | default del servicio. |
| `AUTH_TOKEN_TTL_MINUTES` | Auth | No | `15` | 15 minutos. |
| `AUTH_ADMIN_EMAIL`, `AUTH_ADMIN_PASSWORD` | Auth | No | no mostrar valores | sin ambos, no se crea superadministrador inicial. Juntos deben validar email y contraseña de 12–72 bytes; credenciales sólo de arranque local/controlado. |
| `AUTH_URL` | Catálogo, Reservas, Beneficios | No | `http://auth:8084` | cada consumidor usa ese default Compose. |
| `CATALOGO_URL` | Auth, Reservas | No | `http://catalogo-profesionales:8081` | fallback interno. |
| `PAGOS_URL` | Reservas | No | `http://pagos:8085` | fallback interno. |
| `BENEFICIOS_URL` | Auth, Reservas | No | `http://beneficios:8083` | fallback interno. |
| `OPENSEARCH_URL` | Catálogo | No en proceso; depend. funcional | `http://opensearch:9200` | fallback interno; búsqueda indexada requiere servicio. |
| `MEMCACHED_ADDRESS` | Catálogo | No | `memcached:11211` | fallback interno. |
| `MEMCACHED_NAMESPACE` | Catálogo | No | `melt:catalogo` | prefijo de claves compartido entre réplicas del entorno. |
| `RABBITMQ_URL` | Reservas, Beneficios | No en proceso; depend. eventos | `amqp://guest:guest@rabbitmq:5672/` | publicación/consumo asíncrono no opera. |
| `MELT_TIMEZONE` | Reservas | No | `America/Argentina/Buenos_Aires` | usa esa zona. Valor inválido recurre a default según test. |
| `PAYMENT_SIMULATOR_RESULT` | Pagos | No | `approved` o `rejected` | `approved`; cualquier otro valor detiene el proceso. |
| `BENEFICIOS_PARTNER_KEY` | Beneficios | No para API interna; sí para partner | secreto provisionado privadamente | acreditación externa devuelve no disponible. |
| `BENEFICIOS_INVITATION_SECRET` | Beneficios | Opcional según flujo | secreto aleatorio estable | enlaces de invitación no se pueden emitir/validar correctamente. |
| `MELT_PUBLIC_URL` | Beneficios | No | `http://localhost:8080` | default local para URL de invitación. |
| `GHOSTKITCHEN_API_KEY` | Beneficios | Sí para proveedor real | entregar por canal privado | proxy devuelve error de integración no configurada. |
| `GHOSTKITCHEN_BASE_URL` | Beneficios | No | default del cliente en código; mock local configurable | usa base upstream predeterminada. |
| `PORT` | Cada backend | No en Compose | catálogo 8081, reservas 8082, beneficios 8083, auth 8084, pagos 8085 | valores definidos por servicio. |
| `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` | Catálogo | No | `http://jaeger:4318/v1/traces` | no exporta trazas a Jaeger. |
| `POSTGRES_PASSWORD` | PostgreSQL Compose | No | valor de desarrollo solo local | Compose tiene default inseguro para uso local. |
| `GRAFANA_PASSWORD` | Grafana Compose | No | valor de desarrollo solo local | default local `admin_dev_only`. |

Compose inicializa MongoDB como replica set. Para entorno remoto se requieren valores gestionados fuera de Git y una topología Mongo compatible con transacciones; la tabla no implica que un despliegue externo esté configurado.

## Pruebas y verificaciones disponibles

Pruebas unitarias/paquete (ejecutar desde la carpeta de cada servicio):

```sh
cd services/auth; go test ./...; cd ../..
cd services/catalogo-profesionales; go test ./...; cd ../..
cd services/reservas; go test ./...; cd ../..
cd services/pagos; go test ./...; cd ../..
cd services/beneficios; go test ./...; cd ../..
node --check infra/nginx/web/app.js
docker compose config --quiet
git diff --check
```

Los ejemplos con `;` son para PowerShell. En Bash, usar `cd services/auth && go test ./...` (y análogamente para cada módulo). Los tests con integración Mongo requieren `AUTH_TEST_MONGODB_URI`, `CATALOGO_TEST_URI`/`MONGODB_TEST_URI`, `RESERVAS_TEST_MONGODB_URI`, `BENEFICIOS_TEST_MONGODB_URI` o `PAGOS_TEST_MONGODB_URI` según el módulo y usan datos mutables: apunta cada uno a instancia/base desechable que pueda aislarse. OpenSearch/Memcached se cubren con build tags de Catálogo: `go test -race -tags 'catalogointegration catalogosearchintegration' ./...`; requiere `CATALOGO_TEST_URI`, `CATALOGO_TEST_OPENSEARCH_URL` y `CATALOGO_TEST_MEMCACHED_ADDRESS`. Consultar los tags/variables en los archivos de test antes de ejecutar. Esta guía enumera comandos; no afirma que se hayan ejecutado durante esta actualización documental.

La evidencia fechada en `docs/evidence/` y resultados detallados en documentos de D3/D5/D8/D11 son históricos y deben citar commit/fecha. La prueba de resiliencia documentada observó 11 HTTP 200 y un 504 con una réplica de Catálogo detenida; el criterio de cero errores no se aprobó.

## Entorno remoto

Hay notas históricas en `README.md` previo, `PRUEBAS-RENDER-ACREDITACION.md`, `INTEGRACION-GIMNASIO.md` y `infra/nginx/default.render.conf` con URLs de Render de distintos momentos. Como no se consultó Render en esta tarea documental, no se certifica cuáles siguen activos ni qué commit sirven. Antes de una defensa, confirmar dashboard, rama/commit de cada servicio, env/secrets fuera de documentación, URL del Gateway y salud real. El Gateway remoto depende de que estén publicadas versiones compatibles de Auth, Catálogo, Reservas, Pagos y Beneficios; actualizar un backend sin sus rutas compartidas no publica el sistema completo.

Procedimiento de verificación posterior al despliegue (manual, requiere acceso al entorno): revisar commit configurado por servicio; consultar `/health`; probar por Gateway los endpoints públicos de lectura y autenticación; validar una sesión y un recorrido de reserva con datos de prueba acordados; guardar timestamp, commit, HTTP y respuesta no sensible. No adjuntar tokens, claves, emails reales ni datos personales a evidencias.
