# Mock de la API de Tratamientos

## Descripción

Este proyecto contiene un mock funcional de la API de Tratamientos definida en el contrato OpenAPI `tratamientos-v1.openapi.yaml`.

El mock utiliza datos en memoria y no requiere una conexión a una base de datos.

## Requisitos

- Go 1.26 o superior.

## Ejecución

Desde esta carpeta ejecutar:

    go run .

El servidor queda disponible en:

    http://localhost:8080

## Endpoints

### Listar tratamientos

    GET http://localhost:8080/api/v1/tratamientos

Devuelve los tratamientos disponibles en el mock.

### Buscar por nombre

    GET http://localhost:8080/api/v1/tratamientos?q=limpieza

El parámetro `q` busca únicamente dentro del nombre del tratamiento.

### Filtrar por categoría

    GET http://localhost:8080/api/v1/tratamientos?categoria=Facial

### Filtrar por precio máximo

    GET http://localhost:8080/api/v1/tratamientos?precioMax=20000

### Paginar resultados

    GET http://localhost:8080/api/v1/tratamientos?page=1&limit=10

El valor máximo permitido para `limit` es `50`.

### Ordenar resultados

    GET http://localhost:8080/api/v1/tratamientos?sort=precio&order=asc

Los campos disponibles para ordenar son `nombre` y `precio`.

Los órdenes disponibles son `asc` y `desc`.

### Obtener un tratamiento

    GET http://localhost:8080/api/v1/tratamientos/1

Devuelve el tratamiento correspondiente al identificador indicado.

## Ejemplo de consulta combinada

    GET http://localhost:8080/api/v1/tratamientos?q=limpieza&categoria=Facial&precioMax=20000&page=1&limit=10&sort=precio&order=asc

La consulta combina búsqueda por nombre, categoría, precio máximo, paginación y ordenamiento.

## Errores

El mock utiliza los siguientes códigos HTTP:

- `400` para parámetros inválidos.
- `404` cuando no existe el tratamiento solicitado.
- `500` para errores internos.

Los errores utilizan la siguiente estructura:

    {
      "error": {
        "code": "INVALID_PARAMETER",
        "message": "El parámetro enviado no es válido."
      }
    }

## Datos del mock

Los tratamientos utilizados por el mock son datos de prueba almacenados en memoria.

El mock no representa la persistencia definitiva de MELT y no requiere MongoDB para funcionar.

## Relación con el contrato

El comportamiento del mock debe mantenerse coherente con el contrato definido en:

    ../tratamientos-v1.openapi.yaml