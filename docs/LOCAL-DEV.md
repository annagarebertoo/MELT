# Entorno local MELT

## Requisitos

- Docker Desktop con Docker Compose.
- Go 1.26.1 si se quiere ejecutar los servicios fuera de Docker.

## Arranque

Desde la raíz del repositorio:

```sh
docker compose config
docker compose up --build -d
docker compose ps
```

El primer arranque descarga imágenes y compila los servicios. Para detener el entorno sin borrar datos:

```sh
docker compose down
```

Para borrar también los datos persistidos de desarrollo (acción destructiva):

```sh
docker compose down -v
```

## URLs locales

| Componente | URL |
|---|---|
| API Gateway | http://localhost:8080 |
| Salud del gateway | http://localhost:8080/health |
| Listado público de tratamientos (contrato v1) | http://localhost:8080/api/v1/tratamientos |
| Detalle público de tratamiento (contrato v1) | http://localhost:8080/api/v1/tratamientos/{id} |
| Perfiles públicos de profesionales | http://localhost:8080/api/v1/profesionales |
| Acreditación externa de puntos | http://localhost:8080/api/v1/beneficios/acreditar-puntos |
| Listado mediante alias compatible | http://localhost:8080/api/v1/catalogo/tratamientos |
| Búsqueda | http://localhost:8080/api/v1/catalogo/tratamientos?q=facial |
| Filtro por categoría | http://localhost:8080/api/v1/catalogo/tratamientos?categoria=Facial |
| Paginación y ordenamiento | http://localhost:8080/api/v1/catalogo/tratamientos?page=1&limit=10&sort=precio&order=asc |
| RabbitMQ Management | http://localhost:15672 |
| OpenSearch | http://localhost:9200 |
| Prometheus | http://localhost:9090 |
| Grafana | http://localhost:3000 |

Credenciales de desarrollo por defecto: RabbitMQ `guest/guest`; Grafana `admin/admin_dev_only`. Cambiarlas antes de cualquier entorno compartido. OpenSearch tiene la seguridad deshabilitada solo para desarrollo local.

## Alta de un tratamiento desde Postman

La creación y edición de tratamientos funcionan a través de `POST /api/v1/catalogo/tratamientos` y `PUT /api/v1/catalogo/tratamientos/{id}`. Requieren `Authorization: Bearer <token-administrativo>`; el servicio Catálogo valida el rol con Auth por la red interna. La ruta contractual `/api/v1/tratamientos` es pública y de solo lectura.

- Método: `POST`
- URL: `http://localhost:8080/api/v1/catalogo/tratamientos`
- Header: `Content-Type: application/json`
- Header: `Authorization: Bearer <token-administrativo>`
- Body: `raw` → `JSON`

Ejemplo válido (cambiar el ID si ya existe; el campo es `duracionMinutos`, no `duracionMin`):

```json
{
  "id": "trat-002",
  "nombre": "Limpieza facial profunda",
  "descripcion": "Limpieza profunda e hidratación del rostro",
  "categoria": "Facial",
  "precio": 28000,
  "moneda": "ARS",
  "duracionMinutos": 60,
  "imagenes": [],
  "keywords": ["limpieza", "facial", "hidratacion", "rostro"]
}
```

Resultado esperado: `201 Created` y el tratamiento creado en el cuerpo de la respuesta. Si se repite un ID existente, la API responde `409 Conflict`. Los campos obligatorios incluyen `id`, `nombre`, `descripcion`, una `categoria` exactamente igual a `Facial`, `Corporal`, `Depilación` o `Manos y uñas`, `moneda: "ARS"`, un `precio` finito no negativo y `duracionMinutos` positivo. Si no se envían arreglos, el servicio inicializa `imagenes`, `keywords` y `profesionalesIds` como vacíos.

## Preparar y recorrer las reservas web

Los perfiles activos se consultan en `GET /api/v1/profesionales?tratamientoId={id}`. Para habilitar un tratamiento, asociá el ID de un profesional tanto en el perfil (`tratamientosIds`) como en el tratamiento (`profesionalesIds`) y configurá su agenda con `PUT /api/v1/reservas/agenda`. Las tres operaciones de Catálogo requieren un token administrativo. Reservas valida el ID, la duración, la compatibilidad y el horario antes de crear un bloqueo.

El flujo visual y los ejemplos de preparación de profesional, tratamiento y agenda están en [RESERVAS-WEB.md](RESERVAS-WEB.md). El simulador local funciona sin credenciales externas; configura opcionalmente `PAYMENT_SIMULATOR_RESULT=rejected` para probar un rechazo. El valor predeterminado es `approved`.

