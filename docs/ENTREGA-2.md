# MELT — Cierre de revisión de la Entrega 2

**Fecha del hito:** 23/10/2026. **Estado del informe:** para revisión; entrega con brechas, no aprobada integralmente.

**Rama:** `feature/sofia-adr-entrega-2`. **Base:** `0940e43092988dcf21f0fd540cb76cbd74617db8`.

## Alcance del cierre

La referencia exclusiva de requisitos es `Enunciado TP Final (3).pdf`. Su apartado 6.3 pide al menos un servicio y la funcionalidad compartida operativos, un almacenamiento integrado, logs estructurados y correlacionados y una primera traza distribuida. También pide D2, D6, D7, D9, D10, D12 y D13; validación de D1, D3 y D5; primera versión de D11 y actualización de D8 si cambió el contrato.

El contenido de cada decisión se evalúa con la tabla del apartado 5: no basta con que exista un archivo. La implementación completa de búsqueda indexada, caché, dos estilos internos, tres servicios de negocio y mensajería se desarrolla para la presentación posterior. El balanceo con caída, las fallas controladas, la carga y la observabilidad completa tienen demostraciones explícitas en la defensa. Esto no permite omitir hoy las decisiones ni declarar como implementadas las propuestas.

La exigencia de publicar la capacidad en un entorno accesible y mantenerla operativa está en el apartado 4. La implementación local no acredita esa accesibilidad. Aunque el cronograma posterior explicita despliegue en nube y URL pública, se registra como pendiente la disponibilidad para el consumidor; no se presume una excepción concedida por la cátedra.

Se preservó el trabajo anterior mediante commits locales: D11 `0273c0b`, D3 `1d5394b`, D5 `3b7e5e4`, D8 `3601c0d` y registro de bloqueo D9 `0940e43`. La rama actual contiene esos commits por ascendencia. `feature/sofia-entrega-2` sigue en `3b7e5e4`: D8 y D9 todavía no fueron integradas allí. No hubo push ni merge en esta revisión.

## Criterios de estado

- **Cumplido:** evidencia suficiente para el alcance preciso indicado en la fila; no implica cumplimiento final del sistema.
- **Parcial:** existe una propuesta, implementación o evidencia útil, pero faltan aspectos para cerrar el requisito de hoy.
- **Bloqueado:** falta información externa imprescindible para decidir o validar.
- **Pendiente:** no se dispone de la evidencia o acción necesaria.

Un responsable no confirmado figura como tal. La autoría de un commit o la revisión documental no se toma como asignación de trabajo futuro.

## Matriz del hito

