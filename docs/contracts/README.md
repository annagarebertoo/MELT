# API de Tratamientos MELT

Contratos adicionales para reservas y pagos:

- [Carrito y checkout agrupado de Reservas](reservas-checkout-v1.openapi.yaml).
- [Simulador interno de Pagos](pagos-simulador-v1.openapi.yaml). Sus operaciones requieren `X-Melt-Internal-Key` y no se exponen al frontend.

## Descripción

La API pública permite consultar tratamientos y perfiles profesionales activos de MELT. Para la consigna de integración, el Grupo 1 (Gimnasio) es consumidor del catálogo del Grupo 5 (MELT).

La API de consulta está versionada como `v1`. La carga administrativa de tratamientos mediante POST existe en la ruta compatible de desarrollo, pero no forma parte del contrato público de solo lectura.

## Versión y acceso

Versión actual: `v1`

Contrato OpenAPI del catálogo: `tratamientos-v1.openapi.yaml`  
Contrato OpenAPI de acreditación externa: `acreditar-puntos-v1.openapi.yaml`
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

## Acreditación externa de puntos

`POST /api/v1/beneficios/acreditar-puntos` requiere `X-Partner-Key` y un JSON con
`solicitudId`, `usuarioId` y `puntos` (entero positivo). El consumidor determina la
cantidad. MELT verifica el usuario activo con Auth y suma al saldo existente.

- `201`: nueva operación confirmada; `200`: repetición idéntica, devuelve el resultado original.
- `400`: datos inválidos; `401`: clave ausente/incorrecta; `404`: usuario inexistente/inactivo.
- `409`: solicitudId ya usado con otro usuario o cantidad; `415`: Content-Type distinto de application/json; `422`: saldo fuera de rango.
- `503`: servicio no configurado o dependencia no disponible; reintentar con el mismo solicitudId.

El identificador de solicitud es global entre consumidores y se conserva sin vencimiento.
La respuesta incluye solicitudId, usuarioId, puntos, saldoPuntos, tipo y creadoEn.
El saldo de una respuesta repetida corresponde a esa operación, no al saldo actual.
Las repeticiones confirmadas se resuelven antes de consultar Auth, incluso si la cuenta fue desactivada.

Contrato completo: [acreditar-puntos-v1.openapi.yaml](acreditar-puntos-v1.openapi.yaml).
Operación, migración y pruebas: [ACREDITACION-PUNTOS.md](../ACREDITACION-PUNTOS.md).
El contrato `registro-externo-v1.openapi.yaml` se conserva como histórico retirado;
sus endpoints ya no se publican. Registro y login no acreditan puntos.
El catálogo público y la API externa consumida por MELT conservan sus contratos.

## Mock

El proyecto incluye un mock en `docs/contracts/mock/` para pruebas locales sin MongoDB. Se alineó su modelo de respuesta con el contrato v1: IDs de texto, `keywords`, búsqueda por nombre y palabras clave, filtro de categoría sin distinguir mayúsculas/minúsculas y páginas sin resultados como arreglos vacíos. El mock utiliza datos fijos en memoria; no sustituye la validación de la API real ni es la URL de integración para el Grupo 1.

## Cambios de versión y compatibilidad

La versión actual del contrato es `v1`. Los cambios incompatibles deberán publicarse mediante una nueva versión y comunicarse a los consumidores. Los IDs son cadenas de texto; los consumidores no deben convertirlos a números. El alias agregado es compatible con `v1` y no cambia el esquema de respuesta.


## Autenticación y cuentas

El servicio de autenticación se publica a través del gateway en `/api/v1/auth`. Su contrato versionado está en `autenticacion-v1.openapi.yaml` e incluye registro público de clientes, login con token Bearer, consulta de la cuenta actual y creación protegida de cuentas internas. Los roles permitidos son `cliente`, `profesional`, `recepcion`, `administrador` y `superadministrador`.

El registro público siempre asigna el rol `cliente`; no permite que una persona se otorgue permisos internos. La creación de cuentas internas requiere sesión administrativa, y solo el superadministrador puede asignar roles administrativos. Las credenciales sensibles se configuran mediante variables de entorno, no en el repositorio.

El gateway verifica la sesión en las rutas privadas de Reservas y Beneficios. Propaga identidad verificada a Reservas; Beneficios verifica además el Bearer con Auth e ignora los headers de identidad de la solicitud. Reservas vuelve a aplicar permisos por operación y pertenencia del turno; Catálogo consulta Auth en la red interna antes de aceptar altas administrativas. Los GET de catálogo y perfiles profesionales son públicos.

## Gestión de perfiles y asociación de profesionales

Las operaciones `POST /api/v1/catalogo/profesionales`, `POST /api/v1/catalogo/tratamientos` y `PUT /api/v1/catalogo/tratamientos/{id}` son administrativas, no parte del contrato público de solo lectura. Requieren Bearer válido con rol administrador o superadministrador. Para habilitar reservas, el ID debe figurar en `profesionalesIds` del tratamiento y en `tratamientosIds` del perfil (el CRUD mantiene ambas listas automáticamente), y Reservas debe tener una agenda activa para el mismo ID.

La guía de [configuración y prueba del flujo web de reservas](../RESERVAS-WEB.md) documenta el orden de carga, la agenda y el pago de prueba.


### Flujo por email y registro seguro

La API v1.1 admite `email` en lugar de `usuarioId`, `referencia` en lugar de `solicitudId` y `descripcion` opcional, en `/api/v1/beneficios/acreditaciones` o `/acreditar-puntos`. Sin cuenta verificada devuelve 202 con un enlace de registro; al registrarse o iniciar sesión desde ese enlace se vincula y acredita una sola vez. El consumidor entrega el enlace privadamente al destinatario. Configurar `BENEFICIOS_INVITATION_SECRET` y `MELT_PUBLIC_URL` en Beneficios y `BENEFICIOS_URL` en Auth. No hay reglas de membresías ni canje manual de códigos. Ver [manual completo](../ACREDITACION-PUNTOS.md) para contrato, seguridad, vencimiento, recuperación y pruebas.

## Catálogo y Profesionales — CRUD completo

La primera etapa incorpora categorías y completa edición/baja de tratamientos y profesionales. Ver [manual de backend](../CATALOGO-PROFESIONALES-ETAPA-1.md) y [OpenAPI administrativo](catalogo-admin-v1.openapi.json). Las asociaciones se sincronizan transaccionalmente desde cualquiera de los extremos; ya no se necesita mantener ambas listas manualmente. Usar el prefijo /api/v1/catalogo para todas las rutas nuevas.
