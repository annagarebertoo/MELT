# ADR-014 — D9: Consumo del proveedor externo

**Estado:** Pendiente de decisión; integración bloqueada por falta de información del proveedor.

**Fecha:** 2026-10-23. **Hito:** Entrega 2.

**Rama de investigación:** `feature/sofia-d9-integracion-externa`.

**Base protegida D8:** `3601c0d2f713da15991bc0974aeb9694332a8f5b`.

## Contexto y alcance

El enunciado definitivo `Enunciado TP Final (3).pdf` identifica D9 como consumo del proveedor: incorporación de la capacidad externa, adaptación al contrato y comportamiento ante errores, cambios o indisponibilidad. La Entrega 2 del 23/10 exige la decisión D9. La presentación posterior exige la capacidad del proveedor incorporada a un flujo importante, y la defensa contempla pruebas de contrato y comportamiento ante fallas.

Las relaciones entre proveedor y consumidor son asignadas por los docentes. La llamada debe salir del microservicio responsable del flujo que consume la capacidad, sin iniciarse desde el frontend ni canalizarse mediante el gateway propio. Una consulta aislada sin consecuencias para el negocio no acredita la integración.

Este registro documenta la investigación y el bloqueo; no presenta una decisión cerrada ni una integración implementada. El archivo existente [ADR-009](ADR-009-compatibilidad-carrito-sena.md) documenta reglas de compatibilidad, carrito y seña. Su número de archivo no implica que cubra D9 del enunciado. Se conserva intacto y se utiliza el siguiente número libre, ADR-014, para registrar D9 por separado.

## Evidencia encontrada

| Elemento revisado | Resultado |
| :--- | :--- |
| [README](../../README.md), sección de integración | Prevé consumir una capacidad de otro grupo según asignación docente; no identifica proveedor ni operaciones. |
| [Arquitectura](../ARCHITECTURE.md), sección 9.2 | Declara pendientes el proveedor, la capacidad, el flujo y el servicio responsable. No permite inferir una API concreta. |
| [ADR-009](ADR-009-compatibilidad-carrito-sena.md) | Define reglas de negocio. Mantiene pendiente el proveedor/modalidad de pago; no identifica al grupo proveedor. |
| Contratos disponibles | Solo `docs/contracts/tratamientos-v1.openapi.yaml`: contrato propio de MELT, API v1, documento 1.0.0. Corresponde a D8; no es un contrato del proveedor que MELT debe consumir. |
| Código de servicios | Catálogo tiene MongoDB y telemetría; Reservas y Beneficios son esqueletos HTTP de salud. No se encontró cliente HTTP de negocio hacia un proveedor externo. |
| Clientes HTTP encontrados | El cliente en `persistence_integration_test.go` consulta el gateway local; los exportadores de observabilidad envían telemetría a Jaeger. Ninguno acredita D9. |
| Variables de configuración | En código y Compose se encontraron variables de MongoDB, puertos y telemetría, además de configuración de infraestructura. No hay URL, token ni parámetros identificados como configuración de un proveedor de negocio. |
| Entorno de ejecución | Solo se inspeccionaron nombres de variables: Catálogo usa `MONGODB_URI`, `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`, `PORT` y `PATH`; Reservas y Beneficios usan `PORT` y `PATH`. No se inspeccionaron ni exportaron valores de credenciales. |
| Once ramas locales | Todas tienen cero commits exclusivos respecto de la base D8. No hay trabajo adicional de D9 pendiente de integración en esas referencias locales. |

Se revisaron `develop`, `main`, `docs/arquitectura-inicial`, `docs/readme-inicial`, `docs/spec-inicial`, `feature/estructura-microservicios`, `feature/sofia-entrega-2` y las ramas individuales D3, D5, D8 y D9. Se inspeccionaron sus árboles, archivos Go, módulos, configuraciones y contratos disponibles. No se consultó GitHub ni se actualizaron referencias remotas; puede existir trabajo no descargado o información compartida fuera del repositorio que esta auditoría no acredita.

