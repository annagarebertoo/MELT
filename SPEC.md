# MELT — Especificación funcional y reglas de negocio

**Versión:** 0.2 — Primera versión para revisión del equipo.  
**Entrega:** 1 — Diseño y contrato con mock, 9 de octubre de 2026.  
**Materia:** Arquitectura de Software — 2026.  
**Estado:** Especificación funcional del equipo. Las reglas definen el comportamiento esperado; la inspección documental del código de entrega está resumida en la sección 13 y en la [matriz de cumplimiento](docs/MATRIZ-CUMPLIMIENTO.md).

> **Nota de vigencia:** este documento se originó el 9/10/2026 y conserva requisitos y decisiones de producto de aquella revisión. Contiene referencias históricas a Mercado Pago, una integración proveedora pendiente y pantallas aún no implementadas en esa fecha. El commit `develop` `ea1ead7e85f533ba54aff85bd95958bb749897a0` usa un simulador interno, incluye UI administrativa, puntos/canjes y un adaptador GhostKitchen; la correspondencia entre GhostKitchen y la asignación oficial D9 sigue sin confirmar. Para código actual consultar [README](README.md), [manuales de servicio](docs/servicios/README.md) y [matriz de cumplimiento](docs/MATRIZ-CUMPLIMIENTO.md). No usar la sección 13 antigua como estado de la versión entregada.

## 1. Introducción

MELT es un sistema web de gestión para un centro de estética con una única sucursal. El dominio comprende tratamientos, profesionales, disponibilidad, reservas de turnos y un programa de beneficios basado en puntos. Busca centralizar la información y coordinar la agenda para evitar reservas incompatibles y mantener una relación trazable entre los tratamientos realizados y los beneficios de cada cliente.

La operación de negocio central es la **reserva de un turno con validación de disponibilidad**. Su confirmación exige verificar que el profesional pueda realizar el tratamiento, que el intervalo esté disponible y que no se superponga con otra reserva o bloqueo vigente. El horario se bloquea temporalmente mientras el cliente paga una seña del 30 % mediante el servicio Pagos; en la versión actual este servicio es un simulador interno, no Mercado Pago. El saldo restante se paga presencialmente. La realización efectiva del tratamiento habilita la acreditación de puntos, que pertenece a un flujo distinto.

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

- Exploración pública de las categorías Facial, Corporal, Depilación y Manos y uñas; consulta y búsqueda de tratamientos con filtros, paginación y ordenamiento.

- Consulta de profesionales y disponibilidad de horarios.

- Reserva y cancelación de turnos con validación de conflictos de agenda.

- Seguimiento de estados y registro de tratamientos completados.

- Acreditación de puntos, canje por descuentos o tratamientos gratuitos, aplicación de beneficios durante la reserva y restitución automática ante cancelaciones válidas.

- Correo electrónico de confirmación y notificaciones de eventos relevantes de las reservas.

- Gestión administrativa del catálogo, profesionales, disponibilidad, turnos y programa de beneficios.

Las funcionalidades se ofrecerán mediante una interfaz web según las responsabilidades de cada actor.

MELT publica una API propia de consulta de tratamientos con filtros, información descriptiva y precios; su contrato OpenAPI v1 y mock están en `docs/contracts/`. El código actual incluye un proxy de Beneficios hacia GhostKitchen; la correspondencia entre ese proveedor y la asignación oficial D9 no pudo confirmarse en esta revisión, por lo que no se da D9 por cumplida. Ver [ADR-014](docs/adr/ADR-014-consumo-proveedor.md) y [manual](docs/GHOSTKITCHEN.md).

### 3.2. Funcionalidades fuera de alcance

Quedan fuera de esta versión la gestión de múltiples sucursales, el pago del saldo total en línea, facturación, inventario de insumos, historias clínicas, gestión salarial y aplicación móvil nativa. La seña es del 30 % y la política registrada en Reservas contempla devolución simulada total con al menos 24 horas de anticipación; con menos de 24 horas o inasistencia no hay devolución. La versión de entrega usa el simulador interno de `pagos`; no utiliza credenciales ni operaciones de Mercado Pago. Ver [ADR-015](docs/adr/ADR-015-checkout-unico-simulador-pagos.md).

Las decisiones técnicas de implementación se documentarán en `docs/ARCHITECTURE.md`.

### 3.3. Lineamientos de experiencia visual

La interfaz web estática se sirve desde NGINX/Gateway. Incluye navegación pública, registro/login, catálogo y profesionales, consulta de disponibilidad, carrito/checkout, Mi cuenta y secciones por rol para clientes, profesionales y administración. El estado visual exacto se puede revisar en `infra/nginx/web/`; la descripción por rol está en [manual de usuario](docs/manuales/USUARIO.md). La integración con API real depende de datos válidos y servicios disponibles.

## 4. Actores

