# Servicio Catálogo y Profesionales

## Responsabilidad, datos y almacenamiento

Dueño de categorías, tratamientos, perfiles profesionales y su asociación. MongoDB contiene la fuente canónica; por defecto `catalogo_db` y colecciones `categorias`, `tratamientos`, `profesionales` y control de sincronización. `PROFESIONALES_MONGODB_DATABASE` permite una base aparte si una migración/instalación lo requiere. Los perfiles guardan `authUserId` internamente, que se omite de las respuestas públicas.

OpenSearch sirve búsqueda textual y filtros del catálogo; el índice se reconstruye desde MongoDB mediante un indexador con lease y alias. Memcached almacena consultas de lista por 30 segundos; cambios administrativos actualizan la generación para invalidar entradas de todas las réplicas. El fallo de Memcached provoca miss/fallback: no es dueño de datos. Si OpenSearch está atrasado/no disponible, búsqueda textual no se debe presentar como actualizada; el handler puede responder 503. Lecturas sin búsqueda pueden usar MongoDB.

## Rutas públicas y administrativas

Prefijo público del Gateway `/api/v1/catalogo`; alias de lectura compatibles en `/api/v1/tratamientos` y `/api/v1/profesionales`.

| Método y ruta | Acceso | Descripción |
|---|---|---|
| `GET /api/v1/catalogo/health` | Público | Salud del proceso/dependencia configurada. |
| `GET /api/v1/catalogo/categorias` / `/{id}` | Público | Lista/detalle de categorías activas. |
| `POST /api/v1/catalogo/categorias` | Administrador/superadministrador | Crea categoría. |
| `PUT /api/v1/catalogo/categorias/{id}` | Administrador/superadministrador | Edita categoría. |
| `DELETE /api/v1/catalogo/categorias/{id}` | Administrador/superadministrador | Baja lógica. |
| `GET /api/v1/catalogo/tratamientos` / `/{id}` | Público | Catálogo paginado y detalle. Acepta búsqueda `q`, `categoria`, `precioMax`, `page`, `limit`, `sort` (`nombre`, `precio`) y `order` (`asc`, `desc`). |
| `POST /api/v1/catalogo/tratamientos` | Administrador/superadministrador | Crea tratamiento. |
| `PUT /api/v1/catalogo/tratamientos/{id}` | Administrador/superadministrador | Actualiza tratamiento. |
| `DELETE /api/v1/catalogo/tratamientos/{id}` | Administrador/superadministrador | Baja lógica. |
| `GET /api/v1/catalogo/profesionales` / `/{id}` | Público | Listado y perfil público; admite filtro `tratamientoId` en lista. |
| `POST /api/v1/catalogo/profesionales` | Administrador/superadministrador | Crea perfil. |
| `PUT /api/v1/catalogo/profesionales/{id}` | Administrador/superadministrador | Actualiza perfil y tratamientos asociados. |
| `DELETE /api/v1/catalogo/profesionales/{id}` | Administrador/superadministrador | Desactivación lógica. |
| `GET /api/v1/catalogo/admin/profesionales` | Administrador/superadministrador | Lista de administración. |
| `PUT /api/v1/catalogo/admin/profesionales/{id}/cuenta` | Administrador/superadministrador | Vincula/desvincula cuenta validada por Auth. |
| `/internal/profesionales/por-cuenta/{authUserId}` | Interno | Resuelve Auth ID a ID del perfil; no se enruta por Gateway público. |

Las rutas exactas se pueden verificar en `services/catalogo-profesionales/cmd/api/main.go` y en la configuración Gateway. El OpenAPI administrativo documenta una parte del API; ver notas de cobertura en [contratos](../contracts/README.md).

## Validaciones, indexación y fallos

IDs son cadenas opacas (no slugs ni enteros), máximo 128 caracteres; colecciones asociadas están limitadas y deduplicadas. La duración admitida para tratamientos es 5–480 minutos. Categorías y tratamientos/profesionales se validan y asociaciones se sincronizan en transacción; una misma prestación puede estar vinculada a varios profesionales. Payloads JSON rechazan campos desconocidos/duplicados. Las listas públicas normalizan arreglos como `[]`.

Las altas/cambios/bajas mantienen sincronía hacia índice y generación de caché; el indexador y la operación de búsqueda tienen métricas expuestas en `/metrics` (ruta interna/servicio; Gateway no publica una ubicación de scrape documentada para usuario). OpenSearch y Memcached no son reemplazo de MongoDB.

Catálogo no publica/consume eventos RabbitMQ de negocio en esta versión; sincroniza su proyección OpenSearch desde MongoDB mediante un indexador residente. Errores típicos: 400 validación/parametría, 401/403 autorización administrativa, 404 entidad ausente, 409 conflicto/duplicado, 422 validación de cuenta al vincular y 503 base/índice/dependencia no disponible. Los códigos exactos están en los handlers y OpenAPI administrativo.

## Configuración y pruebas

Variables: `MONGODB_URI`, `CATALOGO_MONGODB_DATABASE`, opcional `PROFESIONALES_MONGODB_DATABASE`, `AUTH_URL`, `MELT_INTERNAL_KEY`, `OPENSEARCH_URL`, `MEMCACHED_ADDRESS`, `MEMCACHED_NAMESPACE`, `PORT`, `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`.

```sh
cd services/catalogo-profesionales
go test ./...
```

Pruebas con dependencias reales requieren variables `CATALOGO_TEST_URI`, `CATALOGO_TEST_OPENSEARCH_URL`, `CATALOGO_TEST_MEMCACHED_ADDRESS` o `MONGODB_TEST_URI` según el archivo y build tags. Consultar `CATALOGO-PROFESIONALES-ETAPA-1.md`; sus benchmark/resultados son evidencia histórica local, no garantía de producción.
