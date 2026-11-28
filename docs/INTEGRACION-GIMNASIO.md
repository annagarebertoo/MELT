# API de puntos del GYM en MELT

El GYM calcula cuántos puntos otorgar. MELT recibe esa cantidad, identifica al destinatario y guarda el movimiento en Beneficios. El GYM conserva la lógica de planes y membresías.

## Demostración con un socio ficticio

1. Iniciar sesión en el GYM con un socio de prueba, por ejemplo `ana.demo@example.test`. Para demostrar el alta, elegir un email que todavía no tenga cuenta en MELT.
2. Desde el **backend del GYM**, enviar la solicitud del ejemplo. La clave compartida se configura como secreto del servidor. Para cada operación nueva, generar una referencia única y guardarla antes del envío.
3. Con **HTTP 202**, leer `registroUrl` de la respuesta. En el perfil privado de ese socio, el GYM debe mostrar **Recibir mis puntos en MELT**, enlazado a la URL completa, incluido el fragmento después de `#`. Esto permite hacer la demo sin una casilla de correo. El GYM debe asociar el enlace al socio autenticado que originó la operación.
4. Abrir el enlace. MELT muestra los puntos pendientes y el email del destinatario. Registrarse con ese email o iniciar sesión con la cuenta correspondiente. Al confirmar la acreditación, Mi cuenta muestra **Recibiste 150 puntos del GYM** y el saldo posterior. El saldo y el movimiento se pueden consultar al volver a entrar.
5. Reenviar la misma solicitud. Después de acreditar, debe devolver **HTTP 200** y el saldo debe seguir igual. Para sumar puntos por una operación distinta, usar otra referencia.

### Solicitud de ejemplo

```http
POST /api/v1/beneficios/acreditaciones HTTP/1.1
Host: melt-gateway-prueba.onrender.com
Content-Type: application/json
X-Partner-Key: <clave privada del backend del GYM>

{"email":"ana.demo@example.test","puntos":150,"referencia":"gym-demo-0001"}
```

### Qué muestra el GYM según la respuesta

| HTTP | Qué ocurrió en MELT | Acción en el GYM |
|---|---|---|
| 201 | Se acreditaron los puntos al usuario existente. | Mostrar la cantidad acreditada. |
| 202 | La acreditación está pendiente de vinculación. | Mostrar al socio el botón con `registroUrl`. Los puntos quedan pendientes hasta confirmar. |
| 200 | La operación ya se acreditó. | Mostrarla como completada. Conservar la referencia original. |
| 409 | La referencia se reutilizó con otros datos o el destinatario está inactivo. | Revisar la operación antes de reintentar. |

## Responsabilidades y autenticación
Gimnasio determina la cantidad. MELT no consulta planes ni membresías. El backend consumidor llama `POST /beneficios/acreditaciones` (alias `/beneficios/acreditar-puntos`) con `Content-Type: application/json` y `X-Partner-Key: <clave compartida por privado>`. No incluir la clave en frontend, enlaces ni repositorios.

## Solicitudes
Por cuenta existente, usar el ID real que devuelve Auth:
```json
{"usuarioId":"ID_REAL_MELT","puntos":500,"referencia":"gym-operacion-001","descripcion":"Puntos otorgados por Gimnasio"}
```
Por email, incluso si todavía no existe una cuenta:
```json
{"email":"persona@example.com","puntos":500,"referencia":"gym-operacion-002","descripcion":"Puntos otorgados por Gimnasio"}
```
Enviar exactamente un destinatario (usuarioId o email). referencia y solicitudId son nombres alternativos: enviar uno solo. La referencia identifica globalmente la operación y debe conservarse para reintentos. Descripcion es opcional y admite hasta 500 caracteres. Puntos debe ser entero positivo (máximo 9007199254740991). Referencia y usuarioId admiten 1–128 caracteres sin espacios extremos. Los nombres de campos son sensibles a mayúsculas; PUNTOS no es válido.

