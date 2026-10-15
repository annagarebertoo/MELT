# MELT

**Sistema web de gestión para un centro de estética**  
**Materia:** Arquitectura de Software — 2026  
**Estado:** Implementación parcial en progreso — infraestructura local y catálogo inicial

## Descripción

MELT es una plataforma web orientada a la gestión de un centro de estética con una única sucursal. Centraliza la consulta de tratamientos y profesionales, la reserva y administración de turnos y un programa de beneficios basado en puntos.

La operación principal es la **reserva de turnos con validación de disponibilidad**: el sistema debe impedir la confirmación de turnos superpuestos para un mismo profesional. El programa de beneficios permite acreditar puntos por tratamientos efectivamente realizados y utilizarlos para canjear beneficios conforme a reglas definidas por el centro.

## Objetivo

Facilitar la organización de la agenda del centro, brindar a los clientes un mecanismo claro de reserva y seguimiento de sus turnos, y gestionar beneficios de manera consistente y trazable.

## Usuarios

- **Cliente:** consulta tratamientos y profesionales, solicita o cancela turnos y consulta sus puntos y beneficios.
- **Profesional:** consulta su agenda y registra la realización de los tratamientos que le corresponden.
- **Administrador:** administra el catálogo, los profesionales, la disponibilidad, los turnos y las reglas del programa de puntos.

## Funcionalidades previstas

1. Consulta y búsqueda de tratamientos mediante filtros, paginación y ordenamiento.
2. Consulta de profesionales y disponibilidad de horarios.
3. Reserva y cancelación de turnos, con validación de conflictos de agenda.
4. Gestión de estados de turnos y registro de tratamientos completados.
5. Acreditación de puntos y canje de beneficios según reglas del centro.
6. Envío de correos electrónicos al confirmar o cancelar un turno por parte del profesional.
7. Carrito de reserva con bloqueo temporal del horario durante 10 minutos y liberación automática al vencer.
8. Gestión de seña obligatoria del 30 % del precio del tratamiento para confirmar la reserva; devolución del 100 % si se cancela con al menos 24 horas de anticipación, sin devolución por cancelación tardía o inasistencia.
9. Gestión administrativa del catálogo, la agenda y el programa de beneficios.

Las reglas detalladas y los criterios de aceptación se documentarán en [SPEC.md](SPEC.md).

## Flujo principal: reserva de un turno

1. El cliente busca un tratamiento y consulta su información.
2. Selecciona un profesional y un horario disponible.
3. Solicita la reserva.
4. El sistema valida que el profesional pueda atender el tratamiento, que no haya incompatibilidades con la agenda del profesional ni con otros turnos del cliente y que el horario siga disponible.
5. Al agregar el horario al carrito, lo bloquea durante 10 minutos; si el cliente no confirma, lo libera automáticamente.
6. Para confirmar, exige el pago exitoso de una seña equivalente al 30 % del precio del tratamiento y transforma el bloqueo en reserva. Si se cancela con 24 horas o más de anticipación, se devuelve el 100 % de la seña; con menos de 24 horas o ante inasistencia, no se devuelve.
7. La confirmación o cancelación realizada por el profesional genera un evento para enviar un correo al cliente.
8. Una vez realizado el tratamiento, el profesional registra su finalización y se inicia la acreditación de puntos correspondiente.

La confirmación de una reserva y la acreditación de puntos deberán contemplar concurrencia, duplicación de solicitudes y fallas parciales. Los puntos se registran con una clave única para evitar acreditaciones duplicadas. Los correos usan un intento inicial y hasta 3 reintentos automáticos con esperas crecientes; si siguen fallando, pasan a una cola de errores para revisión. Los bloqueos temporales del carrito también deberán respetar las reglas de incompatibilidad y expirar automáticamente.

## Arquitectura propuesta

El sistema se diseñará con un **frontend web**, un **API Gateway** y, como mínimo, **tres microservicios** con responsabilidades diferenciadas:

