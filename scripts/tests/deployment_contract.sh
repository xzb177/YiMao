#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$ROOT"

fail() {
    echo "deployment contract: $*" >&2
    exit 1
}

contains() {
    file=$1
    pattern=$2
    grep -Eq -- "$pattern" "$file" || fail "$file missing: $pattern"
}

contains docker-compose.yml 'network_mode:[[:space:]]*"?host"?'
contains docker-compose.yml 'restart:[[:space:]]*unless-stopped'
contains docker-compose.yml 'YIMAO_ENV_FILE:-\.env'
contains docker-compose.yml 'yimao-data:/app/data'
contains docker-compose.yml '/var/run/docker.sock:/var/run/docker.sock'
contains docker-compose.yml '^volumes:'
contains docker-compose.yml '^[[:space:]]+yimao-data:'
contains Dockerfile '^HEALTHCHECK '

for command in install backup update rollback telegram uninstall doctor health; do
    ./manage.sh --help | grep -q "^[[:space:]]*$command" || fail "manage.sh missing command: $command"
done

contains scripts/ops.sh 'docker[[:space:]]+volume'
contains scripts/ops.sh 'sha256sum'
contains scripts/ops.sh 'setChatMenuButton'
contains scripts/ops.sh 'org.opencontainers.image.revision'
contains scripts/ops.sh 'unless-stopped'
contains scripts/ops.sh 'git status --porcelain'
contains scripts/ops.sh 'image=\$\(build_image\)'
contains scripts/ops.sh 'safety_volume'
contains scripts/ops.sh 'data_mount\(\)'
contains scripts/ops.sh 'require_managed_data_volume\(\)'
contains scripts/ops.sh '--mount "\$backup_mount"'
contains .gitignore '^backups/$'
contains .dockerignore '^backups/$'
contains .gitignore '^env\.backup$'
contains .dockerignore '^env\.backup$'
contains scripts/preflight.sh '--lifecycle'
contains scripts/preflight.sh 'deployment_lifecycle\.sh'
contains scripts/preflight.sh 'YIMAO_TEST_TMP_ROOT.*yimao-lifecycle-tests'
contains scripts/preflight.sh 'YIMAO_TEST_HOST_TMP_ROOT.*lifecycle_tmp_root'
contains scripts/preflight.sh 'YIMAO_ENV_FILE=.*ENV_FILE.*docker-compose --env-file.*ENV_FILE'
contains scripts/preflight.sh 'lifecycle_term.*exit 143'
cleanup_line=$(grep -n '^[[:space:]]*cleanup_lifecycle_marker$' scripts/preflight.sh | tail -n 1 | cut -d: -f1)
clear_trap_line=$(grep -n '^[[:space:]]*trap - EXIT HUP INT TERM$' scripts/preflight.sh | tail -n 1 | cut -d: -f1)
[ -n "$cleanup_line" ] && [ -n "$clear_trap_line" ] && [ "$cleanup_line" -lt "$clear_trap_line" ] || \
  fail 'scripts/preflight.sh must remove the lifecycle marker before clearing signal traps'
contains scripts/ops.sh 'preflight\.sh.*--env-file.*ENV_FILE.*--engine docker'
contains scripts/ops.sh 'YIMAO_ENV_FILE=.*ENV_FILE.*docker compose --env-file.*ENV_FILE'
if grep -Eq 'preflight\.sh.*--lifecycle' scripts/ops.sh; then
  fail 'scripts/ops.sh must not run the release lifecycle during normal install/update'
fi
contains scripts/ops.sh 'trap finish_deploy EXIT'
contains scripts/ops.sh '\(set -e; recover_deploy\)'
contains scripts/ops.sh 'restore_backup.*rollback_backup.*previous_image'
contains scripts/ops.sh 'trap finish_restore EXIT'
contains scripts/ops.sh '\(set -e; recover_restore\)'
contains scripts/ops.sh 'trap '\''exit 143'\'' TERM'
contains internal/version/version.go 'const Version = "1\.1\.0"'
contains internal/bot/command.go '"github.com/xzb177/yimao/internal/version"'
contains internal/bot/command.go 'version\.Version'
contains internal/api/router.go '"github.com/xzb177/yimao/internal/version"'
contains internal/api/router.go '"version": *version.Version'
contains internal/server/server.go '"github.com/xzb177/yimao/internal/version"'
contains internal/server/server.go '"version": *version.Version'
contains Dockerfile 'org\.opencontainers\.image\.version=\$VERSION'

if grep -Eq '版本: <code>v1\.0</code>' internal/bot/command.go || grep -Eq '"version":[[:space:]]*"2\.0\.0"' internal/api/router.go; then
    fail "runtime contains a stale hard-coded version"
fi

contains docs/DEPLOY.md '首次初始化'
contains docs/DEPLOY.md '从旧安装迁移'
contains docs/DEPLOY.md 'Mini App'
contains docs/DEPLOY.md '回滚'
contains docs/ARCHITECTURE.md 'SQLite'
contains docs/ARCHITECTURE.md 'MoviePilot'
contains docs/ARCHITECTURE.md 'Emby'

for retired in docs/UPDATE.md docs/OPS.md docs/DOCKER.md docs/DOCKER_MIGRATION.md; do
    [ ! -e "$retired" ] || fail "retired duplicate documentation still exists: $retired"
done

for file in README.md README_EN.md docs/FEATURES.md; do
    if grep -Eq 'docker compose up|\./data:/app/data|cp -r.*data' "$file"; then
        fail "$file contains a retired deployment path"
    fi
done

if grep -Eq 'deploy\.sh|deploy-docker\.sh|update\.sh|start\.sh' README.md README_EN.md; then
    fail "README exposes compatibility entry points"
fi

echo "deployment contract: ok"