#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
NAME=yimao-lifecycle-test
VOLUME=yimao-lifecycle-data
DR_NAME=yimao-lifecycle-disaster
DR_VOLUME=yimao-lifecycle-disaster-data
BASE_IMAGE=yimao:lifecycle-base
IMAGE=yimao:lifecycle-test
UNHEALTHY_IMAGE=yimao:lifecycle-unhealthy
PORT=18088
REAL_CURL=$(command -v curl)
REAL_DOCKER=$(command -v docker)
export REAL_DOCKER
TMP_ROOT=${YIMAO_TEST_TMP_ROOT:-/opt/data/.hermes/tmp}
mkdir -p "$TMP_ROOT"
TMP=$(mktemp -d "$TMP_ROOT/yimao-lifecycle.XXXXXX")
HOST_TMP_ROOT=${YIMAO_TEST_HOST_TMP_ROOT:-$TMP_ROOT}
HOST_TMP=$HOST_TMP_ROOT/$(basename "$TMP")
ENV_FILE=$TMP/test.env
BACKUPS=$TMP/backups

cleanup() {
    docker rm -f "$NAME" >/dev/null 2>&1 || true
    docker rm -f "$DR_NAME" >/dev/null 2>&1 || true
    docker volume rm "$VOLUME" >/dev/null 2>&1 || true
    docker volume rm "$DR_VOLUME" >/dev/null 2>&1 || true
    for volume in $(docker volume ls -q | grep -E "^${VOLUME}-(restore|safety|install-safety)-" || true); do
        docker volume rm "$volume" >/dev/null 2>&1 || true
    done
    for volume in $(docker volume ls -q | grep -E "^${DR_VOLUME}-(restore|safety)-" || true); do
        docker volume rm "$volume" >/dev/null 2>&1 || true
    done
    for test_image in $(docker image ls --format '{{.Repository}}:{{.Tag}}' | grep -E '^yimao:lifecycle-' || true); do
        docker image rm "$test_image" >/dev/null 2>&1 || true
    done
    if [ -n "${HOST_TMP:-}" ]; then
        docker run --rm --mount "type=bind,src=$HOST_TMP,dst=/cleanup" alpine:latest \
            sh -c "chown -R $(id -u):$(id -g) /cleanup" >/dev/null 2>&1 || true
    fi
    rm -rf "$TMP"
}
trap cleanup EXIT INT TERM

cat > "$ENV_FILE" <<EOF
TELEGRAM_BOT_TOKEN=test-token-abcdefghijklmnopqrstuvwxyzABCDE
ADMIN_USER_IDS=123456789
MOVIEPILOT_URL=http://127.0.0.1:1
MOVIEPILOT_API_KEY=abcdefghijklmnopqrstuvwxyz123456
ENABLE_API_AUTH=true
API_KEYS={"abcdefghijklmnopqrstuvwxyz123456":"lifecycle"}
PORT=$PORT
PUID=0
PGID=0
EOF
chmod 600 "$ENV_FILE"

docker build --build-arg REVISION=lifecycle-test -t "$BASE_IMAGE" "$ROOT"
docker build -f "$ROOT/scripts/tests/lifecycle.Dockerfile" --build-arg BASE_IMAGE="$BASE_IMAGE" -t "$IMAGE" "$ROOT"
cat > "$TMP/unhealthy.Dockerfile" <<'EOF'
ARG BASE_IMAGE
FROM ${BASE_IMAGE}
HEALTHCHECK --interval=1s --timeout=1s --start-period=1s --retries=1 CMD false
EOF
docker build -f "$TMP/unhealthy.Dockerfile" --build-arg BASE_IMAGE="$BASE_IMAGE" -t "$UNHEALTHY_IMAGE" "$TMP"

# Exercise backup and rollback without contacting the intentionally absent dependencies.
FAKEBIN=$TMP/bin
mkdir -p "$FAKEBIN"
cat > "$FAKEBIN/curl" <<'EOF'
#!/bin/sh
exit 0
EOF
chmod +x "$FAKEBIN/curl"
cat > "$FAKEBIN/docker" <<'EOF'
#!/bin/sh
if [ "${YIMAO_TEST_SIGNAL_BACKUP:-0}" = 1 ] && [ "$1" = run ]; then
    case " $* " in
        *'--mount'*'dst=/source,readonly'*) kill -TERM "$PPID"; sleep 1; exit 143 ;;
    esac
