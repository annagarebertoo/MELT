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

2. Crea una cuenta interna para quien atenderá el tratamiento. El ID devuelto debe usarse en catálogo y agenda:

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

3. Crea un tratamiento de prueba asociado al ID, y un perfil público con la misma asociación. Este paso aplica a perfiles gestionados por API; los perfiles demo descritos arriba ya existen en la base local:

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
     profesionalesIds = @($professionalID)
   }
   Invoke-RestMethod "$base/catalogo/tratamientos" -Method Post -Headers $headers -ContentType 'application/json' -Body ($treatment | ConvertTo-Json -Depth 5)

   $profile = @{
     id = $professionalID
     nombre = $professional.usuario.nombre
     descripcion = 'Perfil local de demostración.'
     tratamientosIds = @($treatmentID)
     activo = $true
   }
   Invoke-RestMethod "$base/catalogo/profesionales" -Method Post -Headers $headers -ContentType 'application/json' -Body ($profile | ConvertTo-Json -Depth 5)
   ```

4. Activa una agenda semanal para ese ID. Este ejemplo habilita todos los días de 09:00 a 17:00:

   ```powershell
   0..6 | ForEach-Object {
     Invoke-RestMethod "$base/reservas/agenda" -Method Put -Headers $headers -ContentType 'application/json' -Body (@{
       profesionalId = $professionalID
       diaSemana = $_
       horaInicio = '09:00'
       horaFin = '17:00'
       activo = $true
     } | ConvertTo-Json)
   }
   ```

La relación debe quedar completa en ambos sentidos: `tratamiento.profesionalesIds`, `profesional.tratamientosIds` y la agenda de Reservas deben compartir el mismo ID. La interfaz solo ofrece perfiles activos que coincidan con el tratamiento.

## Recorrido de cliente

1. Abre `http://localhost:8080`, crea una cuenta cliente e inicia sesión.
2. Abre una ficha demo, por ejemplo **Limpieza facial hidratante · Demo**, elige a **Sofía Demo** y una fecha futura.
3. Consulta horarios y elige uno. El backend genera el carrito de 10 minutos y la preferencia de pago por el 30 % del importe.
4. Paga desde **Pagar seña en Mercado Pago**. El adaptador crea una preferencia en modo de prueba por el 30 % del precio. Al volver a MELT, ingresá el ID de pago si no configuraste `MP_RETURN_URL`; Reservas confirma únicamente si Mercado Pago devuelve estado aprobado, ARS, importe exacto y la referencia del carrito.
5. En **Mis reservas**, cancela el turno. Con 24 horas o más de anticipación se solicita la devolución total; con menos de 24 horas no hay devolución. La cancelación libera la agenda.

## Mercado Pago de prueba

Compose fuerza `MP_ENV=test`. Para crear enlaces de pago y verificar pagos de prueba, configura `MP_ACCESS_TOKEN` de prueba en el entorno local antes de iniciar los servicios. `MP_RETURN_URL` es opcional; si se usa, debe ser una URL HTTPS pública. Si no existe una credencial de prueba válida, la interfaz puede seguir mostrando el catálogo y la disponibilidad, pero no completar el checkout. No uses tokens de producción en la demostración ni los agregues a `.env.example` o al control de versiones. No se hicieron cobros reales.

Si el proveedor no está configurado o falla al crear la preferencia, Reservas libera el bloqueo y responde con `PAGO_NO_DISPONIBLE`; no deja una franja ocupada sin forma de pago. Las pruebas automatizadas usan un proveedor HTTP simulado y no requieren credenciales de Mercado Pago.

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
