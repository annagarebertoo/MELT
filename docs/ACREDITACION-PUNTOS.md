# Acreditación externa de puntos

## Configuración

Generar secretos distintos para `AUTH_JWT_SECRET`, `MELT_INTERNAL_KEY` y
`BENEFICIOS_PARTNER_KEY` fuera del repositorio. La clave interna se comparte solo
entre Auth y Beneficios; la de partner solo con el servidor consumidor.
Beneficios necesita `AUTH_URL=http://auth:8084` en Compose (en Render, la URL de
Auth), `MONGODB_URI`, `MELT_INTERNAL_KEY` y `BENEFICIOS_PARTNER_KEY`.
Si falta la clave de partner, el POST falla cerrado con 503.

MongoDB debe soportar transacciones: replica set o Atlas. Compose inicia `rs0`
sobre el volumen existente, sin eliminar datos. El healthcheck espera un primario.
No ejecutar `docker compose down -v` sobre un entorno con datos que deban conservarse.
Para conexiones desde el host a este replica set local usar `directConnection=true`;
los servicios Docker resuelven `mongodb:27017` dentro de su red.

```powershell
$env:AUTH_JWT_SECRET = [Convert]::ToBase64String([Security.Cryptography.RandomNumberGenerator]::GetBytes(48))
$env:MELT_INTERNAL_KEY = [Convert]::ToBase64String([Security.Cryptography.RandomNumberGenerator]::GetBytes(48))
$env:BENEFICIOS_PARTNER_KEY = [Convert]::ToBase64String([Security.Cryptography.RandomNumberGenerator]::GetBytes(48))
docker compose up --build -d gateway
```

## Petición

Reemplazar `123` por el `usuario.id` devuelto por el registro de MELT.
El servidor consumidor envía la clave; nunca se incluye en el JavaScript público.
En Internet se requiere HTTPS.

```bash
curl -i -X POST 'http://localhost:8080/api/v1/beneficios/acreditar-puntos' \
  -H 'Content-Type: application/json' \
  -H "X-Partner-Key: $BENEFICIOS_PARTNER_KEY" \
  -d '{"solicitudId":"op-001","usuarioId":"123","puntos":200}'
```

201 devuelve `solicitudId`, `usuarioId`, `puntos`, `saldoPuntos`,
`tipo: acreditacion_externa`, `creadoEn`. Un reintento idéntico devuelve 200 con
el movimiento original; cambiar usuario o puntos devuelve 409. El ID es global,
se conserva sin TTL y debe reutilizarse después de un timeout o 503. Se exige `Content-Type: application/json` (admite `charset=utf-8`); otros tipos o la ausencia del header devuelven 415.
Los nombres de campos son exactos: `PUNTOS`, campos adicionales, claves duplicadas
y los puntos fraccionarios se rechazan con 400. Los identificadores admiten de 1 a 128
caracteres Unicode, sin espacios extremos; el límite del cuerpo completo es 4096 bytes. Cantidad y saldo se limitan a
9007199254740991 para evitar pérdida de precisión en clientes JavaScript.
En solicitudes nuevas, Auth caído devuelve 503 y usuario inexistente/inactivo devuelve 404.
Los reintentos confirmados se resuelven antes de consultar Auth, incluso si la cuenta fue desactivada. La verificación de existencia precede a la transacción: no garantiza
que una cuenta no sea desactivada inmediatamente después en otro servicio.

`GET /api/v1/beneficios/mis-puntos` mantiene su protección Bearer y rol cliente.
Beneficios reenvía el Bearer a `/verify` de Auth y utiliza solo el ID y los roles
confirmados por ese servicio. Ignora los `X-Auth-User-*` recibidos del cliente,
incluso si se accede directamente sin pasar por NGINX. Token ausente/inválido o
cuenta inactiva: 401; rol sin cliente: 403; Auth no disponible: 503.
Devuelve saldo e historial consistente para el `usuarioId` verificado por Auth.

## Persistencia y migración

Se conserva `beneficios_db.registros_externos`, sus documentos y saldos. Al iniciar,
se retira únicamente `uq_registro_externo_email`, que implementaba el regalo único.
El índice único disperso por usuario se conserva y se añade `movimientos_puntos`
con `_id=solicitudId` y un índice de historial por usuario/fecha. No se borran
colecciones ni se recalculan saldos históricos.

El movimiento nuevo y el incremento del saldo se confirman en una transacción
snapshot/majority. La clave única global evita duplicación entre usuarios;
conflictos concurrentes se reintentan y los fallos parciales revierten la operación.
La consulta de saldo e historial usa también una transacción snapshot.
Los documentos antiguos con `usuarioId` continúan disponibles; su regalo previo
aparece como movimiento histórico. Los documentos anteriores que solo tengan
email se conservan pero no se asocian automáticamente: requieren una migración
revisada de identidad, igual que antes de este cambio.

No mezclar versiones antiguas y nuevas de Auth/Beneficios: detener escritores
antiguos, respaldar datos e índices, desplegar ambos servicios y Gateway juntos.
El rollback requiere revisar el índice de email antes de restaurarlo, ya que las
cuentas nuevas pueden no tener email en Beneficios. El endpoint interno de Auth
solo devuelve 204/404, exige clave interna y está bloqueado en el gateway. En Render,
preferir servicios privados/red interna y configurar AUTH_URL de Beneficios con
la URL confiable de Auth (HTTPS si se usa una dirección pública). Aunque Beneficios
quede publicado, `/mis-puntos` exige un Bearer validado con Auth y
`/acreditar-puntos` exige la clave de partner. NGINX conserva su control de sesión,
reenvía Authorization y elimina los headers de identidad hacia Beneficios.
Los cambios de configuración se preparan en el repositorio; esta corrección no
modifica la configuración de servicios existentes en Render ni despliega cambios.

Se retiraron las rutas `/registro-externo` y `/internal/registro-gym` y la lógica de
referidos del registro/login. Su contrato anterior se conserva como histórico.
Canjes, puntos por tratamientos y mensajería no estaban implementados en esta base;
este cambio no agrega ni retira esas funciones. La API consumida por MELT no cambia.

## Pruebas

```powershell
$env:BENEFICIOS_TEST_MONGODB_URI = 'mongodb://localhost:27028/?directConnection=true&replicaSet=test-rs'
Push-Location services/beneficios
go test -race ./...
Pop-Location
$env:AUTH_TEST_MONGODB_URI = $env:BENEFICIOS_TEST_MONGODB_URI
Push-Location services/auth
go test -race ./...
Pop-Location
```

Usar exclusivamente un replica set de pruebas. La prueba de Beneficios crea una base
con nombre único y la elimina al terminar; la de Auth usa `melt_auth_integration_test`.
Sin esas variables las pruebas de integración se omiten explícitamente.
`scripts/smoke-auth-gateway.sh` valida registro/login, saldo inicial, acreditación,
repetición, conflicto, acumulación y bloqueo de rutas internas a través del gateway.
Se ejecuta con `BENEFICIOS_PARTNER_KEY` y credenciales de prueba en un entorno limpio.

Las pruebas de seguridad incluyen headers de identidad falsificados con y sin
Bearer, rol alterado, Auth caído y nombres de campos exactos. Para comprobar
además el acceso directo contra Auth real, ejecutar el smoke desde la red Docker
con `BENEFICIOS_DIRECT_URL=http://beneficios:8083` y `BASE_URL=http://gateway`.
Sin esa variable, el smoke comprueba las rutas del gateway; las pruebas Go de
middleware siguen verificando el rechazo de headers falsificados.