fi
if [ "${YIMAO_TEST_SIGNAL_INSTALL_SAFETY:-0}" = 1 ] && [ "$1" = run ]; then
    case " $* " in
        *'/source:ro'*'/safety'*) kill -TERM "$PPID"; sleep 1; exit 143 ;;
    esac
fi
if [ "${YIMAO_TEST_FAIL_RUN_D_ONCE:-0}" = 1 ] && [ "$1" = run ] && [ "$2" = -d ]; then
    state=${YIMAO_TEST_FAILURE_STATE:?}
    if [ ! -e "$state" ]; then
        : > "$state"
        exit 125
    fi
fi
if [ -n "${YIMAO_TEST_MARK_RUN_D:-}" ] && [ "$1" = run ] && [ "$2" = -d ]; then
    "$REAL_DOCKER" "$@"
    status=$?
    [ "$status" -eq 0 ] && : > "$YIMAO_TEST_MARK_RUN_D"
    exit "$status"
fi
if [ "${YIMAO_TEST_FAKE_BUILD:-0}" = 1 ] && [ "$1" = build ]; then
    shift
    target=""
    while [ $# -gt 0 ]; do
        if [ "$1" = -t ]; then target=$2; break; fi
        shift
    done
    [ -n "$target" ] || exit 125
    exec "$REAL_DOCKER" tag "$YIMAO_TEST_BASE_IMAGE" "$target"
fi
if [ "${YIMAO_TEST_FAKE_BUILD:-0}" = 1 ] && [ "$1" = image ] && [ "$2" = inspect ]; then
    case " $* " in
        *org.opencontainers.image.revision*) printf '%s\n' "$YIMAO_TEST_REVISION"; exit 0 ;;
    esac
fi
exec "$REAL_DOCKER" "$@"
EOF
chmod +x "$FAKEBIN/docker"

env YIMAO_CONTAINER_NAME="$NAME" YIMAO_VOLUME_NAME="$VOLUME" YIMAO_IMAGE="$IMAGE" \
    YIMAO_ENV_FILE="$ENV_FILE" YIMAO_BACKUP_DIR="$BACKUPS" YIMAO_HEALTH_ATTEMPTS=4 \
    "$ROOT/scripts/ops.sh" uninstall >/dev/null

docker volume create "$VOLUME" >/dev/null
docker run --rm -v "$VOLUME:/data" --entrypoint /bin/sh "$IMAGE" -c 'printf original > /data/probe.txt'

# Legacy bind mounts can be backed up, but automated install/update must refuse
# to replace them with an empty managed volume.
BIND_DATA=$TMP/bind-data
BIND_SOURCE=$HOST_TMP/bind-data
BIND_BACKUPS=$TMP/bind-backups
mkdir -p "$BIND_DATA"
printf bind-original > "$BIND_DATA/probe.txt"
docker run -d --name "$NAME" --network host --restart unless-stopped --env-file "$ENV_FILE" \
    -e DATA_DIR=/app/data --mount "type=bind,src=$BIND_SOURCE,dst=/app/data" \
    -v /var/run/docker.sock:/var/run/docker.sock "$IMAGE" >/dev/null
BIND_BACKUP=$(PATH="$FAKEBIN:$PATH" YIMAO_CONTAINER_NAME="$NAME" YIMAO_VOLUME_NAME="$VOLUME" \
    YIMAO_ENV_FILE="$ENV_FILE" YIMAO_BACKUP_DIR="$BIND_BACKUPS" YIMAO_HEALTH_ATTEMPTS=8 \
    "$ROOT/scripts/ops.sh" backup | tail -n 1)
