# API de Tratamientos MELT

## Descripción

La API pública permite consultar tratamientos y perfiles profesionales activos de MELT. Para la consigna de integración, el Grupo 1 (Gimnasio) es consumidor del catálogo del Grupo 5 (MELT).

La API de consulta está versionada como `v1`. La carga administrativa de tratamientos mediante POST existe en la ruta compatible de desarrollo, pero no forma parte del contrato público de solo lectura.

## Versión y acceso

Versión actual: `v1`

Contrato OpenAPI del catálogo: `tratamientos-v1.openapi.yaml`  
Contrato OpenAPI del registro externo: `registro-externo-v1.openapi.yaml`  
Contrato OpenAPI de autenticación: `autenticacion-v1.openapi.yaml`

La versión del documento OpenAPI es `1.0.0` (OpenAPI `3.0.3`). La URL base contractual es `/api/v1`; en el entorno local se utiliza **`http://localhost:8080/api/v1`**. La URL de Internet sigue pendiente: localhost no permite el acceso desde otro grupo. Antes de la integración real, el Grupo 5 debe publicar el gateway en una dirección accesible por el Grupo 1 y comunicar esa URL.

Desde la actualización D8 del 23/10/2026, NGINX publica las rutas contractuales `/api/v1/tratamientos` y `/api/v1/tratamientos/{id}`, y conserva `/api/v1/catalogo/tratamientos` y su detalle como alias compatible. Ambos accesos GET consultan el mismo Catálogo y MongoDB, sin redirecciones ni cambios de esquema. El nuevo alias contractual es de solo lectura y NGINX bloquea POST en esa ruta.

## Endpoints públicos

### Listar profesionales

`GET /api/v1/profesionales?tratamientoId=trat-002`

Devuelve los perfiles públicos activos, ordenados por nombre. El filtro `tratamientoId` limita el resultado a profesionales cuyo perfil y tratamiento estén asociados. La interfaz ofrece la intersección entre esos perfiles y `profesionalesIds` del tratamiento; una asociación incompleta no habilita horarios.

El contrato expone perfiles públicos de solo lectura. Los campos de `Profesional` están descritos en `tratamientos-v1.openapi.yaml`; `activo` controla la publicación del perfil. La carga o edición administrativa permanece fuera de este contrato público y requiere autorización en la ruta compatible del catálogo.

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

## Integración con Grupo 1 — Gimnasio

La integración contempla dos capacidades de MELT:

- **Catálogo público:** el grupo puede consultar tratamientos mediante GET /api/v1/tratamientos y GET /api/v1/tratamientos/{id}. No requiere autenticación.
- **Registro referido con puntos:** el gimnasio dirige a las personas al formulario de alta de MELT usando un enlace con el código acordado, por ejemplo https://melt.example/registro?referencia=GIMNASIO_MELT. El frontend debe conservar ese valor y enviarlo como codigoReferido en POST /api/v1/auth/registro.

### Regla de puntos

- Una cuenta nueva creada con el código de gimnasio válido recibe **100 puntos**.
- El alta normal, sin código o con código incorrecto, no recibe puntos de referido.
- Una cuenta existente no puede obtener el regalo retroactivamente.
- La acreditación se vincula al ID de usuario de Auth y tiene índice único para evitar duplicados.
- El monto se configura con PUNTOS_REGISTRO_GIMNASIO (100 por defecto); no existe conversión a pesos.
- Si Beneficios no está disponible durante el alta, Auth informa pendiente_reintento_en_login y vuelve a intentar acreditar cuando esa persona inicia sesión. La operación interna es idempotente.

### Contrato de registro

POST /api/v1/auth/registro

```json
{
  "nombre": "Cliente de prueba",
  "email": "cliente@example.com",
  "password": "una-clave-segura",
  "codigoReferido": "GIMNASIO_MELT"
}
```

La respuesta 201 incluye beneficioRegistro: acreditado, pendiente_reintento_en_login o no_aplica. El código no reemplaza el alta normal ni permite que el cliente elija la cantidad de puntos.

Auth se comunica con Beneficios por la red interna de Docker usando MELT_INTERNAL_KEY. El endpoint POST /internal/registro-gym no se publica en el gateway; no debe ser llamado directamente por el frontend ni por el gimnasio.

### Acceso compartido

La URL localhost sirve solo para pruebas en la computadora donde corre Docker. Para que el Grupo 1 integre el sistema, MELT debe desplegar el gateway en una URL HTTPS accesible desde fuera de la red local y compartir la URL base y el contrato OpenAPI. No se debe compartir MELT_INTERNAL_KEY, AUTH_JWT_SECRET ni credenciales de Mercado Pago.

## Mock

El proyecto incluye un mock en `docs/contracts/mock/` para pruebas locales sin MongoDB. Se alineó su modelo de respuesta con el contrato v1: IDs de texto, `keywords`, búsqueda por nombre y palabras clave, filtro de categoría sin distinguir mayúsculas/minúsculas y páginas sin resultados como arreglos vacíos. El mock utiliza datos fijos en memoria; no sustituye la validación de la API real ni es la URL de integración para el Grupo 1.

## Cambios de versión y compatibilidad

La versión actual del contrato es `v1`. Los cambios incompatibles deberán publicarse mediante una nueva versión y comunicarse a los consumidores. Los IDs son cadenas de texto; los consumidores no deben convertirlos a números. El alias agregado es compatible con `v1` y no cambia el esquema de respuesta.


## Autenticación y cuentas

El servicio de autenticación se publica a través del gateway en `/api/v1/auth`. Su contrato versionado está en `autenticacion-v1.openapi.yaml` e incluye registro público de clientes, login con token Bearer, consulta de la cuenta actual y creación protegida de cuentas internas. Los roles permitidos son `cliente`, `profesional`, `recepcion`, `administrador` y `superadministrador`.

El registro público siempre asigna el rol `cliente`; no permite que una persona se otorgue permisos internos. La creación de cuentas internas requiere sesión administrativa, y solo el superadministrador puede asignar roles administrativos. Las credenciales sensibles se configuran mediante variables de entorno, no en el repositorio.

El gateway verifica la sesión en las rutas privadas de Reservas y Beneficios y propaga identidad verificada. Reservas vuelve a aplicar permisos por operación y pertenencia del turno; Catálogo consulta Auth en la red interna antes de aceptar altas administrativas. Los GET de catálogo y perfiles profesionales son públicos.

## Gestión de perfiles y asociación de profesionales

Las operaciones `POST /api/v1/catalogo/profesionales`, `POST /api/v1/catalogo/tratamientos` y `PUT /api/v1/catalogo/tratamientos/{id}` son administrativas, no parte del contrato público de solo lectura. Requieren Bearer válido con rol administrador o superadministrador. Para habilitar reservas, el ID debe figurar en `profesionalesIds` del tratamiento, el perfil debe incluir ese tratamiento en `tratamientosIds` y Reservas debe tener una agenda activa para el mismo ID.

La guía de [configuración y prueba del flujo web de reservas](../RESERVAS-WEB.md) documenta el orden de carga, la agenda y el pago de prueba.
