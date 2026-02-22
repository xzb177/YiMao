#!/bin/bash
# Emby Telegram Bot Docker Management Script
# This script manages the bot container - always use this to manage the bot

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

CONTAINER_NAME="emby-telegram-bot"
IMAGE_NAME="emby-telegram-bot-emby-telegram-bot"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Show usage
show_usage() {
    cat << EOF
Emby Telegram Bot Docker Management Script

Usage: $0 [command]

Commands:
    start       Start the bot container
    stop        Stop the bot container
    restart     Restart the bot container
    status      Show container status
    logs        Show container logs (last 50 lines)
    logs-f      Follow container logs in real-time
    build       Rebuild the container (no cache)
    rebuild     Rebuild and restart the container
    update      Pull latest code, rebuild and restart
    shell       Open a shell inside the container
    exec        Execute a command inside the container
    clean       Remove stopped containers and dangling images
    help        Show this help message

EOF
}

# Check if container is running
is_running() {
    docker ps --format '{{.Names}}' | grep -q "^${CONTAINER_NAME}$"
}

# Check if container exists
container_exists() {
    docker ps -a --format '{{.Names}}' | grep -q "^${CONTAINER_NAME}$"
}

# Start container
cmd_start() {
    if is_running; then
        log_warn "Container is already running"
        return 0
    fi

    log_info "Starting container..."
    docker compose up -d
    log_info "Container started"
}

# Stop container
cmd_stop() {
    if ! is_running; then
        log_warn "Container is not running"
        return 0
    fi

    log_info "Stopping container..."
    docker compose down
    log_info "Container stopped"
}

# Restart container
cmd_restart() {
    log_info "Restarting container..."
    docker compose restart
    log_info "Container restarted"
}

# Show status
cmd_status() {
    echo "=== Container Status ==="
    if container_exists; then
        docker ps -a --filter "name=${CONTAINER_NAME}" --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"
    else
        echo "Container does not exist"
    fi

    echo ""
    echo "=== Recent Logs ==="
    if is_running; then
        docker logs --tail 10 "${CONTAINER_NAME}" 2>&1
    else
        echo "Container is not running"
    fi
}

# Show logs
cmd_logs() {
    if ! is_running; then
        log_error "Container is not running"
        return 1
    fi
    docker logs --tail 50 "${CONTAINER_NAME}" 2>&1
}

# Follow logs
cmd_logs_f() {
    if ! is_running; then
        log_error "Container is not running"
        return 1
    fi
    docker logs -f "${CONTAINER_NAME}" 2>&1
}

# Build container
cmd_build() {
    log_info "Building container (no cache)..."
    docker compose build --no-cache
    log_info "Build complete"
}

# Rebuild and restart
cmd_rebuild() {
    log_info "Rebuilding container..."
    docker compose build --no-cache
    log_info "Restarting with new image..."
    docker compose up -d --force-recreate
    log_info "Rebuild complete"
}

# Update and rebuild
cmd_update() {
    log_info "Pulling latest code..."
    git pull || log_warn "Git pull failed, continuing..."

    log_info "Rebuilding container..."
    cmd_rebuild
}

# Open shell
cmd_shell() {
    if ! is_running; then
        log_error "Container is not running"
        return 1
    fi
    docker exec -it "${CONTAINER_NAME}" /bin/sh
}

# Execute command
cmd_exec() {
    if ! is_running; then
        log_error "Container is not running"
        return 1
    fi
    docker exec -it "${CONTAINER_NAME}" "$@"
}

# Clean up
cmd_clean() {
    log_info "Removing stopped containers..."
    docker container prune -f

    log_info "Removing dangling images..."
    docker image prune -f

    log_info "Cleanup complete"
}

# Main command dispatcher
case "${1:-}" in
    start)
        cmd_start
        ;;
    stop)
        cmd_stop
        ;;
    restart)
        cmd_restart
        ;;
    status)
        cmd_status
        ;;
    logs)
        cmd_logs
        ;;
    logs-f)
        cmd_logs_f
        ;;
    build)
        cmd_build
        ;;
    rebuild)
        cmd_rebuild
        ;;
    update)
        cmd_update
        ;;
    shell)
        cmd_shell
        ;;
    exec)
        shift
        cmd_exec "$@"
        ;;
    clean)
        cmd_clean
        ;;
    help|--help|-h|"")
        show_usage
        ;;
    *)
        log_error "Unknown command: $1"
        echo ""
        show_usage
        exit 1
        ;;
esac
