# ADR-007: Caché del catálogo

- **Estado:** Propuesto
- **Fecha:** 2026-10-21

## Contexto

El catálogo de tratamientos puede recibir consultas frecuentes. Se necesita reducir la latencia de las lecturas y evitar consultas repetidas al almacenamiento cuando sea posible.

## Decisión

Se propone utilizar Memcached para almacenar temporalmente los resultados de consultas frecuentes del catálogo.

Las claves de caché deberán considerar los parámetros de la consulta, incluidos los filtros, la paginación y el ordenamiento, para evitar devolver resultados correspondientes a búsquedas diferentes.

Cuando se modifique el catálogo, se deberá definir un mecanismo de invalidación o actualización de las entradas afectadas. También se deberá establecer un tiempo de vida (TTL) para las entradas almacenadas.

## Consecuencias

- Las consultas frecuentes podrían responder más rápido.
- Los resultados almacenados podrían quedar desactualizados si la invalidación falla.
- Será necesario definir qué ocurre cuando Memcached no está disponible.
- Se deberán medir los aciertos y fallos de caché, la latencia y los errores para evaluar su efectividad.

## Pendientes

Memcached, la política de TTL, la invalidación y el comportamiento ante fallos todavía deben implementarse y verificarse.

Esta decisión documenta una propuesta arquitectónica; no implica que la caché ya esté operativa.