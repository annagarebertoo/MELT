# ADR-010 — Resiliencia ante fallos

**Estado:** Propuesto  
**Fecha:** 2026-10-21  
**Entrega:** 2 — 23 de octubre de 2026  
**Decisores:** Equipo MELT

## Contexto y problema

MELT está compuesto por microservicios que pueden depender de otros servicios o componentes externos. Una dependencia lenta o no disponible podría retrasar una operación o impedir que finalice correctamente.

Es necesario definir cómo manejar los tiempos de espera, los errores y los reintentos sin generar esperas indefinidas ni repetir operaciones que puedan producir efectos duplicados.

## Alternativas consideradas

1. **Esperar sin límite y repetir las solicitudes:** se descarta porque una dependencia caída podría bloquear las operaciones durante demasiado tiempo.
2. **Definir tiempos de espera y reintentos limitados:** permite acotar las esperas y recuperar algunos errores transitorios. Requiere establecer límites y condiciones para reintentar.
3. **Agregar un circuit breaker:** podría evitar llamadas repetidas a una dependencia que falla, pero requiere configuración y pruebas adicionales. Su incorporación queda pendiente de evaluación.

## Decisión

Se propone definir tiempos de espera explícitos para las llamadas síncronas y limitar los reintentos a los casos en que sean apropiados.

Los errores de una dependencia deberán traducirse en respuestas controladas, sin exponer detalles internos. No se deberán reintentar automáticamente operaciones que puedan duplicar efectos, salvo que exista un mecanismo de idempotencia adecuado.

Cuando falle un componente no crítico, se deberá evaluar si la operación principal puede continuar. Por ejemplo, el envío de una notificación por correo no deberá bloquear la confirmación de una reserva.

## Consecuencias

- Se limitará el tiempo de espera ante dependencias lentas.
- Los errores podrán comunicarse de manera controlada.
- Los reintentos deberán configurarse cuidadosamente para no agravar una caída ni duplicar operaciones.
- Será necesario registrar los fallos para poder diagnosticarlos.

## Pendientes

Los valores concretos de timeout, la cantidad y separación de los reintentos, y la eventual incorporación de un circuit breaker deberán definirse e implementarse según las comunicaciones reales del sistema.

La decisión deberá validarse mediante pruebas de fallas controladas. Este ADR documenta una propuesta y no demuestra que los mecanismos ya estén implementados.

## Revisión para la Entrega 2 (23/10/2026)

La implementación ya contiene límites concretos: NGINX configura conexión/envío/lectura al upstream en 2s/5s/5s; Catálogo utiliza contextos de 2s para salud y 5s para operaciones de tratamientos sobre MongoDB. Los intervalos de envío/lectura del proxy no equivalen a un límite total de cinco segundos por solicitud. Se conserva la propuesta general y se actualiza su estado real: estos límites están configurados, pero no se provocó su vencimiento.

| Dependencia | Comportamiento inspeccionado | Evidencia y límite |
| :--- | :--- | :--- |
| NGINX → Catálogo | Timeouts y dos destinos disponibles mediante resolución DNS del upstream. | Configuración válida y distribución saludable; sin prueba de caída, failover o agotamiento de tiempo. |
| Catálogo → MongoDB | Salud devuelve 503 si falla el ping; detalle convierte un error de lectura en 500; el helper del listado convierte errores en 503. | El test existente de salud degradada usa un ping simulado y pasó. No prueba la caída de MongoDB real ni uniformidad entre operaciones. |
| Catálogo → Jaeger | La exportación tiene timeout de 2s y reintentos deshabilitados; registra fallas de telemetría. | Código de D11 inspeccionado, sin modificarlo y sin detener el colector. No es una política de reintentos de negocio. |
| Proveedor externo | Sin proveedor ni cliente identificados. | No es posible definir o comprobar el comportamiento concreto; D9 sigue bloqueada. |

No hay política explícita `proxy_next_upstream*` en los archivos inspeccionados ni circuit breaker de negocio. No se infiere ausencia de reintentos implícitos de NGINX. El proveedor de correo desacoplado sigue siendo diseño, no una degradación funcional comprobada.

Para cerrar D10 como decisión faltan concretar por dependencia los resultados visibles, las condiciones de reintento y el mecanismo de protección seleccionado; no se agregan circuit breakers por anticipación. La caída controlada y el postmortem real quedan para la validación posterior autorizada. `docs/POSTMORTEM.md` sigue siendo una plantilla, no evidencia de un incidente ejecutado. Ver [matriz de cierre](../ENTREGA-2.md).
