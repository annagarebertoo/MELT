"""Prueba real contra el entorno indicado explícitamente. Crea datos sintéticos."""
import json
import os
import secrets
import urllib.error
import urllib.request

BASE = os.environ['BASE_URL'].rstrip('/')
KEY = os.environ['BENEFICIOS_PARTNER_KEY']
REF = 'e2e-' + secrets.token_hex(8)
rows = []


def call(name, path, expected, body=None, headers=None, host=None):
    h = {'Content-Type': 'application/json'}
    h.update(headers or {})
    if h.get('Content-Type') is None:
        del h['Content-Type']
    data = None if body is None else json.dumps(body).encode()
    req = urllib.request.Request((host or BASE) + path, data=data, headers=h)
    try:
        with urllib.request.urlopen(req, timeout=90) as r:
            status, raw = r.status, r.read().decode()
    except urllib.error.HTTPError as e:
        status, raw = e.code, e.read().decode()
    try:
        result = json.loads(raw)
    except ValueError:
        result = {'respuesta': raw[:300]}
    safe = {k: v for k, v in result.items() if k not in ('token', 'registroUrl')}
    rows.append({'prueba': name, 'http': status, 'esperado': expected,
                 'estado': 'aprobada' if status == expected else 'fallida', 'respuesta': safe})
    print(name, status, flush=True)
    assert status == expected, (name, status, safe)
    return result


partner = {'X-Partner-Key': KEY}
email = REF + '@melt.test'
password = secrets.token_urlsafe(24)
account = call('Registro', '/api/v1/auth/registro', 201,
               {'nombre': 'Prueba Render', 'email': email, 'password': password})
session = call('Login', '/api/v1/auth/login', 200, {'email': email, 'password': password})
auth = {'Authorization': 'Bearer ' + session['token']}
uid = account['usuario']['id']
initial = call('Saldo inicial', '/api/v1/beneficios/mis-puntos', 200, headers=auth)['saldoPuntos']
operation = {'solicitudId': REF, 'usuarioId': uid, 'puntos': 200}
call('Acreditar por ID', '/api/v1/beneficios/acreditar-puntos', 201, operation, partner)
call('Repetición exacta', '/api/v1/beneficios/acreditar-puntos', 200, operation, partner)
call('Referencia en conflicto', '/api/v1/beneficios/acreditar-puntos', 409, {**operation, 'puntos': 201}, partner)
call('Sin clave', '/api/v1/beneficios/acreditar-puntos', 401, operation)
call('Clave incorrecta', '/api/v1/beneficios/acreditar-puntos', 401, operation, {'X-Partner-Key': 'incorrecta'})
call('Usuario inexistente', '/api/v1/beneficios/acreditar-puntos', 404,
     {**operation, 'solicitudId': REF+'-unknown', 'usuarioId': '000000000000000000000000'}, partner)
call('Mayúscula PUNTOS', '/api/v1/beneficios/acreditar-puntos', 400,
     {'solicitudId': REF+'-upper', 'usuarioId': uid, 'PUNTOS': 200}, partner)
call('Content-Type incorrecto', '/api/v1/beneficios/acreditar-puntos', 415, operation,
     {**partner, 'Content-Type': 'text/plain'})
call('Sin Content-Type JSON', '/api/v1/beneficios/acreditar-puntos', 415, operation,
     {**partner, 'Content-Type': None})
call('Referencia 129 caracteres', '/api/v1/beneficios/acreditar-puntos', 400,
     {**operation, 'solicitudId': 'x'*129}, partner)
spoof = {'X-Auth-User-Id': uid, 'X-Auth-User-Role': 'cliente'}
call('Headers falsos en Gateway', '/api/v1/beneficios/mis-puntos', 401, headers=spoof)
call('Bearer inválido', '/api/v1/beneficios/mis-puntos', 401, headers={'Authorization': 'Bearer falso'})
call('Headers falsos directo a Beneficios', '/mis-puntos', 401,
     headers=spoof, host=os.environ['BENEFICIOS_URL'])
final = call('Saldo final sin duplicados', '/api/v1/beneficios/mis-puntos', 200, headers=auth)
assert final['saldoPuntos'] == initial + 200
assert len(final['movimientos']) == 1
report = {'base': BASE, 'saldoInicial': initial, 'saldoFinal': final['saldoPuntos'], 'resultados': rows}
with open(os.environ['REPORT_PATH'], 'w', encoding='utf-8') as f:
    json.dump(report, f, ensure_ascii=False, indent=2)
print('APROBADAS', len(rows))
