# Glosario MELT

| Término | Significado en MELT |
|---|---|
| Microservicio | Proceso con responsabilidad y datos propios (Auth, Catálogo, Reservas, Pagos o Beneficios). |
| API Gateway | NGINX: punto de entrada, sirve frontend y enruta HTTP; aplica verificación de sesión en ubicaciones configuradas. |
| JWT | Token firmado por Auth que acredita identidad y roles hasta su vencimiento. |
| Rol | Etiqueta autorizada por Auth (`cliente`, `profesional`, `recepcion`, `administrador`, `superadministrador`). |
| Perfil profesional | Registro público en Catálogo; puede vincularse a una cuenta Auth, cuyos IDs son distintos. |
| Agenda | Bloques semanales asociados al ID del perfil Catálogo, usados para calcular disponibilidad. |
| Slot | Intervalo de atención de duración igual a la duración del tratamiento. |
| Carrito | Conjunto temporal de horarios retenidos por un mismo cliente antes del checkout. |
| Retención / bloqueo | Reserva temporal de minutos de un profesional/categoría, expira a los diez minutos si no se confirma. |
| Checkout | Operación de compra agrupada que recalcula montos y procesa en conjunto los elementos del carrito. |
| Seña | Porción del total (30 %) que el simulador representa como pago inicial; no es dinero cobrado. |
| Simulador de pagos | Servicio Pagos que persiste resultados aprobados/rechazados configurables; no llama a una pasarela. |
| Idempotencia | Repetir una solicitud con la misma clave/referencia no duplica pago, checkout o acreditación. |
| Concurrencia | Solicitudes simultáneas que intentan reservar o cambiar el mismo recurso; Reservas combina índices únicos, transacciones y validación en backend. |
| Transacción | Unidad MongoDB multi-documento que confirma o revierte escrituras relacionadas. |
| Evento de dominio | Mensaje que comunica un hecho ocurrido; MELT usa `tratamiento.realizado` y `turno.cancelado`. |
| Outbox | Registro persistido junto al cambio de turno y publicado después a RabbitMQ, para no perder eventos al reiniciar el proceso. |
| RabbitMQ | Broker AMQP para distribuir eventos entre Reservas y Beneficios. |
| Consistencia eventual | Beneficios puede actualizar puntos después de que Reservas confirme el turno, al procesar el evento asíncrono. |
| OpenSearch | Índice de lectura para búsquedas textuales/filtros; MongoDB sigue siendo fuente canónica. |
| Memcached | Caché temporal de listados; expira y se invalida por generación, no es persistencia. |
| Invalidación por generación | Cambia un token común de versión para evitar servir claves de cache creadas antes de una modificación. |
| Circuit breaker | Patrón que corta temporalmente llamadas a una dependencia fallida. No afirmar que existe en MELT salvo evidencia de una implementación concreta. |
| Balanceador | Selecciona entre instancias. NGINX/Compose declaran upstream/replicas; el ensayo histórico observó un 504 y no probó failover transparente. |
| Seña y devolución | Pago/devolución simulada; cancelación se rige por reglas actuales en código (24 h), no por proveedor real. |
