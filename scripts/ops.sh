#!/bin/sh
set -eu

PROJECT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
CONTAINER_NAME=${YIMAO_CONTAINER_NAME:-yimao}
VOLUME_NAME=${YIMAO_VOLUME_NAME:-yimao-data}
BACKUP_ROOT=${YIMAO_BACKUP_DIR:-$PROJECT_DIR/backups}
ENV_FILE=${YIMAO_ENV_FILE:-$PROJECT_DIR/.env}
COMPOSE_FILE=$PROJECT_DIR/docker-compose.yml
cd "$PROJECT_DIR"

info() { printf '[YiMao] %s\n' "$*" >&2; }
die() { printf '[YiMao] ERROR: %s\n' "$*" >&2; exit 1; }

compose() {
    if docker compose version >/dev/null 2>&1; then
        YIMAO_ENV_FILE="$ENV_FILE" docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" "$@"
    elif command -v docker-compose >/dev/null 2>&1; then
        YIMAO_ENV_FILE="$ENV_FILE" docker-compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" "$@"
    else
        die "Docker Compose is required"
    fi
}

get_env() {
    key=$1
    [ -f "$ENV_FILE" ] || return 0
    awk -F= -v key="$key" '$1 == key { value=substr($0,index($0,"=")+1) } END { print value }' "$ENV_FILE"
}

revision() {
    git rev-parse HEAD 2>/dev/null || printf unknown
}

port() {
    value=$(get_env PORT)
    case "$value" in ''|*[!0-9]*) printf 8080 ;; *) printf '%s' "$value" ;; esac
}

require_runtime() {
    command -v docker >/dev/null 2>&1 || die "Docker is required"
    docker info >/dev/null 2>&1 || die "Docker daemon is unavailable or permission is denied"
    command -v curl >/dev/null 2>&1 || die "curl is required"
    command -v sha256sum >/dev/null 2>&1 || die "sha256sum is required"
    [ -S /var/run/docker.sock ] || die "/var/run/docker.sock is unavailable"
}

prepare_env() {
    if [ ! -f "$ENV_FILE" ]; then
        cp .env.example "$ENV_FILE"
        chmod 600 "$ENV_FILE"
        info "已生成 $ENV_FILE（权限 0600）"
        info ""
        info "请编辑 $ENV_FILE 填写以下必填项："
        info "  TELEGRAM_BOT_TOKEN   从 @BotFather 获取"
        info "  ADMIN_USER_IDS       Telegram 数字 ID，逗号分隔；第一个是 root admin（可用 @userinfobot 查询）"
        info "  MOVIEPILOT_URL       宿主机可访问的 MoviePilot 地址，例如 http://192.168.1.10:3000"
        info "  MOVIEPILOT_API_KEY   MoviePilot 设置 → 安全设置里的 API Key"
        info "  API_KEYS             JSON 对象，每个 key 至少 16 位随机字符"
        info ""
        info "填好后再执行一次：./manage.sh install"
        exit 2
    fi
    mode=$(stat -c %a "$ENV_FILE" 2>/dev/null || stat -f %Lp "$ENV_FILE")
    [ "$mode" = 600 ] || die "$ENV_FILE 权限必须是 0600，请执行：chmod 600 .env"
    admin_ids=$(get_env ADMIN_USER_IDS)
    [ -n "$admin_ids" ] || die "ADMIN_USER_IDS 未填写；第一个 ID 会成为 root admin"
    case "$admin_ids" in
        *replace-with*) die "ADMIN_USER_IDS 仍是模板占位值，请填入真实的 Telegram 数字 ID" ;;
    esac
    bot_token=$(get_env TELEGRAM_BOT_TOKEN)
    case "$bot_token" in
        ''|*replace-with*) die "TELEGRAM_BOT_TOKEN 未填写或仍是模板占位值" ;;
    esac
    moviepilot_url=$(get_env MOVIEPILOT_URL)
    case "$moviepilot_url" in
        http://*|https://*) ;;
        '') die "MOVIEPILOT_URL 未填写；host network 下必须是宿主机可访问的地址" ;;
        *) die "MOVIEPILOT_URL 必须以 http:// 或 https:// 开头，当前值无效" ;;
    esac
}

