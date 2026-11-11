"""Ejemplo servidor a servidor; solo biblioteca estándar. Configurar secretos por entorno."""
import json
import os
import urllib.error
import urllib.request


def acreditar(email, puntos, referencia):
    base = os.environ.get("MELT_API_URL", "https://melt-gateway-prueba.onrender.com/api/v1").rstrip("/")
    body = {"email": email, "puntos": puntos, "referencia": referencia}
    request = urllib.request.Request(base + "/beneficios/acreditaciones", data=json.dumps(body).encode(), headers={"Content-Type": "application/json", "X-Partner-Key": os.environ["MELT_PARTNER_KEY"]}, method="POST")
    try:
        with urllib.request.urlopen(request, timeout=90) as response:
            return response.status, json.load(response)
    except urllib.error.HTTPError as error:
        return error.code, json.load(error)
    # Ante timeout/error de red, el llamador conserva referencia y datos para reintentar.


if __name__ == "__main__":
    status, result = acreditar(os.environ["MELT_EMAIL"], int(os.environ["MELT_PUNTOS"]), os.environ["MELT_REFERENCIA"])
    # El backend puede usar result["registroUrl"] para entregarlo PRIVADAMENTE.
    safe = {k: v for k, v in result.items() if k != "registroUrl"}
    if "registroUrl" in result:
        safe["registroUrl"] = "[enlace privado recibido; no registrar]"
    print(json.dumps({"http": status, "respuesta": safe}, ensure_ascii=False))