[ "$(tar -xOzf "$BIND_BACKUP/data.tar.gz" ./probe.txt)" = bind-original ] || {
    echo "bind-mounted data backup mismatch" >&2
    exit 1
}
if PATH="$FAKEBIN:$PATH" YIMAO_CONTAINER_NAME="$NAME" YIMAO_VOLUME_NAME="$VOLUME" \
    YIMAO_ENV_FILE="$ENV_FILE" YIMAO_BACKUP_DIR="$BIND_BACKUPS" YIMAO_HEALTH_ATTEMPTS=8 \
    "$ROOT/scripts/ops.sh" install >/dev/null 2>&1; then
    echo "install unexpectedly replaced a bind-mounted data directory" >&2
    exit 1
fi
[ "$(docker inspect -f '{{range .Mounts}}{{if eq .Destination "/app/data"}}{{.Type}}{{end}}{{end}}' "$NAME")" = bind ] || {
    echo "bind-mounted container changed after rejected install" >&2
    exit 1
}
docker rm -f "$NAME" >/dev/null

docker run -d --name "$NAME" --network host --restart unless-stopped --env-file "$ENV_FILE" \
    -e DATA_DIR=/app/data -v "$VOLUME:/app/data" -v /var/run/docker.sock:/var/run/docker.sock "$IMAGE" >/dev/null

# The test image overrides only Docker HEALTHCHECK. HTTP probing is replaced below.
for _ in 1 2 3 4 5 6; do
    [ "$(docker inspect -f '{{.State.Running}}' "$NAME")" = true ] && break
    sleep 1
done

# TERM during archive creation must abort and restore the original service.
if PATH="$FAKEBIN:$PATH" YIMAO_TEST_SIGNAL_BACKUP=1 \
    YIMAO_CONTAINER_NAME="$NAME" YIMAO_VOLUME_NAME="$VOLUME" \
    YIMAO_ENV_FILE="$ENV_FILE" YIMAO_BACKUP_DIR="$BACKUPS" YIMAO_HEALTH_ATTEMPTS=8 \
    "$ROOT/scripts/ops.sh" backup >/dev/null 2>&1; then
    echo "signalled backup unexpectedly succeeded" >&2
    exit 1
fi
[ "$(docker inspect -f '{{.State.Running}}' "$NAME")" = true ] || {
    echo "container was not restored after backup TERM" >&2
    exit 1
}
[ "$(docker inspect -f '{{.State.Health.Status}}' "$NAME")" = healthy ] || {
    echo "container was not healthy after backup TERM" >&2
    exit 1
}

BACKUP=$(PATH="$FAKEBIN:$PATH" YIMAO_CONTAINER_NAME="$NAME" YIMAO_VOLUME_NAME="$VOLUME" \
    YIMAO_ENV_FILE="$ENV_FILE" YIMAO_BACKUP_DIR="$BACKUPS" YIMAO_HEALTH_ATTEMPTS=8 \
    "$ROOT/scripts/ops.sh" backup | tail -n 1)
[ -n "$BACKUP" ] && [ -d "$BACKUP" ] || { echo "backup path was not returned" >&2; exit 1; }
SECOND_BACKUP=$(PATH="$FAKEBIN:$PATH" YIMAO_CONTAINER_NAME="$NAME" YIMAO_VOLUME_NAME="$VOLUME" \
    YIMAO_ENV_FILE="$ENV_FILE" YIMAO_BACKUP_DIR="$BACKUPS" YIMAO_HEALTH_ATTEMPTS=8 \
    "$ROOT/scripts/ops.sh" backup | tail -n 1)
[ "$SECOND_BACKUP" != "$BACKUP" ] || { echo "consecutive backups reused one directory" >&2; exit 1; }
(cd "$BACKUP" && sha256sum -c SHA256SUMS >/dev/null)
(cd "$SECOND_BACKUP" && sha256sum -c SHA256SUMS >/dev/null)
grep -qx 'image=yimao:lifecycle-test' "$BACKUP/container.state"
grep -qx 'revision=lifecycle-test' "$BACKUP/container.state"
grep -qx 'network=host' "$BACKUP/container.state"
grep -qx 'restart=unless-stopped' "$BACKUP/container.state"

docker run --rm -v "$VOLUME:/data" --entrypoint /bin/sh "$IMAGE" -c 'printf changed > /data/probe.txt'
PATH="$FAKEBIN:$PATH" YIMAO_CONTAINER_NAME="$NAME" YIMAO_VOLUME_NAME="$VOLUME" \
    YIMAO_ENV_FILE="$ENV_FILE" YIMAO_BACKUP_DIR="$BACKUPS" YIMAO_HEALTH_ATTEMPTS=8 \
    "$ROOT/scripts/ops.sh" rollback "$BACKUP" >/dev/null