| Actor | Responsabilidades |
| :--- | :--- |
| Cliente | Consultar tratamientos, profesionales y disponibilidad; solicitar y cancelar turnos; consultar próximos turnos, historial, puntos y beneficios y aplicar beneficios durante la reserva. |
| Profesional | Consultar su agenda vinculada a perfil de Catálogo y sus turnos; registrar realización o inasistencia conforme al estado del turno. |
| Recepción | Operaciones de Reservas permitidas por la matriz de permisos; no posee panel diferenciado. |
| Administrador | Gestionar catálogo, perfiles/vinculaciones, disponibilidad, reglas de puntos, beneficios y recompensas según permisos. |
| Superadministrador | Administración de cuentas y roles, autorizada específicamente por Auth. |

El catálogo es público. Para reservar, el cliente debe iniciar sesión o registrarse. Auth define los roles y Reservas aplica permisos por operación y comprobación de propiedad del recurso. La administración del catálogo requiere autorización administrativa. La versión actual incluye interfaz de administración; las rutas están descritas en [contratos](docs/contracts/README.md) y [manuales](docs/servicios/README.md).

## 5. Requisitos funcionales

| ID | Comportamiento esperado | Aspectos pendientes |
| :--- | :--- | :--- |
| **RF-01** | Explorar el catálogo sin iniciar sesión, organizado inicialmente en Facial, Corporal, Depilación y Manos y uñas. Mostrar nombre, imagen, precio y acceso al detalle de cada tratamiento; el detalle incluye descripción, duración, precio, imágenes, puntos otorgados y profesionales asociados. | Datos principales definidos; filtros: PD-02. |
| **RF-02** | Buscar tratamientos con filtros relevantes para el dominio, paginación y al menos un criterio de ordenamiento. Los resultados deberán reflejar los cambios del catálogo dentro del retraso máximo que se acuerde. | Filtros y orden: PD-02; retraso: PD-11. |
| **RF-03** | Consultar profesionales, los tratamientos que pueden atender y los horarios disponibles para realizar la selección previa a una reserva. | Disponibilidad: PD-03. |
| **RF-04** | Reservar con sesión iniciada: seleccionar tratamiento y profesional, consultar un calendario, elegir fecha y horario disponible y, opcionalmente, aplicar un beneficio. Al agregarlo al carrito se bloquea temporalmente el horario durante 10 minutos. La confirmación requiere validar aptitud, disponibilidad, compatibilidad, pago exitoso de la seña y el beneficio seleccionado. Mostrar el resumen del turno. | Intervalos: PD-03; beneficios: PD-08; seña: RN-20; bloqueo: RN-19. |
| **RF-05** | Permitir al cliente consultar próximos turnos y el historial de tratamientos realizados desde su perfil; permitir al profesional consultar su agenda y los detalles de sus turnos. | Permisos: PD-01, RN-13. |
| **RF-06** | Permitir al cliente cancelar desde su perfil. Si cancela con 24 horas o más de anticipación, corresponde la devolución del 100 % de la seña; si cancela con menos de 24 horas, no corresponde devolución. La cancelación libera el horario y la restitución de puntos o beneficios aplicados se resuelve según las reglas del programa, sin duplicados. | Facultades administrativas y excepciones: PD-05; aplicación de beneficios: PD-08; política de seña: RN-20. |
| **RF-07** | Permitir al profesional correspondiente registrar un tratamiento como completado, vinculándolo con el turno e iniciando la acreditación de puntos. | Transiciones y facultades excepcionales: PD-06. |
| **RF-08** | Acreditar la cantidad fija de puntos configurada para el tratamiento cuando el profesional registra su finalización, nunca al reservar y sin duplicar acreditaciones. | Vigencia de cambios: PD-09. |
| **RF-09** | Mostrar en el perfil del cliente sus puntos disponibles, beneficios e historial. | Trazabilidad: RN-12, PD-13. |
| **RF-10** | Permitir canjear puntos por descuentos o tratamientos gratuitos y aplicar un beneficio disponible durante la reserva. Informar el beneficio aplicado y el importe restante, que se paga presencialmente. | Costos y condiciones: PD-08. |
| **RF-11** | Enviar por correo electrónico al cliente la confirmación de la reserva y notificaciones cuando el profesional confirme o cancele un turno. Los envíos se procesan de forma asíncrona y recuperable: intento inicial más hasta 3 reintentos, con espera creciente; los fallos persistentes se envían a una cola de errores para revisión. | Proveedor de correo y operación de recuperación manual: PD-10. |
| **RF-12** | Permitir al administrador gestionar tratamientos, categorías, profesionales y su asociación con tratamientos. | Cambios con reservas existentes: PD-04. |
| **RF-13** | Permitir al administrador gestionar disponibilidad y turnos, con las validaciones de negocio correspondientes. | Operaciones y cambios: PD-04, PD-06. |
| **RF-14** | Permitir al administrador configurar la cantidad fija de puntos de cada tratamiento y gestionar beneficios y recompensas. | Condiciones: PD-08; vigencia: PD-09. |
| **RF-15** | Determinar el resultado de una operación tras una interrupción, conservando sus efectos y evitando duplicar reservas, acreditaciones, canjes y restituciones. | Recuperación: PD-13, RN-15. |
| **RF-16** | Ofrecer a otros grupos una API de consulta de tratamientos con filtros, información descriptiva y precios. | Contrato y mock: PD-12. |

