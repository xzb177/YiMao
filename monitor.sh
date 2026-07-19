#!/bin/sh
set -u

# Compatibility health monitor. It never sources .env, prints credentials, or
# restarts production services. Configure scheduling and alert delivery in the
# host monitoring system; this script only returns the health-check exit code.
PROJECT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

check_once() {
  "$PROJECT_DIR/scripts/ops.sh" health
}

case "${1:-}" in
  "")
    check_once
    ;;
  --loop)
    interval=${MONITOR_INTERVAL_SECONDS:-300}
    case "$interval" in
      0|*[!0-9]*|"") echo "MONITOR_INTERVAL_SECONDS must be a positive integer" >&2; exit 2 ;;
    esac
    while true; do
      check_once || true
      sleep "$interval"
    done
    ;;
  *)
    echo "Usage: $0 [--loop]" >&2
    exit 2
    ;;
esac