VALUE=$(docker run --rm -v "$VOLUME:/data:ro" --entrypoint /bin/sh "$IMAGE" -c 'cat /data/probe.txt')
[ "$VALUE" = original ] || { echo "rollback data mismatch: $VALUE" >&2; exit 1; }
VERSION=$("$REAL_CURL" -sS --max-time 5 "http://127.0.0.1:$PORT/health" | sed -n 's/.*\"version\":\"\([^\"]*\)\".*/\1/p')
[ "$VERSION" = 1.1.0 ] || { echo "health version mismatch: $VERSION" >&2; exit 1; }

# Failure while starting the rollback target must restore data, env and service.
docker run --rm -v "$VOLUME:/data" --entrypoint /bin/sh "$IMAGE" -c 'printf before-failed-rollback > /data/probe.txt'
ENV_BEFORE=$(sha256sum "$ENV_FILE" | awk '{print $1}')
FAILURE_STATE=$TMP/fail-run-d.once
if PATH="$FAKEBIN:$PATH" YIMAO_TEST_FAIL_RUN_D_ONCE=1 YIMAO_TEST_FAILURE_STATE="$FAILURE_STATE" \
    YIMAO_CONTAINER_NAME="$NAME" YIMAO_VOLUME_NAME="$VOLUME" \
    YIMAO_ENV_FILE="$ENV_FILE" YIMAO_BACKUP_DIR="$BACKUPS" YIMAO_HEALTH_ATTEMPTS=8 \
    "$ROOT/scripts/ops.sh" rollback "$BACKUP" >/dev/null 2>&1; then
    echo "fault-injected rollback unexpectedly succeeded" >&2
    exit 1
fi
[ -e "$FAILURE_STATE" ] || { echo "rollback failure was not injected" >&2; exit 1; }
[ "$(docker inspect -f '{{.State.Running}}' "$NAME")" = true ] || {
    echo "container was not restored after rollback failure" >&2
    exit 1
}
VALUE=$(docker run --rm -v "$VOLUME:/data:ro" --entrypoint /bin/sh "$IMAGE" -c 'cat /data/probe.txt')
[ "$VALUE" = before-failed-rollback ] || { echo "safety data was not restored: $VALUE" >&2; exit 1; }
[ "$(sha256sum "$ENV_FILE" | awk '{print $1}')" = "$ENV_BEFORE" ] || {
    echo "environment was not restored after rollback failure" >&2
    exit 1
}
if docker volume ls -q | grep -Eq "^${VOLUME}-(restore|safety)-"; then
    echo "rollback temporary volumes were not cleaned" >&2
    exit 1
fi
if [ -n "$(find "$(dirname "$ENV_FILE")" -maxdepth 1 -name 'test.env.rollback.*' -print -quit)" ]; then
    echo "rollback environment copy was not cleaned" >&2
    exit 1
fi

# If the on-disk env is missing, a failed rollback must still recreate the
# existing container from its captured runtime environment, then remove env.
docker run --rm -v "$VOLUME:/data" --entrypoint /bin/sh "$IMAGE" -c 'printf before-missing-env-rollback > /data/probe.txt'
rm -f "$ENV_FILE"
MISSING_ENV_FAILURE_STATE=$TMP/fail-missing-env-run-d.once
if PATH="$FAKEBIN:$PATH" YIMAO_TEST_FAIL_RUN_D_ONCE=1 YIMAO_TEST_FAILURE_STATE="$MISSING_ENV_FAILURE_STATE" \
    YIMAO_CONTAINER_NAME="$NAME" YIMAO_VOLUME_NAME="$VOLUME" \
    YIMAO_ENV_FILE="$ENV_FILE" YIMAO_BACKUP_DIR="$BACKUPS" YIMAO_HEALTH_ATTEMPTS=8 \
    "$ROOT/scripts/ops.sh" rollback "$BACKUP" >/dev/null 2>&1; then
    echo "missing-env fault-injected rollback unexpectedly succeeded" >&2
    exit 1