preflight() {
    prepare_env
    "$PROJECT_DIR/scripts/preflight.sh" --env-file "$ENV_FILE" --engine docker
}

build_image() {
    rev=$(revision)
    image=${YIMAO_IMAGE:-yimao:$rev}
    info "Building verified image for revision $rev"
    docker build --target verify -t "yimao:verify-$rev" . >&2
    docker build --build-arg "REVISION=$rev" -t "$image" . >&2
    actual=$(docker image inspect -f '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$image")
    [ "$actual" = "$rev" ] || die "image revision mismatch: expected $rev, got $actual"
    printf '%s\n' "$image"
}

wait_healthy() {
    attempts=${YIMAO_HEALTH_ATTEMPTS:-24}
    i=1
    while [ "$i" -le "$attempts" ]; do
        state=$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$CONTAINER_NAME" 2>/dev/null || true)
        if [ "$state" = healthy ] && curl -fsS --max-time 5 "http://127.0.0.1:$(port)/health" >/dev/null; then
            return 0
        fi
        [ "$state" = exited ] || [ "$state" = dead ] || sleep 5
        i=$((i + 1))
    done
    docker logs --tail 80 "$CONTAINER_NAME" >&2 || true
    return 1
}

run_container() {
    image=$1
    docker volume create "$VOLUME_NAME" >/dev/null
    docker run -d \
        --name "$CONTAINER_NAME" \
        --network host \
        --restart unless-stopped \
        --env-file "$ENV_FILE" \
        -e DATA_DIR=/app/data \
        -v "$VOLUME_NAME:/app/data" \
        "$image" >/dev/null
}

data_mount() {
    mounts=$(docker inspect -f '{{range .Mounts}}{{if eq .Destination "/app/data"}}{{println .Type "|" .Name "|" .Source}}{{end}}{{end}}' "$CONTAINER_NAME" 2>/dev/null || true)
    lines=$(printf '%s\n' "$mounts" | sed '/^[[:space:]]*$/d' | wc -l | tr -d ' ')
    [ "$lines" = 1 ] || die "container $CONTAINER_NAME must have exactly one /app/data mount"
    printf '%s\n' "$mounts"
}

require_managed_data_volume() {
    mount=$(data_mount)
    mount_type=$(printf '%s\n' "$mount" | awk -F ' \| ' '{print $1}')
    mount_name=$(printf '%s\n' "$mount" | awk -F ' \| ' '{print $2}')
    if [ "$mount_type" != volume ] || [ "$mount_name" != "$VOLUME_NAME" ]; then
        die "container $CONTAINER_NAME uses a non-managed /app/data mount; migrate it to named volume $VOLUME_NAME before install/update"
    fi
}

deploy_image() (
    image=$1
    rollback_backup=${2:-}
    old=""
    previous_image=""
    safety_volume=""
    safety_complete=0
    transaction_active=0

    recover_deploy() {
        [ "$transaction_active" -eq 1 ] || return 0
        trap - EXIT HUP INT TERM
        info "Deployment interrupted; restoring previous release"
        if docker inspect "$CONTAINER_NAME" >/dev/null 2>&1; then
            docker rm -f "$CONTAINER_NAME" >/dev/null
        fi
        if [ -n "$safety_volume" ] && [ "$safety_complete" -eq 1 ]; then
            if docker volume inspect "$VOLUME_NAME" >/dev/null 2>&1; then
                docker volume rm "$VOLUME_NAME" >/dev/null
            fi
            docker volume create "$VOLUME_NAME" >/dev/null
            docker run --rm --entrypoint /bin/sh -v "$safety_volume:/source:ro" -v "$VOLUME_NAME:/restore" "$image" \
                -c 'set -o pipefail; cd /source && tar -cf - . | tar -xf - -C /restore'
            docker volume rm "$safety_volume" >/dev/null
        elif [ -n "$safety_volume" ]; then
            docker volume rm "$safety_volume" >/dev/null 2>&1 || true
        elif [ -n "$rollback_backup" ]; then
            if [ -n "$old" ] && docker inspect "$old" >/dev/null 2>&1; then
                docker rm -f "$old" >/dev/null
            fi
            restore_backup "$rollback_backup" "$previous_image"
        elif [ -n "$old" ]; then
            docker rename "$old" "$CONTAINER_NAME"
            docker start "$CONTAINER_NAME" >/dev/null
            wait_healthy
        fi
        transaction_active=0
    }
    finish_deploy() {
        status=$?
        trap - EXIT HUP INT TERM
        if [ "$transaction_active" -eq 1 ]; then
            set +e
            (set -e; recover_deploy)
            recovery_status=$?
            set -e
            [ "$recovery_status" -eq 0 ] || info "Automatic deployment recovery failed; use the verified backup: $rollback_backup"
        fi
        exit "$status"
    }
    trap finish_deploy EXIT
    trap 'exit 129' HUP
    trap 'exit 130' INT
    trap 'exit 143' TERM

    if docker inspect "$CONTAINER_NAME" >/dev/null 2>&1; then
        old="${CONTAINER_NAME}-rollback-$(date +%Y%m%d%H%M%S)"
        previous_image=$(docker inspect -f '{{.Config.Image}}' "$CONTAINER_NAME")
        info "Preserving current container as $old"
        transaction_active=1
        docker stop -t 20 "$CONTAINER_NAME" >/dev/null
        docker rename "$CONTAINER_NAME" "$old"
    elif docker volume inspect "$VOLUME_NAME" >/dev/null 2>&1; then
        safety_volume="${VOLUME_NAME}-install-safety-$(date +%Y%m%d%H%M%S)-$$"
        transaction_active=1
        docker volume create "$safety_volume" >/dev/null
        if ! docker run --rm --entrypoint /bin/sh -v "$VOLUME_NAME:/source:ro" -v "$safety_volume:/safety" "$image" \
            -c 'set -o pipefail; cd /source && tar -cf - . | tar -xf - -C /safety'; then
            docker volume rm "$safety_volume" >/dev/null 2>&1 || true
            die "could not preserve the existing data volume before install"
        fi
        safety_complete=1
    fi

    # First installs have no previous container, but a failed candidate must
    # still be removed by the transaction trap.
    transaction_active=1
    run_container "$image"
    wait_healthy || die "new container failed health checks; automatic recovery started"

    if [ -n "$old" ]; then
        docker rm "$old" >/dev/null
    fi
    transaction_active=0
    trap - EXIT HUP INT TERM
    if [ -n "$safety_volume" ]; then
        docker volume rm "$safety_volume" >/dev/null || die "container is healthy but install safety volume cleanup failed: $safety_volume"
    fi
    info "Container is healthy: image=$image revision=$(docker image inspect -f '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$image")"
)