| Requisito del 23/10 | Estado | Evidencia verificable y alcance | Responsable confirmado | Próxima acción |
| :--- | :--- | :--- | :--- | :--- |
| Al menos un servicio operativo y capacidad propia funcionando | Cumplido, local | Catálogo real, GET contractual por NGINX y MongoDB; [D8](contracts/README.md), [28 casos históricos](evidence/d8-2026-10-23.json) y 12 GET actuales en [evidencia de cierre](evidence/entrega-2-2026-10-23.json). | Sofía, validación D8; mantenimiento futuro no confirmado | Revisar límites conocidos y preparar demostración desde la rama que se acuerde presentar. |
| Capacidad accesible al consumidor, apartado 4 | Pendiente | Solo URL local `http://localhost:8080/api/v1`. No hay URL de Internet ni prueba desde otro grupo. | No confirmado | Definir entorno accesible, condiciones de acceso y continuidad operativa; comprobar acceso del consumidor. |
| Al menos un almacenamiento integrado | Cumplido, flujo actual | MongoDB en Catálogo; [D3](PERSISTENCE.md), [evidencia D3](evidence/d3-2026-10-23.json) y tres subtests de integración real ejecutados de nuevo. | Sofía, validación D3 | Conservar evidencia y datos; segundo motor integrado corresponde a la etapa posterior. |
| Logs estructurados y correlacionados, primera traza | Cumplido, flujo actual | [D11](OBSERVABILITY.md), regresión actual con cuatro casos y traza NGINX → Catálogo → driver MongoDB. No es todavía el flujo de reservas. | Sofía, D11 | Mostrar una traza vigente; métricas, tablero y alertas completos siguen pendientes. |
| D1 — validación de límites | Cumplido, diseño y flujo disponibles | [ADR-001](adr/ADR-001-limites-servicios.md): tres responsabilidades, datos y API de Catálogo coherentes; ninguna consulta cruzada encontrada. Reservas/Beneficios siguen siendo diseño. | No confirmado | Mantener esos límites al implementar agenda y puntos; verificar permisos y flujos futuros. |
| D2 — arquitectura interna | Parcial | [ADR-002](adr/ADR-002-arquitectura-interna.md): capas y hexagonal justificadas como propuesta. Catálogo mezcla handler y colección MongoDB; no hay puertos/adaptadores de negocio. | No confirmado | Ratificar y concretar dependencias internas del diseño. Materializar y demostrar dos estilos después. |
| D6 — búsqueda | Parcial | [ADR-006](adr/ADR-006-busqueda.md): OpenSearch, campos y consultas propuestos. Código actual usa MongoDB; sin índice o sincronizador implementados. | No confirmado | Cerrar mecanismo de actualización/reconstrucción y retraso máximo aceptable; implementación y mediciones posteriores. |
| D7 — caché | Parcial | [ADR-007](adr/ADR-007-cache.md): Memcached y claves de consultas propuestos; sin cliente ni medición de necesidad. | No confirmado | Seleccionar flujo, invalidación, vigencia y degradación; justificar necesidad y plan de métricas. Integrar y medir después. |
| D9 — consumo del proveedor | Bloqueado | [ADR-014](adr/ADR-014-consumo-proveedor.md): sin grupo proveedor, contrato, URL ni operaciones identificadas. ADR-009 es una decisión de negocio diferente. | Sofía, registro de investigación; proveedor y servicio consumidor no confirmados | Confirmar asignación docente y solicitar contrato/URL/acceso. No sustituir por una API inventada. |
| D10 — resiliencia | Parcial | [ADR-010](adr/ADR-010-resiliencia.md): límites reales y errores inspeccionados; test unitario de salud degradada aprobado. Sin prueba de dependencia real caída. | No confirmado | Cerrar política por dependencia, resultados visibles y condiciones de reintento; ejecutar ensayo autorizado y postmortem después. |
| D12 — balanceo | Parcial | [ADR-012](adr/ADR-012-balanceo-carga.md): Catálogo, dos réplicas y NGINX operativos; distribución 6/6 comprobada. Disponibilidad ante caída no demostrada. | No confirmado | Explicitar exclusión/recuperación de destinos. Verificar falla y recuperación en un ensayo autorizado posterior. |
| D13 — capacidad y costos | Parcial | [ADR-013](adr/ADR-013-capacidad-costos.md): método y alternativas de escalado propuestos; instantánea de recursos. No hay carga, límite medido ni estimación de costos. | No confirmado | Definir escenario, objetivo, dimensionamiento y entorno; obtener resultados y estimación sustentada. Acordar alcance de evidencia inicial, sin llamar medición al plan. |
| D3 — validación de persistencia | Cumplido, alcance de Entrega 2 | Decisión y lectura real ya documentadas y verificadas. No se acreditan permisos, escrituras concurrentes o durabilidad ante reinicios. | Sofía, D3 | Mantener límites explícitos y validar garantías finales con implementaciones posteriores. |
| D5 — validación de comunicación | Parcial | [D5](COMMUNICATION.md): HTTP y correlación probados; asincronía solo diseñada, políticas de falla y reintentos aún incompletas. | Sofía, validación D5 | Completar decisiones pendientes de comunicación sin dar por existentes productores/consumidores. |
| Primera versión D11 | Cumplido | Implementación, [ADR-011](adr/ADR-011-observabilidad.md) y pruebas existentes; regresión volvió a pasar. | Sofía, D11 | Preparar evidencia de presentación y completar observabilidad para la etapa final. |
| D8 — revisión/actualización | Cumplido, actualización local | [ADR-008](adr/ADR-008-contrato-propio.md) actualizado y alias contractual operativo. OpenAPI no cambió por el alias; persisten limitaciones documentadas. | Sofía, validación D8 | Resolver publicación y coordinar consumidores; corregir divergencias pendientes con autorización. |

## Lectura de las decisiones revisadas

### D1 y D2: responsabilidades frente a estructura del código

El diseño mantiene Catálogo como autoridad de tratamientos y perfiles, Reservas como autoridad de agenda y Beneficios como autoridad de saldos. El único acceso a datos de negocio implementado e inspeccionado usa `catalogo_db.tratamientos`. La ausencia de accesos cruzados en esqueletos de servicios no demuestra aislamiento por permisos ni garantías de agenda o saldo.