Las menciones a correo y pago son necesidades del dominio, no evidencia de que el proveedor asignado ofrezca alguna de esas capacidades. No se elige una de ellas por inferencia.

## Estado de la decisión

| Aspecto | Estado |
| :--- | :--- |
| Grupo y sistema proveedor | No identificados en la evidencia local. |
| Operaciones que MELT necesita consumir | Pendientes de conocer la capacidad asignada y acordar su efecto en un flujo importante. |
| Contrato formal y versión verificable | No disponibles en el repositorio. |
| URL base y ambiente de pruebas | No disponibles para el proveedor. La URL local de D8 corresponde a MELT. |
| Autenticación y credenciales | Mecanismo desconocido; no hay credenciales del proveedor identificadas. No se presupone que una API pública requiera credenciales. |
| Servicio consumidor y adaptación al dominio | Sin seleccionar; dependerán del flujo acordado. |
| Cliente, test de contrato y manejo de fallas | No implementados para D9. |
| Alternativas y justificación de integración | No evaluables con precisión hasta recibir contrato y capacidad reales. |

Se mantiene el límite arquitectónico de consumir desde el microservicio responsable. No se introducen endpoints, simulaciones, dependencias ni variables ficticias. Tampoco se fijan timeouts o reintentos de negocio sin conocer las operaciones y sus garantías de idempotencia.

## Información que debemos solicitar

1. **A los docentes o al equipo MELT:** confirmar qué grupo y sistema nos corresponde consumir y quién es el contacto técnico. La identidad del grupo no puede deducirse del dominio de MELT.
2. **Al grupo proveedor:** enlace al contrato formal publicado y versionado, con versión o revisión identificable; operaciones disponibles, parámetros, esquemas de entrada/salida, ejemplos válidos y errores. Si ofrece eventos, incluir contrato y mecanismo de entrega correspondientes.
3. **Al grupo proveedor:** URL base accesible del ambiente de pruebas, prefijo de versión, disponibilidad prevista hasta finalizar la evaluación y procedimiento para avisar cambios o interrupciones.
4. **Al grupo proveedor:** condiciones de autenticación y uso; si requiere credenciales, cómo obtener credenciales de prueba por un canal seguro, permisos necesarios, vencimiento y renovación. No enviar secretos al repositorio.
5. **Al grupo proveedor:** límites de uso, comportamiento ante errores o lentitud y garantías de idempotencia cuando haya efectos de escritura, incluidos resultados inciertos y reintentos admitidos.
6. **Al grupo proveedor y al equipo MELT:** datos o casos de prueba autorizados y forma segura de verificar resultados sin crear efectos no deseados; acordar qué operación soportará un flujo importante y qué sucede si el proveedor falla.

## Próximo paso y criterios para desbloquear

Una vez confirmados proveedor, contrato verificable y URL, revisar las operaciones y proponer la integración mínima: flujo y efecto de negocio, microservicio responsable, adaptación de datos, configuración, tratamiento de errores y resultados inciertos, timeouts, reintentos compatibles con idempotencia y test de contrato capaz de detectar incompatibilidades.

Ese plan debe recibir autorización antes de hacer llamadas externas, modificar código de negocio, introducir credenciales o agregar dependencias. La investigación actual no autoriza automáticamente esa implementación.

## Resultado para la Entrega 2

Se pueden entregar ahora la auditoría, la delimitación arquitectónica y la lista concreta de información faltante. **D9 no queda completa:** falta la información necesaria para justificar y cerrar la decisión sobre el consumo del proveedor. Documentar el bloqueo no sustituye la decisión exigida para el 23/10.

No se hicieron llamadas a proveedores ni pruebas de integración externa. No se afirman aprobados el contrato externo, la autenticación, el consumo real ni el comportamiento ante fallas. No se modificaron servicios, reglas, contratos propios o infraestructura durante esta etapa.
