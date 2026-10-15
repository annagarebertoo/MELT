# Postmortem — plantilla de resiliencia

> **Estado:** plantilla operativa. No se declara una prueba de fallo ejecutada hasta completar la tabla de evidencias con fecha, commit, comandos y resultados reales.

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
