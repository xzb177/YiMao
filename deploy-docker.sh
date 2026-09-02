#!/bin/sh
set -eu

# Compatibility entry point for historical Docker deployments. It no longer
# disables systemd services or migrates files implicitly; back up explicitly,
# then use ./manage.sh install.
PROJECT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
printf '[YiMao] deploy-docker.sh is deprecated; use ./manage.sh install\n' >&2
exec "$PROJECT_DIR/scripts/ops.sh" install
