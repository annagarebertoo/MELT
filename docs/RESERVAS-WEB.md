# Identidad, reservas e interfaz web

Esta guía documenta el recorrido local de identidad y reservas, los datos demo disponibles y cómo preparar datos reales. Los tratamientos, nombres, precios y agendas demo no representan precios ni personal real de MELT.

## Datos demo ya cargados en el entorno local

La base MongoDB local asociada a `docker compose` tiene estos registros:

| Tratamiento ficticio | Precio de prueba | Duración | Profesionales demo |
| :--- | ---: | ---: | :--- |
| Limpieza facial hidratante · Demo | ARS 18.000 | 60 min | Sofía Demo |
| Masaje relajante · Demo | ARS 24.000 | 60 min | Sofía Demo y Valentina Demo |
| Manicura clásica · Demo | ARS 12.000 | 45 min | Valentina Demo |

Ambas agendas demo están habilitadas todos los días de 09:00 a 18:00. Sus IDs son `demo-prof-sofia` y `demo-prof-valentina`; los tratamientos tienen IDs con prefijo `demo-`. Son perfiles públicos de Catálogo asociados a agendas de Reservas, **no son cuentas de Auth ni personas reales**. Sirven para explorar las fichas, elegir profesional y consultar disponibilidad con una cuenta cliente.

Los datos fueron cargados en el volumen local de MongoDB; cada integrante debe cargarlos en su propio entorno. El script versionado solo escribe los IDs `demo-*` en las colecciones de demostración y se puede volver a ejecutar sin duplicarlos:

```powershell
Get-Content scripts/seed-demo-reservas.js -Raw | docker compose exec -T mongodb mongosh --quiet
```

`docker compose down` conserva el volumen; `docker compose down -v` lo elimina junto con los demás datos locales. Para crear una reserva, registrá/iniciá sesión como cliente desde la interfaz. Para altas administrativas por API se necesita una cuenta administrativa de Auth configurada.

## Preparar una agenda reservable

El usuario administrador debe estar creado al iniciar Auth (`AUTH_ADMIN_EMAIL` y `AUTH_ADMIN_PASSWORD`). No guardes credenciales ni tokens en el repositorio.

1. Inicia sesión como administrador y guarda el token en una variable temporal:

   ```powershell
   $base = 'http://localhost:8080/api/v1'
   $admin = Invoke-RestMethod "$base/auth/login" -Method Post -ContentType 'application/json' -Body (@{email=$env:AUTH_ADMIN_EMAIL; password=$env:AUTH_ADMIN_PASSWORD} | ConvertTo-Json)
   $headers = @{ Authorization = "Bearer $($admin.token)" }
   ```

2. Crea una cuenta interna para quien atenderá el tratamiento. Conservá el ID Auth para vincular la cuenta; el perfil público tendrá su propio ID:

   ```powershell
   $professional = Invoke-RestMethod "$base/auth/usuarios" -Method Post -Headers $headers -ContentType 'application/json' -Body (@{
     nombre = 'Profesional de demostración'
     email = 'profesional-demo@example.test'
     password = 'cambiar-esta-clave'
     roles = @('profesional')
   } | ConvertTo-Json)
   $professionalID = $professional.usuario.id
   ```

   Usa un email nuevo y una contraseña local propia. Auth no devuelve contraseñas en las respuestas. Si el email ya existe, consulta su identidad con su propia sesión profesional o elige otro email de demostración.

3. Crea primero un tratamiento sin profesionales y luego el perfil público asociado a ese tratamiento. El CRUD sincroniza automáticamente ambos extremos de la asociación. Este paso aplica a perfiles gestionados por API; los perfiles demo descritos arriba ya existen en la base local:

   ```powershell
   $treatmentID = 'demo-facial-60'
   $treatment = @{
     id = $treatmentID
     nombre = 'Limpieza facial de demostración'
     descripcion = 'Tratamiento de prueba para verificar el flujo de reservas.'
     categoria = 'Facial'
     precio = 10000
     moneda = 'ARS'
     duracionMinutos = 60
     imagenes = @()
     keywords = @('demostración', 'facial')
     profesionalesIds = @()
   }
   Invoke-RestMethod "$base/catalogo/tratamientos" -Method Post -Headers $headers -ContentType 'application/json' -Body ($treatment | ConvertTo-Json -Depth 5)

   $profileID = 'profesional-demo-01'
   $profile = @{
     id = $profileID
     nombre = $professional.usuario.nombre
     descripcion = 'Perfil local de demostración.'
     tratamientosIds = @($treatmentID)
     activo = $true
   }
   Invoke-RestMethod "$base/catalogo/profesionales" -Method Post -Headers $headers -ContentType 'application/json' -Body ($profile | ConvertTo-Json -Depth 5)
   Invoke-RestMethod "$base/catalogo/admin/profesionales/$profileID/cuenta" -Method Put -Headers $headers -ContentType 'application/json' -Body (@{authUserId=$professionalID} | ConvertTo-Json)
   ```

4. Activa una agenda semanal para ese ID. Este ejemplo habilita todos los días de 09:00 a 17:00:

   ```powershell
   0..6 | ForEach-Object {
     Invoke-RestMethod "$base/reservas/agenda" -Method Put -Headers $headers -ContentType 'application/json' -Body (@{
     profesionalId = $profileID
       diaSemana = $_
       horaInicio = '09:00'
       horaFin = '17:00'
       activo = $true
     } | ConvertTo-Json)
   }
   ```

La relación debe quedar completa en ambos sentidos: `tratamiento.profesionalesIds`, `profesional.tratamientosIds` y la agenda de Reservas usan el ID del perfil de Catálogo; Auth mantiene su ID separado en `profesional.authUserId`. La interfaz solo ofrece perfiles activos que coincidan con el tratamiento.