Al desplegar Catálogo fuera de Compose, definí `AUTH_URL` con la URL base interna o privada del servicio Auth, por ejemplo `https://melt-tygp.onrender.com`. En Compose, el servicio usa `http://auth:8084` como valor predeterminado. No configures la URL de Auth en el frontend.

## Escalar el catálogo

```sh
docker compose up --build -d --scale catalogo-profesionales=2 catalogo-profesionales gateway
docker compose ps
```

El gateway usa resolución DNS de Docker y un upstream dinámico de NGINX. La tolerancia a la caída de una instancia debe comprobarse con el procedimiento de [POSTMORTEM.md](POSTMORTEM.md); no se considera validada por tener la configuración escrita.

## Pruebas Go

```sh
cd services/auth && go test -count=1 ./... && cd ../..
cd services/catalogo-profesionales && go test -count=1 ./... && cd ../..
cd services/reservas && go test -p=1 -count=1 ./... && cd ../..
cd services/pagos && go test -count=1 ./... && cd ../..
node --check infra/nginx/web/app.js
docker compose config --quiet
```

Las pruebas de Reservas incluyen `TestReservasMongoIntegrationFlujoCompleto`, que usa MongoDB real para disponibilidad, solicitudes simultáneas, bloqueo temporal, seña, confirmación idempotente, devolución y liberación; Catálogo y Pagos se simulan en esa prueba. Para ejecutarla, con MongoDB local levantado:

```powershell
$env:RESERVAS_TEST_MONGODB_URI = 'mongodb://localhost:27017'
Push-Location services/reservas
go test -p=1 -count=1 -run TestReservasMongoIntegrationFlujoCompleto ./...
Pop-Location
Remove-Item Env:RESERVAS_TEST_MONGODB_URI
```

La cobertura automatizada no reemplaza una comprobación de pago sandbox con las credenciales del equipo ni una prueba de carga/producción.

## Alcance y limitaciones actuales

El catálogo y los perfiles profesionales persisten en MongoDB (`catalogo_db.tratamientos` y `catalogo_db.profesionales`). Reservas persiste agendas, turnos, bloqueos y carritos en MongoDB (`reservas_db`). La búsqueda actual es simple en MongoDB, con filtro, paginación y ordenamiento; OpenSearch no está integrado. El POST de administración de catálogo es una ruta privada compatible; la API pública contractual v1 expone lectura. La web local en `http://localhost:8080` permite explorar catálogo, registrarse, iniciar sesión, consultar perfil/puntos, revisar disponibilidad, crear el carrito, pagar la seña en modo test y cancelar reservas. RabbitMQ, OpenSearch, Memcached, PostgreSQL y Prometheus están declarados, pero sus integraciones de negocio siguen pendientes. También están pendientes la API externa de búsqueda de sedes cercanas del grupo de Cocina Fantasma, notificaciones por correo, canjes de puntos y pruebas de carga.


## Acreditación externa de puntos

Ver [ACREDITACION-PUNTOS.md](ACREDITACION-PUNTOS.md) para configuración,
contrato, pruebas y migración. Registro ordinario y login son independientes de Beneficios; el registro con invitación requiere validar el enlace.
El saldo e historial se consultan con `GET /api/v1/beneficios/mis-puntos` y Bearer.

## Autenticación y cuentas

Antes de iniciar Compose, configurá el secreto JWT fuera del repositorio. En PowerShell podés usar:

```powershell
$env:AUTH_JWT_SECRET = [Convert]::ToBase64String([Security.Cryptography.RandomNumberGenerator]::GetBytes(48))
docker compose config
docker compose up --build -d
docker compose ps
```

En Bash, podés generar una clave con `openssl rand -base64 48` y exportarla en `AUTH_JWT_SECRET`. No compartas ni subas la clave generada. Para crear el primer superadministrador, definí también `AUTH_ADMIN_EMAIL` y `AUTH_ADMIN_PASSWORD` en el entorno; la contraseña inicial debe tener al menos 12 caracteres.

El servicio `auth` se publica por el gateway en `http://localhost:8080/api/v1/auth`. Para levantarlo, definí una clave JWT de al menos 32 caracteres en `AUTH_JWT_SECRET`. No uses una clave real en archivos versionados. Si la cuenta de superadministrador ya existe, el arranque no modifica sus datos.

Endpoints iniciales:

- `POST /api/v1/auth/registro`: alta pública de cliente; nunca acepta roles enviados por el usuario.
- `POST /api/v1/auth/login`: inicia sesión y devuelve un token Bearer de corta duración.
- `GET /api/v1/auth/me`: devuelve la cuenta actual; requiere `Authorization: Bearer <token>`.
- `POST /api/v1/auth/usuarios`: crea usuarios internos con uno o más roles; requiere rol `administrador` o `superadministrador`.

