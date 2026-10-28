# API de Tratamientos MELT

## Descripción

La API pública de Tratamientos permite a otros componentes y grupos consultar el catálogo de tratamientos de MELT. Para la consigna de integración, el Grupo 1 (Gimnasio) es consumidor del catálogo del Grupo 5 (MELT).

La API de consulta está versionada como `v1`. La carga administrativa de tratamientos mediante POST existe en la ruta compatible de desarrollo, pero no forma parte del contrato público de solo lectura.

## Versión y acceso

Versión actual: `v1`

Contrato OpenAPI del catálogo: `tratamientos-v1.openapi.yaml`  
Contrato OpenAPI del registro externo: `registro-externo-v1.openapi.yaml`

La versión del documento OpenAPI es `1.0.0` (OpenAPI `3.0.3`). La URL base contractual es `/api/v1`; en el entorno local se utiliza **`http://localhost:8080/api/v1`**. La URL de Internet sigue pendiente: localhost no permite el acceso desde otro grupo. Antes de la integración real, el Grupo 5 debe publicar el gateway en una dirección accesible por el Grupo 1 y comunicar esa URL.

Desde la actualización D8 del 23/10/2026, NGINX publica las rutas contractuales `/api/v1/tratamientos` y `/api/v1/tratamientos/{id}`, y conserva `/api/v1/catalogo/tratamientos` y su detalle como alias compatible. Ambos accesos GET consultan el mismo Catálogo y MongoDB, sin redirecciones ni cambios de esquema. El nuevo alias contractual es de solo lectura y NGINX bloquea POST en esa ruta.

## Endpoints públicos

### Listar y buscar tratamientos

`GET /api/v1/tratamientos`

Permite consultar tratamientos con filtros, paginación y ordenamiento.

| Parámetro | Tipo | Descripción |
|---|---|---|
| `q` | string | Busca por nombre y palabras clave (`keywords`) asociadas al tratamiento |
| `categoria` | string | Filtra por categoría |
| `precioMax` | number | Filtra por precio máximo |
| `page` | integer | Número de página. Por defecto: `1` |
| `limit` | integer | Cantidad de resultados por página. Por defecto: `10`. Máximo: `50` |
| `sort` | string | Campo de ordenamiento: `nombre` o `precio` |
| `order` | string | Orden: `asc` o `desc` |

Categorías disponibles: `Facial`, `Corporal`, `Depilación`, `Manos y uñas`.

Ejemplo:

`GET /api/v1/tratamientos?q=limpieza&categoria=Facial&precioMax=20000&page=1&limit=10&sort=precio&order=asc`

### Obtener un tratamiento

`GET /api/v1/tratamientos/{id}`

Devuelve la información detallada de un tratamiento.

Ejemplo: `GET /api/v1/tratamientos/trat-002`.

## Modelo de respuesta

Un tratamiento contiene identificador, nombre, categoría, descripción, precio, moneda, duración en minutos, imágenes y `keywords`.

```json
{
  "id": "trat-002",
  "nombre": "Limpieza facial profunda",
  "categoria": "Facial",
  "descripcion": "Limpieza profunda e hidratación del rostro",
  "precio": 28000,
  "moneda": "ARS",
  "duracionMinutos": 60,
  "imagenes": [],
  "keywords": ["limpieza", "facial", "hidratacion", "rostro"]
}
```

El listado devuelve un objeto con `tratamientos` (arreglo) y `pagination`, que contiene `page`, `limit`, `total` y `totalPages`.

## Errores

Los errores de la API pública usan la estructura:

```json
{
  "error": {
    "code": "INVALID_PARAMETER",
    "message": "El parámetro enviado no es válido."
  }
}
```

| HTTP | Código | Uso |
|---|---|---|
| `400` | `INVALID_PARAMETER` | Parámetros inválidos |
| `404` | `TRATAMIENTO_NOT_FOUND` | Tratamiento inexistente |
| `500` | `INTERNAL_ERROR` | Error interno |
| `503` | `CATALOGO_NO_DISPONIBLE` | Catálogo temporalmente no disponible |

La ruta POST de carga de tratamientos responde actualmente con algunos errores de formato diferentes (por ejemplo, cuerpo JSON inválido o ID duplicado). Esa operación no está cubierta por el contrato público OpenAPI v1.

## Alcance de la integración con Grupo 1 — Gimnasio

La consigna del proyecto indica que el Grupo 1 consume del Grupo 5 dos capacidades: **catálogo** y **registro con puntos de regalo adaptado a planes y membresías del gimnasio**.

- **Catálogo:** implementado para consulta pública mediante los dos endpoints GET documentados.
- **Registro externo con puntos de regalo:** implementado como `POST /api/v1/beneficios/registro-externo`. Registra el origen `gimnasio`, guarda el plan y la membresía y acredita el regalo configurado para esa combinación. No es parte de la API de Catálogo ni reemplaza la creación de cuenta/autenticación del cliente. Los registros directos no pasan por este endpoint y no reciben este regalo. La operación requiere configurar el código de referido, una clave privada para el consumidor (`X-Partner-Key`) y la tabla de puntos por plan/membresía.
- **Acceso compartido:** la URL local `localhost` solo sirve para pruebas en el entorno local de MELT. La integración entre grupos requiere desplegar el gateway en una URL accesible desde el entorno del Grupo 1.

Los valores concretos de planes, membresías y puntos quedan a cargo de la configuración acordada con el Grupo 1; no hay valores de negocio inventados en el código. El endpoint guarda el registro y el saldo inicial en Beneficios, pero todavía no está integrado con un servicio de usuarios/autenticación, que no existe en el repositorio.

## Mock

El proyecto incluye un mock en `docs/contracts/mock/` para pruebas locales sin MongoDB. Se alineó su modelo de respuesta con el contrato v1: IDs de texto, `keywords`, búsqueda por nombre y palabras clave, filtro de categoría sin distinguir mayúsculas/minúsculas y páginas sin resultados como arreglos vacíos. El mock utiliza datos fijos en memoria; no sustituye la validación de la API real ni es la URL de integración para el Grupo 1.

## Cambios de versión y compatibilidad

La versión actual del contrato es `v1`. Los cambios incompatibles deberán publicarse mediante una nueva versión y comunicarse a los consumidores. Los IDs son cadenas de texto; los consumidores no deben convertirlos a números. El alias agregado es compatible con `v1` y no cambia el esquema de respuesta.


### Registro externo del gimnasio

`POST /api/v1/beneficios/registro-externo`

Body de ejemplo (reemplazar los valores por los del gimnasio):

```json
{
  "solicitudId": "gym-req-0001",
  "nombre": "Cliente de prueba",
  "email": "cliente@example.com",
  "codigoReferido": "CODIGO_CONFIGURADO",
  "plan": "basico",
  "membresia": "mensual"
}
```

La combinación `plan|membresia` debe existir en la configuración `PUNTOS_POR_PLAN_MEMBRESIA`. El consumidor también debe enviar `X-Partner-Key`. La respuesta `201` confirma el registro y el regalo asociado; repetir la misma `solicitudId` devuelve `200` con el registro existente. Email duplicado con otra solicitud devuelve `409`; código inválido devuelve `403`; combinación de plan/membresía no configurada devuelve `422`. Si falta configurar el código, la clave del consumidor o la tabla de puntos, devuelve `503` en lugar de aplicar una regla inventada.
