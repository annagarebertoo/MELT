#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"
CLIENT_EMAIL="${CLIENT_EMAIL:-cliente.integracion@melt.test}"
CLIENT_PASSWORD="${CLIENT_PASSWORD:-Cliente-Integration-123!}"
ADMIN_EMAIL="${AUTH_ADMIN_EMAIL:-admin.integracion@melt.test}"
ADMIN_PASSWORD="${AUTH_ADMIN_PASSWORD:-Integration-Admin-123!}"

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

wait_for_gateway() {
  for _ in $(seq 1 60); do
    if curl --silent --fail "$BASE_URL/health" >/dev/null; then
      auth_code=$(curl --silent --output /dev/null --write-out '%{http_code}' -X POST "$BASE_URL/api/v1/auth/login" -H 'Content-Type: application/json' -d '{}')
      if [[ "$auth_code" == "400" ]]; then return 0; fi
    fi
    sleep 2
  done
  fail "el gateway o el servicio auth no respondió dentro del tiempo esperado"
}

http_code() {
  local output="$1"
  shift
  curl --silent --output "$output" --write-out '%{http_code}' "$@"
}

wait_for_gateway

# 1. Registro y login de un cliente.
register_code=$(http_code /tmp/melt-register.json -X POST "$BASE_URL/api/v1/auth/registro" \
  -H 'Content-Type: application/json' \
  -d "{\"nombre\":\"Cliente Integración\",\"email\":\"$CLIENT_EMAIL\",\"password\":\"$CLIENT_PASSWORD\"}")
[[ "$register_code" == "201" ]] || fail "registro de cliente: se esperaba 201, se obtuvo $register_code ($(cat /tmp/melt-register.json))"

login_code=$(http_code /tmp/melt-login.json -X POST "$BASE_URL/api/v1/auth/login" \
  -H 'Content-Type: application/json' \
  -d "{\"email\":\"$CLIENT_EMAIL\",\"password\":\"$CLIENT_PASSWORD\"}")
[[ "$login_code" == "200" ]] || fail "login de cliente: se esperaba 200, se obtuvo $login_code ($(cat /tmp/melt-login.json))"
TOKEN=$(jq -er '.token' /tmp/melt-login.json) || fail "login no devolvió token"

me_code=$(http_code /tmp/melt-me.json "$BASE_URL/api/v1/auth/me" -H "Authorization: Bearer $TOKEN")
[[ "$me_code" == "200" ]] || fail "GET /auth/me: se esperaba 200, se obtuvo $me_code"

# 2. Reservas debe rechazar acceso anónimo, incluso con cabeceras falsificadas.
anonymous_code=$(http_code /tmp/melt-anonymous.json "$BASE_URL/api/v1/reservas/me" \
  -H 'X-Auth-User-ID: admin-falso' -H 'X-Auth-User-Email: admin@melt.test' -H 'X-Auth-User-Roles: superadministrador')
[[ "$anonymous_code" == "401" ]] || fail "Reservas anónimo/spoof: se esperaba 401, se obtuvo $anonymous_code"

reservas_code=$(http_code /tmp/melt-reservas.json "$BASE_URL/api/v1/reservas/me" \
  -H "Authorization: Bearer $TOKEN" -H 'X-Auth-User-ID: admin-falso' -H 'X-Auth-User-Email: admin@melt.test' -H 'X-Auth-User-Roles: superadministrador')
[[ "$reservas_code" == "200" ]] || fail "Reservas autenticado: se esperaba 200, se obtuvo $reservas_code"
actual_id=$(jq -er '.usuario.id' /tmp/melt-reservas.json)
[[ "$actual_id" != "admin-falso" ]] || fail "el gateway permitió falsificar X-Auth-User-ID"

# 3. El cliente puede consultar puntos; sin sesión no puede.
points_anon_code=$(http_code /tmp/melt-points-anon.json "$BASE_URL/api/v1/beneficios/mis-puntos")
[[ "$points_anon_code" == "401" ]] || fail "Beneficios anónimo: se esperaba 401, se obtuvo $points_anon_code"
points_code=$(http_code /tmp/melt-points.json "$BASE_URL/api/v1/beneficios/mis-puntos" -H "Authorization: Bearer $TOKEN")
[[ "$points_code" == "200" ]] || fail "puntos de cliente: se esperaba 200, se obtuvo $points_code"
[[ "$(jq -er '.saldoPuntos' /tmp/melt-points.json)" == "0" ]] || fail "saldo inicial inesperado para cliente nuevo"

# 4. El rol administrador no debe acceder a la consulta exclusiva de clientes.
admin_login_code=$(http_code /tmp/melt-admin-login.json -X POST "$BASE_URL/api/v1/auth/login" \
  -H 'Content-Type: application/json' \
  -d "{\"email\":\"$ADMIN_EMAIL\",\"password\":\"$ADMIN_PASSWORD\"}")
[[ "$admin_login_code" == "200" ]] || fail "login de superadministrador: se esperaba 200, se obtuvo $admin_login_code"
ADMIN_TOKEN=$(jq -er '.token' /tmp/melt-admin-login.json) || fail "login de superadministrador no devolvió token"
admin_points_code=$(http_code /tmp/melt-points-admin.json "$BASE_URL/api/v1/beneficios/mis-puntos" -H "Authorization: Bearer $ADMIN_TOKEN")
[[ "$admin_points_code" == "403" ]] || fail "Beneficios debe denegar rol administrador con 403, obtuvo $admin_points_code"

echo "OK: registro/login, /me, protección de Reservas, identidad no falsificable, consulta de puntos y autorización por rol."
