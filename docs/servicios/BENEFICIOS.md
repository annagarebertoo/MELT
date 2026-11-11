# Servicio Beneficios

## Responsabilidad, datos y dependencias

Dueño de saldo, movimientos, reglas de puntos por tratamiento, recompensas, canjes, invitaciones de acreditación externa y registro de mensajes fallidos. MongoDB `beneficios_db` por defecto. Consulta Auth para sesión/identidad y cuenta; RabbitMQ recibe eventos de Reservas. Reenvía requests de GhostKitchen desde backend; la credencial no se expone al cliente.

## Rutas públicas

Prefijo Gateway `/api/v1/beneficios`; el Gateway requiere sesión para este prefijo salvo rutas allowlisted de partner/invitación. La ruta pública para el partner usa clave partner. El health del handler se encuentra tras ese prefijo protegido; para health anónimo usar el `GET /health` raíz del Gateway.

| Método/ruta | Acceso | Función |
|---|---|---|
| `GET /health` | Público | Verifica MongoDB. |
| `GET /mis-puntos` | cliente autenticado | Saldo e historial propios. |
| `GET /recompensas` | Público | Catálogo de recompensas. |
| `GET /mis-canjes`, `POST /canjes` | cliente | Consultar/canjear recompensas propias. |
| `POST /invitaciones/consultar` | Público con token | Inspecciona invitación. |
| `POST /invitaciones/confirmar` | Cliente autenticado destinatario | Confirma invitación y acreditación pendiente. |
| `POST /acreditar-puntos` y alias `POST /acreditaciones` | Partner con `X-Partner-Key` | Acreditación idempotente o pendiente por email. |
| `GET /admin/recompensas`, `POST`, `PUT /admin/recompensas/{id}` | Administrador/superadministrador | Administración de recompensas. |
| `GET /admin/reglas-tratamientos`, `PUT /admin/reglas-tratamientos/{id}` | Administrador/superadministrador | Regla de puntos para tratamiento realizado. |
| `GET /admin/mensajes-fallidos`, `POST /admin/mensajes-fallidos/{id}/reintentar` | Administrador/superadministrador | Revisión/reintento manual de mensaje fallido. |

Beneficios también contiene rutas protegidas de GhostKitchen: `GET /ghostkitchen/sedes`, `/sedes/cercana`, `/sedes/{sedeId}/platos`, `/platos/{platoId}` y `POST /ghostkitchen/proximidad`. Todas requieren cliente autenticado.

## Puntos, eventos y prevención de duplicados

Una compra/pago simulado no acredita puntos automáticamente. Al recibir `tratamiento.realizado`, Beneficios consulta regla activa y registra movimiento idempotente por `eventId`. Reservas no publica ese evento para `no_asistio`. Evento `turno.cancelado` permite restituir un canje asociado. RabbitMQ aplica reintentos limitados; después de cuatro intentos, mensaje se registra en fallidos y queda disponible para reintento administrativo. Los endpoints internos de canjes/invitaciones requieren clave interna y no son rutas públicas.

Beneficios consume mensajes, pero no publica eventos de dominio de Reservas. Las operaciones síncronas con Auth verifican token/usuario y las llamadas internas usan clave de servicio. Errores comunes: 400 validación, 401 sesión/partner inválido, 403 rol/destinatario no autorizado, 404 recurso ausente, 409 idempotencia/estado incompatible, 410 invitación vencida, 422 límite de saldo y 503 dependencia/configuración no disponible. No todos los endpoints comparten el mismo esquema de error; revisar el contrato específico.

La acreditación externa acepta solicitud con ID de usuario o email según contrato actual. Referencia/solicitud es clave de idempotencia. Si no existe cuenta activa/verificada para completar directamente, puede quedar pendiente y devolver URL de invitación; MELT no envía correo, el consumidor debe entregar el enlace de forma privada. No registrar ni compartir el token.

## GhostKitchen

El cliente de salida usa `X-API-Key`, timeout de 3 s y hasta tres intentos en respuesta 429/503, respetando `Retry-After` numérico o backoff. Base URL por defecto está en el código; `GHOSTKITCHEN_BASE_URL` permite un mock/endpoint alternativo. Si falta la key o el proveedor falla, la consulta no tiene fallback de datos ficticios.

## Configuración y pruebas

`MONGODB_URI`, `BENEFICIOS_MONGODB_DATABASE`, `AUTH_URL`, `MELT_INTERNAL_KEY`, `RABBITMQ_URL`, `BENEFICIOS_PARTNER_KEY`, `BENEFICIOS_INVITATION_SECRET`, `MELT_PUBLIC_URL`, `GHOSTKITCHEN_API_KEY`, `GHOSTKITCHEN_BASE_URL`.

```sh
cd services/beneficios
go test ./...
```

Pruebas de integración usan `BENEFICIOS_TEST_MONGODB_URI`; pruebas del proxy GhostKitchen usan HTTP mock. El comando no inicia servicios/dependencias por sí solo.
