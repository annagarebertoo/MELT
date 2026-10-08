# API de Tratamientos MELT

## Descripción

La API de Tratamientos permite consultar el catálogo público de tratamientos de MELT.

La capacidad está versionada como `v1` y está destinada al consumo de otros componentes o grupos que necesiten consultar tratamientos disponibles.

## Versión

Versión actual: `v1`

Contrato OpenAPI:

`tratamientos-v1.openapi.yaml`

## Endpoints

### Listar y buscar tratamientos

GET /api/v1/tratamientos

Permite consultar tratamientos con filtros, paginación y ordenamiento.

Parámetros disponibles:

| Parámetro | Tipo | Descripción |
|---|---|---|
| `q` | string | Busca únicamente por el nombre del tratamiento |
| `categoria` | string | Filtra por categoría |
| `precioMax` | number | Filtra por precio máximo |
| `page` | integer | Número de página. Por defecto: `1` |
| `limit` | integer | Cantidad de resultados por página. Por defecto: `10`. Máximo: `50` |
| `sort` | string | Campo de ordenamiento: `nombre` o `precio` |
| `order` | string | Orden: `asc` o `desc` |

Categorías disponibles:

- `Facial`
- `Corporal`
- `Depilación`
- `Manos y uñas`

Ejemplo:

GET /api/v1/tratamientos?q=limpieza&categoria=Facial&precioMax=20000&page=1&limit=10&sort=precio&order=asc

### Obtener un tratamiento

GET /api/v1/tratamientos/{id}

Devuelve la información detallada de un tratamiento.

Ejemplo:

GET /api/v1/tratamientos/1

## Respuesta

Un tratamiento contiene:

- identificador
- nombre
- categoría
- descripción
- precio
- moneda
- duración en minutos
- imágenes

Ejemplo:

{
  "id": 1,
  "nombre": "Limpieza facial profunda",
  "categoria": "Facial",
  "descripcion": "Limpieza profunda de la piel.",
  "precio": 15000,
  "moneda": "ARS",
  "duracionMinutos": 60,
  "imagenes": [
    "https://melt.example.com/images/limpieza-facial.jpg"
  ]
}

## Paginación

La respuesta del listado incluye información de paginación:

{
  "tratamientos": [],
  "pagination": {
    "page": 1,
    "limit": 10,
    "total": 25,
    "totalPages": 3
  }
}

## Errores

La API utiliza una estructura común para los errores:

{
  "error": {
    "code": "INVALID_PARAMETER",
    "message": "El parámetro enviado no es válido."
  }
}

Códigos principales:

| HTTP | Código | Uso |
|---|---|---|
| `400` | `INVALID_PARAMETER` | Parámetros inválidos |
| `404` | `TRATAMIENTO_NOT_FOUND` | Tratamiento inexistente |
| `500` | `INTERNAL_ERROR` | Error interno del servidor |

## Acceso y uso

La capacidad corresponde a la consulta del catálogo público de tratamientos.

Los endpoints definidos en esta versión utilizan operaciones `GET` y no requieren operaciones de escritura ni idempotencia adicional.

Los datos expuestos son únicamente datos públicos del catálogo.

## Mock

El proyecto incluye un mock funcional en:

`docs/contracts/mock/`

El mock permite probar el contrato sin necesidad de una base de datos.

Para conocer cómo ejecutarlo y probarlo, consultar:

`docs/contracts/mock/README.md`

## Cambios de versión

La versión actual del contrato es `v1`.

Los cambios incompatibles con esta versión deberán publicarse mediante una nueva versión de la API y comunicarse a los consumidores.