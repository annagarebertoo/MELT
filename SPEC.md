# MELT — Especificación funcional y reglas de negocio

**Versión:** 0.1 — Primera versión para revisión del equipo.  
**Entrega:** 1 — Diseño y contrato con mock, 9 de octubre de 2026.  
**Materia:** Arquitectura de Software — 2026.  
**Estado:** Diseño inicial; las funcionalidades descritas son previstas, no se declara su implementación.  

## 1. Introducción

MELT es un sistema web de gestión para un centro de estética con una única sucursal. El dominio comprende tratamientos, profesionales, disponibilidad, reservas de turnos y un programa de beneficios basado en puntos. Busca centralizar la información y coordinar la agenda para evitar reservas incompatibles y mantener una relación trazable entre los tratamientos realizados y los beneficios de cada cliente.

La operación de negocio central es la **reserva de un turno con validación de disponibilidad**. Su confirmación exige verificar que el profesional pueda realizar el tratamiento y que el intervalo solicitado esté disponible y no se superponga con otra reserva confirmada para ese profesional. La confirmación ocupa la agenda y da lugar a notificaciones. La realización efectiva del tratamiento habilita la acreditación de puntos.

### 1.1. Vocabulario

| Término | Significado en este documento |
| :--- | :--- |
| Tratamiento | Prestación del catálogo del centro. Su realización concreta se registra en relación con un turno. |
| Turno o reserva | Compromiso de atención que relaciona cliente, tratamiento, profesional e intervalo de atención. Ambos términos se usan como equivalentes. |
| Disponibilidad | Intervalos en los que un profesional puede atender, sujetos a la ocupación vigente de su agenda. |
| Finalización | Registro de que el tratamiento asociado al turno fue efectivamente realizado. |
| Acreditación | Incorporación de puntos al cliente por un tratamiento realizado, según las reglas aplicables. |
| Canje | Uso de puntos para obtener un beneficio, según las condiciones del programa. |

## 2. Objetivos

### 2.1. Objetivo general

Facilitar la organización de la agenda de la sucursal, ofrecer a los clientes un mecanismo claro de reserva y seguimiento de turnos, y gestionar beneficios de manera consistente y trazable.

### 2.2. Objetivos específicos

1. Centralizar la consulta y búsqueda de tratamientos y profesionales.

2. Permitir la consulta de disponibilidad y la reserva de turnos sin superposiciones para un mismo profesional.

3. Permitir cancelaciones y seguimiento de estados conforme a reglas explícitas del centro.

4. Registrar la realización efectiva de tratamientos y vincularla con la acreditación de puntos.

5. Permitir consultar puntos y beneficios y efectuar canjes según reglas definidas.

6. Ofrecer funciones administrativas para gestionar catálogo, profesionales, disponibilidad, turnos y programa de beneficios.

7. Preservar los resultados de las operaciones relevantes frente a solicitudes concurrentes, duplicadas o fallas parciales.

## 3. Alcance

### 3.1. Funcionalidades incluidas en esta primera especificación

El alcance funcional comprende:

- Consulta y búsqueda de tratamientos con filtros, paginación y ordenamiento.

- Consulta de profesionales y disponibilidad de horarios.

- Reserva y cancelación de turnos con validación de conflictos de agenda.

- Seguimiento de estados y registro de tratamientos completados.

- Acreditación de puntos y canje de beneficios según reglas del centro.

- Notificaciones relacionadas con eventos relevantes de las reservas.

- Gestión administrativa del catálogo, profesionales, disponibilidad, turnos y programa de beneficios.

Las funcionalidades se ofrecerán mediante una interfaz web según las responsabilidades de cada actor.

MELT publicará una capacidad para otro grupo y consumirá una capacidad externa dentro de un flujo importante del sistema. La selección y sus efectos de negocio están pendientes (PD-12). La capacidad propia deberá quedar seleccionada y documentada para la primera entrega, con su contrato y mock.

### 3.2. Funcionalidades fuera de alcance

Quedan fuera de esta versión la gestión de múltiples sucursales, pagos en línea, facturación, inventario de insumos, historias clínicas, gestión salarial y una aplicación móvil nativa.

Las decisiones técnicas de implementación se documentarán en `docs/ARCHITECTURE.md`.

## 4. Actores

