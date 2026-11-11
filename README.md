# MELT

**Sistema web de gestión para un centro de estética**  
**Materia:** Arquitectura de Software — 2026  
**Estado:** Flujos locales de identidad, catálogo, reservas y pagos implementados; ver estado y límites vigentes más abajo.

## Descripción

MELT es una plataforma web orientada a la gestión de un centro de estética con una única sucursal. Centraliza la consulta de tratamientos y profesionales, la reserva y administración de turnos y un programa de beneficios basado en puntos.

La operación principal es la **reserva de turnos con validación de disponibilidad**: el sistema debe impedir la confirmación de turnos superpuestos para un mismo profesional. El programa de beneficios permite acreditar puntos por tratamientos efectivamente realizados y utilizarlos para canjear beneficios conforme a reglas definidas por el centro.

## Objetivo

Facilitar la organización de la agenda del centro, brindar a los clientes un mecanismo claro de reserva y seguimiento de sus turnos, y gestionar beneficios de manera consistente y trazable.

## Usuarios

- **Cliente:** consulta tratamientos y profesionales, solicita o cancela turnos y consulta sus puntos y beneficios.
- **Profesional:** consulta su agenda y registra la realización de los tratamientos que le corresponden.
- **Recepción:** gestiona reservas y disponibilidad según los permisos que se definan.
- **Administrador:** administra el catálogo, los profesionales, la disponibilidad, los turnos y las reglas del programa de puntos.
- **Superadministrador:** administra cuentas y asigna roles administrativos.

## Funcionalidades y estado

| Funcionalidad | Estado actual |
| --- | --- |
| Catálogo con búsqueda, filtros, paginación y ordenamiento | Implementada para lecturas locales; usa consultas a MongoDB. OpenSearch no está conectado. |
| Perfiles públicos, selección profesional y consulta de disponibilidad | Implementados; requieren asociación válida y agenda activa. |
| Identidad y permisos por operación | Implementados para las rutas de identidad, administración del catálogo y reservas descritas en la documentación. |
| Reserva, carrito de 10 minutos, seña del 30 %, confirmación y cancelación | Implementados para el recorrido local con adaptador Mercado Pago en modo test. |
| Prevención de cruces y concurrencia | Implementada para el bloqueo/turno de un profesional y cubierta por la prueba de integración de Reservas. |
| Interfaz web de registro, login, perfil, catálogo y reserva | Implementada y servida por Gateway/NGINX. |
| Puntos por tratamientos completados, canjes y notificaciones por correo | Pendientes de implementación en los flujos de negocio. |
| API consumida para búsqueda de sedes cercanas (Cocina Fantasma) | Pendiente de entrega del contrato/API del otro grupo. |
| Consola administrativa web | No incluida; la gestión disponible se realiza por rutas protegidas. |