Cada servicio tiene módulo y proceso propios. Eso acredita separación de despliegue, no estilos internos diferentes. `main.go` de Catálogo contiene modelo, HTTP y persistencia; Reservas y Beneficios solo salud. D2 puede documentarse hoy sin implementar los tres servicios, pero debe mostrar qué dependencias se propone separar. La revisión agrega esa precisión conservando los estilos elegidos.

### D6 y D7: tecnologías seleccionadas frente a mecanismos pendientes

El diseño de búsqueda enumera nombre, descripción, categoría y keywords; el endpoint real busca solo en nombre y keywords con MongoDB. Compose declara OpenSearch, pero no hay cliente ni sincronizador. Falta cerrar mecanismo de actualización/reconstrucción y tolerancia de retraso; son aspectos de la decisión, aunque el índice funcional se implemente después.

Memcached está declarado, sin cliente ni uso real. Las claves deben distinguir filtros, páginas y ordenamiento, pero siguen sin elegirse invalidación, TTL y comportamiento ante indisponibilidad. No se asignan valores por conveniencia ni se afirma una mejora de latencia. D7 requiere justificar necesidad observable, no únicamente elegir una tecnología.

### D10 y D12: configuración y distribución frente a falla comprobada

Los timeouts 2s/5s/5s de NGINX están presentes; Catálogo limita operaciones MongoDB y devuelve 503 en salud degradada. El test de esa salud usa una dependencia simulada. No se provocaron vencimientos, errores del proxy ni pérdida de una réplica; `POSTMORTEM.md` continúa como plantilla.

Los 12 GET de esta revisión se distribuyeron 6/6 entre las dos instancias, con upstream, instancia y traza correlacionados. Esto verifica distribución saludable, no detección de caída. No hay healthcheck activo de NGINX explícito en los archivos inspeccionados; tampoco una política explícita `proxy_next_upstream*`. No se supone que eso elimine el comportamiento implícito de NGINX. La política exacta y el ensayo permanecen pendientes.

### D13: inventario frente a capacidad y costo

La evidencia incluye una captura de recursos del motor Docker y de los contenedores, no datos de carga ni costo. El motor informó 16 CPU lógicas y 7.956.668.416 bytes de memoria; son recursos del entorno local, no de una máquina de producción dimensionada. Las réplicas no tienen límites específicos de CPU/memoria configurados. Ninguno de estos datos prueba que soporte cierto número de clientes.

La tabla D13 del enunciado pide resultados de carga, límite identificado, alternativa de escalado y estimación de costos, mientras el cronograma explicita la ejecución del test de carga en la defensa. Para el 23/10 se conserva esa brecha visible: el ADR actual es un plan inicial, no una decisión validada con todos esos elementos. No se da por cerrado D13 ni se inventan importes. Preparar un plan no sustituye los resultados cuando deban evaluarse.

## Evidencia y comandos ejecutados

La [captura de cierre](evidence/entrega-2-2026-10-23.json) fue tomada el 23/10 a las 14:33 de Argentina. Incluye revisión de fuentes, configuración, nombres y estado de contenedores, salud, 12 solicitudes y distribución, además de una instantánea de recursos. No contiene configuración de credenciales. Los hashes identifican archivos de la implementación inspeccionada, no una aprobación de todos los ADR.

Desde la raíz, con el entorno existente en funcionamiento:

```powershell
python -X utf8 scripts/verify-delivery2.py
```

Para otra captura sin reemplazar la evidencia del hito:

```powershell
python -X utf8 scripts/verify-delivery2.py --output "$env:TEMP\melt-cierre-nueva-captura.json"
```

El verificador solo inspecciona Docker y archivos, consulta salud y envía 12 GET secuenciales. No arranca servicios, reinicia contenedores ni inyecta fallas. No es una prueba de carga ni un validador automático de todas las decisiones de diseño. El campo de distribución informa si se observaron ambas réplicas; que existan dos contenedores no garantiza por sí solo que ambos hayan recibido tráfico.

