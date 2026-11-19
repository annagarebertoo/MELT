# Integración GhostKitchen

La sección **Cocinas GhostKitchen cercanas** está disponible para cuentas con rol `cliente`. El frontend se comunica exclusivamente con Beneficios de MELT; la clave del proveedor no se envía al navegador.

## Configuración

Si todavía no existe, copiá `.env.example` a `.env`; completá `GHOSTKITCHEN_API_KEY` con la clave privada que entregó el Grupo 5. El ejemplo ya incluye la URL v1 oficial en `GHOSTKITCHEN_BASE_URL`. Compose pasa ambas variables al contenedor de Beneficios y el programa las lee al atender cada solicitud. Para levantar el entorno completo, configurá también los secretos obligatorios del proyecto (`AUTH_JWT_SECRET` y `MELT_INTERNAL_KEY`) según [Operaciones](OPERACIONES.md#requisitos-y-arranque-local). `.env` está excluido de Git; no pegues la clave en archivos versionados, logs ni en el frontend.

```powershell
if (!(Test-Path .env)) { Copy-Item .env.example .env }
# Editá .env y completá GHOSTKITCHEN_API_KEY con la clave recibida.
docker compose up --build -d beneficios gateway
```

La URL upstream es `https://57-156-56-219.sslip.io/public/v1`; corresponde al servidor `https://57-156-56-219.sslip.io/public` y las rutas `/v1/...` del [contrato OpenAPI del proveedor](https://github.com/Guille270106/GhostKitchen-AS/blob/main/docs/contracts/v1/openapi.yaml). `GHOSTKITCHEN_BASE_URL` permite cambiarla solo para un entorno controlado; para MELT debe conservarse la URL oficial. Si no se define, el backend usa esa misma URL por defecto.

El cliente HTTP usa timeout de 3 segundos por intento y realiza hasta 3 intentos únicamente para 429 y 503, respetando `Retry-After` en segundos o usando espera creciente (2 y 4 segundos) si falta el encabezado. Si la espera indicada excede el límite total de la solicitud, devuelve el último estado del proveedor sin reintentar fuera de plazo. Los demás estados, incluidos 400, 401 y 404, no se reintentan.

## Endpoints internos

Todos requieren un Bearer válido de un usuario con rol `cliente`. El navegador llama a MELT; Beneficios agrega `X-API-Key` y reenvía la solicitud al servicio real:

| MELT (Gateway) | GhostKitchen (upstream) |
|---|---|
| `GET /api/v1/beneficios/ghostkitchen/sedes` | `GET /v1/sedes` |
| `GET /api/v1/beneficios/ghostkitchen/sedes/cercana?lat={lat}&lng={lng}` | `GET /v1/sedes/cercana?lat={lat}&lng={lng}` |
| `GET /api/v1/beneficios/ghostkitchen/sedes/{sedeId}/platos` | `GET /v1/sedes/{sedeId}/platos` |
| `GET /api/v1/beneficios/ghostkitchen/platos/{platoId}` | `GET /v1/platos/{platoId}` |
| `POST /api/v1/beneficios/ghostkitchen/proximidad` | `POST /v1/proximidad` |

El listado de platos acepta `q`, `categoria`, `etiqueta`, `restauranteId`, `precioMax`, `soloDisponibles`, `orden`, `page` y `pageSize`. Beneficios reenvía solo estos parámetros permitidos.

Los errores del proveedor conservan su estado y código estable. `SIN_COBERTURA` se presenta como ausencia de una cocina dentro del radio de 25 km. Se conserva `correlationId` en la respuesta para facilitar el soporte.

## Interfaz

Iniciá MELT con Docker Compose y abrí el gateway local (el puerto publicado depende de tu configuración de `compose.yaml`). Iniciá sesión con una cuenta cliente, abrí **Cocinas GhostKitchen cercanas** en el panel y compartí la ubicación o ingresá latitud y longitud. La pantalla muestra sede más cercana, restaurantes y platos con filtros, orden, paginación y detalle. La disponibilidad se indica como orientativa; no crea reservas, pedidos ni pagos.

## Pruebas

Las pruebas automatizadas usan `httptest` de Go y no dependen del proveedor:

```sh
cd services/beneficios
go test ./...
```

Cubren el envío de `X-API-Key` leído desde el entorno, filtros y operaciones, respuestas exitosas, 401, 404/SIN_COBERTURA, 429, 503, reintentos acotados, timeout y falta de configuración. Los `httptest` son dobles de transporte aislados para las pruebas automatizadas; no son un servicio alternativo ni se usan al ejecutar MELT. En ejecución no hay fallback ni datos de reemplazo.

Para comprobar el acceso al proveedor desde tu máquina, con la variable cargada desde `.env`, ejecutá una consulta autenticada sin mostrar la clave:

```powershell
$envFile = Get-Content .env
$env:GHOSTKITCHEN_API_KEY = (($envFile | Where-Object { $_ -match '^GHOSTKITCHEN_API_KEY=' }) -split '=', 2)[1]
if ([string]::IsNullOrWhiteSpace($env:GHOSTKITCHEN_API_KEY)) { throw 'Falta GHOSTKITCHEN_API_KEY en .env' }
Invoke-WebRequest -Uri 'https://57-156-56-219.sslip.io/public/v1/sedes' -Headers @{ 'X-API-Key' = $env:GHOSTKITCHEN_API_KEY } | Select-Object -ExpandProperty StatusCode
Remove-Item Env:\GHOSTKITCHEN_API_KEY
```

Una respuesta `200` confirma autenticación y conectividad; sin clave válida el proveedor responde `401`. Para comprobar el recorrido desde MELT, levantá Compose y consultá uno de los endpoints internos con una sesión cliente.
