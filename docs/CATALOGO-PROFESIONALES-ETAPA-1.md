# Catálogo y Profesionales — etapa 1 (Sofía)

## Base y alcance

Rama local `feature/sofi-catalogo-profesionales`, creada desde la rama remota
`origin/feature/leti-beneficios-ghostkitchen`, HEAD `c91da99` al comenzar.
No se modifican Auth, Beneficios, Reservas, pagos, RabbitMQ, GhostKitchen ni
Metalfitness. No se despliega, no se fusionan ramas y no se activa búsqueda
indexada ni caché.

La base ya tenía listado y detalle de tratamientos, filtros por nombre/keywords,
categoría y precio máximo, paginación y ordenamiento, alta y reemplazo de
tratamientos, listado y alta de profesionales, autenticación administrativa
contra Auth, MongoDB, salud y observabilidad. Las categorías eran una lista fija;
las asociaciones se editaban por separado y podían quedar inconsistentes.

## API

Base vía Gateway existente: `/api/v1/catalogo`. El servicio directo escucha en
`PORT` (8081 por defecto) y recibe los mismos paths sin ese prefijo.

| Recurso | Consulta pública | Administración con Bearer |
|---|---|---|
| Tratamientos | GET /tratamientos, GET /tratamientos/{id} | POST /tratamientos, PUT /tratamientos/{id}, DELETE /tratamientos/{id} |
| Categorías | GET /categorias, GET /categorias/{id} | POST /categorias, PUT /categorias/{id}, DELETE /categorias/{id} |
| Profesionales | GET /profesionales, GET /profesionales/{id} | POST /profesionales, PUT /profesionales/{id}, DELETE /profesionales/{id} |

Se conservan los alias existentes de tratamientos y listado de profesionales.
Para las rutas nuevas utilizar el prefijo canónico `/api/v1/catalogo`, que ya
está soportado en ambos archivos NGINX. No se modifican los destinos de Render.

[Contrato administrativo](contracts/catalogo-admin-v1.openapi.json) y
[contrato público de consultas](contracts/tratamientos-v1.openapi.yaml).
La ampliación pública a categorías dinámicas se documenta como versión 1.1.0:
`categoria` sigue siendo un nombre, pero ya no es un enum cerrado.

Ejemplo de creación de categoría:

```json
{"id":"capilar","nombre":"Capilar","descripcion":"Cuidado del cabello"}
```

Primero crear los recursos sin asociaciones y luego asociarlos desde cualquiera
de los extremos; no hace falta crear ambos lados manualmente:

```json
{"id":"prof-001","nombre":"Profesional de prueba","tratamientosIds":[]}
```

```json
{"id":"trat-001","nombre":"Limpieza facial","descripcion":"Limpieza profunda","categoria":"Facial","precio":15000,"moneda":"ARS","duracionMinutos":60,"imagenes":[],"keywords":["piel"],"profesionalesIds":["prof-001"]}
```

POST devuelve 201; PUT devuelve 200 y reemplaza la representación completa.
PUT permite omitir el ID del cuerpo; si se incluye debe coincidir con la ruta.
Listas omitidas se interpretan como vacías; `activo` de profesionales vale true
si se omite. Se puede reactivar un profesional conocido mediante PUT.
Profesionales inactivos no admiten asociaciones vigentes.

Validaciones: JSON de hasta 64 KiB, Content-Type application/json (admite charset),
campos con mayúsculas exactas, sin propiedades desconocidas, repetidas o null.
IDs opacos: 1–128 caracteres UTF-8, conservando compatibilidad con IDs heredados;
se rechazan controles, separadores de ruta y espacios en los extremos.
Nombre de tratamiento/profesional: hasta 200 caracteres; descripción: hasta 5000.
Categoría: nombre hasta 100 y descripción hasta 1000. Precio finito >= 0, moneda
ARS, duración entera de 5–480 minutos, compatible con Reservas. Hasta 20 URLs HTTP(S) absolutas de imágenes
sin credenciales, 100 keywords de hasta 100 caracteres, 200 IDs asociados; los IDs
repetidos se normalizan a una sola asociación. Todos los asociados deben existir
y estar activos. Nombre de categoría único sin distinguir mayúsculas.