## Respuestas
- 201: acreditación realizada. Devuelve solicitudId, usuarioId, puntos, saldoPuntos, tipo, creadoEn y estado.
- 200: la misma operación ya fue acreditada; devuelve el movimiento original sin sumar otra vez. saldoPuntos corresponde al saldo después de ESA operación.
- 202: pendiente. Devuelve solicitudId, estado pendiente, puntos, puntosAcreditados 0, registroUrl y venceEn.

Ejemplo ilustrativo de acreditación (los IDs y fechas dependen de la ejecución):
```json
{"solicitudId":"gym-operacion-001","usuarioId":"ID_REAL_MELT","puntos":500,"saldoPuntos":500,"tipo":"acreditacion_externa","estado":"acreditada","creadoEn":"2026-11-11T05:00:00Z"}
```

Cuando devuelve 202, Gimnasio entrega registroUrl privadamente al destinatario cuyo email verificó. MELT no envía el correo. La persona abre el enlace y se registra o inicia sesión con el mismo email; MELT confirma los puntos automáticamente. El enlace vence en siete días. Repetir la misma solicitud de partner renueva un enlace pendiente vencido sin perder los puntos. No publicar enlaces ni registrarlos en logs.

Una cuenta existente sin email verificado también recibe enlace antes de su primera acreditación por email. Por usuarioId, una cuenta activa recibe acreditación directa.

## Errores
| HTTP | Significado | Acción |
|---|---|---|
| 400 | Datos inválidos, propiedades desconocidas o puntos inválidos | Corregir el JSON |
| 401 | Clave ausente/incorrecta | Revisar X-Partner-Key |
| 404 | UsuarioId inexistente/inactivo | Obtener ID válido; por email se admite pendiente |
| 409 | Referencia ya usada con otros datos o cuenta email inactiva | No reutilizar referencias para otra operación |
| 415 | Content-Type incorrecto o ausente | Enviar application/json |
| 422 | Saldo fuera de rango | Revisar operación, no se acredita parcialmente |
| 503 | Dependencia/configuración no disponible o resultado incierto | Reintentar con la misma referencia y datos |

Un timeout no demuestra que la operación falló: conservar la referencia al reintentar. Los servicios gratuitos pueden tardar en arrancar tras inactividad; precalentar Auth, Beneficios y Gateway mediante sus /health antes de una demostración. Un fallo al primer intento puede requerir repetir después de que estén activos.

## Consulta e invitaciones
- `GET /beneficios/mis-puntos`: Bearer de un usuario con rol cliente. Devuelve saldo actual e historial de su cuenta. Ignora headers de identidad falsificados.
- `POST /beneficios/invitaciones/consultar`: JSON {"token":"TOKEN_DEL_ENLACE"}; sin sesión; permite mostrar el destinatario y puntos.
- `POST /beneficios/invitaciones/confirmar`: mismo JSON y Bearer del destinatario. 201 primera acreditación, 200 repetición, 403 destinatario distinto, 404 token inválido, 410 token pendiente vencido.
- `POST /auth/registro`: nombre, email, password y opcional invitacionToken. 201 cuenta creada; puede informar acreditacion pendiente_reintento si la entrega necesita recuperación.
- `POST /auth/login`: email y password; devuelve token para mis-puntos.
Las rutas /internal no son accesibles por Gateway.

## Ejemplo para backend
Ejecutar `python scripts/ejemplo-gimnasio.py` con las variables MELT_PARTNER_KEY, MELT_EMAIL, MELT_REFERENCIA y MELT_PUNTOS. Guardar la referencia en el sistema consumidor antes del primer envío. El ejemplo no imprime claves ni enlaces completos. El backend debe entregar registroUrl mediante su propio canal privado.

Contrato completo: [OpenAPI](contracts/acreditar-puntos-v1.openapi.yaml). Configuración técnica: [manual](ACREDITACION-PUNTOS.md).
