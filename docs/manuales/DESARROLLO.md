# Manual de desarrollo

## Organización

```text
services/{auth,catalogo-profesionales,reservas,pagos,beneficios}/  servicios Go independientes
infra/nginx/                                                       Gateway y frontend estático
infra/{jaeger,prometheus}/                                        configuración de observabilidad
docs/                                                              arquitectura, ADR, contratos, manuales, evidencia
scripts/                                                           smoke checks y utilidades de desarrollo
compose.yaml                                                       entorno integrado local
```

Cada servicio tiene `go.mod`; abrir el módulo apropiado para editar/probar. Mantener propiedad de datos: consultar APIs del servicio dueño, no leer su colección desde otro proceso. El frontend llama al Gateway, no a direcciones internas.

## Cambios y contratos

1. Confirmar rama y estado local antes de empezar.
2. Localizar la ruta real y su middleware en el servicio propietario y en `infra/nginx/default.conf`.
3. Mantener autenticación/roles y verificar además propiedad de recursos dentro del handler.
4. Para cambio de API, actualizar OpenAPI y ejemplos sólo después de comparar con implementación y respuesta real.
5. Para flujo entre servicios, identificar ownership, fallos parciales, idempotencia, eventos y recuperación; registrar una decisión en ADR si cambia una decisión arquitectónica.
6. Agregar pruebas al servicio que posee el comportamiento y separar unitarias de integración con infraestructura externa.
7. Actualizar manual correspondiente y el índice de [docs](../README.md).

## Pruebas

Las pruebas rápidas son `go test ./...` desde el módulo. Los tests de integración pueden modificar bases, crear índices y/o colecciones; usar instancias aisladas mediante variables de test. Catálogo requiere etiquetas para integration/search integration. No ejecutar suites destructivas en el MongoDB de la demo. Revisar `docs/OPERACIONES.md` y encabezados `//go:build` antes de invocar un comando con tags.

Verificaciones estáticas útiles: `node --check infra/nginx/web/app.js`, `docker compose config --quiet`, `git diff --check`. Estos comandos verifican sintaxis/configuración y whitespace; no sustituyen pruebas funcionales.

## Seguridad y privacidad

- Nunca versionar `.env`, tokens, contraseñas, claves de partner/internas o datos personales.
- No escribir secretos en logs, capturas ni archivos de evidencia.
- Auth firma JWT; Gateway verifica sesión en rutas configuradas; cada servicio vuelve a validar sus autorizaciones donde corresponde.
- Las APIs internas usan red privada y clave compartida. No crear rutas Gateway para ellas.
- Los IDs de Auth y Catálogo son dominios distintos. Reservas usa la resolución interna.

## Estilo de trabajo y documentación

Usar ramas de feature y Pull Request conforme a la práctica del equipo/cátedra. No inferir el estado remoto desde Compose. Una evidencia debe incluir fecha, commit, comando/endpoint, resultado y límites, sin secretos. Mantener ADR anteriores como historial; agregar estado/enmienda en vez de borrar decisiones superadas.