backup_data() (
    require_runtime
    prepare_env
    docker inspect "$CONTAINER_NAME" >/dev/null 2>&1 || die "container $CONTAINER_NAME does not exist; nothing can be backed up consistently"
    backup_image=$(docker inspect -f '{{.Config.Image}}' "$CONTAINER_NAME")
    mount=$(data_mount)
    mount_type=$(printf '%s\n' "$mount" | awk -F ' \| ' '{print $1}')
    mount_name=$(printf '%s\n' "$mount" | awk -F ' \| ' '{print $2}')
    mount_source=$(printf '%s\n' "$mount" | awk -F ' \| ' '{print $3}')
    case "$mount_type" in
        volume) backup_mount="type=volume,src=$mount_name,dst=/source,readonly" ;;
        bind) backup_mount="type=bind,src=$mount_source,dst=/source,readonly" ;;
        *) die "unsupported /app/data mount type: $mount_type" ;;
    esac
    mkdir -p "$BACKUP_ROOT"
    chmod 700 "$BACKUP_ROOT"
    stamp=$(date +%Y%m%d_%H%M%S)
    target=$(mktemp -d "$BACKUP_ROOT/$stamp.XXXXXX") || die "could not create a unique backup directory"
    chmod 700 "$target"

    was_running=0
    backup_complete=0
    recover_service() {
        status=$?
        trap - EXIT HUP INT TERM
        if [ "$was_running" -eq 1 ] && [ "$backup_complete" -eq 0 ]; then
            if ! docker start "$CONTAINER_NAME" >/dev/null; then
                info "Backup interrupted and the service could not be restarted"
                exit 1
            fi
            if ! wait_healthy; then
                info "Backup interrupted and the restarted service is not healthy"
                exit 1
            fi
        fi
        exit "$status"
    }
    trap recover_service EXIT
    trap 'exit 129' HUP
    trap 'exit 130' INT
    trap 'exit 143' TERM
    if [ "$(docker inspect -f '{{.State.Running}}' "$CONTAINER_NAME" 2>/dev/null || true)" = true ]; then
        was_running=1
        info "Stopping YiMao briefly for a consistent SQLite/WAL backup"
        docker stop -t 20 "$CONTAINER_NAME" >/dev/null
    fi

    if ! docker run --rm --entrypoint /bin/sh --mount "$backup_mount" "$backup_image" \
        -c 'cd /source && tar -czf - .' > "$target/data.tar.gz"; then
        die "volume backup failed"
    fi

    {
        docker inspect -f 'image={{.Config.Image}}' "$CONTAINER_NAME"
        docker inspect -f 'revision={{index .Config.Labels "org.opencontainers.image.revision"}}' "$CONTAINER_NAME"
        docker inspect -f 'network={{.HostConfig.NetworkMode}}' "$CONTAINER_NAME"
        docker inspect -f 'restart={{.HostConfig.RestartPolicy.Name}}' "$CONTAINER_NAME"
    } > "$target/container.state"
    cp "$ENV_FILE" "$target/env.backup"
    chmod 600 "$target/env.backup" "$target/container.state" 2>/dev/null || true
    (cd "$target" && sha256sum data.tar.gz container.state env.backup > SHA256SUMS)
    (cd "$target" && sha256sum -c SHA256SUMS >/dev/null)

    if [ "$was_running" -eq 1 ]; then
        docker start "$CONTAINER_NAME" >/dev/null
        wait_healthy || die "backup succeeded but service did not recover"
    fi
    backup_complete=1
    trap - EXIT HUP INT TERM
    info "Backup verified: $target"
    printf '%s\n' "$target"
)

