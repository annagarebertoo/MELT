# Servicio Pagos — Mercado Pago Checkout Pro (pruebas)

## Responsabilidad

Pagos es un servicio interno servidor a servidor. Crea preferencias de Checkout Pro usando únicamente credenciales de prueba, consulta el estado del pago en la API de Mercado Pago y envía devoluciones al entorno de prueba cuando la política de Reservas las solicita. La persistencia de referencias y estados está en MongoDB Atlas, base `pagos_db`; Reservas conserva los checkouts y turnos en PostgreSQL.

El comprador paga la seña total del carrito. Reservas calcula el importe; Pagos no acepta importes ni resultados definidos por el navegador. Una reserva sólo se confirma después de consultar a Mercado Pago y comprobar estado aprobado, referencia del checkout, importe y moneda. El frontend también verifica el resultado al volver, y Mercado Pago puede avisar por webhook firmado si el usuario no regresa.

## Rutas

Todas las rutas internas requieren `X-Melt-Internal-Key`, salvo el webhook firmado que Mercado Pago llama desde Internet. El Gateway publica únicamente este webhook y no expone las operaciones internas al navegador.

| Método/ruta | Uso |
|---|---|
| `GET /health` | Estado del servicio y proveedor habilitado. |
| `POST /payments/preferences` | Crea o recupera una preferencia idempotente y devuelve la URL de Checkout Pro. |
| `POST /payments/verify` | Consulta el pago al proveedor y valida `pagoId`, `referencia`, importe y moneda. |
| `POST /payments/refunds` | Solicita una devolución a Mercado Pago con clave idempotente. |
| `POST /webhooks/mercadopago` | Valida firma HMAC, vuelve a consultar el pago y notifica a Reservas. |

La preferencia lleva el checkout como `external_reference`; al regresar, MELT verifica el pago en servidor. `back_urls` apuntan al Gateway HTTPS compartido. Nunca se confía en el `status` ni en el importe recibido desde la URL de retorno.

## Configuración

Configurar en el servicio **melt-pagos** de Render:

- `MP_ENV=test`
- `MP_TEST_ACCESS_TOKEN`: copiar desde el selector **Prueba** de la aplicación Checkout Pro. En esta solución el prefijo de prueba también puede ser `APP_USR`; verifica el entorno en el panel, no sólo el prefijo.
- `MP_WEBHOOK_SECRET`: secreto de firma generado en la configuración de Webhooks de la aplicación de Mercado Pago.
- `MELT_PUBLIC_URL=https://melt-gateway-prueba.onrender.com`
- `MELT_INTERNAL_KEY`: el mismo secreto interno que usa Reservas.
- `MONGODB_URI` y `PAGOS_MONGODB_DATABASE=pagos_db`.

Pagos siempre redirige al `sandbox_init_point` y rechaza una operación cuyo `live_mode` sea `true`; nunca usa el enlace de producción. No enviar tokens ni secretos al navegador o al repositorio. La ruta de notificación configurada en Mercado Pago es `https://melt-gateway-prueba.onrender.com/api/v1/pagos/webhooks/mercadopago`, con el evento **Payments** en entorno de prueba.

Reservas necesita `PAGOS_URL` apuntando al servicio Pagos en Render, y Pagos notifica por el Gateway a `/api/v1/internal/reservas/pago-confirmado`. Esas dos variables deben corresponder a servicios del mismo despliegue.

En el entorno local, sin `MP_TEST_ACCESS_TOKEN`, Pagos informa `provider: deshabilitado` y no simula aprobaciones. Para probar Checkout Pro hay que usar un dominio HTTPS público y credenciales de prueba.
