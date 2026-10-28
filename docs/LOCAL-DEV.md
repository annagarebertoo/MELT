# Entorno local MELT

## Requisitos

- Docker Desktop con Docker Compose.
- Go 1.26.1 si se quiere ejecutar los servicios fuera de Docker.

## Arranque

Desde la raíz del repositorio:

```sh
docker compose config
docker compose up --build -d
docker compose ps
```

El primer arranque descarga imágenes y compila los servicios. Para detener el entorno sin borrar datos:

```sh
docker compose down
```

Para borrar también los datos persistidos de desarrollo (acción destructiva):

```sh
docker compose down -v
```

## URLs locales

| Componente | URL |
|---|---|
| API Gateway | http://localhost:8080 |
| Salud del gateway | http://localhost:8080/health |
| Listado público de tratamientos (contrato v1) | http://localhost:8080/api/v1/tratamientos |
| Detalle público de tratamiento (contrato v1) | http://localhost:8080/api/v1/tratamientos/{id} |
| Listado mediante alias compatible | http://localhost:8080/api/v1/catalogo/tratamientos |
| Búsqueda | http://localhost:8080/api/v1/catalogo/tratamientos?q=facial |
| Filtro por categoría | http://localhost:8080/api/v1/catalogo/tratamientos?categoria=Facial |
| Paginación y ordenamiento | http://localhost:8080/api/v1/catalogo/tratamientos?page=1&limit=10&sort=precio&order=asc |
| RabbitMQ Management | http://localhost:15672 |
| OpenSearch | http://localhost:9200 |
| Prometheus | http://localhost:9090 |
| Grafana | http://localhost:3000 |

Credenciales de desarrollo por defecto: RabbitMQ `guest/guest`; Grafana `admin/admin_dev_only`. Cambiarlas antes de cualquier entorno compartido. OpenSearch tiene la seguridad deshabilitada solo para desarrollo local.

## Alta de un tratamiento desde Postman

La creación de tratamientos funciona en el servicio de Catálogo, a través de la ruta compatible `POST /api/v1/catalogo/tratamientos`. La ruta contractual `/api/v1/tratamientos` es pública y de solo lectura; el gateway bloquea POST en ese alias. No confundir el endpoint administrativo/local de carga con la API GET que consume otro grupo.

- Método: `POST`
- URL: `http://localhost:8080/api/v1/catalogo/tratamientos`
- Header: `Content-Type: application/json`
- Body: `raw` → `JSON`

Ejemplo válido (cambiar el ID si ya existe; el campo es `duracionMinutos`, no `duracionMin`):

```json
{
  "id": "trat-002",
  "nombre": "Limpieza facial profunda",
  "descripcion": "Limpieza profunda e hidratación del rostro",
  "categoria": "Facial",
  "precio": 28000,
  "moneda": "ARS",
  "duracionMinutos": 60,
  "imagenes": [],
  "keywords": ["limpieza", "facial", "hidratacion", "rostro"]
}
```

Resultado esperado: `201 Created` y el tratamiento creado en el cuerpo de la respuesta. Si se repite un ID existente, la API responde `409 Conflict`. Los campos obligatorios incluyen `id`, `nombre`, `descripcion`, `categoria`, `moneda: "ARS"`, `precio` no negativo y `duracionMinutos` positivo. Si no se envían `imagenes` o `keywords`, el servicio los inicializa como arreglos vacíos.

## Escalar el catálogo

```sh
docker compose up --build -d --scale catalogo-profesionales=2 catalogo-profesionales gateway
docker compose ps
```

El gateway usa resolución DNS de Docker y un upstream dinámico de NGINX. La tolerancia a la caída de una instancia debe comprobarse con el procedimiento de [POSTMORTEM.md](POSTMORTEM.md); no se considera validada por tener la configuración escrita.

## Pruebas Go

```sh
cd services/catalogo-profesionales
go test -v ./...
```

Los tests unitarios del catálogo cubren el endpoint de salud mediante una dependencia simulada, el parseo de paginación y funciones auxiliares. Todavía falta una prueba de integración contra MongoDB real y pruebas de los flujos completos de reservas y beneficios.

## Alcance y limitaciones actuales

El catálogo persiste tratamientos en MongoDB, ofrece búsqueda textual simple, filtro por categoría/precio máximo, paginación y ordenamiento. La creación mediante POST está implementada en la ruta compatible indicada arriba; la API pública contractual v1 expone solo lectura. RabbitMQ, OpenSearch, Memcached, PostgreSQL y Prometheus están declarados en Compose, pero todavía no están integrados en los flujos de negocio. Reservas y Beneficios continúan siendo esqueletos HTTP. El registro de clientes, el enlace de registro referido por el Grupo 1 (Gimnasio), la asignación de puntos de regalo por referido, el frontend, autenticación, eventos de dominio, cache hit/miss, métricas de negocio, pruebas de carga y contrato externo desplegado siguen pendientes.
