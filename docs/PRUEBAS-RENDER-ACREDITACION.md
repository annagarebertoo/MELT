# Verificación pública de acreditación — 11 de noviembre de 2026

> Captura histórica fechada, con rama/commit y URLs abajo. No certifica el estado actual de Render ni debe usarse como prueba de despliegue del HEAD de `develop` sin volver a consultar el servicio.

## Entorno verificado

- Gateway: https://melt-gateway-prueba.onrender.com (`srv-dbpvn4gu01pc739d6vhg`).
- Auth: https://melt-auth-prueba.onrender.com (`srv-dbpuo4jl550s7390t2u0`).
- Beneficios: https://melt-beneficios-prueba.onrender.com (`srv-dbpv1ruk1f9s73d3j61g`).
- Rama: `feature/api-acreditacion-puntos-externos`. Gateway desplegado desde `d26bc8bd9dcb34355fdfd40a4617b3c6257cd95d`; Auto-Deploy del Gateway desactivado.
- Bases configuradas en los servicios: `usuarios_prueba_db` y `beneficios_prueba_db`. Solo se crearon cuentas sintéticas con dominio `melt.test` y operaciones de prueba.
- Beneficios: `MELT_PUBLIC_URL=https://melt-gateway-prueba.onrender.com`. Se verificó que el enlace generado abre este Gateway.
- Los tres `/health` respondieron HTTP 200. Render mostró Beneficios live después de actualizar la URL pública.

## Resultados reales

Se ejecutaron `scripts/smoke-invitaciones.py` (49 comprobaciones HTTP, más aserciones de saldo y movimientos) y `scripts/smoke-render-acreditacion.py` (17 comprobaciones HTTP, más aserciones de saldo y movimiento único) contra el Gateway público. Las 66 comprobaciones pasaron.

También pasó la validación de `acreditar-puntos-v1.openapi.yaml` con `openapi-spec-validator` en Python 3.12, la comprobación de sintaxis de los dos scripts nuevos y `git diff --check`.

| Prueba | HTTP obtenido | Resultado real | Estado |
|---|---|---|---|
| Registro e inicio de sesión | 201 / 200 | Cuenta activa y token válido | Aprobada |
| Saldo inicial | 200 | 0 puntos | Aprobada |
| Acreditación por ID de 200 | 201 | Saldo 200, un movimiento | Aprobada |
| Repetición idéntica | 200 | Mismo movimiento, sin otros 200 | Aprobada |
| Misma referencia con otra cantidad | 409 | SOLICITUD_EN_CONFLICTO | Aprobada |
| Sin clave / clave incorrecta | 401 | PARTNER_NO_AUTORIZADO | Aprobada |
| ID inexistente | 404 | USUARIO_NO_EXISTE | Aprobada |
| Campo PUNTOS | 400 | DATOS_INVALIDOS | Aprobada |
| Content-Type text/plain o no JSON | 415 | CONTENT_TYPE_INVALIDO | Aprobada |
| Referencia de 129 caracteres | 400 | DATOS_INVALIDOS | Aprobada |
| Headers de identidad falsificados en Gateway | 401 | Acceso rechazado | Aprobada |
| Bearer inválido en Gateway | 401 | Acceso rechazado | Aprobada |
| Headers falsos directo a Beneficios /mis-puntos | 401 | TOKEN_REQUERIDO | Aprobada |
| Saldo final después de negativas | 200 | 200 puntos, un movimiento | Aprobada |
| Email sin cuenta, 100 / 500 / 137 puntos | 202 | Operación pendiente y enlace | Aprobada |
| Registro desde cada enlace | 201 | Email verificado y puntos vinculados | Aprobada |
| Confirmación repetida | 200 | No duplica acreditación | Aprobada |
| Acreditación adicional por email verificado | 201 | Saldos 107 / 507 / 144 | Aprobada |
| Cuenta previa con email sin verificar | 202 / 201 | Enlace; saldo 0 antes y 500 tras confirmar | Aprobada |
| Confirmación sin sesión | 401 | Rechazada | Aprobada |
| Rutas internas de Auth y Beneficios por Gateway | 404 | No expuestas | Aprobada |

Primera ejecución del nuevo script: una prueba apuntaba equivocadamente a `/api/v1/beneficios/mis-puntos` en el servicio directo y obtuvo 404. Se corrigió únicamente la ruta del script a `/mis-puntos`; la repetición completa pasó con 401. No fue necesario modificar código de Auth o Beneficios.

## Respuesta real de acreditación

HTTP 201, recibida el 11/11/2026 a las 05:30:55 UTC:

```json
{"solicitudId":"e2e-95b2ceae11138793","usuarioId":"6af3fd8cb37ddc085f584eb8","puntos":200,"saldoPuntos":200,"tipo":"acreditacion_externa","creadoEn":"2026-11-11T05:30:55.126Z","estado":"acreditada"}
```

Repetir exactamente la solicitud devolvió ese mismo cuerpo con HTTP 200. Cambiar puntos devolvió HTTP 409:

```json
{"error":{"code":"SOLICITUD_EN_CONFLICTO","message":"solicitudId ya se utilizó con otros datos."}}
```

Saldo inicial **0**, final **200 = 0 + 200**, incluso después de reintentos y solicitudes negativas.

## Prueba de navegador

En Chrome se abrió un enlace real generado para una cuenta nueva y 500 puntos. MELT mostró el destinatario y los puntos pendientes. Al completar el registro, mostró: **“Acreditación confirmada: 500 puntos. Tu saldo después de esa operación es 500.”** y **“Tu saldo de beneficios: 500 puntos”**. No se publican tokens, contraseñas ni enlaces privados en este informe.

## Alcance y límites

La API de acreditación está operativa para la entrega académica por HTTPS. Esto verifica persistencia, autenticación y comportamiento de idempotencia en el entorno público; no es una prueba de carga ni de fallos inducidos de MongoDB. Las pruebas de rollback/concurrencia locales existentes son complementarias.

Los servicios gratuitos pueden suspenderse por inactividad. Antes de una demostración, consultar los tres `/health` y esperar respuesta; ante timeout conservar la referencia para reintentar. El Gateway puede devolver errores de autenticación HTML, aunque los errores propios de acreditación son JSON.

El catálogo conserva su upstream anterior; esta tarea no desplegó Reservas ni certifica el flujo completo de turnos. No se modificaron servicios antiguos, no se hizo merge a develop/main y no se compartió la clave con terceros. El equipo debe entregar la clave de partner por un canal privado al backend del gimnasio.

## Repetir las pruebas

Configurar `BASE_URL` con el Gateway, `BENEFICIOS_PARTNER_KEY` desde el secreto de Render, `BENEFICIOS_URL` con la URL directa y `REPORT_PATH` con un archivo local fuera del repositorio. Ejecutar ambos scripts. Crean nuevas cuentas sintéticas; no ejecutar contra una base que contenga datos reales sin autorización. El informe JSON del segundo script excluye tokens y enlaces privados.
