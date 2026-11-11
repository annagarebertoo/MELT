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

echo "OK: registro sin acreditación automática, login, /me, protección de Reservas, identidad no falsificable y permisos."

# Acreditación externa por gateway; registro y login no acreditan puntos.
USER_ID=$(jq -er '.usuario.id' /tmp/melt-register.json)
: "${BENEFICIOS_PARTNER_KEY:?Definí la clave externa para probar acreditaciones}"
for spec in "op-ci-1 200 201" "op-ci-1 200 200" "op-ci-1 201 409" "op-ci-2 50 201"; do
  read -r op points expected <<< "$spec"
  code=$(http_code /tmp/melt-credit.json -X POST "$BASE_URL/api/v1/beneficios/acreditar-puntos" -H 'Content-Type: application/json' -H "X-Partner-Key: $BENEFICIOS_PARTNER_KEY" -d "{\"solicitudId\":\"$op\",\"usuarioId\":\"$USER_ID\",\"puntos\":$points}")
  [[ "$code" == "$expected" ]] || fail "acreditación: esperado $expected, recibido $code ($(cat /tmp/melt-credit.json))"
done
code=$(http_code /tmp/melt-credit.json -X POST "$BASE_URL/api/v1/beneficios/acreditar-puntos" -H 'Content-Type: application/json' -d '{}')
[[ "$code" == "401" ]] || fail "acreditación sin clave debe devolver 401"
http_code /tmp/melt-final.json "$BASE_URL/api/v1/beneficios/mis-puntos" -H "Authorization: Bearer $TOKEN" >/dev/null
[[ "$(jq -er '.saldoPuntos' /tmp/melt-final.json)" == "250" ]] || fail "saldo acumulado incorrecto"
for path in auth/internal/usuarios/$USER_ID beneficios/internal/registro-gym; do
  code=$(http_code /tmp/melt-internal.json "$BASE_URL/api/v1/$path")
  [[ "$code" == "404" ]] || fail "el gateway publicó una ruta interna"
done
echo "OK: acreditación externa, acumulación, idempotencia y rutas internas."

# Validación contractual estricta del POST.
code=$(http_code /tmp/melt-case.json -X POST "$BASE_URL/api/v1/beneficios/acreditar-puntos" -H 'Content-Type: application/json' -H "X-Partner-Key: $BENEFICIOS_PARTNER_KEY" -d "{\"solicitudId\":\"case-invalid\",\"usuarioId\":\"$USER_ID\",\"PUNTOS\":7}")
[[ "$code" == "400" ]] || fail "PUNTOS no debe sustituir puntos"
code=$(http_code /tmp/melt-content-type.json -X POST "$BASE_URL/api/v1/beneficios/acreditar-puntos" -H 'Content-Type: text/plain' -H "X-Partner-Key: $BENEFICIOS_PARTNER_KEY" -d "{\"solicitudId\":\"type-invalid\",\"usuarioId\":\"$USER_ID\",\"puntos\":7}")
[[ "$code" == "415" ]] || fail "Content-Type incorrecto debe devolver 415"
UNICODE_ID=$(jq -nr '"\u00e9" * 100')
UNICODE_BODY=$(jq -nc --arg id "$UNICODE_ID" --arg user "$USER_ID" '{solicitudId:$id,usuarioId:$user,puntos:1}')
for expected in 201 200; do
  code=$(http_code /tmp/melt-unicode.json -X POST "$BASE_URL/api/v1/beneficios/acreditar-puntos" -H 'Content-Type: application/json; charset=utf-8' -H "X-Partner-Key: $BENEFICIOS_PARTNER_KEY" -d "$UNICODE_BODY")
  [[ "$code" == "$expected" ]] || fail "ID Unicode de 100 caracteres: esperado $expected, obtenido $code"
done

# Opcional: ejecutar desde la red Docker para comprobar el servicio sin gateway.
if [[ -n "${BENEFICIOS_DIRECT_URL:-}" ]]; then
  for token in "" "Bearer falso"; do
    code=$(http_code /tmp/melt-direct-anon.json "$BENEFICIOS_DIRECT_URL/mis-puntos" -H "Authorization: $token" -H "X-Auth-User-ID: $USER_ID" -H 'X-Auth-User-Email: falso@melt.test' -H 'X-Auth-User-Roles: cliente')
    [[ "$code" == "401" ]] || fail "Beneficios directo aceptó identidad falsificada"
  done
  code=$(http_code /tmp/melt-direct.json "$BENEFICIOS_DIRECT_URL/mis-puntos" -H "Authorization: Bearer $TOKEN" -H 'X-Auth-User-ID: victima-falsa' -H 'X-Auth-User-Email: falso@melt.test' -H 'X-Auth-User-Roles: administrador')
  [[ "$code" == "200" ]] || fail "Beneficios directo rechazó Bearer válido"
  [[ "$(jq -er '.usuarioId' /tmp/melt-direct.json)" == "$USER_ID" ]] || fail "Beneficios confió en el ID falsificado"
  [[ "$(jq -er '.saldoPuntos' /tmp/melt-direct.json)" == "251" ]] || fail "saldo incorrecto tras Unicode y reintento"
  code=$(http_code /tmp/melt-direct-admin.json "$BENEFICIOS_DIRECT_URL/mis-puntos" -H "Authorization: Bearer $ADMIN_TOKEN" -H 'X-Auth-User-Roles: cliente')
  [[ "$code" == "403" ]] || fail "Beneficios permitió alterar el rol con headers"
fi
echo "OK: contrato estricto, Unicode y seguridad del acceso directo cuando se configura BENEFICIOS_DIRECT_URL."
