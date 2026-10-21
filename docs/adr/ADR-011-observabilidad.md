# ADR-011 — Observabilidad

**Estado:** Propuesto  
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

**Pendiente de validar:** la instrumentación real, la correlación de registros, la generación de trazas y la configuración de métricas y tableros. Esta decisión documenta una propuesta; no implica que estos mecanismos ya estén implementados.
