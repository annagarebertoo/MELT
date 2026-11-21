"""Static gateway regression checks; no network, secrets or production calls."""
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
LOCAL = ROOT / "infra/nginx/default.conf"
RENDER = ROOT / "infra/nginx/default.render.conf"


class GatewaySafetyTests(unittest.TestCase):
    def test_private_auth_verify_is_not_public(self):
        for config in (LOCAL, RENDER):
            with self.subTest(config=config.name):
                text = config.read_text(encoding="utf-8")
                self.assertIn("location = /api/v1/auth/verify", text)
                self.assertIn("return 404;", text.split("location = /api/v1/auth/verify", 1)[1].split("}", 1)[0])

    def test_private_service_prefixes_are_blocked(self):
        for config in (LOCAL, RENDER):
            text = config.read_text(encoding="utf-8")
            for prefix in ("auth/internal/", "beneficios/internal/", "catalogo/internal/"):
                with self.subTest(config=config.name, prefix=prefix):
                    self.assertIn("location ^~ /api/v1/" + prefix, text)

    def test_catalogue_retry_is_bounded_and_safe(self):
        text = LOCAL.read_text(encoding="utf-8")
        self.assertIn("max_fails=1 fail_timeout=10s", text)
        self.assertIn("proxy_next_upstream_tries 2;", text)
        self.assertIn("proxy_next_upstream error timeout http_502 http_503 http_504;", text)
        self.assertNotIn("proxy_next_upstream non_idempotent", text)


if __name__ == "__main__":
    unittest.main()
