const API = "/api/v1";
const TOKEN_KEY = "melt.session";
const INVITATION_KEY = "melt.pending-invitation";
let invitation = null;
const state = { page: 1, pageSize: 8, hasNext: false, search: "", category: "", priceMax: "", sort: "nombre", order: "asc", bookingTreatment: null, detailTreatment: null, cartTimer: null, selectedSlot: null, cartItems: [], cartSummary: null, checkoutKey: null, cancelling: null, mode: "login", user: null, rewardRequests: new Map(), rewards: [], canjes: [], categories: [], professionals: [] };
const catalogAdminState = { categories: [], treatments: [], professionals: [], adminProfessionals: [], professionalAccounts: [] };

const $ = (selector) => document.querySelector(selector);
const token = () => sessionStorage.getItem(TOKEN_KEY);
const formatPrice = (amount) => new Intl.NumberFormat("es-AR", { style: "currency", currency: "ARS", maximumFractionDigits: 0 }).format(Number(amount || 0));
const escapeHTML = (value) => String(value ?? "").replace(/[&<>"']/g, (char) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[char]);
const treatmentLabel = (item) => Number(item?.duracionMinutos) >= 5 ? `${item.nombre} · ${Number(item.duracionMinutos)} min` : `${item.nombre} · agenda en actualización`;
const canBook = (item) => Number(item?.duracionMinutos) >= 5 && Array.isArray(item?.profesionalesIds) && item.profesionalesIds.length > 0 && (window.professionalsByTreatment?.[item.id]?.length || 0) > 0;
function imageURL(value) {
  if (typeof value !== "string" || !value.trim()) return "";
  try {
    const url = new URL(value.trim(), window.location.origin);
    return ["http:", "https:"].includes(url.protocol) ? url.href : "";
  } catch { return ""; }
}

function professionalImage(person) {
  return imageURL(person?.fotografia || person?.foto || person?.imagen || person?.avatar);
}

function loadImageMarkup(value, alt, className) {
  const src = imageURL(value);
  return src ? `<img class="${className}" src="${escapeHTML(src)}" alt="${escapeHTML(alt)}" loading="lazy">` : "";
}

async function request(path, options = {}) {
  const headers = new Headers(options.headers || {});
  if (options.body && !(options.body instanceof FormData)) headers.set("Content-Type", "application/json");
  if (token()) headers.set("Authorization", `Bearer ${token()}`);
  const response = await fetch(`${API}${path}`, { ...options, headers });
  if (response.status === 204) return null;
  const contentType = response.headers.get("content-type") || "";
  const payload = contentType.includes("application/json") ? await response.json() : await response.text();
  if (!response.ok) {
    const message = payload?.error?.message || payload?.error || payload?.message || `La solicitud falló (${response.status}).`;
    throw new Error(typeof message === "string" ? message : "No pudimos completar la solicitud.");
  }
  return payload;
}

function showToast(message, isError = false) {
  const toast = $("#toast");
  toast.textContent = message;
  toast.classList.toggle("error", isError);
  toast.classList.add("visible");
  window.clearTimeout(showToast.timer);
  showToast.timer = window.setTimeout(() => toast.classList.remove("visible"), 4200);
}

function setStatus(target, message, isError = false) {
  const element = $(target);
  element.textContent = message;
  element.classList.toggle("error-text", isError);
}

function updateSteps(step) {
  document.querySelectorAll("[data-booking-step]").forEach((item) => {
    const current = Number(item.dataset.bookingStep);
    item.classList.toggle("active", current === step);
    item.classList.toggle("complete", current < step);
    if (current === step) item.setAttribute("aria-current", "step"); else item.removeAttribute("aria-current");
  });
}

function updateAuthChrome() {
  const signedIn = Boolean(token());
  $("#booking-guest-note").classList.toggle("hidden", signedIn);
  $("#header-login").textContent = signedIn ? "Mi cuenta" : "Ingresar";
  $("#header-register").classList.toggle("hidden", signedIn);
  $("#availability-form").classList.toggle("auth-required", !signedIn);
}

function professionalLabel(id, index) {
  const reference = String(id);
  return `Profesional ${index + 1} · ref. ${reference.slice(-5)}`;
}

function updateBookingSummary(treatment) {
  const summary = $("#booking-summary");
  if (!treatment) {
    summary.classList.add("hidden");
    summary.innerHTML = "";
    return;
  }
  summary.classList.remove("hidden");
  summary.innerHTML = `<span class="summary-mark">M</span><div><small>Tu elección</small><strong>${escapeHTML(treatment.nombre)}</strong></div><span>${formatPrice(treatment.precio)} · ${Number(treatment.duracionMinutos) >= 5 ? `${Number(treatment.duracionMinutos)} min` : "duración a confirmar"}</span>`;
}

function updateProfessionals() {
  const id = $("#booking-treatment").value;
  const treatment = window.treatments?.find((item) => item.id === id) || (state.bookingTreatment?.id === id ? state.bookingTreatment : null);
  state.bookingTreatment = treatment;
  updateBookingSummary(treatment);
  const ids = Array.isArray(treatment?.profesionalesIds) ? treatment.profesionalesIds : [];
  const select = $("#booking-professional");
  const profiles = window.professionalsByTreatment?.[id] || [];
  select.innerHTML = `<option value="">${profiles.length ? "Elegí un profesional" : "Sin profesionales publicados"}</option>${profiles.map((person) => `<option value="${escapeHTML(person.id)}">${escapeHTML(person.nombre)}</option>`).join("")}`;
  const submit = $("#availability-form").querySelector("button[type=submit]");
  const valid = canBook(treatment);
  submit.disabled = !valid || !token();
  if (!treatment) setStatus("#availability-status", "Elegí un tratamiento para consultar la agenda.");
  else if (!Number(treatment.duracionMinutos) || Number(treatment.duracionMinutos) < 5) setStatus("#availability-status", "Este tratamiento todavía no tiene una duración válida. La agenda estará disponible cuando el catálogo se actualice.");
  else if (!ids.length) setStatus("#availability-status", "Este tratamiento todavía no tiene profesionales asignados. Consultá en recepción mientras se actualiza la agenda.");
  else if (!profiles.length) setStatus("#availability-status", "Falta publicar el perfil del profesional asociado. La reserva quedará disponible cuando el catálogo se complete.");
  else setStatus("#availability-status", "Seleccioná profesional y fecha para consultar horarios.");
  updateSteps(1);
}

async function loadProfessionalProfiles(treatments) {
  window.professionalsByTreatment = {};
  await Promise.all(treatments.map(async (item) => {
    if (!Array.isArray(item.profesionalesIds) || !item.profesionalesIds.length) return;
    try {
      const result = await request(`/profesionales?tratamientoId=${encodeURIComponent(item.id)}`);
      const assigned = new Set(item.profesionalesIds);
      window.professionalsByTreatment[item.id] = (result.profesionales || []).filter((person) => assigned.has(person.id));
    } catch { window.professionalsByTreatment[item.id] = []; }
  }));
}

async function loadCategories() {
  const select = $("#search-category");
  const status = $("#category-status");
  select.disabled = true;
  if (status) status.textContent = "Cargando categorías…";
  try {
    const result = await request("/catalogo/categorias");
    state.categories = Array.isArray(result?.categorias) ? result.categorias : [];
    const selected = state.category || select.value;
    select.innerHTML = `<option value="">Todas las categorías</option>${state.categories.map((item) => `<option value="${escapeHTML(item.nombre)}">${escapeHTML(item.nombre)}</option>`).join("")}`;
    if (state.categories.some((item) => item.nombre === selected)) select.value = selected;
    select.disabled = false;
    if (status) status.textContent = state.categories.length ? "" : "Todavía no hay categorías disponibles.";
    renderCategoryNavigation();
  } catch (error) {
    state.categories = [];
    select.innerHTML = `<option value="">Todas las categorías</option>`;
    select.disabled = false;
    if (status) status.textContent = `No pudimos cargar las categorías: ${error.message}`;
    renderCategoryNavigation();
  }
}

function renderCategoryNavigation() {
  const nav = $("#category-navigation");
  if (!nav) return;
  nav.innerHTML = `<button type="button" class="category-chip" data-category="" aria-pressed="${!state.category}">Todos</button>${state.categories.map((category) => `<button type="button" class="category-chip" data-category="${escapeHTML(category.nombre)}" aria-pressed="${category.nombre === state.category}">${escapeHTML(category.nombre)}</button>`).join("")}`;
  nav.querySelectorAll("[data-category]").forEach((button) => button.addEventListener("click", () => {
    state.category = button.dataset.category;
    $("#search-category").value = state.category;
    syncCategoryNavigation();
    state.page = 1;
    loadTreatments();
    $("#tratamientos").scrollIntoView({ behavior: "smooth" });
  }));
}

function syncCategoryNavigation() {
  $("#category-navigation").querySelectorAll("[data-category]").forEach((button) => {
    button.setAttribute("aria-pressed", String(button.dataset.category === state.category));
  });
}

function renderProfessionals() {
  const list = $("#professionals-list");
  $("#professionals-count").textContent = `${state.professionals.length} ${state.professionals.length === 1 ? "profesional para acompañarte" : "profesionales para acompañarte"}`;
  list.innerHTML = state.professionals.map((person) => {
    const photo = professionalImage(person);
    const name = person.nombre || "Profesional";
    const specialties = Array.isArray(person.especialidades) ? person.especialidades.join(" · ") : person.especialidad;
    return `<article class="professional-card"><button class="professional-card-link" type="button" data-professional-id="${escapeHTML(person.id)}" aria-label="Ver perfil de ${escapeHTML(name)}"><span class="professional-portrait">${photo ? loadImageMarkup(photo, name, "professional-photo") : `<span aria-hidden="true">${escapeHTML(name.trim().slice(0, 1).toLocaleUpperCase("es-AR"))}</span>`}</span><span class="professional-card-copy"><span class="eyebrow">${escapeHTML(specialties || "Profesional MELT")}</span><strong>${escapeHTML(name)}</strong><span class="professional-card-description">${escapeHTML(person.descripcion || "")}</span><span class="professional-card-more">Conocer perfil ↗</span></span></button></article>`;
  }).join("") || `<p class="empty-state">Todavía no hay profesionales publicados.</p>`;
  list.querySelectorAll("[data-professional-id]").forEach((button) => button.addEventListener("click", () => showProfessionalProfile(button.dataset.professionalId)));
}

async function loadProfessionals() {
  setStatus("#professionals-status", "Cargando profesionales…");
  try {
    const result = await request("/profesionales");
    state.professionals = Array.isArray(result?.profesionales) ? result.profesionales : [];
    renderProfessionals();
    setStatus("#professionals-status", state.professionals.length ? `${state.professionals.length} profesionales` : "No hay profesionales publicados por el momento.");
  } catch (error) {
    state.professionals = [];
    $("#professionals-count").textContent = "Profesionales para acompañarte";
    $("#professionals-list").innerHTML = `<p class="empty-state">No pudimos cargar los profesionales. Intentá nuevamente.</p>`;
    setStatus("#professionals-status", `Error al cargar profesionales: ${error.message}`, true);
  }
}

async function showProfessionalProfile(id, updateHash = true) {
  const profile = $("#professional-profile");
  const listView = $("#professionals-list-view");
  const content = $("#professional-profile-content");
  listView.classList.add("hidden");
  profile.classList.remove("hidden");
  content.innerHTML = `<p class="status-line" role="status">Cargando perfil…</p>`;
  if (updateHash) {
    const nextHash = `#profesionales/${encodeURIComponent(id)}`;
    if (window.location.hash !== nextHash) history.pushState(null, "", nextHash);
  }
  try {
    const person = await request(`/catalogo/profesionales/${encodeURIComponent(id)}`);
    const photo = professionalImage(person);
    const name = person.nombre || "Profesional";
    const specialties = Array.isArray(person.especialidades) ? person.especialidades.join(" · ") : person.especialidad;
    const treatmentIDs = Array.isArray(person.tratamientosIds) ? person.tratamientosIds : [];
    const treatments = await Promise.all(treatmentIDs.map(async (treatmentID) => {
      try { return await request(`/tratamientos/${encodeURIComponent(treatmentID)}`); } catch { return null; }
    }));
    const availableTreatments = treatments.filter(Boolean);
    const treatmentLinks = availableTreatments.map((item) => `<button type="button" class="professional-treatment-link" data-treatment-id="${escapeHTML(item.id)}"><span>${escapeHTML(item.nombre)}</span><span>${formatPrice(item.precio)} ↗</span></button>`).join("");
    const treatmentNotice = treatments.some((item) => !item)
      ? `<p class="muted">No pudimos cargar todos los tratamientos asociados.</p>`
      : treatmentIDs.length ? "" : `<p class="muted">No hay tratamientos publicados para este perfil.</p>`;
    content.innerHTML = `<div class="professional-profile-header"><div class="professional-profile-portrait">${photo ? loadImageMarkup(photo, name, "professional-photo") : `<span aria-hidden="true">${escapeHTML(name.trim().slice(0, 1).toLocaleUpperCase("es-AR"))}</span>`}</div><div><p class="eyebrow">${escapeHTML(specialties || "Profesional MELT")}</p><h2 id="professional-profile-name">${escapeHTML(name)}</h2><p class="professional-bio">${escapeHTML(person.descripcion || "")}</p></div></div><section class="professional-treatments" aria-labelledby="professional-treatments-title"><p class="eyebrow">Su experiencia</p><h3 id="professional-treatments-title">Tratamientos</h3><div class="professional-treatment-list">${treatmentLinks}${treatmentNotice}</div></section>`;
    content.querySelectorAll("[data-treatment-id]").forEach((button) => button.addEventListener("click", () => showTreatmentDetail(button.dataset.treatmentId)));
    $("#profesionales").scrollIntoView({ behavior: "smooth" });
  } catch (error) {
    content.innerHTML = `<p class="empty-state">No pudimos cargar este perfil: ${escapeHTML(error.message)}</p>`;
  }
}

function showProfessionalList(updateHash = true) {
  $("#professional-profile").classList.add("hidden");
  $("#professionals-list-view").classList.remove("hidden");
  if (updateHash && window.location.hash.startsWith("#profesionales/")) window.location.hash = "#profesionales";
}

function syncProfessionalHash() {
  if (!window.location.hash.startsWith("#profesionales/")) {
    showProfessionalList(false);
    return;
  }
  const id = decodeURIComponent(window.location.hash.slice("#profesionales/".length));
  if (id) showProfessionalProfile(id, false);
}

function detailTreatment(item) {
  const treatmentImage = Array.isArray(item.imagenes) ? item.imagenes.find((image) => imageURL(image)) : "";
  $("#detail-visual").innerHTML = treatmentImage ? loadImageMarkup(treatmentImage, item.nombre || "Tratamiento", "treatment-detail-photo") : `<span id="detail-initial">${escapeHTML((item.categoria || item.nombre || "M").slice(0, 1).toLocaleUpperCase("es-AR"))}</span>`;
  $("#detail-category").textContent = item.categoria || "Tratamiento MELT";
  $("#detail-title").textContent = item.nombre || "Tratamiento";
  $("#detail-description").textContent = item.descripcion || "Un momento para vos, con atención personalizada.";
  $("#detail-duration").textContent = Number(item.duracionMinutos) >= 5 ? `${Number(item.duracionMinutos)} min` : "A confirmar";
  $("#detail-price").textContent = formatPrice(item.precio);
  const keywords = Array.isArray(item.keywords) ? item.keywords : [];
  $("#detail-keywords").innerHTML = keywords.length ? keywords.map((word) => `<span>${escapeHTML(word)}</span>`).join("") : "";
  const people = window.professionalsByTreatment?.[item.id] || [];
  $("#detail-professionals").innerHTML = people.length ? `<span>Atiende${people.length === 1 ? "" : "n"}:</span>${people.map((person) => `<button class="detail-professional-link" type="button" data-professional-id="${escapeHTML(person.id)}">${escapeHTML(person.nombre)}</button>`).join("")}` : "Profesionales: agenda en actualización";
  $("#detail-professionals").querySelectorAll("[data-professional-id]").forEach((button) => button.addEventListener("click", () => {
    $("#treatment-dialog").close();
    showProfessionalProfile(button.dataset.professionalId);
  }));
  const available = canBook(item);
  $("#detail-availability").textContent = available ? "Consultá disponibilidad en tiempo real y elegí el horario que te quede mejor." : "La reserva online para este tratamiento estará disponible cuando el catálogo tenga duración y profesionales asignados.";
  const button = $("#detail-book");
  button.disabled = !available;
  button.textContent = available ? "Elegir horario ↗" : "Agenda en actualización";
  button.dataset.id = item.id;
  state.detailTreatment = item;
  $("#treatment-dialog").showModal();
}

async function showTreatmentDetail(id) {
  try {
    const item = await request(`/tratamientos/${encodeURIComponent(id)}`);
    if (!window.professionalsByTreatment) window.professionalsByTreatment = {};
    const assignedIDs = Array.isArray(item.profesionalesIds) ? item.profesionalesIds : [];
    if (!assignedIDs.length) {
      window.professionalsByTreatment[item.id] = [];
    } else if (!Array.isArray(window.professionalsByTreatment[item.id]) || !window.professionalsByTreatment[item.id].length) {
      try {
        const result = await request(`/profesionales?tratamientoId=${encodeURIComponent(item.id)}`);
        const assigned = new Set(assignedIDs);
        window.professionalsByTreatment[item.id] = (result.profesionales || []).filter((person) => assigned.has(person.id));
      } catch { window.professionalsByTreatment[item.id] = []; }
    }
    detailTreatment(item);
  } catch (error) { showToast(error.message, true); }
}

function treatmentCard(item) {
  const duration = Number(item.duracionMinutos) >= 5 ? `${Number(item.duracionMinutos)} min` : "Duración a confirmar";
  const bookable = canBook(item);
  const image = Array.isArray(item.imagenes) ? item.imagenes.find((value) => imageURL(value)) : "";
  return `<article class="treatment-card ${bookable ? "" : "unavailable"}">
    <button class="treatment-visual detail-treatment" type="button" data-id="${escapeHTML(item.id)}" aria-label="Ver detalles de ${escapeHTML(item.nombre)}">${image ? loadImageMarkup(image, item.nombre || "Tratamiento", "treatment-card-photo") : `<span>${escapeHTML((item.categoria || "M").slice(0, 1))}</span>`}</button>
    <div class="treatment-copy"><p class="eyebrow">${escapeHTML(item.categoria || "MELT")}</p><h3>${escapeHTML(item.nombre)}</h3>
      <p>${escapeHTML(item.descripcion || "Un momento de cuidado pensado para vos.")}</p><div class="treatment-meta"><span>${duration}</span><strong>${formatPrice(item.precio)}</strong></div>
      ${bookable ? `<button class="text-link detail-treatment" type="button" data-id="${escapeHTML(item.id)}">Ver detalles y horarios <span aria-hidden="true">↗</span></button>` : `<p class="catalog-unavailable">Agenda en actualización</p><button class="text-link detail-treatment" type="button" data-id="${escapeHTML(item.id)}">Ver detalles <span aria-hidden="true">↗</span></button>`}
    </div></article>`;
}

async function loadTreatments() {
  const params = new URLSearchParams({ page: String(state.page), limit: String(state.pageSize), sort: state.sort, order: state.order });
  if (state.search) params.set("q", state.search);
  if (state.category) params.set("categoria", state.category);
  if (state.priceMax) params.set("precioMax", state.priceMax);
  setStatus("#catalog-status", "Cargando tratamientos…");
  try {
    const result = await request(`/tratamientos?${params}`);
    const treatments = result.tratamientos || [];
    await loadProfessionalProfiles(treatments);
    window.treatments = treatments;
    const total = Number(result.pagination?.total ?? treatments.length);
    state.hasNext = state.page * state.pageSize < total;
    $("#previous-page").disabled = state.page <= 1;
    $("#next-page").disabled = !state.hasNext;
    $("#page-label").textContent = `Página ${state.page}`;
    setStatus("#catalog-status", `${total} ${total === 1 ? "tratamiento en el catálogo" : "tratamientos en el catálogo"}${treatments.some((item) => !canBook(item)) ? " · algunas agendas están en actualización" : ""}`);
    const featured = treatments.slice(0, 4);
    $("#featured-list").innerHTML = featured.map((item, index) => `
      <article class="featured-card ${canBook(item) ? "" : "unavailable"}">
        <button class="featured-image featured-image-${index + 1} detail-treatment" type="button" data-id="${escapeHTML(item.id)}" aria-label="Ver detalles de ${escapeHTML(item.nombre)}">${Array.isArray(item.imagenes) && item.imagenes.some((image) => imageURL(image)) ? loadImageMarkup(item.imagenes.find((image) => imageURL(image)), item.nombre || "Tratamiento", "treatment-card-photo") : `<span>${escapeHTML((item.categoria || "M").slice(0, 1))}</span>`}</button>
        <div class="featured-info"><div><h3>${escapeHTML(item.nombre)}</h3><p>${formatPrice(item.precio)} <span>· ${Number(item.duracionMinutos) >= 5 ? `${Number(item.duracionMinutos)} min` : "duración a confirmar"}</span></p></div><button class="featured-link detail-treatment" type="button" data-id="${escapeHTML(item.id)}">Ver detalles ↗</button></div>
      </article>`).join("") || `<p class="featured-loading">La selección se actualizará cuando haya tratamientos disponibles.</p>`;
    $("#treatments-list").innerHTML = treatments.map(treatmentCard).join("") || `<p class="empty-state">No encontramos tratamientos con esos filtros. Probá con otra búsqueda.</p>`;
    document.querySelectorAll(".detail-treatment").forEach((button) => button.addEventListener("click", () => showTreatmentDetail(button.dataset.id)));
    const select = $("#booking-treatment");
    const previous = select.value;
    select.innerHTML = `<option value="">Elegí un tratamiento</option>${treatments.map((item) => `<option value="${escapeHTML(item.id)}">${escapeHTML(treatmentLabel(item))}</option>`).join("")}`;
    if (previous && treatments.some((item) => item.id === previous)) select.value = previous;
    else if (state.bookingTreatment && treatments.some((item) => item.id === state.bookingTreatment.id)) select.value = state.bookingTreatment.id;
    updateProfessionals();
  } catch (error) {
    setStatus("#catalog-status", `No pudimos cargar el catálogo: ${error.message}`, true);
    $("#featured-list").innerHTML = `<p class="empty-state">No pudimos actualizar la selección. Intentá nuevamente en unos minutos.</p>`;
    $("#treatments-list").innerHTML = `<p class="empty-state">Revisá que los servicios estén disponibles e intentá de nuevo.</p>`;
  }
}

function selectTreatment(item) {
  state.bookingTreatment = item;
  $("#booking-treatment").value = item.id;
  $("#booking-professional").value = "";
  updateProfessionals();
  document.querySelector("#reservas").scrollIntoView({ behavior: "smooth" });
}

function invitationMessage(message) {
  $("#invitation-panel").classList.remove("hidden");
  $("#invitation-details").textContent = message;
}

function clearInvitation() {
  invitation = null;
  sessionStorage.removeItem(INVITATION_KEY);
  $("#auth-email").readOnly = false;
  $("#invitation-panel").classList.add("hidden");
}

async function initializeInvitation() {
  const params = new URLSearchParams(window.location.hash.slice(1));
  const incoming = params.get("invitacion");
  if (incoming) {
    history.replaceState(null, "", window.location.pathname + window.location.search);
    sessionStorage.setItem(INVITATION_KEY, incoming);
  }
  const pendingToken = sessionStorage.getItem(INVITATION_KEY);
  if (!pendingToken) return;
  try {
    const info = await request("/beneficios/invitaciones/consultar", { method: "POST", body: JSON.stringify({ token: pendingToken }) });
    invitation = { ...info, token: pendingToken };
    if (!token()) {
      setAuthMode(info.estado === "acreditada" ? "login" : "register");
      $("#auth-email").value = info.email;
      $("#auth-email").readOnly = true;
    }
    invitationMessage(info.estado === "acreditada" ? "Este enlace ya fue utilizado. Ingresá con tu cuenta para consultar el saldo." : `Tenés ${info.puntos} puntos pendientes para ${info.email}. Creá tu cuenta o ingresá si ya tenés una; los puntos se vincularán automáticamente.`);
    $("#auth-panel").scrollIntoView({ behavior: "smooth" });
  } catch (error) {
    // Un fallo temporal no descarta el enlace: se puede volver a abrir o recargar.
    invitationMessage(error.message);
  }
}

async function confirmInvitation() {
  if (!invitation || !token()) return;
  try {
    const result = await request("/beneficios/invitaciones/confirmar", { method: "POST", body: JSON.stringify({ token: invitation.token }) });
    invitation = null;
    sessionStorage.removeItem(INVITATION_KEY);
    $("#auth-email").readOnly = false;
    invitationMessage(`Acreditación confirmada: ${result.puntos} puntos. Tu saldo después de esa operación es ${result.saldoPuntos}.`);
    $("#discard-invitation").textContent = "Cerrar aviso";
  } catch (error) {
    invitationMessage(error.message + " Podés volver a intentar actualizando tu cuenta; si acabás de registrarte, MELT también recupera la acreditación automáticamente.");
  }
}

function setAuthMode(mode) {
  state.mode = mode;
  const register = mode === "register";
  $("#register-tab").classList.toggle("active", register);
  $("#login-tab").classList.toggle("active", !register);
  $("#register-tab").setAttribute("aria-selected", String(register));
  $("#login-tab").setAttribute("aria-selected", String(!register));
  $("#name-field").classList.toggle("hidden", !register);
  $("#auth-name").required = register;
  $("#auth-submit").textContent = register ? "Crear cuenta" : "Ingresar";
  $("#auth-password").autocomplete = register ? "new-password" : "current-password";
  setStatus("#auth-status", "");
}

async function submitAuth(event) {
  event.preventDefault();
  const name = $("#auth-name").value.trim();
  const email = $("#auth-email").value.trim();
  const password = $("#auth-password").value;
  const path = state.mode === "register" ? "/auth/registro" : "/auth/login";
  const body = state.mode === "register" ? { nombre: name, email, password, ...(invitation ? { invitacionToken: invitation.token } : {}) } : { email, password };
  $("#auth-submit").disabled = true;
  setStatus("#auth-status", state.mode === "register" ? "Creando tu cuenta…" : "Ingresando…");
  try {
    const result = await request(path, { method: "POST", body: JSON.stringify(body) });
    if (state.mode === "register" && invitation) {
      const session = await request("/auth/login", { method: "POST", body: JSON.stringify({ email, password }) });
      sessionStorage.setItem(TOKEN_KEY, session.token);
      $("#auth-form").reset();
      updateAuthChrome();
      await loadAccount();
      showToast(result.acreditacion?.estado === "pendiente_reintento" ? "Tu cuenta está lista. Si la acreditación sigue pendiente, MELT la reintentará automáticamente." : "Tu cuenta está lista y los puntos están vinculados.");
      return;
    }
    if (state.mode === "register") {
      setStatus("#auth-status", "Tu cuenta está lista. Ingresá con tu email y contraseña.");
      $("#auth-password").value = "";
      setAuthMode("login");
      $("#auth-email").value = email;
      setStatus("#auth-status", "Tu cuenta está lista. Ingresá con tu email y contraseña.");
      return;
    }
    sessionStorage.setItem(TOKEN_KEY, result.token);
    $("#auth-form").reset();
    updateAuthChrome();
    await loadAccount();
    showToast("Sesión iniciada. Ya podés consultar tu agenda.");
  } catch (error) { setStatus("#auth-status", error.message, true); }
  finally { $("#auth-submit").disabled = false; }
}

async function loadAccount() {
  updateAuthChrome();
  if (!token()) return showSignedOut();
  try {
    const result = await request("/auth/me");
    const user = result.usuario;
    state.user = user;
    $("#auth-panel").classList.remove("signed-out");
    $("#profile-panel").classList.remove("hidden");
    $("#profile-name").textContent = `Hola, ${user.nombre || ""}`;
    $("#profile-email").textContent = user.email || "";
    $("#profile-initials").textContent = (user.nombre || "M").trim().slice(0, 1).toLocaleUpperCase("es-AR");
    $("#my-bookings").classList.remove("hidden");
    const roles = Array.isArray(user.roles) ? user.roles : [];
    const visibility = accountSectionVisibility(roles);
    $("#points-panel").classList.toggle("hidden", !visibility.client);
    $("#points-balance").closest(".points-box").classList.toggle("hidden", !visibility.client);
    $("#ghostkitchen-panel").classList.toggle("hidden", !visibility.client);
    $("#profile-ghostkitchen").classList.toggle("hidden", !visibility.client);
    $("#professional-panel").classList.toggle("hidden", !visibility.professional);
    $("#admin-panel").classList.toggle("hidden", !visibility.admin);
    $("#superadmin-users-panel").classList.toggle("hidden", !visibility.superadmin);
    $("#catalog-admin-link").classList.toggle("hidden", !visibility.admin);
    $("#cart-menu-link").classList.toggle("hidden", !visibility.client);
    $("#carrito").classList.toggle("hidden", !visibility.client);
    $("#profile-reservations").classList.toggle("hidden", !visibility.bookings);
    $("#availability-form").closest(".calendar-card").classList.toggle("hidden", !visibility.bookings);
    $("#my-bookings").classList.toggle("hidden", !visibility.bookings);
    $("#auth-form").closest(".account-card").classList.add("hidden");
    await confirmInvitation();
    await Promise.all([...(visibility.bookings ? [loadBookings()] : []), ...(visibility.client ? [loadPoints(), loadBenefits(), loadCart()] : [])]);
    if (visibility.professional) await Promise.all([loadProfessionalAgenda(), loadProfessionalTurns()]);
    if (visibility.admin) await Promise.all([loadAdminBenefits(), loadCatalogAdmin()]);
    if (visibility.superadmin) await loadSuperadminUsers();
  } catch {
    sessionStorage.removeItem(TOKEN_KEY);
    updateAuthChrome();
    showSignedOut();
  }
}

function accountSectionVisibility(userRoles) {
  const roles = Array.isArray(userRoles) ? userRoles : [];
  const superadmin = roles.includes("superadministrador");
  const admin = !superadmin && roles.includes("administrador");
  const professional = !superadmin && !admin && roles.includes("profesional");
  const client = !superadmin && !admin && !professional && roles.includes("cliente");
  const receptionOnly = !superadmin && !admin && !professional && !client && roles.includes("recepcion");
  return { superadmin, admin, professional, client, bookings: client || receptionOnly };
}

function showSignedOut() {
  state.user = null;
  $("#catalog-admin-link").classList.add("hidden");
  $("#cart-menu-link").classList.add("hidden");
  $("#carrito").classList.add("hidden");
  $("#cart-count").textContent = "0";
  window.clearInterval(state.cartTimer);
  $("#points-panel").classList.add("hidden");
  $("#ghostkitchen-panel").classList.add("hidden");
  $("#profile-ghostkitchen").classList.add("hidden");
  $("#professional-panel").classList.add("hidden");
  $("#admin-panel").classList.add("hidden");
  $("#superadmin-users-panel").classList.add("hidden");
  $("#profile-panel").classList.add("hidden");
  $("#my-bookings").classList.add("hidden");
  $("#availability-form").closest(".calendar-card").classList.remove("hidden");
  $("#auth-form").closest(".account-card").classList.remove("hidden");
  $("#points-balance").textContent = "—";
  updateAuthChrome();
}

const authRoleLabels = { cliente: "Cliente", profesional: "Profesional", recepcion: "Recepción", administrador: "Administrador", superadministrador: "Superadministrador" };
function isSuperadmin() { return Boolean(token() && state.user?.roles?.includes("superadministrador")); }

async function loadSuperadminUsers() {
  if (!isSuperadmin()) return;
  setStatus("#superadmin-users-status", "Cargando cuentas…");
  try {
    const result = await request("/auth/usuarios");
    const users = Array.isArray(result?.usuarios) ? result.usuarios : [];
    $("#superadmin-users-list").innerHTML = users.map((user) => {
      const roleText = (user.roles || []).map((role) => authRoleLabels[role] || role).join(", ") || "Sin rol";
      const options = Object.entries(authRoleLabels).map(([role, label]) => `<option value="${role}">${label}</option>`).join("");
      return `<article class="catalog-admin-row superadmin-user-row"><span><strong>${escapeHTML(user.nombre)} · ${escapeHTML(user.email)}</strong><small>Rol actual: ${escapeHTML(roleText)} · ${user.activo ? "Activa" : "Inactiva"}</small></span><div class="superadmin-user-controls"><select class="form-field" data-user-role="${escapeHTML(user.id)}" aria-label="Nuevo rol para ${escapeHTML(user.email)}"><option value="">Cambiar rol…</option>${options}</select><button class="button button-outline" type="button" data-save-user-role="${escapeHTML(user.id)}">Guardar rol</button></div></article>`;
    }).join("") || `<p class="empty-state">No hay cuentas registradas.</p>`;
    setStatus("#superadmin-users-status", `${users.length} cuentas cargadas.`);
  } catch (error) { setStatus("#superadmin-users-status", error.message, true); }
}

async function createManagedUser(event) {
  event.preventDefault();
  if (!isSuperadmin()) return setStatus("#superadmin-create-status", "Solo un superadministrador puede crear cuentas.", true);
  const form = event.currentTarget;
  const data = new FormData(form);
  const rol = String(data.get("rol") || "");
  if (!window.confirm(`¿Crear una cuenta nueva con rol ${authRoleLabels[rol]}?`)) return;
  try {
    await request("/auth/usuarios", { method: "POST", body: JSON.stringify({ nombre: String(data.get("nombre") || "").trim(), email: String(data.get("email") || "").trim(), password: String(data.get("password") || ""), roles: [rol] }) });
    form.reset();
    setStatus("#superadmin-create-status", "Cuenta creada correctamente. La contraseña no se volverá a mostrar.");
    await loadSuperadminUsers();
  } catch (error) { setStatus("#superadmin-create-status", error.message, true); }
}

async function saveManagedUserRole(event) {
  const button = event.target.closest("[data-save-user-role]");
  if (!button) return;
  if (!isSuperadmin()) return setStatus("#superadmin-users-status", "Solo un superadministrador puede cambiar roles.", true);
  const id = button.dataset.saveUserRole;
  const select = $(`[data-user-role="${CSS.escape(id)}"]`);
  const rol = String(select?.value || "");
  if (!rol) return setStatus("#superadmin-users-status", "Elegí el nuevo rol.", true);
  if (!window.confirm(`¿Cambiar el rol de esta cuenta a ${authRoleLabels[rol]}?`)) return;
  button.disabled = true;
  try {
    await request(`/auth/usuarios/${encodeURIComponent(id)}/rol`, { method: "PUT", body: JSON.stringify({ rol }) });
    setStatus("#superadmin-users-status", "Rol actualizado correctamente.");
    await loadSuperadminUsers();
  } catch (error) { setStatus("#superadmin-users-status", error.message, true); button.disabled = false; }
}

async function loadPoints() {
  try {
    const result = await request("/beneficios/mis-puntos");
    $("#points-balance").textContent = new Intl.NumberFormat("es-AR").format(result.saldoPuntos || 0);
    const movements = result.movimientos || [];
    $("#points-history").innerHTML = movements.length ? movements.map((movement) => {
      const amount = Number(movement.puntos || 0);
      const date = movement.creadoEn ? new Intl.DateTimeFormat("es-AR", { dateStyle: "medium", timeStyle: "short" }).format(new Date(movement.creadoEn)) : "Movimiento histórico";
      const label = movement.descripcion || movement.tipo || "Movimiento de puntos";
      return `<article class="movement-row"><div><strong>${escapeHTML(label)}</strong><small>${escapeHTML(date)}</small></div><b class="${amount < 0 ? "points-negative" : "points-positive"}">${amount > 0 ? "+" : ""}${new Intl.NumberFormat("es-AR").format(amount)} pts</b></article>`;
    }).join("") : `<p class="empty-state">Todavía no hay movimientos de puntos.</p>`;
  } catch { $("#points-balance").textContent = "—"; }
}

async function loadBenefits() {
  try {
    const [rewards, redemptions] = await Promise.all([request("/beneficios/recompensas"), request("/beneficios/mis-canjes")]);
    state.rewards = rewards.recompensas || [];
    state.canjes = redemptions.canjes || [];
    $("#rewards-list").innerHTML = state.rewards.length ? state.rewards.map((reward) => {
      const value = reward.tipo === "descuento_porcentaje" ? `${Number(reward.porcentaje)}% de descuento` : "Tratamiento sin cargo";
      return `<article class="reward-row"><div><strong>${escapeHTML(reward.nombre)}</strong><span>${escapeHTML(reward.descripcion || value)}</span><small>${new Intl.NumberFormat("es-AR").format(reward.costoPuntos)} puntos · ${escapeHTML(value)}</small></div><button class="button button-outline redeem-reward" type="button" data-id="${escapeHTML(reward.id)}">Canjear</button></article>`;
    }).join("") : `<p class="empty-state">Pronto vas a encontrar recompensas para canjear.</p>`;
    $("#rewards-list").querySelectorAll(".redeem-reward").forEach((button) => button.addEventListener("click", () => redeemReward(button.dataset.id, button)));
    $("#my-rewards").innerHTML = state.canjes.length ? state.canjes.map((canje) => `<article class="movement-row"><div><strong>${escapeHTML(canje.recompensa?.nombre || "Recompensa")}</strong><small>${escapeHTML(canje.estado)}${canje.reservaExpiraEn ? " · reservada para una compra" : ""}</small></div><span class="reward-state">${escapeHTML(canje.estado)}</span></article>`).join("") : `<p class="empty-state">Todavía no hiciste canjes.</p>`;
    const available = state.canjes.filter((canje) => canje.estado === "disponible");
    $("#booking-benefit").innerHTML = `<option value="">Sin beneficio</option>${available.map((canje) => `<option value="${escapeHTML(canje.id)}">${escapeHTML(canje.recompensa?.nombre || "Recompensa")}</option>`).join("")}`;
  } catch (error) {
    $("#rewards-list").innerHTML = `<p class="form-status error-text">No pudimos cargar las recompensas: ${escapeHTML(error.message)}</p>`;
  }
}

function createRequestId() {
  return globalThis.crypto?.randomUUID ? globalThis.crypto.randomUUID() : `canje-${Date.now()}-${Math.random().toString(16).slice(2)}`;
}

async function redeemReward(rewardId, button) {
  if (!token()) return showToast("Ingresá a tu cuenta para canjear una recompensa.", true);
  let requestId = state.rewardRequests.get(rewardId);
  if (!requestId) {
    requestId = createRequestId();
    state.rewardRequests.set(rewardId, requestId);
  }
  button.disabled = true;
  try {
    await request("/beneficios/canjes", { method: "POST", body: JSON.stringify({ solicitudId: requestId, recompensaId: rewardId }) });
    state.rewardRequests.delete(rewardId);
    showToast("Canje confirmado. Ya podés usarlo al reservar.");
    await Promise.all([loadPoints(), loadBenefits()]);
  } catch (error) {
    showToast(error.message, true);
  } finally { button.disabled = false; }
}

async function loadBookings() {
  const list = $("#bookings-list");
  list.innerHTML = `<p class="muted">Cargando tus reservas…</p>`;
  try {
    const result = await request("/reservas/turnos");
    const bookings = result.turnos || [];
    const treatmentIds = [...new Set(bookings.map((booking) => booking.tratamientoId))];
    const names = new Map();
    const professionalNames = new Map();
    await Promise.all(treatmentIds.map(async (id) => {
      try {
        const item = await request(`/tratamientos/${encodeURIComponent(id)}`);
        names.set(id, item.nombre);
        const people = await request(`/profesionales?tratamientoId=${encodeURIComponent(id)}`);
        for (const person of people.profesionales || []) professionalNames.set(person.id, person.nombre);
      } catch {}
    }));
    list.innerHTML = bookings.map((booking) => {
      const date = new Intl.DateTimeFormat("es-AR", { dateStyle: "medium", timeStyle: "short" }).format(new Date(booking.inicio));
      const status = booking.estado === "confirmado" ? "Confirmado" : booking.estado === "completado" ? "Tratamiento realizado" : booking.estado;
      const amount = booking.sena != null ? `<small>Seña: ${formatPrice(booking.sena)}${booking.descuentoBeneficio ? ` · beneficio: −${formatPrice(booking.descuentoBeneficio)}` : ""}</small>` : "";
      const professional = professionalNames.get(booking.profesionalId) || professionalLabel(booking.profesionalId, 0);
      const actions = `${booking.estado === "confirmado" && state.user?.roles?.includes("profesional") ? `<button class="button button-outline complete-treatment" type="button" data-id="${escapeHTML(booking.id)}">Confirmar realizado</button>` : ""}${booking.estado === "confirmado" && state.user?.roles?.includes("cliente") ? `<button class="text-link cancel-booking" type="button" data-id="${escapeHTML(booking.id)}" data-start="${escapeHTML(booking.inicio)}">Cancelar turno</button>` : ""}`;
      return `<article class="booking-row"><div><strong>${escapeHTML(names.get(booking.tratamientoId) || "Tratamiento")}</strong><span>${escapeHTML(date)} · ${escapeHTML(professional)}</span><small class="booking-status">${escapeHTML(status)}</small>${amount}</div><div class="booking-actions">${actions}</div></article>`;
    }).join("") || `<p class="empty-state">Todavía no tenés reservas. Elegí un tratamiento y buscá un horario.</p>`;
    list.querySelectorAll(".cancel-booking").forEach((button) => button.addEventListener("click", () => askCancel(button.dataset.id, button.dataset.start)));
    list.querySelectorAll(".complete-treatment").forEach((button) => button.addEventListener("click", () => completeTreatment(button.dataset.id, button)));
  } catch (error) { list.innerHTML = `<p class="form-status error-text">No pudimos cargar las reservas: ${escapeHTML(error.message)}</p>`; }
}

async function completeTreatment(id, button) {
  button.disabled = true;
  try {
    const result = await request(`/reservas/turnos/${encodeURIComponent(id)}/completar`, { method: "POST", body: "{}" });
    showToast(result.acreditacion?.mensaje || "Tratamiento confirmado.");
    const refresh = [loadProfessionalTurns()];
    if (state.user?.roles?.includes("cliente")) refresh.push(loadBookings(), loadPoints());
    await Promise.all(refresh);
  } catch (error) {
    showToast(error.message, true);
    button.disabled = false;
  }
}

async function loadProfessionalTurns() {
  const upcomingList = $("#professional-upcoming");
  const historyList = $("#professional-history");
  try {
    const result = await request("/reservas/turnos");
    const turns = result.turnos || [];
    const ids = [...new Set(turns.map((turn) => turn.tratamientoId).filter(Boolean))];
    const names = new Map();
    await Promise.all(ids.map(async (id) => { try { const treatment = await request(`/tratamientos/${encodeURIComponent(id)}`); names.set(id, treatment.nombre); } catch {} }));
    const now = Date.now();
    const renderTurn = (turn) => {
      const start = new Date(turn.inicio);
      const end = new Date(turn.fin);
      const date = new Intl.DateTimeFormat("es-AR", { dateStyle: "medium", timeStyle: "short" }).format(start);
      const stateText = ({ confirmado: "Confirmado", cancelado: "Cancelado", completado: "Realizado", no_asistio: "No asistió" })[turn.estado] || turn.estado;
      const open = turn.estado === "confirmado";
      const actions = open ? `<div class="booking-actions"><button class="button button-outline complete-treatment" type="button" data-id="${escapeHTML(turn.id)}">Marcar realizado</button><button class="text-link professional-no-show" type="button" data-id="${escapeHTML(turn.id)}" ${end.getTime() > now ? "disabled title=\"Disponible al finalizar el turno\"" : ""}>Marcar no asistió</button><button class="text-link professional-cancel-turn" type="button" data-id="${escapeHTML(turn.id)}">Cancelar turno</button></div>` : "";
      return `<article class="booking-row"><div><strong>${escapeHTML(names.get(turn.tratamientoId) || turn.tratamientoId || "Tratamiento")}</strong><span>${escapeHTML(date)} · Cliente: ${escapeHTML(turn.clienteNombre || turn.clienteId || "No disponible")}</span><small class="booking-status">${escapeHTML(stateText)}</small></div>${actions}</article>`;
    };
    const upcoming = turns.filter((turn) => turn.estado === "confirmado" && new Date(turn.inicio).getTime() >= now);
    const history = turns.filter((turn) => turn.estado !== "confirmado" || new Date(turn.inicio).getTime() < now);
    upcomingList.innerHTML = upcoming.map(renderTurn).join("") || `<p class="empty-state">No hay próximas reservas.</p>`;
    historyList.innerHTML = history.map(renderTurn).join("") || `<p class="empty-state">Todavía no hay turnos en el historial.</p>`;
    document.querySelectorAll(".complete-treatment").forEach((button) => button.addEventListener("click", () => completeTreatment(button.dataset.id, button)));
    document.querySelectorAll(".professional-cancel-turn").forEach((button) => button.addEventListener("click", () => cancelProfessionalTurn(button)));
    document.querySelectorAll(".professional-no-show").forEach((button) => button.addEventListener("click", () => recordNoShow(button)));
  } catch (error) {
    upcomingList.innerHTML = historyList.innerHTML = `<p class="form-status error-text">No pudimos cargar tus turnos: ${escapeHTML(error.message)}</p>`;
  }
}

const professionalWeekdays = ["Domingo", "Lunes", "Martes", "Miércoles", "Jueves", "Viernes", "Sábado"];
async function loadProfessionalAgenda() {
  const container = $("#professional-agenda-days");
  try {
    const result = await request("/reservas/agenda");
    const days = Array.isArray(result.dias) ? result.dias : [];
    container.innerHTML = professionalWeekdays.map((label, day) => {
      const current = days.find((item) => Number(item.diaSemana) === day);
      const blocks = current?.bloques || (current?.activo && current.horaInicio && current.horaFin ? [{ inicio: current.horaInicio, fin: current.horaFin }] : []);
      return `<fieldset class="professional-agenda-day" data-day="${day}"><legend><label><input class="professional-day-active" type="checkbox" ${blocks.length ? "checked" : ""}> ${label}</label></legend><div class="professional-day-blocks">${blocks.map((block) => agendaBlockMarkup(block)).join("")}</div><button class="text-link add-agenda-block" type="button">+ Agregar bloque</button></fieldset>`;
    }).join("");
    container.querySelectorAll(".professional-day-active").forEach((input) => input.addEventListener("change", () => {
      input.closest("fieldset").querySelector(".professional-day-blocks").classList.toggle("hidden", !input.checked);
    }));
    container.querySelectorAll(".add-agenda-block").forEach((button) => button.addEventListener("click", () => {
      const day = button.closest("fieldset");
      day.querySelector(".professional-day-active").checked = true;
      day.querySelector(".professional-day-blocks").classList.remove("hidden");
      day.querySelector(".professional-day-blocks").insertAdjacentHTML("beforeend", agendaBlockMarkup({ inicio: "09:00", fin: "18:00" }));
      bindAgendaBlockRemoval(day);
    }));
    container.querySelectorAll(".professional-day-blocks").forEach((node) => node.classList.toggle("hidden", !node.closest("fieldset").querySelector(".professional-day-active").checked));
    bindAgendaBlockRemoval(container);
    setStatus("#professional-agenda-status", "Disponibilidad cargada.");
  } catch (error) {
    container.innerHTML = `<p class="form-status error-text">No pudimos cargar la disponibilidad: ${escapeHTML(error.message)}</p>`;
  }
}

function agendaBlockMarkup(block) {
  return `<div class="professional-agenda-block"><label>Desde <input type="time" min="08:00" max="20:00" value="${escapeHTML(block.inicio || "09:00")}" required></label><label>Hasta <input type="time" min="08:00" max="20:00" value="${escapeHTML(block.fin || "18:00")}" required></label><button class="text-link remove-agenda-block" type="button">Quitar</button></div>`;
}
function bindAgendaBlockRemoval(root) {
  root.querySelectorAll(".remove-agenda-block").forEach((button) => { if (button.dataset.bound) return; button.dataset.bound = "true"; button.addEventListener("click", () => button.closest(".professional-agenda-block").remove()); });
}
async function saveProfessionalAgenda(event) {
  event.preventDefault();
  const button = $("#save-professional-agenda");
  button.disabled = true;
  try {
    const days = [...document.querySelectorAll(".professional-agenda-day")].map((fieldset) => {
      const diaSemana = Number(fieldset.dataset.day);
      const activo = fieldset.querySelector(".professional-day-active").checked;
      const bloques = activo ? [...fieldset.querySelectorAll(".professional-agenda-block")].map((node) => ({ inicio: node.querySelector("input").value, fin: node.querySelectorAll("input")[1].value })) : [];
      if (activo && !bloques.length) throw new Error(`${professionalWeekdays[diaSemana]} necesita al menos un bloque horario.`);
      return { diaSemana, bloques };
    });
    for (const day of days) await request("/reservas/agenda", { method: "PUT", body: JSON.stringify(day) });
    setStatus("#professional-agenda-status", "Disponibilidad guardada. Los horarios de reservas existentes se conservaron.");
  } catch (error) { setStatus("#professional-agenda-status", error.message, true); }
  finally { button.disabled = false; }
}
async function cancelProfessionalTurn(button) {
  if (!window.confirm("¿Cancelar este turno? Se aplicarán las reglas actuales de cancelación y devolución.")) return;
  button.disabled = true;
  try { await request(`/reservas/turnos/${encodeURIComponent(button.dataset.id)}`, { method: "DELETE" }); showToast("Turno cancelado."); await loadProfessionalTurns(); }
  catch (error) { showToast(error.message, true); button.disabled = false; }
}
async function recordNoShow(button) {
  if (!window.confirm("¿Registrar que el cliente no asistió? No se acreditarán puntos por este turno.")) return;
  button.disabled = true;
  try { await request(`/reservas/turnos/${encodeURIComponent(button.dataset.id)}/no-asistio`, { method: "POST", body: "{}" }); showToast("Inasistencia registrada sin acreditar puntos."); await loadProfessionalTurns(); }
  catch (error) { showToast(error.message, true); button.disabled = false; }
}

async function loadAdminBenefits() {
  try {
    const [rewards, rules, failed] = await Promise.all([
      request("/beneficios/admin/recompensas"), request("/beneficios/admin/reglas-tratamientos"), request("/beneficios/admin/mensajes-fallidos")
    ]);
    $("#admin-rewards-list").innerHTML = `<h3>Recompensas configuradas</h3>${(rewards.recompensas || []).map((reward) => `<article class="movement-row"><div><strong>${escapeHTML(reward.nombre)}</strong><small>${escapeHTML(reward.id)} · ${reward.activa ? "activa" : "inactiva"}</small></div><button class="button button-outline edit-reward" type="button" data-id="${escapeHTML(reward.id)}">Editar</button></article>`).join("") || `<p class="empty-state">Todavía no hay recompensas configuradas.</p>`}`;
    $("#admin-rewards-list").querySelectorAll(".edit-reward").forEach((button) => button.addEventListener("click", () => {
      const reward = (rewards.recompensas || []).find((item) => item.id === button.dataset.id);
      if (!reward) return;
      const form = $("#reward-admin-form");
      for (const key of ["id", "nombre", "descripcion", "tipo", "costoPuntos", "porcentaje", "tratamientoId"]) form.elements[key].value = reward[key] ?? "";
      form.elements.activa.checked = Boolean(reward.activa);
      form.scrollIntoView({ behavior: "smooth", block: "center" });
    }));
    $("#rules-list").innerHTML = (rules.reglas || []).map((rule) => `<article class="movement-row"><div><strong>${escapeHTML(rule.tratamientoId)}</strong><small>${new Intl.NumberFormat("es-AR").format(rule.puntos)} puntos por realización</small></div></article>`).join("") || `<p class="empty-state">Todavía no hay reglas.</p>`;
    $("#failed-messages").innerHTML = (failed.mensajes || []).map((message) => `<article class="movement-row"><div><strong>${escapeHTML(message.evento?.tipo || "Evento")}</strong><small>${escapeHTML(message.error || "Error sin detalle")} · ${Number(message.intentos || 0)} intentos</small></div><button class="button button-outline retry-failed" type="button" data-id="${escapeHTML(message.id)}">Reintentar</button></article>`).join("") || `<p class="empty-state">No hay mensajes pendientes de revisión.</p>`;
    $("#failed-messages").querySelectorAll(".retry-failed").forEach((button) => button.addEventListener("click", async () => {
      button.disabled = true;
      try { await request(`/beneficios/admin/mensajes-fallidos/${encodeURIComponent(button.dataset.id)}/reintentar`, { method: "POST", body: "{}" }); await loadAdminBenefits(); }
      catch (error) { showToast(error.message, true); button.disabled = false; }
    }));
  } catch (error) {
    $("#rules-list").innerHTML = `<p class="form-status error-text">${escapeHTML(error.message)}</p>`;
    $("#failed-messages").innerHTML = `<p class="form-status error-text">${escapeHTML(error.message)}</p>`;
  }
}

function askCancel(id, start) {
  state.cancelling = id;
  const hours = (new Date(start).getTime() - Date.now()) / 3600000;
  $("#cancel-policy").textContent = hours >= 24 ? "Faltan al menos 24 horas para el turno. Según la política de MELT, la seña puede devolverse." : "Faltan menos de 24 horas para el turno. La seña no es reembolsable según la política de MELT.";
  $("#cancel-dialog").showModal();
}

async function cancelBooking() {
  if (!state.cancelling) return;
  const button = $("#confirm-cancel");
  button.disabled = true;
  try {
    await request(`/reservas/turnos/${encodeURIComponent(state.cancelling)}`, { method: "DELETE" });
    $("#cancel-dialog").close();
    state.cancelling = null;
    showToast("La reserva fue cancelada.");
    await loadBookings();
  } catch (error) { showToast(error.message, true); }
  finally { button.disabled = false; }
}

async function loadAvailability(event) {
  event.preventDefault();
  if (!token()) {
    setStatus("#availability-status", "Ingresá o creá tu cuenta para consultar la agenda.", true);
    $("#cuenta").scrollIntoView({ behavior: "smooth" });
    $("#auth-email").focus({ preventScroll: true });
    return;
  }
  const treatmentId = $("#booking-treatment").value;
  const professionalId = $("#booking-professional").value;
  const date = $("#booking-date").value;
  const treatment = window.treatments?.find((item) => item.id === treatmentId) || state.bookingTreatment;
  if (!canBook(treatment)) return updateProfessionals();
  if (!treatmentId || !professionalId || !date) return;
  const query = new URLSearchParams({ profesionalId: professionalId, tratamientoId: treatmentId, fecha: date });
  setStatus("#availability-status", "Consultando disponibilidad…");
  $("#time-slots").innerHTML = "";
  $("#selected-slot-action").classList.add("hidden");
  try {
    const result = await request(`/reservas/disponibilidad?${query}`);
    const slots = result.horarios || [];
    setStatus("#availability-status", slots.length ? "Elegí un horario disponible. Después podés agregarlo al carrito; el pago se inicia desde allí." : "No hay horarios disponibles para esa fecha. Probá con otro día.");
    $("#time-slots").innerHTML = slots.map((time) => `<button class="slot-button" type="button" data-time="${escapeHTML(time)}">${escapeHTML(time)}</button>`).join("");
    $("#time-slots").querySelectorAll(".slot-button").forEach((button) => button.addEventListener("click", () => selectBookingSlot(button.dataset.time, button)));
    updateSteps(2);
  } catch (error) { setStatus("#availability-status", error.message, true); }
}

function selectBookingSlot(time, button) {
  const treatment = window.treatments?.find((item) => item.id === $("#booking-treatment").value) || state.bookingTreatment;
  if (!treatment) return;
  state.selectedSlot = { time, date: $("#booking-date").value, treatmentId: treatment.id, professionalId: $("#booking-professional").value };
  document.querySelectorAll(".slot-button").forEach((item) => item.classList.toggle("selected", item === button));
  $("#selected-slot-action").innerHTML = `<p>Elegiste las <strong>${escapeHTML(time)}</strong>. Se retendrá durante diez minutos al agregarlo.</p><button class="button button-dark" id="add-selected-to-cart" type="button">Agregar al carrito</button>`;
  $("#selected-slot-action").classList.remove("hidden");
  $("#add-selected-to-cart").addEventListener("click", holdSlot);
}

async function holdSlot() {
  const selected = state.selectedSlot;
  if (!selected) return;
  const localStart = new Date(`${selected.date}T${selected.time}:00`);
  if (Number.isNaN(localStart.getTime())) return showToast("La fecha o el horario no son válidos.", true);
  const button = $("#add-selected-to-cart");
  if (button) { button.disabled = true; button.textContent = "Agregando…"; }
  try {
    const result = await request("/reservas/carrito/bloqueos", {
      method: "POST",
      body: JSON.stringify({ profesionalId: selected.professionalId, tratamientoId: selected.treatmentId, inicio: localStart.toISOString(), beneficioCanjeId: $("#booking-benefit").value || undefined, diferirPago: true })
    });
    state.selectedSlot = null;
    $("#selected-slot-action").classList.add("hidden");
    $("#time-slots").querySelector(`[data-time="${CSS.escape(selected.time)}"]`)?.remove();
    setStatus("#availability-status", result.mensaje || "Tratamiento agregado al carrito. El horario quedó retenido por diez minutos.");
    showToast("Tratamiento agregado al carrito. Todavía no se inició ningún pago.");
    updateSteps(3);
    await loadCart();
  } catch (error) { setStatus("#availability-status", error.message, true); if (button) { button.disabled = false; button.textContent = "Agregar al carrito"; } }
}

async function loadCart() {
  if (!token() || !state.user?.roles?.includes("cliente")) return;
  const list = $("#cart-items");
  $("#cart-checkout").disabled = true;
  list.innerHTML = `<p class="muted">Cargando carrito…</p>`;
  try {
    const result = await request("/reservas/carrito");
    state.cartItems = Array.isArray(result.items) ? result.items : [];
    state.cartSummary = summarizeCartItems(state.cartItems, result.resumen);
    const treatmentIDs = [...new Set(state.cartItems.map((item) => item.tratamientoId))];
    const professionalIDs = [...new Set(state.cartItems.map((item) => item.profesionalId))];
    const treatments = new Map();
    const professionals = new Map();
    await Promise.all([
      ...treatmentIDs.map(async (id) => { try { treatments.set(id, await request(`/tratamientos/${encodeURIComponent(id)}`)); } catch {} }),
      ...treatmentIDs.map(async (id) => { try { const response = await request(`/profesionales?tratamientoId=${encodeURIComponent(id)}`); for (const person of response.profesionales || []) if (professionalIDs.includes(person.id)) professionals.set(person.id, person.nombre); } catch {} })
    ]);
    renderCart(treatments, professionals);
    setStatus("#cart-status", state.cartItems.length ? `${state.cartItems.length} ${state.cartItems.length === 1 ? "tratamiento retenido" : "tratamientos retenidos"}. Confirmá una compra simulada para todos los turnos.` : "Tu carrito está vacío. Consultá horarios y agregá un tratamiento.");
  } catch (error) {
    state.cartItems = [];
    state.cartSummary = null;
    list.innerHTML = `<p class="form-status error-text">No pudimos cargar el carrito: ${escapeHTML(error.message)}</p>`;
    $("#cart-count").textContent = "0";
    $("#cart-summary").replaceChildren();
    $("#cart-checkout").disabled = true;
    setStatus("#cart-status", error.message, true);
  }
}

function summarizeCartItems(items, serverSummary) {
  if (!items.length) return { subtotal: 0, descuento: 0, total: 0, sena: 0, restante: 0 };
  let subtotal = 0, descuento = 0, total = 0, sena = 0;
  for (const item of items) {
    const price = Number(item.precio);
    const discount = Number(item.descuentoBeneficio || 0);
    const finalPrice = item.precioFinal == null ? price - discount : Number(item.precioFinal);
    const deposit = item.sena == null ? Math.round(finalPrice * 30) / 100 : Number(item.sena);
    if (![price, discount, finalPrice, deposit].every(Number.isFinite) || price <= 0 || discount < 0 || finalPrice < 0 || deposit < 0) return null;
    subtotal += price;
    descuento += discount;
    total += finalPrice;
    sena += deposit;
  }
  const computed = { subtotal, descuento, total, sena, restante: Math.max(0, total - sena) };
  const keys = Object.keys(computed);
  const serverValues = keys.map((key) => Number(serverSummary?.[key]));
  if (serverValues.every(Number.isFinite) && serverValues.every((value, index) => Math.abs(value - computed[keys[index]]) < 0.02)) {
    return Object.fromEntries(keys.map((key, index) => [key, serverValues[index]]));
  }
  return computed;
}

function renderCart(treatments = new Map(), professionals = new Map()) {
  const items = state.cartItems;
  $("#cart-count").textContent = String(items.length);
  $("#cart-checkout").disabled = !items.length || !state.cartSummary;
  const list = $("#cart-items");
  const summary = state.cartSummary || {};
  $("#cart-summary").innerHTML = items.length && state.cartSummary ? `<div><span>Subtotal</span><strong>${formatPrice(summary.subtotal)}</strong></div><div><span>Descuentos</span><strong>−${formatPrice(summary.descuento)}</strong></div><div><span>Total</span><strong>${formatPrice(summary.total)}</strong></div><div><span>Seña · 30 %</span><strong>${formatPrice(summary.sena)}</strong></div><div><span>Restante</span><strong>${formatPrice(summary.restante)}</strong></div><p class="muted">Pago simulado por MELT · no se realizará ningún cobro real.</p>` : items.length ? `<p class="form-status error-text">Los importes del carrito no son válidos. Actualizá el carrito antes de confirmar.</p>` : "";
  if (!items.length) { list.innerHTML = `<p class="empty-state">Tu carrito está vacío. Podés seguir explorando y agregar varios tratamientos.</p>`; window.clearInterval(state.cartTimer); state.cartTimer = null; return; }
  list.innerHTML = items.map((item) => {
    const treatment = treatments.get(item.tratamientoId) || window.treatments?.find((candidate) => candidate.id === item.tratamientoId);
    const date = new Intl.DateTimeFormat("es-AR", { dateStyle: "medium" }).format(new Date(item.inicio));
    const time = new Intl.DateTimeFormat("es-AR", { timeStyle: "short" }).format(new Date(item.inicio));
    const duration = Math.max(0, Math.round((new Date(item.fin) - new Date(item.inicio)) / 60000));
    return `<article class="cart-item" data-id="${escapeHTML(item.id)}"><div class="cart-item-summary"><div><p class="eyebrow">${escapeHTML(date)} · ${escapeHTML(time)}</p><h3>${escapeHTML(treatment?.nombre || item.tratamientoId)}</h3><p>${escapeHTML(professionals.get(item.profesionalId) || professionalLabel(item.profesionalId, 0))} · ${duration} min</p></div><button class="text-link cart-remove" type="button" data-id="${escapeHTML(item.id)}">Quitar</button></div><div class="cart-item-meta"><span>Precio ${formatPrice(item.precioFinal ?? item.precio)}</span><strong>Seña 30 % · ${formatPrice(item.sena)}</strong><span>Retención <b class="cart-countdown" data-cart-expiration="${escapeHTML(item.expiraEn)}">10:00</b></span></div>${item.descuentoBeneficio ? `<p class="muted">Beneficio aplicado: −${formatPrice(item.descuentoBeneficio)}</p>` : ""}</article>`;
  }).join("");
  list.querySelectorAll(".cart-remove").forEach((button) => button.addEventListener("click", () => removeCartItem(button.dataset.id)));
  tickCartCountdowns();
  window.clearInterval(state.cartTimer);
  state.cartTimer = window.setInterval(tickCartCountdowns, 1000);
}

function tickCartCountdowns() {
  let expired = false;
  document.querySelectorAll("[data-cart-expiration]").forEach((node) => {
    const remaining = Math.max(0, Math.ceil((new Date(node.dataset.cartExpiration).getTime() - Date.now()) / 1000));
    node.textContent = `${String(Math.floor(remaining / 60)).padStart(2, "0")}:${String(remaining % 60).padStart(2, "0")}`;
    if (!remaining) expired = true;
  });
  if (expired) { window.clearInterval(state.cartTimer); state.cartTimer = null; loadCart(); }
}

async function removeCartItem(id) {
  try { await request(`/reservas/carrito/bloqueos/${encodeURIComponent(id)}`, { method: "DELETE" }); state.checkoutKey = null; showToast("Tratamiento quitado y horario liberado."); await loadCart(); }
  catch (error) { showToast(error.message, true); }
}

async function confirmCartPurchase() {
  const button = $("#cart-checkout");
  if (!state.cartItems.length || !state.cartSummary) { button.disabled = true; setStatus("#cart-status", "Actualizá el carrito y verificá sus importes antes de confirmar la compra.", true); return; }
  button.disabled = true;
  try {
    state.checkoutKey ||= (globalThis.crypto?.randomUUID?.() || `checkout-${Date.now()}-${Math.random().toString(36).slice(2)}`);
    const result = await request("/reservas/carrito/checkout", { method: "POST", body: JSON.stringify({ idempotencyKey: state.checkoutKey }) });
    const checkout = result.checkout || {};
    if (checkout.estado === "confirmed") {
      state.checkoutKey = null;
      showToast("Compra simulada aprobada. Todos los turnos quedaron confirmados.");
      setStatus("#cart-status", `Compra confirmada. Se registraron ${checkout.turnos?.length || state.cartItems.length} turnos; no se realizó ningún cobro real.`);
      await Promise.all([loadCart(), loadBookings()]);
    } else if (checkout.estado === "rejected") {
      state.checkoutKey = null;
      setStatus("#cart-status", "Pago simulado rechazado. No se confirmó ningún turno; tus retenciones siguen vigentes para reintentar.", true);
      await loadCart();
    } else {
      setStatus("#cart-status", "La compra sigue procesándose. Podés reintentar sin duplicar el pago.");
    }
  } catch (error) {
    setStatus("#cart-status", error.message, true);
  } finally { button.disabled = !state.cartItems.length || !state.cartSummary; }
}

async function saveRewardAdmin(event) {
  event.preventDefault();
  const form = event.currentTarget;
  const data = new FormData(form);
  const reward = {
    id: String(data.get("id") || "").trim(), nombre: String(data.get("nombre") || "").trim(),
    descripcion: String(data.get("descripcion") || "").trim(), tipo: String(data.get("tipo") || ""),
    costoPuntos: Number(data.get("costoPuntos")), porcentaje: Number(data.get("porcentaje") || 0),
    tratamientoId: String(data.get("tratamientoId") || "").trim(), activa: data.get("activa") === "on"
  };
  try {
    await request("/beneficios/admin/recompensas", { method: "POST", body: JSON.stringify(reward) });
    setStatus("#reward-admin-status", "Recompensa guardada.");
    form.reset();
    form.elements.activa.checked = true;
    await Promise.all([loadBenefits(), loadAdminBenefits()]);
  } catch (error) { setStatus("#reward-admin-status", error.message, true); }
}

async function saveRuleAdmin(event) {
  event.preventDefault();
  const form = event.currentTarget;
  const data = new FormData(form);
  const treatmentId = String(data.get("tratamientoId") || "").trim();
  try {
    await request(`/beneficios/admin/reglas-tratamientos/${encodeURIComponent(treatmentId)}`, { method: "PUT", body: JSON.stringify({ puntos: Number(data.get("puntos")) }) });
    setStatus("#rule-admin-status", "Regla de puntos guardada.");
    form.reset();
    await loadAdminBenefits();
  } catch (error) { setStatus("#rule-admin-status", error.message, true); }
}

function isCatalogAdmin() {
  return Boolean(token() && state.user?.roles?.some((role) => ["administrador", "superadministrador"].includes(role)));
}

function catalogAdminStatus(message, isError = false) {
  const permissionError = /administrador|permiso|sesión administrativa/i.test(message);
  setStatus("#catalog-admin-access", permissionError ? `No tenés permiso para administrar el catálogo. ${message}` : message, isError);
}

function selectedValues(form) {
  return Array.from(form.querySelectorAll('input[name="tratamientosIds"]:checked'), (input) => input.value);
}

function renderCatalogAdmin() {
  const categoryOptions = catalogAdminState.categories.map((item) => `<option value="${escapeHTML(item.nombre)}">${escapeHTML(item.nombre)}</option>`).join("");
  $("#admin-treatment-form").elements.categoria.innerHTML = `<option value="">Elegí una categoría</option>${categoryOptions}`;
  $("#admin-professional-treatments").innerHTML = catalogAdminState.treatments.map((item) => `<label class="catalog-admin-checkbox"><input type="checkbox" name="tratamientosIds" value="${escapeHTML(item.id)}"><span>${escapeHTML(item.nombre)}</span></label>`).join("") || `<p class="muted">No hay tratamientos activos.</p>`;
  const list = (kind, rows, name) => $(`[data-admin-list="${kind}"]`).innerHTML = rows.map((item) => `<article class="catalog-admin-row"><span><strong>${escapeHTML(item.nombre)}</strong><small>${escapeHTML(item.id)}</small></span><span class="catalog-admin-actions"><button class="text-link" type="button" data-admin-edit="${kind}" data-id="${escapeHTML(item.id)}">Editar</button><button class="text-link" type="button" data-admin-delete="${kind}" data-id="${escapeHTML(item.id)}">Desactivar</button></span></article>`).join("") || `<p class="muted">No hay elementos activos.</p>`;
  list("categories", catalogAdminState.categories);
  list("treatments", catalogAdminState.treatments);
  list("professionals", catalogAdminState.professionals);
  renderProfessionalAccountLinks();
}

function renderProfessionalAccountLinks() {
  const container = $("#professional-account-links");
  if (!container) return;
  const claimed = new Map(catalogAdminState.adminProfessionals.filter((item) => item.authUserId).map((item) => [item.authUserId, item.id]));
  container.innerHTML = catalogAdminState.adminProfessionals.map((profile) => {
    const account = catalogAdminState.professionalAccounts.find((item) => item.id === profile.authUserId);
    const currentLabel = account ? `${account.nombre} · ${account.email}` : profile.authUserId ? `Cuenta no disponible · ${profile.authUserId}` : "Sin cuenta vinculada";
    const options = catalogAdminState.professionalAccounts
      .filter((item) => !claimed.has(item.id) || item.id === profile.authUserId)
      .map((item) => `<option value="${escapeHTML(item.id)}">${escapeHTML(item.nombre)} · ${escapeHTML(item.email)}</option>`).join("");
    return `<article class="catalog-admin-row professional-account-row"><span><strong>${escapeHTML(profile.nombre)}</strong><small>${escapeHTML(profile.id)} · ${escapeHTML(currentLabel)}</small></span><div class="professional-account-controls"><select class="form-field professional-account-select" data-account-select="${escapeHTML(profile.id)}" aria-label="Cuenta Auth para ${escapeHTML(profile.nombre)}"><option value="">Elegí una cuenta profesional</option>${options}</select><button class="button button-outline save-professional-account" type="button" data-id="${escapeHTML(profile.id)}">Vincular</button><button class="text-link unlink-professional-account" type="button" data-id="${escapeHTML(profile.id)}" ${profile.authUserId ? "" : "disabled"}>Quitar vínculo</button></div></article>`;
  }).join("") || `<p class="muted">No hay perfiles profesionales activos.</p>`;
  for (const profile of catalogAdminState.adminProfessionals) {
    const select = container.querySelector(`[data-account-select="${CSS.escape(profile.id)}"]`);
    if (select && profile.authUserId && Array.from(select.options).some((option) => option.value === profile.authUserId)) select.value = profile.authUserId;
  }
  setStatus("#professional-account-links-status", `${catalogAdminState.professionalAccounts.length} cuentas profesionales activas disponibles.`);
}

async function loadCatalogAdmin() {
  if (!isCatalogAdmin()) {
    catalogAdminStatus("Iniciá sesión con una cuenta administradora para acceder a esta sección.", true);
    return;
  }
  catalogAdminStatus("Cargando categorías, tratamientos y profesionales…");
  try {
    const [categories, treatments, professionals, adminProfessionals, accounts] = await Promise.all([
      request("/catalogo/categorias"),
      request("/catalogo/tratamientos?page=1&limit=50"),
      request("/catalogo/profesionales"),
      request("/catalogo/admin/profesionales"),
      request("/auth/usuarios/profesionales")
    ]);
    catalogAdminState.categories = Array.isArray(categories?.categorias) ? categories.categorias : [];
    catalogAdminState.treatments = Array.isArray(treatments?.tratamientos) ? treatments.tratamientos : [];
    catalogAdminState.professionals = Array.isArray(professionals?.profesionales) ? professionals.profesionales : [];
    catalogAdminState.adminProfessionals = Array.isArray(adminProfessionals?.profesionales) ? adminProfessionals.profesionales : [];
    catalogAdminState.professionalAccounts = Array.isArray(accounts?.usuarios) ? accounts.usuarios : [];
    renderCatalogAdmin();
    catalogAdminStatus("Listas actualizadas.");
  } catch (error) {
    catalogAdminStatus(error.message, true);
  }
}

function resetCatalogAdminForm(kind) {
  const form = $(`#admin-${kind}-form`);
  form.reset();
  form.dataset.editing = "false";
  if (kind === "treatment") form.dataset.profesionalesIds = "[]";
  form.elements.id.readOnly = false;
  form.querySelector(`[data-admin-cancel="${kind}"]`).classList.add("hidden");
  setStatus(`[data-admin-status="${kind}"]`, "");
}

function editCatalogAdmin(kind, id) {
  const forms = { categories: "category", treatments: "treatment", professionals: "professional" };
  const formKind = forms[kind];
  const item = catalogAdminState[kind].find((row) => row.id === id);
  if (!item || !formKind) return;
  const form = $(`#admin-${formKind}-form`);
  form.reset();
  form.dataset.editing = "true";
  form.elements.id.value = item.id;
  form.elements.id.readOnly = true;
  form.elements.nombre.value = item.nombre || "";
  if (formKind === "category") form.elements.descripcion.value = item.descripcion || "";
  if (formKind === "treatment") {
    form.elements.descripcion.value = item.descripcion || "";
    form.elements.categoria.value = item.categoria || "";
    form.elements.precio.value = item.precio ?? 0;
    form.elements.duracionMinutos.value = item.duracionMinutos ?? "";
    form.elements.keywords.value = (item.keywords || []).join(", ");
    form.elements.imagenes.value = (item.imagenes || []).join(", ");
    // El contrato de actualización reemplaza el tratamiento completo: conservar asociaciones aquí
    // evita que editar sus datos las modifique; su gestión se realiza desde Profesionales.
    form.dataset.profesionalesIds = JSON.stringify(item.profesionalesIds || []);
  }
  if (formKind === "professional") {
    form.elements.descripcion.value = item.descripcion || "";
    form.querySelectorAll('input[name="tratamientosIds"]').forEach((input) => { input.checked = (item.tratamientosIds || []).includes(input.value); });
  }
  form.querySelector(`[data-admin-cancel="${formKind}"]`).classList.remove("hidden");
  setStatus(`[data-admin-status="${formKind}"]`, `Editando ${item.nombre}.`);
  form.scrollIntoView({ behavior: "smooth", block: "start" });
}

async function saveCatalogAdmin(event, kind) {
  event.preventDefault();
  if (!isCatalogAdmin()) {
    setStatus(`[data-admin-status="${kind}"]`, "No tenés permiso para administrar el catálogo.", true);
    return;
  }
  const form = event.currentTarget;
  const data = new FormData(form);
  const id = String(data.get("id") || "").trim();
  let resource, payload;
  if (kind === "category") {
    resource = "categorias";
    payload = { id, nombre: String(data.get("nombre") || "").trim(), descripcion: String(data.get("descripcion") || "").trim() };
  } else if (kind === "treatment") {
    resource = "tratamientos";
    payload = {
      id, nombre: String(data.get("nombre") || "").trim(), descripcion: String(data.get("descripcion") || "").trim(), categoria: String(data.get("categoria") || ""),
      precio: Number(data.get("precio")), moneda: "ARS", duracionMinutos: Number(data.get("duracionMinutos")),
      keywords: String(data.get("keywords") || "").split(",").map((value) => value.trim()).filter(Boolean),
      imagenes: String(data.get("imagenes") || "").split(",").map((value) => value.trim()).filter(Boolean),
      profesionalesIds: JSON.parse(form.dataset.profesionalesIds || "[]")
    };
  } else {
    resource = "profesionales";
    payload = { id, nombre: String(data.get("nombre") || "").trim(), descripcion: String(data.get("descripcion") || "").trim(), tratamientosIds: selectedValues(form), activo: true };
  }
  const editing = form.dataset.editing === "true";
  const path = `/catalogo/${resource}${editing ? `/${encodeURIComponent(id)}` : ""}`;
  try {
    await request(path, { method: editing ? "PUT" : "POST", body: JSON.stringify(payload) });
    resetCatalogAdminForm(kind);
    setStatus(`[data-admin-status="${kind}"]`, `${kind === "category" ? "Categoría" : kind === "treatment" ? "Tratamiento" : "Profesional"} ${editing ? "actualizado" : "creado"}.`);
    await Promise.all([loadCatalogAdmin(), loadCategories(), loadTreatments(), loadProfessionals()]);
  } catch (error) {
    setStatus(`[data-admin-status="${kind}"]`, error.message, true);
  }
}

async function deactivateCatalogItem(kind, id) {
  if (!isCatalogAdmin()) return catalogAdminStatus("No tenés permiso para administrar el catálogo.", true);
  const labels = { categories: "esta categoría", treatments: "este tratamiento", professionals: "este profesional" };
  if (!window.confirm(`¿Querés desactivar ${labels[kind]}?`)) return;
  const resource = { categories: "categorias", treatments: "tratamientos", professionals: "profesionales" }[kind];
  try {
    await request(`/catalogo/${resource}/${encodeURIComponent(id)}`, { method: "DELETE" });
    catalogAdminStatus("Elemento desactivado.");
    await Promise.all([loadCatalogAdmin(), loadCategories(), loadTreatments(), loadProfessionals()]);
  } catch (error) { catalogAdminStatus(error.message, true); }
}

function handleCatalogAdminAction(event) {
  const edit = event.target.closest("[data-admin-edit]");
  if (edit) return editCatalogAdmin(edit.dataset.adminEdit, edit.dataset.id);
  const remove = event.target.closest("[data-admin-delete]");
  if (remove) deactivateCatalogItem(remove.dataset.adminDelete, remove.dataset.id);
}

async function handleProfessionalAccountLink(event) {
  const link = event.target.closest(".save-professional-account, .unlink-professional-account");
  if (!link) return;
  if (!isCatalogAdmin()) return catalogAdminStatus("No tenés permiso para administrar las cuentas profesionales.", true);
  const profileID = link.dataset.id;
  const select = $(`#professional-account-links [data-account-select="${CSS.escape(profileID)}"]`);
  const authUserId = link.classList.contains("unlink-professional-account") ? "" : String(select?.value || "");
  if (!authUserId && link.classList.contains("save-professional-account")) {
    setStatus("#professional-account-links-status", "Elegí una cuenta profesional para vincular.", true);
    return;
  }
  link.disabled = true;
  try {
    await request(`/catalogo/admin/profesionales/${encodeURIComponent(profileID)}/cuenta`, { method: "PUT", body: JSON.stringify({ authUserId }) });
    setStatus("#professional-account-links-status", authUserId ? "Cuenta vinculada al perfil." : "Cuenta desvinculada del perfil.");
    await loadCatalogAdmin();
  } catch (error) {
    setStatus("#professional-account-links-status", error.message, true);
    link.disabled = false;
  }
}

function initialize() {
  const updateHeaderOnScroll = () => $(".site-header").classList.toggle("site-header-scrolled", window.scrollY > 70);
  window.addEventListener("scroll", updateHeaderOnScroll, { passive: true });
  updateHeaderOnScroll();
  $("#search-form").addEventListener("submit", (event) => {
    event.preventDefault();
    state.search = $("#search-query").value.trim();
    state.category = $("#search-category").value;
    state.priceMax = $("#search-price").value;
    [state.sort, state.order] = $("#search-sort").value.split(":");
    state.page = 1;
    syncCategoryNavigation();
    loadTreatments();
  });
  $("#clear-filters").addEventListener("click", () => {
    $("#search-form").reset();
    state.search = state.category = state.priceMax = "";
    state.sort = "nombre"; state.order = "asc"; state.page = 1;
    syncCategoryNavigation();
    loadTreatments();
  });
  $("[data-professional-back]").addEventListener("click", () => showProfessionalList());
  $("#professionals-prev").addEventListener("click", () => $("#professionals-list").scrollBy({ left: -260, behavior: "smooth" }));
  $("#professionals-next").addEventListener("click", () => $("#professionals-list").scrollBy({ left: 260, behavior: "smooth" }));
  $("#previous-page").addEventListener("click", () => { if (state.page > 1) { state.page--; loadTreatments(); } });
  $("#next-page").addEventListener("click", () => { if (state.hasNext) { state.page++; loadTreatments(); } });
  $("#login-tab").addEventListener("click", () => setAuthMode("login"));
  $("#register-tab").addEventListener("click", () => setAuthMode("register"));
  $("#auth-form").addEventListener("submit", submitAuth);
  $("#discard-invitation").addEventListener("click", clearInvitation);
  window.addEventListener("hashchange", () => {
    if (new URLSearchParams(window.location.hash.slice(1)).has("invitacion")) initializeInvitation().then(() => loadAccount());
    if (window.location.hash.startsWith("#profesionales")) syncProfessionalHash();
    else showProfessionalList(false);
  });
  window.addEventListener("popstate", syncProfessionalHash);
  $("#availability-form").addEventListener("submit", loadAvailability);
  $("#booking-treatment").addEventListener("change", () => { state.selectedSlot = null; $("#time-slots").innerHTML = ""; $("#selected-slot-action").classList.add("hidden"); updateProfessionals(); });
  $("#booking-professional").addEventListener("change", () => { state.selectedSlot = null; $("#time-slots").innerHTML = ""; $("#selected-slot-action").classList.add("hidden"); updateSteps(1); });
  $("#booking-date").addEventListener("change", () => { state.selectedSlot = null; $("#time-slots").innerHTML = ""; $("#selected-slot-action").classList.add("hidden"); updateSteps(1); });
  $("#logout-button").addEventListener("click", async () => { sessionStorage.removeItem(TOKEN_KEY); showSignedOut(); showToast("Cerraste sesión."); });
  $("#refresh-account").addEventListener("click", loadAccount);
  $("#refresh-bookings").addEventListener("click", loadBookings);
  $("#refresh-cart").addEventListener("click", loadCart);
  $("#cart-checkout").addEventListener("click", confirmCartPurchase);
  $("#refresh-benefits").addEventListener("click", async () => { await Promise.all([loadPoints(), loadBenefits()]); });
  $("#refresh-professional-turns").addEventListener("click", () => Promise.all([loadProfessionalAgenda(), loadProfessionalTurns()]));
  $("#professional-agenda-form").addEventListener("submit", saveProfessionalAgenda);
  $("#refresh-admin").addEventListener("click", loadAdminBenefits);
  $("#refresh-users").addEventListener("click", loadSuperadminUsers);
  $("#superadmin-create-user").addEventListener("submit", createManagedUser);
  $("#superadmin-users-list").addEventListener("click", saveManagedUserRole);
  $("#refresh-catalog-admin").addEventListener("click", loadCatalogAdmin);
  $("#professional-account-links").addEventListener("click", handleProfessionalAccountLink);
  $("#admin-category-form").addEventListener("submit", (event) => saveCatalogAdmin(event, "category"));
  $("#admin-treatment-form").addEventListener("submit", (event) => saveCatalogAdmin(event, "treatment"));
  $("#admin-professional-form").addEventListener("submit", (event) => saveCatalogAdmin(event, "professional"));
  document.querySelectorAll("[data-admin-cancel]").forEach((button) => button.addEventListener("click", () => resetCatalogAdminForm(button.dataset.adminCancel)));
  document.querySelectorAll("[data-admin-list]").forEach((list) => list.addEventListener("click", handleCatalogAdminAction));
  $("#reward-admin-form").addEventListener("submit", saveRewardAdmin);
  $("#rule-admin-form").addEventListener("submit", saveRuleAdmin);
  $("#profile-reservations").addEventListener("click", () => $("#my-bookings").scrollIntoView({ behavior: "smooth" }));
  document.querySelectorAll("[data-booking-auth]").forEach((button) => button.addEventListener("click", () => { setAuthMode(button.dataset.bookingAuth); $("#auth-email").focus({ preventScroll: true }); }));
  document.querySelectorAll("[data-scroll]").forEach((button) => button.addEventListener("click", () => document.getElementById(button.dataset.scroll).scrollIntoView({ behavior: "smooth" })));
  document.querySelectorAll("[data-register]").forEach((button) => button.addEventListener("click", () => setAuthMode("register")));
  $("#detail-book").addEventListener("click", () => {
    const id = $("#detail-book").dataset.id;
    const item = window.treatments?.find((treatment) => treatment.id === id) || (state.detailTreatment?.id === id ? state.detailTreatment : null);
    if (item && canBook(item)) { $("#treatment-dialog").close(); selectTreatment(item); }
  });
  $("#confirm-cancel").addEventListener("click", (event) => { event.preventDefault(); cancelBooking(); });
  $("#cancel-dialog").addEventListener("click", (event) => { if (event.target === $("#cancel-dialog")) $("#cancel-dialog").close(); });
  const menu = $("#site-menu"), backdrop = $("#menu-backdrop"), menuToggle = $("#menu-toggle");
  const closeMenu = () => { if (!menu.classList.contains("open")) return; menu.classList.remove("open"); backdrop.classList.remove("visible"); menu.setAttribute("aria-hidden", "true"); menu.inert = true; menuToggle.setAttribute("aria-expanded", "false"); document.body.classList.remove("menu-open"); menuToggle.focus(); };
  const openMenu = () => { menu.classList.add("open"); backdrop.classList.add("visible"); menu.setAttribute("aria-hidden", "false"); menu.inert = false; menuToggle.setAttribute("aria-expanded", "true"); document.body.classList.add("menu-open"); $("#menu-close").focus(); };
  menuToggle.addEventListener("click", openMenu);
  $("#menu-close").addEventListener("click", closeMenu);
  backdrop.addEventListener("click", closeMenu);
  menu.querySelectorAll("a").forEach((link) => link.addEventListener("click", closeMenu));
  document.addEventListener("keydown", (event) => { if (event.key === "Escape") closeMenu(); });
  $("#featured-prev").addEventListener("click", () => $("#featured-list").scrollBy({ left: -340, behavior: "smooth" }));
  $("#featured-next").addEventListener("click", () => $("#featured-list").scrollBy({ left: 340, behavior: "smooth" }));
  const dateInput = $("#booking-date");
  dateInput.min = new Date(Date.now() - new Date().getTimezoneOffset() * 60000).toISOString().slice(0, 10);
  dateInput.value = dateInput.min;
  updateAuthChrome();
  loadCategories();
  loadProfessionals();
  loadTreatments();
  syncProfessionalHash();
  initializeInvitation().then(() => loadAccount());
}

initialize();
