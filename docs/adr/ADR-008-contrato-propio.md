# ADR-008 — Contrato propio (API de consulta de tratamientos)

**Estado:** Aceptado (decisión inicial completa)  
**Fecha:** 2026-10-08  
**Entrega:** 1 — 9 de octubre de 2026  
**Decisores:** Equipo MELT

## Contexto y problema

El proyecto exige que MELT ofrezca una capacidad formal y versionada para que otro grupo (sistema consumidor) pueda integrarse. La capacidad debe ser útil, estable y limitada a datos públicos.

MELT ya tiene un catálogo de tratamientos con información descriptiva y precios. Esa información es natural para exponer hacia afuera sin revelar agendas privadas, clientes, saldos ni movimientos de puntos.

Hay que decidir **qué** se expone, **quién** es el responsable, **cómo** se versiona y qué alternativas se descartaron.

## Alternativas consideradas

1. **API de disponibilidad / agenda**  
   Útil para un consumidor que quisiera reservar, pero expone datos sensibles de ocupación y complica la autorización y el control de concurrencia desde fuera. No se elige como capacidad inicial.

2. **API de puntos o beneficios del cliente**  
   Requiere autenticación fuerte y expone datos personales; no es adecuada como capacidad pública de integración entre grupos en esta etapa.

3. **API completa de administración del catálogo (escritura)**  
   Fuera de alcance: el catálogo lo administra el equipo de MELT; el consumidor solo necesita consultar.

4. **API versionada de consulta de tratamientos (opción elegida)**  
   Lectura de tratamientos con filtros, descripción y precios. Alineada con el dominio, con datos públicos y con el servicio Catálogo y Profesionales.

## Decisión

MELT publicará una **API versionada de consulta de tratamientos**.

- **Responsable:** el microservicio **Catálogo y Profesionales**.
- **Exposición:** a través del API Gateway (entrada pública), bajo el prefijo de versión.
- **Operaciones propuestas:**

| Operación | Resultado previsto |
| :--- | :--- |
| `GET /api/v1/tratamientos` | Lista de tratamientos con búsqueda, filtros, paginación y ordenamiento. |
| `GET /api/v1/tratamientos/{id}` | Información descriptiva y precio del tratamiento identificado. |

- **Datos públicos previstos:** identificador, nombre, categoría, descripción y precio. Imágenes, duración y profesionales asociados se acordarán en el contrato formal.
- **No se exponen:** clientes, agendas privadas, credenciales, saldos ni movimientos de puntos.

### Versionado

- La versión inicial será **v1** (`/api/v1/...`).
- Los cambios incompatibles requerirán una nueva versión explícita (v2, …) y comunicación al consumidor.
- El contrato se documentará en OpenAPI y se almacenará en `docs/contracts/tratamientos-v1.openapi.yaml`, acompañado de `docs/contracts/README.md` y un mock coherente con ejemplos y errores.

El contrato OpenAPI y el mock se elaborarán por separado; este ADR no los implementa.

## Justificación

- El catálogo es la capacidad más estable y menos sensible del sistema: información comercial pública del centro.
- Permite a otro grupo integrar búsqueda o visualización de tratamientos y precios sin acoplarse a la agenda ni a los beneficios.
- Encaja con la responsabilidad ya asignada a Catálogo y Profesionales (ADR-001) y con el requisito de publicar una capacidad propia con contrato formal.
- El versionado explícito protege al consumidor frente a cambios incompatibles y al equipo frente a roturas no planificadas.

## Consecuencias

### Positivas

- Capacidad clara, acotada y defendible en la evaluación.
- El consumidor puede filtrar y obtener precios sin conocer la estructura interna de MELT.
- Catálogo mantiene el control de lo que se publica; no se abren puertas a datos privados.
- Facilita pruebas de contrato y un mock independiente para el otro grupo.

### Negativas / riesgos

- Hay que mantener el contrato y el mock alineados con la implementación real a lo largo del proyecto.
- Los filtros, límites de paginación, formatos de importe/moneda y códigos de error deben cerrarse en el OpenAPI; cualquier omisión generará fricción con el consumidor.
- Cambios incompatibles obligan a versionar y coordinar; no se puede “romper en silencio”.

### Aspectos pendientes (fuera de este ADR, para la misma entrega o siguientes)

- Redacción del contrato OpenAPI v1 (parámetros, esquemas, errores, ejemplos).
- Mock ejecutable coherente con el contrato.
- Condiciones de acceso (si se requiere API key u otro mecanismo en etapas posteriores).
- Publicación de la capacidad real en un entorno accesible cuando exista implementación.

## Actualización y validación — Entrega 2 (23/10/2026)

Se mantiene la decisión de ofrecer consultas GET de tratamientos en **`/api/v1/tratamientos`** y **`/api/v1/tratamientos/{id}`**. El contrato vigente es OpenAPI 3.0.3, documento `1.0.0`, API `v1`, con identificadores de texto, imágenes, duración y keywords. El contrato y el mock ya existen; el mock histórico conserva diferencias documentadas y no prueba conformidad de la API real.

La auditoría confirmó que NGINX solo publicaba la ruta con `/catalogo/`. Se agregó un alias de lectura hacia el mismo upstream, sin cambiar Catálogo ni los esquemas. La ruta anterior se conserva para los clientes existentes. Es un cambio compatible con v1: no requiere una nueva versión de API ni modifica el OpenAPI vigente.

**URL base local:** `http://localhost:8080/api/v1`. **URL pública de Internet:** pendiente de publicación; no se considera accesible para otros grupos por funcionar en localhost. Las consultas locales no requieren autenticación; las condiciones del despliegue compartido deben acordarse antes de su publicación.

Se ejecutaron 28 casos de contrato con esquemas de respuesta, parámetros, errores y comparación entre ambas rutas; se comprobaron MongoDB real, dos réplicas, correlación y trazas. Pasaron las regresiones D3, D5 y D11. Solo se recargó NGINX con autorización; no se modificaron datos ni se reiniciaron contenedores.

Las limitaciones incluyen una muestra de un documento, entradas límite todavía divergentes, mock desactualizado y errores de dependencia sin prueba de caída. El procedimiento, la estrategia de compatibilidad y los pendientes se detallan en la [documentación de consumo](../contracts/README.md), con [evidencia D8](../evidence/d8-2026-10-23.json).

Quedan pendientes la URL accesible y su continuidad operativa, las condiciones de acceso definitivas y la integración efectiva del grupo consumidor. La capacidad queda operativa y validada localmente para el alcance comprobado, sin afirmar cumplimiento completo de la publicación e integración entre grupos.
