# Servicio Auth

## Responsabilidad y datos

Auth es dueño de las cuentas y sus credenciales, estados, roles y JWT. Usa MongoDB (`AUTH_MONGODB_DATABASE`, predeterminada `usuarios_db`) y colecciones de usuarios/control de roles/invitaciones. Ningún otro servicio consulta sus colecciones directamente: las validaciones se hacen por API interna.

## Rutas

Base pública: `/api/v1/auth`. Los métodos/handlers se registran en `services/auth/cmd/api/main.go`.

| Método y ruta | Acceso | Uso |
|---|---|---|
| `GET /health` | Público | Verifica conectividad a MongoDB. |
| `POST /registro` | Público | Crea una cuenta cliente; puede aceptar token de invitación de puntos. |
| `POST /login` | Público | Emite JWT para credenciales válidas. |
| `GET /me` | Bearer | Identidad y roles de la sesión. |
| `GET /verify` | Bearer, reservado al Gateway | Valida JWT y devuelve identidad en headers internos. Gateway deniega el acceso público a esta ruta directa. |
| `GET /verify-admin` | Administrador o superadministrador | Verificación usada por Catálogo. |
| `POST /usuarios` | Administrador o superadministrador | Crea cuentas internas con roles admitidos; no eleva a administrador común. |
| `GET /usuarios` | Solo superadministrador | Lista usuarios sin credenciales/hash. |
| `PUT /usuarios/{id}/rol` | Solo superadministrador | Cambia rol con protección contra quitarse el propio rol y contra dejar cero superadministradores. |
| `GET /usuarios/profesionales` | Administrador o superadministrador | Lista cuentas profesionales candidatas a vincular. |
| `GET /internal/usuarios/{id}` | Interno con clave compartida | Valida cuenta al vincular desde Catálogo. |
| `POST /internal/usuarios/buscar` | Interno con clave | Búsqueda para integraciones server-to-server. |
| `POST /internal/usuarios/verificar-email` | Interno con clave | Validación de email/identidad para Beneficios. |

La configuración del Gateway no publica `/internal/` ni la validación directa `/verify` al navegador. Las rutas administrativas requieren Bearer además de la autorización por rol.

## Roles y permisos

Roles reconocidos: `cliente`, `profesional`, `recepcion`, `administrador`, `superadministrador`. El registro público no permite elegir rol. Recepción conserva las capacidades habilitadas en Reservas, pero no se presenta como un panel distinto en el frontend. Los permisos por operación de Reservas están documentados en [RESERVAS](RESERVAS.md). Auth no otorga automáticamente al perfil público de catálogo el rol autenticado.

## Integraciones, variables y errores

- `MONGODB_URI`, `AUTH_MONGODB_DATABASE`, `AUTH_JWT_SECRET` y `MELT_INTERNAL_KEY` conectan persistencia, firma y llamadas internas.
- `AUTH_TOKEN_TTL_MINUTES` define duración del token (Compose: 15 por defecto).
- `AUTH_ADMIN_EMAIL` y `AUTH_ADMIN_PASSWORD` permiten bootstrap de administrador inicial según `ensureAdmin`; usarlos sólo en local/operación controlada y nunca poner valores reales en la documentación.
- `BENEFICIOS_URL` y `CATALOGO_URL` apuntan a servicios internos para flujos de invitación y vinculación.
- `GET /health` devuelve 503 si MongoDB no responde. En el resto de rutas se devuelven errores HTTP de validación/autorización; nunca debe exponerse el hash de contraseña.
- Auth no consume eventos de dominio de RabbitMQ. Para validaciones de cuenta/invitación se comunica de manera síncrona por HTTP con Beneficios o Catálogo según el flujo; no accede directamente a sus bases.
- Casos usuales: 400 para datos inválidos, 401 para sesión/credenciales inválidas, 403 para rol insuficiente, 404 para cuenta inexistente, 409 para estado/duplicado incompatible, 503 para dependencia no disponible. El cuerpo exacto puede variar entre login, invitaciones y administración; OpenAPI es la fuente de esquema cuando define la operación.

## Ejecutar pruebas

```sh
cd services/auth
go test ./...
```

Las pruebas de integración pueden requerir `AUTH_TEST_MONGODB_URI`; usar una base desechable separada. No ejecutar contra datos compartidos, ya que las pruebas de integración escriben registros.
