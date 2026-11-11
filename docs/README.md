# Documentación MELT

Esta carpeta combina guías vigentes con registros históricos de entregas. La implementación de referencia es `develop` en `ea1ead7e85f533ba54aff85bd95958bb749897a0`; para el estado actual prevalecen los manuales de esta página y el código de esa revisión. Los JSON de `evidence/` conservan fecha y contexto originales: no deben presentarse como pruebas ejecutadas contra el HEAD actual.

Para el inventario archivo por archivo, incluidas las referencias PDF y la evidencia JSON, ver [INVENTARIO.md](INVENTARIO.md).

## Guías vigentes

| Necesidad | Documento |
|---|---|
| Comprender el sistema y sus flujos | [Arquitectura y diagramas](ARCHITECTURE.md) |
| Instalar, configurar, ejecutar y diagnosticar | [Operaciones](OPERACIONES.md) |
| Usar las pantallas según rol | [Manual de usuario](manuales/USUARIO.md) |
| Desarrollar, probar y agregar cambios | [Manual de desarrollo](manuales/DESARROLLO.md) |
| Entender rutas, ownership, dependencias y datos | [Manuales de microservicios](servicios/README.md) |
| Encontrar contratos HTTP | [Contratos OpenAPI e integraciones](contracts/README.md) |
| Comparar el estado verificable con la consigna | [Matriz de cumplimiento](MATRIZ-CUMPLIMIENTO.md) |
| Resolver síntomas comunes | [Troubleshooting](TROUBLESHOOTING.md) |
| Aclarar términos | [Glosario](GLOSARIO.md) |
| Revisar aprendizajes y fallos observados | [Postmortem](POSTMORTEM.md) |

## Índice de documentación existente

### Arquitectura, operación y desarrollo

- `ARCHITECTURE.md`: arquitectura vigente, propiedad de datos y diagramas.
- `OPERACIONES.md`: Compose, variables, observabilidad, ejecución local y despliegue remoto.
- `manuales/USUARIO.md`, `manuales/DESARROLLO.md`: guías por audiencia.
- `TROUBLESHOOTING.md`, `GLOSARIO.md`, `MATRIZ-CUMPLIMIENTO.md`.
- `LOCAL-DEV.md`: guía previa de desarrollo local; sigue siendo útil para ejemplos de Postman y recorridos, pero contiene valores de puertos/datos demo que pueden depender del entorno.
- `OBSERVABILITY.md`, `PERSISTENCE.md`, `COMMUNICATION.md`: evidencias de ejercicios fechados; describen el alcance y limitaciones de dichos ensayos, no una garantía operacional permanente.
- `ENTREGA-2.md`: registro de la entrega académica de octubre de 2026; conservar como histórico.

### Servicios, negocio e integración

- `servicios/`: manual vigente por servicio.
- `RESERVAS-WEB.md`: preparación de datos y demostración de agenda/carrito.
- `FLUJO-BENEFICIOS.md`: puntos, canjes, eventos y mensajes fallidos.
- `CATALOGO-PROFESIONALES-ETAPA-1.md`: CRUD, búsqueda, caché y pruebas históricas del módulo.
- `ACREDITACION-PUNTOS.md`, `IMPLEMENTACION-ACREDITACION-PUNTOS.md`, `INTEGRACION-GIMNASIO.md`: operación y consumo de la API externa; no contienen la clave compartida.
- `GHOSTKITCHEN.md`: proxy y configuración del proveedor; las respuestas reales dependen de credencial/servicio externo.
- `PRUEBAS-RENDER-ACREDITACION.md`: evidencia fechada de un endpoint público; no implica que todo el despliegue siga igual.

### ADR y contratos

- `adr/README.md` y `adr/ADR-001` a `ADR-015`: índice de estado y decisiones por fecha. Los títulos iniciales/propuestos son parte del historial; revisar las enmiendas para identificar el estado aplicado. No se borran decisiones reemplazadas.
- `contracts/`: especificaciones OpenAPI y mock. Cada contrato declara su versión y cobertura. Algunos son específicos de una capacidad y no constituyen por sí solos una especificación exhaustiva de todas las rutas del Gateway.
- `contracts/registro-externo-v1.openapi.yaml`: histórico retirado; la implementación actual de acreditación externa está en `acreditar-puntos-v1.openapi.yaml`.
- `contracts/mock/`: mock de catálogo con datos fijos, no reemplaza la API real.

### Evidencias históricas

`evidence/*.json` contiene capturas de verificaciones fechadas (D3, D5, D8, D11, fallos de réplica y cierre). Consultar la fecha, rama/commit y límites descritos en el documento asociado antes de citar resultados. Una evidencia antigua no certifica el despliegue ni las dependencias actuales.

## Enunciado y referencias

`SPEC.md` recoge requisitos y estado técnico. `referencias/` incluye el PDF del enunciado y material de cátedra. No se pudo extraer el contenido del PDF en esta sesión porque el entorno no dispone de lector/extractor PDF utilizable y la tarea prohíbe instalar dependencias. Por ello, la matriz identifica esta limitación y no afirma una comparación página por página contra el PDF; debe validarla una persona con el enunciado abierto.
