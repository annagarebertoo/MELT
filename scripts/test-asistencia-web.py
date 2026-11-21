"""Prueba la interfaz publicada en el repositorio contra respuestas de API aisladas."""
from datetime import datetime, timezone
from pathlib import Path
from urllib.parse import urlsplit

from playwright.sync_api import expect, sync_playwright


def main():
    web = Path(__file__).resolve().parents[1] / "infra/nginx/web"
    turn = dict(id="turno-prueba", estado="confirmado", clienteNombre="Cliente de prueba",
                tratamientoId="facial", inicio="2026-11-23T13:00:00Z", fin="2026-11-23T14:00:00Z")
    writes = []

    def respond(route):
        path = urlsplit(route.request.url).path
        if path == "/api/v1/reservas/turnos/turno-prueba/no-asistio":
            writes.append(route.request.method)
            turn["estado"] = "no_asistio"
            route.fulfill(json={"turno": turn})
        elif path == "/api/v1/reservas/turnos":
            route.fulfill(json={"turnos": [turn]})
        elif path == "/api/v1/auth/me":
            route.fulfill(json={"usuario": {"nombre": "Profesional", "roles": ["profesional"]}})
        elif path.startswith("/api/v1/"):
            route.fulfill(json={"nombre": "Facial", "dias": [], "items": []})
        else:
            asset = web / (path.lstrip("/") or "index.html")
            if asset.is_file():
                route.fulfill(path=asset)
            else:
                route.fulfill(status=404)

    with sync_playwright() as pw:
        browser = pw.chromium.launch()
        page = browser.new_page()
        page.route("http://melt.test/**", respond)
        page.route("https://**/*", lambda route: route.abort())
        page.add_init_script("sessionStorage.setItem('melt.session', 'sesion-ficticia')")
        page.clock.install(time=datetime(2026, 11, 23, 13, 59, tzinfo=timezone.utc))
        page.goto("http://melt.test/#/cuenta")
        button = page.get_by_role("button", name="Marcar no asistió")
        expect(button).to_be_disabled()
        page.clock.fast_forward(61000)
        expect(button).to_be_enabled()
        page.once("dialog", lambda dialog: dialog.dismiss())
        button.click()
        assert not writes
        page.once("dialog", lambda dialog: dialog.accept())
        button.click()
        history = page.locator("#professional-history")
        expect(history.get_by_text("No asistió", exact=True)).to_be_visible()
        expect(button).to_have_count(0)
        page.reload()
        expect(history.get_by_text("No asistió", exact=True)).to_be_visible()
        expect(history.get_by_role("button")).to_have_count(0)
        assert writes == ["POST"], writes
        browser.close()


if __name__ == "__main__":
    main()