| Actor | Responsabilidades |
| :--- | :--- |
| Cliente | Consultar tratamientos, profesionales y disponibilidad; solicitar y cancelar turnos; consultar puntos y beneficios y solicitar canjes. |
| Profesional | Consultar su agenda y registrar la realización de los tratamientos que le corresponden. |
| Administrador | Gestionar catálogo, profesionales, disponibilidad, turnos y reglas del programa de beneficios; registrar finalizaciones. |

El ingreso, la asignación de roles y los permisos detallados están pendientes (PD-01). La restricción del cliente a sus propios turnos y puntos y del profesional a los turnos asignados se propone en RN-13.

## 5. Requisitos funcionales

| ID | Comportamiento esperado | Aspectos pendientes |
| :--- | :--- | :--- |
| **RF-01** | Consultar el catálogo y la información de los tratamientos ofrecidos por el centro. | Datos: PD-02. |
| **RF-02** | Buscar tratamientos con filtros relevantes para el dominio, paginación y al menos un criterio de ordenamiento. Los resultados deberán reflejar los cambios del catálogo dentro del retraso máximo que se acuerde. | Filtros y orden: PD-02; retraso: PD-11. |
| **RF-03** | Consultar profesionales, los tratamientos que pueden atender y los horarios disponibles para realizar la selección previa a una reserva. | Disponibilidad: PD-03. |
| **RF-04** | Solicitar un turno relacionando cliente, tratamiento, profesional y horario. Validar aptitud del profesional, disponibilidad y ausencia de superposición antes de confirmar; informar el motivo de un rechazo de negocio. | Intervalo: PD-03. |
| **RF-05** | Consultar los turnos y su estado según las responsabilidades de cada actor; permitir al profesional consultar su agenda. | Permisos: PD-01, RN-13. |
| **RF-06** | Solicitar y registrar la cancelación de un turno conforme a las condiciones acordadas, informando si se acepta o rechaza. | Política: PD-05, RN-07. |
| **RF-07** | Permitir al profesional correspondiente o al administrador registrar que un tratamiento fue efectivamente realizado, vinculándolo con el turno y habilitando la acreditación correspondiente. | Transiciones: PD-06, RN-10. |
| **RF-08** | Acreditar puntos por tratamientos realizados conforme a las reglas del programa, sin duplicar la acreditación al repetir el registro o su procesamiento. | Cálculo y elegibilidad: PD-07. |
| **RF-09** | Permitir al cliente consultar su saldo de puntos y los beneficios disponibles. | Movimientos: RN-12, PD-13. |
| **RF-10** | Permitir solicitar un canje y comunicar su resultado según las reglas del centro. | Condiciones: PD-08, RN-11. |
| **RF-11** | Notificar eventos relevantes de las reservas conforme a los destinatarios y medios acordados. | Notificaciones: PD-10. |
| **RF-12** | Permitir al administrador gestionar el catálogo, los profesionales y la relación entre profesionales y tratamientos. | Cambios: PD-04. |
| **RF-13** | Permitir al administrador gestionar disponibilidad y turnos, con las validaciones de negocio correspondientes. | Operaciones y cambios: PD-04, PD-06. |
| **RF-14** | Permitir al administrador gestionar las reglas de acreditación y las condiciones de los beneficios y canjes. | Vigencia: PD-09. |
| **RF-15** | Permitir determinar el resultado de una operación relevante tras una interrupción, sin crear una segunda reserva ni acreditar nuevamente puntos por la misma operación. | Recuperación: PD-13, RN-15. |

## 6. Reglas de negocio

### 6.1. Reglas acordadas

- **RN-01 — Sucursal única.** Los tratamientos, profesionales, agendas, reservas y beneficios corresponden a una única sucursal.

- **RN-02 — Profesional apto.** Solo podrá confirmarse un turno si el profesional seleccionado puede atender el tratamiento solicitado.

- **RN-03 — Disponibilidad vigente.** La confirmación exige verificar la disponibilidad para el intervalo de atención. Un horario mostrado durante una consulta no garantiza que siga disponible al confirmar.

- **RN-04 — Ausencia de superposición.** No podrán coexistir reservas confirmadas cuyos intervalos de atención se superpongan para un mismo profesional. Duración y límites del intervalo: PD-03.