## 6. Reglas de negocio

### 6.1. Reglas acordadas

- **RN-01 — Sucursal única.** Los tratamientos, profesionales, agendas, reservas y beneficios corresponden a una única sucursal.

- **RN-02 — Profesional apto.** Solo podrá confirmarse un turno si el profesional seleccionado puede atender el tratamiento solicitado.

- **RN-03 — Disponibilidad vigente.** La confirmación exige verificar la disponibilidad para el intervalo de atención. Un horario mostrado durante una consulta no garantiza que siga disponible al confirmar.

- **RN-04 — Ausencia de superposición.** No podrán coexistir reservas confirmadas cuyos intervalos de atención se superpongan para un mismo profesional. Duración y límites del intervalo: PD-03.

- **RN-05 — Concurrencia y repetición.** Ante solicitudes simultáneas incompatibles se preservará RN-04. Repetir la misma intención no deberá crear otra reserva ni perder su resultado confirmado. Identificación de la intención: PD-13.

- **RN-06 — Puntos por realización efectiva.** Cada tratamiento tiene una cantidad fija de puntos configurable por el administrador. Se acredita cuando el profesional registra el tratamiento como completado; reservar o cancelar sin realización no acredita puntos.

- **RN-07 — Cancelación y restitución.** El cliente puede cancelar desde su perfil antes del inicio del turno. La cancelación libera el intervalo. Si cancela con 24 horas o más de anticipación, corresponde la devolución del 100 % de la seña; si cancela con menos de 24 horas, no se devuelve la seña. La restitución de puntos o beneficios aplicados se realiza conforme a las reglas del programa. Repetir la cancelación no duplica devoluciones ni restituciones. La inasistencia no genera devolución de la seña.

- **RN-08 — Acreditación sin duplicados.** El registro repetido o el procesamiento repetido de la finalización de un mismo tratamiento no podrá generar acreditaciones adicionales por ese mismo hecho.

- **RN-09 — Beneficios y pago presencial.** Los puntos pueden canjearse por descuentos o tratamientos gratuitos. El cliente puede aplicar un beneficio disponible durante la reserva; cualquier importe restante se paga presencialmente. Confirmar, completar y pagar son hechos distintos; no se implementan pagos online.

### 6.2. Consistencia y precisiones para revisión

RN-11 expresa las decisiones de canje y restitución acordadas. RN-10 y RN-12 a RN-15 son precisiones propuestas; sus estados, flujos y criterios asociados requieren aprobación.

- **RN-10 — Finalización válida.** Solo un turno confirmado podrá pasar a completado cuando se registre la realización efectiva. Un turno cancelado no podrá completarse mediante el flujo ordinario. Completado y cancelado serán estados finales de ese flujo; las correcciones excepcionales quedan pendientes (PD-06).

- **RN-11 — Canje consistente.** El canje debe validar saldo suficiente y condiciones del beneficio; la aplicación debe validar la disponibilidad y las condiciones del beneficio obtenido. Un mismo beneficio o saldo no puede consumirse dos veces por concurrencia o repetición. Una cancelación válida debe recuperar exactamente los puntos o el beneficio consumido, sin duplicados. Los costos y condiciones concretos permanecen pendientes (PD-08).

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
| Cancelado | Se aceptó la cancelación conforme a la política del centro. | Libera el intervalo y habilita la restitución del beneficio o puntos aplicados; no acredita puntos por realización. |
| Completado | Se registró la realización efectiva del tratamiento. | Habilita evaluar y procesar la acreditación; no significa que los puntos ya estén acreditados. |

| Origen | Acción y actor | Condición | Destino |
| :--- | :--- | :--- | :--- |
| Sin turno confirmado | Solicitar reserva — cliente; alcance administrativo por precisar | Aptitud, disponibilidad y ausencia de conflicto validadas | Confirmado |
| Confirmado | Cancelar — cliente; facultades administrativas por precisar | Solicitud válida antes del inicio; la devolución de la seña depende de la anticipación establecida en RN-20 | Cancelado |
| Confirmado | Registrar finalización — profesional correspondiente | Tratamiento efectivamente realizado | Completado |

El rechazo no crea un turno confirmado. Los estados adicionales y las correcciones excepcionales quedan pendientes (PD-06).

### 7.2. Resultado de las operaciones

