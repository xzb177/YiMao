#!/bin/sh
set -eu

PROJECT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
CHECK_ENV=0

if [ "${1:-}" = "--env" ]; then
  CHECK_ENV=1
elif [ $# -gt 0 ]; then
  echo "Usage: $0 [--env]" >&2
  exit 2
fi

cd "$PROJECT_DIR"

echo "== YiMao deployment preflight =="

for file in go.mod go.sum Dockerfile docker-compose.yml; do
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
  if [ ! -f .env ]; then
    echo ".env not found" >&2
    exit 1
  fi
  for key in TELEGRAM_BOT_TOKEN MOVIEPILOT_URL MOVIEPILOT_API_KEY; do
    if ! awk -F= -v key="$key" '$1 == key && length(substr($0, index($0, "=") + 1)) > 0 { found=1 } END { exit !found }' .env; then
      echo "Missing required environment value: $key" >&2
      exit 1
    fi
  done
  telegram_token=$(awk -F= '$1 == "TELEGRAM_BOT_TOKEN" { print substr($0, index($0, "=") + 1) }' .env | tail -n 1)
  moviepilot_url=$(awk -F= '$1 == "MOVIEPILOT_URL" { print substr($0, index($0, "=") + 1) }' .env | tail -n 1)
  moviepilot_key=$(awk -F= '$1 == "MOVIEPILOT_API_KEY" { print substr($0, index($0, "=") + 1) }' .env | tail -n 1)
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
  api_auth=$(awk -F= '$1 == "ENABLE_API_AUTH" { print tolower($2) }' .env | tail -n 1)
  case "$api_auth" in
    false|0|no|off) ;;
    *)
      if ! awk -F= '$1 == "API_KEYS" && length(substr($0, index($0, "=") + 1)) > 0 { found=1 } END { exit !found }' .env; then
        echo "API_KEYS is required when ENABLE_API_AUTH is enabled" >&2
        exit 1
      fi
      ;;
  esac
  echo "[ok] required environment values"
fi

if command -v go >/dev/null 2>&1; then
  echo "[run] go mod verify"
  GOTOOLCHAIN=auto go mod verify
  if [ "$CHECK_ENV" -eq 1 ]; then
    echo "[run] application config validation"
    GOTOOLCHAIN=auto go run ./cmd/bot --check-config .env
  fi
  echo "[run] go vet ./..."
  GOTOOLCHAIN=auto go vet ./...
  echo "[run] go test ./..."
  GOTOOLCHAIN=auto go test ./...
  echo "[run] go build ./..."
  GOTOOLCHAIN=auto go build ./...
elif command -v docker >/dev/null 2>&1; then
  echo "[run] Docker verify target (go vet + go test)"
  docker build --target verify -t yimao:verify .
  echo "[run] Docker production image build"
  docker build -t yimao:preflight .
  if [ "$CHECK_ENV" -eq 1 ]; then
    echo "[run] application config validation"
    docker run --rm --env-file .env --entrypoint ./yimao yimao:preflight --check-config
  fi
else
  echo "Go or Docker is required for build validation" >&2
  exit 1
fi

echo "[ok] build and tests"

if command -v docker >/dev/null 2>&1; then
  if docker compose version >/dev/null 2>&1; then
    docker compose config --quiet
    echo "[ok] docker compose config"
  elif command -v docker-compose >/dev/null 2>&1; then
    docker-compose config --quiet
    echo "[ok] docker-compose config"
  fi
fi

echo "Preflight passed. No service was started or restarted."
