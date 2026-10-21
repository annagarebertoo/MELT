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