Para la solicitud de reserva y el canje se propone distinguir **en evaluación**, **confirmada**, **rechazada** y **resultado por verificar**. Son situaciones del procesamiento o de la información disponible para el usuario, no estados adicionales del turno.

La evaluación conduce a confirmación o rechazo cuando existe un resultado conocido. Si una interrupción impide conocerlo, se informa «resultado por verificar» y se consulta o recupera la misma operación hasta obtener un resultado definitivo. La interrupción no libera una reserva ya confirmada ni habilita repetir un débito.

### 7.3. Acreditación de puntos

Se propone distinguir **pendiente**, **acreditada** y **requiere revisión**. El registro de realización habilita una acreditación pendiente de evaluación según las reglas; al aplicar el movimiento pasa a acreditada. Si no puede resolverse, queda identificada para revisión sin inventar un saldo. Recuperar el procesamiento puede resolverla como acreditada, con un único efecto.

Cada acreditación utiliza los puntos fijos del tratamiento. La regla aplicable ante cambios de configuración se definirá en PD-09.

### 7.4. Aplicación y restitución de beneficios

Se propone distinguir **disponible**, **aplicado** y **restituido**. Una aplicación válida vincula el beneficio con un turno y evita su reutilización simultánea. Una cancelación válida lo restituye, o devuelve los puntos utilizados, según la modalidad acordada (PD-08). La restitución puede estar **pendiente**, **realizada** o **requiere revisión**, sin modificar el estado cancelado del turno. Su recuperación debe producir un único efecto.

## 8. Flujos principales y alternativos

### 8.1. Reserva de un turno

**Actor principal:** cliente. **Precondiciones:** tratamiento, profesional e intervalo identificados.

1. El cliente explora el catálogo público y consulta un tratamiento.
2. Inicia sesión o se registra para reservar.
3. Elige un profesional, consulta el calendario, selecciona una fecha y un horario disponible.
4. MELT verifica aptitud del profesional, disponibilidad vigente y ausencia de conflictos; crea un carrito y bloquea el intervalo durante un máximo de 10 minutos.
5. Reservas ejecuta checkout agrupado: recalcula todos los importes, la seña del 30 % y la vigencia de las retenciones; solicita al servicio Pagos un resultado simulado.
6. Si el simulador aprueba, Reservas confirma el conjunto en una transacción. Si rechaza, no confirma turnos; ante error posterior solicita una reversa simulada. La operación usa clave idempotente.
7. El cliente puede cancelar el turno conforme a RN-20.1; la devolución se registra mediante Pagos simulado cuando corresponde.

**Resultado actual:** turnos confirmados luego de la aprobación simulada de la seña; el saldo restante se abona presencialmente. Beneficios disponibles se pueden aplicar desde el flujo de carrito; puntos por tratamiento realizado se acreditan en un flujo asíncrono distinto. No hay cobros reales.

**Alternativas:**

- Sin sesión iniciada, se solicita ingreso o registro antes de reservar.
- Ante profesional no apto, horario no disponible o conflicto concurrente, se rechaza la reserva con motivo.
- Si el beneficio no puede aplicarse, no se confirma su consumo; el cliente puede corregir la selección.
- Si no puede determinarse el resultado, se verifica la misma operación antes de repetirla (RN-15, propuesta).
- Si falla el correo, el turno permanece confirmado y el envío queda pendiente de resolución (RN-14, propuesta).

### 8.2. Cancelación

1. El cliente consulta sus próximos turnos desde su perfil y solicita cancelar uno.
2. MELT verifica titularidad y estado del turno, y calcula la anticipación respecto del inicio para aplicar la política de devolución de la seña.
3. Acepta la cancelación y libera el intervalo.
4. Si había un beneficio aplicado, restituye automáticamente los puntos o el beneficio utilizado e informa el resultado.

Si la cancelación ocurre con menos de 24 horas de anticipación, se acepta y libera el turno, pero la seña no se devuelve. Repetir una cancelación válida no duplica devoluciones ni restituciones. Si la restitución de puntos o beneficios falla, queda pendiente de recuperación; el turno sigue cancelado. Las facultades administrativas y correcciones excepcionales permanecen pendientes (PD-05 y PD-06).

### 8.3. Finalización del tratamiento y acreditación

1. El profesional consulta su agenda y los detalles del turno.
2. Registra la realización efectiva del tratamiento.
3. Conforme al modelo propuesto, MELT valida responsable y estado confirmado y registra el turno como completado.
4. Inicia la acreditación de los puntos fijos correspondientes al tratamiento.
5. Registra una única acreditación y actualiza el saldo del cliente.

**Alternativas:** un actor sin permiso o un estado incompatible impiden la finalización. Repetir el registro o su procesamiento no vuelve a acreditar puntos. Si falla la acreditación, el turno permanece completado y la obligación queda pendiente o requiere revisión. La vigencia de los puntos ante cambios y su relación con tratamientos gratuitos se resuelven en PD-07 y PD-09. El pago presencial no determina esta transición.