restore_backup() (
    require_runtime
    if docker inspect "$CONTAINER_NAME" >/dev/null 2>&1; then
        require_managed_data_volume
    fi
    target=${1:-}
    requested_image=${2:-}
    [ -n "$target" ] || die "usage: ./manage.sh rollback BACKUP_DIR"
    [ -d "$target" ] || die "backup directory not found: $target"
    target=$(CDPATH= cd -- "$target" && pwd)
    (cd "$target" && sha256sum -c SHA256SUMS) || die "backup checksum verification failed"
    image=$(awk -F= '$1 == "image" {print substr($0,index($0,"=")+1)}' "$target/container.state")
    [ -n "$image" ] && docker image inspect "$image" >/dev/null 2>&1 || die "rollback image is unavailable: $image"

    stamp=$(date +%Y%m%d%H%M%S)
    restore_volume="${VOLUME_NAME}-restore-$stamp"
    safety_volume="${VOLUME_NAME}-safety-$stamp"
    env_safety=""
    env_existed=0
    volume_existed=0
    current_image="$requested_image"
    if docker inspect "$CONTAINER_NAME" >/dev/null 2>&1; then
        current_image=$(docker inspect -f '{{.Config.Image}}' "$CONTAINER_NAME")
    fi
    if [ -f "$ENV_FILE" ]; then
        env_safety=$(mktemp "${ENV_FILE}.rollback.XXXXXX")
        chmod 600 "$env_safety"
        cp "$ENV_FILE" "$env_safety"
        env_existed=1
    elif [ -n "$current_image" ]; then
        env_safety=$(mktemp "${ENV_FILE}.rollback.XXXXXX")
        chmod 600 "$env_safety"
        docker inspect -f '{{range .Config.Env}}{{println .}}{{end}}' "$CONTAINER_NAME" > "$env_safety"
    fi
    transaction_active=0

    recover_restore() {
        [ "$transaction_active" -eq 1 ] || return 0
        trap - EXIT HUP INT TERM
        info "Rollback interrupted; restoring the pre-rollback release"
        if docker inspect "$CONTAINER_NAME" >/dev/null 2>&1; then
            docker rm -f "$CONTAINER_NAME" >/dev/null
        fi
        if [ "$volume_existed" -eq 1 ] && docker volume inspect "$safety_volume" >/dev/null 2>&1; then
            if docker volume inspect "$VOLUME_NAME" >/dev/null 2>&1; then
                docker volume rm "$VOLUME_NAME" >/dev/null
            fi
            docker volume create "$VOLUME_NAME" >/dev/null
            docker run --rm --entrypoint /bin/sh -v "$safety_volume:/source:ro" -v "$VOLUME_NAME:/restore" "$image" \
                -c 'set -o pipefail; cd /source && tar -cf - . | tar -xf - -C /restore'
        elif docker volume inspect "$VOLUME_NAME" >/dev/null 2>&1; then
            docker volume rm "$VOLUME_NAME" >/dev/null
        fi
        if [ -n "$env_safety" ]; then
            cp "$env_safety" "$ENV_FILE"
            chmod 600 "$ENV_FILE"
        else
            rm -f "$ENV_FILE"
        fi
        if [ -n "$current_image" ]; then
            run_container "$current_image"
            [ "$env_existed" -eq 1 ] || rm -f "$ENV_FILE"
            wait_healthy
        elif [ "$env_existed" -eq 0 ]; then
            rm -f "$ENV_FILE"
        fi
        transaction_active=0
    }
    finish_restore() {
        status=$?
        trap - EXIT HUP INT TERM
        recovered=1
        if [ "$transaction_active" -eq 1 ]; then
            set +e
            (set -e; recover_restore)
            recovery_status=$?
            set -e
            [ "$recovery_status" -eq 0 ] || recovered=0
        fi
        if [ "$recovered" -eq 1 ]; then
            docker volume rm "$restore_volume" "$safety_volume" >/dev/null 2>&1 || true
            [ -z "$env_safety" ] || rm -f "$env_safety"
        else
            info "Automatic rollback recovery failed; safety volume and environment copy were preserved"
        fi
        exit "$status"
    }
    trap finish_restore EXIT
    trap 'exit 129' HUP
    trap 'exit 130' INT
    trap 'exit 143' TERM

    docker volume create "$restore_volume" >/dev/null
    docker run --rm -i --entrypoint /bin/sh -v "$restore_volume:/restore" "$image" \
        -c 'cd /restore && tar -xzf -' < "$target/data.tar.gz"

    if docker volume inspect "$VOLUME_NAME" >/dev/null 2>&1; then
        volume_existed=1
        docker volume create "$safety_volume" >/dev/null
        docker run --rm --entrypoint /bin/sh -v "$VOLUME_NAME:/source:ro" -v "$safety_volume:/safety" "$image" \
            -c 'set -o pipefail; cd /source && tar -cf - . | tar -xf - -C /safety'
    fi

    transaction_active=1
    docker rm -f "$CONTAINER_NAME" >/dev/null 2>&1 || true
    if docker volume inspect "$VOLUME_NAME" >/dev/null 2>&1; then
        docker volume rm "$VOLUME_NAME" >/dev/null
    fi
    docker volume create "$VOLUME_NAME" >/dev/null
    docker run --rm --entrypoint /bin/sh -v "$restore_volume:/source:ro" -v "$VOLUME_NAME:/restore" "$image" \
        -c 'set -o pipefail; cd /source && tar -cf - . | tar -xf - -C /restore'
    cp "$target/env.backup" "$ENV_FILE"
    chmod 600 "$ENV_FILE"
    run_container "$image"
    wait_healthy || die "restored container failed health checks"

    transaction_active=0
    trap - EXIT HUP INT TERM
    docker volume rm "$restore_volume" "$safety_volume" >/dev/null 2>&1 || true
    [ -z "$env_safety" ] || rm -f "$env_safety"
    info "Rollback completed from $target"
)

