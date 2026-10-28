# ADR-003 — Persistencia (versión inicial)

**Estado:** Propuesto (versión inicial)  
**Fecha:** 2026-10-08  
**Entrega:** 1 — 9 de octubre de 2026  
**Decisores:** Equipo MELT

## Contexto y problema

Cada microservicio de MELT es dueño exclusivo de sus datos (ver ADR-001). Hay que elegir el tipo de almacenamiento primario para:

- **Catálogo y Profesionales:** documentos descriptivos (tratamientos, categorías, profesionales, imágenes, precios, duraciones, puntos configurados).
- **Reservas:** agenda, turnos, intervalos, estados y transiciones que deben protegerse frente a concurrencia.
- **Beneficios:** saldos, movimientos de puntos, canjes, aplicaciones y restituciones con control de no duplicación.

Se requiere persistencia relacional y no relacional (requisito del proyecto). Las bases serán privadas: un servicio no consulta directamente las tablas o colecciones de otro.

## Alternativas consideradas

1. **PostgreSQL para los tres servicios**  
   Homogeneidad y madurez transaccional. El catálogo, sin embargo, es predominantemente documental (descripciones, listas de imágenes, atributos variables) y se beneficia de un modelo más flexible para lecturas y actualizaciones parciales.

2. **MongoDB para los tres servicios**  
   Flexibilidad de esquema. Pierde las garantías ACID y las restricciones relacionales naturales para agenda (intervalos no solapados) y saldos (movimientos consistentes).

3. **MongoDB para Catálogo + PostgreSQL para Reservas y Beneficios** (opción elegida)  
   Ajusta el motor al patrón de acceso de cada dominio.

4. **Una sola instancia compartida con esquemas separados**  
   Posible en desarrollo, pero los servicios no deben compartir tablas ni credenciales de escritura cruzada.

## Decisión (propuesta inicial)

| Servicio | Persistencia propuesta | Motivo principal |
| :--- | :--- | :--- |
| Catálogo y Profesionales | **MongoDB** | Lectura y actualización de documentos del catálogo con información descriptiva e imágenes asociadas. |
| Reservas | **PostgreSQL** | Relaciones y transacciones para proteger la agenda y las transiciones del turno. |
| Beneficios | **PostgreSQL** | Cambios consistentes de saldo y movimientos, con control de duplicación. |

- Cada servicio tendrá datos y credenciales privados.
- En desarrollo, Reservas y Beneficios podrán compartir una instancia de PostgreSQL, pero utilizarán **bases separadas**: no compartirán tablas ni consultarán datos ajenos.
- Las referencias entre servicios se transmitirán mediante identificadores (API o eventos), no mediante joins cross-service.
- OpenSearch y Redis contendrán copias derivadas; no serán autoridad sobre agenda, saldo o validaciones comerciales.
- Esquemas, migraciones, índices y alojamiento de imágenes se definirán en etapas posteriores.

## Justificación

- **Catálogo:** el acceso es de lectura intensiva, con documentos que agrupan nombre, descripción, precio, duración, puntos e imágenes. MongoDB facilita actualizaciones parciales y un modelo flexible sin forzar un esquema relacional rígido desde el inicio.
- **Reservas:** la integridad de la agenda (evitar superposiciones) y las transiciones de estado del turno se benefician de transacciones ACID, restricciones de unicidad/exclusión y consultas relacionales. PostgreSQL es el candidato natural.
- **Beneficios:** los movimientos de puntos deben ser consistentes (débito/crédito sin pérdidas ni duplicados). Una base relacional con transacciones facilita el control de saldos y la trazabilidad.

Esta elección cumple el requisito de combinar persistencia relacional y no relacional y mantiene la propiedad exclusiva de datos de ADR-001.

## Consecuencias

### Positivas

- Motor alineado al patrón de acceso de cada servicio.
- Reservas y Beneficios pueden aprovechar transacciones y constraints para las garantías de negocio (no superposición, no doble acreditación).
- Catálogo puede evolucionar su documento sin migraciones rígidas tempranas.

### Negativas / limitaciones

- Dos tecnologías de persistencia aumentan la curva de aprendizaje y la superficie operativa.
- MongoDB no garantiza por sí solo la publicación confiable de cambios hacia el índice de búsqueda o hacia otros consumidores; ese mecanismo se evaluará con el diseño de comunicación (D5).
- La coordinación entre Reservas y Beneficios (reserva con beneficio, restitución) no puede apoyarse en una transacción distribuida nativa; requerirá patrones de consistencia eventual / compensación.

### Aspectos pendientes

- Esquemas concretos, migraciones e índices.
- Política de backups y retención.
- Evaluación de Transactional Outbox u otros patrones para publicar eventos de forma confiable desde cada base.
- Decisión definitiva de instancias (compartida vs. separadas en entornos distintos).
- Alojamiento de imágenes (objeto store vs. referencias en MongoDB).


## Enmienda de decisión — revisión de alcance

**Estado de esta enmienda:** Aceptada por el equipo para el diseño actualizado.

1. Se mantienen PostgreSQL para Reservas y Beneficios.
2. Se separa MongoDB en dos bases lógicas: `catalogo_db` (tratamientos, categorías y `keywords`) y `profesionales_db` (perfiles, aptitudes y asociaciones). Pueden residir en la misma instancia durante desarrollo, pero con bases y permisos lógicos separados.
3. La caché seleccionada pasa a ser **Memcached** en lugar de Redis, para alinearse con las tecnologías trabajadas en clase. Se utilizará únicamente como caché descartable; nunca como fuente de verdad.
4. OpenSearch conserva el índice de lectura y deberá indexar también el arreglo `keywords` de cada tratamiento.

La separación lógica no implica por sí sola alta disponibilidad ni aislamiento físico. El esquema, las credenciales, los TTL y la estrategia de invalidación se especificarán en la implementación.

## Validación incremental — Entrega 2 (23/10/2026)

**Estado de la validación:** comprobada para el flujo de lectura de Catálogo; implementación parcial del diseño. Se conservan la decisión inicial y su enmienda, sin dar por implementadas todas las bases previstas.

- Se verificó el flujo cliente HTTP → NGINX → Catálogo → MongoDB, recuperando el documento preexistente `test-001` desde `catalogo_db.tratamientos`. El detalle, el listado y las dos réplicas consultan datos reales.
- Se agregaron pruebas automatizadas de integración de solo lectura y evidencia reproducible. No se modificó el servicio, no se insertaron ni eliminaron registros y no se alteraron índices o volúmenes.
- Se inspeccionaron el índice único `uq_tratamientos_id` y el volumen persistente montado en `/data/db`. La coincidencia de lecturas no acredita escrituras concurrentes, transacciones ni recuperación ante reinicio de MongoDB.
- `profesionales_db`, las bases PostgreSQL privadas y sus permisos siguen pendientes. La configuración inicial de PostgreSQL declara una base `melt`, pero Reservas y Beneficios todavía no utilizan almacenamiento.
- Las garantías para agenda y saldos, la coordinación entre servicios y la sincronización de copias derivadas mantienen su estado de diseño pendiente de validación. No se incorporan mecanismos nuevos ni se modifica la comparación histórica de alternativas.

Los patrones de acceso, resultados ejecutados y limitaciones se detallan en [Validación de persistencia](../PERSISTENCE.md), con [evidencia de la ejecución](../evidence/d3-2026-10-23.json). Esta validación cubre D3 y el almacenamiento integrado exigido para la Entrega 2; no acredita la integración de los dos tipos de almacenamiento requerida para la etapa posterior.
