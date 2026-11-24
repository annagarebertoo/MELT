# ADR-013 — Capacidad y costos

> **Actualización 24/11/2026:** la prueba de carga se ejecutó; resultados, límite principal, alternativa de escalado y costos están en [Resultados medidos](#resultados-medidos--24112026). Lo que sigue hasta esa sección es la historia de la decisión.

**Estado:** Aceptado — resultados medidos el 24/11/2026 (ver al final)  
**Fecha:** 2026-10-21  
**Entrega:** 2 — 23 de octubre de 2026  
**Decisores:** Equipo MELT

## Contexto y problema

MELT necesita evaluar cuánto trabajo puede soportar el sistema y cómo se comporta ante diferentes niveles de carga. También es necesario identificar los límites de los componentes y considerar los recursos requeridos para ejecutar la solución.

Sin mediciones, no es posible establecer de manera confiable la capacidad del sistema ni justificar decisiones de escalado.

## Alternativas consideradas

1. **Estimar la capacidad sin ejecutar pruebas:** se descarta como método principal porque no permite verificar el comportamiento real.
2. **Realizar pruebas de carga y registrar métricas:** permite medir tiempos de respuesta, cantidad de solicitudes procesadas y errores bajo distintas cargas.
3. **Escalar recursos sin mediciones previas:** se descarta como decisión inicial porque puede aumentar los costos sin demostrar una mejora suficiente.

## Decisión

Se propone evaluar la capacidad de MELT mediante pruebas de carga sobre los servicios seleccionados, registrando al menos la latencia, el volumen de solicitudes procesadas y la tasa de errores.

A partir de los resultados se deberán identificar los límites observados y analizar posibles alternativas de escalado y sus costos.

No se establecerán cifras de capacidad ni costos estimados como resultados comprobados hasta contar con mediciones y datos de infraestructura.

## Consecuencias

- Se obtendrá evidencia para evaluar el comportamiento del sistema bajo carga.
- Será posible identificar cuellos de botella y necesidades de escalado.
- Las conclusiones dependerán de las condiciones y los límites de las pruebas realizadas.
- El análisis de costos requerirá documentar los recursos y el entorno utilizados.

## Pendientes

Quedan pendientes la definición del escenario de carga, la ejecución de las pruebas, el registro de resultados, la identificación de límites y el análisis de costos.

Esta decisión documenta una propuesta de evaluación; no implica que las pruebas se hayan ejecutado ni que la capacidad esté medida.

## Revisión para la Entrega 2 (23/10/2026)

La evidencia actual consiste en una instantánea de recursos Docker y doce consultas secuenciales para verificar distribución. No es un test de carga ni permite establecer throughput máximo, percentiles bajo concurrencia, cuello de botella o costos. Catálogo no tiene límites explícitos de memoria o CPU en la configuración inspeccionada; el valor cero informado por Docker significa ausencia de ese límite específico, no consumo nulo ni recursos ilimitados del equipo.

Se mantiene medir antes de escalar. El escenario candidato es la consulta pública de tratamientos por NGINX contra MongoDB, con dataset y entorno declarados, concurrencia creciente, duración y límites de interrupción acordados; deberá medir solicitudes/segundo, errores, percentiles de latencia y recursos por componente. Dataset, niveles de carga, duración y objetivo de servicio requieren definición antes de ejecutarlo. No se autoriza ni ejecuta carga en esta revisión.

Comparar más réplicas de Catálogo frente a aumentar recursos de una instancia son alternativas de escalado, no soluciones justificadas por un cuello de botella observado. También deben considerarse MongoDB y el gateway; no se atribuye el límite a un componente sin medirlo.

El modelo de costos debe separar cómputo, almacenamiento, transferencia y servicios auxiliares. Para estimar un período: sumar recursos contratados por horas de uso, almacenamiento y transferencia según las tarifas del entorno elegido, documentando moneda, fecha y supuestos. No hay proveedor, dimensionamiento ni tarifas confirmados; no se inventan importes ni se equipara ejecución local con costo de despliegue cero.

D13 continúa parcial para la Entrega 2: existe una propuesta de evaluación, pero faltan resultados, límite identificado, comparación de escalado y estimación sustentada. El cronograma exige D13 hoy y explicita la prueba de carga en la defensa; esta revisión deja visible esa brecha documental sin declarar mediciones inexistentes ni forzar una prueba de carga no autorizada. Ver [matriz de cierre](../ENTREGA-2.md).

## Hipótesis y presupuesto inicial de evaluación

Se define el escenario de lectura del catálogo por NGINX, dos réplicas y MongoDB real. Hipótesis inicial, no demanda medida: 5 solicitudes/segundo sostenidas y ráfagas de 20 solicitudes/segundo durante un minuto, mezcla 80 % listado y 20 % detalle, dataset autorizado de hasta 1.000 tratamientos. La muestra actual tiene uno: no se amplía automáticamente ni se usa para extrapolar capacidad.

Objetivos iniciales de evaluación, no un SLO ya alcanzado: p95 de lectura menor que 500 ms y menos de 1 % de errores en condiciones saludables. Registrar p50/p95/p99, throughput, errores por causa, CPU/memoria por componente y operaciones MongoDB. Detener un ensayo futuro si p95 supera 2 segundos o errores superan 5 % durante 30 segundos, o se detecta impacto en otro trabajo. La carga requiere un ensayo acordado; no se ejecuta por definir estos umbrales.

Estimación de recursos para probar exclusivamente el flujo de esta entrega: 2 vCPU, 4 GiB de memoria y 20 GiB de disco persistente como punto de partida de dimensionamiento, no recomendación de producción ni consumo medido. Presupuestar dentro de esa memoria dos procesos Catálogo, gateway, MongoDB y Jaeger; verificar luego con medición que el conjunto cabe. Excluye PostgreSQL, OpenSearch, Memcached y el sistema final: habilitarlos exige un dimensionamiento nuevo. El host local observado no equivale a ese despliegue.

| Escenario hipotético | Evaluación posterior | Decisión condicionada a evidencia |
| :--- | :--- | :--- |
| Base: 5 solicitudes/s | Latencia, errores y recursos con dos réplicas. | Mantener tamaño si cumple objetivos. |
| Crecimiento: 20 solicitudes/s | Comparar dos y cuatro réplicas sobre el mismo dataset. | Escalar horizontalmente solo si Catálogo limita y la fuente soporta la carga. |
| Mayor volumen de catálogo | Aumentar dataset autorizado y revisar consultas/índices y disco. | Optimizar fuente o recursos antes de multiplicar réplicas sin efecto. |

**Estimación económica paramétrica para 30 días:** `C = 720 × tarifa_horaria_del_host(2 vCPU, 4 GiB) + 20 × tarifa_GiB_mes_disco + transferencia_GiB × tarifa_GiB + respaldos + servicios_no_incluidos`. El escenario duplica cómputo solo si se contrata un segundo host; cuatro procesos en el mismo host no duplican automáticamente la factura ni garantizan más capacidad. La transferencia y los respaldos siguen sin estimar por falta de datos.

No se verificó una tarifa, moneda, proveedor ni región; por eso no existe una suma monetaria defendible. La fórmula con recursos constituye una estimación condicionada, no un precio ni una medición inventada. Para completar el análisis económico se debe obtener una cotización fechada, identificar lo incluido, reemplazar las variables y explicitar impuestos o créditos aplicables. No se contrata ni consulta un proveedor externo en este cierre.

**Resultado:** diseño y plan de evaluación completos para revisión; capacidad máxima, cuello de botella, resultados de carga y costo monetario siguen pendientes. No se convierte un conjunto de hipótesis en evidencia del requisito final.

### Referencia económica verificada para el escenario, no contratación

Como estimación ilustrativa posterior al apartado paramétrico, se consultó el 23/10/2026 la [tabla oficial de DigitalOcean Basic Droplets](https://www.digitalocean.com/pricing/droplets): opción Regular con 2 vCPU, 4 GiB de RAM, 80 GiB SSD y 4.000 GiB de transferencia por **USD 24/mes**; opción de crecimiento de 4 vCPU, 8 GiB, 160 GiB SSD y 5.000 GiB de transferencia por **USD 48/mes**. Son precios publicados consultados, no mediciones, selección de proveedor para D9 ni despliegue contratado. La región y disponibilidad no fueron seleccionadas o verificadas.

Para el escenario base, los 20 GiB presupuestados caben en el disco incluido: no se suman otros 20 GiB como si fueran almacenamiento adicional. Un host base cuesta nominalmente USD 24 por mes según esa tabla; dos hosts iguales suman USD 48 antes de extras y no eliminan por sí solos los puntos únicos de falla. Un host mayor también figura en USD 48, pero no se acredita equivalencia de rendimiento entre esas alternativas.

Se excluyen impuestos, dominio, backups, disco adicional, exceso de transferencia, servicios administrados y tiempo de operación; su costo debe agregarse si se utiliza. La tabla es una referencia comercial variable: reconfirmar moneda, región, condiciones y precio antes de publicar. No se convierten USD a ARS sin cotización verificada. Con esto existe una estimación monetaria explícita del escenario mínimo, pero el costo total y la elección sustentada en capacidad medida siguen pendientes.

## Herramienta de medición local (agregada en la rama de migración)

Se agregó `scripts/benchmark-catalogo-local.py` para generar una medición **exploratoria**, limitada al host local y a lecturas GET del catálogo. No se ejecutó una carga ni se infieren resultados de producción.

1. Levantar MELT localmente y comprobar que `http://localhost:8080/api/v1/tratamientos` responda.
2. Ejecutar `python3 scripts/benchmark-catalogo-local.py --requests 40 --concurrency 2 --output catalogo-benchmark.json`.
3. Repetir con niveles acordados de concurrencia, registrar fecha, commit, dataset, CPU/RAM, servicios y resultados; conservar los JSON como evidencia.

El script reporta p50/p95/p99, media, solicitudes por segundo, códigos HTTP y errores. Limita las solicitudes a 200, los trabajadores a 10 y solo acepta HTTP a localhost/127.0.0.1/::1. **No usar contra Render ni terceros**. No mide saturación de bases ni uso de CPU por sí mismo: esas métricas se deben recolectar aparte. Los resultados de este ensayo local no sustituyen la prueba de carga de la defensa ni validan D13 sin ejecución y análisis.

## Resultados medidos — 24/11/2026

Esta sección cierra los pendientes anteriores con una medición real. Las secciones previas se conservan como historia de la decisión.

### Escenario

- **Entorno:** host local de 4 vCPU compartidas y 15,7 GiB, `compose.s1.yaml` con Gateway NGINX, **dos réplicas de Catálogo**, MongoDB, OpenSearch, Memcached y Jaeger. Sin límites de CPU por contenedor. El generador de carga corre en el mismo host, dentro de la red Docker, así que compite por CPU.
- **Herramienta:** `scripts/carga-capacidad.py` (sólo biblioteca estándar; acepta únicamente `gateway`, `localhost` o `127.0.0.1`). Escalones de concurrencia de 10 s cada uno, timeout 5 s.
- **Flujos:** listado público `GET /api/v1/catalogo/tratamientos?page=1&limit=10` (cacheado en Memcached) y búsqueda `?page=2&limit=10&q=masajes&orden=precio_asc` (OpenSearch).
- **Evidencia:** [`docs/evidence/2026-11-24/`](../evidence/2026-11-24/) (`catalogo-*.json` por escalón y `capacidad-docker-stats.csv` con CPU y memoria por contenedor cada segundo).

### Resultados

| Flujo | Concurrencia | RPS | p95 | Errores |
|---|---|---|---|---|
| Listado | 1 | 612 | 2,4 ms | 0 % |
| Listado | **4** | **1.138 (máximo)** | 6,4 ms | 0 % |
| Listado | 16 | 859 | 38 ms | 0 % |
| Listado | 64 | 828 | 289 ms | 0 % |
| Búsqueda | 1 | 684 | 2,2 ms | 0 % |
| Búsqueda | 16 | 888 | 39 ms | 0 % |
| Búsqueda | 64 | 1.159 (máximo) | 133 ms | 0 % |

**Impacto de la caché** (mismo listado, Memcached detenido frente a activo):

| Concurrencia | Sin caché | Con caché | Mejora |
|---|---|---|---|
| 1 | 253 RPS, p95 5,3 ms | 598 RPS, p95 2,5 ms | ×2,4 RPS, −54 % p95 |
| 4 | 536 RPS, p95 13,2 ms | 994 RPS, p95 6,8 ms | ×1,9 RPS, −48 % p95 |
| 16 | 734 RPS, p95 42,7 ms | 1.135 RPS, p95 27,9 ms | ×1,5 RPS, −35 % p95 |
| 64 | 796 RPS, p95 176 ms | 760 RPS, p95 246 ms | sin mejora: el host ya está saturado |

**Uso de recursos en el pico:** cada réplica de Catálogo llegó a 1,4–1,6 vCPU, MongoDB a 1,4 vCPU (con caché, mucho menos), el generador a ~0,9 vCPU, el Gateway a 0,24 vCPU y Memcached a 0,16 vCPU. La memoria nunca fue un problema (Catálogo < 25 MiB por réplica, OpenSearch ~950 MiB).

### Análisis

- **Capacidad:** en este host, el sistema sostiene alrededor de **1.100 lecturas por segundo sin errores**. Pasado ese punto el throughput se aplana y la latencia crece casi linealmente con la concurrencia: es encolamiento, no fallas. Ningún escalón produjo errores ni timeouts.
- **Principal límite:** la **CPU de Catálogo** (serialización JSON y manejo HTTP en Go) compitiendo con MongoDB en el mismo host. El Gateway y Memcached quedan lejos de su límite. Con 4 vCPU repartidas entre Catálogo ×2, MongoDB y el generador, no queda CPU libre por encima de concurrencia 4–16.
- **La caché importa más con poca carga:** duplica el throughput y reduce la latencia a la mitad porque evita el viaje a MongoDB. Cuando la CPU del host está saturada, deja de notarse: el cuello ya no es la base.
- **Contra la hipótesis de demanda** (5 RPS sostenidas, ráfagas de 20 RPS): el margen es de más de 50 veces. El objetivo de p95 < 500 ms se cumple en todos los escalones medidos.
- **Límites de la medición:** host compartido con el generador, dataset de semilla pequeño y una sola ruta por prueba. Los números sirven para comparar y ubicar el cuello, no como capacidad de producción.

### Alternativa de escalado

1. **Primero, escalar Catálogo horizontalmente en hosts separados.** Es *stateless* y ya está detrás del upstream con detección de caída ([ADR-012](ADR-012-balanceo-carga.md)); agregar réplicas no exige cambios de código. Llevar réplicas a otro host sólo tiene sentido si ese host tiene CPU propia; más réplicas en el mismo host no suman capacidad (se comprobó: el límite es la CPU total).
2. **Después, MongoDB dedicado** cuando las lecturas no cacheadas (búsquedas filtradas, detalle) dominen, o subir el TTL de caché para listados.
3. **No** escalar verticalmente el Gateway: usa < 0,25 vCPU al máximo medido.

### Estimación de costos

Con la referencia de precios ya citada (DigitalOcean Basic, consultada el 23/10/2026; precios a reconfirmar):

| Escenario | Recursos | Costo mensual estimado |
|---|---|---|
| Base (demanda de la hipótesis, margen ×50) | 1 host 2 vCPU / 4 GiB con todo el stack de lectura | USD 24 |
| Crecimiento ×10 o alta disponibilidad | 2 hosts 2 vCPU / 4 GiB, una réplica de Catálogo en cada uno | USD 48 |
| Lecturas no cacheadas dominantes | 2 hosts para Catálogo + 1 host 4 vCPU / 8 GiB para MongoDB | USD 96 |

Excluye impuestos, backups, dominio y transferencia por encima de lo incluido. El despliegue académico actual (Render, Supabase y Atlas) no se incluye en esta estimación.
