(() => {
  const API = "/api/v1/beneficios/ghostkitchen";
  const state = { lat: null, lng: null, sede: null, page: 1, pageSize: 20, totalPages: 1 };
  const $ = (selector) => document.querySelector(selector);
  const escapeHTML = (value) => String(value ?? "").replace(/[&<>"']/g, (char) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[char]);
  const money = (value) => new Intl.NumberFormat("es-AR", { style: "currency", currency: "ARS", maximumFractionDigits: 0 }).format(Number(value || 0));
  const setStatus = (message, isError = false) => { const element = $("#ghostkitchen-status"); element.textContent = message; element.classList.toggle("error-text", isError); };

  async function request(path, options = {}) {
    const headers = new Headers(options.headers || {});
    const session = sessionStorage.getItem("melt.session");
    if (session) headers.set("Authorization", `Bearer ${session}`);
    if (options.body) headers.set("Content-Type", "application/json");
    const response = await fetch(`${API}${path}`, { ...options, headers });
    const type = response.headers.get("content-type") || "";
    const payload = type.includes("json") ? await response.json() : await response.text();
    if (!response.ok) {
      const error = new Error(payload?.detail || payload?.title || payload?.error?.message || payload?.message || `No pudimos consultar GhostKitchen (${response.status}).`);
      error.code = payload?.code || payload?.error?.code || "";
      error.correlationId = payload?.correlationId || payload?.error?.correlationId || response.headers.get("X-Correlation-ID") || "";
      error.status = response.status;
      throw error;
    }
    return payload;
  }

  function showFailure(error) {
    if (error.code === "SIN_COBERTURA") {
      setStatus("Todavía no hay cocinas GhostKitchen dentro de los 25 km de esta ubicación. Podés probar con otras coordenadas.", true);
    } else {
      const correlation = error.correlationId ? ` (referencia: ${error.correlationId})` : "";
      setStatus(`${error.message}${correlation}`, true);
    }
    $("#ghostkitchen-sede").innerHTML = "";
    $("#ghostkitchen-filters").classList.add("hidden");
    $("#ghostkitchen-results").innerHTML = "";
    $("#ghostkitchen-pagination").classList.add("hidden");
  }

  function renderSede(result) {
    const sede = result.sede || {};
    state.sede = sede;
    const delivery = sede.tiempoEntregaMin != null && sede.tiempoEntregaMax != null
      ? `${escapeHTML(sede.tiempoEntregaMin)}–${escapeHTML(sede.tiempoEntregaMax)} min`
      : "A confirmar";
    const restaurants = Array.isArray(sede.restaurantes) ? sede.restaurantes : [];
    $("#ghostkitchen-sede").innerHTML = `<article class="ghostkitchen-sede-card">
      <div><p class="eyebrow">La más cercana · ${escapeHTML(result.distanciaKm ?? "—")} km</p><h3>${escapeHTML(sede.nombre || "Cocina GhostKitchen")}</h3><p>${escapeHTML(sede.direccion || "")} · ${escapeHTML(sede.localidad || "")}</p></div>
      <div class="ghostkitchen-sede-meta"><span>Entrega estimada<strong>${delivery}</strong></span><span>Envío<strong>${money(sede.costoEnvio)}</strong></span></div>
      <div class="ghostkitchen-restaurants"><strong>Restaurantes en esta sede</strong><div>${restaurants.map((item) => `<span>${escapeHTML(item.nombre)}</span>`).join("") || "<span>Sin restaurantes informados</span>"}</div></div>
    </article>`;
    const select = $("#ghostkitchen-restaurant");
    select.innerHTML = `<option value="">Todos los restaurantes</option>${restaurants.map((item) => `<option value="${escapeHTML(item.id)}">${escapeHTML(item.nombre)}</option>`).join("")}`;
    $("#ghostkitchen-filters").classList.remove("hidden");
  }

  async function loadDishes() {
    if (!state.sede?.id) return;
    const form = $("#ghostkitchen-filters");
    const values = new FormData(form);
    const params = new URLSearchParams({ page: String(state.page), pageSize: String(state.pageSize) });
    for (const key of ["q", "categoria", "etiqueta", "restauranteId", "precioMax", "orden"]) {
      const value = String(values.get(key) || "").trim();
      if (value) params.set(key, value);
    }
    params.set("soloDisponibles", form.elements.soloDisponibles.checked ? "true" : "false");
    $("#ghostkitchen-results").innerHTML = `<p class="featured-loading">Cargando platos…</p>`;
    try {
      const result = await request(`/sedes/${encodeURIComponent(state.sede.id)}/platos?${params}`);
      const items = Array.isArray(result.items) ? result.items : [];
      state.totalPages = Number(result.totalPages || 1);
      $("#ghostkitchen-results").innerHTML = items.map((item) => `<article class="ghostkitchen-dish-card">
        <p class="eyebrow">${escapeHTML(item.categoria || "Plato")}</p><h3>${escapeHTML(item.nombre)}</h3><p>${escapeHTML(item.descripcion || "")}</p>
        <div class="ghostkitchen-dish-meta"><strong>${money(item.precio)}</strong><span class="${item.disponible ? "is-available" : "is-unavailable"}">${item.disponible ? "Disponible ahora" : "No disponible ahora"}</span></div>
        <div class="detail-keywords">${(Array.isArray(item.etiquetas) ? item.etiquetas : []).map((tag) => `<span>${escapeHTML(tag)}</span>`).join("")}</div>
        <small>${escapeHTML(item.restaurante?.nombre || "Restaurante")}</small><button class="text-link ghostkitchen-detail-button" type="button" data-plato-id="${escapeHTML(item.id)}">Ver detalle ↗</button>
      </article>`).join("") || `<p class="empty-state">No encontramos platos con esos filtros. Probá con otra búsqueda.</p>`;
      $("#ghostkitchen-page").textContent = `Página ${Number(result.page || state.page)} de ${state.totalPages}`;
      $("#ghostkitchen-prev").disabled = state.page <= 1;
      $("#ghostkitchen-next").disabled = state.page >= state.totalPages;
      $("#ghostkitchen-pagination").classList.toggle("hidden", state.totalPages <= 1);
      $("#ghostkitchen-results").querySelectorAll(".ghostkitchen-detail-button").forEach((button) => button.addEventListener("click", () => loadDishDetail(button.dataset.platoId)));
      const correlation = result.correlationId ? ` · ref. ${result.correlationId}` : "";
      setStatus(`${Number(result.total || items.length)} platos en ${state.sede.nombre || "la sede cercana"}${correlation}.`);
    } catch (error) {
      $("#ghostkitchen-results").innerHTML = `<p class="empty-state">No pudimos cargar los platos. Podés volver a intentar.</p>`;
      const correlation = error.correlationId ? ` (referencia: ${error.correlationId})` : "";
      setStatus(`${error.message}${correlation}`, true);
    }
  }

  async function loadDishDetail(id) {
    try {
      const item = await request(`/platos/${encodeURIComponent(id)}`);
      $("#ghostkitchen-detail-title").textContent = item.nombre || "Detalle del plato";
      $("#ghostkitchen-detail-restaurant").textContent = item.restaurante?.nombre || item.sede?.nombre || "GhostKitchen";
      $("#ghostkitchen-detail-description").textContent = item.descripcion || "";
      $("#ghostkitchen-detail-price").textContent = money(item.precio);
      $("#ghostkitchen-detail-availability").textContent = item.disponible ? "Disponible ahora*" : "No disponible ahora";
      $("#ghostkitchen-detail-tags").innerHTML = (Array.isArray(item.etiquetas) ? item.etiquetas : []).map((tag) => `<span>${escapeHTML(tag)}</span>`).join("");
      $("#ghostkitchen-detail-updated").textContent = item.actualizadoEn ? `Actualizado: ${new Date(item.actualizadoEn).toLocaleString("es-AR")}. *La disponibilidad es orientativa.` : "*La disponibilidad es orientativa y no garantiza una reserva ni una compra.";
      $("#ghostkitchen-detail-dialog").showModal();
    } catch (error) {
      const correlation = error.correlationId ? ` (referencia: ${error.correlationId})` : "";
      setStatus(`${error.message}${correlation}`, true);
    }
  }

  async function findNearest(lat, lng) {
    state.lat = Number(lat); state.lng = Number(lng);
    const params = new URLSearchParams({ lat: String(state.lat), lng: String(state.lng) });
    $("#ghostkitchen-sede").innerHTML = "";
    $("#ghostkitchen-results").innerHTML = "";
    $("#ghostkitchen-filters").classList.add("hidden");
    $("#ghostkitchen-pagination").classList.add("hidden");
    setStatus("Buscando la cocina más cercana…");
    try {
      const result = await request(`/sedes/cercana?${params}`);
      if (!result.sede?.id) throw new Error("GhostKitchen no informó una sede cercana.");
      renderSede(result);
      state.page = 1;
      await loadDishes();
    } catch (error) { showFailure(error); }
  }

  function locate() {
    if (!navigator.geolocation) {
      setStatus("Este navegador no ofrece ubicación automática. Ingresá latitud y longitud manualmente.", true);
      $("#ghostkitchen-lat").focus();
      return;
    }
    setStatus("Solicitando tu ubicación…");
    navigator.geolocation.getCurrentPosition((position) => {
      const { latitude, longitude } = position.coords;
      $("#ghostkitchen-lat").value = latitude;
      $("#ghostkitchen-lng").value = longitude;
      findNearest(latitude, longitude);
    }, () => {
      setStatus("No pudimos acceder a tu ubicación. Ingresá latitud y longitud manualmente para continuar.", true);
      $("#ghostkitchen-lat").focus();
    }, { enableHighAccuracy: false, timeout: 10000, maximumAge: 60000 });
  }

  document.addEventListener("DOMContentLoaded", () => {
    $("#profile-ghostkitchen").addEventListener("click", () => $("#ghostkitchen-panel").scrollIntoView({ behavior: "smooth", block: "start" }));
    $("#ghostkitchen-geolocate").addEventListener("click", locate);
    $("#ghostkitchen-location-form").addEventListener("submit", (event) => {
      event.preventDefault();
      const lat = Number($("#ghostkitchen-lat").value), lng = Number($("#ghostkitchen-lng").value);
      if (!Number.isFinite(lat) || lat < -90 || lat > 90 || !Number.isFinite(lng) || lng < -180 || lng > 180) {
        setStatus("Ingresá una latitud entre -90 y 90 y una longitud entre -180 y 180.", true); return;
      }
      findNearest(lat, lng);
    });
    $("#ghostkitchen-filters").addEventListener("submit", (event) => { event.preventDefault(); state.page = 1; loadDishes(); });
    $("#ghostkitchen-prev").addEventListener("click", () => { if (state.page > 1) { state.page -= 1; loadDishes(); } });
    $("#ghostkitchen-next").addEventListener("click", () => { if (state.page < state.totalPages) { state.page += 1; loadDishes(); } });
    $("#ghostkitchen-retry").addEventListener("click", () => state.lat == null ? locate() : findNearest(state.lat, state.lng));
  });
})();
