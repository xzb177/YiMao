#!/bin/sh
set -eu

# Compatibility entry point. All operational logic lives in scripts/ops.sh so
# preflight, fast-forward updates, health checks, and rebuild safety cannot
# drift across multiple management scripts.
PROJECT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
exec "$PROJECT_DIR/scripts/ops.sh" "$@"
