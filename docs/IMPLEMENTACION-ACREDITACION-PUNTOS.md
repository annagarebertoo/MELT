# Implementación de acreditación externa de puntos

## Ampliación actual: pendientes y enlace seguro

La ampliación sin commit en `feature/api-acreditacion-puntos-externos` incorpora
email, referencia y descripción; pendientes persistentes y enlace con token de
siete días; registro y login desde el enlace; verificación del destinatario;
idempotencia global y recuperación durable de acreditaciones después del registro.
El consumidor entrega privadamente el enlace. MELT no envía correos ni calcula puntos.
El registro ordinario y login siguen sin regalar puntos.

Se modificaron Auth, Beneficios, frontend y configuración de Gateway/Compose,
contratos, documentación y CI; se añadieron pruebas Go y el smoke de invitaciones.
No se modificaron los servicios de Catálogo, Reservas, Pagos ni la API externa consumida.
Verificación local de la ampliación (base aislada, sin Render):
- Auth y Beneficios: `go test -race -count=1 ./...` con MongoDB de prueba: aprobado.
- Smoke real por Gateway: 49 comprobaciones de invitaciones aprobadas con cantidades
  100, 500 y 137, más regresión de Auth/Gateway y seguridad directa de Beneficios.
- Web: registro real desde enlace y saldo visible de 500 puntos, sin ingreso manual de código.
- Contratos Auth y acreditación: validados con openapi-spec-validator.
- NGINX local y Render: sintaxis válida; JavaScript y git diff sin errores.

La descripción siguiente corresponde a la entrega anterior, antes de esta ampliación.
Ver [manual vigente](ACREDITACION-PUNTOS.md).

## Rama y alcance

Rama: `feature/api-acreditacion-puntos-externos`.
Origen: `origin/develop`, commit `a3d3ae04b0979a150cd0a9b2761b47bdb5ae0f92`.
El repositorio estaba limpio antes de crear la rama. La implementación y sus
correcciones se realizaron sin commits intermedios, push, merges ni rebases.
La entrega autorizada se agrupa en el commit `feat: implementar API de acreditación de puntos externos`.
Las referencias posteriores al estado sin commit corresponden al cierre histórico de cada etapa, anterior a esa entrega.

## Comportamiento

POST `/api/v1/beneficios/acreditar-puntos`, con `X-Partner-Key`, recibe
`solicitudId`, `usuarioId` y `puntos`. Una nueva operación verifica usuario activo
con Auth, incrementa el saldo conservado y persiste el movimiento en una transacción.
La misma solicitud devuelve 200 sin duplicación, aun si Auth está caído; otro
usuario o cantidad para ese ID devuelve 409 antes de consultar Auth.
Las operaciones nuevas devuelven 201. Se validan campos, enteros positivos,
límites y formato JSON. Errores documentados: 400, 401, 404, 409, 415, 422 y 503.

Se retiraron regalo fijo de registro, cálculos de planes/membresías, validación
de código, acreditación automática y reintentos durante login, campos de referido
en Auth/frontend, rutas antiguas y configuración asociada. El contrato antiguo
se conserva explícitamente como histórico retirado. La integración consumida
por MELT y los servicios ajenos no se modificaron.

Se conservan saldos y consulta mis-puntos, con historial de movimientos nuevos
y del regalo histórico vinculado por ID. Canjes y reglas por tratamientos no
estaban implementados en develop; siguen pendientes, sin retirar código ajeno.

## Validación ejecutada

- `go test ./...` de Auth y Beneficios con MongoDB de pruebas: aprobado.
- `go test -race ./...` de ambos servicios con MongoDB replica set: aprobado.
- Pruebas de Beneficios: validación, autorización, usuario inexistente, acumulación,
  reintento idéntico, conflicto por usuario/cantidad, Auth caído, persistencia desde
  otra conexión, índice histórico, carreras sobre cuentas existentes y nuevas,
  rollback luego de insertar un movimiento y consulta de saldo/historial: aprobadas.
- Auth: registro, login, token, /me y consulta interna autenticada: aprobados.
- `scripts/smoke-auth-gateway.sh` en Compose aislado: aprobado; incluye registro,
  login, permisos, saldo inicial cero, acreditación, repetición, conflicto,
  acumulación y bloqueo de rutas internas.
- Compilación de los servicios al levantar Compose: aprobada.
- `nginx -t` para configuración local y Render: aprobado.
- YAML de contratos y workflow de CI: parseado correctamente (5 archivos).
- `node --check infra/nginx/web/app.js` y `git diff --check`: aprobados.

El workflow incorpora las pruebas transaccionales con replica set para futuras
 ejecuciones en GitHub; no se disparó CI remoto porque no se hizo push.
No se modificó ni desplegó el entorno publicado de Render.

## Configuración y límites pendientes de despliegue

