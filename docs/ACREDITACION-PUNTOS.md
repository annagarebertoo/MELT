# Acreditación externa de puntos

Entrega pública del proyecto: [guía para Gimnasio](INTEGRACION-GIMNASIO.md).
Resultados reales del despliegue: [pruebas en Render](PRUEBAS-RENDER-ACREDITACION.md).

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
cuentas nuevas pueden no tener email en Beneficios. La consulta interna por ID de Auth devuelve 204/404; todas las rutas internas exigen clave interna y están bloqueadas en el gateway. En Render,
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

## Acreditaciones pendientes y enlace seguro (v1.1)

El consumidor decide los puntos; MELT no consulta planes ni membresías. Se mantiene
el contrato por `usuarioId`. Ambos endpoints `/acreditar-puntos` y `/acreditaciones`
admiten exactamente un destinatario (`usuarioId` o `email`) y exactamente una
referencia (`solicitudId` o `referencia`). `descripcion` es opcional, hasta 500
caracteres. No se ingresan códigos manualmente ni hay puntos fijos por registro.

Ejemplo servidor a servidor, con Content-Type application/json y X-Partner-Key:

```json
{"email":"persona@example.com","puntos":500,"referencia":"gym-operacion-001","descripcion":"Puntos externos"}
```

- Cuenta activa con email verificado: 201 y movimiento con saldo actualizado.
- Sin cuenta o con email todavía no verificado: 202 con `solicitudId`,
  `estado: pendiente`, `puntos: 500`, `puntosAcreditados: 0`, `registroUrl` y `venceEn`.
  No modifica ningún saldo hasta completar la vinculación.
- El consumidor entrega privadamente `registroUrl` al destinatario. MELT no envía
  correos. La seguridad depende de que el consumidor compruebe el email y entregue
  el enlace únicamente a su titular; no debe publicarlo ni registrarlo en logs.
- La web abre el formulario habitual, fija el email del destinatario y envía
  `invitacionToken` al registro. Auth valida el enlace antes de crear la cuenta.
  Después crea el usuario, vincula la operación y acredita automáticamente.
- Si ya tiene cuenta, inicia sesión desde el mismo enlace. La confirmación exige
  Bearer de esa cuenta, rol cliente y email coincidente. La posesión del enlace
  entregado por el canal confiable verifica ese email. No basta conocer el email.
- El registro ordinario sigue sin puntos automáticos y no depende de Beneficios.
  Login sigue funcionando sin Beneficios. Solo registrar con invitación requiere
  validarlo. Si la acreditación falla después de crear la cuenta, Auth conserva
  una entrega pendiente y reintenta cada cinco segundos, incluso tras reiniciar.

Una cuenta antigua sin email verificado requiere el enlace antes de recibir su
primera acreditación por email; esto evita otorgar puntos a alguien que haya
registrado una dirección ajena. Por `usuarioId` continúa la acreditación directa.

El token es impredecible, ligado al destinatario y operación, de un solo uso para
vincular, con vencimiento de siete días. Se guarda hash y nonce, no el token en
claro. La URL usa fragmento `/#invitacion=...`; la web lo retira de la dirección y
lo conserva en sessionStorage hasta confirmar o descartarlo. Un reintento del
mismo usuario ya acreditado es idempotente; otra cuenta no puede reclamarlo.

La referencia es global entre operaciones pendientes y acreditadas. Repetir los
mismos datos devuelve 202 pendiente o 200 acreditada sin duplicar; cambiarlos
devuelve 409. Token vencido: 410. La operación pendiente no se elimina por TTL:
el partner puede repetir exactamente la solicitud para renovar el enlace vencido.
Saldo e historial se actualizan juntos mediante transacción; ante desborde (422)
se conserva el pendiente sin incrementar el saldo.

### Configuración adicional

- Beneficios: `BENEFICIOS_INVITATION_SECRET`, secreto estable distinto de las otras
  claves, de al menos 32 caracteres; `MELT_PUBLIC_URL`, origen público de la web
  (HTTPS; se permite HTTP únicamente para localhost en desarrollo).
- Auth: `BENEFICIOS_URL=http://beneficios:8083` en Compose; en Render usar la URL
  confiable del servicio. Beneficios conserva `AUTH_URL=http://auth:8084`.
- Ambos comparten `MELT_INTERNAL_KEY` secreto. Auth sigue usando `AUTH_JWT_SECRET`;
  solo el consumidor conoce `BENEFICIOS_PARTNER_KEY`. No exponer estas variables
  en el frontend. Compose ya configura las URLs internas.
- Configurar el secreto de invitaciones antes de habilitar el flujo. Rotarlo
  invalida los enlaces pendientes; requiere un procedimiento coordinado de
  renovación. No cambiarlo arbitrariamente en cada arranque.
- NGINX local y Render permiten consulta de enlace y acreditación de partner sin
  sesión de usuario; confirmar exige sesión. Los prefijos internos devuelven 404.
  Auth verifica internamente invitaciones y Beneficios verifica las sesiones aun
  cuando se accede directamente; usar red privada entre servicios cuando exista.

```powershell
$env:BENEFICIOS_INVITATION_SECRET = [Convert]::ToBase64String([Security.Cryptography.RandomNumberGenerator]::GetBytes(48))
$env:MELT_PUBLIC_URL = 'http://localhost:8080'
```

### Verificación del nuevo flujo

Con un entorno y una base exclusivamente de prueba, ejecutar
`scripts/smoke-invitaciones.py` con `BASE_URL` y `BENEFICIOS_PARTNER_KEY`.
Prueba 100, 500 y 137 puntos, registro por enlace, cuenta existente, reintentos,
conflictos, saldo/historial y rutas internas. Las pruebas Go agregan concurrencia,
reinicios, recuperación de entrega, vencimiento, cambio de destinatario y rollback.
No usar el smoke sobre datos de producción: crea cuentas y movimientos reales.

## Pruebas Render en el clúster existente

En servicios separados de prueba, configurar `AUTH_MONGODB_DATABASE=usuarios_prueba_db` en Auth y `BENEFICIOS_MONGODB_DATABASE=beneficios_prueba_db` en Beneficios. Ambos pueden usar el MongoDB existente, siempre que el usuario de conexión tenga permisos sobre estas bases. No cambiar los servicios existentes. Los valores predeterminados siguen siendo `usuarios_db` y `beneficios_db`. No basta cambiar el nombre dentro de MONGODB_URI. Las pruebas comparten recursos del clúster, pero no las colecciones del equipo.