fi
[ -e "$MISSING_ENV_FAILURE_STATE" ] || { echo "missing-env rollback failure was not injected" >&2; exit 1; }
[ "$(docker inspect -f '{{.State.Running}}' "$NAME")" = true ] || {
    echo "container was not restored after missing-env rollback failure" >&2
    exit 1
}
[ "$(docker inspect -f '{{.Config.Image}}' "$NAME")" = "$IMAGE" ] || {
    echo "container image changed after missing-env rollback failure" >&2
    exit 1
}
[ ! -e "$ENV_FILE" ] || { echo "missing environment was recreated on disk" >&2; exit 1; }
VALUE=$(docker run --rm -v "$VOLUME:/data:ro" --entrypoint /bin/sh "$IMAGE" -c 'cat /data/probe.txt')
[ "$VALUE" = before-missing-env-rollback ] || {
    echo "missing-env safety data was not restored: $VALUE" >&2
    exit 1
}
if docker volume ls -q | grep -Eq "^${VOLUME}-(restore|safety)-"; then
    echo "missing-env rollback temporary volumes were not cleaned" >&2
    exit 1
fi
if [ -n "$(find "$(dirname "$ENV_FILE")" -maxdepth 1 -name 'test.env.rollback.*' -print -quit)" ]; then
    echo "missing-env rollback environment copy was not cleaned" >&2
    exit 1
fi
docker inspect -f '{{range .Config.Env}}{{println .}}{{end}}' "$NAME" > "$ENV_FILE"
chmod 600 "$ENV_FILE"

# A failed candidate deployment must release the preserved container before
# restoring the managed volume, then recreate the previous healthy release.
docker run --rm -v "$VOLUME:/data" --entrypoint /bin/sh "$IMAGE" -c 'printf before-failed-deploy > /data/probe.txt'
DEPLOY_FAILURE_STATE=$TMP/fail-deploy-run-d.once
DEPLOY_FAILURE_LOG=$TMP/fail-deploy.log
TEST_PROJECT=$TMP/project
REVISION=$(git -C "$ROOT" rev-parse HEAD)
mkdir -p "$TEST_PROJECT/scripts"
cp "$ROOT/scripts/ops.sh" "$TEST_PROJECT/scripts/ops.sh"
cp "$ROOT/Dockerfile" "$ROOT/docker-compose.yml" "$TEST_PROJECT/"
cat > "$TEST_PROJECT/scripts/preflight.sh" <<'EOF'
#!/bin/sh
exit 0
EOF
chmod 755 "$TEST_PROJECT/scripts/ops.sh" "$TEST_PROJECT/scripts/preflight.sh"
if PATH="$FAKEBIN:$PATH" YIMAO_TEST_FAIL_RUN_D_ONCE=1 YIMAO_TEST_FAILURE_STATE="$DEPLOY_FAILURE_STATE" \
    YIMAO_TEST_FAKE_BUILD=1 YIMAO_TEST_BASE_IMAGE="$IMAGE" YIMAO_TEST_REVISION=unknown \
    YIMAO_CONTAINER_NAME="$NAME" YIMAO_VOLUME_NAME="$VOLUME" YIMAO_IMAGE="yimao:lifecycle-candidate" \
    YIMAO_REVISION="$REVISION" YIMAO_ENV_FILE="$ENV_FILE" YIMAO_BACKUP_DIR="$BACKUPS" YIMAO_HEALTH_ATTEMPTS=8 \
    "$TEST_PROJECT/scripts/ops.sh" install >"$DEPLOY_FAILURE_LOG" 2>&1; then
    echo "fault-injected deployment unexpectedly succeeded" >&2
    exit 1
fi
[ -e "$DEPLOY_FAILURE_STATE" ] || {
    echo "deployment failure was not injected" >&2
    cat "$DEPLOY_FAILURE_LOG" >&2
    exit 1
}
[ "$(docker inspect -f '{{.State.Health.Status}}' "$NAME")" = healthy ] || {
    echo "previous container was not restored healthy after deployment failure" >&2
    exit 1
}
VALUE=$(docker run --rm -v "$VOLUME:/data:ro" --entrypoint /bin/sh "$IMAGE" -c 'cat /data/probe.txt')
[ "$VALUE" = before-failed-deploy ] || { echo "deployment backup was not restored: $VALUE" >&2; exit 1; }
if docker ps -a --format '{{.Names}}' | grep -Eq "^${NAME}-rollback-"; then
    echo "preserved deployment container was not cleaned" >&2
    exit 1
