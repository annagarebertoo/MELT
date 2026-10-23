# MELT — Validación de D3: Persistencia

**Responsable:** Sofía. **Entrega:** 2 — 23 de octubre de 2026.

**Rama:** `feature/sofia-d3-persistencia`. **Base D11:** `0273c0b824327ecc480fcf20930fa51ee5d0805e`.

## Alcance y resultado

D3 queda validada para el alcance de esta entrega: Catálogo utiliza MongoDB real en el flujo **cliente HTTP → NGINX → Catálogo → MongoDB**. Se recuperó el tratamiento existente `test-001` y se compararon sus datos con las respuestas del detalle, del listado y de las dos réplicas de Catálogo. El cliente de esta validación es una prueba HTTP; no se acredita un frontend implementado.

El enunciado definitivo `Enunciado TP Final (3).pdf`, apartados 5 (D3) y 6.3, exige justificar almacenamiento, patrones de acceso y limitaciones, validar D3 y tener al menos un tipo de almacenamiento integrado al flujo. El segundo tipo de almacenamiento integrado corresponde a la presentación posterior; esta validación no implementa anticipadamente Reservas ni Beneficios.

No se modificaron el servicio, su configuración, los contratos, el mock ni las reglas de negocio. Las pruebas agregadas son de solo lectura: no ejecutan el arranque de la aplicación, no crean índices y no insertan ni eliminan documentos.

## Decisión y estado real

| Área | Decisión documentada | Implementación y validación al 23/10 |
| :--- | :--- | :--- |
| Catálogo | MongoDB para documentos descriptivos y consultas del catálogo. | Integrado: base `catalogo_db`, colección `tratamientos`, driver oficial MongoDB para Go. Conexión y lecturas reales comprobadas. |
| Profesionales | Base lógica `profesionales_db`, separada de `catalogo_db`, según la enmienda de ADR-003. | Pendiente. La base no aparece en la instancia inspeccionada y no existe un flujo de profesionales implementado. |
| Reservas | PostgreSQL, con datos privados y garantías transaccionales para agenda y estados. | Pendiente. El servicio solo expone salud; no abre conexiones a una base. |
| Beneficios | PostgreSQL, con datos privados y control de duplicación de movimientos y saldos. | Pendiente. El servicio solo expone salud; no abre conexiones a una base. |
| PostgreSQL local | Instancia compartible, pero bases y permisos separados por servicio. | Compose declara una instancia y una base inicial `melt`; no implementa todavía las dos bases privadas ni sus credenciales. No se inició PostgreSQL para esta validación. |
| OpenSearch y Memcached | Copias derivadas, sin autoridad sobre los datos del negocio. | No integrados al flujo comprobado. No se evaluaron sincronización, invalidación ni retraso. |

MongoDB es adecuado para el documento agregado de tratamiento: información descriptiva, precio, duración, imágenes y palabras clave recuperables en una consulta. Los datos disponibles son una muestra funcional de un documento; no acreditan capacidad, rendimiento bajo carga ni un volumen de producción.

### Patrones de acceso existentes

- Detalle por `id`: `FindOne` sobre `catalogo_db.tratamientos`.
- Listado: `CountDocuments` y `Find`, con paginación y ordenamiento por nombre o precio, y desempate por `id`.
- Filtros: categoría, precio máximo y búsqueda simple por nombre o `keywords`. Esta consulta directa a MongoDB no equivale al motor de búsqueda previsto.
- Alta: el código existente utiliza `InsertOne`; no se ejecutó para evitar escrituras sin autorización.
- Índices: se inspeccionó `uq_tratamientos_id`, único sobre `id`. No se probó una inserción duplicada ni se evaluaron índices adicionales o planes de ejecución.

### Propiedad y consistencia

El código inspeccionado de Catálogo accede a su propia colección; no consulta bases de Reservas o Beneficios. Estos dos servicios no tienen acceso a datos implementado. Esto verifica el límite del flujo actual, pero no prueba aislamiento mediante permisos: MongoDB local no configura usuarios por servicio. La separación prevista de bases PostgreSQL y de `profesionales_db` sigue pendiente.

La unicidad de identificadores está respaldada por el índice existente, inspeccionado sin alterarlo. No se acreditan garantías de escrituras concurrentes, idempotencia de altas o transacciones entre servicios. El listado ejecuta conteo y lectura por separado; su coincidencia en esta muestra estable no garantiza una instantánea consistente frente a escrituras concurrentes.

Para agenda y saldos se mantienen las decisiones de datos privados, transacciones locales y prevención de duplicados. La actualización de búsquedas, los efectos de puntos y la publicación confiable de eventos todavía no pueden comprobarse. Outbox y la coordinación o compensación entre servicios conservan el estado pendiente definido en los ADR; no se adoptan nuevos mecanismos en esta etapa.

La comparación histórica de alternativas en ADR-003 no debe interpretarse como ausencia general de garantías ACID en MongoDB. Esta validación no ensaya transacciones multidocumento ni permite inferir garantías de esa clase a partir de una lectura correcta.

## Pruebas reproducibles

Requisitos: Python 3, Go 1.26.1, Docker Compose y el entorno existente en funcionamiento: MongoDB, gateway, Catálogo y, para verificar D11, Jaeger. Ejecutar desde la raíz del repositorio salvo indicación contraria. Los comandos siguientes no arrancan ni reinician servicios ni preparan datos. Si falta `test-001`, las pruebas fallan; se debe solicitar autorización antes de crear un registro.

### Integración de solo lectura y evidencia

```powershell
python -X utf8 scripts/verify-persistence.py
```