Errores JSON con `error.code` y `error.message`: 400 datos/asociaciones inválidos,
401 sin sesión, 403 sin rol, 404 recurso o categoría inexistente, 409 duplicado o
categoría en uso, 413 cuerpo excesivo, 415 Content-Type inválido, 503 dependencia
no disponible. Los errores no revelan detalles de MongoDB ni credenciales.
Todas las escrituras verifican Bearer con `/verify-admin`; no confían en
`X-Auth-User-*` ni siguen redirecciones de Auth.

## Integridad y bajas

- Categorías persistidas en `categorias`, con las cuatro iniciales insertadas
  solo si faltan. Reiniciar no revierte ediciones ni restaura categorías dadas de baja.
- Editar el nombre de una categoría actualiza los tratamientos vigentes dentro
  de la misma transacción, preservando el campo público `categoria`.
- Guardar un tratamiento sincroniza `tratamientosIds` de sus profesionales;
  guardar un profesional sincroniza `profesionalesIds` de los tratamientos.
- DELETE aplica baja lógica y devuelve 204. Listados/detalles públicos excluyen
  bajas; repetir la baja responde 404. IDs y nombres únicos quedan reservados.
- No se borra una categoría con tratamientos vigentes: responde 409.
- La baja de profesional retira sus aptitudes de los tratamientos. La baja de
  tratamiento retira sus referencias de los profesionales. Se conservan los
  documentos, pero esto no es un historial versionado de todas las ediciones.
- Todas las mutaciones utilizan transacciones MongoDB con lectura snapshot y
  escritura majority. Un documento `control_catalogo/escrituras` serializa las
  escrituras administrativas entre réplicas para evitar referencias colgantes
  en carreras entre creación, edición y baja. Es apropiado para administración
  de bajo volumen; no se presenta como una solución de alto throughput.

Decisión conservadora ante PD-04: no se modifican, cancelan ni reprograman reservas
existentes. Reservas conserva sus snapshots y su autoridad de agenda. Un tratamiento
retirado o una aptitud removida ya no debe habilitar nuevas operaciones; una
reserva/bloqueo todavía en validación puede ser rechazada al consultar el catálogo
vigente. Cualquier política comercial adicional sobre reservas existentes requiere
acuerdo del equipo y trabajo en Reservas, fuera de esta etapa.

Las reglas de puntos por tratamiento ya pertenecen a Beneficios en `c91da99`
(`reglas_puntos_tratamientos`). Se conserva esa implementación y su integración con
RabbitMQ: este CRUD no introduce un segundo valor de puntos ni modifica canjes.
Los documentos previos se conservan; no se hace una migración masiva de asociaciones
históricamente inconsistentes. Guardar cada recurso reconcilia sus asociaciones.

## Configuración

- `MONGODB_URI`: instancia con replica set (el Compose actual ya lo configura).
- `AUTH_URL`: Auth, default http://auth:8084.
- `CATALOGO_MONGODB_DATABASE`: default catalogo_db; contiene tratamientos,
  categorías y control de escrituras.
- `PROFESIONALES_MONGODB_DATABASE`: opcional, usa la base de Catálogo si está vacío.
  Se conserva catalogo_db como ubicación predeterminada de los perfiles existentes.
  En una instalación nueva puede usarse profesionales_db. Cambiarlo en una
  instalación con datos exige migración explícita; no se migra ni oculta información
  automáticamente. Las pruebas cubren transacciones entre ambas bases.
- `PORT`: default 8081. Variables de observabilidad existentes sin cambios.
- `OPENSEARCH_URL`: default `http://opensearch:9200`.
- `MEMCACHED_ADDRESS`: default `memcached:11211`; `MEMCACHED_NAMESPACE`: default
  `melt:catalogo`, compartido por las réplicas del mismo entorno. Usar otro namespace
  para pruebas aisladas.

