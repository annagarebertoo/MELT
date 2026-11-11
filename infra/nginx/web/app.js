const API = "/api/v1";
const TOKEN_KEY = "melt.session";
const CART_KEY = "melt.pending-cart";
const INVITATION_KEY = "melt.pending-invitation";
let invitation = null;
const state = { page: 1, pageSize: 8, hasNext: false, search: "", category: "", priceMax: "", sort: "nombre", order: "asc", bookingTreatment: null, heldCartId: null, holdExpiresAt: null, holdTimer: null, cancelling: null, mode: "login", user: null, rewardRequests: new Map(), rewards: [], canjes: [] };

const $ = (selector) => document.querySelector(selector);
const token = () => sessionStorage.getItem(TOKEN_KEY);
const formatPrice = (amount) => new Intl.NumberFormat("es-AR", { style: "currency", currency: "ARS", maximumFractionDigits: 0 }).format(Number(amount || 0));
const escapeHTML = (value) => String(value ?? "").replace(/[&<>"']/g, (char) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[char]);
const treatmentLabel = (item) => Number(item?.duracionMinutos) >= 5 ? `${item.nombre} · ${Number(item.duracionMinutos)} min` : `${item.nombre} · agenda en actualización`;
const canBook = (item) => Number(item?.duracionMinutos) >= 5 && Array.isArray(item?.profesionalesIds) && item.profesionalesIds.length > 0 && (window.professionalsByTreatment?.[item.id]?.length || 0) > 0;

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

function updateProfessionals() {
  const id = $("#booking-treatment").value;
  const treatment = window.treatments?.find((item) => item.id === id) || (state.bookingTreatment?.id === id ? state.bookingTreatment : null);
  state.bookingTreatment = treatment;
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

function detailTreatment(item) {
  $("#detail-initial").textContent = (item.categoria || item.nombre || "M").slice(0, 1).toLocaleUpperCase("es-AR");
  $("#detail-category").textContent = item.categoria || "Tratamiento MELT";
  $("#detail-title").textContent = item.nombre || "Tratamiento";
  $("#detail-description").textContent = item.descripcion || "Un momento para vos, con atención personalizada.";
  $("#detail-duration").textContent = Number(item.duracionMinutos) >= 5 ? `${Number(item.duracionMinutos)} min` : "A confirmar";
  $("#detail-price").textContent = formatPrice(item.precio);
  const keywords = Array.isArray(item.keywords) ? item.keywords : [];
  $("#detail-keywords").innerHTML = keywords.length ? keywords.map((word) => `<span>${escapeHTML(word)}</span>`).join("") : "";
  const people = window.professionalsByTreatment?.[item.id] || [];
  $("#detail-professionals").textContent = people.length ? `Atiende${people.length === 1 ? "" : "n"}: ${people.map((person) => person.nombre).join(", ")}` : "Profesionales: agenda en actualización";
  const available = canBook(item);
  $("#detail-availability").textContent = available ? "Consultá disponibilidad en tiempo real y elegí el horario que te quede mejor." : "La reserva online para este tratamiento estará disponible cuando el catálogo tenga duración y profesionales asignados.";
  const button = $("#detail-book");
  button.disabled = !available;
  button.textContent = available ? "Elegir horario ↗" : "Agenda en actualización";
  button.dataset.id = item.id;
  $("#treatment-dialog").showModal();
}

async function showTreatmentDetail(id) {
  try {
    const item = await request(`/tratamientos/${encodeURIComponent(id)}`);
    detailTreatment(item);
  } catch (error) { showToast(error.message, true); }
}

function treatmentCard(item) {
  const duration = Number(item.duracionMinutos) >= 5 ? `${Number(item.duracionMinutos)} min` : "Duración a confirmar";
  const bookable = canBook(item);
  return `<article class="treatment-card ${bookable ? "" : "unavailable"}">
    <button class="treatment-visual detail-treatment" type="button" data-id="${escapeHTML(item.id)}" aria-label="Ver detalles de ${escapeHTML(item.nombre)}"><span>${escapeHTML((item.categoria || "M").slice(0, 1))}</span></button>
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
        <button class="featured-image featured-image-${index + 1} detail-treatment" type="button" data-id="${escapeHTML(item.id)}" aria-label="Ver detalles de ${escapeHTML(item.nombre)}"><span>${escapeHTML((item.categoria || "M").slice(0, 1))}</span></button>
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
  $("#booking-summary").classList.remove("hidden");
  $("#booking-summary").innerHTML = `<span class="summary-mark">M</span><div><small>Tu elección</small><strong>${escapeHTML(item.nombre)}</strong></div><span>${formatPrice(item.precio)} · ${Number(item.duracionMinutos) >= 5 ? `${Number(item.duracionMinutos)} min` : "duración a confirmar"}</span>`;
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
    const isClient = roles.includes("cliente");
    $("#points-panel").classList.toggle("hidden", !isClient);
    $("#professional-panel").classList.toggle("hidden", !roles.includes("profesional"));
    $("#admin-panel").classList.toggle("hidden", !roles.some((role) => ["administrador", "superadministrador"].includes(role)));
    $("#auth-form").closest(".account-card").classList.add("hidden");
    await confirmInvitation();
    await Promise.all([loadBookings(), ...(isClient ? [loadPoints(), loadBenefits()] : [])]);
    if (roles.includes("profesional")) await loadProfessionalTurns();
    if (roles.some((role) => ["administrador", "superadministrador"].includes(role))) await loadAdminBenefits();
  } catch {
    sessionStorage.removeItem(TOKEN_KEY);
    updateAuthChrome();
    showSignedOut();
  }
}

function showSignedOut() {
  state.user = null;
  $("#points-panel").classList.add("hidden");
  $("#professional-panel").classList.add("hidden");
  $("#admin-panel").classList.add("hidden");
  $("#profile-panel").classList.add("hidden");
  $("#my-bookings").classList.add("hidden");
  $("#auth-form").closest(".account-card").classList.remove("hidden");
  $("#points-balance").textContent = "—";
  updateAuthChrome();
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
    await Promise.all([loadBookings(), loadProfessionalTurns(), loadPoints()]);
  } catch (error) {
    showToast(error.message, true);
    button.disabled = false;
  }
}

async function loadProfessionalTurns() {
  const list = $("#professional-turns");
  try {
    const result = await request("/reservas/turnos");
    const turns = result.turnos || [];
    list.innerHTML = turns.length ? turns.map((turn) => {
      const date = new Intl.DateTimeFormat("es-AR", { dateStyle: "medium", timeStyle: "short" }).format(new Date(turn.inicio));
      const completed = turn.estado === "completado";
      const confirm = turn.estado === "confirmado" ? `<button class="button button-outline complete-treatment" type="button" data-id="${escapeHTML(turn.id)}">Confirmar realizado</button>` : "";
      return `<article class="movement-row"><div><strong>${escapeHTML(turn.tratamientoId)}</strong><small>${escapeHTML(date)} · ${completed ? "Tratamiento realizado" : escapeHTML(turn.estado)}</small></div>${confirm}</article>`;
    }).join("") : `<p class="empty-state">No hay turnos asignados para mostrar.</p>`;
    list.querySelectorAll(".complete-treatment").forEach((button) => button.addEventListener("click", () => completeTreatment(button.dataset.id, button)));
  } catch (error) {
    list.innerHTML = `<p class="form-status error-text">No pudimos cargar tus turnos: ${escapeHTML(error.message)}</p>`;
  }
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
  $("#payment-form").classList.add("hidden");
  try {
    const result = await request(`/reservas/disponibilidad?${query}`);
    const slots = result.horarios || [];
    setStatus("#availability-status", slots.length ? "Elegí un horario disponible. Lo vas a tener reservado por 10 minutos." : "No hay horarios disponibles para esa fecha. Probá con otro día.");
    $("#time-slots").innerHTML = slots.map((time) => `<button class="slot-button" type="button" data-time="${escapeHTML(time)}">${escapeHTML(time)}</button>`).join("");
    $("#time-slots").querySelectorAll(".slot-button").forEach((button) => button.addEventListener("click", () => holdSlot(button.dataset.time)));
    updateSteps(2);
  } catch (error) { setStatus("#availability-status", error.message, true); }
}

function clearHoldUI() {
  window.clearInterval(state.holdTimer);
  state.holdTimer = null;
  state.heldCartId = null;
  state.holdExpiresAt = null;
  $("#hold-countdown").textContent = "10:00";
  $("#payment-form").classList.add("hidden");
  $("#payment-id").value = "";
  sessionStorage.removeItem(CART_KEY);
}

function startCountdown(expiration) {
  state.holdExpiresAt = new Date(expiration).getTime();
  window.clearInterval(state.holdTimer);
  const tick = () => {
    const remaining = Math.max(0, Math.ceil((state.holdExpiresAt - Date.now()) / 1000));
    $("#hold-countdown").textContent = `${String(Math.floor(remaining / 60)).padStart(2, "0")}:${String(remaining % 60).padStart(2, "0")}`;
    if (!remaining) {
      clearHoldUI();
      setStatus("#availability-status", "El tiempo de reserva terminó. Volvé a consultar la agenda para elegir otro horario.", true);
      updateSteps(2);
    }
  };
  tick();
  state.holdTimer = window.setInterval(tick, 1000);
}

async function holdSlot(time) {
  const date = $("#booking-date").value;
  const localStart = new Date(`${date}T${time}:00`);
  if (Number.isNaN(localStart.getTime())) return showToast("La fecha o el horario no son válidos.", true);
  try {
    if (state.heldCartId) await releaseHold(false);
    const result = await request("/reservas/carrito/bloqueos", {
      method: "POST",
      body: JSON.stringify({ profesionalId: $("#booking-professional").value, tratamientoId: $("#booking-treatment").value, inicio: localStart.toISOString(), beneficioCanjeId: $("#booking-benefit").value || undefined })
    });
    state.heldCartId = result.bloqueo.id;
    $("#hold-message").textContent = result.bloqueo.descuentoBeneficio ? `Beneficio aplicado: ${formatPrice(result.bloqueo.descuentoBeneficio)} de descuento. El horario está protegido durante diez minutos.` : `El horario está protegido durante diez minutos. Completá el pago antes de que venza el contador.`;
    $("#hold-amount").textContent = formatPrice(result.bloqueo.sena);
    $("#checkout-link").href = result.bloqueo.urlPago || result.urlPago;
    sessionStorage.setItem(CART_KEY, JSON.stringify({ id: result.bloqueo.id, expiresAt: result.bloqueo.expiraEn, urlPago: result.bloqueo.urlPago || result.urlPago, sena: result.bloqueo.sena }));
    $("#payment-form").classList.remove("hidden");
    $("#payment-id").focus();
    startCountdown(result.bloqueo.expiraEn);
    setStatus("#availability-status", "Horario guardado temporalmente en tu carrito.");
    updateSteps(3);
  } catch (error) { setStatus("#availability-status", error.message, true); }
}

function restorePendingCart() {
  if (!token()) return;
  try {
    const saved = JSON.parse(sessionStorage.getItem(CART_KEY) || "null");
    if (!saved?.id || !saved?.expiresAt || new Date(saved.expiresAt).getTime() <= Date.now()) {
      if (saved) {
        sessionStorage.removeItem(CART_KEY);
        setStatus("#availability-status", "El bloqueo anterior venció. Consultá nuevamente para elegir otro horario.");
      }
      return;
    }
    state.heldCartId = saved.id;
    $("#hold-amount").textContent = formatPrice(saved.sena);
    $("#checkout-link").href = saved.urlPago || "#";
    $("#hold-message").textContent = "Tu carrito sigue activo. Completá el pago antes de que termine el contador.";
    $("#payment-form").classList.remove("hidden");
    startCountdown(saved.expiresAt);
    updateSteps(3);
    const paymentID = new URLSearchParams(window.location.search).get("payment_id") || new URLSearchParams(window.location.search).get("collection_id");
    if (paymentID && /^\d+$/.test(paymentID)) {
      $("#payment-id").value = paymentID;
      setStatus("#availability-status", "Recibimos el regreso de Mercado Pago. Validá el ID para confirmar tu turno.");
    }
  } catch {
    sessionStorage.removeItem(CART_KEY);
  }
}

async function confirmBooking(event) {
  event.preventDefault();
  if (!state.heldCartId) return;
  const button = $("#confirm-booking");
  button.disabled = true;
  button.textContent = "Validando pago…";
  try {
    await request(`/reservas/carrito/bloqueos/${encodeURIComponent(state.heldCartId)}/confirmar`, { method: "POST", body: JSON.stringify({ pagoId: $("#payment-id").value.trim() }) });
    clearHoldUI();
    $("#payment-form").reset();
    setStatus("#availability-status", "¡Turno confirmado! Ya está en tu lista de reservas.");
    showToast("¡Tu turno quedó confirmado!");
    await Promise.all([loadBookings(), loadBenefits(), loadPoints()]);
    updateSteps(1);
    $("#my-bookings").scrollIntoView({ behavior: "smooth" });
  } catch (error) { setStatus("#availability-status", error.message, true); }
  finally { button.disabled = false; button.textContent = "Validar pago y confirmar turno"; }
}

async function releaseHold(showMessage = true) {
  if (!state.heldCartId) return;
  const id = state.heldCartId;
  try {
    await request(`/reservas/carrito/bloqueos/${encodeURIComponent(id)}`, { method: "DELETE" });
    clearHoldUI();
    if (showMessage) setStatus("#availability-status", "Liberaste el horario. Podés elegir otro.");
    updateSteps(2);
  } catch (error) {
    if (showMessage) setStatus("#availability-status", error.message, true);
    else throw error;
  }
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

function initialize() {
  $("#search-form").addEventListener("submit", (event) => {
    event.preventDefault();
    state.search = $("#search-query").value.trim();
    state.category = $("#search-category").value;
    state.priceMax = $("#search-price").value;
    [state.sort, state.order] = $("#search-sort").value.split(":");
    state.page = 1;
    loadTreatments();
  });
  $("#clear-filters").addEventListener("click", () => {
    $("#search-form").reset();
    state.search = state.category = state.priceMax = "";
    state.sort = "nombre"; state.order = "asc"; state.page = 1;
    loadTreatments();
  });
  $("#previous-page").addEventListener("click", () => { if (state.page > 1) { state.page--; loadTreatments(); } });
  $("#next-page").addEventListener("click", () => { if (state.hasNext) { state.page++; loadTreatments(); } });
  $("#login-tab").addEventListener("click", () => setAuthMode("login"));
  $("#register-tab").addEventListener("click", () => setAuthMode("register"));
  $("#auth-form").addEventListener("submit", submitAuth);
  $("#discard-invitation").addEventListener("click", clearInvitation);
  window.addEventListener("hashchange", () => {
    if (new URLSearchParams(window.location.hash.slice(1)).has("invitacion")) initializeInvitation().then(() => loadAccount());
  });
  $("#availability-form").addEventListener("submit", loadAvailability);
  $("#booking-treatment").addEventListener("change", updateProfessionals);
  $("#booking-date").addEventListener("change", () => { $("#time-slots").innerHTML = ""; updateSteps(1); });
  $("#payment-form").addEventListener("submit", confirmBooking);
  $("#release-hold").addEventListener("click", () => releaseHold());
  $("#logout-button").addEventListener("click", async () => { if (state.heldCartId) await releaseHold(false); sessionStorage.removeItem(TOKEN_KEY); showSignedOut(); showToast("Cerraste sesión."); });
  $("#refresh-account").addEventListener("click", loadAccount);
  $("#refresh-bookings").addEventListener("click", loadBookings);
  $("#refresh-benefits").addEventListener("click", async () => { await Promise.all([loadPoints(), loadBenefits()]); });
  $("#refresh-professional-turns").addEventListener("click", loadProfessionalTurns);
  $("#refresh-admin").addEventListener("click", loadAdminBenefits);
  $("#reward-admin-form").addEventListener("submit", saveRewardAdmin);
  $("#rule-admin-form").addEventListener("submit", saveRuleAdmin);
  $("#profile-reservations").addEventListener("click", () => $("#my-bookings").scrollIntoView({ behavior: "smooth" }));
  document.querySelectorAll("[data-booking-auth]").forEach((button) => button.addEventListener("click", () => { setAuthMode(button.dataset.bookingAuth); $("#auth-email").focus({ preventScroll: true }); }));
  document.querySelectorAll("[data-scroll]").forEach((button) => button.addEventListener("click", () => document.getElementById(button.dataset.scroll).scrollIntoView({ behavior: "smooth" })));
  document.querySelectorAll("[data-register]").forEach((button) => button.addEventListener("click", () => setAuthMode("register")));
  $("#detail-book").addEventListener("click", () => {
    const item = window.treatments?.find((treatment) => treatment.id === $("#detail-book").dataset.id);
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
  loadTreatments();
  initializeInvitation().then(() => loadAccount());
  restorePendingCart();
}

initialize();