| Verificación ejecutada en esta revisión | Resultado y límite |
| :--- | :--- |
| Lectura completa del PDF definitivo y revisión de siete ADR | Requisitos del hito separados de los finales; no se usaron otras versiones del enunciado. |
| Inspección de código, Compose, NGINX, CI y Prometheus | Catálogo con dependencia directa de MongoDB; dos esqueletos; sin integración OpenSearch/Memcached. CI declara tests y compilación, pero no se ejecutó GitHub Actions ni `-race` en esta revisión. |
| `docker compose ps --all` y configuración | Siete contenedores operativos: gateway, dos Catálogos, MongoDB, Jaeger, Reservas y Beneficios. Sin contenedores de OpenSearch, Memcached, RabbitMQ, PostgreSQL, Prometheus o Grafana. |
| `nginx -T` | Correcto; no recarga el proceso. |
| Salud directa de las dos réplicas | Correcta; el health endpoint consulta MongoDB. |
| Doce GET a `/api/v1/tratamientos` | 12 respuestas 200; distribución 6/6 y correlación comprobadas. |
| `docker stats --no-stream` y recursos del motor | Captura puntual; sin concluir capacidad o costos. |
| `go test -mod=readonly -count=1 -v ./...` en Catálogo | Diez tests principales aprobados, incluido `TestHealthEndpointDegradado` con ping simulado. El test previo de ruta con colección nula sigue pasando con 500 y no se interpreta como persistencia funcional. |
| `go vet -mod=readonly -tags=integration ./...` y compilación de Catálogo | Correctos; binario temporal fuera del repositorio. |
| `TestPersistenceReadOnly` con etiqueta `integration` | Tres subtests aprobados con MongoDB y gateway reales, sin escrituras. |
| `scripts/verify-observability.py` | Cuatro casos aprobados; trazas, privacidad y dos réplicas saludables. Traza de esta ejecución: `f20aa0fc6b3a9abc55558dff7533007d`. |

La regresión de D11 y los comandos Go anteriores se ejecutaron durante esta revisión; sus resultados están resumidos aquí. La captura JSON del verificador de cierre no contiene esas salidas completas. Las evidencias históricas de D3, D5 y D8 se conservan y no se presentan como nuevas ejecuciones de sus verificadores completos. Las trazas de Jaeger residen en memoria y pueden expirar o perderse al reiniciar ese componente.

## Coherencia documental y cambios mínimos

Se conservaron las decisiones y se agregaron revisiones a D1, D2, D6, D7, D10, D12 y D13. En D12 se corrigió la afirmación de que faltaban configuración y réplicas, ya contradicha por la implementación. El resto de los agregados delimita evidencia, alternativas y pendientes, sin introducir valores comerciales o nuevas políticas técnicas cerradas.

La arquitectura conserva afirmaciones históricas desactualizadas sobre ausencia de contratos, servicios o configuración. Se agregó una advertencia con enlace a esta matriz, sin reescribir diagramas o decisiones ajenas. Las enmiendas de negocio posteriores siguen siendo aplicables. README también tiene texto histórico: su actualización general y la preparación final de la presentación quedan pendientes, no se corrigen silenciosamente en esta etapa.

No se modificaron D3, D5, D8, D9, D11, OpenAPI, mocks, lógica de negocio, Compose o NGINX. La ausencia de cambios en esos archivos no elimina sus limitaciones ya documentadas.

## Acciones necesarias antes de presentar

1. Revisar y acordar el cierre de diseño de D2, D6, D7, D10 y D12, con responsables confirmados. Mantener visibles los parámetros o mecanismos que el equipo todavía no resolvió.
2. Desbloquear D9 con asignación docente, contrato, URL y condiciones del proveedor. No marcar como satisfecha esa decisión con un registro de bloqueo.
3. Definir qué evidencia inicial sustentará D13 y cómo completar resultados y costos; no confundir estas consultas secuenciales con carga.
4. Completar decisiones pendientes de D5 y presentar su validación HTTP como parcial, con asincronía solo diseñada.
5. Resolver la accesibilidad de la capacidad para el consumidor y preparar una demostración reproducible con datos existentes, logs y traza vigente. Acordar publicación y acceso con el equipo; no exponer indiscriminadamente las bases del Compose local.
6. Revisar esta etapa y acordar integración/publicación de ramas por el procedimiento del equipo. La evidencia actual vive en la rama de auditoría, no en `develop`, `main` ni toda ella en `feature/sofia-entrega-2`.

Para la etapa final quedan implementación reconocible de dos estilos, servicios de negocio completos, frontend, autenticación, segundo tipo de almacenamiento integrado, índice y caché con mediciones, eventos idempotentes y mensajes fallidos, consumo del proveedor y pruebas de contrato, garantías de la operación crítica, carga/costos, failover y caída controlada con postmortem, además de observabilidad completa. Esta lista no justifica postergar las decisiones de diseño exigidas hoy ni inventa funcionalidades adicionales.
