const API = "/api/v1";
const TOKEN_KEY = "melt.session";
const state = { page: 1, pageSize: 8, hasNext: false, search: "", category: "", priceMax: "", bookingTreatment: null, heldCartId: null, mode: "login" };

const $ = (selector) => document.querySelector(selector);
const token = () => sessionStorage.getItem(TOKEN_KEY);
const formatPrice = (amount) => new Intl.NumberFormat("es-AR", { style: "currency", currency: "ARS", maximumFractionDigits: 0 }).format(Number(amount || 0));
const escapeHTML = (value) => String(value ?? "").replace(/[&<>"']/g, (char) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[char]);

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

async function loadTreatments() {
  const params = new URLSearchParams({ page: String(state.page), limit: String(state.pageSize), sort: "nombre", order: "asc" });
  if (state.search) params.set("q", state.search);
  if (state.category) params.set("categoria", state.category);
  if (state.priceMax) params.set("precioMax", state.priceMax);
  setStatus("#catalog-status", "Cargando tratamientos…");
  try {
    const result = await request(`/tratamientos?${params}`);
    const treatments = result.tratamientos || [];
    window.treatments = treatments;
    const total = Number(result.pagination?.total || 0);
    state.hasNext = state.page * state.pageSize < total;
    $("#previous-page").disabled = state.page <= 1;
    $("#next-page").disabled = !state.hasNext;
    $("#page-label").textContent = `Página ${state.page}`;
    $("#catalog-status").textContent = `${total} ${total === 1 ? "tratamiento disponible" : "tratamientos disponibles"}`;
    $("#treatments-list").innerHTML = treatments.map((item) => `
      <article class="treatment-card">
        <div class="treatment-visual" aria-hidden="true"><span>${escapeHTML((item.categoria || "M").slice(0, 1))}</span></div>
        <div class="treatment-copy"><p class="eyebrow">${escapeHTML(item.categoria)}</p><h3>${escapeHTML(item.nombre)}</h3>
          <p>${escapeHTML(item.descripcion)}</p><div class="treatment-meta"><span>${Number(item.duracionMinutos)} min</span><strong>${formatPrice(item.precio)}</strong></div>
          <button class="text-link book-treatment" data-id="${escapeHTML(item.id)}">Ver horarios <span aria-hidden="true">↗</span></button>
        </div>
      </article>`).join("") || `<p class="empty-state">No encontramos tratamientos con esos filtros. Probá con otra búsqueda.</p>`;
    $("#treatments-list").querySelectorAll(".book-treatment").forEach((button) => button.addEventListener("click", () => selectTreatment(button.dataset.id)));
    const select = $("#booking-treatment");
    const previous = select.value;
    select.innerHTML = `<option value="">Elegí un tratamiento</option>${treatments.map((item) => `<option value="${escapeHTML(item.id)}">${escapeHTML(item.nombre)}</option>`).join("")}`;
    if (previous && treatments.some((item) => item.id === previous)) select.value = previous;
    if (state.bookingTreatment && treatments.some((item) => item.id === state.bookingTreatment.id)) select.value = state.bookingTreatment.id;
    updateProfessionals();
  } catch (error) {
    setStatus("#catalog-status", `No pudimos cargar el catálogo: ${error.message}`, true);
    $("#treatments-list").innerHTML = `<p class="empty-state">Revisá que los servicios estén disponibles e intentá de nuevo.</p>`;
  }
}

function updateProfessionals() {
  const selected = $("#booking-treatment").selectedOptions[0];
  const treatment = selected?.value ? window.treatments?.find((item) => item.id === selected.value) : state.bookingTreatment;
  const ids = treatment?.profesionalesIds || [];
  const select = $("#booking-professional");
  select.innerHTML = `<option value="">Elegí un profesional</option>${ids.map((id) => `<option value="${escapeHTML(id)}">Profesional ${escapeHTML(id)}</option>`).join("")}`;
  if (ids.length === 0) select.insertAdjacentHTML("beforeend", `<option value="">Consultá en recepción</option>`);
}

async function selectTreatment(id) {
  try {
    const treatment = await request(`/tratamientos/${encodeURIComponent(id)}`);
    state.bookingTreatment = treatment;
    $("#booking-treatment").value = treatment.id;
    updateProfessionals();
    document.querySelector("#reservas").scrollIntoView({ behavior: "smooth" });
    setStatus("#availability-status", `Elegiste ${treatment.nombre}. Seleccioná profesional y fecha para consultar horarios.`);
  } catch (error) { showToast(error.message, true); }
}

function setAuthMode(mode) {
  state.mode = mode;
  const register = mode === "register";
  $("#register-tab").classList.toggle("active", register);
  $("#login-tab").classList.toggle("active", !register);
  $("#register-tab").setAttribute("aria-selected", String(register));
  $("#login-tab").setAttribute("aria-selected", String(!register));
  $("#name-field").classList.toggle("hidden", !register);
  $("#referral-field").classList.toggle("hidden", !register);
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
  const body = state.mode === "register"
    ? { nombre: name, email, password, codigoReferido: $("#auth-referral").value.trim() }
    : { email, password };
  $("#auth-submit").disabled = true;
  setStatus("#auth-status", state.mode === "register" ? "Creando tu cuenta…" : "Ingresando…");
  try {
    const result = await request(path, { method: "POST", body: JSON.stringify(body) });
    if (state.mode === "register") {
      setStatus("#auth-status", "Tu cuenta está lista. Ingresá con tu email y contraseña.");
      $("#auth-password").value = "";
      setAuthMode("login");
      $("#auth-email").value = email;
      return;
    }
    sessionStorage.setItem(TOKEN_KEY, result.token);
    $("#auth-form").reset();
    await loadAccount();
    showToast("Sesión iniciada. Ya podés reservar.");
  } catch (error) { setStatus("#auth-status", error.message, true); }
  finally { $("#auth-submit").disabled = false; }
}

async function loadAccount() {
  if (!token()) return showSignedOut();
  try {
    const result = await request("/auth/me");
    const user = result.usuario;
    $("#auth-panel").classList.remove("signed-out");
    $("#profile-panel").classList.remove("hidden");
    $("#profile-name").textContent = `Hola, ${user.nombre || ""}`;
    $("#profile-email").textContent = user.email || "";
    $("#profile-initials").textContent = (user.nombre || "M").trim().slice(0, 1).toLocaleUpperCase("es-AR");
    $("#my-bookings").classList.remove("hidden");
    $("#auth-form").closest(".account-card").classList.add("hidden");
    await Promise.all([loadPoints(), loadBookings()]);
  } catch {
    sessionStorage.removeItem(TOKEN_KEY);
    showSignedOut();
  }
}

function showSignedOut() {
  $("#profile-panel").classList.add("hidden");
  $("#my-bookings").classList.add("hidden");
  $("#auth-form").closest(".account-card").classList.remove("hidden");
  $("#points-balance").textContent = "—";
}

async function loadPoints() {
  try {
    const result = await request("/beneficios/mis-puntos");
    $("#points-balance").textContent = new Intl.NumberFormat("es-AR").format(result.saldoPuntos || 0);
  } catch { $("#points-balance").textContent = "0"; }
}

async function loadBookings() {
  const list = $("#bookings-list");
  list.innerHTML = `<p class="muted">Cargando tus reservas…</p>`;
  try {
    const result = await request("/reservas/turnos");
    const bookings = result.turnos || [];
    list.innerHTML = bookings.map((booking) => {
      const date = new Intl.DateTimeFormat("es-AR", { dateStyle: "medium", timeStyle: "short" }).format(new Date(booking.inicio));
      const status = booking.estado === "confirmado" ? "Confirmado" : booking.estado;
      return `<article class="booking-row"><div><strong>${escapeHTML(booking.tratamientoId)}</strong><span>${escapeHTML(date)} · Profesional ${escapeHTML(booking.profesionalId)}</span><small>${escapeHTML(status)}</small></div>${booking.estado === "confirmado" ? `<button class="text-link cancel-booking" data-id="${escapeHTML(booking.id)}">Cancelar</button>` : ""}</article>`;
    }).join("") || `<p class="empty-state">Todavía no tenés reservas. Elegí un tratamiento y buscá un horario.</p>`;
    list.querySelectorAll(".cancel-booking").forEach((button) => button.addEventListener("click", () => cancelBooking(button.dataset.id)));
  } catch (error) { list.innerHTML = `<p class="form-status error-text">No pudimos cargar las reservas: ${escapeHTML(error.message)}</p>`; }
}

async function cancelBooking(id) {
  if (!window.confirm("¿Querés cancelar esta reserva? La política de devolución depende de la anticipación.")) return;
  try {
    await request(`/reservas/turnos/${encodeURIComponent(id)}`, { method: "DELETE" });
    showToast("La reserva fue cancelada.");
    await loadBookings();
  } catch (error) { showToast(error.message, true); }
}

async function loadAvailability(event) {
  event.preventDefault();
  if (!token()) {
    setStatus("#availability-status", "Ingresá a tu cuenta para consultar y reservar horarios.", true);
    document.querySelector("#cuenta").scrollIntoView({ behavior: "smooth" });
    return;
  }
  const treatmentId = $("#booking-treatment").value;
  const professionalId = $("#booking-professional").value;
  const date = $("#booking-date").value;
  if (!treatmentId || !professionalId || !date) return;
  const query = new URLSearchParams({ profesionalId: professionalId, tratamientoId: treatmentId, fecha: date });
  setStatus("#availability-status", "Consultando disponibilidad…");
  $("#time-slots").innerHTML = "";
  $("#payment-form").classList.add("hidden");
  try {
    const result = await request(`/reservas/disponibilidad?${query}`);
    const slots = result.horarios || [];
    setStatus("#availability-status", slots.length ? "Elegí un horario disponible. Lo reservamos por 10 minutos para completar la seña." : "No hay horarios disponibles para esa fecha. Probá otro día.");
    $("#time-slots").innerHTML = slots.map((time) => `<button class="slot-button" type="button" data-time="${escapeHTML(time)}">${escapeHTML(time)}</button>`).join("");
    $("#time-slots").querySelectorAll(".slot-button").forEach((button) => button.addEventListener("click", () => holdSlot(button.dataset.time)));
  } catch (error) { setStatus("#availability-status", error.message, true); }
}

async function holdSlot(time) {
  const date = $("#booking-date").value;
  const localStart = new Date(`${date}T${time}:00`);
  if (Number.isNaN(localStart.getTime())) return showToast("La fecha o el horario no son válidos.", true);
  try {
    const result = await request("/reservas/carrito/bloqueos", {
      method: "POST",
      body: JSON.stringify({ profesionalId: $("#booking-professional").value, tratamientoId: $("#booking-treatment").value, inicio: localStart.toISOString() })
    });
    state.heldCartId = result.bloqueo.id;
    $("#hold-message").textContent = `Horario bloqueado hasta ${new Intl.DateTimeFormat("es-AR", { timeStyle: "short" }).format(new Date(result.bloqueo.expiraEn))}. Aboná una seña de ${formatPrice(result.bloqueo.sena)} en Mercado Pago y pegá acá el ID del pago aprobado.`;
    $("#payment-form").classList.remove("hidden");
    $("#payment-id").focus();
    setStatus("#availability-status", "Horario reservado temporalmente.");
  } catch (error) { setStatus("#availability-status", error.message, true); }
}

async function confirmBooking(event) {
  event.preventDefault();
  if (!state.heldCartId) return;
  try {
    await request(`/reservas/carrito/bloqueos/${encodeURIComponent(state.heldCartId)}/confirmar`, { method: "POST", body: JSON.stringify({ pagoId: $("#payment-id").value.trim() }) });
    state.heldCartId = null;
    $("#payment-form").classList.add("hidden");
    $("#payment-form").reset();
    showToast("¡Tu turno quedó confirmado!");
    await loadBookings();
    document.querySelector("#my-bookings").scrollIntoView({ behavior: "smooth" });
  } catch (error) { setStatus("#availability-status", error.message, true); }
}

async function releaseHold() {
  if (!state.heldCartId) return;
  try {
    await request(`/reservas/carrito/bloqueos/${encodeURIComponent(state.heldCartId)}`, { method: "DELETE" });
    state.heldCartId = null;
    $("#payment-form").classList.add("hidden");
    setStatus("#availability-status", "Liberaste el horario. Podés elegir otro.");
  } catch (error) { setStatus("#availability-status", error.message, true); }
}

function initialize() {
  $("#search-form").addEventListener("submit", (event) => {
    event.preventDefault();
    state.search = $("#search-query").value.trim();
    state.category = $("#search-category").value;
    state.priceMax = $("#search-price").value;
    state.page = 1;
    loadTreatments();
  });
  $("#previous-page").addEventListener("click", () => { if (state.page > 1) { state.page--; loadTreatments(); } });
  $("#next-page").addEventListener("click", () => { if (state.hasNext) { state.page++; loadTreatments(); } });
  $("#login-tab").addEventListener("click", () => setAuthMode("login"));
  $("#register-tab").addEventListener("click", () => setAuthMode("register"));
  $("#auth-form").addEventListener("submit", submitAuth);
  $("#availability-form").addEventListener("submit", loadAvailability);
  $("#booking-treatment").addEventListener("change", updateProfessionals);
  $("#payment-form").addEventListener("submit", confirmBooking);
  $("#release-hold").addEventListener("click", releaseHold);
  $("#logout-button").addEventListener("click", () => { sessionStorage.removeItem(TOKEN_KEY); showSignedOut(); showToast("Cerraste sesión."); });
  $("#refresh-account").addEventListener("click", loadAccount);
  $("#refresh-bookings").addEventListener("click", loadBookings);
  document.querySelectorAll("[data-scroll]").forEach((button) => button.addEventListener("click", () => document.getElementById(button.dataset.scroll).scrollIntoView({ behavior: "smooth" })));
  const dateInput = $("#booking-date");
  dateInput.min = new Date(Date.now() - new Date().getTimezoneOffset() * 60000).toISOString().slice(0, 10);
  dateInput.value = dateInput.min;
  const referral = new URLSearchParams(window.location.search).get("referencia");
  if (referral) {
    setAuthMode("register");
    $("#auth-referral").value = referral;
  }
  loadTreatments();
  loadAccount();
}

initialize();
