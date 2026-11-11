# Servicio Pagos (simulador)

## Responsabilidad

Servicio interno servidor a servidor. No integra Mercado Pago ni otra pasarela: no hay cargos reales, SDK de proveedor ni webhooks. Registra de forma idempotente operaciones simuladas y ofrece lectura/verificación, reversa y devolución. La persistencia está en MongoDB `pagos_db`, colecciones `pagos_simulados` y `devoluciones_simuladas`.

## Operaciones internas

Todas las rutas requieren `X-Melt-Internal-Key`; no se publican por el Gateway para el navegador.

| Método/ruta interna | Uso |
|---|---|
| `GET /health` | Estado y modo del simulador (no incluye clave). |
| `POST /payments/simulate` | Registra/reutiliza pago del checkout con `Idempotency-Key`, `checkoutId`, `importe`, `moneda=ARS`. Resultado sale de `PAYMENT_SIMULATOR_RESULT`. |
| `GET /payments/{id}` | Consulta operación guardada. |
| `POST /payments/verify` | Comprueba pago, referencia, importe, moneda y aprobación. |
| `POST /payments/{id}/reverse` | Revierte aprobación simulada ante fallo compensatorio de Reservas. |
| `POST /payments/refunds` | Registra devolución simulada idempotente según política invocada por Reservas. |

El checkout de Reservas determina precios y seña; el navegador no envía importe ni resultado. Resultado válido: `approved` o `rejected` (Compose usa `approved` si no se define). Los estados internos de pago incluyen `approved`, `rejected`, `reversed`; la devolución es un registro aparte. Una respuesta rechazada no confirma turnos. La persistencia/idempotencia se apoya en índices únicos por clave y checkout.

Pagos no consume ni publica eventos RabbitMQ. Errores típicos: 400 solicitud inválida, 401 clave interna ausente/incorrecta, 404 operación inexistente, 409 reuso idempotente con datos distintos/operación incompatible y 503 error de persistencia. Los detalles por endpoint están en el OpenAPI del simulador y sus handlers.

## Configuración, salud y pruebas

Variables: `PORT` (8085), `MONGODB_URI`, `PAGOS_MONGODB_DATABASE`, `MELT_INTERNAL_KEY`, `PAYMENT_SIMULATOR_RESULT`.

```sh
cd services/pagos
go test ./...
```

`GET /health` devuelve `status`, `provider: simulador`, `mode`. No usar una salida `approved` como evidencia de cobro real.
