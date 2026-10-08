# ADR-001 — Límites de microservicios

**Estado:** Aceptado (decisión inicial completa)  
**Fecha:** 2026-10-08  
**Entrega:** 1 — 9 de octubre de 2026  
**Decisores:** Equipo MELT

## Contexto y problema

MELT es un sistema de gestión para un centro de estética de una sola sucursal. Debe centralizar el catálogo de tratamientos y profesionales, la reserva de turnos con validación de disponibilidad y un programa de beneficios basado en puntos.

La operación central es la reserva con verificación de aptitud del profesional y de ausencia de superposición en la agenda. La confirmación del turno, su realización efectiva y el pago presencial son hechos distintos. El sistema debe evitar turnos superpuestos, operaciones duplicadas y pérdida de efectos (acreditación, restitución, notificaciones).

El proyecto exige una arquitectura de microservicios con responsabilidades y datos propios. Hay que definir límites claros de capacidades de negocio, de modo que cada servicio sea dueño exclusivo de sus datos y pueda evolucionar sin acoplamiento fuerte a las bases ajenas.

## Alternativas consideradas

1. **Monolito modular**  
   Un único despliegue con módulos internos (catálogo, reservas, beneficios). Simplicidad de transacciones locales y menor latencia, pero no cumple el objetivo de desarrollar y demostrar un sistema distribuido, ni facilita la independencia de despliegue y de bases de datos.

2. **Más de tres microservicios** (por ejemplo, separar Catálogo de Profesionales, o extraer Notificaciones y Agenda)  
   Mayor granularidad, pero aumenta la coordinación, la superficie de fallas y la complejidad operativa para un dominio de una sola sucursal y un equipo pequeño. Muchas responsabilidades de notificación o de eventos son efectos posteriores de Reservas o Beneficios, no dominios independientes.

3. **Dos microservicios** (Catálogo+Profesionales+Reservas juntos, y Beneficios aparte)  
   Reduce comunicaciones, pero mezcla la autoridad de disponibilidad (agenda) con la de publicación del catálogo, dificultando la evolución independiente y la propiedad exclusiva de datos.

4. **Tres microservicios por capacidad de negocio** (opción elegida)  
   Separación alineada con reglas y datos propios de cada dominio.

## Decisión

MELT se organiza en **tres microservicios** con las siguientes responsabilidades y propiedad de datos:

| Servicio | Responsabilidades | Datos propios |
| :--- | :--- | :--- |
| **Catálogo y Profesionales** | Administrar tratamientos, categorías, profesionales y aptitudes; publicar nombres, imágenes, descripciones, precios, duraciones y puntos fijos configurados por tratamiento. Ofrecer catálogo público, búsqueda y la API propia de consulta de tratamientos. | Categorías, tratamientos, referencias de imágenes, precios, duraciones, puntos configurados, profesionales y aptitudes. |
| **Reservas** | Administrar agenda, disponibilidad efectiva, turnos, confirmación, cancelación y finalización de tratamientos. Ofrecer próximos turnos, historial de realizaciones y agenda profesional. Coordinar la aplicación de beneficios y el disparo de correo de confirmación. | Horarios, excepciones, turnos, intervalos, estados, realizaciones y referencias a beneficios aplicados. |
| **Beneficios** | Administrar saldos, movimientos, recompensas, canjes, aplicaciones y restituciones. Acreditar puntos por tratamientos completados y controlar que no se dupliquen sus efectos. | Saldos, movimientos de puntos, beneficios, canjes, aplicaciones y restituciones. |

**Principios de los límites:**

- **Reservas** es la autoridad de disponibilidad y ocupación. Catálogo administra la información del profesional y los tratamientos que puede atender, pero no mantiene una segunda agenda editable.
- **Beneficios** utiliza los puntos configurados por tratamiento (publicados por Catálogo); no es dueño del catálogo.
- Cada servicio es propietario exclusivo de su base de datos. No se comparten tablas ni se consultan directamente datos ajenos. Las referencias entre servicios se transmiten mediante identificadores por API o eventos.
- Los trabajadores de correo o de procesamiento de eventos pertenecen al servicio responsable; no se crea un microservicio adicional por cada efecto lateral.

## Justificación

La separación responde a **capacidades de negocio con reglas y datos propios**:

- El catálogo es de lectura intensiva, descriptivo y publicable hacia el exterior.
- La agenda exige integridad transaccional fuerte (evitar superposiciones) y es el núcleo de la operación de negocio.
- Los puntos y canjes requieren consistencia de saldos y trazabilidad de movimientos, con reglas de no duplicación distintas de las de la agenda.

Separar así permite:

- Asignar responsabilidades claras al equipo.
- Evolucionar cada dominio (por ejemplo, cambiar el modelo de puntos o la forma de publicar el catálogo) sin tocar la base de otro servicio.
- Cumplir el objetivo académico de un sistema distribuido con comunicaciones remotas y coordinación.

No se justifica por una escala de producción aún no medida, sino por la separación de concerns y por las exigencias del TP.

## Consecuencias

### Positivas

- Límites de negocio alineados con el vocabulario del dominio (tratamiento, turno, puntos).
- Propiedad exclusiva de datos: cada servicio puede elegir persistencia y esquema sin acoplarse a los demás.
- Facilita la asignación de trabajo y la defensa de responsabilidades en la evaluación.
- Permite exponer la API pública de tratamientos desde Catálogo sin filtrar datos de clientes o agendas.

### Negativas / riesgos

- Introduce comunicaciones remotas (síncronas y asíncronas) y posibles fallas parciales.
- La reserva con beneficio involucra dos servicios con bases distintas: hay que coordinar validación, aplicación y eventual restitución sin transacciones distribuidas nativas.
- Mayor complejidad operativa (despliegue, observabilidad, reintentos) respecto de un monolito.

### Aspectos pendientes

- Formalizar los contratos internos (API y eventos) y los mecanismos de idempotencia / recuperación (relacionados con D5).
- Evaluar si en el futuro conviene refinar los límites (por ejemplo, extraer notificaciones) solo si aparece evidencia de necesidad.
- Detallar permisos y autenticación (fuera del alcance de este ADR).
