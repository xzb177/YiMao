#!/bin/sh
set -eu

# Compatibility deployment entry point. The guarded workflow validates code,
# configuration, tests, and build before replacing the running service.
PROJECT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
exec "$PROJECT_DIR/scripts/ops.sh" rebuild
