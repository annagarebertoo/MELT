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
| Registro externo con puntos del gimnasio | http://localhost:8080/api/v1/beneficios/registro-externo |
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

La creación de tratamientos funciona en el servicio de Catálogo, a través de la ruta compatible `POST /api/v1/catalogo/tratamientos`. La ruta contractual `/api/v1/tratamientos` es pública y de solo lectura; el gateway bloquea POST en ese alias. No confundir el endpoint administrativo/local de carga con la API GET que consume otro grupo.

- Método: `POST`
- URL: `http://localhost:8080/api/v1/catalogo/tratamientos`
- Header: `Content-Type: application/json`
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

Resultado esperado: `201 Created` y el tratamiento creado en el cuerpo de la respuesta. Si se repite un ID existente, la API responde `409 Conflict`. Los campos obligatorios incluyen `id`, `nombre`, `descripcion`, una `categoria` exactamente igual a `Facial`, `Corporal`, `Depilación` o `Manos y uñas`, `moneda: "ARS"`, un `precio` finito no negativo y `duracionMinutos` positivo. Si no se envían `imagenes` o `keywords`, el servicio los inicializa como arreglos vacíos.

## Escalar el catálogo

```sh
docker compose up --build -d --scale catalogo-profesionales=2 catalogo-profesionales gateway
docker compose ps
```

El gateway usa resolución DNS de Docker y un upstream dinámico de NGINX. La tolerancia a la caída de una instancia debe comprobarse con el procedimiento de [POSTMORTEM.md](POSTMORTEM.md); no se considera validada por tener la configuración escrita.

## Pruebas Go

```sh
cd services/catalogo-profesionales
go test -v ./...
```

Los tests unitarios del catálogo cubren el endpoint de salud mediante una dependencia simulada, el parseo de paginación y funciones auxiliares. Todavía falta una prueba de integración contra MongoDB real y pruebas de los flujos completos de reservas y beneficios.

## Alcance y limitaciones actuales

El catálogo persiste tratamientos en MongoDB, ofrece búsqueda textual simple, filtro por categoría/precio máximo, paginación y ordenamiento. La creación mediante POST está implementada en la ruta compatible indicada arriba; la API pública contractual v1 expone solo lectura. RabbitMQ, OpenSearch, Memcached, PostgreSQL y Prometheus están declarados en Compose, pero todavía no están integrados en los flujos de negocio. Reservas y Beneficios continúan siendo esqueletos HTTP. El frontend, la integración de la identidad con Reservas y Beneficios, los flujos completos de negocio, los eventos de dominio, cache hit/miss, métricas de negocio, pruebas de carga y el contrato externo desplegado siguen pendientes. La autenticación inicial ya está implementada en esta rama; ver la sección siguiente.


## Registro externo del gimnasio y puntos de regalo

El endpoint vive en Beneficios, no en Catálogo. Para habilitarlo, configurá estas variables en el entorno antes de levantar Compose (no subas claves reales al repositorio):

- `CODIGO_GIMNASIO`: código de referido que debe venir en el body.
- `CLAVE_API_GIMNASIO`: clave privada que el backend del gimnasio envía en el header `X-Partner-Key`.
- `PUNTOS_POR_PLAN_MEMBRESIA`: JSON con la cantidad de puntos por combinación de plan y membresía. Ejemplo de formato, con nombres y valores ilustrativos que hay que reemplazar por los acordados con el Grupo 1:

```json
{"basico|mensual":100,"premium|anual":500}
```

Ejemplo de llamada:

```sh
curl -X POST http://localhost:8080/api/v1/beneficios/registro-externo \
  -H "Content-Type: application/json" \
  -H "X-Partner-Key: $CLAVE_API_GIMNASIO" \
  -d '{
    "solicitudId": "gym-req-0001",
    "nombre": "Cliente de prueba",
    "email": "cliente@example.com",
    "codigoReferido": "CODIGO_CONFIGURADO",
    "plan": "basico",
    "membresia": "mensual"
  }'
```

La primera solicitud devuelve `201`; repetir la misma `solicitudId` con el mismo email devuelve `200` sin sumar el regalo otra vez. Un email ya registrado con otra solicitud devuelve `409`. La ruta requiere configuración: si faltan las variables, devuelve `503` y no inventa puntos. Esta ruta de integración del gimnasio se autentica con `X-Partner-Key`, por lo que queda exceptuada de la sesión Bearer de clientes.

**Alcance importante:** este endpoint guarda el registro externo y el saldo inicial de puntos en la base de Beneficios. No está conectado todavía con el servicio `auth` para vincular la identidad de ese registro con una cuenta MELT. Antes de usarlo con el Grupo 1 hay que acordar los nombres reales de planes/membresías y las cantidades de puntos, configurar las credenciales por fuera del código y desplegar una URL accesible para ambos grupos.


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

**Límites de esta primera versión:** el token es stateless, por lo que cerrar sesión en el frontend no revoca un token ya emitido; vence automáticamente. Todavía no hay recuperación de contraseña por email ni verificación de email. NGINX verifica la sesión para las rutas privadas de Reservas y Beneficios y propaga el ID, email y roles devueltos por Auth; las cabeceras que manda el cliente se sobrescriben en esas rutas.