fi

# A real candidate that starts but never becomes healthy has no previous
# release to restore, but it must still be removed by the deployment trap.
docker rm -f "$NAME" >/dev/null
docker volume rm "$VOLUME" >/dev/null
FIRST_INSTALL_CREATE_STATE=$TMP/first-install-created
if PATH="$FAKEBIN:$PATH" YIMAO_TEST_MARK_RUN_D="$FIRST_INSTALL_CREATE_STATE" \
    YIMAO_TEST_FAKE_BUILD=1 YIMAO_TEST_BASE_IMAGE="$UNHEALTHY_IMAGE" YIMAO_TEST_REVISION=unknown \
    YIMAO_CONTAINER_NAME="$NAME" YIMAO_VOLUME_NAME="$VOLUME" YIMAO_IMAGE="yimao:lifecycle-first-install" \
    YIMAO_REVISION="$REVISION" YIMAO_ENV_FILE="$ENV_FILE" YIMAO_BACKUP_DIR="$BACKUPS" YIMAO_HEALTH_ATTEMPTS=4 \
    "$TEST_PROJECT/scripts/ops.sh" install >/dev/null 2>&1; then
    echo "unhealthy first install unexpectedly succeeded" >&2
    exit 1
fi
[ -e "$FIRST_INSTALL_CREATE_STATE" ] || { echo "first-install candidate was not created" >&2; exit 1; }
if docker inspect "$NAME" >/dev/null 2>&1; then
    echo "failed first install left a candidate container" >&2
    exit 1
fi

# Reinstall after the default uninstall must preserve an existing named volume
# if the new candidate starts but never becomes healthy.
docker volume create "$VOLUME" >/dev/null
docker run --rm -v "$VOLUME:/data" --entrypoint /bin/sh "$IMAGE" -c 'printf preserved-after-uninstall > /data/probe.txt'
if PATH="$FAKEBIN:$PATH" YIMAO_TEST_SIGNAL_INSTALL_SAFETY=1 \
    YIMAO_TEST_FAKE_BUILD=1 YIMAO_TEST_BASE_IMAGE="$UNHEALTHY_IMAGE" YIMAO_TEST_REVISION=unknown \
    YIMAO_CONTAINER_NAME="$NAME" YIMAO_VOLUME_NAME="$VOLUME" YIMAO_IMAGE="yimao:lifecycle-preserved-install" \
    YIMAO_REVISION="$REVISION" YIMAO_ENV_FILE="$ENV_FILE" YIMAO_BACKUP_DIR="$BACKUPS" YIMAO_HEALTH_ATTEMPTS=4 \
    "$TEST_PROJECT/scripts/ops.sh" install >/dev/null 2>&1; then
    echo "signalled install safety copy unexpectedly succeeded" >&2
    exit 1
else
    SIGNAL_STATUS=$?
fi
[ "$SIGNAL_STATUS" -eq 143 ] || { echo "install safety signal status mismatch: $SIGNAL_STATUS" >&2; exit 1; }
VALUE=$(docker run --rm -v "$VOLUME:/data:ro" --entrypoint /bin/sh "$IMAGE" -c 'cat /data/probe.txt')
[ "$VALUE" = preserved-after-uninstall ] || { echo "interrupted safety copy changed original data: $VALUE" >&2; exit 1; }
if docker volume ls -q | grep -Eq "^${VOLUME}-install-safety-"; then
    echo "interrupted install safety copy left a volume" >&2
    exit 1