MongoDB es la única fuente de verdad. Si no hay soporte transaccional, las
escrituras fallan con 503; no se degradan a varias escrituras no atómicas.

## Pruebas reproducibles

Con un replica set aislado, en services/catalogo-profesionales:

```powershell
$env:CATALOGO_TEST_URI='mongodb://127.0.0.1:27127/?directConnection=true&replicaSet=sofiTest'
go test -race -tags catalogointegration ./... -count=1
go vet ./...
```

Las nuevas pruebas crean bases `sofi_catalogo_test_<timestamp>` y su base de
profesionales; eliminan únicamente esas bases al finalizar. No usan catalogo_db
ni datos existentes. Sin URI explícita, las pruebas etiquetadas fallan con un
mensaje de configuración. El job `catalogo-transactions` de CI levanta su propia
instancia desechable; no se alteran los jobs previos.

Cobertura: ciclo CRUD, categorías iniciales/dinámicas, conflictos, renombrado,
filtros/paginación, asociaciones bidireccionales, bajas, referencias inválidas,
rollback, carreras entre baja y alta/asociación, validación JSON y autorización.
El test histórico `TestPersistenceReadOnly` (tag integration) requiere una fixture
y Gateway preexistentes y se conserva sin cambios; no se ejecuta contra servicios
ni bases del equipo para esta etapa.

Las pruebas reales de OpenSearch y Memcached se activan además con la etiqueta
`catalogosearchintegration`; requieren endpoints de prueba separados:

```powershell
$env:CATALOGO_TEST_URI='mongodb://127.0.0.1:27127/?directConnection=true&replicaSet=sofiTest'
$env:CATALOGO_TEST_OPENSEARCH_URL='http://127.0.0.1:9200'
$env:CATALOGO_TEST_MEMCACHED_ADDRESS='127.0.0.1:11211'
go test -race -tags 'catalogointegration catalogosearchintegration' ./... -count=1
go test -tags 'catalogointegration catalogosearchintegration' ./cmd/api -run '^$' -bench '^BenchmarkCatalogListCache$' -benchtime=30x -count=3
```

Estas pruebas crean bases Mongo temporales y alias/índices OpenSearch propios de
test; los eliminan al finalizar. No apuntan al alias operativo de Catálogo.

## Etapa 1: búsqueda y caché

MongoDB sigue siendo la fuente de verdad. Catálogo reconstruye OpenSearch al iniciar
y cada 30 segundos. Un lease en `control_catalogo` evita reconstrucciones
simultáneas entre réplicas. Cada generación construye índices nuevos y actualiza
los alias de tratamientos y profesionales solo después de completar la carga; una
generación fallida no reemplaza el índice vigente. Se indexan tratamientos activos
y profesionales habilitados. Tratamientos indexa nombre, descripción, keywords,
categoría, precio, duración e IDs; profesionales indexa nombre, descripción,
aptitudes y estado activo.

`GET /tratamientos` conserva su respuesta. Con `q` usa OpenSearch y mantiene
categoría, precio máximo, paginación y orden. Sin `q` usa MongoDB. Si OpenSearch no
responde, la búsqueda textual devuelve 503; las listas sin `q` siguen disponibles
desde MongoDB. Catálogo no sirve un índice cuya última generación válida tenga más
de 60 segundos. Los cambios confirmados en MongoDB permanecen allí aunque falle
OpenSearch y la próxima reconstrucción vuelve a indexar el estado completo. El
objetivo de frescura de 60 s supone reconstrucciones de 30 s que finalicen en hasta
27 s; no es una garantía durante una interrupción prolongada.