Las reglas y la verificación por punto están en [SPEC.md](SPEC.md#13-estado-de-implementación-y-verificación). La operación local y el flujo demo están en [docs/LOCAL-DEV.md](docs/LOCAL-DEV.md) y [docs/RESERVAS-WEB.md](docs/RESERVAS-WEB.md).

## Flujo principal: reserva de un turno

1. El cliente busca un tratamiento y consulta su información.
2. Selecciona un profesional y un horario disponible.
3. Solicita la reserva.
4. El sistema valida que el profesional pueda atender el tratamiento, que el horario esté dentro de su agenda y que no se superponga con turnos o bloqueos vigentes de ese profesional.
5. Al agregar el horario al carrito, lo bloquea durante 10 minutos; si el cliente no confirma, lo libera automáticamente.
6. Para confirmar, exige el pago exitoso de una seña equivalente al 30 % del precio del tratamiento y transforma el bloqueo en reserva. Si se cancela con 24 horas o más de anticipación, se devuelve el 100 % de la seña; con menos de 24 horas o ante inasistencia, no se devuelve.
7. La cancelación libera el horario; si se solicita con al menos 24 horas de anticipación, se tramita la devolución de la seña.

La acreditación de puntos por tratamientos completados, el envío de correos y las incompatibilidades entre tratamientos del mismo cliente pertenecen al alcance general, pero todavía no forman parte del flujo local implementado.

## Arquitectura y estado de implementación

El sistema cuenta con un **frontend web**, un **API Gateway** y microservicios con responsabilidades diferenciadas (el mínimo de la consigna es tres; se incorporó además autenticación):

| Componente | Responsabilidad |
| --- | --- |
| Autenticación | Registro e inicio de sesión, identidad de la cuenta y autorización por roles. | 
| Catálogo y Profesionales | Tratamientos, categorías, profesionales, precios, duraciones, palabras clave y puntos configurados. |
| Reservas | Agenda, disponibilidad efectiva, creación, cancelación y finalización de turnos. |
| Pagos | Adaptación de Checkout Pro, verificación de señas y solicitud de devoluciones en Mercado Pago test. |
| Beneficios | Saldos, movimientos de puntos, canjes, aplicaciones y restituciones. |
| API Gateway | Punto de entrada del frontend y enrutamiento hacia los servicios. |

Cada microservicio es propietario de sus datos. La interfaz web está servida por NGINX y usa el gateway para buscar tratamientos, crear cuentas, consultar el perfil y los puntos, buscar disponibilidad, reservar y cancelar turnos. Los límites de datos, comunicaciones y limitaciones vigentes se detallan en [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) y en los ADR.

Las consultas y validaciones actuales usan HTTP síncrono. La arquitectura final también requiere eventos asíncronos, búsqueda indexada, caché y métricas; sus estados de implementación y evidencia se describen en [docs/ENTREGA-2.md](docs/ENTREGA-2.md) y los ADR vigentes.

## Tecnologías

| Área | Tecnología |
| --- | --- |
| Frontend | HTML, CSS y JavaScript servidos por NGINX |
| Backend de microservicios | Go y Gin |
| API Gateway y balanceo | NGINX |
| Persistencia relacional | PostgreSQL |
| Persistencia no relacional | MongoDB |
| Mensajería asíncrona | RabbitMQ |
| Caché | Memcached |
| Motor de búsqueda | OpenSearch |
| Contenedores y ejecución local | Docker y Docker Compose |
| Métricas y tableros | Prometheus y Grafana |
| Versionado y colaboración | Git y GitHub |

La arquitectura interna de los microservicios combinará al menos dos estilos trabajados en la materia; su elección y justificación se documentarán en los ADR correspondientes.

## Autenticación

El servicio de autenticación se publica por el gateway en `/api/v1/auth`. Permite registrar clientes, iniciar sesión y consultar la cuenta actual. Las cuentas internas se crean con permisos administrativos; el registro público no permite elegir roles. Ver [guía local](docs/LOCAL-DEV.md) y [contrato OpenAPI](docs/contracts/autenticacion-v1.openapi.yaml).

Las rutas de negocio protegidas integran autenticación y permisos por operación. Reservas aplica una matriz de roles con denegación por defecto y comprueba además la propiedad de turnos y agendas en cada handler. La recuperación de contraseña y la verificación general de email quedan pendientes; el flujo de invitaciones verifica la dirección mediante el enlace seguro entregado al destinatario.

## Ejecución local

El entorno local se inicia con Docker Compose. Antes de arrancar, configurá una clave JWT por fuera del repositorio.

En PowerShell:

```powershell
$env:AUTH_JWT_SECRET = [Convert]::ToBase64String([Security.Cryptography.RandomNumberGenerator]::GetBytes(48))
$env:MELT_INTERNAL_KEY = [Convert]::ToBase64String([Security.Cryptography.RandomNumberGenerator]::GetBytes(48))
$env:BENEFICIOS_PARTNER_KEY = [Convert]::ToBase64String([Security.Cryptography.RandomNumberGenerator]::GetBytes(48))
docker compose config
docker compose up --build -d
docker compose ps
```

La guía completa, las variables opcionales del administrador inicial y los ejemplos de registro/login están en [docs/LOCAL-DEV.md](docs/LOCAL-DEV.md). No guardes la clave generada ni contraseñas reales en el repositorio.

## Despliegue

## Despliegue

El backend de MELT cuenta con tres servicios desplegados en Render, utilizando MongoDB Atlas para la persistencia de datos.

### Servicios publicados

| Servicio | Plataforma | URL |
|---|---|---|
| API Gateway | Render | https://melt-gateway.onrender.com |
| Autenticación (Auth) | Render | https://melt-tygp.onrender.com |
| Beneficios | Render | https://melt-beneficios.onrender.com |

### Punto de entrada

La interfaz local y el API Gateway comparten origen:

`http://localhost:8080`

En Render, el gateway sirve los archivos estáticos incluidos en su imagen. La versión publicada debe actualizarse desde la rama de entrega antes de considerarla disponible para evaluación.

El Gateway centraliza el acceso a los servicios y gestiona la autenticación de las rutas protegidas.

### Verificación del despliegue

El despliegue publicado puede corresponder a una versión anterior. Esta rama incorpora
la acreditación externa y requiere actualizar Auth, Beneficios y Gateway en conjunto;
las pruebas locales no acreditan el estado de Render.

Los datos se persisten en MongoDB Atlas, en las bases correspondientes a cada servicio.

**Nota:** Las variables de entorno y credenciales se configuran en Render y no deben incluirse en el repositorio.

## Integración con otros grupos

MELT publica la API versionada de catálogo para el Grupo 1 (Gimnasio); el contrato y el mock están en [docs/contracts/README.md](docs/contracts/README.md). MELT debe consumir la API del grupo de Cocina Fantasma para búsqueda de sedes cercanas adaptada a centros o profesionales. Esa integración sigue pendiente hasta que dicho grupo entregue su API y contrato.

## Documentación del proyecto

- [Alcance, requisitos y reglas de negocio — SPEC.md](SPEC.md)
- [Arquitectura y diagramas — docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)
- [Decisiones arquitectónicas — docs/adr/](docs/adr/)
- [Contratos de integración — docs/contracts/](docs/contracts/)

La guía para ejecutar y probar el sistema localmente es [docs/LOCAL-DEV.md](docs/LOCAL-DEV.md).

## Organización del equipo

- Sofía Acuña
- Anna Garello Bertorello
- Leticia Vega Pereira

**Repositorio:** https://github.com/annagarebertoo/MELT

**Estrategia de trabajo:** GitHub Flow. La rama `main` contendrá la versión integrada; cada cambio se desarrollará en una rama específica y se incorporará mediante pull request y revisión.

## Estado del proyecto

El repositorio ya incluye una interfaz web conectada al gateway, autenticación, catálogo público de tratamientos y profesionales, permisos por operación en Reservas, agenda y disponibilidad, bloqueo temporal de carrito, checkout de seña y cancelaciones con la política de devolución. El estado verificable de la parte de identidad y reservas, las pruebas ejecutadas y las condiciones para una prueba real están en [SPEC.md, sección 13](SPEC.md#13-estado-de-implementación-y-verificación) y en la [guía de reservas web](docs/RESERVAS-WEB.md).

La integración con el proveedor externo asignado (búsqueda de sedes cercanas) sigue pendiente de que el otro grupo publique su API y contrato. También siguen fuera del flujo implementado las funciones de puntos por tratamientos completados, canjes, notificaciones por correo, y las integraciones de negocio con RabbitMQ, OpenSearch, Memcached y Prometheus. Las pruebas locales usan Mercado Pago en modo test; esto no equivale a un despliegue ni a cobros de producción.


## Desarrollo local

El entorno de desarrollo y los endpoints iniciales están documentados en [docs/LOCAL-DEV.md](docs/LOCAL-DEV.md). Para levantar los componentes declarados:

```sh
docker compose config
docker compose up --build -d
docker compose ps
```

El estado de resiliencia y el procedimiento para registrar evidencia real se encuentran en [docs/POSTMORTEM.md](docs/POSTMORTEM.md). No se deben interpretar las configuraciones declaradas como pruebas ejecutadas: las integraciones de búsqueda, caché, mensajería y métricas de negocio siguen pendientes.


## Pagos de reservas con Mercado Pago (modo pruebas)

El servicio `pagos` adapta la API de Mercado Pago al contrato interno que consume `reservas`. En el entorno local, Compose configura `PAGOS_URL=http://pagos:8085`. El adaptador sólo arranca con `MP_ENV=test`; no se debe cambiar a producción como parte de esta configuración.

1. En el panel de desarrolladores de Mercado Pago, abrí tu aplicación y copiá el **Access Token de prueba** (no una credencial de producción).
2. Creá un archivo `.env` en la raíz del repositorio (no lo subas a Git) con:

   ```dotenv
   MP_ENV=test
   MP_ACCESS_TOKEN=PEGAR_ACCESS_TOKEN_DE_PRUEBA
   MP_BASE_URL=https://api.mercadopago.com
   MP_RETURN_URL=
   ```

3. Levantá o reconstruí los servicios:

   ```bash
   docker compose up -d --build pagos reservas
   ```

4. Comprobá el adaptador en `http://localhost:8085/health` sólo si publicás el puerto localmente; por defecto, `pagos` es interno a la red de Compose y no expone un puerto en el host.

### Contrato interno

- `POST /payments/verify`: consulta `GET /v1/payments/{id}` a Mercado Pago y sólo marca aprobado si coinciden estado `approved`, ID, importe, moneda `ARS` y `external_reference` con el ID del carrito.
- `POST /payments/preferences`: crea un Checkout Pro por el importe exacto de la seña, con el ID del carrito como `external_reference`; devuelve el enlace sandbox de pago. Si se configura `MP_RETURN_URL`, debe ser una URL HTTPS pública y se agrega como destino de retorno.
- `POST /payments/refunds`: crea una devolución parcial mediante `POST /v1/payments/{id}/refunds`, enviando el importe y el encabezado `X-Idempotency-Key` para evitar duplicados.
- `GET /health`: chequeo de disponibilidad del adaptador.

La interfaz ofrece el enlace Checkout Pro devuelto al crear el carrito. Después del pago, el cliente ingresa o recupera el ID de la operación para que MELT verifique el estado aprobado, el importe exacto de la seña (30 %), moneda ARS y `external_reference` igual al ID del carrito generado por `POST /carrito/bloqueos`.

No guardes el Access Token en el repositorio, en el frontend ni en logs. Usá cuentas y credenciales de prueba de Mercado Pago; nunca uses credenciales productivas en esta configuración.

## API de acreditación externa

`POST /api/v1/beneficios/acreditar-puntos` recibe `solicitudId`, `usuarioId` y
`puntos` y exige `X-Partner-Key`. Verifica la cuenta en Auth, acumula puntos y
registra el movimiento mediante una transacción MongoDB. Repetir una solicitud
no duplica puntos; reutilizar su ID con otros datos devuelve 409.
El registro normal y el login no regalan puntos. Por email se puede guardar una acreditación pendiente y devolver un enlace seguro; el registro desde ese enlace vincula y acredita automáticamente. Una cuenta existente sin email verificado confirma el enlace con su sesión. El consumidor entrega el enlace privadamente al destinatario.

Configurar `BENEFICIOS_PARTNER_KEY`, `MELT_INTERNAL_KEY`, `AUTH_URL`, `BENEFICIOS_INVITATION_SECRET` y `MELT_PUBLIC_URL` en Beneficios; `BENEFICIOS_URL` en Auth.
MongoDB debe ser replica set (Compose lo inicializa); Atlas admite transacciones.
Ver [guía y migración](docs/ACREDITACION-PUNTOS.md) y
[contrato OpenAPI](docs/contracts/acreditar-puntos-v1.openapi.yaml).
