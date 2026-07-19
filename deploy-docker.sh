#!/bin/sh
set -eu

# Compatibility entry point for historical Docker deployments. It no longer
# disables systemd services or migrates files implicitly; back up explicitly,
# then use the guarded Compose rebuild flow.
PROJECT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
exec "$PROJECT_DIR/scripts/ops.sh" rebuild
