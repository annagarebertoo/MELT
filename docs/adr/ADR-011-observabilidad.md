# ADR-011 — Observabilidad

**Estado:** Implementación inicial verificada localmente para logs, correlación y trazas de Catálogo; métricas y tableros pendientes.

**Entrega:** 2 — 23 de octubre de 2026

## Contexto

MELT utiliza tres microservicios. Para identificar errores y seguir una operación entre componentes, se necesita registrar información estructurada y correlacionada, medir el comportamiento de los servicios y visualizar su estado.

## Decisión propuesta

Se propone incorporar observabilidad progresivamente:

- **Logs:** registros estructurados con fecha y hora, nivel, servicio, operación, resultado e identificador de correlación.
- **Métricas:** medir solicitudes, errores y duración de las operaciones.
- **Trazas distribuidas:** permitir seguir una solicitud a través de los componentes involucrados.
- **Tableros:** utilizar Prometheus para recolectar métricas y Grafana para visualizarlas.
- **Alertas y SLO:** definirlos cuando existan métricas y datos de ejecución suficientes para establecer objetivos realistas.

## Alcance inicial

Para la entrega 2 se priorizan los logs correlacionados y una primera traza del flujo principal. Las métricas y los tableros se incorporarán progresivamente.

## Alternativas consideradas

1. **Logs únicamente:** permiten investigar errores, pero dificultan analizar el rendimiento y seguir operaciones distribuidas.
2. **Observabilidad progresiva (elegida):** permite comenzar con registros y trazas, y sumar métricas y tableros a medida que se implementan los servicios.

## Consecuencias

La solución facilitará la investigación de errores y el análisis del comportamiento de los servicios. Como contrapartida, requerirá instrumentación, configuración y recursos adicionales.

## Implementación inicial de Sofía — 23/10/2026

La implementación se realiza en `feature/sofia-entrega-2`, desde `0096f25`, sin modificar contratos, mock ni reglas de negocio.

### Decisión técnica aplicada

- Logs JSON de Catálogo con `log/slog`: fecha, nivel, servicio, instancia, método, ruta normalizada, estado, duración e identificadores de solicitud y traza.
- Correlación por `X-Request-ID`, reutilizado solo si cumple una lista de caracteres y longitud acotadas; generación de uno nuevo si no es válido. NGINX y Catálogo devuelven el identificador sin duplicar cabeceras.
- Contexto W3C `traceparent`/`tracestate` propagado por el módulo nativo OpenTelemetry de NGINX y extraído por Catálogo.
- OpenTelemetry Go 1.47.0 y monitor oficial del driver MongoDB v2 para spans HTTP y de comandos reales. La instrumentación MongoDB se fija a una versión de commit en `go.mod`, porque no dispone de una versión etiquetada estable en la selección realizada.
- Jaeger 2.11.0 como receptor OTLP y consulta local, en un solo proceso. Almacenamiento en memoria y UI disponible únicamente en localhost.
- Exclusión de cuerpos, credenciales y textos de comandos de los logs de Catálogo. Jaeger elimina los atributos de URL sin normalizar, user agent y textos de consulta antes de almacenarlos.

Se elige esta solución para obtener spans padre/hijo reales con una sola dependencia local adicional. Solo correlacionar logs no demostraría trazabilidad distribuida; agregar otro Collector o un sistema de almacenamiento persistente no es necesario para esta primera evidencia.

### Verificación realizada

La compilación local, el build de la imagen Linux, los tests existentes y cuatro tests nuevos de observabilidad pasan. `go vet` no informa problemas.

El verificador de integración ejecutó GET de tratamientos con ID suministrado, sin ID, con ID inválido y con contexto W3C externo. Consultó Jaeger y comprobó la relación NGINX → HTTP Catálogo → comandos MongoDB (`aggregate` y `find`), la coincidencia de logs y las dos réplicas saludables. El marcador sensible de prueba no aparece en los logs de solicitud ni en la traza almacenada.

Evidencia saneada: [D11 — ejecución del 23/10](../evidence/d11-2026-10-23.json). Incluye fecha, commit base, cambios sin commit, hashes de los archivos de implementación, logs y referencias padre/hijo. Procedimiento reproducible: [Observabilidad D11](../OBSERVABILITY.md).

### Límites y evolución

El cliente no exporta un span propio y MongoDB no tiene instrumentación interna: se observan sus operaciones desde el driver. La traza verificada corresponde a la consulta real del Catálogo; no acredita una reserva completa, que todavía no está implementada. Reservas, Beneficios y la mensajería no se instrumentan en esta parte.

Jaeger pierde las trazas al reiniciarse y no es una configuración de producción. La exportación se realiza en lotes, por lo que una caída forzada puede perder telemetría. Los logs internos de arranque/error de NGINX conservan su formato nativo; sus accesos HTTP y los logs de aplicación de Catálogo sí son JSON.

Quedan pendientes métricas de negocio, tableros, alertas, SLO y trazas de los demás flujos. La evidencia no declara completa la observabilidad del proyecto final.