## Recorrido de cliente

1. Abre `http://localhost:8080`, crea una cuenta cliente e inicia sesión.
2. Abre una ficha demo, por ejemplo **Limpieza facial hidratante · Demo**, elige a **Sofía Demo** y una fecha futura.
3. Consultá horarios y agregá uno al carrito. La retención dura 10 minutos y todavía no inicia un pago.
4. Agregá otros tratamientos si querés. **Mi carrito** muestra subtotal, descuentos, total, seña total del 30 % y vencimiento de cada retención. Pulsá **Confirmar compra** para ejecutar una única simulación y confirmar todos los turnos en conjunto.
5. En **Mis reservas**, cancela un turno. Con 24 horas o más de anticipación se registra la devolución simulada total; con menos de 24 horas no hay devolución. La cancelación libera la agenda.

## Agenda y turnos profesionales

Una cuenta Auth con rol `profesional` debe estar vinculada a un perfil activo de Catálogo. Reservas resuelve internamente esa relación; el profesional no puede elegir un `profesionalId` distinto desde el formulario.

- `GET /api/v1/reservas/agenda`: consulta los siete días de la propia agenda. Cada día devuelve `bloques: [{inicio, fin}]`; los registros históricos con `horaInicio` y `horaFin` siguen leyéndose como un bloque.
- `PUT /api/v1/reservas/agenda`: guarda un día con `{diaSemana, bloques}` (domingo=0, lunes=1 … sábado=6). Se permiten hasta ocho bloques ordenados y no superpuestos, dentro de 08:00–20:00. También conserva el formato histórico `{diaSemana, horaInicio, horaFin, activo}`.
- `GET /api/v1/reservas/turnos`: un profesional recibe únicamente los turnos de su perfil vinculado e incluye el nombre del cliente consultado internamente en Auth.
- `DELETE /api/v1/reservas/turnos/{id}`: cancela un turno propio y aplica la política actual de seña/devolución; el turno y sus bloqueos se actualizan en una transacción.
- `POST /api/v1/reservas/turnos/{id}/completar`: conserva el outbox idempotente `tratamiento.realizado` que consume Beneficios.
- `POST /api/v1/reservas/turnos/{id}/no-asistio`: registra `no_asistio` cuando finalizó el horario. No publica el evento de tratamiento realizado ni acredita puntos; el mismo profesional puede reintentar sin duplicar cambios.

Las ediciones de agenda se rechazan si desplazarían una reserva confirmada o una retención de checkout. Las nuevas retenciones comparten una fila de control por perfil/día con las ediciones, y los índices únicos de minutos conservan la exclusión ante solicitudes simultáneas. También se bloquea el solapamiento de la misma categoría para un mismo cliente usando el `categoriaId` estable devuelto por Catálogo.

## Carrito de reservas

Reservas conserva sin migración destructiva la colección `carritos_bloqueos`; el carrito único se obtiene de las retenciones activas de la identidad autenticada. La colección `checkouts` registra una operación por cliente y clave de idempotencia, con instantánea de importes y todos los turnos relacionados.

- `GET /api/v1/reservas/carrito`: lista únicamente las retenciones propias vigentes y calcula subtotal, descuentos, total, seña y restante.
- `POST /api/v1/reservas/carrito/bloqueos` con `diferirPago: true`: crea una retención por 10 minutos y no llama a Pagos. Omitir el campo conserva la integración antigua que inicia el pago inmediatamente.
- `DELETE /api/v1/reservas/carrito/bloqueos/{id}`: quita el elemento, libera los minutos de manera transaccional y restituye el beneficio asociado.
- `POST /api/v1/reservas/carrito/checkout` con `idempotencyKey`: recalcula precios vigentes y beneficios en backend, calcula una sola seña total del 30 %, registra una operación simulada y confirma todos los turnos dentro de una única transacción.

Los elementos pueden corresponder a diferentes categorías, profesionales, fechas y horarios. Una falla de validación impide confirmar cualquier turno del checkout; si la simulación ya fue aprobada, Pagos la revierte. Una respuesta de rechazo conserva las retenciones vigentes para reintentar o quitar elementos. Repetir la clave de idempotencia recupera el mismo resultado. Los endpoints antiguos de pago por elemento se conservan como compatibilidad, pero la interfaz no los utiliza.

## Simulador académico de pagos

No se usa pasarela externa, preferencia ni webhook: el servicio Pagos registra una transacción simulada en su propia base MongoDB `pagos_db`. El resultado predeterminado es aprobado para que la demo funcione sin credenciales; configurar `PAYMENT_SIMULATOR_RESULT=rejected` solo en el backend permite probar rechazos. El frontend no acepta el resultado del cliente. No se efectúan cobros reales.

Si Reservas no puede completar una compra aprobada, el checkout transaccional evita confirmaciones parciales y solicita a Pagos revertir la operación simulada. El estado de recuperación queda persistido si esa compensación no está disponible.

## Pruebas reproducibles

Desde la raíz del repositorio:

```powershell
Push-Location services/auth; go test ./...; Pop-Location
Push-Location services/catalogo-profesionales; go test ./...; Pop-Location
Push-Location services/reservas; go test ./...; Pop-Location
Push-Location services/pagos; go test ./...; Pop-Location
node --check infra/nginx/web/app.js
docker compose config --quiet
```

La integración con MongoDB de Reservas puede ejecutarse con una base de pruebas aislada mediante `RESERVAS_TEST_MONGODB_URI`; el test crea una base con nombre único y la elimina al terminar. No apuntes esa variable a una base con datos de equipo o producción.