### 8.4. Consulta, canje y aplicación de beneficios

1. El cliente consulta sus puntos, beneficios e historial desde su perfil.
2. Solicita un canje por descuento o tratamiento gratuito según las condiciones disponibles.
3. MELT verifica saldo y condiciones, registra un único canje y su débito y habilita el beneficio.
4. Durante la reserva, el cliente selecciona un beneficio disponible.
5. MELT valida y vincula su aplicación con el turno confirmado, informando el importe restante a pagar presencialmente.
6. Si el turno se cancela válidamente, restituye automáticamente el beneficio o los puntos utilizados, sin duplicados.

Si el saldo o las condiciones no permiten el canje, se rechaza sin débito ni beneficio. La concurrencia no permite usar dos veces los mismos puntos o beneficios. Una interrupción exige resolver la operación original sin duplicar sus efectos. La modalidad de canje previo o integrado en la reserva y la forma exacta de restitución están pendientes (PD-08).

## 9. Criterios de aceptación

Los criterios se verificarán durante la implementación. Los marcados como **propuestos** requieren aprobar sus reglas asociadas. Los parámetros comerciales se tomarán de las decisiones correspondientes.

| ID | Requisitos / reglas | Condición verificable |
| :--- | :--- | :--- |
| **CA-01** | RF-01, RF-02 | Sin iniciar sesión, se pueden explorar las cuatro categorías iniciales y consultar nombre, imagen y precio de los tratamientos. El detalle muestra descripción, duración, precio, imágenes, puntos y profesionales asociados. La búsqueda respeta filtros, páginas y orden y muestra resultados vacíos cuando no hay coincidencias. |
| **CA-02** | RF-02 | Una modificación del catálogo aparece en los resultados antes de superar el retraso máximo acordado en PD-11. |
| **CA-03** | RF-03, RF-04; RN-02–RN-04 | Sin sesión no se confirma una reserva. Con sesión, profesional apto y horario disponible seleccionado mediante calendario, fecha y horario, se confirma automáticamente un turno y se muestra su resumen. El pago presencial no condiciona la confirmación. |
| **CA-04** | RF-04; RN-02 | Seleccionar un profesional que no puede realizar el tratamiento provoca un rechazo con motivo y no crea un turno confirmado. |
| **CA-05** | RF-04; RN-03, RN-04 | Un intervalo fuera de la disponibilidad o superpuesto con un turno confirmado del mismo profesional no puede confirmarse. Debe comprobarse una superposición parcial además de la coincidencia total. Duración y casos de intervalos contiguos dependen de PD-03. |
| **CA-06** | RF-04, RF-15; RN-04, RN-05 | Ante dos solicitudes simultáneas válidas salvo por competir por intervalos incompatibles del mismo profesional, se confirma una sola. Repetir la misma intención no incrementa el número de reservas. |
| **CA-07** | RF-06; RN-07; RN-20 | Una solicitud válida del titular cancela el turno y libera el intervalo. Con 24 horas o más de anticipación se devuelve el 100 % de la seña; con menos de 24 horas no se devuelve. La inasistencia tampoco genera devolución. La restitución de puntos o beneficios aplicados no se duplica. |
| **CA-08** | RF-07, RF-08; RN-06, RN-08 | El registro de finalización por el profesional inicia la acreditación de los puntos fijos del tratamiento. Repetirlo o reprocesarlo produce una única acreditación. Reservar o cancelar sin realización no acredita puntos. |
| **CA-09** | RF-05, RF-07; RN-10, RN-13 | **Propuesto:** un cliente no consulta turnos ajenos ni registra finalizaciones; un profesional no completa turnos de otro profesional; un turno cancelado no puede completarse por el flujo ordinario. |
| **CA-10** | RF-08, RF-09; RN-12, RN-14 | **Propuesto:** si falla la acreditación tras registrar la realización, el turno sigue completado y la acreditación pendiente puede identificarse. Al recuperarla, existe un único movimiento asociado y el saldo refleja una sola acreditación. |
| **CA-11** | RF-09, RF-10; RN-11, RN-12 | Con saldo y condiciones suficientes, el canje por descuento o tratamiento gratuito registra un beneficio y su débito. Al aplicarlo a una reserva se informa el importe restante presencial. Sin saldo suficiente no hay débito ni beneficio. |
| **CA-12** | RF-10; RN-11 | Los canjes y aplicaciones concurrentes no permiten consumir dos veces los mismos puntos o beneficios. Repetir un canje o una restitución conserva un único efecto. |
| **CA-13** | RF-11; RN-14 | Una reserva confirmada genera un correo electrónico de confirmación. **Propuesto:** si falla su envío, el turno sigue confirmado y el efecto pendiente puede identificarse y recuperarse. |
| **CA-14** | RF-12–RF-14 | El administrador gestiona tratamientos, categorías, profesionales, disponibilidad, turnos, puntos fijos por tratamiento y beneficios. Las operaciones posteriores aplican la configuración vigente acordada (PD-04 y PD-09). |
| **CA-15** | RF-15; RN-15 | **Propuesto:** una respuesta perdida se informa como resultado por verificar y se recupera la operación original sin duplicar reservas, puntos, canjes ni restituciones. |
| **CA-16** | RF-05, RF-09 | El cliente puede consultar próximos turnos, historial, saldo y beneficios; el profesional puede consultar su agenda y detalles de sus turnos. |
| **CA-17** | RF-16 | La API de consulta permite listar tratamientos con filtros y consultar información descriptiva y precios según su contrato versionado. Los criterios de la capacidad externa se definirán al acordar la integración. |