fi
PRESERVED_INSTALL_CREATE_STATE=$TMP/preserved-install-created
if PATH="$FAKEBIN:$PATH" YIMAO_TEST_MARK_RUN_D="$PRESERVED_INSTALL_CREATE_STATE" \
    YIMAO_TEST_FAKE_BUILD=1 YIMAO_TEST_BASE_IMAGE="$UNHEALTHY_IMAGE" YIMAO_TEST_REVISION=unknown \
    YIMAO_CONTAINER_NAME="$NAME" YIMAO_VOLUME_NAME="$VOLUME" YIMAO_IMAGE="yimao:lifecycle-preserved-install" \
    YIMAO_REVISION="$REVISION" YIMAO_ENV_FILE="$ENV_FILE" YIMAO_BACKUP_DIR="$BACKUPS" YIMAO_HEALTH_ATTEMPTS=4 \
    "$TEST_PROJECT/scripts/ops.sh" install >/dev/null 2>&1; then
    echo "unhealthy install with preserved data unexpectedly succeeded" >&2
    exit 1
fi
[ -e "$PRESERVED_INSTALL_CREATE_STATE" ] || { echo "preserved-data candidate was not created" >&2; exit 1; }
if docker inspect "$NAME" >/dev/null 2>&1; then
    echo "failed install with preserved data left a candidate container" >&2
    exit 1
fi
VALUE=$(docker run --rm -v "$VOLUME:/data:ro" --entrypoint /bin/sh "$IMAGE" -c 'cat /data/probe.txt')
[ "$VALUE" = preserved-after-uninstall ] || { echo "preserved volume was not restored: $VALUE" >&2; exit 1; }
if docker volume ls -q | grep -Eq "^${VOLUME}-install-safety-"; then
    echo "install safety volume was not cleaned" >&2
    exit 1
fi

# Recreate the fixture for rollback and uninstall checks below.
docker run --rm -v "$VOLUME:/data" --entrypoint /bin/sh "$IMAGE" -c 'printf before-failed-deploy > /data/probe.txt'
docker run -d --name "$NAME" --restart unless-stopped --network host \
    -v "$VOLUME:/app/data" -v /var/run/docker.sock:/var/run/docker.sock \
    --env-file "$ENV_FILE" "$IMAGE" >/dev/null
for _ in 1 2 3 4 5 6 7 8; do
    [ "$(docker inspect -f '{{.State.Health.Status}}' "$NAME")" = healthy ] && break
    sleep 1
done
[ "$(docker inspect -f '{{.State.Health.Status}}' "$NAME")" = healthy ] || {
    echo "recreated lifecycle fixture is not healthy" >&2
    exit 1
}

# A corrupt archive must fail before the current container or data volume changes.
CORRUPT_VALUE=$(docker run --rm -v "$VOLUME:/data:ro" --entrypoint /bin/sh "$IMAGE" -c 'cat /data/probe.txt')
cp "$BACKUP/data.tar.gz" "$BACKUP/data.tar.gz.good"
printf corrupt > "$BACKUP/data.tar.gz"
(cd "$BACKUP" && sha256sum data.tar.gz container.state env.backup > SHA256SUMS)
if PATH="$FAKEBIN:$PATH" YIMAO_CONTAINER_NAME="$NAME" YIMAO_VOLUME_NAME="$VOLUME" \
    YIMAO_ENV_FILE="$ENV_FILE" YIMAO_HEALTH_ATTEMPTS=8 \
    "$ROOT/scripts/ops.sh" rollback "$BACKUP" >/dev/null 2>&1; then
    echo "corrupt rollback unexpectedly succeeded" >&2
    exit 1
fi
[ "$(docker inspect -f '{{.State.Running}}' "$NAME")" = true ] || { echo "container stopped after corrupt rollback" >&2; exit 1; }
VALUE=$(docker run --rm -v "$VOLUME:/data:ro" --entrypoint /bin/sh "$IMAGE" -c 'cat /data/probe.txt')
[ "$VALUE" = "$CORRUPT_VALUE" ] || { echo "data changed after corrupt rollback: $VALUE" >&2; exit 1; }
mv "$BACKUP/data.tar.gz.good" "$BACKUP/data.tar.gz"
(cd "$BACKUP" && sha256sum data.tar.gz container.state env.backup > SHA256SUMS)

