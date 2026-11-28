#!/usr/bin/env bash
# Mide la cobertura de pruebas (unitarias + integración contra dependencias reales
# descartables) de cada servicio Go y falla si alguno queda por debajo del umbral.
#
#   scripts/cobertura.sh [--umbral 60] [--keep]
#
# Levanta PostgreSQL, RabbitMQ, MongoDB, Memcached y OpenSearch con compose.test.yaml
# (datos en tmpfs, nada persiste). Nunca usa Atlas, Supabase ni Render.
# Resultado: tabla en consola y output/cobertura/resumen.md; perfiles por servicio
# en output/cobertura/<servicio>.out (abrir con `go tool cover -html=...`).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMPOSE=(docker compose -f "$ROOT/compose.test.yaml" -p melt-test)
UMBRAL=60
KEEP=false
while [[ $# -gt 0 ]]; do
  case "$1" in
    --umbral) UMBRAL="$2"; shift 2 ;;
    --keep) KEEP=true; shift ;;
    *) echo "uso: $0 [--umbral N] [--keep]" >&2; exit 2 ;;
  esac
done

limpiar() {
  if [[ "$KEEP" == false ]]; then
    "${COMPOSE[@]}" down -v --remove-orphans >/dev/null 2>&1 || true
  fi
}
trap limpiar EXIT

echo "==> Levantando dependencias descartables"
"${COMPOSE[@]}" up -d --wait postgres rabbitmq mongo memcached opensearch

# Base aparte con el esquema aplicado para la prueba de asistencia de Reservas.
"${COMPOSE[@]}" exec -T postgres psql -q -U postgres -c "drop database if exists reservas_asistencia" >/dev/null
"${COMPOSE[@]}" exec -T postgres psql -q -U postgres -c "create database reservas_asistencia" >/dev/null
"${COMPOSE[@]}" exec -T postgres psql -q -U postgres -d reservas_asistencia -v ON_ERROR_STOP=1 < "$ROOT/infra/supabase/reservas.sql" >/dev/null

MONGO="mongodb://127.0.0.1:55017/?directConnection=true&replicaSet=melt-test"
export MELT_TEST_POSTGRES_URL="postgres://postgres:melt-test@127.0.0.1:55432/postgres?sslmode=disable"
export RESERVAS_TEST_DATABASE_URL="postgres://postgres:melt-test@127.0.0.1:55432/reservas_asistencia?sslmode=disable"
export MELT_TEST_RABBITMQ_URL="amqp://guest:guest@127.0.0.1:55672/"
export MELT_TEST_RABBITMQ_API="http://guest:guest@127.0.0.1:55673"
export CATALOGO_TEST_URI="$MONGO" AUTH_TEST_MONGODB_URI="$MONGO" BENEFICIOS_TEST_MONGODB_URI="$MONGO"
export MONGODB_TEST_URI="$MONGO" RESERVAS_TEST_MONGODB_URI="$MONGO" PAGOS_TEST_MONGODB_URI="$MONGO"
export CATALOGO_TEST_OPENSEARCH_URL="http://127.0.0.1:59200" CATALOGO_TEST_MEMCACHED_ADDRESS="127.0.0.1:55211"
export GIN_MODE=release

SALIDA="$ROOT/output/cobertura"
mkdir -p "$SALIDA"
RESUMEN="$SALIDA/resumen.md"
{
  echo "| Servicio | Cobertura (unitarias + integración) | Umbral |"
  echo "|---|---|---|"
} > "$RESUMEN"

fallo=0
for servicio in auth beneficios catalogo-profesionales pagos reservas; do
  tags=""
  [[ "$servicio" == catalogo-profesionales ]] && tags="-tags=catalogointegration,catalogosearchintegration"
  echo "==> $servicio"
  perfil="$SALIDA/$servicio.out"
  (cd "$ROOT/services/$servicio" && go test $tags -count=1 -p 1 -coverprofile="$perfil" ./... 2>&1 | grep -E '^(ok|FAIL|---|panic)' ) || fallo=1
  total=$(cd "$ROOT/services/$servicio" && go tool cover -func="$perfil" | awk '/^total:/ {gsub("%","",$3); print $3}')
  estado="✅"
  if awk -v t="$total" -v u="$UMBRAL" 'BEGIN {exit !(t < u)}'; then
    estado="❌"
    fallo=1
  fi
  echo "| $servicio | $total % $estado | $UMBRAL % |" >> "$RESUMEN"
done

echo
cat "$RESUMEN"
if [[ -n "${GITHUB_STEP_SUMMARY:-}" ]]; then
  { echo "## Cobertura de pruebas"; echo; cat "$RESUMEN"; } >> "$GITHUB_STEP_SUMMARY"
fi
exit "$fallo"
