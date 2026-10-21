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