Configurar secretos distintos para partner e integración interna, AUTH_URL en
Beneficios y un MongoDB con replica set/Atlas. Desplegar Auth, Beneficios y Gateway
juntos; no mantener escritores de la versión anterior. Se retira el índice único
de email del regalo, sin borrar documentos. Los registros históricos que solo
tienen email requieren asociación de identidad revisada por separado.
Beneficios valida el Bearer con Auth incluso en accesos directos y obtiene de
allí el usuario y sus roles. NGINX reenvía Authorization y elimina headers de identidad.
Se recomienda red privada en Render y AUTH_URL confiable/HTTPS; no se cambió ni
se desplegó infraestructura real. El contrato exige nombres exactos, application/json
(415 en caso contrario) y entre 1 y 128 caracteres Unicode por identificador.
La existencia de cuenta se verifica antes de la transacción de Beneficios; no
hay una transacción distribuida que impida una desactivación simultánea en Auth.

Petición completa, configuración y migración: [ACREDITACION-PUNTOS.md](ACREDITACION-PUNTOS.md).
Contrato: [acreditar-puntos-v1.openapi.yaml](contracts/acreditar-puntos-v1.openapi.yaml).

## Archivos modificados

- `.env.example`
- `.github/workflows/go.yml`
- `README.md`
- `compose.yaml`
- `docs/ARCHITECTURE.md`
- `docs/LOCAL-DEV.md`
- `docs/contracts/README.md`
- `docs/contracts/autenticacion-v1.openapi.yaml`
- `docs/contracts/registro-externo-v1.openapi.yaml`
- `infra/nginx/default.conf`
- `infra/nginx/default.render.conf`
- `infra/nginx/web/app.js`
- `infra/nginx/web/index.html`
- `scripts/smoke-auth-gateway.sh`
- `services/auth/cmd/api/integration_test.go`
- `services/auth/cmd/api/main.go`
- `services/auth/cmd/api/main_test.go`
- `services/beneficios/cmd/api/main.go`
- `services/beneficios/cmd/api/main_test.go`

## Archivos creados

- `docs/ACREDITACION-PUNTOS.md`
- `docs/contracts/acreditar-puntos-v1.openapi.yaml`
- `services/beneficios/cmd/api/validacion.go`
- `services/beneficios/cmd/api/seguridad_test.go`
- `services/beneficios/cmd/api/identidad.go`
- `services/beneficios/cmd/api/integration_test.go`
- `docs/IMPLEMENTACION-ACREDITACION-PUNTOS.md` (este informe).

## Archivos eliminados

Ninguno.

## Correcciones de la auditoría — 10/11/2026

Beneficios verifica el Bearer con Auth también en accesos directos y obtiene la
identidad y los roles de la respuesta de Auth. No confía en X-Auth-User-* del cliente.
NGINX local y Render reenvían Authorization y eliminan los headers de identidad
hacia Beneficios. No se modificó la infraestructura real ni se desplegó nada.

El POST exige application/json (415 en caso contrario), nombres exactos (PUNTOS
no sustituye puntos), rechaza claves duplicadas y limita los identificadores a
128 caracteres Unicode, igual que maxLength en OpenAPI. La guía local describe
la consulta por usuarioId verificado con Auth.

Pruebas de esta corrección: Auth y Beneficios con go test -race -count=1 y MongoDB
real, rollback/transacciones/persistencia/concurrencia, smoke con acceso directo
contra Auth real, suplantación de ID/roles, tokens inválidos, media type, campos
exactos y límites Unicode: aprobadas. Un Auth detenido devuelve 503 para mis-puntos;
un replay de acreditación ya confirmado sigue devolviendo 200. OpenAPI de los cuatro
contratos, YAML de CI y nginx -t local/Render: aprobados.

La comparación SHA-256 antes/después confirmó que no se tocaron los archivos de
Auth, Reservas, Catálogo, Pagos ni la integración consumida de otro grupo durante
esta corrección. Se conserva la misma rama y HEAD; sin commits, push ni merges.

Archivos de esta corrección (respecto del estado al iniciar el pedido):

- `.github/workflows/go.yml`
- `docs/ACREDITACION-PUNTOS.md`
- `docs/ARCHITECTURE.md`
- `docs/IMPLEMENTACION-ACREDITACION-PUNTOS.md`
- `docs/LOCAL-DEV.md`
- `docs/contracts/README.md`
- `docs/contracts/acreditar-puntos-v1.openapi.yaml`
- `infra/nginx/default.conf`
- `infra/nginx/default.render.conf`
- `scripts/smoke-auth-gateway.sh`
- `services/beneficios/cmd/api/identidad.go`
- `services/beneficios/cmd/api/main.go`
- `services/beneficios/cmd/api/main_test.go`
- `services/beneficios/cmd/api/seguridad_test.go`
- `services/beneficios/cmd/api/validacion.go`