telegram_menu() {
    require_runtime
    prepare_env
    base=$(get_env MINI_APP_URL)
    [ -n "$base" ] || die "MINI_APP_URL is required, for example https://bot.example.com/miniapp"
    case "$base" in https://*) ;; *) die "MINI_APP_URL must use https://" ;; esac
    case "$base" in *\"*|*\\*|*[[:space:]]*) die "MINI_APP_URL contains unsupported characters" ;; esac
    rev=$(docker inspect -f '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$CONTAINER_NAME" 2>/dev/null || revision)
    separator='?'; case "$base" in *\?*) separator='&' ;; esac
    url="${base}${separator}v=${rev}"
    token=$(get_env TELEGRAM_BOT_TOKEN)
    payload=$(printf '{"menu_button":{"type":"web_app","text":"打开 YiMao","web_app":{"url":"%s"}}}' "$url")
    response=$(printf 'url = "https://api.telegram.org/bot%s/setChatMenuButton"\nsilent\nshow-error\nfail\nheader = "Content-Type: application/json"\ndata = "%s"\n' "$token" "$(printf '%s' "$payload" | sed 's/"/\\"/g')" | curl --config -)
    printf '%s' "$response" | grep -q '"ok":true' || die "Telegram rejected setChatMenuButton"
    info "Telegram default Mini App menu updated for revision $rev"
}

