# Mock de la API de Tratamientos

## Propósito

Mock local en memoria para probar el contrato OpenAPI `tratamientos-v1.openapi.yaml` sin MongoDB. No es la API real ni una URL para que el Grupo 1 acceda remotamente.

## Requisitos y ejecución

Requiere Go 1.26.1 o superior. Desde esta carpeta:

```sh
go run .
```

Queda disponible en `http://localhost:8080`. Si el gateway de MELT ya usa el puerto 8080, detenelo antes de iniciar el mock para evitar conflicto.

## Endpoints

- `GET /api/v1/tratamientos`: lista tratamientos con paginación.
- `GET /api/v1/tratamientos/{id}`: devuelve el detalle del tratamiento.

Parámetros del listado:

| Parámetro | Comportamiento |
|---|---|
| `q` | Busca sin distinguir mayúsculas/minúsculas en nombre y `keywords`. |
| `categoria` | Filtra por categoría, sin distinguir mayúsculas/minúsculas. |
| `precioMax` | Precio máximo inclusivo; debe ser un número no negativo. |
| `page` | Entero positivo; por defecto `1`. |
| `limit` | Entero entre 1 y 50; por defecto `10`. |
| `sort` | `nombre` o `precio`; por defecto `nombre`. |
| `order` | `asc` o `desc`; por defecto `asc`. |

Ejemplo combinado:

```http
GET /api/v1/tratamientos?q=limpieza&categoria=Facial&precioMax=20000&page=1&limit=10&sort=precio&order=asc
```

Los IDs son cadenas como `trat-001`; no deben convertirse a números. Las respuestas incluyen `moneda`, `duracionMinutos`, `imagenes` y `keywords`. Cuando no hay coincidencias, el listado devuelve un arreglo vacío y metadatos de paginación.

## Errores

Los parámetros inválidos devuelven `400` con `error.code = INVALID_PARAMETER`. Un ID inexistente devuelve `404` con `error.code = TRATAMIENTO_NOT_FOUND`. Los métodos no admitidos devuelven `405`.

## Limitaciones

Los datos son fijos y viven en memoria; no se sincronizan con MongoDB. El mock solo sirve para pruebas locales y debe mantenerse alineado con el contrato, pero no sustituye la validación contra la API real. Para la integración del Grupo 1, el Grupo 5 debe publicar la API real en una URL accesible.
