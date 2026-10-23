# ADR-002: Arquitectura interna de los servicios

> **Cierre de diseño para Entrega 2:** el apartado final concreta la propuesta conservada. Es una decisión documental para implementar; no acredita una refactorización ya realizada.

- **Estado:** Propuesto
- **Fecha:** 2026-10-21

## Contexto

MELT utiliza tres microservicios de negocio: Catálogo y Profesionales, Reservas y Beneficios. Cada servicio tiene responsabilidades y datos propios. La arquitectura interna debe mantener separadas las responsabilidades y facilitar el desarrollo y las pruebas.

## Decisión

Se propone utilizar dos estilos de arquitectura interna:

- **Arquitectura por capas para Catálogo y Profesionales**, separando la entrada HTTP, la lógica del servicio y el acceso a MongoDB.
- **Arquitectura hexagonal para Reservas y Beneficios**, aislando la lógica de negocio de las tecnologías externas mediante puertos y adaptadores.

## Justificación

Catálogo y Profesionales ofrece principalmente consultas y administración de información del catálogo, por lo que la separación por capas permite organizar los manejadores HTTP, la lógica de aplicación y la persistencia.

Reservas y Beneficios contienen reglas de negocio que requieren coordinar operaciones, persistencia y comunicaciones con otros servicios. La arquitectura hexagonal busca reducir el acoplamiento de esas reglas con las tecnologías utilizadas.

## Consecuencias

- Cada estilo deberá reconocerse en la estructura y las dependencias del código.
- Las capas y los adaptadores deberán mantener responsabilidades diferenciadas.
- Las pruebas deberán permitir verificar la lógica sin depender innecesariamente de servicios externos.
- La aplicación efectiva de estos estilos deberá comprobarse sobre las implementaciones reales.

## Alternativas consideradas

- Utilizar el mismo estilo en los tres servicios: se descartó como propuesta porque el equipo busca aplicar y comparar dos estilos internos diferentes.
- Mantener toda la lógica en los manejadores HTTP: se descartó como propuesta porque mezclaría transporte, reglas de negocio y persistencia.

## Pendientes

La implementación concreta de la arquitectura hexagonal en Reservas y Beneficios deberá verificarse durante el desarrollo. Esta decisión no implica que esos dos servicios ya estén implementados.

## Revisión para la Entrega 2 (23/10/2026)

Se conserva la propuesta de capas para Catálogo y hexagonal para Reservas y Beneficios. La primera favorece una organización simple de consultas del catálogo; la segunda permite que las reglas de agenda y saldos dependan de abstracciones en lugar de transportes y motores concretos. A cambio, requiere interfaces y adaptadores que todavía no existen y que no deben agregarse solo para aparentar un estilo distinto.

La inspección muestra que Catálogo concentra modelo, handlers y acceso directo a `*mongo.Collection` en `cmd/api/main.go`; solo la observabilidad está separada en otro archivo. No se acredita una arquitectura por capas implementada. Reservas y Beneficios contienen únicamente `cmd/api/main.go` con salud HTTP: no hay núcleo, puertos ni adaptadores hexagonales de negocio.

La decisión está documentada como propuesta justificada, pendiente de ratificación y de definir dependencias internas concretas. Para cerrar el diseño, explicitar los límites HTTP → aplicación → persistencia de Catálogo y adaptadores → casos de uso/dominio ← puertos en los servicios hexagonales; su materialización y la demostración de dos estilos reconocibles corresponden al desarrollo posterior. No se refactoriza código en esta revisión. Ver [cierre de Entrega 2](../ENTREGA-2.md).

## Diseño concretado para implementar

Se conserva capas para Catálogo y hexagonal para Reservas/Beneficios. Esta concreción reemplaza los pendientes de dependencias del apartado anterior, sin cambiar los estilos elegidos ni afirmar aprobación docente.

| Servicio / módulo lógico | Responsabilidad | Dependencia permitida |
| :--- | :--- | :--- |
| Catálogo: HTTP | Decodificar parámetros, validar formato y traducir resultados a HTTP. | Aplicación; no construir consultas MongoDB. |
| Catálogo: aplicación | Coordinar consultas y operaciones del catálogo, validaciones y errores del caso de uso. | Modelo del catálogo y capa de persistencia; no Gin ni detalles del request. |
| Catálogo: persistencia | Traducir filtros y documentos, ejecutar MongoDB y convertir errores técnicos. | Driver MongoDB y modelo; no handlers HTTP ni bases ajenas. |
| Reservas/Beneficios: dominio | Invariantes de agenda, estados, saldo y movimientos de su respectivo servicio. | Tipos propios; sin Gin, SQL, AMQP o clientes de otros servicios. |
| Reservas/Beneficios: aplicación y puertos | Coordinar casos de uso y declarar operaciones requeridas de repositorios, eventos y proveedores. | Dominio y contratos de puertos propios; sin implementaciones de adaptadores. |
| Adaptadores de entrada | HTTP o recepción de eventos, validación de transporte y traducción. | Casos de uso; no acceso directo a repositorios para saltar reglas. |
| Adaptadores de salida | Implementar repositorios PostgreSQL, clientes HTTP y publicación de eventos. | Puertos y tecnologías correspondientes; no reglas duplicadas del dominio. |
| Composición de cada servicio | Crear dependencias, configuración e instrumentación y conectarlas al iniciar. | Módulos propios; no lógica comercial. |

No se permiten ciclos entre capas, importaciones desde dominio hacia adaptadores ni acceso de un microservicio a la base de otro. Las llamadas remotas se encapsulan en el servicio responsable, no en el frontend ni en reglas del gateway. Las pruebas de dominio serán independientes del transporte; los adaptadores tendrán integración real.

Capas mantiene simple el flujo documental de Catálogo; hexagonal protege las reglas más sensibles de agenda y saldos de cambios de transporte y persistencia. Se acepta mayor trabajo de interfaces y traducciones en los servicios hexagonales. No se exige crear un archivo por cada clase o abstracción: las responsabilidades y dirección de dependencias deben ser reconocibles.

**Implementación actual:** Catálogo todavía accede a MongoDB desde handlers; los otros dos servicios solo ofrecen salud. Las tablas anteriores son diseño, no descripción del código actual. No se modifican servicios en este cierre.