## 10. Decisiones pendientes

Se conservan los identificadores originales. Las decisiones resueltas se registran como tales; los aspectos restantes requieren definición o aprobación.

### 10.1. Usuarios y catálogo

| ID | Estado y decisión |
| :--- | :--- |
| **PD-01** | Resuelta para las operaciones implementadas: roles en Auth, permisos por operación y comprobaciones de propiedad en Reservas; autorización administrativa en Catálogo. Las funciones de perfil y gestión que no tienen endpoint siguen fuera del alcance implementado. |
| **PD-02** | Resuelta para el catálogo web/API actual: búsqueda por nombre y `keywords`, categorías, precio máximo, paginación y orden por nombre/precio. La sincronización con un índice OpenSearch permanece pendiente (PD-11). |
| **PD-04** | Pendiente: cambios o retiros de tratamientos y profesionales, cambios de aptitud o disponibilidad con reservas existentes y conservación histórica. |
| **PD-11** | Pendiente: retraso máximo entre cambios del catálogo y búsqueda y comunicación de información desactualizada. |

### 10.2. Agenda y turnos

| ID | Estado y decisión |
| :--- | :--- |
| **PD-03** | Resuelta para el flujo local implementado: duración desde Catálogo, agenda semanal, zona horaria configurable, intervalos de 15 minutos, disponibilidad y validación de cruces; no hay calendario de excepciones ni reglas de anticipación adicionales. La confirmación requiere pago aprobado según RN-20. |
| **PD-05** | Resuelta para el cliente: cancelación antes del inicio, liberación del horario y política de seña según RN-20. Pendientes: facultades administrativas y tratamiento de excepciones. |
| **PD-06** | Pendiente: aprobar estados y transiciones de turnos, RN-10 y estados de beneficios; decidir sobre inasistencias, reprogramación y correcciones. El profesional registra los tratamientos completados; cualquier facultad excepcional del administrador requiere confirmación. |

### 10.3. Puntos y beneficios

| ID | Estado y decisión |
| :--- | :--- |
| **PD-07** | Resuelta la cantidad fija configurable y la acreditación al completar. Pendientes: valores concretos, puntos de tratamientos gratuitos o con descuento, vencimiento y ajustes. |
| **PD-08** | Resueltos descuentos, tratamientos gratuitos, aplicación al reservar, importe restante presencial y restitución sin duplicados. Pendientes: costos, elegibilidad, cálculo del descuento, límites, canje previo o integrado y modalidad exacta de devolución de puntos o beneficio. |
| **PD-09** | Pendiente: vigencia de cambios de puntos y beneficios para reservas existentes, tratamientos completados y canjes iniciados. |

### 10.4. Notificaciones, recuperación e integración

| ID | Estado y decisión |
| :--- | :--- |
| **PD-10** | Resueltas las notificaciones al cliente por confirmación/cancelación del profesional y la estrategia inicial de recuperación: intento inicial, hasta 3 reintentos con espera creciente y cola de errores para revisión. Pendientes: proveedor de correo, procedimiento operativo de reenvío manual, otros eventos/destinatarios y aprobación de RN-14 junto con PD-13. |
| **PD-12** | Resuelta la capacidad propia: API de consulta de tratamientos con filtros, contrato OpenAPI v1 y mock. Pendiente la capacidad consumida: el grupo de Cocina Fantasma aún debe entregar su API de búsqueda de sedes cercanas y su contrato; no se implementa ni se simula como integración real en este sistema. |
| **PD-13** | Acordada la prevención de reservas, acreditaciones, canjes y restituciones duplicadas. Pendientes: aprobación de RN-12 y RN-15, estados de procesamiento, reconocimiento de una misma intención y responsables de recuperación. |

Los precios y horarios cargados para demostración no son valores comerciales. **Esta nota pertenece a la especificación original y está obsoleta para la versión actual:** la interfaz ahora incluye administración de catálogo y agenda según rol. El estado actual se documenta en la sección 13 y en la [matriz de cumplimiento](docs/MATRIZ-CUMPLIMIENTO.md).


## 12. Decisiones funcionales incorporadas — revisión de alcance

