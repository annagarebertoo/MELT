# Supabase para MELT

MELT conserva MongoDB para Catálogo y Profesionales. Beneficios y Reservas usan proyectos PostgreSQL separados, tal como establece `docs/adr/ADR-003-persistencia.md`.

## Crear los esquemas

1. En Supabase, abrí el proyecto **MELT-Beneficios**.
2. Entrá a **SQL Editor → New query**.
3. Copiá y ejecutá el contenido de `beneficios.sql`.
4. Repetí en el proyecto **MELT-Reservas**, usando `reservas.sql`.

Los scripts crean las tablas privadas del servicio y habilitan RLS sin políticas para los roles de navegador. No copies credenciales en el editor SQL ni en estos archivos.

## Preparar las conexiones locales

1. Copiá `.env.example` como `.env` en la raíz del proyecto.
2. En cada proyecto de Supabase, abrí **Connect** y elegí la conexión PostgreSQL **Session pooler** para la aplicación local en Docker.
3. Copiá la URL de cada proyecto en `BENEFICIOS_DATABASE_URL` o `RESERVAS_DATABASE_URL`, respectivamente. No reutilices una URL entre proyectos.
4. Conservá `sslmode=require`. Si la contraseña contiene caracteres reservados de URL, codificalos antes de incluirla.
5. No agregues `.env` a Git ni compartas estas URLs; tienen la contraseña de la base.

## Verificar el esquema

En el SQL Editor de cada proyecto, ejecutá:

```sql
select table_name
from information_schema.tables
where table_schema = 'public'
order by table_name;
```

Beneficios debe mostrar `canjes`, `mensajes_fallidos`, `movimientos_puntos`, `registros_externos`, `reglas_puntos_tratamientos` y `recompensas`.

Reservas debe mostrar `agendas_semanales`, `bloqueos_turnos`, `carritos_bloqueos`, `checkouts`, `control_agendas`, `outbox_eventos` y `turnos`.

Aplicar estos esquemas crea las tablas. Los backends de Beneficios y Reservas leen sus respectivas variables `*_DATABASE_URL`; Reservas verifica al iniciar que existan sus tablas. La conexión real a tus dos proyectos queda verificada cuando ambos servicios arrancan correctamente con tus URLs privadas.
