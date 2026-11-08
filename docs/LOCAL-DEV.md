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


## Integración de registro y puntos del gimnasio

Esta integración ya no utiliza planes ni membresías. El gimnasio comparte un enlace hacia el formulario de MELT con el código de referido. El frontend conserva ese código y lo envía en el alta como codigoReferido.

Regla: cada cuenta nueva que se registra con el código correcto recibe 100 puntos (configurable). Los registros normales no reciben puntos. El regalo se acredita una sola vez por usuario y queda vinculado al ID de Auth. Una cuenta existente no recibe puntos retroactivos.

### Configuración

Copiá .env.example a .env y completá los valores locales. No subas .env al repositorio.

```powershell
Copy-Item .env.example .env
$env:AUTH_JWT_SECRET = [Convert]::ToBase64String([Security.Cryptography.RandomNumberGenerator]::GetBytes(48))
$env:CODIGO_GIMNASIO = "GIMNASIO_MELT_DEV"
$env:MELT_INTERNAL_KEY = [Convert]::ToBase64String([Security.Cryptography.RandomNumberGenerator]::GetBytes(48))
$env:PUNTOS_REGISTRO_GIMNASIO = "100"
docker compose config
docker compose up --build -d
```

También podés guardar CODIGO_GIMNASIO, MELT_INTERNAL_KEY y PUNTOS_REGISTRO_GIMNASIO en .env. La clave interna debe ser un secreto largo compartido únicamente por Auth y Beneficios. El servicio de Beneficios no expone su endpoint interno a través del gateway.

### Registro referido desde el frontend

El enlace que entrega el gimnasio puede tener esta forma (reemplazar dominio y código al desplegar):

https://melt.example/registro?referencia=GIMNASIO_MELT

El frontend debe leer referencia de la URL y enviar ese valor en el JSON del registro:

```json
{
  "nombre": "Cliente de prueba",
  "email": "cliente@example.com",
  "password": "una-clave-segura",
  "codigoReferido": "GIMNASIO_MELT"
}
```

Endpoint: POST http://localhost:8080/api/v1/auth/registro

Respuesta esperada: 201 Created. El campo beneficioRegistro indica acreditado, pendiente_reintento_en_login o no_aplica. Si Beneficios está temporalmente caído, Auth conserva el origen referido y vuelve a intentar la acreditación cuando la persona inicia sesión. Beneficios aplica una clave única por usuario para no duplicar el regalo.

### Consulta del saldo

Después del login, el frontend consulta GET http://localhost:8080/api/v1/beneficios/mis-puntos con Authorization: Bearer <token>. El saldo se busca por el ID de usuario verificado, no solo por email.

El gimnasio no debe llamar directamente a /internal/registro-gym ni recibir MELT_INTERNAL_KEY. Esa ruta es exclusivamente interna entre microservicios.

### Contrato y límites

El contrato de registro está documentado en docs/contracts/autenticacion-v1.openapi.yaml. La URL localhost es solo local: para compartir la API con el otro grupo hay que desplegar el gateway en HTTPS y comunicar una URL pública. No se deben compartir secretos JWT, claves internas ni credenciales de pago.

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

La acreditación del regalo del gimnasio queda vinculada al ID de usuario de Auth. La API de Beneficios aún no implementa un catálogo completo de movimientos ni canjes; el saldo inicial de este regalo sí queda persistido e idempotente. Solo el superadministrador puede asignar roles administrativos.

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


## Reservas: agenda y endpoints iniciales

El servicio Reservas persiste agendas semanales y turnos en MongoDB, en la base `reservas_db`. Compose configura `MONGODB_URI=mongodb://mongodb:27017` y espera que MongoDB esté saludable.

- `PUT /api/v1/reservas/agenda`: crea o actualiza el horario semanal. `diaSemana` usa 0=domingo y 6=sábado; `horaInicio` y `horaFin` usan `HH:MM`. Un profesional solo puede editar su propia agenda; recepción y administración pueden gestionar la agenda de un profesional indicado.
- `GET /api/v1/reservas/disponibilidad?profesionalId=ID&fecha=YYYY-MM-DD&duracionMinutos=60`: devuelve inicios disponibles cada 15 minutos dentro de la agenda. La duración debe estar entre 5 y 480 minutos.
- `POST /api/v1/reservas/turnos`: requiere rol cliente y JSON con `profesionalId`, `tratamientoId`, `inicio` (RFC3339 con zona horaria) y `duracionMinutos`.
- `GET /api/v1/reservas/turnos`: clientes ven sus turnos, profesionales sus turnos asignados y recepción/administración pueden consultar todos.
- `DELETE /api/v1/reservas/turnos/{id}`: el cliente solo cancela turnos propios; recepción/administración pueden cancelar otros turnos.

Todos los endpoints de Reservas quedan detrás de `auth_request` en el gateway y aplican middleware de identidad y permisos por operación. El backend verifica que el horario esté dentro de la agenda y que no se solape con turnos confirmados.

**Limitaciones de esta primera implementación:** la duración del tratamiento llega en la solicitud y todavía debe validarse contra Catálogo; no se implementaron pagos/seña, carrito con bloqueo de 10 minutos, reglas de devolución ni verificación de propiedad del profesional contra el catálogo de usuarios. La prevención de solapamientos usa un mutex dentro de una instancia del servicio; para despliegues con múltiples réplicas hace falta coordinación distribuida o una restricción transaccional en la persistencia. No usar este diseño como garantía de concurrencia multiinstancia en producción.
