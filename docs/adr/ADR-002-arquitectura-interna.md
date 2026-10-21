# ADR-002: Arquitectura interna de los servicios

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