Roles definidos: `cliente`, `profesional`, `recepcion`, `administrador` y `superadministrador`. El email es único y normalizado a minúsculas; las contraseñas se guardan con bcrypt. El token vence según `AUTH_TOKEN_TTL_MINUTES` (15 minutos por defecto). NGINX limita los intentos de login a 10 por minuto (con una ráfaga de 5) y los registros a 5 por minuto (con una ráfaga de 3) por IP.

Ejemplo de registro de cliente:

```sh
curl -X POST http://localhost:8080/api/v1/auth/registro \
  -H "Content-Type: application/json" \
  -d '{"nombre":"Cliente de prueba","email":"cliente@example.com","password":"una-clave-segura"}'
```

Ejemplo de inicio de sesión:

```sh
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"cliente@example.com","password":"una-clave-segura"}'
```

**Límites de esta primera versión:** el token es stateless, por lo que cerrar sesión en el frontend no revoca un token ya emitido; vence automáticamente. Todavía no hay recuperación de contraseña por email ni verificación de email. NGINX verifica la sesión para las rutas privadas de Reservas y Beneficios. En Reservas propaga el ID, email y roles devueltos por Auth y sobrescribe las cabeceras del cliente. Hacia Beneficios reenvía Authorization y elimina los headers `X-Auth-User-*`; Beneficios verifica además el Bearer directamente con Auth.

- `GET /api/v1/reservas/me`: endpoint de integración que devuelve la identidad verificada propagada por el gateway. Las operaciones de reservas aplican identidad, permisos por operación y comprobaciones de propiedad del recurso.
- `GET /api/v1/beneficios/mis-puntos`: requiere sesión y rol `cliente`; consulta el saldo y movimientos de Beneficios asociados al `usuarioId` verificado por Auth a partir del token Bearer. Si no hay registro, devuelve saldo cero.
- `POST /api/v1/beneficios/acreditar-puntos`: conserva la autenticación independiente mediante `X-Partner-Key` para el sistema externo del gimnasio.

La acreditación externa acumula saldo y persiste movimientos por ID de usuario de Auth. Canjes y reglas por tratamientos continúan pendientes. Solo el superadministrador puede asignar roles administrativos.

Para ejecutar la prueba de integración de login, cabeceras falsificadas, rutas privadas y permisos por rol en un entorno con Docker Compose levantado, usá `bash scripts/smoke-auth-gateway.sh`. GitHub Actions ejecuta esta misma prueba en el job `gateway-integration`.


### Política de permisos de Reservas

El gateway protege todo `/api/v1/reservas/`: exige un Bearer válido mediante `auth_request`, verifica que la cuenta siga activa y sobrescribe las cabeceras de identidad con los datos devueltos por Auth. El servicio Reservas vuelve a exigir identidad en sus rutas privadas con `requiereIdentidad`; no se debe confiar en cabeceras enviadas directamente por el cliente fuera del gateway.

El middleware `requierePermiso` aplica autorización por operación y deniega por defecto las operaciones que no tengan política:

| Operación | Roles permitidos |
|---|---|
| `reservas.consultar-disponibilidad` | cliente, profesional, recepción, administrador, superadministrador |
| `reservas.consultar-turnos` | cliente, profesional, recepción, administrador, superadministrador |
| `reservas.crear` | cliente |
| `reservas.cancelar-turno` | cliente, recepción, administrador, superadministrador |
| `reservas.gestionar-agenda` | profesional, recepción, administrador, superadministrador |
| `reservas.crear-bloqueo-carrito` | cliente |
| `reservas.confirmar-bloqueo-carrito` | cliente |
| `reservas.liberar-bloqueo-carrito` | cliente |
| `reservas.reintentar-devolucion` | cliente, recepción, administrador, superadministrador |

Cada ruta privada encadena `requiereIdentidad()` y `requierePermiso("clave.de.operacion")`; las políticas desconocidas se deniegan por defecto. La matriz define qué roles pueden invocar cada operación. Los handlers aplican además autorización por recurso: clientes consultan/cancelan sus turnos, profesionales consultan los propios, y solo recepción/administración ven o gestionan turnos de terceros. Los permisos no sustituyen esas comprobaciones de propiedad.


## Reservas: agenda, disponibilidad y turnos

El servicio Reservas persiste agendas semanales y turnos en MongoDB, en la base `reservas_db`. Compose configura `MONGODB_URI=mongodb://mongodb:27017` y espera que MongoDB esté saludable.