- **RN-05 — Concurrencia y repetición.** Ante solicitudes simultáneas incompatibles se preservará RN-04. Repetir la misma intención no deberá crear otra reserva ni perder su resultado confirmado. Identificación de la intención: PD-13.

- **RN-06 — Puntos por realización efectiva.** La reserva por sí sola no acredita puntos. La acreditación depende de un tratamiento efectivamente realizado y registrado por el profesional correspondiente o el administrador, conforme a las reglas aplicables. Un turno cancelado sin realización no habilita puntos.

- **RN-07 — Cancelación condicionada.** Las cancelaciones se resolverán según la política del centro, pendiente en PD-05. **Propuesta:** una cancelación aceptada deja de ocupar el intervalo para nuevas reservas, que deberán superar nuevamente las validaciones de disponibilidad.

- **RN-08 — Acreditación sin duplicados.** El registro repetido o el procesamiento repetido de la finalización de un mismo tratamiento no podrá generar acreditaciones adicionales por ese mismo hecho.

- **RN-09 — Reglas comerciales explícitas.** Los puntos y beneficios se determinarán según reglas administradas por el centro. Valores y condiciones comerciales: PD-07–PD-09.

### 6.2. Precisiones propuestas para revisión

RN-10 a RN-15 requieren aprobación, al igual que la liberación del intervalo de RN-07. Los estados, flujos y criterios asociados conservan ese carácter hasta su aprobación.

- **RN-10 — Finalización válida.** Solo un turno confirmado podrá pasar a completado cuando se registre la realización efectiva. Un turno cancelado no podrá completarse mediante el flujo ordinario. Completado y cancelado serán estados finales de ese flujo; las correcciones excepcionales quedan pendientes (PD-06).

- **RN-11 — Canje consistente.** Confirmar un canje exige cumplir las condiciones del beneficio y disponer de puntos suficientes. La confirmación registra el beneficio y su débito como un único resultado. La concurrencia o repetición de solicitudes no podrá duplicar débitos, beneficios ni el uso de puntos; el saldo no podrá resultar negativo. Utilización del beneficio: PD-08.

- **RN-12 — Trazabilidad.** Cada acreditación deberá poder relacionarse con el cliente y el tratamiento realizado que la originó; cada débito, con su canje. El saldo deberá ser explicable a partir de los movimientos y de las reglas aplicables. El administrador podrá distinguir una acreditación pendiente de una ya aplicada.

- **RN-13 — Permisos por responsabilidad.** El cliente operará sobre sus propios turnos y puntos; el profesional registrará tratamientos de su agenda; el administrador actuará dentro de sus facultades sin eludir las restricciones de negocio. La matriz definitiva queda pendiente (PD-01).

- **RN-14 — Efectos posteriores independientes.** Una demora o falla en la notificación no anulará una reserva confirmada. Una falla en la acreditación no borrará la realización registrada: deberá conservarse la obligación pendiente y existir una vía de resolución sin duplicar puntos. La recuperación concreta debe acordarse (PD-10 y PD-13).

- **RN-15 — Resultado incierto.** La ausencia de una respuesta no se interpretará automáticamente como rechazo o cancelación. Antes de generar otra operación deberá poder determinarse el resultado de la original, preservando los efectos ya realizados.

## 7. Estados y transiciones

### 7.1. Turnos

Modelo propuesto, pendiente de aprobación en PD-06:

| Estado | Significado | Efecto de negocio |
| :--- | :--- | :--- |
| Confirmado | La reserva superó las validaciones y quedó registrada. | Ocupa el intervalo del profesional; todavía no habilita puntos por realización. |
| Cancelado | Se aceptó la cancelación conforme a la política del centro. | Deja de ocupar el intervalo, según RN-07 propuesta; no habilita puntos por un tratamiento no realizado. |
| Completado | Se registró la realización efectiva del tratamiento. | Habilita evaluar y procesar la acreditación; no significa que los puntos ya estén acreditados. |

| Origen | Acción y actor | Condición | Destino |
| :--- | :--- | :--- | :--- |
| Sin turno confirmado | Solicitar reserva — cliente; alcance administrativo por precisar | Aptitud, disponibilidad y ausencia de conflicto validadas | Confirmado |
| Confirmado | Cancelar — cliente; facultades administrativas por precisar | Cancelación permitida por la política acordada | Cancelado |
| Confirmado | Registrar finalización — profesional correspondiente o administrador | Tratamiento efectivamente realizado | Completado |

