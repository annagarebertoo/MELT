# Integración GhostKitchen

La sección **Cocinas GhostKitchen cercanas** está disponible para cuentas con rol `cliente`. El frontend se comunica exclusivamente con Beneficios de MELT; la clave del proveedor no se envía al navegador.

## Configuración

Definí `GHOSTKITCHEN_API_KEY` en el entorno local antes de levantar los servicios. Compose la pasa al contenedor de Beneficios y el programa la lee al atender una solicitud. No la escribas en archivos versionados ni en el frontend.

```powershell
$env:GHOSTKITCHEN_API_KEY = '<clave entregada por GhostKitchen>'
docker compose up --build -d beneficios gateway
```

La URL upstream predeterminada es `https://57-156-56-219.sslip.io/public/v1`. Para apuntar a un mock compatible se puede definir `GHOSTKITCHEN_BASE_URL`; nunca se usa una clave real en pruebas. El cliente HTTP usa timeout de 3 segundos por intento y realiza hasta 3 intentos únicamente para 429 y 503, esperando `Retry-After` en segundos o usando una espera creciente si falta el encabezado.

## Endpoints internos

Todos requieren un Bearer válido de un usuario con rol `cliente` y responden a través del gateway `/api/v1/beneficios/`:

- `GET /api/v1/beneficios/ghostkitchen/sedes`
- `GET /api/v1/beneficios/ghostkitchen/sedes/cercana?lat={lat}&lng={lng}`
- `GET /api/v1/beneficios/ghostkitchen/sedes/{sedeId}/platos`
- `GET /api/v1/beneficios/ghostkitchen/platos/{platoId}`
- `POST /api/v1/beneficios/ghostkitchen/proximidad`

El listado de platos acepta `q`, `categoria`, `etiqueta`, `restauranteId`, `precioMax`, `soloDisponibles`, `orden`, `page` y `pageSize`. Beneficios reenvía solo estos parámetros permitidos.

Los errores del proveedor conservan su estado y código estable. `SIN_COBERTURA` se presenta como ausencia de una cocina dentro del radio de 25 km. Se conserva `correlationId` en la respuesta para facilitar el soporte.

Si GhostKitchen no responde, Beneficios devuelve el error del proveedor y la interfaz informa que no pudo cargar la búsqueda y permite volver a intentar. No se crean ni confirman reservas y no se alteran puntos: esta capacidad consulta sedes y platos; el POST de proximidad también es una comparación sin persistencia. La disponibilidad de los platos no reserva ni compra nada.

## Interfaz

Iniciá MELT con Docker Compose y abrí el gateway local (el puerto publicado depende de tu configuración de `compose.yaml`). Iniciá sesión con una cuenta cliente, abrí **Cocinas GhostKitchen cercanas** en el panel y compartí la ubicación o ingresá latitud y longitud. La pantalla muestra sede más cercana, restaurantes y platos con filtros, orden, paginación y detalle. La disponibilidad se indica como orientativa; no crea reservas, pedidos ni pagos.

## Pruebas

Las pruebas unitarias y de integración locales usan `httptest` de Go y no dependen de la nube:

```sh
cd services/beneficios
go test ./...
```

Cubren el envío de `X-API-Key`, filtros y operaciones, respuestas exitosas, 401, 404/SIN_COBERTURA, 429, 503, reintentos acotados, timeout y falta de configuración. La prueba `TestGhostKitchen503NoModificaSaldoNiMovimientos` simula tres respuestas 503, comprueba que Beneficios informa la indisponibilidad y que el saldo y los movimientos quedan iguales.

La prueba contra el contrato publicado se ejecuta en el workflow [GhostKitchen provider contract](../.github/workflows/ghostkitchen-contract.yml). Inicia Prism con el [OpenAPI oficial v1.1.0 versionado](contracts/ghostkitchen-v1.1.0.openapi.yaml), compara la copia con la [especificación publicada por el Grupo 3](https://github.com/Guille270106/GhostKitchen-AS/blob/5946412f3787f53fea840294a34ac679e1b8e062/docs/contracts/v1/openapi.yaml) y llama las cinco operaciones desde Beneficios. No necesita la clave real y no llama a producción.

Para ver datos reales en la interfaz se necesita una API Key válida. Para pruebas de UI se puede indicar un mock compatible mediante `GHOSTKITCHEN_BASE_URL`.
