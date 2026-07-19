#!/bin/sh
set -eu

# Compatibility entry point for historical deployments. Direct background
# binaries and broad pkill matching are intentionally retired; the supported
# runtime is Docker Compose through the guarded ops workflow.
PROJECT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
exec "$PROJECT_DIR/scripts/ops.sh" start