El rechazo no crea un turno confirmado. Los estados adicionales y las correcciones excepcionales quedan pendientes (PD-06).

### 7.2. Resultado de las operaciones

Para la solicitud de reserva y el canje se propone distinguir **en evaluación**, **confirmada**, **rechazada** y **resultado por verificar**. Son situaciones del procesamiento o de la información disponible para el usuario, no estados adicionales del turno.

La evaluación conduce a confirmación o rechazo cuando existe un resultado conocido. Si una interrupción impide conocerlo, se informa «resultado por verificar» y se consulta o recupera la misma operación hasta obtener un resultado definitivo. La interrupción no libera una reserva ya confirmada ni habilita repetir un débito.

### 7.3. Acreditación de puntos

Se propone distinguir **pendiente**, **acreditada** y **requiere revisión**. El registro de realización habilita una acreditación pendiente de evaluación según las reglas; al aplicar el movimiento pasa a acreditada. Si no puede resolverse, queda identificada para revisión sin inventar un saldo. Recuperar el procesamiento puede resolverla como acreditada, con un único efecto.

La posibilidad de tratamientos que no otorguen puntos y su representación deben definirse en PD-07.

## 8. Flujos principales y alternativos

### 8.1. Reserva de un turno

**Actor principal:** cliente. **Precondiciones:** tratamiento y profesional identificados; disponibilidad publicada; actor reconocido conforme a sus permisos. Debe poder determinarse el intervalo de atención.

1. El cliente busca un tratamiento y consulta su información.

2. Consulta los profesionales que pueden atenderlo y selecciona profesional y horario.

3. Solicita la reserva.

4. MELT valida los datos y permisos necesarios, la aptitud del profesional y la disponibilidad vigente para todo el intervalo, incluida la ausencia de superposición.

5. Si las validaciones son correctas, registra y confirma un único turno e informa su resultado.

6. La confirmación da lugar a la notificación correspondiente, según la definición pendiente de eventos y destinatarios.

**Resultado:** turno confirmado y agenda ocupada para ese intervalo. La reserva no acredita puntos.

**Alternativas:**

- Si el profesional no puede atender el tratamiento o el intervalo no está disponible, se informa el motivo y no se confirma el turno.

- Si otra solicitud ocupa el intervalo entre la consulta y la confirmación, se rechaza la solicitud incompatible y el cliente puede elegir otra alternativa.

- Si se repite la misma intención, se conserva un único resultado de negocio.

- Si no puede verificarse la disponibilidad, no se presenta una confirmación sin validación. Si la respuesta se pierde y el resultado es incierto, se aplica la propuesta de consulta y recuperación de la sección 7.2.

- Conforme a RN-14, una notificación fallida no revierte la reserva; su resolución queda pendiente sin crear otro turno.

### 8.2. Cancelación

1. El cliente consulta su turno y solicita cancelarlo.

2. MELT verifica sus permisos, el estado del turno y las condiciones de cancelación acordadas.

3. Si se admite, registra la cancelación e informa el resultado; conforme a RN-07, el intervalo deja de estar ocupado por ese turno.

4. Se produce la notificación que corresponda según PD-10.

Si la política o el estado no permiten cancelar, se informa el motivo y se conserva el estado anterior. Según el modelo propuesto, repetir una cancelación ya aceptada no genera otra transición.

### 8.3. Finalización del tratamiento y acreditación

1. El profesional consulta un turno de su agenda, o el administrador accede a él dentro de sus facultades.

2. Registra que el tratamiento se realizó efectivamente.

3. Conforme a RN-10 y RN-13, MELT verifica el responsable y que el turno esté confirmado; registra la finalización como completado.

4. Se inicia la evaluación de los puntos aplicables conforme a las reglas acordadas, manteniendo la relación con el tratamiento realizado.

5. Si corresponde acreditar y el procesamiento concluye, se registra una única acreditación y se actualiza el saldo del cliente.