El verificador consulta MongoDB mediante `mongosh`, compara los documentos con la API a través de NGINX y con cada réplica, inspecciona el volumen montado en `/data/db` y exige que las instantáneas de documentos e índices permanezcan iguales antes y después de sus consultas. Una escritura concurrente puede provocar una falla de comparación que requiere revisar el entorno.

Para guardar una nueva captura, usar un archivo diferente y conservar la evidencia de la entrega:

```powershell
python -X utf8 scripts/verify-persistence.py --output "$env:TEMP\melt-d3-nueva-evidencia.json"
```

La evidencia contiene identificadores técnicos, hashes y resultados de comparación; no exporta documentos completos, credenciales ni datos personales. El verificador lee documentos de catálogo en memoria para compararlos y calcular la huella.

### Compilación, tests e integración real de Catálogo

```powershell
Set-Location services/catalogo-profesionales
go test -mod=readonly -count=1 -v ./...
go vet -mod=readonly ./...
go build -mod=readonly -o "$env:TEMP\melt-d3-catalogo.exe" ./cmd/api
$env:MONGODB_TEST_URI = 'mongodb://localhost:27017'
$env:GATEWAY_TEST_URL = 'http://localhost:8080'
go test -mod=readonly -tags=integration -count=1 -v ./...
Remove-Item Env:MONGODB_TEST_URI, Env:GATEWAY_TEST_URL
Set-Location ../..
```

La etiqueta `integration` habilita `TestPersistenceReadOnly`, con tres verificaciones: handler contra una colección real, detalle vía gateway igual al documento de MongoDB y total del listado igual al conteo real. No utiliza mock ni ejecuta `main`. Requiere ambas variables explícitas y un registro preexistente; no omite silenciosamente una dependencia faltante.

### Regresión de D11

```powershell
python -X utf8 scripts/verify-observability.py
```

Se reutiliza el verificador de D11 sin modificarlo ni reemplazar su evidencia anterior. Comprueba cuatro variantes de correlación y propagación, las relaciones entre spans de NGINX, Catálogo y el driver MongoDB, privacidad de la telemetría y salud de las dos réplicas.

## Evidencia de la ejecución

Captura del 23/10/2026 a las 17:00 UTC (14:00 de Argentina): [d3-2026-10-23.json](evidence/d3-2026-10-23.json).

| Comprobación ejecutada | Resultado |
| :--- | :--- |
| Compilación de Catálogo, Reservas y Beneficios | Correcta; binarios fuera del repositorio. |
| Tests existentes de Catálogo | 10 tests principales aprobados, incluidos los cuatro de observabilidad. |
| `go vet` de Catálogo | Sin errores. |
| Integración Go contra MongoDB y gateway | Aprobada: tres subtests de `TestPersistenceReadOnly`. |
| Tests existentes de Reservas y Beneficios | Comando correcto, sin archivos de tests; no representa cobertura funcional. |
| Conexión y documento existente | Ping correcto; un documento en `catalogo_db.tratamientos`, identificado como `test-001`. |
| Salud, detalle y listado vía NGINX | HTTP 200; detalle y listado coinciden con MongoDB real. |
| Réplicas de Catálogo | Las dos recuperan el mismo documento almacenado. |
| Almacenamiento | Volumen `melt_melt_mongodb_data` montado en `/data/db`; documentos e índices sin cambios durante el verificador. |
| Regresión D11 | Cuatro casos aprobados; trazas, logs correlacionados, privacidad y dos réplicas saludables. |

El detalle devolvió `Limpieza facial profunda`, precio `15000`, moneda `ARS` y duración `60`, iguales al documento preexistente. Son datos de prueba existentes, no políticas comerciales nuevas. La huella SHA-256 del documento público comparado es `11e424868c49987dc421108ac6471ecaf7748f6e10e8cc01f42db488fc6880dc`.

La solicitud de detalle quedó identificada por `request_id=5a71c59db4c96b3895d81f79552379e6` y `trace_id=3de19902627570256d8c18592b739670`. La regresión D11 comprobó, entre otras, la traza `a3bde1a6949e13b67f29c419875eeac5` con spans de gateway, Catálogo y operaciones reales del driver MongoDB. Las trazas de Jaeger residen en memoria y pueden desaparecer al reiniciar ese componente; los resultados de D3 quedan conservados en el JSON.

El test existente `TestRutaObtenerTratamientoRegistrada` sigue pasando con una colección nula y respuesta 500; no constituye evidencia de persistencia. No se corrigió ese test ajeno: las nuevas pruebas acreditan la lectura satisfactoria por separado.

## Límites y trabajo posterior

- Se comprobó recuperación repetida de datos preexistentes y configuración del volumen. No se insertaron documentos nuevos ni se reinició MongoDB: no se acredita durabilidad ante reinicio, caída abrupta, pérdida de disco o restauración de backups.
- La separación de propietarios es coherente con el código actual, pero faltan usuarios y permisos privados, bases PostgreSQL independientes y el flujo de profesionales.
- Faltan validar escrituras, duplicados bajo concurrencia, migraciones o validadores de esquema, índices según carga, backups, recuperación y necesidades de capacidad.
- Agenda, saldos, eventos, idempotencia y coordinación entre servicios requieren validaciones posteriores sobre implementaciones reales. Una lectura correcta no demuestra que una operación crítica no pueda duplicarse o perderse.
- La ruta real validada es `/api/v1/catalogo/tratamientos`; subsiste la diferencia con la ruta publicada en el contrato. No se modificaron OpenAPI, mock ni el enrutamiento para resolverla.
- D3 cumple el alcance local de persistencia de la Entrega 2; no acredita por sí sola la entrega completa, el despliegue público ni todos los requisitos finales del sistema.
