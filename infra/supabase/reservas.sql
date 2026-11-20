-- MELT / Reservas. Aplicar únicamente en el proyecto Supabase MELT-Reservas.
-- Esta base es privada: sólo el backend de Reservas debe conectarse con su URL
-- de PostgreSQL. Las referencias a Auth, Catálogo y Beneficios son IDs externos.

create extension if not exists btree_gist;

create table if not exists agendas_semanales (
  id text primary key,
  profesional_id text not null,
  dia_semana smallint not null check (dia_semana between 0 and 6),
  hora_inicio time,
  hora_fin time,
  bloques jsonb not null default '[]'::jsonb,
  activo boolean not null default true,
  check ((hora_inicio is null and hora_fin is null) or (hora_inicio is not null and hora_fin is not null and hora_inicio < hora_fin)),
  unique (profesional_id, dia_semana)
);

create table if not exists turnos (
  id text primary key,
  cliente_id text not null,
  profesional_id text not null,
  tratamiento_id text not null,
  categoria_id text,
  inicio timestamptz not null,
  fin timestamptz not null,
  estado text not null,
  creado_en timestamptz not null default now(),
  precio_tratamiento numeric(12,2) not null default 0 check (precio_tratamiento >= 0),
  sena numeric(12,2) not null default 0 check (sena >= 0),
  pago_id text,
  estado_pago text,
  importe_devolucion numeric(12,2) not null default 0 check (importe_devolucion >= 0),
  estado_devolucion text,
  tratamiento_realizado_en timestamptz,
  tratamiento_realizado_por text,
  no_asistio_en timestamptz,
  no_asistio_por text,
  beneficio_canje_id text,
  beneficio_reserva_id text,
  descuento_beneficio numeric(12,2) not null default 0 check (descuento_beneficio >= 0),
  precio_final numeric(12,2) not null default 0 check (precio_final >= 0),
  check (inicio < fin),
  exclude using gist (profesional_id with =, tstzrange(inicio, fin, '[)') with &&)
    where (estado in ('confirmado', 'pendiente', 'completado'))
);
create index if not exists ix_turnos_cliente_inicio on turnos (cliente_id, inicio desc);
create index if not exists ix_turnos_profesional_inicio on turnos (profesional_id, inicio);

create table if not exists bloqueos_turnos (
  id text primary key,
  profesional_id text not null,
  cliente_id text,
  categoria_id text,
  minuto bigint not null,
  turno_id text not null,
  expira_en timestamptz,
  unique (profesional_id, minuto)
);
create unique index if not exists uq_bloqueo_cliente_categoria_minuto
  on bloqueos_turnos (cliente_id, categoria_id, minuto)
  where cliente_id is not null and categoria_id is not null;
create index if not exists ix_bloqueos_expira_en on bloqueos_turnos (expira_en);

create table if not exists carritos_bloqueos (
  id text primary key,
  cliente_id text not null,
  profesional_id text not null,
  tratamiento_id text not null,
  categoria_id text,
  inicio timestamptz not null,
  fin timestamptz not null,
  precio numeric(12,2) not null check (precio >= 0),
  moneda text not null default 'ARS',
  sena numeric(12,2) not null default 0 check (sena >= 0),
  beneficio_canje_id text,
  descuento_beneficio numeric(12,2) not null default 0 check (descuento_beneficio >= 0),
  precio_final numeric(12,2) not null default 0 check (precio_final >= 0),
  estado text not null,
  expira_en timestamptz,
  turno_id text,
  pago_id text,
  estado_pago text,
  preferencia_id text,
  url_pago text,
  checkout_id text,
  creado_en timestamptz not null default now(),
  check (inicio < fin)
);
create index if not exists ix_carritos_cliente_estado on carritos_bloqueos (cliente_id, estado, inicio);
create index if not exists ix_carritos_expira_en on carritos_bloqueos (expira_en);

create table if not exists checkouts (
  id text primary key,
  cliente_id text not null,
  idempotency_key text not null,
  item_ids jsonb not null default '[]'::jsonb,
  items jsonb not null default '[]'::jsonb,
  subtotal numeric(12,2) not null default 0,
  descuento numeric(12,2) not null default 0,
  total numeric(12,2) not null default 0,
  sena numeric(12,2) not null default 0,
  estado text not null,
  pago_id text,
  turno_ids jsonb not null default '[]'::jsonb,
  resultado jsonb not null default '{}'::jsonb,
  creado_en timestamptz not null default now(),
  actualizado_en timestamptz not null default now(),
  unique (cliente_id, idempotency_key)
);

create table if not exists control_agendas (
  id text primary key,
  profesional_id text not null,
  dia_semana smallint not null check (dia_semana between 0 and 6),
  version bigint not null default 0,
  unique (profesional_id, dia_semana)
);

create table if not exists outbox_eventos (
  id text primary key,
  tipo text not null,
  routing_key text not null,
  payload jsonb not null,
  creado_en timestamptz not null default now(),
  publicado_en timestamptz,
  intentos integer not null default 0,
  ultimo_error text
);
create index if not exists ix_outbox_pendientes on outbox_eventos (creado_en) where publicado_en is null;

alter table agendas_semanales enable row level security;
alter table turnos enable row level security;
alter table bloqueos_turnos enable row level security;
alter table carritos_bloqueos enable row level security;
alter table checkouts enable row level security;
alter table control_agendas enable row level security;
alter table outbox_eventos enable row level security;

-- Semilla no personal. Se puede borrar una vez verificada la conexión.
insert into agendas_semanales (id, profesional_id, dia_semana, hora_inicio, hora_fin, bloques)
values ('demo-agenda-001', 'demo-profesional-001', 1, '09:00', '17:00', '[{"inicio":"13:00","fin":"14:00"}]'::jsonb)
on conflict (id) do nothing;