**Alternativas:** un actor sin permiso o un estado incompatible impiden la finalización según el modelo propuesto. Un registro repetido no vuelve a acreditar puntos. Si falla la acreditación después de registrar la realización, el turno permanece completado; conforme a RN-14, el procesamiento queda pendiente o requiere revisión y se recupera sin duplicar el movimiento. La conducta ante reglas inexistentes o tratamientos no elegibles debe resolverse en PD-07.

### 8.4. Consulta y canje de puntos

1. El cliente consulta su saldo y los beneficios disponibles.

2. Selecciona un beneficio y solicita el canje.

3. Conforme a RN-11, MELT verifica condiciones vigentes del beneficio y saldo suficiente al confirmar, aunque la consulta anterior mostrara otro saldo.

4. Registra un único canje confirmado, el beneficio obtenido y el débito de puntos correspondiente; informa el resultado y el nuevo saldo.

**Alternativas propuestas:** si las condiciones no se cumplen o el saldo es insuficiente, se rechaza sin efectuar el débito ni otorgar el beneficio. Si llegan canjes concurrentes, se impide gastar dos veces los mismos puntos. Si se repite la misma intención, se conserva el resultado original. Una interrupción no debe dejar un canje informado como confirmado sin su débito correspondiente ni duplicar efectos durante la recuperación. La utilización posterior y las posibles anulaciones del beneficio permanecen pendientes (PD-08).

## 9. Criterios de aceptación

Los criterios se verificarán durante la implementación. Los marcados como **propuestos** requieren aprobar sus reglas asociadas. Los parámetros comerciales se tomarán de las decisiones correspondientes.

| ID | Requisitos / reglas | Condición verificable |
| :--- | :--- | :--- |
| **CA-01** | RF-01, RF-02 | Con un catálogo y filtros acordados, la búsqueda devuelve tratamientos que cumplen esos filtros, permite recorrer páginas y respeta el orden seleccionado. Una búsqueda sin coincidencias muestra un resultado vacío. Parámetros: PD-02. |
| **CA-02** | RF-02 | Una modificación del catálogo aparece en los resultados antes de superar el retraso máximo acordado en PD-11. |
| **CA-03** | RF-03, RF-04; RN-02–RN-04 | Dado un profesional apto y un intervalo disponible, solicitar la reserva confirma un turno asociado al cliente, tratamiento y profesional elegidos. Consultar la agenda permite identificarlo. |
| **CA-04** | RF-04; RN-02 | Seleccionar un profesional que no puede realizar el tratamiento provoca un rechazo con motivo y no crea un turno confirmado. |
| **CA-05** | RF-04; RN-03, RN-04 | Un intervalo fuera de la disponibilidad o superpuesto con un turno confirmado del mismo profesional no puede confirmarse. Debe comprobarse una superposición parcial además de la coincidencia total. Duración y casos de intervalos contiguos dependen de PD-03. |
| **CA-06** | RF-04, RF-15; RN-04, RN-05 | Ante dos solicitudes simultáneas válidas salvo por competir por intervalos incompatibles del mismo profesional, se confirma una sola. Repetir la misma intención no incrementa el número de reservas. |
| **CA-07** | RF-06; RN-07 | Con una política de cancelación acordada, el sistema acepta los casos permitidos y rechaza los no permitidos informando el motivo. **Propuesto:** una cancelación aceptada deja el turno cancelado y permite volver a evaluar el intervalo para otra reserva; repetirla conserva el mismo estado. |
| **CA-08** | RF-07, RF-08; RN-06, RN-08 | Registrar una realización efectiva por un actor habilitado inicia la acreditación según las reglas acordadas. Repetir la finalización o su procesamiento produce como máximo una acreditación por el mismo hecho. Reservar o cancelar sin realización no acredita puntos. |
| **CA-09** | RF-05, RF-07; RN-10, RN-13 | **Propuesto:** un cliente no consulta turnos ajenos ni registra finalizaciones; un profesional no completa turnos de otro profesional; un turno cancelado no puede completarse por el flujo ordinario. |
| **CA-10** | RF-08, RF-09; RN-12, RN-14 | **Propuesto:** si falla la acreditación tras registrar la realización, el turno sigue completado y la acreditación pendiente puede identificarse. Al recuperarla, existe un único movimiento asociado y el saldo refleja una sola acreditación. |
| **CA-11** | RF-09, RF-10; RN-11, RN-12 | **Propuesto:** con saldo suficiente y condiciones satisfechas, un canje confirma un beneficio y su débito; con saldo insuficiente no modifica el saldo ni otorga el beneficio. El saldo resultante es explicable por los movimientos. |
| **CA-12** | RF-10; RN-11 | **Propuesto:** si dos canjes concurrentes excederían juntos el saldo, no se confirman ambos. Repetir una misma intención conserva un solo canje, débito y beneficio. |
| **CA-13** | RF-11; RN-14 | Se genera la notificación prevista para el evento acordado en PD-10. **Propuesto:** provocar una falla en su envío conserva el turno confirmado y permite identificar el efecto pendiente para su resolución. |
| **CA-14** | RF-12–RF-14 | El administrador puede gestionar catálogo, profesionales, disponibilidad y reglas del programa según sus permisos acordados. Una reserva posterior valida la aptitud y disponibilidad vigentes; un canje o acreditación aplica la regla cuya vigencia se haya definido. Casos de modificación con operaciones existentes: PD-04 y PD-09. |
| **CA-15** | RF-15; RN-15 | **Propuesto:** si se pierde la respuesta de una operación, el usuario ve que su resultado debe verificarse. La recuperación determina el resultado original sin otra reserva, acreditación o canje como efecto de la repetición. |

