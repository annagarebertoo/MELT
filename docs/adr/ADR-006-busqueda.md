# ADR-006: Búsqueda de tratamientos

> **Cierre documental de Entrega 2:** se concreta al final una estrategia y un objetivo verificable. No se presenta OpenSearch como integrado ni el objetivo como medido.

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

## Estrategia definida para implementar

Se mantiene OpenSearch como índice derivado, privado de Catálogo, con un documento por tratamiento y su `id` como identificador estable. Indexar nombre, descripción y keywords para texto, categoría para filtro exacto, precio y nombre para ordenamiento y `id` como desempate estable. Paginación y filtros deberán preservar el contrato versionado; no se amplía aquí el comportamiento de `q` publicado en v1 sin revisar su compatibilidad.

La estrategia inicial será **reconstrucción periódica asíncrona desde MongoDB**, en lugar de dar por implementada una entrega confiable de eventos. Un trabajador interno de Catálogo leerá una instantánea lógica del catálogo, construirá una generación nueva y cambiará el alias de lectura solo cuando termine correctamente. No será un cuarto servicio de negocio. Se propone esta opción para el volumen inicial pequeño: permite recuperarse sin eventos perdidos; se acepta costo de reconstrucción completa y una ventana de desactualización. La publicación incremental mediante RabbitMQ continúa como evolución candidata, no como decisión ya implementada.

**Objetivo de diseño inicial:** iniciar reconstrucción cada 30 segundos; presupuesto de hasta 30 segundos para completarla; retraso máximo objetivo de 60 segundos desde un cambio confirmado en MongoDB hasta su visibilidad. Son parámetros técnicos seleccionados para el diseño inicial, no requisitos del profesor, mediciones ni una garantía acreditada. Si el volumen impide cumplirlos, revisar estrategia y registrar una nueva decisión. No activar un proceso periódico en esta etapa.

Una generación fallida no reemplaza a la última válida. Registrar error, duración y antigüedad; reintentar en el siguiente ciclo sin duplicar documentos. Si no existe una generación válida o la antigüedad supera el objetivo, responder indisponibilidad controlada en el futuro flujo de búsqueda, sin presentar resultados antiguos como vigentes. El catálogo y su detalle conservan MongoDB como autoridad. No se incorpora un fallback silencioso que cambie la semántica de búsqueda.

Medir generación activa, última reconstrucción exitosa, errores, duración, documentos fuente/indexados y retraso de visibilidad. La prueba posterior modificará un dato autorizado, comprobará su aparición dentro del objetivo y verificará recuperación de una generación fallida. El diseño no implementa esas modificaciones ni las declara probadas hoy.
