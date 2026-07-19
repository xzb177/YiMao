#!/bin/sh
set -eu

PROJECT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
CHECK_ENV=0
ENV_FILE=""
COMPOSE_FILE="docker-compose.yml"
ENGINE="auto"

while [ $# -gt 0 ]; do
  case "$1" in
    --env)
      CHECK_ENV=1
      ENV_FILE=".env"
      shift
      ;;
    --env-file)
      if [ $# -lt 2 ]; then
        echo "--env-file requires a path" >&2
        exit 2
      fi
      CHECK_ENV=1
      ENV_FILE="$2"
      shift 2
      ;;
    --compose-file)
      if [ $# -lt 2 ]; then
        echo "--compose-file requires a path" >&2
        exit 2
      fi
      COMPOSE_FILE="$2"
      shift 2
      ;;
    --engine)
      if [ $# -lt 2 ]; then
        echo "--engine requires auto, go, or docker" >&2
        exit 2
      fi
      ENGINE="$2"
      shift 2
      ;;
    *)
      echo "Usage: $0 [--env | --env-file PATH] [--compose-file PATH] [--engine auto|go|docker]" >&2
      exit 2
      ;;
  esac
done

case "$ENGINE" in
  auto|go|docker) ;;
  *) echo "--engine requires auto, go, or docker" >&2; exit 2 ;;
esac

cd "$PROJECT_DIR"

echo "== YiMao deployment preflight =="

for file in go.mod go.sum Dockerfile "$COMPOSE_FILE"; do
  if [ ! -f "$file" ]; then
    echo "Missing required file: $file" >&2
    exit 1
  fi
done

if command -v gofmt >/dev/null 2>&1; then
  unformatted=$(find . -type f -name '*.go' -not -path './vendor/*' -exec gofmt -l {} +)
  if [ -n "$unformatted" ]; then
    echo "Unformatted Go files:" >&2
    echo "$unformatted" >&2
    exit 1
  fi
  echo "[ok] formatting"
else
  echo "[defer] formatting check will run in Docker verify target"
fi

secret_hits=$(git grep -nE 'TELEGRAM_BOT_TOKEN=[0-9]+:|ghp_[A-Za-z0-9]{20,}' -- . ':(exclude)*.patch' 2>/dev/null || true)
if [ -n "$secret_hits" ]; then
  echo "$secret_hits" >&2
  echo "Potential secret committed" >&2
  exit 1
fi
echo "[ok] secret scan"

if [ "$CHECK_ENV" -eq 1 ]; then
  if [ ! -f "$ENV_FILE" ]; then
    echo "$ENV_FILE not found" >&2
    exit 1
  fi
  for key in TELEGRAM_BOT_TOKEN MOVIEPILOT_URL MOVIEPILOT_API_KEY; do
    if ! awk -F= -v key="$key" '$1 == key && length(substr($0, index($0, "=") + 1)) > 0 { found=1 } END { exit !found }' "$ENV_FILE"; then
      echo "Missing required environment value: $key" >&2
      exit 1
    fi
  done
  telegram_token=$(awk -F= '$1 == "TELEGRAM_BOT_TOKEN" { print substr($0, index($0, "=") + 1) }' "$ENV_FILE" | tail -n 1)
  moviepilot_url=$(awk -F= '$1 == "MOVIEPILOT_URL" { print substr($0, index($0, "=") + 1) }' "$ENV_FILE" | tail -n 1)
  moviepilot_key=$(awk -F= '$1 == "MOVIEPILOT_API_KEY" { print substr($0, index($0, "=") + 1) }' "$ENV_FILE" | tail -n 1)
  if [ "${#telegram_token}" -lt 30 ] || [ "${#moviepilot_key}" -lt 10 ]; then
    echo "Telegram or MoviePilot credential is too short" >&2
    exit 1
  fi
  case "$telegram_token:$moviepilot_key" in
    *your_*|*YOUR_*|*replace-*|*REPLACE-*) echo "Placeholder credentials must be replaced" >&2; exit 1 ;;
  esac
  case "$moviepilot_url" in
    http://*|https://*) ;;
    *) echo "MOVIEPILOT_URL must use http:// or https://" >&2; exit 1 ;;
  esac
  api_auth=$(awk -F= '$1 == "ENABLE_API_AUTH" { print tolower($2) }' "$ENV_FILE" | tail -n 1)
  case "$api_auth" in
    false|0|no|off) ;;
    *)
      if ! awk -F= '$1 == "API_KEYS" && length(substr($0, index($0, "=") + 1)) > 0 { found=1 } END { exit !found }' "$ENV_FILE"; then
        echo "API_KEYS is required when ENABLE_API_AUTH is enabled" >&2
        exit 1
      fi
      ;;
  esac
  echo "[ok] required environment values"
fi

USE_GO=0
case "$ENGINE" in
  go)
    command -v go >/dev/null 2>&1 || { echo "Go is required by --engine go" >&2; exit 1; }
    USE_GO=1
    ;;
  docker)
    command -v docker >/dev/null 2>&1 || { echo "Docker is required by --engine docker" >&2; exit 1; }
    ;;
  auto)
    if command -v go >/dev/null 2>&1; then
      USE_GO=1
    elif ! command -v docker >/dev/null 2>&1; then
      echo "Go or Docker is required for build validation" >&2
      exit 1
    fi
    ;;
esac

if [ "$USE_GO" -eq 1 ]; then
  echo "[run] go mod verify"
  GOTOOLCHAIN=auto go mod verify
  if [ "$CHECK_ENV" -eq 1 ]; then
    echo "[run] application config validation"
    GOTOOLCHAIN=auto go run ./cmd/bot --check-config "$ENV_FILE"
  fi
  echo "[run] go vet ./..."
  GOTOOLCHAIN=auto go vet ./...
  echo "[run] go test ./..."
  GOTOOLCHAIN=auto go test ./...
  echo "[run] go build ./..."
  GOTOOLCHAIN=auto go build ./...
else
  echo "[run] Docker verify target (go vet + go test)"
  docker build --target verify -t yimao:verify .
  echo "[run] Docker production image build"
  docker build -t yimao:preflight .
  if [ "$CHECK_ENV" -eq 1 ]; then
    echo "[run] application config validation"
    docker run --rm --env-file "$ENV_FILE" --entrypoint ./yimao yimao:preflight --check-config
  fi
fi

echo "[ok] build and tests"

if command -v docker >/dev/null 2>&1; then
  if docker compose version >/dev/null 2>&1; then
    if [ "$CHECK_ENV" -eq 1 ]; then
      docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" config --quiet
    else
      docker compose -f "$COMPOSE_FILE" config --quiet
    fi
    echo "[ok] docker compose config"
  elif command -v docker-compose >/dev/null 2>&1; then
    docker-compose -f "$COMPOSE_FILE" config --quiet
    echo "[ok] docker-compose config"
  fi
fi

echo "Preflight passed. No service was started or restarted."
