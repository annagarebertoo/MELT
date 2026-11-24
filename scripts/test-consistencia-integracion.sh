#!/usr/bin/env bash
# Ejecuta las pruebas L1–L3 contra dependencias descartables (compose.test.yaml).
#
#   scripts/test-consistencia-integracion.sh [l1|l2|l3|all] [--keep]
#
#   l1  consistencia de reservas y pagos (PostgreSQL)
#   l2  eventos RabbitMQ, deduplicación, DLQ y recuperación (PostgreSQL + RabbitMQ)
#   l3  OpenSearch y Memcached: índices, filtros, caché y fallback (MongoDB + OpenSearch + Memcached)
#
# Nunca usa Atlas, Supabase ni Render: las URLs apuntan a 127.0.0.1 y las
# pruebas abortan si detectan un host compartido. --keep deja los contenedores
# levantados para repetir pruebas; sin --keep se borran con sus datos (tmpfs).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMPOSE=(docker compose -f "$ROOT/compose.test.yaml" -p melt-test)
SUITE="${1:-all}"
KEEP=false
for arg in "$@"; do
  [[ "$arg" == "--keep" ]] && KEEP=true
done

case "$SUITE" in
  l1) SERVICIOS=(postgres) ;;
  l2) SERVICIOS=(postgres rabbitmq) ;;
  l3) SERVICIOS=(mongo memcached opensearch) ;;
  all) SERVICIOS=(postgres rabbitmq mongo memcached opensearch) ;;
  *) echo "uso: $0 [l1|l2|l3|all] [--keep]" >&2; exit 2 ;;
esac

limpiar() {
  if [[ "$KEEP" == false ]]; then
    "${COMPOSE[@]}" down -v --remove-orphans >/dev/null 2>&1 || true
  fi
}
trap limpiar EXIT

echo "==> Levantando dependencias descartables: ${SERVICIOS[*]}"
"${COMPOSE[@]}" up -d --wait "${SERVICIOS[@]}"

export MELT_TEST_POSTGRES_URL="postgres://postgres:melt-test@127.0.0.1:55432/postgres?sslmode=disable"
export MELT_TEST_RABBITMQ_URL="amqp://guest:guest@127.0.0.1:55672/"
export MELT_TEST_RABBITMQ_API="http://guest:guest@127.0.0.1:55673"
export CATALOGO_TEST_URI="mongodb://127.0.0.1:55017/?directConnection=true&replicaSet=melt-test"
export CATALOGO_TEST_OPENSEARCH_URL="http://127.0.0.1:59200"
export CATALOGO_TEST_MEMCACHED_ADDRESS="127.0.0.1:55211"
export GIN_MODE=release

correr() {
  local titulo="$1" dir="$2"; shift 2
  echo "==> $titulo"
  (cd "$ROOT/$dir" && go test -race -count=1 "$@")
}

if [[ "$SUITE" == l1 || "$SUITE" == all ]]; then
  correr "L1 · reservas y pagos concurrentes" services/reservas -run 'TestL1' ./cmd/api/
fi
if [[ "$SUITE" == l2 || "$SUITE" == all ]]; then
  correr "L2 · outbox de Reservas" services/reservas -run 'TestL2' ./cmd/api/
  correr "L2 · consumidor de Beneficios" services/beneficios -run 'TestL2' ./cmd/api/
fi
if [[ "$SUITE" == l3 || "$SUITE" == all ]]; then
  correr "L3 · OpenSearch y Memcached" services/catalogo-profesionales \
    -tags 'catalogointegration catalogosearchintegration' -run 'TestL3|TestOpenSearchAndMemcachedEndToEnd' ./cmd/api/
  echo "==> L3 · comparación caché fría/caliente (benchmark)"
  (cd "$ROOT/services/catalogo-profesionales" && go test -count=1 -tags 'catalogointegration catalogosearchintegration' \
    -run '^$' -bench 'BenchmarkCatalogListCache' -benchtime 200x ./cmd/api/ | grep -v 'http_request')
fi
echo "==> Pruebas $SUITE completadas"
