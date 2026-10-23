# MELT — Entrega 2: cierre y demostración

**Hito:** 23/10/2026. **Estado:** preparado para revisión y demostración local; cumplimiento global parcial.

**Rama:** `feature/sofia-adr-entrega-2`. **Base de esta etapa:** `cc230fa` (auditoría protegida). No se crearon más ramas ni se integró a otras.

## Requisitos y alcance

La fuente exclusiva de requisitos es `Enunciado TP Final (3).pdf`. El apartado 6.3 exige un servicio y la capacidad compartida operativos, un almacenamiento integrado, logs estructurados/correlacionados y primera traza; D2, D6, D7, D9, D10, D12 y D13; validaciones D1, D3, D5, primera D11 y actualización D8 si corresponde. La tabla del apartado 5 define el contenido de cada decisión: un archivo existente no basta para darla por cubierta.

La presentación posterior incluye tres servicios completos, frontend, autenticación, dos estilos reconocibles, segundo almacenamiento, búsqueda indexada, caché, eventos y consumo del proveedor. La defensa explicita garantías críticas, carga, fallas controladas, balanceo ante caída y observabilidad completa. La publicación accesible de la capacidad y su continuidad están además en el apartado 4: localhost no acredita ese acceso para otro grupo.

Los números de TTL, retraso, carga, recursos y objetivos agregados en este cierre son decisiones o hipótesis técnicas del proyecto para revisión. No proceden del profesor ni se presentan como mediciones. No se cambió lógica de negocio, OpenAPI, mock, Compose ni NGINX.

## Matriz final de requisitos del 23/10

Cumplido significa evidencia suficiente para el alcance de la fila; parcial indica una brecha; bloqueado indica información externa faltante; pendiente significa que falta la acción o evidencia. El cierre documental no equivale a implementación ni aprobación docente. No se asignan responsables por inferir autoría de commits.

| Requisito | Estado | Evidencia verificable | Responsable confirmado | Próxima acción |
| :--- | :--- | :--- | :--- | :--- |
| Servicio y capacidad compartida operativos | Cumplido localmente | Catálogo, MongoDB y ruta contractual; [28 casos finales D8](evidence/cierre-final-d8-2026-10-23.json). | Sofía: validación D8 | Presentar la demostración local y resolver acceso externo. |
| Capacidad accesible al consumidor y disponible hasta evaluación | Pendiente | Solo URL local; no se comprobó despliegue público. | No confirmado | Seleccionar entorno, publicar de forma segura y verificar desde el grupo consumidor. |
| Un almacenamiento integrado | Cumplido para este flujo | [D3 final](evidence/cierre-final-d3-2026-10-23.json), documento test-001 y comparación con API. | Sofía: D3 | Conservar evidencia y completar garantías finales posteriormente. |
| Logs correlacionados y primera traza | Cumplido para Catálogo | [D5/D11 final](evidence/cierre-final-d5-2026-10-23.json) y traza de D8. | Sofía: D11 | Mostrar traza vigente; métricas/tablero finales pendientes. |
| D1: validación | Cumplido en diseño y flujo disponible | [ADR-001](adr/ADR-001-limites-servicios.md); datos de Catálogo propios, sin acceso cruzado encontrado. | No confirmado | Mantener los límites en futuras implementaciones. |
| D2: decisión | Cumplido documentalmente, para revisión | [ADR-002](adr/ADR-002-arquitectura-interna.md): módulos, responsabilidades y dependencias definidos. | No confirmado | Aplicar y probar capas/hexagonal en el desarrollo posterior. |
| D6: decisión | Cumplido documentalmente, para revisión | [ADR-006](adr/ADR-006-busqueda.md): reconstrucción, generación/alias, objetivo 60s, errores y medición. | No confirmado | Implementar y medir; OpenSearch no integrado hoy. |
| D7: decisión | Cumplido documentalmente, para revisión | [ADR-007](adr/ADR-007-cache.md): cache-aside, claves, TTL 30s, invalidación, caída y métricas. | No confirmado | Contrastar necesidad observable antes de habilitar; medir beneficio real. |
| D9: decisión de consumo | Bloqueado | [ADR-014](adr/ADR-014-consumo-proveedor.md): falta proveedor, contrato, URL y operaciones. | Sofía: registro; servicio consumidor sin asignar | Confirmar asignación y solicitar información; no inventar integración. |
| D10: decisión y validación | Parcial | [ADR-010](adr/ADR-010-resiliencia.md), timeouts, errores y [ensayo real](evidence/fallo-replica-2026-10-23.json): un 504 con réplica sana disponible. | No confirmado | Investigar y acordar política de intentos/degradación; cambios requieren autorización. |
| D12: decisión y validación | Parcial | [ADR-012](adr/ADR-012-balanceo-carga.md): reparto saludable, caída y reincorporación comprobados; no failover sin errores. | No confirmado | Explicitar/validar exclusión y recuperación efectiva; investigar 504. |
| D13: decisión | Parcial | [ADR-013](adr/ADR-013-capacidad-costos.md): hipótesis, recursos, métricas, umbrales y referencia económica verificable; sin carga ni cuello de botella medidos. | No confirmado | Ejecutar carga acordada y contrastar dimensionamiento/costo total. |
| D3: validación | Cumplido para Entrega 2 | Tres subtests de integración real y verificador final aprobados; sin escrituras. | Sofía: D3 | Permisos, concurrencia y durabilidad ante reinicios siguen fuera de esta prueba. |
| D5: validación | Parcial | Seis casos HTTP y regresión D11 aprobados; eventos y política completa de fallas pendientes. | Sofía: validación D5 | Conservar separación entre comunicación real y diseño asíncrono. |
| D11: primera versión | Cumplido | Logs, trazas y tests existentes; regresión aprobada tras restauración. | Sofía: D11 | Completar observabilidad final en su etapa. |
| D8: actualización | Cumplido localmente | Contrato v1/1.0.0 y alias NGINX; ambas rutas coinciden. | Sofía: D8 | Publicar capacidad y resolver divergencias conocidas autorizadamente. |

