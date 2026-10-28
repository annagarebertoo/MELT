# ADR-007: Caché del catálogo

> **Cierre documental de Entrega 2:** se define al final la política inicial, con hipótesis y parámetros de diseño. La necesidad y mejora de rendimiento siguen sin medición.

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

## Política inicial definida para implementar

Se conserva Memcached. El flujo elegido es el listado público de tratamientos consultado repetidamente con los mismos parámetros, no datos privados ni disponibilidad, precios de confirmación de reservas o saldos. La hipótesis es que repetir listados del catálogo genera lecturas evitables; deberá contrastarse con frecuencia real y latencia antes de habilitar caché. La muestra actual no demuestra esa necesidad ni justifica dimensionamiento productivo.

Se usará cache-aside en Catálogo: buscar respuesta pública del listado; en miss consultar la fuente del flujo y guardar el resultado 200. No cachear errores ni mezclar páginas o consumidores con datos privados. La clave incluirá versión de API, generación de catálogo y hash de parámetros normalizados `q`, categoría, precio máximo, página, límite, campo y sentido de ordenamiento. El hash acota el tamaño de clave; normalizar no debe cambiar la semántica del contrato.

**TTL inicial de diseño: 30 segundos.** Es un compromiso técnico de frescura para lecturas públicas, no medición ni política comercial. Luego de una escritura confirmada o un cambio de generación del índice, invalidar la generación del listado; las entradas anteriores expiran por TTL. Si falla invalidación, registrar el incidente y no prometer frescura inmediata. La generación y las claves no pueden utilizarse como autoridad de reglas de negocio; Reservas siempre validará con la fuente responsable.

Si Memcached no responde, limitar el intento de caché a un objetivo inicial de 50 ms, registrar fallo y continuar hacia la fuente. No reintentar la caché dentro de esa misma solicitud ni bloquear una lectura válida por no poder guardar su respuesta. Si la fuente falla, conservar el error: no servir valores vencidos como un resultado actual. El fallback y su presupuesto son diseño, no código existente.

Medir hits, misses, errores/timeouts, latencia de caché y fuente, lecturas evitadas, tasa de claves repetidas y antigüedad observada. Comparar con/sin caché sobre el mismo dataset y patrón. Criterio inicial de habilitación: observar consultas repetidas y una reducción de lecturas a la fuente sin aumento de errores ni incumplimiento del objetivo de frescura; si no se demuestra beneficio, mantenerla deshabilitada y revisar la necesidad. No se inventa un porcentaje de mejora.
