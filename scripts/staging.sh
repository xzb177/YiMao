#!/bin/sh
set -eu
umask 077

PROJECT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
ENV_FILE="$PROJECT_DIR/.env.staging"
COMPOSE_FILE="$PROJECT_DIR/docker-compose.staging.yml"
REPORT_DIR="$PROJECT_DIR/staging-reports"
DATA_DIR="$PROJECT_DIR/staging-data"
IMAGE="yimao:staging-smoke"
SMOKE_IMAGE_READY=0

cd "$PROJECT_DIR"

compose() {
  docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" "$@"
}

value_of() {
  awk -F= -v key="$1" '$1 == key { print substr($0, index($0, "=") + 1) }' "$2" | tail -n 1
}

init_env() {
  if [ -e "$ENV_FILE" ]; then
    echo "$ENV_FILE already exists; not overwriting"
    return 0
  fi
  cp .env.staging.example "$ENV_FILE"
  chmod 600 "$ENV_FILE"
  echo "Created $ENV_FILE with mode 600. Fill isolated staging values before use."
}

require_isolated() {
  if [ ! -f "$ENV_FILE" ]; then
    echo "$ENV_FILE not found; run: $0 init" >&2
    exit 1
  fi
  chmod 600 "$ENV_FILE"
  confirm=$(value_of STAGING_CONFIRM_ISOLATED "$ENV_FILE")
  expected_bot=$(value_of STAGING_EXPECTED_BOT_USERNAME "$ENV_FILE")
  staging_token=$(value_of TELEGRAM_BOT_TOKEN "$ENV_FILE")
  staging_mp=$(value_of MOVIEPILOT_URL "$ENV_FILE")
  staging_mp_key=$(value_of MOVIEPILOT_API_KEY "$ENV_FILE")
  api_keys=$(value_of API_KEYS "$ENV_FILE")

  if [ "$confirm" != "true" ]; then
    echo "STAGING_CONFIRM_ISOLATED must be true" >&2
    exit 1
  fi
  if [ -z "$expected_bot" ] || [ -z "$staging_token" ] || [ -z "$staging_mp" ] || [ -z "$staging_mp_key" ] || [ -z "$api_keys" ]; then
    echo "Staging bot identity, MoviePilot credentials, and API_KEYS are required" >&2
    exit 1
  fi
  for command in docker curl awk; do
    if ! command -v "$command" >/dev/null 2>&1; then
      echo "Required command not found: $command" >&2
      exit 1
    fi
  done
  if ! docker compose version >/dev/null 2>&1; then
    echo "Docker Compose v2 is required for staging" >&2
    exit 1
  fi
  if [ -f .env ]; then
    production_token=$(value_of TELEGRAM_BOT_TOKEN .env)
    production_mp=$(value_of MOVIEPILOT_URL .env)
    if [ -n "$production_token" ] && [ "$staging_token" = "$production_token" ]; then
      echo "Refusing to reuse the production Telegram bot token" >&2
      exit 1
    fi
    if [ -n "$production_mp" ] && [ "$staging_mp" = "$production_mp" ]; then
      echo "Refusing to reuse the production MoviePilot endpoint" >&2
      exit 1
    fi
  fi
  mkdir -p "$DATA_DIR" "$REPORT_DIR"
  chmod 700 "$DATA_DIR" "$REPORT_DIR" 2>/dev/null || true
}

preflight() {
  require_isolated
  sh scripts/preflight.sh --env-file "$ENV_FILE" --compose-file "$COMPOSE_FILE" --engine docker
}

wait_ready() {
  attempts=0
  while [ "$attempts" -lt 30 ]; do
    if curl -fsS http://127.0.0.1:18080/health >/dev/null 2>&1; then
      return 0
    fi
    attempts=$((attempts + 1))
    sleep 2
  done
  echo "staging service did not become healthy within 60 seconds" >&2
  return 1
}

ensure_smoke_image() {
  if [ "$SMOKE_IMAGE_READY" -eq 0 ]; then
    docker build --target smoke -t "$IMAGE" . >/dev/null
    SMOKE_IMAGE_READY=1
  fi
}

start_staging() {
  preflight
  compose build
  compose up -d --force-recreate
  wait_ready
  echo "Staging started on loopback port 18080. Production service was not touched."
}

check_runtime_logs() {
  critical=$(docker logs yimao-staging --since 15m 2>&1 | grep -ciE 'panic|fatal|terminated by other getUpdates|Conflict:.*getUpdates' || true)
  if [ "$critical" -gt 0 ]; then
    echo "Critical staging log patterns detected: $critical" >&2
    return 1
  fi
}

run_smoke() {
  require_isolated
  if ! docker ps --format '{{.Names}}' | grep -qx 'yimao-staging'; then
    echo "yimao-staging is not running" >&2
    exit 1
  fi
  check_runtime_logs
  ensure_smoke_image
  stamp=$(date -u +%Y%m%dT%H%M%SZ)
  report="$REPORT_DIR/smoke-$stamp.json"
  temp="$report.tmp"
  if docker run --rm --network host --env-file "$ENV_FILE" "$IMAGE" >"$temp"; then
    mv "$temp" "$report"
    check_runtime_logs
    echo "Smoke passed: $report"
  else
    code=$?
    if [ -s "$temp" ]; then
      mv "$temp" "$report"
      echo "Smoke failed: $report" >&2
    else
      rm -f "$temp"
      echo "Smoke failed before a report was produced" >&2
    fi
    return "$code"
  fi
}

run_soak() {
  require_isolated
  duration=${SOAK_DURATION_SECONDS:-259200}
  interval=${SOAK_INTERVAL_SECONDS:-300}
  case "$duration:$interval" in
    *[!0-9:]*|0:*|*:0) echo "Soak duration and interval must be positive integers" >&2; exit 2 ;;
  esac
  started=$(date +%s)
  deadline=$((started + duration))
  while [ "$(date +%s)" -lt "$deadline" ]; do
    run_smoke
    sleep "$interval"
  done
  echo "Soak completed without a failed smoke run."
}

case "${1:-help}" in
  init) init_env ;;
  preflight) preflight ;;
  up) start_staging ;;
  smoke) run_smoke ;;
  soak) run_soak ;;
  status) compose ps ;;
  logs) compose logs -f --tail 100 ;;
  down) compose down ;;
  help|--help|-h)
    echo "Usage: $0 {init|preflight|up|smoke|soak|status|logs|down}"
    ;;
  *)
    echo "Unknown command: $1" >&2
    exit 2
    ;;
esac
