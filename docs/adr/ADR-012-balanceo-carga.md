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

La configuración ejecutable selecciona Catálogo y Profesionales y declara dos réplicas. La distribución y salud actuales se comprobaron en la revisión del 23/10 descrita a continuación. Permanecen pendientes la validación de detección de fallos y el comportamiento con una instancia fuera de servicio.

El diseño de recuperación ante fallas continúa propuesto; la distribución entre instancias saludables ya cuenta con evidencia local y no debe confundirse con una prueba de failover.

## Revisión para la Entrega 2 (23/10/2026)

NGINX utiliza `catalogo_backend` con resolución DNS de Docker, zona compartida y conexiones persistentes. No hay algoritmo de distribución ni verificaciones activas de salud declarados explícitamente en ese upstream. El healthcheck de MongoDB y la consulta manual de salud de Catálogo no constituyen un healthcheck activo del balanceador.

Se ejecutaron 12 GET secuenciales contra la ruta contractual. Todos respondieron 200; los logs del gateway identificaron el upstream y los de Catálogo la instancia, con IDs correlacionados. Se observaron seis respuestas de cada réplica y ambas devolvieron salud correcta. Es evidencia de distribución en este entorno, no una garantía de reparto exacto bajo cualquier carga.

No se detuvieron ni reiniciaron contenedores. Para cerrar la estrategia de disponibilidad falta documentar explícitamente las condiciones de exclusión y recuperación de destinos y validar sus efectos. La detección efectiva de una réplica caída, los tiempos y la recuperación necesitan el ensayo posterior autorizado. Ver [evidencia de cierre](../evidence/entrega-2-2026-10-23.json) y [matriz](../ENTREGA-2.md).

## Ensayo posterior autorizado de caída y reincorporación

Se conserva NGINX sobre dos réplicas de Catálogo. La resolución DNS existente actualiza destinos con vigencia de cinco segundos. Sin algoritmo explícito, el balanceo utiliza round-robin ponderado predeterminado; los parámetros pasivos predeterminados son `max_fails=1` y `fail_timeout=10s`, según la [documentación técnica de NGINX](https://nginx.org/en/docs/http/ngx_http_upstream_module.html#server). No hay healthcheck activo configurado. Estos valores predeterminados explican el mecanismo previsto, pero el ensayo no midió directamente sus temporizadores ni prueba una exclusión exacta de diez segundos.

Con la réplica 2 detenida por autorización expresa, se observaron once respuestas 200 de la réplica 1 y un 504. Se restauró el mismo contenedor; salud volvió a ser correcta y las doce consultas siguientes respondieron 200 con las dos instancias presentes en logs. La recuperación y reincorporación se comprobaron; la tolerancia sin errores no. Se mantiene D12 parcial hasta investigar el 504 y verificar la política efectiva de intentos, exclusión y recuperación. Ver [postmortem](../POSTMORTEM.md) y [evidencia](../evidence/fallo-replica-2026-10-23.json).
