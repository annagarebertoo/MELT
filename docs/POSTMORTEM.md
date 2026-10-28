# Postmortem — plantilla de resiliencia

> **Estado al 23/10/2026:** ensayo real de una réplica ejecutado y documentado al final. Las instrucciones y la tabla originales se conservan como preparación para escenarios adicionales; no todos fueron ejecutados. Cualquier nueva interrupción requiere autorización.

## Incidente a evaluar

**Escenario:** una instancia del servicio de Catálogo deja de responder mientras el gateway sigue recibiendo solicitudes.

**Impacto esperado a verificar:** el gateway debe seguir alcanzando la instancia saludable; si ninguna instancia responde, el cliente recibe un error HTTP controlado y no una conexión colgada indefinidamente.

## Preparación

1. Arrancar el entorno: `docker compose up --build -d`.
2. Verificar salud del gateway: `curl http://localhost:8080/health`.
3. Crear al menos dos instancias del catálogo: `docker compose up --build -d --scale catalogo-profesionales=2 catalogo-profesionales gateway`.
4. Verificar catálogo: `curl "http://localhost:8080/api/v1/catalogo/tratamientos?page=1&limit=10"`.
5. Registrar el commit probado, la configuración Docker y la hora de inicio.

## Simulación de fallo

La forma exacta de detener una réplica depende del nombre que Docker Compose asigne a cada contenedor. Identificar las réplicas con:

```sh
docker compose ps
```

Detener **una sola** instancia del catálogo con `docker stop <contenedor_catalogo>`. Repetir varias solicitudes de consulta al gateway y guardar códigos HTTP, tiempos y respuestas. Luego restaurar la instancia con `docker start <contenedor_catalogo>`.

Después, detener todas las instancias del catálogo, repetir las consultas y comprobar que los errores son acotados por los timeouts del gateway. Volver a levantar el servicio al finalizar.

## Evidencia que debe adjuntarse

| Evidencia | Resultado real |
|---|---|
| Fecha y commit probado | Pendiente |
| `docker compose ps` con dos réplicas | Pendiente |
| Solicitudes antes del fallo | Pendiente |
| Solicitudes con una réplica detenida | Pendiente |
| Solicitudes sin réplicas disponibles | Pendiente |
| Tiempo máximo observado y códigos HTTP | Pendiente |
| Recuperación tras reiniciar la réplica | Pendiente |
| Capturas/logs adjuntos | Pendiente |

## Aprendizajes y acciones

Completar esta sección **después** de ejecutar la simulación. Registrar qué se observó, qué no se comportó como se esperaba, la causa comprobada y las acciones correctivas con responsables. No inferir resultados a partir de la configuración solamente.

## Limitaciones conocidas del entorno actual

- El gateway configura timeouts de conexión y lectura; eso no sustituye un test de resiliencia ejecutado.
- Los endpoints de salud de Catálogo verifican MongoDB. Reservas y Beneficios todavía tienen endpoints mínimos de salud.
- La configuración actual de Prometheus solo scrapea Prometheus; aún no hay instrumentación de métricas de negocio ni dashboard provisionado.
- La autenticación de MongoDB y las credenciales de desarrollo no son aptas para despliegue público. No exponer este Compose a Internet.

## Ensayo autorizado — 23/10/2026

**Base:** `cc230fa`, rama `feature/sofia-adr-entrega-2`. **Evidencia:** [fallo-replica-2026-10-23.json](evidence/fallo-replica-2026-10-23.json). Verificador: `scripts/verify-replica-failure.py`.

El usuario autorizó detener únicamente `melt-catalogo-profesionales-2`, consultar el listado público y restaurar ese mismo contenedor incluso si fallaba una comprobación. No se modificó configuración ni se detuvieron MongoDB, gateway, Jaeger o todas las réplicas.

| Momento UTC (Argentina: UTC−3) | Hecho registrado |
| :--- | :--- |
| 17:45:09.574 | Inicio del ensayo y referencia de estado previo. |
| 17:45:09.971 | Se confirmó detenida la réplica 2. Se ejecutaron doce GET a `/api/v1/tratamientos`. |
| Durante esa fase | Once respuestas 200 y una 504; máximo observado 2.004,042 ms. Los handlers que respondieron pertenecían solo a la réplica 1. |
| 17:45:12.570 | Restauración solicitada desde `finally`; después se comprobó salud de la réplica 2 y se esperaron seis segundos antes de consultar recuperación. No se midió el instante exacto de recuperación. |
| Fase posterior | Doce respuestas 200; máximo observado 26,478 ms; se observaron las dos instancias. |

**Impacto:** una de doce solicitudes de la fase interrumpida no obtuvo el catálogo aunque había una réplica saludable. La solicitud `sofia-fallo-1b98f1fbf44f46828b9e7445e7e56064` devolvió 504 y su log registró únicamente el destino `172.19.0.4:8081`. No llegó a registrarse un handler de Catálogo para esa solicitud. No se afirma que se reintentara contra la otra instancia.

**Detección y respuesta:** las respuestas HTTP y los logs correlacionados hicieron visible el fallo. La restauración automática recuperó el mismo contenedor. Las instantáneas de documentos e índices de MongoDB coincidieron antes/después; los identificadores y horas de inicio de gateway, MongoDB y Jaeger no cambiaron.

**Causa comprobada y límite del análisis:** hubo un intento registrado contra la réplica detenida y la respuesta 504 tardó aproximadamente el timeout de conexión configurado. El mecanismo exacto por el cual esa solicitud no pasó a otro destino no quedó demostrado; la interacción entre resolución DNS, destinos disponibles y comportamiento de reintento es una hipótesis a investigar, no una causa raíz confirmada. No se midió un período exacto de exclusión pasiva.

**Acciones pendientes:** acordar política explícita de intentos y exclusión/recuperación de destinos, revisar el camino de errores del proxy y repetir el ensayo con autorización después de cualquier cambio. No se cambió infraestructura en este cierre. La comprobación estricta de cero errores del verificador devolvió código 1 y se conserva como resultado no aprobado; no se transformó en éxito por haber restaurado la réplica.

**Validado:** interrupción real de una réplica, continuidad parcial, error acotado observado, restauración y reincorporación de ambas instancias. **No validado:** failover sin errores, caída total, caída de MongoDB o del proveedor, agotamiento del timeout de lectura, carga, duplicación o recuperación transaccional. Las filas originales que corresponden a estos escenarios continúan pendientes.
