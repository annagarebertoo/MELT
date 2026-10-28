# ADR-013 — Capacidad y costos

> **Cierre documental de Entrega 2:** el plan final concreta hipótesis, recursos y modelo económico. D13 sigue parcial en resultados medidos y cotización; no se afirma capacidad demostrada.

**Estado:** Propuesto  
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
