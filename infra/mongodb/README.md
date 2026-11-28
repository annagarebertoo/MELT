# MongoDB Atlas compartido para MELT

El proyecto MELT usa un solo clúster Atlas compartido para Auth, Catálogo, Profesionales y Pagos. Cada servicio conserva una base lógica propia: `usuarios_db`, `catalogo_db`, `profesionales_db` y `pagos_db`. Reservas y Beneficios usan exclusivamente PostgreSQL en Supabase.

## Preparar una computadora del equipo

1. En el `.env` local, establecer `MONGODB_URI` con la cadena del clúster Atlas. La contraseña no debe quedar en Git. Si contiene caracteres especiales, URL-encodearlos.
2. Mantener las bases lógicas `AUTH_MONGODB_DATABASE=usuarios_db`, `CATALOGO_MONGODB_DATABASE=catalogo_db`, `PROFESIONALES_MONGODB_DATABASE=profesionales_db` y `PAGOS_MONGODB_DATABASE=pagos_db`.
3. En MongoDB Atlas, los usuarios del equipo deben poder conectarse desde sus redes. La lista actual contiene `0.0.0.0/0`, que permite intentos desde cualquier IPv4; restringirla requiere considerar los consumidores conectados, como Render.
4. Las tres integrantes configuran la misma URI Atlas en sus entornos; así leen y escriben en el mismo clúster. Reservas y Beneficios conservan sus URLs privadas de Supabase.

La cadena del clúster se obtiene en Atlas: **MELT → Connect → Drivers → Go**. El host actual termina en `melt.e7rud0p.mongodb.net`. Usar usuario y contraseña de base de datos; la cuenta de inicio de sesión de Atlas no sirve como credencial de aplicación.

El usuario de aplicación debe tener permisos de lectura y escritura en las cuatro bases lógicas. No compartir su contraseña en Git ni en archivos versionados; cada integrante la configura en su `.env` ignorado.
