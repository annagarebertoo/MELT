# Estado de verificación de MELT — 20/11/2026 (Argentina)

Este documento distingue **código implementado**, **pruebas automatizadas**, **despliegue en Render** y **pruebas funcionales pendientes**. No sustituye la matriz de requisitos ni la evidencia histórica. Fecha de los registros de Render/Actions: 21/11/2026 UTC.

## Versión y resultados comprobados

- Rama: `codex/supabase-postgres-migration`. Commit revisado: `6e1195b28faff34fc89f53fad2aa21979524c4a1`.
- GitHub Actions: ejecuciones [44852939413](https://github.com/annagarebertoo/MELT/actions/runs/44852939413) y [44852933181](https://github.com/annagarebertoo/MELT/actions/runs/44852933181) completadas con conclusión **success**. Incluyen comprobaciones de contratos OpenAPI y frontend, tests Go y jobs de integración según los workflows. El resultado de CI no prueba el comportamiento de un navegador autenticado contra Render.
- Se corrigió la sintaxis OpenAPI 3.0 del importe positivo en Pagos (`4c129b77`) y una descripción YAML con coma en Reservas (`6e1195b2`).
- El Gateway de prueba de Render `melt-gateway-prueba` se desplegó manualmente desde `6e1195b2` el 21/11/2026 UTC: deploy `dep-dc0fn7d9el5s73dvvqv0`, estado **live**. Los logs de arranque muestran configuración NGINX completada y una solicitud HEAD con HTTP 200. URL: https://melt-gateway-prueba.onrender.com/.
- En el momento de la consulta, los servicios `melt-reservas` y `melt-pagos` informaban deploy **live** del mismo commit. Los servicios `melt-auth-prueba`, `melt-beneficios-prueba` y `MELT-CATALOGO` mostraban deploys **live** de commits anteriores. **Live** indica estado del deploy, no prueba todas las funciones de negocio.
- El Gateway Render de prueba tiene una instancia; la configuración de dos réplicas de Catálogo está en Docker Compose local. No afirmar failover de dos réplicas en Render.

## Asistencia: comportamiento implementado

El backend `services/reservas/cmd/api/inasistencia.go` implementa `POST /turnos/:id/no-asistio` para el profesional asignado. Comprueba que el turno esté confirmado y haya finalizado, guarda `estado=no_asistio` en una transacción y acepta reintentos idempotentes. La ruta de inasistencia **no emite el evento de tratamiento realizado** que acredita puntos. El frontend incorpora el botón «Marcar no asistió», confirmación y actualización de estado.

La workflow [Asistencia en PostgreSQL](https://github.com/annagarebertoo/MELT/actions) tiene pruebas de persistencia/eventos y de interfaz Playwright con API simulada; la ejecución histórica `44846085679` concluyó **success** sobre el commit `ad7b2f56`. Esto **no es** una verificación funcional del botón en el Gateway Render actualmente desplegado.

### Prueba funcional pendiente, sin tocar datos reales

Cuando haya acceso a una sesión de prueba de profesional (por ejemplo Lolo Rossi), usar **únicamente un turno de prueba confirmado cuyo horario ya haya terminado**:

1. Registrar el saldo de puntos del cliente de prueba y el estado inicial del turno.
2. Desde la agenda del profesional asignado, pulsar «Marcar no asistió» y aceptar la confirmación.
3. Verificar respuesta exitosa, estado `no_asistio` y persistencia después de recargar la página.
4. Verificar que **no aumentó el saldo** y que no se produjo una acreditación por `tratamiento.realizado` para ese turno.
5. Repetir la solicitud solo en el entorno de prueba para confirmar idempotencia; comprobar que tampoco crea puntos.
6. Si el botón falla, conservar hora, ID del turno de prueba, código HTTP, `request_id` y logs de Reservas/Beneficios **sin incluir tokens, contraseñas ni datos personales**.

No se ejecutó esta prueba desde esta conversación: no se accedió a una sesión autenticada de navegador ni se modificaron turnos o saldos.

## Otros requisitos aún sin cierre verificable

| Área | Evidencia disponible | Pendiente |
| --- | --- | --- |
| Failover y balanceo | Configuración de reintentos pasivos en `infra/nginx/default.conf`; ensayo histórico en `docs/POSTMORTEM.md` con 1 respuesta 504 de 12 | Repetir ensayo controlado con dos réplicas locales y adjuntar nueva evidencia |
| Observabilidad | Logs JSON y trazas locales de Gateway/Catálogo; `docs/OBSERVABILITY.md` | Métricas de negocio, paneles, alertas y prueba del recorrido completo |
| Capacidad | Script local `scripts/benchmark-catalogo-local.py` y ADR-013 | Ejecutar mediciones, registrar entorno, p95/errores y cuellos de botella |
| GhostKitchen | Adaptador Beneficios y pruebas con mocks; `docs/GHOSTKITCHEN.md` | Validar proveedor real con credenciales autorizadas; no hay flujo de pedido/pago GhostKitchen |
| Asistencia y puntos en Render | Código, pruebas automatizadas e implementación de la ruta | Verificación autenticada de extremo a extremo descrita arriba |

## Límites operativos

- No desplegar nuevamente ni cambiar bases de datos solo para completar esta documentación.
- No marcar el proyecto como 100 % verificado mientras falten las pruebas de la tabla.
- Los cambios de documentación posteriores a `6e1195b2` pueden generar nuevos deploys automáticos en algunos servicios de Render; verificar su estado antes de cualquier demostración.
