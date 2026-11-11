# Resolución de problemas frecuentes

Antes de cambiar archivos, revisar `docker compose ps`, `/health` del Gateway, logs acotados del servicio y request ID (`X-Request-ID`). No imprimir Authorization, claves ni cuerpos con datos personales.

| Síntoma | Diagnóstico | Resolución segura |
|---|---|---|
| Gateway no carga / conexión rechazada | `docker compose ps`; puerto 8080 ocupado; `docker compose logs gateway` | Liberar/configurar otro puerto local sólo en un override local; validar Compose y levantar Gateway. No afectar otro proyecto Compose. |
| Servicio reinicia/no está healthy | `docker compose logs --tail 100 <servicio>`; leer health de servicio y dependencias | Corregir variable o dependencia reportada; `docker compose up --build -d <servicio>` sin tocar volúmenes. |
| 401 | Token ausente, expirado o inválido; revisar login y `/me`, hora/secret del entorno | Iniciar sesión nuevamente; comprobar que todos los servicios del mismo entorno comparten valores compatibles. Nunca copiar JWT al informe. |
| 403 | Rol no habilitado o propiedad del recurso no coincide | Usar cuenta con rol pertinente; verificar vinculación profesional y dueño del turno. No cambiar rol para saltar autorización. |
| 404 | Prefijo/ruta Gateway incorrecto o ID inexistente/inactivo | Contrastar ruta con manual/OpenAPI y `default.conf`; rutas `/internal` se ocultan adrede detrás del Gateway. |
| 500/503 | Error de servicio o dependencia; request ID y logs de Gateway/servicio | Revisar primero salud MongoDB, OpenSearch/Memcached/RabbitMQ según flujo. No reinicializar o borrar base. |
| Búsqueda textual no devuelve datos / 503 | índice no actualizado o OpenSearch no disponible | revisar `docker compose ps opensearch catalogo-profesionales`, logs y métricas de índice; esperar indexación/reconstrucción. No declarar éxito basándose sólo en lectura sin `q`. |
| Catálogo tarda pero responde | Memcached no disponible, generando misses | verificar Memcached; el cache es acelerador, no persistencia. Alta carga no se resuelve borrando Mongo. |
| Horarios vacíos | falta asociación tratamiento-profesional, perfil inactivo, agenda sin bloque, fecha pasada o conflicto | confirmar perfil activo y relación, llamar disponibilidad con IDs opacos correctos y revisar agenda en zona configurada. No crear agenda/reserva sin autorización. |
| Retención vencida | transcurrieron 10 minutos / expiró `expiraEn` | actualizar disponibilidad y agregar nuevamente el horario. El índice TTL puede limpiar físicamente después; lectura descarta vencidos. |
| Checkout 409 | retención vencida, cambió precio/duración/asociación o conflicto concurrente | actualizar carrito y reintentar con horarios vigentes; no forzar confirmación parcial. |
| Pago simulado rechazado | `PAYMENT_SIMULATOR_RESULT=rejected` | aceptar resultado de prueba; turnos no quedan confirmados. Cambiar variable sólo en entorno local permitido y reiniciar Pagos si se desea otro escenario. |
| Pago simulado no disponible | Pagos detenido o clave interna discrepante | comprobar `docker compose ps pagos` y logs de Reservas/Pagos; verificar variable internamente sin imprimir su valor. |
| No se acreditan puntos | turno no completado, regla cero/inactiva, RabbitMQ no disponible o evento fallido | verificar estado `completado`, regla administrativa y mensajes fallidos. Pago aprobado por sí solo no acredita puntos. |
| GhostKitchen no carga | no hay `GHOSTKITCHEN_API_KEY`, API upstream inaccesible, timeout/429/503 | comprobar variable de Beneficios por presencia (no imprimir valor), logs sin header; reintenta 429/503 hasta tres intentos; proveedor debe estar operativo. |
| Partner obtiene 401/503 al acreditar | `X-Partner-Key` falta/no coincide o dependencia no disponible | verificar configuración en canal secreto, `Content-Type: application/json`, endpoint y cuenta; reintentar con misma referencia si respuesta es incierta. |
| `docker compose config` falla | faltan `AUTH_JWT_SECRET`/`MELT_INTERNAL_KEY` o YAML inválido | generar secretos locales fuera del repo y validar de nuevo; no añadirlos al `.env.example` con valores reales. |

Comandos usuales: `docker compose ps`, `docker compose logs --tail 100 <servicio>`, `docker compose up --build -d <servicio>`, `docker compose restart <servicio>`. Evitar `docker compose down -v`, `docker volume rm` y comandos prune durante diagnóstico.
