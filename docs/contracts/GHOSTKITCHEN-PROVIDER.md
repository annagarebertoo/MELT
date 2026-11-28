# Contrato consumido de GhostKitchen

MELT consume la API pública de GhostKitchen desde Beneficios. El contrato de
referencia es el publicado por el Grupo 3: versión **1.1.0**, OpenAPI 3.0.3.

- Especificación oficial: [`GhostKitchen-AS/docs/contracts/v1/openapi.yaml`](https://github.com/Guille270106/GhostKitchen-AS/blob/5946412f3787f53fea840294a34ac679e1b8e062/docs/contracts/v1/openapi.yaml).
- Copia versionada usada por el CI: [`ghostkitchen-v1.1.0.openapi.yaml`](ghostkitchen-v1.1.0.openapi.yaml).
- Commit de origen revisado: `5946412f3787f53fea840294a34ac679e1b8e062` (28/11/2026).
- Prueba de consumidor: [workflow GhostKitchen provider contract](../../.github/workflows/ghostkitchen-contract.yml) y `TestGhostKitchenAgainstPublishedContract`.

El workflow compara la copia con el archivo oficial actual, inicia Prism a partir
del contrato y llama las cinco operaciones desde los endpoints de Beneficios. Si
el Grupo 3 cambia la especificación, el control de sincronización falla y requiere
revisar y actualizar juntos el contrato, el cliente y la prueba.

El test determinista usa el mock Prism derivado del OpenAPI; no llama a la nube ni
necesita la API key real. La caída se prueba por separado con respuestas 503
simuladas. Esto valida compatibilidad con el contrato publicado y el comportamiento
del consumidor; no demuestra disponibilidad del proveedor en producción.

## Efecto de una falla para el cliente

La integración permite consultar cocinas y platos para decidir qué opciones
mostrar. Si GhostKitchen no responde, Beneficios devuelve el error y la interfaz
informa que no pudo cargar la búsqueda y permite volver a intentar. No se confirma
una reserva ni se modifica el saldo de puntos: estas consultas son de lectura.
La disponibilidad del plato es orientativa y no equivale a una reserva o compra.

## Actualizar la copia

Cuando el equipo proveedor publique un cambio, revisar el OpenAPI oficial y su
versión, actualizar la copia junto con el cliente y el test, y comprobar el
workflow antes de integrar. No reemplazarla por JSON de ejemplo escrito por MELT.