doctor() {
    require_runtime
    prepare_env
    if docker compose version >/dev/null 2>&1 || command -v docker-compose >/dev/null 2>&1; then
        compose config --quiet
    else
        info "Docker Compose is unavailable; skipping the optional Compose rendering check"
    fi
    "$PROJECT_DIR/scripts/preflight.sh" --env-file "$ENV_FILE" --engine docker
    docker inspect "$CONTAINER_NAME" >/dev/null 2>&1 || die "container $CONTAINER_NAME does not exist"
    network=$(docker inspect -f '{{.HostConfig.NetworkMode}}' "$CONTAINER_NAME")
    restart=$(docker inspect -f '{{.HostConfig.RestartPolicy.Name}}' "$CONTAINER_NAME")
    mounts=$(docker inspect -f '{{range .Mounts}}{{println .Destination}}{{end}}' "$CONTAINER_NAME")
    [ "$network" = host ] || die "container network is $network, expected host"
    [ "$restart" = unless-stopped ] || die "restart policy is $restart, expected unless-stopped"
    printf '%s\n' "$mounts" | grep -qx /app/data || die "data volume is not mounted"
    if printf '%s\n' "$mounts" | grep -qx /var/run/docker.sock; then
        die "Docker socket must not be mounted in the production request bot"
    fi
    wait_healthy || die "health check failed"
    info "Doctor passed: config, build, topology and dependencies are healthy"
}