La capacidad de integración deberá incorporar criterios propios una vez seleccionada, incluyendo su efecto en un flujo importante y el comportamiento observable ante la indisponibilidad del proveedor.

## 10. Decisiones pendientes

### 10.1. Usuarios y catálogo

| ID | Decisión |
| :--- | :--- |
| **PD-01** | Definir ingreso, alta de cuentas, asignación de roles y permisos, incluidas las facultades administrativas sobre reservas y canjes. Aprobar RN-13. |
| **PD-02** | Definir datos de tratamientos y profesionales, filtros, ordenamiento y paginación; confirmar si se publicarán precios. |
| **PD-04** | Definir cambios o retiros de tratamientos y profesionales y modificaciones de aptitud o disponibilidad con reservas existentes; conservar la información histórica necesaria. |
| **PD-11** | Fijar el retraso máximo entre cambios del catálogo y resultados de búsqueda y cómo comunicar información desactualizada. |

### 10.2. Agenda y turnos

| ID | Decisión |
| :--- | :--- |
| **PD-03** | Definir duración y límites de intervalos, horarios, excepciones y anticipación; confirmar tiempos entre atenciones y restricciones por cliente o recursos compartidos. |
| **PD-05** | Definir condiciones, plazos, responsables y consecuencias de cancelación. Aprobar la liberación del intervalo de RN-07. |
| **PD-06** | Aprobar estados y transiciones de turnos y RN-10; decidir sobre inasistencias, reprogramación, atención en curso, condiciones temporales de finalización y corrección de registros. |

### 10.3. Puntos y beneficios

| ID | Decisión |
| :--- | :--- |
| **PD-07** | Definir cálculo, unidad, elegibilidad y momento de acreditación; tratamiento de reglas inexistentes, prestaciones sin puntos, vencimientos y ajustes. |
| **PD-08** | Definir beneficios, costo en puntos, disponibilidad, condiciones, entrega o utilización, límites, vencimientos y anulaciones. Aprobar RN-11. |
| **PD-09** | Definir vigencia de cambios del programa y su aplicación a reservas existentes, tratamientos completados y canjes iniciados. |

### 10.4. Notificaciones, recuperación e integración

| ID | Decisión |
| :--- | :--- |
| **PD-10** | Definir eventos, destinatarios, contenido y canales de notificación; resolver envíos fallidos y asignar responsables. Aprobar RN-14 junto con PD-13. |
| **PD-12** | Seleccionar y documentar para la entrega del 9 de octubre la capacidad propia a compartir, con contrato y mock. Definir la capacidad externa según la asignación del proveedor, el flujo afectado y la conducta ante su indisponibilidad. |
| **PD-13** | Aprobar RN-12, RN-15 y los estados de operaciones y acreditación. Definir cómo reconocer una misma intención, consultar resultados inciertos y resolver efectos pendientes sin duplicarlos. Completar la recuperación de acreditaciones de RN-14. |

Las decisiones comerciales permanecerán abiertas hasta su aprobación; esta versión no fija cantidades de puntos, precios, horarios ni plazos de cancelación.
