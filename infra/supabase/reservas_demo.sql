-- Agendas ficticias para acompañar scripts/seed-demo-reservas.js.
-- Ejecutar únicamente en el proyecto MELT-Reservas después de reservas.sql.
-- Repetirlo actualiza solo las agendas demo identificadas por estos IDs.

insert into agendas_semanales
  (id, profesional_id, dia_semana, hora_inicio, hora_fin, bloques, activo)
values
  ('demo-prof-sofia-Sunday', 'demo-prof-sofia', 0, '09:00', '18:00', '[]'::jsonb, true),
  ('demo-prof-sofia-Monday', 'demo-prof-sofia', 1, '09:00', '18:00', '[]'::jsonb, true),
  ('demo-prof-sofia-Tuesday', 'demo-prof-sofia', 2, '09:00', '18:00', '[]'::jsonb, true),
  ('demo-prof-sofia-Wednesday', 'demo-prof-sofia', 3, '09:00', '18:00', '[]'::jsonb, true),
  ('demo-prof-sofia-Thursday', 'demo-prof-sofia', 4, '09:00', '18:00', '[]'::jsonb, true),
  ('demo-prof-sofia-Friday', 'demo-prof-sofia', 5, '09:00', '18:00', '[]'::jsonb, true),
  ('demo-prof-sofia-Saturday', 'demo-prof-sofia', 6, '09:00', '18:00', '[]'::jsonb, true),
  ('demo-prof-valentina-Sunday', 'demo-prof-valentina', 0, '09:00', '18:00', '[]'::jsonb, true),
  ('demo-prof-valentina-Monday', 'demo-prof-valentina', 1, '09:00', '18:00', '[]'::jsonb, true),
  ('demo-prof-valentina-Tuesday', 'demo-prof-valentina', 2, '09:00', '18:00', '[]'::jsonb, true),
  ('demo-prof-valentina-Wednesday', 'demo-prof-valentina', 3, '09:00', '18:00', '[]'::jsonb, true),
  ('demo-prof-valentina-Thursday', 'demo-prof-valentina', 4, '09:00', '18:00', '[]'::jsonb, true),
  ('demo-prof-valentina-Friday', 'demo-prof-valentina', 5, '09:00', '18:00', '[]'::jsonb, true),
  ('demo-prof-valentina-Saturday', 'demo-prof-valentina', 6, '09:00', '18:00', '[]'::jsonb, true)
on conflict (profesional_id, dia_semana) do update set
  id = excluded.id,
  hora_inicio = excluded.hora_inicio,
  hora_fin = excluded.hora_fin,
  bloques = excluded.bloques,
  activo = excluded.activo;