install_service() {
    require_runtime
    prepare_env
    if docker inspect "$CONTAINER_NAME" >/dev/null 2>&1; then
        require_managed_data_volume
    fi
    preflight
    image=$(build_image) || die "verified image build failed"
    rollback_backup=""
    if docker inspect "$CONTAINER_NAME" >/dev/null 2>&1; then
        rollback_backup=$(backup_data) || die "pre-deployment backup failed"
    fi
    deploy_image "$image" "$rollback_backup"
    if [ -n "$(get_env MINI_APP_URL)" ]; then telegram_menu; fi
    info "Install complete. Send /start to the Bot, then /link USERNAME or /link USERNAME PASSWORD."
}

update_service() {
    require_runtime
    prepare_env
    [ -z "$(git status --porcelain)" ] || die "working tree contains tracked or untracked changes; commit or stash before update"
    require_managed_data_volume
    git fetch --prune origin
    upstream=$(git rev-parse --abbrev-ref --symbolic-full-name '@{u}' 2>/dev/null || true)
    [ -n "$upstream" ] || die "current branch has no upstream"
    git merge --ff-only "$upstream"
    preflight
    image=$(build_image) || die "verified image build failed"
    rollback_backup=$(backup_data) || die "pre-deployment backup failed"
    deploy_image "$image" "$rollback_backup"
    if [ -n "$(get_env MINI_APP_URL)" ]; then telegram_menu; fi
    info "Update complete"
}

uninstall_service() {
    delete_data=0
    [ "${1:-}" != --delete-data ] || delete_data=1
    docker rm -f "$CONTAINER_NAME" >/dev/null 2>&1 || true
    if [ "$delete_data" -eq 1 ]; then
        docker volume rm "$VOLUME_NAME"
        info "Application and data volume removed"
    else
        info "Application removed; data volume $VOLUME_NAME was preserved"
    fi
}

show_help() {
    cat <<'EOF'
Usage: ./manage.sh COMMAND

  install              validate, build, start and wait for health
  backup               stop briefly and back up the active /app/data mount safely
  update               fast-forward, verify, back up and transactionally deploy
  rollback BACKUP_DIR  restore image, environment and data from a verified backup
  telegram             set the default Telegram Mini App menu and revision
  uninstall            remove the app but preserve the data volume
  doctor               validate config, build, topology and health
  status               show container status and immutable revision
  health               wait for Docker and HTTP health checks
  logs                 follow application logs
  restart              restart and wait for health
  start                 start the existing container
  stop                  stop the existing container
  preflight             run tests and config validation without deployment
  compose               print the resolved Compose configuration
EOF
}

case "${1:-help}" in
    install) install_service ;;
    backup) backup_data ;;
    update) update_service ;;
    rollback) shift; restore_backup "${1:-}" ;;
    telegram) telegram_menu ;;
    uninstall) shift; uninstall_service "${1:-}" ;;
    doctor) doctor ;;
    status) docker inspect -f 'name={{.Name}} image={{.Config.Image}} revision={{index .Config.Labels "org.opencontainers.image.revision"}} state={{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}} restarts={{.RestartCount}}' "$CONTAINER_NAME" ;;
    health) require_runtime; wait_healthy || die "health check failed"; info "Health check passed" ;;
    logs|logs-f) docker logs --tail 100 -f "$CONTAINER_NAME" ;;
    restart) docker restart "$CONTAINER_NAME" >/dev/null; wait_healthy ;;
    start) docker start "$CONTAINER_NAME" ;;
    stop) docker stop -t 20 "$CONTAINER_NAME" ;;
    preflight) require_runtime; preflight ;;
    compose) prepare_env; compose config ;;
    help|--help|-h) show_help ;;
    *) show_help >&2; exit 2 ;;
esac