## Estado de D1 a D13

D1 validada; D2 concretada como diseño; D3 validada en lectura real; **D4 no se cierra en este hito**: consistencia/concurrencia críticas siguen pendientes de implementación y defensa. D5 parcial; D6 y D7 concretadas como diseño, sin motores integrados; D8 operativa localmente; D9 bloqueada; D10 parcial; D11 inicial operativa; D12 parcial; D13 parcial en mediciones, con estimación inicial de recursos y costos. ADR-009 documenta reglas de negocio, no D9: se conserva sin modificaciones.

## Implementado y solamente diseñado

Implementado/comprobado: HTTP Go/Gin de Catálogo, MongoDB real, gateway, dos réplicas, GET contractual y ruta anterior, búsqueda simple MongoDB, filtros, paginación, logs estructurados, correlación y trazas Jaeger. Reservas y Beneficios solo tienen salud HTTP, no flujos de negocio.

Solo diseñado: capas/hexagonal completas, OpenSearch y reconstrucción periódica, caché Memcached y su invalidación, eventos, garantías de agenda y puntos. PostgreSQL, RabbitMQ, OpenSearch, Memcached, Prometheus y Grafana están declarados, no integrados en el flujo probado. D6 define ciclos de 30s y objetivo de visibilidad de 60s; D7 define TTL 30s y objetivo de intento de caché de 50 ms. No se afirma que esos parámetros estén configurados o cumplidos.

D13 propone evaluar 5 solicitudes/s y ráfagas de 20, p95 menor que 500 ms y errores menores que 1 %, sobre un dataset autorizado distinto de la muestra actual de un documento. Presupuesta inicialmente 2 vCPU, 4 GiB y 20 GiB. La referencia comercial nominal es USD 24/mes para un host ilustrativo; el ADR enlaza la tarifa, fecha, inclusiones/exclusiones y escenario de crecimiento. No se contrató nada ni se midió capacidad máxima.

## Pruebas finales y evidencia

| Verificación ejecutada | Resultado |
| :--- | :--- |
| Compilación de Catálogo, tests existentes y go vet con etiqueta integration | Correctos; diez tests principales aprobados. |
| TestPersistenceReadOnly | Tres subtests aprobados con MongoDB y gateway reales. |
| Verificador D3 tras restauración | Aprobado: test-001, listado, detalle, réplicas, volumen y snapshots sin cambios. |
| Verificador D5 tras restauración | Seis casos aprobados: tres 200, dos 400 y un 404, correlación y configuración. |
| Regresión D11 incluida en D5 | Cuatro casos aprobados; privacidad, relaciones entre spans y dos réplicas saludables. |
| Verificador D8 tras restauración | OpenAPI válido y 28 casos aprobados contra esquemas, parámetros, errores y ruta anterior; logs/trazas y MongoDB reales. |
| Distribución saludable previa | Doce GET 200, seis por réplica, en [captura de auditoría](evidence/entrega-2-2026-10-23.json). No es carga. |
| Caída autorizada de réplica 2 | Once 200 y un 504 entre doce GET; máximo 2.004,042 ms. **Criterio estricto de cero errores no aprobado (código 1).** |
| Restauración | Mismo contenedor recuperado; doce GET posteriores 200 y ambas instancias nuevamente observadas. |
| Protección de datos/servicios | Snapshot MongoDB sin cambios; MongoDB, Jaeger y gateway con mismos IDs y horas de inicio durante el ensayo. |

