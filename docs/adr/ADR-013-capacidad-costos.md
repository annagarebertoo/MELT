# ADR-013 — Capacidad y costos

**Estado:** Propuesto  
**Fecha:** 2026-10-21  
**Entrega:** 2 — 23 de octubre de 2026  
**Decisores:** Equipo MELT

## Contexto y problema

MELT necesita evaluar cuánto trabajo puede soportar el sistema y cómo se comporta ante diferentes niveles de carga. También es necesario identificar los límites de los componentes y considerar los recursos requeridos para ejecutar la solución.

Sin mediciones, no es posible establecer de manera confiable la capacidad del sistema ni justificar decisiones de escalado.

## Alternativas consideradas

1. **Estimar la capacidad sin ejecutar pruebas:** se descarta como método principal porque no permite verificar el comportamiento real.
2. **Realizar pruebas de carga y registrar métricas:** permite medir tiempos de respuesta, cantidad de solicitudes procesadas y errores bajo distintas cargas.
3. **Escalar recursos sin mediciones previas:** se descarta como decisión inicial porque puede aumentar los costos sin demostrar una mejora suficiente.

## Decisión

Se propone evaluar la capacidad de MELT mediante pruebas de carga sobre los servicios seleccionados, registrando al menos la latencia, el volumen de solicitudes procesadas y la tasa de errores.

A partir de los resultados se deberán identificar los límites observados y analizar posibles alternativas de escalado y sus costos.

No se establecerán cifras de capacidad ni costos estimados como resultados comprobados hasta contar con mediciones y datos de infraestructura.

## Consecuencias

- Se obtendrá evidencia para evaluar el comportamiento del sistema bajo carga.
- Será posible identificar cuellos de botella y necesidades de escalado.
- Las conclusiones dependerán de las condiciones y los límites de las pruebas realizadas.
- El análisis de costos requerirá documentar los recursos y el entorno utilizados.

## Pendientes

Quedan pendientes la definición del escenario de carga, la ejecución de las pruebas, el registro de resultados, la identificación de límites y el análisis de costos.

Esta decisión documenta una propuesta de evaluación; no implica que las pruebas se hayan ejecutado ni que la capacidad esté medida.