Esta sección consolida las correcciones acordadas por el equipo. Las reglas siguen siendo normativas; su estado de implementación comprobado se indica en la sección 13.

### 12.1. Puntos y notificaciones

- **RN-16 — Acreditación posterior a la realización.** Los puntos se acreditan únicamente después de que el profesional registra el tratamiento como completado. La reserva, el bloqueo temporal del carrito y el pago de la seña no acreditan puntos. Cada acreditación se vincula a un identificador único de tratamiento/evento y se registra transaccionalmente con una restricción de unicidad, para que repetir o recibir varias veces el mismo evento no duplique puntos. Los fallos se reintentan; si no se resuelven, la operación queda identificada para revisión sin perder el registro de finalización.
- **RN-17 — Correos por acciones del profesional.** Cuando el profesional confirma un turno o lo cancela, MELT genera un evento identificado de forma única para enviar un correo al cliente con el estado actualizado y los datos relevantes del turno. El envío es asíncrono y no revierte la operación de negocio si falla. Se realiza un intento inicial y hasta 3 reintentos automáticos con esperas crecientes. Si persiste el fallo, el mensaje pasa a una cola de errores (dead-letter) para revisión y eventual reenvío manual. Se registra el estado de procesamiento para no volver a tratar como nuevo un evento ya resuelto. Si el proveedor acepta el correo pero se pierde la respuesta, el resultado puede ser incierto y debe revisarse, ya que no puede garantizarse ausencia absoluta de duplicados solo desde el consumidor.

### 12.2. Compatibilidad de tratamientos

- **RN-18 — Incompatibilidad por profesional y por cliente.** Antes de confirmar o bloquear un horario, el sistema evalúa las reglas de compatibilidad configuradas para los tratamientos involucrados. Debe impedirse una combinación incompatible tanto cuando afecte la agenda de un mismo profesional como cuando dos tratamientos del mismo cliente se solapen y sean incompatibles entre sí. No se prohíben automáticamente todos los tratamientos de una misma categoría: la incompatibilidad se define mediante reglas explícitas entre tratamientos/categorías y, cuando corresponda, intervalos de separación.
- La validación debe contemplar reservas confirmadas y bloqueos temporales activos. Los controles concurrentes deben impedir que dos solicitudes incompatibles se acepten simultáneamente.

### 12.3. Carrito y bloqueo temporal

- **RN-19 — Bloqueo temporal de 10 minutos.** Al incorporar al carrito una selección que requiere un horario concreto, MELT crea un bloqueo temporal de ese intervalo por 10 minutos. El bloqueo evita que otra solicitud confirme o bloquee un turno incompatible mientras está vigente.
- El bloqueo tiene identificador, titular y vencimiento explícito. Al confirmar la reserva, se transforma/libera de forma atómica según el flujo definido; al quitar el turno del carrito, abandonar la operación o vencer los 10 minutos, el intervalo se libera. Una reserva no confirmada nunca debe quedar bloqueada indefinidamente.
- La creación y confirmación del bloqueo deben ser seguras ante concurrencia y repetición. La expiración no puede depender exclusivamente de que el cliente vuelva a abrir la página.

### 12.4. Seña

- **RN-20 — Seña requerida para reservar.** Para confirmar una reserva, el cliente debe abonar una seña equivalente al **30 % del precio del tratamiento**, calculada sobre el precio vigente mostrado en el resumen antes de confirmar. La reserva solo queda confirmada cuando se registra el resultado exitoso del pago; un pago pendiente o fallido no confirma el turno.
- **RN-20.1 — Devolución por cancelación.** Si el cliente cancela con **24 horas o más de anticipación** respecto del inicio del turno, se devuelve el **100 % de la seña**. Si cancela con menos de 24 horas de anticipación, la seña no se devuelve. La cancelación tardía libera el turno igualmente.
- **RN-20.2 — Inasistencia.** Si el cliente no asiste al turno, la seña no se devuelve.
- La seña es distinta del saldo restante del tratamiento y de los puntos del programa. El importe se informa antes de pagar. El servicio `reservas` verifica la aprobación, el importe exacto, la moneda ARS y la referencia del carrito a través del adaptador `pagos`; las devoluciones se ejecutan por el mismo adaptador con una clave idempotente y tienen un endpoint para reintentar un fallo. El pago en producción y los cambios de turno quedan fuera de la configuración local.

### 12.5. Búsqueda, datos y caché

- **RN-21 — Palabras clave del tratamiento.** Cada tratamiento incluye un arreglo `keywords` con términos relacionados, sinónimos y expresiones de búsqueda. La búsqueda indexa estos términos además del nombre y los campos ya definidos; los filtros, paginación y ordenamiento del contrato público deben mantenerse consistentes con el índice.
- **Decisión de persistencia:** se separa la persistencia documental en dos bases lógicas MongoDB: una para Catálogo y otra para Profesionales. Cada servicio conserva la propiedad de sus datos; compartir una instancia física puede evaluarse para el entorno local, pero las bases y permisos lógicos se mantienen separados.
- **Decisión de caché:** se reemplaza Redis por Memcached para la caché de lecturas del catálogo. La caché no es fuente de verdad; se documentarán TTL, invalidación, métricas de hit/miss y comportamiento ante indisponibilidad.