El [postmortem](POSTMORTEM.md) describe hechos, hipótesis de causa y acciones. No se probó una caída total, de MongoDB o proveedor, ni agotamiento de todos los timeouts. No se modificó infraestructura para ocultar el resultado. El usuario autorizó conservar el fallo y confirmar documentación/evidencias si pasaban las regresiones saludables; dichas regresiones pasaron.

Las evidencias D3/D5/D8 finales son ejecuciones nuevas, posteriores al ensayo. Las anteriores se conservan históricamente. La captura general contiene una instantánea de recursos, no una medición de carga. El test unitario de salud degradada usa un ping simulado; el test histórico de ruta con colección nula sigue pasando con 500 y no se usa como evidencia de persistencia.

## Demostración reproducible sin alterar datos

Requisitos: entorno existente con gateway, dos Catálogos, MongoDB, Jaeger y test-001 preexistente. No ejecutar arranques generales o cambios de escala sin revisar el entorno. Desde la raíz:

```powershell
docker compose ps
curl.exe -i http://localhost:8080/api/v1/tratamientos
curl.exe -i http://localhost:8080/api/v1/tratamientos/test-001
curl.exe -i "http://localhost:8080/api/v1/tratamientos?q=facial&categoria=Facial&precioMax=20000&page=1&limit=10&sort=precio&order=asc"
curl.exe -i "http://localhost:8080/api/v1/tratamientos?limit=0"
python -X utf8 scripts/verify-persistence.py
python -X utf8 scripts/verify-communication.py
python -X utf8 scripts/verify-delivery2.py
```

Para las pruebas de esquemas, usar las dependencias de prueba aisladas ya instaladas o prepararlas según la guía de consumo:

```powershell
$env:PYTHONPATH = "$env:TEMP\melt-d8-contract-tools"
python -X utf8 scripts/verify-shared-capability.py
```

La instalación reproducible está en [contratos](contracts/README.md). Ninguno de esos verificadores necesita crear datos. Si falta test-001, fallan sin prepararlo automáticamente. Usar --output con otra ruta para nuevas capturas; no reemplazar las históricas. Jaeger está en http://localhost:16686 y sus trazas son temporales: elegir un trace ID recién emitido por el verificador, no confiar en que una traza vieja siga disponible.

La caída **no forma parte de esta demostración rutinaria**. El script verify-replica-failure.py requiere autorización previa y --execute-authorized; detiene una réplica y la restaura en finally. El ensayo ejecutado ya quedó preservado: no repetirlo para obtener un resultado más favorable sin investigar.

## Limitaciones y dependencias para presentar

- D9 impide declarar todas las decisiones del 23/10 completas. Solicitar asignación docente y al proveedor contrato versionado, URL, operaciones, ejemplos/errores, condiciones de acceso, datos de prueba y garantías de idempotencia cuando correspondan. No hay proveedor supuesto ni cliente externo implementado.
- La capacidad solo está en localhost. Falta URL accesible al consumidor y continuidad; no se expone el Compose local de bases sin preparación.
- D10/D12 recuperan servicio pero mostraron un 504 transitorio. Se desconoce la causa exacta de la ausencia de segundo intento en esa solicitud. La reincorporación fue comprobada; no se garantiza failover sin errores.
- D13 tiene hipótesis y referencia de presupuesto, pero no carga, cuello de botella o costo total medidos. La muestra de un tratamiento no demuestra orden relativo entre documentos ni capacidad de producción.
- Persisten las divergencias de entradas límite y el mock desactualizado detallados en D8. No se modificaron contrato ni negocio para resolverlos en esta etapa.
- D5 mantiene la asincronía como diseño y políticas pendientes. No se acreditan permisos privados, segundo motor integrado, concurrencia, garantías críticas o backups por una lectura correcta.
- D2/D6/D7 son decisiones documentales nuevas para revisión, no implementación ni aprobación docente. README/arquitectura conservan texto histórico señalado en la auditoría; los estados operativos actuales están en este informe y guías específicas.

**Preparación real:** lista para demostrar el flujo local y presentar decisiones, evidencias y limitaciones de manera honesta. **No es una entrega íntegramente completa:** D9, acceso externo y brechas D5/D10/D12/D13 siguen visibles. No afirmar aprobación de la cátedra ni conformidad exhaustiva.

## Git y archivos de esta etapa

Se protegió la auditoría en cc230fa y se trabajó en esta misma rama. Se modificaron D2, D6, D7, D10, D12, D13, este informe y el postmortem; se agregaron el verificador del ensayo y cuatro evidencias nuevas. D1, D3, D5, D8, D9, D11, servicios, contrato, mock y configuración permanecen intactos durante este cierre.

Historial requerido: D11 0273c0b → D3 1d5394b → D5 3b7e5e4 → D8 3601c0d → D9 0940e43 → auditoría cc230fa → cierre. El hash definitivo del cierre se informa después del commit, evitando un identificador autorreferente en el archivo. No se hizo merge ni push y las demás ramas no se movieron.
