# API de Tratamientos MELT

## Descripción

La API de Tratamientos permite consultar el catálogo público de tratamientos de MELT.

La capacidad está versionada como `v1` y está destinada al consumo de otros componentes o grupos que necesiten consultar tratamientos disponibles.

## Versión

Versión actual: `v1`

Contrato OpenAPI:

`tratamientos-v1.openapi.yaml`

La versión del documento OpenAPI es `1.0.0` (OpenAPI `3.0.3`). La URL base contractual es `/api/v1`; en el entorno local se utiliza **`http://localhost:8080/api/v1`**. La URL de Internet sigue pendiente: localhost no permite el acceso desde otro grupo.

Desde la actualización D8 del 23/10/2026, NGINX publica las rutas contractuales y conserva `/api/v1/catalogo/tratamientos` y su detalle como alias previo compatible. Ambos accesos consultan el mismo Catálogo y MongoDB, sin redirecciones ni cambios de esquema. El nuevo alias admite consultas GET (NGINX también permite HEAD); no publica las operaciones administrativas de escritura. La ruta anterior mantiene su configuración.

## Endpoints

### Listar y buscar tratamientos

GET /api/v1/tratamientos

Permite consultar tratamientos con filtros, paginación y ordenamiento.

Parámetros disponibles:

| Parámetro | Tipo | Descripción |
|---|---|---|
| `q` | string | Busca por nombre y palabras clave (`keywords`) asociadas al tratamiento |
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
- `keywords`: arreglo de términos relacionados y sinónimos utilizados para la búsqueda

Ejemplo:

{
  "id": "1",
  "nombre": "Limpieza facial profunda",
  "categoria": "Facial",
  "descripcion": "Limpieza profunda de la piel.",
  "precio": 15000,
  "moneda": "ARS",
  "duracionMinutos": 60,
  "imagenes": [
    "https://melt.example.com/images/limpieza-facial.jpg"
  ],
  "keywords": ["limpieza", "limpieza profunda", "higiene facial"]
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
| `503` | `CATALOGO_NO_DISPONIBLE` | Catálogo temporalmente no disponible |

## Acceso y uso

La capacidad corresponde a la consulta del catálogo público de tratamientos.

Los endpoints definidos en esta versión utilizan operaciones `GET` y no requieren operaciones de escritura ni idempotencia adicional.

Los datos expuestos son únicamente datos públicos del catálogo.

Las consultas GET del entorno local no requieren autenticación. Se propagan `X-Request-ID` y el contexto W3C `traceparent`; el gateway devuelve `X-Request-ID` y `X-Trace-ID`. Esto facilita correlación, pero no sustituye una política de acceso para el despliegue compartido. Los errores de aplicación usan el esquema anterior; una falla del proxy puede devolver 502/504 con formato propio de NGINX, todavía sin normalizar ni probar mediante caída controlada en D8.

## Mock

El proyecto incluye un mock funcional en:

`docs/contracts/mock/`

El mock permite probar el contrato sin necesidad de una base de datos.

Para conocer cómo ejecutarlo y probarlo, consultar:

`docs/contracts/mock/README.md`

**Limitación conocida:** el mock histórico no es una implementación conforme a la versión actual: devuelve IDs numéricos, omite `keywords`, busca solo por nombre y rechaza algunas páginas que la API real devuelve vacías. No debe usarse como evidencia de conformidad de la API real. No se modificó su código durante esta corrección de enrutamiento. Además utiliza el puerto 8080; no ejecutarlo junto al gateway en ese puerto.

## Cambios de versión

La versión actual del contrato es `v1`.

Los cambios incompatibles con esta versión deberán publicarse mediante una nueva versión de la API y comunicarse a los consumidores.

El alias agregado es compatible con `v1` y no cambia el contrato OpenAPI. La implementación real utiliza IDs de texto, incluido el registro local `test-001`; los consumidores no deben convertirlos a números. El ejemplo anterior se corrigió para reflejar el tipo ya establecido en el contrato.

## Validación D8 — Entrega 2

La discrepancia inicial quedó comprobada: `/api/v1/tratamientos` devolvía 404 de NGINX y la ruta con `/catalogo/` devolvía 200. La corrección agrega únicamente un alias de lectura en NGINX hacia el mismo upstream, con la misma propagación de correlación y trazas. Se validó la configuración y, con autorización, se realizó una recarga gradual de NGINX; no se reiniciaron ni recrearon contenedores.

Para reproducir las pruebas, desde la raíz y con el entorno existente en funcionamiento, usar Python 3 y las dependencias de prueba aisladas en una carpeta temporal:

```powershell
python -m pip install --target "$env:TEMP\melt-d8-contract-tools" -r scripts/requirements-contract.txt
$env:PYTHONPATH = "$env:TEMP\melt-d8-contract-tools"
python -X utf8 scripts/verify-shared-capability.py
```

El entorno debe tener gateway, Jaeger, MongoDB, dos réplicas de Catálogo y el documento `test-001` con keywords. El verificador no inicia servicios ni prepara datos; si falta esa muestra, falla sin crearla. `--output` permite guardar otra captura en un archivo elegido. No se deben sobrescribir las evidencias históricas.

La [evidencia D8](../evidence/d8-2026-10-23.json) registra 28 casos sobre la ruta contractual, con validación de respuestas contra los esquemas OpenAPI y comparación con la ruta anterior. Incluye búsqueda por nombre y keywords, las cuatro categorías, precio máximo, paginación, ambos campos y sentidos de ordenamiento, consulta combinada, ocho errores 400 y un recurso inexistente 404. El verificador también detecta respuestas con ID numérico o sin keywords como inválidas.

Se comprobaron logs correlacionados en las dos capas, propagación de contexto y una traza NGINX → Catálogo → comandos MongoDB. Ambas réplicas recuperaron el documento real y las instantáneas de documentos e índices permanecieron iguales durante los casos principales. Pasaron compilación, tests y `go vet` de Catálogo, integración D3 y los verificadores D3/D5, que incluyen la regresión D11. El mock compiló y pasó `go vet`, pero no tiene tests ni se ejecutó como servidor.

### Límites y pendientes

- La muestra existente contiene un tratamiento. Se comprobaron respuestas y parámetros de ordenamiento; no se demostró el orden relativo entre múltiples documentos. No se insertaron ni eliminaron datos para ampliar la muestra.
- No se provocaron fallas para verificar respuestas 500/503 ni errores del proxy. Se mantienen los límites y pendientes de D3, D5 y D11.
- Se observaron diferencias de entradas límite: una categoría fuera del enum devuelve 200 vacío; `precioMax=NaN` e `Inf` se aceptan aunque no son números válidos del contrato; `page=1000001` se rechaza con 400 aunque OpenAPI no declara ese máximo. Estos casos se registran como observaciones, no como conformidad aprobada. No se cambió la lógica de Catálogo ni se debilitó el contrato para ocultarlos.
- La consulta `q` actual usa MongoDB sobre nombre y keywords; no acredita integración de OpenSearch. El mock requiere alineación posterior con el contrato vigente.
- Falta definir y publicar una URL accesible para el grupo consumidor y la cátedra, acordar condiciones de acceso y asegurar disponibilidad hasta finalizar la evaluación. También falta comprobar el consumo real por el grupo asignado dentro de un flujo importante de su sistema.

Esta validación cubre la capacidad real de consulta mediante el gateway local y actualiza D8 para el 23/10. No acredita publicación en Internet, integración entre grupos ni conformidad exhaustiva para todas las entradas posibles.
