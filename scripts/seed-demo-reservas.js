// Datos ficticios para probar el catálogo y la agenda en MongoDB local.
// Solo modifica documentos con IDs demo-* en catalogo_db y reservas_db.
const catalog = db.getSiblingDB("catalogo_db");
const reservations = db.getSiblingDB("reservas_db");

const professionals = [
  {
    id: "demo-prof-sofia",
    nombre: "Sofía Demo",
    descripcion: "Profesional ficticia para probar la experiencia de reservas.",
    tratamientosIds: ["demo-limpieza-facial", "demo-masaje-relajante"],
    activo: true,
  },
  {
    id: "demo-prof-valentina",
    nombre: "Valentina Demo",
    descripcion: "Profesional ficticia para probar la experiencia de reservas.",
    tratamientosIds: ["demo-masaje-relajante", "demo-manicura"],
    activo: true,
  },
];

const treatments = [
  {
    id: "demo-limpieza-facial",
    nombre: "Limpieza facial hidratante · Demo",
    descripcion:
      "Tratamiento ficticio para recorrer catálogo, selección profesional y reservas.",
    categoria: "Facial",
    precio: 18000,
    moneda: "ARS",
    duracionMinutos: 60,
    imagenes: [],
    keywords: ["demo", "facial", "limpieza", "hidratación"],
    profesionalesIds: ["demo-prof-sofia"],
  },
  {
    id: "demo-masaje-relajante",
    nombre: "Masaje relajante · Demo",
    descripcion:
      "Servicio ficticio de demostración. Precio y duración solo para pruebas locales.",
    categoria: "Corporal",
    precio: 24000,
    moneda: "ARS",
    duracionMinutos: 60,
    imagenes: [],
    keywords: ["demo", "masaje", "relajación", "corporal"],
    profesionalesIds: ["demo-prof-sofia", "demo-prof-valentina"],
  },
  {
    id: "demo-manicura",
    nombre: "Manicura clásica · Demo",
    descripcion:
      "Servicio ficticio de demostración para explorar el flujo de turnos.",
    categoria: "Manos y uñas",
    precio: 12000,
    moneda: "ARS",
    duracionMinutos: 45,
    imagenes: [],
    keywords: ["demo", "manos", "uñas", "manicura"],
    profesionalesIds: ["demo-prof-valentina"],
  },
];

for (const professional of professionals) {
  catalog.profesionales.updateOne(
    { id: professional.id },
    { $set: professional },
    { upsert: true },
  );
}

for (const treatment of treatments) {
  catalog.tratamientos.updateOne(
    { id: treatment.id },
    { $set: treatment },
    { upsert: true },
  );
}

const weekdays = [
  "Sunday",
  "Monday",
  "Tuesday",
  "Wednesday",
  "Thursday",
  "Friday",
  "Saturday",
];

for (const professional of professionals) {
  for (let day = 0; day < weekdays.length; day += 1) {
    reservations.agendas_semanales.updateOne(
      { profesionalId: professional.id, diaSemana: day },
      {
        $set: {
          _id: `${professional.id}-${weekdays[day]}`,
          profesionalId: professional.id,
          diaSemana: day,
          horaInicio: "09:00",
          horaFin: "18:00",
          activo: true,
        },
      },
      { upsert: true },
    );
  }
}

print(
  JSON.stringify({
    tratamientos: catalog.tratamientos.countDocuments({ id: /^demo-/ }),
    profesionales: catalog.profesionales.countDocuments({ id: /^demo-prof-/ }),
    agendas: reservations.agendas_semanales.countDocuments({
      profesionalId: /^demo-prof-/,
    }),
  }),
);
