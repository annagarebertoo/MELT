# Flujo de puntos, canjes y tratamientos realizados

## Puntos

`GET /beneficios/mis-puntos` devuelve el saldo y el historial del usuario autenticado. Las acreditaciones existentes continúan usando `POST /beneficios/acreditaciones` y su `solicitudId` idempotente.

La administración define puntos por tratamiento con `PUT /beneficios/admin/reglas-tratamientos/{tratamientoId}` y `{"puntos": 25}`. El valor cero desactiva la acreditación automática para ese tratamiento. Las reglas solo se consultan y modifican con sesión de administración.

## Confirmación profesional

El profesional asignado confirma la realización con `POST /reservas/turnos/{turnoId}/completar`. La operación solo admite turnos confirmados y verifica que el usuario autenticado sea el profesional asignado. Una segunda confirmación responde como repetida y no crea otro evento.

En la misma transacción de MongoDB, Reservas cambia el turno a `completado` y guarda un registro outbox con tipo `tratamiento.realizado`. El publicador entrega ese registro al exchange durable `melt.dominio` de RabbitMQ y lo marca publicado después de la confirmación del broker. Si se repite la publicación, Beneficios deduplica por `eventId` antes de acreditar.

Ejemplo de evento:

```json
{
  "eventId": "tratamiento-realizado:turno-123",
  "turnoId": "turno-123",
  "usuarioId": "cliente-456",
  "profesionalId": "profesional-789",
  "tratamientoId": "facial-01",
  "realizadoEn": "2026-11-11T12:00:00Z",
  "beneficioCanjeId": "canje-abc",
  "beneficioReservaId": "carrito-def"
}
```

El consumidor `melt.beneficios.eventos` consulta la regla para `tratamientoId` y acredita con una clave derivada del `eventId`. Así, una repetición del mensaje no duplica puntos. Una cancelación genera `turno.cancelado`; si se había aplicado un canje, el mismo consumidor restituye el beneficio sin devolver puntos del costo del canje.

## Reintentos y mensajes fallidos

Errores transitorios se reintentan tres veces, con esperas de 1, 3 y 8 segundos. Si el procesamiento sigue fallando, Beneficios conserva el evento y el motivo en `mensajes_fallidos` y lo envía a la cola durable `melt.beneficios.eventos.fallidos`.

Administración puede revisar `GET /beneficios/admin/mensajes-fallidos` y solicitar otro intento con `POST /beneficios/admin/mensajes-fallidos/{id}/reintentar`. Un fallo posterior vuelve a dejar el mensaje en estado pendiente para que siga visible.

## Recompensas y aplicación

Administración crea o actualiza recompensas con `POST /beneficios/admin/recompensas`. Los tipos admitidos son `descuento_porcentaje` y `tratamiento_gratis`. Los clientes consultan el catálogo en `GET /beneficios/recompensas`, canjean con `POST /beneficios/canjes` y consultan sus canjes con `GET /beneficios/mis-canjes`.

El costo de puntos y el registro del canje se confirman en una transacción MongoDB, con `solicitudId` único para idempotencia. El beneficio puede quedar disponible, reservado para una reserva, aplicado al confirmar el pago o restituido al cancelar el turno. Los bloqueos de canje vencen junto con la ventana de 10 minutos del carrito.

Reservas consume la API interna de Beneficios con `MELT_INTERNAL_KEY`; esa clave no se envía al navegador. La seña se calcula sobre el importe final después del descuento.

## Variables locales y pruebas

Compose configura `RABBITMQ_URL` para Reservas y Beneficios y espera a que RabbitMQ esté saludable. Reservas también recibe `BENEFICIOS_URL` y la clave interna `MELT_INTERNAL_KEY`.

Las pruebas unitarias se ejecutan con `go test ./...` dentro de `services/beneficios` y `services/reservas`. Las pruebas transaccionales de Mongo requieren un replica set aislado en `BENEFICIOS_TEST_MONGODB_URI` y `RESERVAS_TEST_MONGODB_URI`, respectivamente; si no se configuran, esas pruebas se omiten.