| Componente | Responsabilidad preliminar |
| --- | --- |
| Catálogo y Profesionales | Tratamientos, categorías, profesionales, precios, duraciones, palabras clave y puntos configurados. |
| Reservas | Agenda, disponibilidad efectiva, creación, cancelación y finalización de turnos. |
| Beneficios | Saldos, movimientos de puntos, canjes, aplicaciones y restituciones. |
| API Gateway | Punto de entrada del frontend y enrutamiento hacia los servicios. |

Cada microservicio será propietario de sus datos. Los límites definitivos, la asignación de la agenda y las comunicaciones se detallarán en [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) y en los registros de decisiones arquitectónicas.

Se prevé utilizar **comunicación síncrona** para las consultas y validaciones que requieran una respuesta inmediata, y **mensajería asíncrona** para eventos y efectos posteriores. Se incorporarán un motor de búsqueda, caché, balanceo de carga, mecanismos de resiliencia y observabilidad. Las incompatibilidades se validarán para la agenda del profesional y para los turnos del mismo cliente.

## Tecnologías propuestas

> Selección inicial sujeta a revisión durante el diseño. Las tecnologías listadas todavía no implican que estén implementadas.

| Área | Tecnología |
| --- | --- |
| Frontend | React y TypeScript |
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

## Ejecución local

**Pendiente de implementación.** Se prevé ofrecer un procedimiento único de arranque mediante Docker Compose, que permita iniciar los servicios y sus dependencias sin configuraciones manuales específicas de cada integrante.

Cuando esté disponible, esta sección incluirá los requisitos previos, las variables de entorno de ejemplo, el comando de inicio, las direcciones de acceso y las verificaciones de funcionamiento. Los secretos no se almacenarán en el repositorio.

## Despliegue

**Pendiente.** Las URL del frontend y de las capacidades publicadas se incorporarán cuando exista un entorno desplegado y accesible.

## Integración con otro grupo

Se diseñará y documentará una capacidad de MELT para consumo externo mediante un contrato formal y versionado. La capacidad elegida es una API versionada de consulta de tratamientos con filtros, información descriptiva y precios. Su contrato y mock se prepararán en la documentación de integración. También se incorporará una capacidad provista por otro grupo según la asignación docente.

Documentación prevista: [docs/contracts/README.md](docs/contracts/README.md).

## Documentación del proyecto

- [Alcance, requisitos y reglas de negocio — SPEC.md](SPEC.md)
- [Arquitectura y diagramas — docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)
- [Decisiones arquitectónicas — docs/adr/](docs/adr/)
- [Contratos de integración — docs/contracts/](docs/contracts/)

Los enlaces a documentos aún no creados comenzarán a funcionar cuando se incorporen al repositorio.

## Organización del equipo

- Sofía Acuña
- Anna Garello Bertorello
- Leticia Vega Pereira

**Repositorio:** https://github.com/annagarebertoo/MELT

**Estrategia de trabajo:** GitHub Flow. La rama `main` contendrá la versión integrada; cada cambio se desarrollará en una rama específica y se incorporará mediante pull request y revisión.

## Estado del proyecto

Este documento corresponde a la **primera entrega de diseño**. La arquitectura, las tecnologías y las funcionalidades aquí descritas representan el alcance y las decisiones preliminares del equipo, no funcionalidades ya implementadas. Se actualizará durante el desarrollo para reflejar el estado real del sistema.


## Desarrollo local

El entorno de desarrollo y los endpoints iniciales están documentados en [docs/LOCAL-DEV.md](docs/LOCAL-DEV.md). Para levantar los componentes declarados:

```sh
docker compose config
docker compose up --build -d
docker compose ps
```

El estado de resiliencia y el procedimiento para registrar evidencia real se encuentran en [docs/POSTMORTEM.md](docs/POSTMORTEM.md). No se deben interpretar las configuraciones declaradas como pruebas ejecutadas: las integraciones de búsqueda, caché, mensajería y métricas de negocio siguen pendientes.
