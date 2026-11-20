# MELT

MELT es una aplicación web para la operación de un centro de estética: ofrece un catálogo de tratamientos y profesionales, agenda y reservas, checkout de prueba y un programa de beneficios. Es el trabajo final de Arquitectura de Software (UCC, 2026).

## Funcionalidades en esta versión

- Registro público e inicio de sesión; administración de cuentas y roles protegida según el rol.
- Categorías, tratamientos y perfiles públicos de profesionales, con asociaciones entre ambos.
- Agenda semanal profesional, consulta de disponibilidad, reservas y gestión de turnos.
- Carrito único por cliente, retenciones temporales de 10 minutos y checkout agrupado.
- Pago simulado, con seña del 30 % y sin cobros reales.
- Puntos y recompensas, canjes, y acreditación externa autenticada.
- Consulta de sedes y platos de GhostKitchen mediante un proxy server-side de Beneficios, si el proveedor está configurado.

El alcance implementado y sus límites, con referencias al código y evidencia, están en [la matriz de cumplimiento](docs/MATRIZ-CUMPLIMIENTO.md). No se debe interpretar una ruta declarada como evidencia de despliegue público.

## Arquitectura y tecnologías

El navegador accede a NGINX, que sirve el frontend HTML/CSS/JavaScript y actúa como Gateway. El Gateway deriva solicitudes a Auth, Catálogo y Profesionales, Reservas o Beneficios. Reservas llama a Catálogo, Auth, Pagos y Beneficios para completar los flujos; publica eventos a RabbitMQ y Beneficios los consume. Auth, Catálogo, Profesionales y Pagos comparten un clúster MongoDB Atlas con bases lógicas separadas. Reservas y Beneficios son dueños de bases PostgreSQL privadas e independientes en Supabase. No se levanta una base de negocio local en Compose.

Tecnologías declaradas: Go/Gin, MongoDB Atlas, PostgreSQL (Supabase), NGINX, Docker Compose, RabbitMQ, OpenSearch, Memcached, Prometheus, Grafana y Jaeger. Más información: [arquitectura y diagramas](docs/ARCHITECTURE.md), [configuración de Atlas](infra/mongodb/README.md) y [configuración de Supabase](infra/supabase/README.md).

## Requisitos previos

- Git y Docker Desktop con Docker Compose v2.
- Go 1.26.1 únicamente para ejecutar servicios o pruebas fuera de Docker.
- Node.js para verificar sintaxis del JavaScript del frontend.

## Inicio local

Desde la raíz, prepará `.env` a partir de `.env.example`, configurá `MONGODB_URI` con la URI del clúster Atlas, aplicá los dos SQL de `infra/supabase/` y configurá `BENEFICIOS_DATABASE_URL` y `RESERVAS_DATABASE_URL` con sus URLs privadas respectivas. Reservas y Beneficios requieren sus PostgreSQL de Supabase; no usan MongoDB. Luego creá los secretos locales fuera del repositorio. En PowerShell:

```powershell
$env:AUTH_JWT_SECRET = [Convert]::ToBase64String([Security.Cryptography.RandomNumberGenerator]::GetBytes(48))
$env:MELT_INTERNAL_KEY = [Convert]::ToBase64String([Security.Cryptography.RandomNumberGenerator]::GetBytes(48))
docker compose config --quiet
docker compose up --build -d
docker compose ps
```

Abrir <http://localhost:8080>. Salud del Gateway: <http://localhost:8080/health>. No uses `docker compose down -v` salvo que quieras borrar deliberadamente los datos de infraestructura local.

Las variables opcionales, pruebas, comandos de diagnóstico, rutas locales y apagado están en [operaciones y ejecución](docs/OPERACIONES.md). Un `.env.example` sirve como referencia, no como lugar para guardar claves reales.

## Documentación

- [Índice integral de documentación](docs/README.md)
- [Manual de usuario](docs/manuales/USUARIO.md)
- [Desarrollo y pruebas](docs/manuales/DESARROLLO.md)
- [Operaciones, configuración y despliegue](docs/OPERACIONES.md)
- [Manual de microservicios y endpoints](docs/servicios/README.md)
- [Contratos y APIs](docs/contracts/README.md)
- [Matriz académica y estado verificable](docs/MATRIZ-CUMPLIMIENTO.md)
- [Troubleshooting](docs/TROUBLESHOOTING.md)
- [Glosario](docs/GLOSARIO.md)
- [Postmortem y evidencia de resiliencia](docs/POSTMORTEM.md)
- [Decisiones arquitectónicas (ADR)](docs/adr/)

## Integraciones y límites relevantes

El servicio Pagos implementa un simulador interno: la variable local `PAYMENT_SIMULATOR_RESULT` controla el resultado de demostración (`approved` por defecto o `rejected`); no hay cobro externo. La integración de puntos para el Grupo 1 se documenta en [Integración Gimnasio](docs/INTEGRACION-GIMNASIO.md). La aplicación incluye un cliente server-side para GhostKitchen, pero una ejecución real requiere configuración y disponibilidad del proveedor; ver [Integración GhostKitchen](docs/GHOSTKITCHEN.md). Los servicios publicados y URLs pueden estar desactualizados: consultar [el estado documentado del despliegue](docs/OPERACIONES.md#entorno-remoto) y verificar el entorno antes de una demostración.

## Equipo y repositorio

Anna Garello Bertorello, Sofía Acuña y Leticia Vega Pereira. Repositorio: <https://github.com/annagarebertoo/MELT>.