# A verified backup must restore onto an empty disaster-recovery target without
# requiring a current env file or managed volume.
DR_ENV_FILE=$TMP/disaster/test.env
mkdir -p "$(dirname "$DR_ENV_FILE")"
rm -f "$DR_ENV_FILE"
docker stop -t 20 "$NAME" >/dev/null
docker rm -f "$DR_NAME" >/dev/null 2>&1 || true
docker volume rm "$DR_VOLUME" >/dev/null 2>&1 || true
PATH="$FAKEBIN:$PATH" YIMAO_CONTAINER_NAME="$DR_NAME" YIMAO_VOLUME_NAME="$DR_VOLUME" \
    YIMAO_ENV_FILE="$DR_ENV_FILE" YIMAO_HEALTH_ATTEMPTS=8 \
    "$ROOT/scripts/ops.sh" rollback "$BACKUP" >/dev/null
[ -f "$DR_ENV_FILE" ] || { echo "disaster recovery did not restore env" >&2; exit 1; }
cmp "$DR_ENV_FILE" "$BACKUP/env.backup" >/dev/null || { echo "disaster recovery env mismatch" >&2; exit 1; }
VALUE=$(docker run --rm -v "$DR_VOLUME:/data:ro" --entrypoint /bin/sh "$IMAGE" -c 'cat /data/probe.txt')
[ "$VALUE" = original ] || { echo "disaster recovery data mismatch: $VALUE" >&2; exit 1; }
[ "$(docker inspect -f '{{.State.Health.Status}}' "$DR_NAME")" = healthy ] || {
    echo "disaster recovery container is not healthy" >&2
    exit 1
}
docker rm -f "$DR_NAME" >/dev/null
docker volume rm "$DR_VOLUME" >/dev/null
rm -f "$DR_ENV_FILE"

# A failed restore onto an empty target must return that target to empty.
DR_FAILURE_STATE=$TMP/disaster-fail-run-d.once
if PATH="$FAKEBIN:$PATH" YIMAO_TEST_FAIL_RUN_D_ONCE=1 YIMAO_TEST_FAILURE_STATE="$DR_FAILURE_STATE" \
    YIMAO_CONTAINER_NAME="$DR_NAME" YIMAO_VOLUME_NAME="$DR_VOLUME" \
    YIMAO_ENV_FILE="$DR_ENV_FILE" YIMAO_HEALTH_ATTEMPTS=8 \
    "$ROOT/scripts/ops.sh" rollback "$BACKUP" >/dev/null 2>&1; then
    echo "fault-injected empty-target recovery unexpectedly succeeded" >&2
    exit 1
fi
[ -e "$DR_FAILURE_STATE" ] || { echo "empty-target recovery failure was not injected" >&2; exit 1; }
if docker inspect "$DR_NAME" >/dev/null 2>&1; then
    echo "failed empty-target recovery left a container" >&2
    exit 1
fi
if docker volume inspect "$DR_VOLUME" >/dev/null 2>&1; then
    echo "failed empty-target recovery left a managed volume" >&2
    exit 1
fi
[ ! -e "$DR_ENV_FILE" ] || { echo "failed empty-target recovery left an env file" >&2; exit 1; }
if docker volume ls -q | grep -Eq "^${DR_VOLUME}-(restore|safety)-"; then
    echo "failed empty-target recovery left temporary volumes" >&2
    exit 1
fi
docker start "$NAME" >/dev/null
for _ in 1 2 3 4 5 6 7 8; do
    [ "$(docker inspect -f '{{.State.Health.Status}}' "$NAME")" = healthy ] && break
    sleep 1
done
[ "$(docker inspect -f '{{.State.Health.Status}}' "$NAME")" = healthy ] || {
    echo "main lifecycle fixture did not recover after disaster tests" >&2
    exit 1
}

YIMAO_CONTAINER_NAME="$NAME" YIMAO_VOLUME_NAME="$VOLUME" "$ROOT/scripts/ops.sh" uninstall >/dev/null
docker volume inspect "$VOLUME" >/dev/null
YIMAO_CONTAINER_NAME="$NAME" YIMAO_VOLUME_NAME="$VOLUME" "$ROOT/scripts/ops.sh" uninstall --delete-data >/dev/null
if docker volume inspect "$VOLUME" >/dev/null 2>&1; then
    echo "data volume was not deleted explicitly" >&2
    exit 1
fi

echo "deployment lifecycle: ok"
