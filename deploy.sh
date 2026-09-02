#!/bin/sh
set -eu

# Compatibility deployment entry point. Superseded by ./manage.sh install, which
# validates code, configuration, tests and build before replacing the running
# service. Kept so existing runbooks and cron entries do not break.
PROJECT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
printf '[YiMao] deploy.sh is deprecated; use ./manage.sh install\n' >&2
exec "$PROJECT_DIR/scripts/ops.sh" install
