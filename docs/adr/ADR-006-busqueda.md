# ADR-006: Búsqueda de tratamientos

- **Estado:** Propuesto
- **Fecha:** 2026-10-21

## Contexto

MELT necesita ofrecer búsquedas de tratamientos que permitan encontrar información mediante texto y aplicar filtros, paginación y ordenamiento. La búsqueda deberá mantenerse sincronizada con los cambios realizados en el catálogo.

## Decisión

Se propone utilizar OpenSearch como motor de búsqueda de tratamientos.

El índice incluirá nombre, descripción, categoría y palabras clave (`keywords`). Las consultas deberán soportar filtros relevantes, paginación y al menos un criterio de ordenamiento.

Los cambios del catálogo deberán propagarse al índice de forma asíncrona. Se acepta consistencia eventual entre los datos fuente y los resultados de búsqueda.

## Consecuencias

- Las consultas de búsqueda dependerán del índice de OpenSearch.
- Los cambios del catálogo podrán tardar un tiempo en aparecer en los resultados.
- Será necesario definir un mecanismo de actualización o reconstrucción del índice.
- Se deberá establecer y medir el retraso máximo tolerable entre un cambio en el catálogo y su aparición en los resultados.

## Pendientes

OpenSearch y el mecanismo de actualización todavía deben implementarse y verificarse. El retraso máximo tolerable y la evidencia de funcionamiento quedan pendientes de definición y medición.

Esta decisión documenta la propuesta arquitectónica; no implica que la búsqueda indexada ya esté operativa.

## Revisión para la Entrega 2 (23/10/2026)

Se mantiene OpenSearch y la información indexable ya definida. Frente a conservar únicamente consultas directas a MongoDB, el motor dedicado permite separar el índice de lectura y cubrir la búsqueda indexada prevista; introduce otra dependencia y el riesgo de resultados temporalmente desactualizados. MongoDB continúa como fuente de verdad. La consulta actual por nombre y keywords usa expresiones regulares de MongoDB: no es evidencia de OpenSearch.

Compose declara el motor, pero el entorno inspeccionado no tiene contenedor OpenSearch y no hay cliente, mappings ni proceso de actualización en Catálogo. Se verificó la ausencia de integración, no la funcionalidad del índice.

Para completar D6 como decisión del 23/10, faltan seleccionar el mecanismo concreto de actualización o reconstrucción y acordar el retraso máximo aceptable. La propagación asíncrona está prevista; RabbitMQ y Outbox no se consideran mecanismos ya implementados ni una selección definitiva de publicación confiable. No se inventa un plazo en esta revisión.

Como criterios de validación posteriores, comparar resultados con los datos fuente, medir el tiempo entre modificación y visibilidad, y verificar reconstrucción y recuperación de errores. La implementación del índice, sus consultas y esas mediciones corresponden a la etapa posterior. Ver [matriz de cierre](../ENTREGA-2.md).
