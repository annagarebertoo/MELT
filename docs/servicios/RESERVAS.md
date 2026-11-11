# Servicio Reservas

## Responsabilidad y persistencia

Reservas es dueño de agenda, turnos, bloqueos por minuto, carritos, operaciones de checkout y outbox. Usa MongoDB `reservas_db` y replica set para transacciones. Consulta Catálogo para precio/duración/compatibilidad, Catálogo para resolver la cuenta Auth del profesional, Auth para verificar usuarios, Pagos para la simulación, Beneficios para aplicar canjes/restituciones y RabbitMQ para eventos posteriores.

## Endpoints del Gateway y autorización

Prefijo `/api/v1/reservas`; Gateway valida Bearer en todo ese prefijo y pasa identidad verificada en `X-Auth-User-*`; Reservas vuelve a validar que identidad/roles estén presentes y aplica permiso y propiedad en handlers. Por eso el `GET /health` del handler no equivale a salud anónima a través del prefijo protegido; usar el `GET /health` raíz del Gateway para health público.

| Método/ruta | Roles con permiso de operación | Uso |
|---|---|---|
| `GET /health` | Público | Salud básica del proceso. |
| `GET /me` | Cualquier sesión | Inspecciona identidad propagada. |
| `GET`, `PUT /agenda` | profesional, recepción, administrador, superadministrador | Consultar/guardar agenda. Si rol exclusivamente profesional, perfil se resuelve desde Auth y no se toma del ID enviado por cliente. |
| `GET /disponibilidad` | Todos los roles autenticados | Consulta slots por profesional, tratamiento y fecha. Para cliente requiere parámetros; duración/compatibilidad se resuelve con Catálogo. |
| `GET /turnos` | Todos los roles autenticados | Lista propia para cliente y del profesional vinculado; otros roles autorizados consultan según filtros. |
| `GET /carrito` | cliente | Recupera carrito y resumen del cliente. |
| `POST /carrito/bloqueos` | cliente | Retiene slot y lo agrega al carrito; 10 minutos. |
| `DELETE /carrito/bloqueos/{id}` | cliente | Quita item propio y libera minutos retenidos. |
| `POST /carrito/checkout` | cliente | Compra agrupada, calcula importe/seña en backend e idempotencia por cliente + clave. |
| `POST /carrito/bloqueos/{id}/pago`, `/confirmar` y `POST /turnos` | cliente | Rutas de compatibilidad/operación existentes; el flujo recomendado de compra es checkout del carrito. |
| `DELETE /turnos/{id}` | cliente, profesional, recepción, administrador, superadministrador | Cancela cuando dueño/regla y estado lo permiten. |
| `POST /turnos/{id}/devolucion/reintentar` | cliente, recepción, administrador, superadministrador | Reintento de devolución simulada. |
| `POST /turnos/{id}/completar` | profesional | Sólo perfil asignado; turno confirmado. Genera outbox de tratamiento realizado una vez. |
| `POST /turnos/{id}/no-asistio` | profesional | Sólo perfil asignado; registra inasistencia después de la hora final, sin puntos por realización. |

`reservas.consultar-disponibilidad`, `consultar-turnos` y `cancelar-turno` admiten todos los roles; esto no elimina los controles de propiedad/estado implementados por handler.

## Reglas de agenda y reserva

La agenda semanal contiene bloques del profesional. La disponibilidad calcula slots de longitud igual a `duracionMinutos` de Catálogo y revisa turnos confirmados y bloqueos vigentes. El horario admitido por la configuración del frontend/documentación local es 08:00–20:00; zona predeterminada `America/Argentina/Buenos_Aires`, configurable con `MELT_TIMEZONE`.

Los bloqueos guardan minutos, con índices únicos por profesional/minuto y por cliente/categoría/minuto. Se validan nuevamente dentro de operaciones transaccionales; checkout revisa vigencia, precio, asociación, duración, locks y beneficios antes de crear los turnos. Evita doble reserva del profesional y superposición de misma categoría de un cliente. Bloqueos/carritos usan TTL MongoDB de vencimiento; la expiración TTL es eventual, mientras las consultas filtran `expiraEn`.

Estados observados en el código: `confirmado`, `cancelado`, `completado`, `no_asistio`; las operaciones de checkout tienen estados propios como `processing`, `confirmed`, `rejected`, `reverted` y `approval_needs_recovery`. No confundir pago con estado del turno.

## Idempotencia y eventos

Checkout exige `idempotencyKey`; un índice único impide repetir combinación cliente+clave. Confirmación de turno realizado guarda actualización y `tratamiento.realizado` en outbox en transacción. Cancelación también registra `turno.cancelado`, que Beneficios usa para restituir canje si corresponde. El consumidor deduplica por `eventId`; mensajes se reintentan de forma limitada y pueden terminar en colección de fallidos.

Los errores usuales distinguen 400 (parámetros/cuerpo inválidos), 401 (sin identidad verificada), 403 (rol o propiedad), 404 (turno/perfil inexistente), 409 (estado, agenda, retención o conflicto), 422 (tratamiento/profesional incompatible) y 503 (dependencia no disponible). Algunas rutas antiguas de compatibilidad tienen cuerpos diferentes; el flujo actual de checkout está especificado en OpenAPI.

## Configuración y pruebas

`MONGODB_URI`, `CATALOGO_URL`, `AUTH_URL`, `PAGOS_URL`, `BENEFICIOS_URL`, `MELT_INTERNAL_KEY`, `RABBITMQ_URL`, `MELT_TIMEZONE`, `PORT`.

```sh
cd services/reservas
go test ./...
```

Las pruebas de integración requieren `RESERVAS_TEST_MONGODB_URI` y replica set aislado. No ejecutar pruebas de integración sobre base con datos de demostración compartidos.
