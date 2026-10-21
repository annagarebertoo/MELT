# ADR-012 — Balanceo de carga

**Estado:** Propuesto  
**Fecha:** 2026-10-21  
**Entrega:** 2 — 23 de octubre de 2026  
**Decisores:** Equipo MELT

## Contexto y problema

MELT deberá distribuir solicitudes entre varias instancias de al menos un microservicio. También deberá detectar cuándo una instancia deja de estar disponible y evitar enviarle solicitudes mientras continúe fallando.

La distribución y el estado de las instancias deberán poder comprobarse mediante la observabilidad del sistema.

## Alternativas consideradas

1. **Distribución manual desde el cliente:** se descarta porque el cliente tendría que conocer las instancias disponibles y gestionar sus fallos.
2. **Balanceo mediante API Gateway con NGINX:** permite centralizar la distribución de solicitudes y configurar verificaciones de disponibilidad.
3. **Balanceador administrado externo:** podría delegar parte de la configuración a una plataforma de infraestructura, pero agrega una dependencia que debe evaluarse según el entorno de despliegue.

## Decisión

Se propone utilizar NGINX como componente de balanceo para distribuir las solicitudes entre dos o más instancias del servicio seleccionado.

Se deberá configurar y verificar el comportamiento ante instancias no disponibles. La solución también deberá permitir observar la distribución de solicitudes y el estado de las instancias.

## Consecuencias

- Las solicitudes podrán distribuirse entre varias instancias.
- Será necesario configurar la detección de instancias no disponibles.
- La configuración deberá probarse con instancias operativas y con al menos una instancia fuera de servicio.
- La observabilidad deberá aportar evidencia del comportamiento del balanceador.

## Pendientes

Quedan pendientes la configuración ejecutable, la elección definitiva del servicio, la puesta en marcha de dos o más instancias y las pruebas que demuestren la distribución y la detección de fallos.

Esta decisión documenta una propuesta arquitectónica; no implica que el balanceo ya esté operativo.