- `GET /api/v1/reservas/me`: endpoint de integración que devuelve la identidad verificada propagada por el gateway. El servicio Reservas aún no tiene operaciones de turnos; cada operación nueva deberá aplicar `requiereIdentidad` y roles por actor, además de comprobar propiedad del recurso.
- `GET /api/v1/beneficios/mis-puntos`: requiere sesión y rol `cliente`; consulta el saldo del registro de Beneficios asociado al email verificado. Si no hay registro, devuelve saldo cero.
- `POST /api/v1/beneficios/registro-externo`: conserva la autenticación independiente mediante `X-Partner-Key` para el sistema externo del gimnasio.

El registro externo del gimnasio todavía no tiene una vinculación persistente con el ID de cuenta MELT; la consulta inicial de puntos usa el email normalizado. Antes de producción hay que reemplazar esta vinculación transitoria por una asociación explícita de identidad. Solo el superadministrador puede asignar roles administrativos; las operaciones reales de turnos y el resto de operaciones de puntos/canjes siguen pendientes de implementación.

Para ejecutar la prueba de integración de login, cabeceras falsificadas, rutas privadas y permisos por rol en un entorno con Docker Compose levantado, usá `bash scripts/smoke-auth-gateway.sh`. GitHub Actions ejecuta esta misma prueba en el job `gateway-integration`.


### Política de permisos de Reservas

El gateway protege todo `/api/v1/reservas/`: exige un Bearer válido mediante `auth_request`, verifica que la cuenta siga activa y sobrescribe las cabeceras de identidad con los datos devueltos por Auth. El servicio Reservas vuelve a exigir identidad en sus rutas privadas con `requiereIdentidad`; no se debe confiar en cabeceras enviadas directamente por el cliente fuera del gateway.

El middleware `requierePermiso` aplica autorización por operación y deniega por defecto las operaciones que no tengan política:

| Operación | Roles permitidos |
|---|---|
| `reservas.consultar-propias` | cliente |
| `reservas.crear` | cliente |
| `reservas.cancelar-propia` | cliente |
| `reservas.consultar-agenda` | profesional, recepción, administrador, superadministrador |
| `reservas.gestionar-agenda` | profesional, recepción, administrador, superadministrador |
| `reservas.consultar-todas` | recepción, administrador, superadministrador |
| `reservas.gestionar-turnos` | recepción, administrador, superadministrador |

Cuando se implementen los handlers, encadenar ambos middlewares en este orden: `requiereIdentidad(), requierePermiso("clave.de.operacion")`. La autorización por rol no reemplaza la autorización por recurso: consultar, cancelar o modificar una reserva propia debe comparar el ID autenticado con el propietario de la reserva. Las rutas reales de turnos aún no están implementadas; por eso esta política y sus tests preparan el control de acceso, pero no implican que crear/cancelar reservas ya funcione.


## Reservas: agenda y endpoints iniciales

El servicio Reservas persiste agendas semanales y turnos en MongoDB, en la base `reservas_db`. Compose configura `MONGODB_URI=mongodb://mongodb:27017` y espera que MongoDB esté saludable.

- `PUT /api/v1/reservas/agenda`: crea o actualiza el horario semanal. `diaSemana` usa 0=domingo y 6=sábado; `horaInicio` y `horaFin` usan `HH:MM`. Un profesional solo puede editar su propia agenda; recepción y administración pueden gestionar la agenda de un profesional indicado.
- `GET /api/v1/reservas/disponibilidad?profesionalId=ID&fecha=YYYY-MM-DD&duracionMinutos=60`: devuelve inicios disponibles cada 15 minutos dentro de la agenda. La duración debe estar entre 5 y 480 minutos.
- `POST /api/v1/reservas/turnos`: requiere rol cliente y JSON con `profesionalId`, `tratamientoId`, `inicio` (RFC3339 con zona horaria) y `duracionMinutos`.
- `GET /api/v1/reservas/turnos`: clientes ven sus turnos, profesionales sus turnos asignados y recepción/administración pueden consultar todos.
- `DELETE /api/v1/reservas/turnos/{id}`: el cliente solo cancela turnos propios; recepción/administración pueden cancelar otros turnos.

Todos los endpoints de Reservas quedan detrás de `auth_request` en el gateway y aplican middleware de identidad y permisos por operación. El backend verifica que el horario esté dentro de la agenda y que no se solape con turnos confirmados.

**Limitaciones de esta primera implementación:** la duración del tratamiento llega en la solicitud y todavía debe validarse contra Catálogo; no se implementaron pagos/seña, carrito con bloqueo de 10 minutos, reglas de devolución ni verificación de propiedad del profesional contra el catálogo de usuarios. La prevención de solapamientos usa un mutex dentro de una instancia del servicio; para despliegues con múltiples réplicas hace falta coordinación distribuida o una restricción transaccional en la persistencia. No usar este diseño como garantía de concurrencia multiinstancia en producción.
