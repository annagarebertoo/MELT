# ADR-009 — Reglas de compatibilidad, carrito temporal y seña

**Estado:** Aceptado para el diseño actualizado; parámetros comerciales pendientes.  
**Fecha:** 2026-10-14  
**Decisores:** Equipo MELT

## Contexto

Las reglas iniciales de reservas deben contemplar tratamientos incompatibles, selección temporal en carrito y una seña previa a la confirmación. También deben mantenerse los invariantes del dominio: no solapar reservas del profesional, no aceptar combinaciones incompatibles para el mismo cliente y no acreditar puntos antes de la realización efectiva.

## Decisiones

### 1. Compatibilidad de tratamientos

Reservas valida incompatibilidades en dos dimensiones:

- **Agenda profesional:** no se admiten intervalos incompatibles para un mismo profesional.
- **Agenda del cliente:** no se admiten dos tratamientos simultáneos del mismo cliente cuando sus tratamientos sean incompatibles.

Las incompatibilidades se modelan como reglas explícitas entre tratamientos o categorías; no se asume que todos los tratamientos de una categoría sean incompatibles entre sí. Las reglas podrán expresar intervalos de separación cuando corresponda. La validación contempla reservas confirmadas y bloqueos temporales vigentes, y debe ser atómica ante solicitudes concurrentes.

### 2. Carrito con bloqueo de 10 minutos

Al agregar al carrito un tratamiento, profesional y horario concretos, Reservas crea un bloqueo temporal por 10 minutos. El bloqueo identifica al titular y su vencimiento. Se libera al confirmar, quitar la selección o alcanzar el vencimiento, incluso si el cliente abandona la página. La expiración no depende de una nueva solicitud del cliente.

Los bloqueos temporales participan de la validación de disponibilidad y compatibilidad. La confirmación convierte el bloqueo en reserva de forma segura, evitando que otra solicitud ocupe el intervalo entre ambas operaciones.

### 3. Seña

La seña forma parte del alcance. Antes de confirmar la reserva, debe quedar registrado un resultado exitoso del pago de la seña. La seña se distingue del saldo restante, que continúa pagándose presencialmente, y del programa de puntos.

**Pendiente de parametrización:** importe o porcentaje, modalidad/proveedor de pago, política de devolución, ausencias, cancelaciones tardías y cambios de turno. Hasta definirlos no se fijan valores comerciales ni se presume un proveedor específico.

### 4. Puntos y correos

Los puntos se acreditan una única vez luego de que el profesional registra el tratamiento como completado. Ni el bloqueo del carrito, ni la reserva confirmada, ni el pago de la seña acreditan puntos.

La confirmación o cancelación de un turno realizada por el profesional genera un evento que da lugar al envío de un correo al cliente. El correo se procesa de forma asíncrona, con reintentos e idempotencia. Un fallo del proveedor de correo no revierte el cambio de estado del turno.

## Consecuencias

### Positivas

- Se hacen explícitas las reglas de incompatibilidad para profesionales y clientes.
- Se reduce la carrera por horarios durante el proceso de reserva.
- La seña pasa a ser una condición verificable de confirmación.
- Puntos y correos quedan desacoplados del flujo principal y pueden recuperarse ante fallas.

### Costos y riesgos

- Los bloqueos temporales necesitan expiración confiable y limpieza de estados vencidos.
- La validación de compatibilidad debe coordinarse con la reserva bajo concurrencia.
- Pagos, devoluciones y mensajes requieren correlación, idempotencia y trazabilidad.
- Los parámetros comerciales de la seña permanecen abiertos hasta acuerdo del equipo.

## Criterios de aceptación

1. Una combinación incompatible se rechaza tanto para el mismo profesional como para el mismo cliente.
2. Un horario agregado al carrito se bloquea por un máximo de 10 minutos y se libera al confirmar, quitarlo o vencer.
3. No se confirma una reserva sin pago exitoso de la seña registrada.
4. Reintentar eventos no duplica puntos, correos ni efectos de pago/devolución.
5. Los puntos solo se acreditan tras registrar la realización efectiva del tratamiento.
