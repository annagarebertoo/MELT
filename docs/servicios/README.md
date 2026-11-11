# Manuales de microservicios

Las rutas de cada manual son relativas al router del servicio, salvo donde se escribe el prefijo Gateway. El health público y común es `GET http://localhost:8080/health`. El Gateway puede añadir autenticación aunque el handler `/health` del servicio sea abierto (por ejemplo, Reservas y Beneficios); `/internal` es sólo para llamadas de servicio y no se debe invocar desde navegador. Gateway y handlers aplican controles propios.

| Servicio | Dueño de los datos | Manual |
|---|---|---|
| Auth | Cuentas, credenciales, roles | [AUTH](AUTH.md) |
| Catálogo y Profesionales | Categorías, tratamientos, perfiles y vinculación | [CATALOGO-PROFESIONALES](CATALOGO-PROFESIONALES.md) |
| Reservas | Agenda, turnos, bloqueos, carritos, checkout y outbox | [RESERVAS](RESERVAS.md) |
| Pagos | Pagos y devoluciones simulados | [PAGOS](PAGOS.md) |
| Beneficios | Puntos, recompensas, canjes, invitaciones y mensajes fallidos | [BENEFICIOS](BENEFICIOS.md) |

Los contratos OpenAPI cubren capacidades seleccionadas, no todas las rutas listadas aquí. Ver [contratos y cobertura](../contracts/README.md).