La lista pública de tratamientos usa cache-aside en Memcached con TTL de 30 s.
La clave SHA-256 contempla versión, `q`, categoría, precio máximo, página, límite,
orden y sentido; la respuesta lleva un token de generación compartido entre réplicas.
Cada escritura administrativa confirmada y cada reconstrucción rotan ese token.
Memcached tiene un timeout de 50 ms; si no responde, Catálogo continúa
con MongoDB y no cachea errores. Si falla la invalidación, las entradas expiran en
30 s. No se cachean datos privados ni se afecta la autoridad de Reservas.

`GET /metrics` expone hits, misses y errores de caché, fallos de búsqueda y de
reconstrucción, y la marca temporal de indexación válida. Prometheus incluye un
scrape job para este endpoint.

## Resultados ejecutados — 11/11/2026

| Verificación | Resultado |
|---|---|
| Estado inicial de Git | Limpio; base remota c91da99 |
| Catálogo, `go test -race -tags catalogointegration ./... -count=1` | Aprobado: 23 tests principales, 59 contando subtests; sin omisiones |
| Catálogo, `go vet ./...` y `go build ./...` | Aprobados |
| Auth, `go test -race ./... -count=1` con AUTH_TEST_MONGODB_URI | Aprobado, integración Mongo habilitada |
| Beneficios, `go test -race ./... -count=1` con BENEFICIOS_TEST_MONGODB_URI | Aprobado, transacciones/invitaciones habilitadas; tests de GhostKitchen incluidos |
| Reservas, `go test -race ./... -count=1` con RESERVAS_TEST_MONGODB_URI | Aprobado, integración Mongo habilitada |
| OpenAPI público y administrativo | Ambos válidos con openapi-spec-validator |
| Compose con secretos ficticios de validación | Configuración válida |
| git diff --check | Sin errores de espacios |

### Verificación de búsqueda y caché — 11/11/2026

| Verificación | Resultado |
|---|---|
| `go test -race -tags 'catalogointegration catalogosearchintegration' ./... -count=1` | Aprobado contra MongoDB aislado, OpenSearch 2 y Memcached locales; cubre texto, filtros, paginación, orden, altas/ediciones/bajas indexadas, inactivos, reconstrucción fallida y recuperación, hit/miss, invalidación, TTL, parámetros distintos y fallos de dependencias. |
| `go vet ./...` | Aprobado. |
| Benchmark de lista (100 documentos, página 1, límite 20, orden por nombre; 3 × 30 solicitudes por modo) | Mongo sin caché: 4.77, 4.97 y 5.17 ms/op (media 4.97). Memcached: 3.29, 2.99 y 3.10 ms/op (media 3.13), 30 hits y 1 miss de precalentamiento por corrida. Promedio local ~37 % menor; muestra pequeña, no extrapolable a producción. |
| Comando del benchmark | `go test -tags 'catalogointegration catalogosearchintegration' ./cmd/api -run '^$' -bench '^BenchmarkCatalogListCache$' -benchtime=30x -count=3` |

Se ejecutó desde el host contra el MongoDB de prueba aislado y Memcached en loopback.
El benchmark llamó el router Go directamente, precalentó la clave antes de medir el
modo cacheado y usó la misma página/orden en ambos modos. El dataset y la base
temporal se eliminaron al terminar. El resultado mide este entorno y carga; no es
una prueba de carga de red ni evidencia de producción.

Todas las pruebas Mongo utilizaron un contenedor temporal exclusivo, expuesto solo
en 127.0.0.1:27127; no se usaron Atlas ni los volúmenes locales previos. Las pruebas
HTTP de Auth/proveedores usan dobles de prueba cuando lo define cada suite; no
constituyen una llamada real a GhostKitchen ni una prueba de broker RabbitMQ real.
No se corrió el flujo de reservas/pagos en Render ni se modificó ningún despliegue.
La configuración de CI queda preparada localmente; no se afirma una ejecución en
GitHub Actions porque esta etapa no fue subida.

Entrega: cambios guardados localmente en la rama de Sofía, sin commit, push,
merge ni despliegue.