- `PUT /api/v1/reservas/agenda`: crea o actualiza el horario semanal. `diaSemana` usa 0=domingo y 6=sábado; `horaInicio` y `horaFin` usan `HH:MM`. Un profesional solo puede editar su propia agenda; recepción y administración pueden gestionar la agenda de un profesional indicado.
- `GET /api/v1/reservas/disponibilidad?profesionalId=ID&tratamientoId=ID&fecha=YYYY-MM-DD`: devuelve inicios cada 15 minutos dentro de la agenda del profesional, tomando duración del Catálogo. Rechaza profesionales que no estén asociados al tratamiento, turnos confirmados y bloqueos activos del carrito. La zona horaria local se configura con `MELT_TIMEZONE`; por omisión es `America/Argentina/Buenos_Aires`.
- `POST /api/v1/reservas/carrito/bloqueos`: JSON con `profesionalId`, `tratamientoId` e `inicio` RFC3339 con zona. Valida que el profesional sea compatible, que el horario futuro esté dentro de su agenda y que no se superponga; reserva los minutos con un índice único compartido por todas las réplicas. En la interfaz se envía `diferirPago: true`; el bloqueo vence a los 10 minutos.
- `POST /api/v1/reservas/carrito/checkout`: JSON `{"idempotencyKey":"..."}`. Reservas recalcula precios/beneficios, agrupa todos los bloqueos activos propios, calcula una sola seña del 30 %, solicita una simulación idempotente a Pagos y confirma todas las reservas en una transacción.
- `POST /api/v1/reservas/carrito/bloqueos/{id}/pago` y `POST /carrito/bloqueos/{id}/confirmar` se conservan como alias históricos y responden `409 CHECKOUT_UNICO_REQUERIDO`; ya no crean ni confirman pagos individuales.
- `DELETE /api/v1/reservas/carrito/bloqueos/{id}`: libera un bloqueo pendiente que pertenece al cliente autenticado.
- `POST /api/v1/reservas/turnos`: la creación directa está deshabilitada; devuelve `CARRITO_REQUERIDO`. Usá el bloqueo temporal y confirmalo después de pagar la seña.
- `GET /api/v1/reservas/turnos`: clientes ven sus turnos, profesionales sus turnos asignados y recepción/administración pueden consultar todos.
- `DELETE /api/v1/reservas/turnos/{id}`: el cliente solo cancela turnos propios; recepción/administración pueden cancelar otros. Desde 24 horas antes inclusive se devuelve el 100 % de la seña; con menos de 24 horas no hay devolución. Si el proveedor falla, la cancelación queda registrada y la devolución puede reintentarse en `POST /api/v1/reservas/turnos/{id}/devolucion/reintentar`.

Los endpoints privados quedan detrás de `auth_request` en el gateway y aplican identidad y permisos por operación en Reservas. La restricción única de profesional y minuto coordina bloqueos aun con varias réplicas; al cancelar se liberan los minutos ocupados. Catálogo y Pagos se invocan con timeout. La disponibilidad usa la duración real y profesionales habilitados que devuelve Catálogo. Pagos persiste simulaciones y devoluciones en `pagos_db`, una base separada; no necesita tokens ni llamadas externas.

### Pruebas de Reservas

Las pruebas unitarias se ejecutan con:

```sh
cd services/reservas
go test -p=1 -count=1 ./...
```

La prueba `TestReservasMongoIntegrationFlujoCompleto` recorre disponibilidad, dos solicitudes simultáneas, bloqueo, seña, confirmación idempotente, devolución y liberación contra MongoDB real, con Catálogo y Pagos simulados. Para ejecutarla, dejá MongoDB de Compose levantado y configurá la URI de prueba:

```powershell
$env:RESERVAS_TEST_MONGODB_URI = 'mongodb://localhost:27017'
go test -p=1 -count=1 -run TestReservasMongoIntegrationFlujoCompleto ./...
```

La prueba crea y elimina una base de datos de nombre único; no toca `reservas_db`.


### Flujo por email y registro seguro

La API v1.1 admite `email` en lugar de `usuarioId`, `referencia` en lugar de `solicitudId` y `descripcion` opcional, en `/api/v1/beneficios/acreditaciones` o `/acreditar-puntos`. Sin cuenta verificada devuelve 202 con un enlace de registro; al registrarse o iniciar sesión desde ese enlace se vincula y acredita una sola vez. El consumidor entrega el enlace privadamente al destinatario. Configurar `BENEFICIOS_INVITATION_SECRET` y `MELT_PUBLIC_URL` en Beneficios y `BENEFICIOS_URL` en Auth. No hay reglas de membresías ni canje manual de códigos. Ver [manual completo](ACREDITACION-PUNTOS.md) para contrato, seguridad, vencimiento, recuperación y pruebas.
