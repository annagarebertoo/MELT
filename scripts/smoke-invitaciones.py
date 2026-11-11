#!/usr/bin/env python3
"""E2E en un entorno MELT de pruebas limpio. No imprime claves ni tokens."""
import json
import os
import secrets
import urllib.error
import urllib.parse
import urllib.request

BASE = os.environ.get('BASE_URL', 'http://localhost:8080').rstrip('/') + '/api/v1'
KEY = os.environ['BENEFICIOS_PARTNER_KEY']
PREFIX = 'inv-' + secrets.token_hex(5)
checks = []


def call(name, path, body=None, *, expected=200, partner=False, bearer=None):
    headers = {'Content-Type': 'application/json'}
    if partner:
        headers['X-Partner-Key'] = KEY
    if bearer:
        headers['Authorization'] = 'Bearer ' + bearer
    req = urllib.request.Request(BASE + path, data=None if body is None else json.dumps(body).encode(), headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=15) as response:
            status, raw = response.status, response.read().decode()
    except urllib.error.HTTPError as error:
        status, raw = error.code, error.read().decode()
    try:
        data = json.loads(raw)
    except json.JSONDecodeError:
        data = None
    if status != expected:
        # Nunca imprimir cuerpos de login ni enlaces de invitación.
        code = data.get('error', {}).get('code') if isinstance(data, dict) else None
        raise AssertionError(f'{name}: HTTP {status}, esperado {expected}, error={code}')
    checks.append({'prueba': name, 'http': status})
    return data


def link_token(result):
    link = urllib.parse.urlsplit(result['registroUrl'])
    assert not link.query and result['estado'] == 'pendiente'
    return urllib.parse.parse_qs(link.fragment)['invitacion'][0]


for amount in (100, 500, 137):
    email = f'{PREFIX}-{amount}@melt.test'
    operation = {'referencia': f'{PREFIX}-{amount}', 'email': email, 'puntos': amount, 'descripcion': 'Prueba de invitación'}
    pending = call(f'Pendiente {amount}', '/beneficios/acreditaciones', operation, expected=202, partner=True)
    token = link_token(pending)
    assert call('Reintento pendiente', '/beneficios/acreditaciones', operation, expected=202, partner=True) == pending
    call('Referencia en conflicto', '/beneficios/acreditaciones', {**operation, 'puntos': amount + 1}, expected=409, partner=True)
    call('Consulta enlace', '/beneficios/invitaciones/consultar', {'token': token})
    password = secrets.token_urlsafe(20)
    account = call(f'Registro desde enlace {amount}', '/auth/registro', {'nombre': 'Invitación E2E', 'email': email, 'password': password, 'invitacionToken': token}, expected=201)
    assert account['usuario']['emailVerificado']
    session = call('Login', '/auth/login', {'email': email, 'password': password})
    bearer = session['token']
    call('Confirmación sin sesión', '/beneficios/invitaciones/confirmar', {'token': token}, expected=401)
    call('Confirmación o recuperación', '/beneficios/invitaciones/confirmar', {'token': token}, expected=200, bearer=bearer)
    balance = call('Saldo después del registro', '/beneficios/mis-puntos', bearer=bearer)
    assert balance['saldoPuntos'] == amount and len(balance['movimientos']) == 1
    credited = call('Repetición ya acreditada', '/beneficios/acreditaciones', operation, partner=True)
    assert credited['usuarioId'] == account['usuario']['id'] and credited['estado'] == 'acreditada'
    extra = {'solicitudId': f'{PREFIX}-extra-{amount}', 'email': email, 'puntos': 7}
    call('Cuenta existente verificada: inmediata', '/beneficios/acreditar-puntos', extra, expected=201, partner=True)
    balance = call('Saldo acumulado', '/beneficios/mis-puntos', bearer=bearer)
    assert balance['saldoPuntos'] == amount + 7 and len(balance['movimientos']) == 2
    call('Pendiente sin autorización', '/beneficios/acreditaciones', operation, expected=401)

# Una cuenta normal no puede conseguir puntos por el solo hecho de declarar un email.
email = PREFIX + '-existente@melt.test'
password = secrets.token_urlsafe(20)
call('Cuenta normal previa', '/auth/registro', {'nombre': 'Existente', 'email': email, 'password': password}, expected=201)
operation = {'referencia': PREFIX + '-existente', 'email': email, 'puntos': 500}
pending = call('Cuenta sin email verificado: enlace', '/beneficios/acreditaciones', operation, expected=202, partner=True)
token = link_token(pending)
session = call('Login cuenta previa', '/auth/login', {'email': email, 'password': password})
assert call('Antes de vincular', '/beneficios/mis-puntos', bearer=session['token'])['saldoPuntos'] == 0
call('Vincular cuenta existente', '/beneficios/invitaciones/confirmar', {'token': token}, expected=201, bearer=session['token'])
assert call('Después de vincular', '/beneficios/mis-puntos', bearer=session['token'])['saldoPuntos'] == 500
call('Repetir vínculo', '/beneficios/invitaciones/confirmar', {'token': token}, bearer=session['token'])
assert call('Saldo sin duplicados', '/beneficios/mis-puntos', bearer=session['token'])['saldoPuntos'] == 500
call('Ruta interna Auth bloqueada', '/auth/internal/usuarios/buscar', {'email': email}, expected=404)
call('Ruta interna Beneficios bloqueada', '/beneficios/internal/invitaciones/confirmar', {}, expected=404)
print(json.dumps({'aprobadas': len(checks), 'resultados': checks}, ensure_ascii=False))
