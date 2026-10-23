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

## Revisión para la Entrega 2 (23/10/2026)

Se conserva Memcached como caché descartable de consultas del catálogo. La alternativa de leer siempre MongoDB simplifica la consistencia y evita otra dependencia, pero no reduce lecturas repetidas; usar una caché exige justificar ese costo adicional con métricas. No existe todavía evidencia de una necesidad de rendimiento ni de una mejora medida: un catálogo de muestra con un documento no basta para justificarlas.

Compose declara Memcached, pero no existe contenedor de ese servicio en el entorno inspeccionado ni cliente o consultas de caché en el código. Los GET comprobados siguen consultando MongoDB. La estrategia no está implementada.

Para cerrar D7 como decisión, falta fijar el flujo preciso y su necesidad observable, seleccionar invalidación o versionado, vigencia y comportamiento ante indisponibilidad. Evitar que una falla de caché impida consultar la fuente es una opción a evaluar, no un fallback ya implementado. No se asigna un TTL sin acuerdo del equipo.

La validación posterior deberá medir hit/miss, latencia, errores, consultas evitadas y resultados equivalentes con y sin caché; también comprobar aislamiento entre claves de filtros, páginas y ordenamiento. La implementación y la comparación antes/después no se anticipan en esta entrega documental. Ver [matriz de cierre](../ENTREGA-2.md).