### 12.6. Criterios de aceptación adicionales

- **CA-16:** los puntos no se acreditan al reservar ni al pagar la seña; se acreditan una sola vez al registrar la finalización.
- **CA-17:** la confirmación o cancelación de un turno por el profesional genera un correo al cliente. Una falla temporal del correo no revierte el cambio y el envío pendiente puede recuperarse.
- **CA-18:** se rechazan combinaciones incompatibles tanto para un profesional como para un cliente, incluso ante solicitudes concurrentes.
- **CA-19:** un horario incorporado al carrito queda bloqueado durante un máximo de 10 minutos y se libera al confirmar, quitarlo o vencer el bloqueo.
- **CA-20:** no se confirma una reserva si la seña del 30 % no tiene pago exitoso registrado; con cancelación a 24 horas o más se devuelve el 100 %, con menos de 24 horas o ante inasistencia no se devuelve. Los pagos y devoluciones no se procesan dos veces ante reintentos.
- **CA-21:** una búsqueda por términos asociados encuentra tratamientos por sus `keywords` y respeta filtros, paginación y orden.
- **CA-22:** cada acreditación de puntos está vinculada a un identificador único y repetir el mismo evento no altera el saldo por segunda vez; si el procesamiento falla de forma persistente, queda registrado para revisión.
- **CA-23:** los correos se procesan con un intento inicial y hasta 3 reintentos automáticos; los fallos persistentes quedan registrados en una cola de errores para revisión, sin revertir el estado del turno.

## 13. Estado de implementación y verificación

**Referencia del estado de código:** `develop`, `ea1ead7e85f533ba54aff85bd95958bb749897a0`. La siguiente tabla es una revisión documental de rutas/código y no una nueva ejecución de pruebas. El registro histórico de pruebas y límites está en [MATRIZ-CUMPLIMIENTO.md](docs/MATRIZ-CUMPLIMIENTO.md) y `docs/evidence/`.

| Área | Estado visible en el código de referencia | Evidencia / límite |
|---|---|---|
| Auth y roles | Cliente, profesional, recepción, administrador y superadministrador; listar/cambiar roles es operación de superadministrador. | `services/auth/cmd/api`; no se verificó un despliegue actual en esta tarea. |
| Catálogo y profesionales | CRUD, bajas lógicas, asociación bidireccional y vinculación Auth-perfil; APIs públicas. | `services/catalogo-profesionales/cmd/api`; el ID Auth se omite del perfil público. |
| Búsqueda y caché | OpenSearch para búsqueda; Memcached para listados, TTL 30 s e invalidación por generación. | Evidencia local y benchmark histórico en `CATALOGO-PROFESIONALES-ETAPA-1.md`; muestra no extrapolable. |
| Agenda y turnos | Agenda semanal, disponibilidad, carrito/retenciones, turnos, cancelación, completado e inasistencia. | Control de propiedad en backend; ver [manual Reservas](docs/servicios/RESERVAS.md). |
| Checkout/pago | Checkout agrupado y pago/devolución simulados; no existe cobro externo en esta versión. | `services/reservas/cmd/api/checkout.go`, `services/pagos/main.go`, ADR-015. |
| Beneficios | Puntos, reglas, recompensas, canjes, integración de eventos, acreditación de partner y proxy GhostKitchen. | No confundir evento de tratamiento realizado con pago; proveedor D9 asignado oficialmente sin confirmar. |
| Notificaciones por correo | No se encontró servicio/cliente de correo operativo en la configuración versionada. | Los requisitos de notificación de este documento permanecen pendientes; RabbitMQ de eventos de beneficios no acredita envío de correo. |
| Persistencia | MongoDB replica set y bases por servicio. PostgreSQL está declarado en Compose, sin consumidor business localizado. | No afirmar que MELT usa PostgreSQL funcionalmente. |
| Resiliencia y observabilidad | Logs/trazas y métricas parciales; ejercicio de réplica produjo 1 HTTP 504 de 12 solicitudes. | Failover transparente no comprobado; revisar [Postmortem](docs/POSTMORTEM.md). |
| Despliegue | Configuración de Render histórica existe en documentación/configuración versionada. | La URL, commit y servicios públicos actuales no se verificaron en esta sesión. |

Los escenarios de pruebas y los comandos vigentes están en [manual de desarrollo](docs/manuales/DESARROLLO.md). Ningún resultado anterior se atribuye automáticamente al commit de referencia si la evidencia original identifica otro commit.

