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
| Listado de tratamientos | http://localhost:8080/api/v1/catalogo/tratamientos |
| Búsqueda | http://localhost:8080/api/v1/catalogo/tratamientos?q=facial |
| Filtro por categoría | http://localhost:8080/api/v1/catalogo/tratamientos?categoria=Facial |
| Paginación y ordenamiento | http://localhost:8080/api/v1/catalogo/tratamientos?page=1&limit=10&sort=precio&order=asc |
| RabbitMQ Management | http://localhost:15672 |
| OpenSearch | http://localhost:9200 |
| Prometheus | http://localhost:9090 |
| Grafana | http://localhost:3000 |

Credenciales de desarrollo por defecto: RabbitMQ `guest/guest`; Grafana `admin/admin_dev_only`. Cambiarlas antes de cualquier entorno compartido. OpenSearch tiene la seguridad deshabilitada solo para desarrollo local.

## Alta de un tratamiento

```sh
curl -i -X POST http://localhost:8080/api/v1/catalogo/tratamientos \
  -H "Content-Type: application/json" \
  -d '{
    "id":"trat-001",
    "nombre":"Limpieza facial",
    "descripcion":"Limpieza profunda del rostro",
    "categoria":"Facial",
    "precio":25000,
    "duracionMin":60,
    "keywords":["limpieza","facial","rostro"]
  }'
```

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

El catálogo persiste tratamientos en MongoDB, ofrece búsqueda textual simple, filtro por categoría/precio máximo, paginación y ordenamiento. RabbitMQ, OpenSearch, Memcached, PostgreSQL y Prometheus están declarados en Compose, pero todavía no están integrados en los flujos de negocio. Reservas y Beneficios continúan siendo esqueletos HTTP. El frontend, autenticación, eventos de dominio, cache hit/miss, métricas de negocio, pruebas de carga y contrato externo desplegado siguen pendientes.
