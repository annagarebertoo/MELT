-- MELT / Beneficios. Aplicar únicamente en el proyecto Supabase MELT-Beneficios.
-- No se crean FK hacia usuarios, turnos o tratamientos de otros servicios.

create table if not exists registros_externos (
  id text primary key,
  usuario_id text unique,
  email text,
  activo boolean not null default true,
  saldo_puntos bigint not null default 0 check (saldo_puntos >= 0),
  puntos_regalo bigint not null default 0,
  creado_en timestamptz not null default now(),
  datos jsonb not null default '{}'::jsonb
);

create table if not exists movimientos_puntos (
  solicitud_id text primary key,
  usuario_id text,
  puntos bigint not null,
  saldo_puntos bigint not null default 0 check (saldo_puntos >= 0),
  tipo text not null,
  creado_en timestamptz not null default now(),
  email_destino text,
  descripcion text,
  estado text,
  nonce text,
  token_hash text unique,
  vence_en timestamptz
);
create index if not exists ix_movimientos_usuario_fecha on movimientos_puntos (usuario_id, creado_en);

create table if not exists recompensas (
  id text primary key,
  nombre text not null,
  descripcion text not null default '',
  tipo text not null,
  costo_puntos bigint not null check (costo_puntos > 0),
  porcentaje integer,
  tratamiento_id text,
  activa boolean not null default true,
  actualizada_en timestamptz not null default now()
);

create table if not exists reglas_puntos_tratamientos (
  tratamiento_id text primary key,
  puntos bigint not null check (puntos >= 0),
  actualizada_en timestamptz not null default now()
);

create table if not exists canjes (
  id text primary key,
  solicitud_id text not null unique,
  usuario_id text not null,
  recompensa jsonb not null,
  estado text not null,
  reserva_id text,
  precio_original numeric(12,2),
  descuento numeric(12,2),
  precio_final numeric(12,2),
  reserva_expira_en timestamptz,
  creado_en timestamptz not null default now(),
  aplicado_en timestamptz
);
create index if not exists ix_canjes_usuario_estado_fecha on canjes (usuario_id, estado, creado_en desc);

create table if not exists mensajes_fallidos (
  id text primary key,
  evento jsonb not null,
  error text not null,
  intentos integer not null default 0,
  estado text not null,
  creado_en timestamptz not null default now(),
  reintentado_en timestamptz
);
create index if not exists ix_fallidos_estado_fecha on mensajes_fallidos (estado, creado_en desc);

alter table registros_externos enable row level security;
alter table movimientos_puntos enable row level security;
alter table recompensas enable row level security;
alter table reglas_puntos_tratamientos enable row level security;
alter table canjes enable row level security;
alter table mensajes_fallidos enable row level security;

-- Semilla no personal para mostrar un elemento de catálogo de beneficios.
insert into recompensas (id, nombre, descripcion, tipo, costo_puntos, porcentaje, activa)
values ('demo-recompensa-001', 'Beneficio de prueba', 'Recompensa sintética para verificar la conexión.', 'descuento_porcentaje', 100, 10, true)
on conflict (id) do nothing;
