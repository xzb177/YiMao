#!/bin/bash
set -euo pipefail

# ========================================
# YiMao 运维管理脚本（路径自适应版）
# 用法: ./scripts/ops.sh [命令]
# ========================================

# 动态获取项目根目录
PROJECT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONTAINER_NAME="yimao"
COMPOSE_FILE="$PROJECT_DIR/docker-compose.yml"

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; BLUE='\033[0;34m'; NC='\033[0m'

cd "$PROJECT_DIR" || exit 1

HEALTH_PORT=8080
if [ -f "$PROJECT_DIR/.env" ]; then
    configured_port=$(awk -F= '$1 == "PORT" { print $2 }' "$PROJECT_DIR/.env" | tail -n 1 | tr -d '[:space:]')
    if [[ "$configured_port" =~ ^[0-9]+$ ]]; then
        HEALTH_PORT="$configured_port"
    fi
fi

compose() {
    if docker compose version >/dev/null 2>&1; then
        docker compose -f "$COMPOSE_FILE" "$@"
    elif command -v docker-compose >/dev/null 2>&1; then
        docker-compose -f "$COMPOSE_FILE" "$@"
    else
        echo -e "${RED}未找到 Docker Compose${NC}" >&2
        return 1
    fi
}

show_help() {
    echo -e "${BLUE}=== YiMao 运维管理脚本 ===${NC}"
    echo ""
    echo "用法: ./scripts/ops.sh [命令]"
    echo ""
    echo "命令:"
    echo "  status      查看服务状态"
    echo "  logs        查看实时日志（logs-f 兼容别名）"
    echo "  logs-error  只看错误日志"
    echo "  shell       进入容器 shell"
    echo "  exec        在容器内执行命令"
    echo "  restart     重启服务"
    echo "  build       部署前验收并构建镜像，不重启"
    echo "  rebuild     部署前验收、重新构建并启动"
    echo "  stop        停止服务"
    echo "  start       启动服务"
    echo "  update      更新代码并重启"
    echo "  clean       清理图片缓存"
    echo "  backup      备份配置和数据"
    echo "  preflight   部署前验收（不启动、不重启服务）"
    echo "  health      健康检查"
    echo ""
}

show_status() {
    echo -e "${BLUE}=== 服务状态 ===${NC}"
    docker ps -a --filter "name=$CONTAINER_NAME" --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"
    echo ""
    echo -e "${BLUE}=== 资源使用 ===${NC}"
    docker stats "$CONTAINER_NAME" --no-stream --format "table {{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}" 2>/dev/null || echo "容器未运行"
    echo ""
}

show_logs() {
    docker logs "$CONTAINER_NAME" --tail 50 -f
}

show_error_logs() {
    docker logs "$CONTAINER_NAME" 2>&1 | grep -i "error\|fail\|panic" | tail -20 || true
}

open_shell() {
    docker exec -it "$CONTAINER_NAME" /bin/sh
}

exec_in_container() {
    if [ $# -eq 0 ]; then
        echo -e "${RED}exec 需要提供命令${NC}" >&2
        return 2
    fi
    docker exec -it "$CONTAINER_NAME" "$@"
}

build_service() {
    echo -e "${YELLOW}先执行部署前验收...${NC}"
    preflight_check
    echo -e "${YELLOW}验收通过，构建镜像...${NC}"
    compose build --no-cache
}

restart_service() {
    echo -e "${YELLOW}重启服务...${NC}"
    docker restart "$CONTAINER_NAME"
    sleep 3
    show_status
}

stop_service() {
    echo -e "${YELLOW}停止服务...${NC}"
    docker stop "$CONTAINER_NAME"
}

start_service() {
    echo -e "${YELLOW}启动服务...${NC}"
    docker start "$CONTAINER_NAME" 2>/dev/null || compose up -d
    sleep 3
    show_status
}

rebuild_service() {
    echo -e "${YELLOW}先执行部署前验收...${NC}"
    preflight_check
    echo -e "${YELLOW}验收通过，备份当前数据...${NC}"
    backup_data
    echo -e "${YELLOW}备份完成，重新构建服务...${NC}"
    compose build --no-cache
    compose up -d --force-recreate
    echo -e "${GREEN}构建完成！${NC}"
    show_status
}

update_code() {
    echo -e "${YELLOW}更新代码...${NC}"
    git fetch origin
    LOCAL=$(git rev-parse @)
    REMOTE=$(git rev-parse @{u})

    if [ "$LOCAL" = "$REMOTE" ]; then
        echo -e "${GREEN}已是最新版本${NC}"
    else
        echo -e "${YELLOW}发现更新，正在拉取...${NC}"
        git pull --ff-only origin master
        echo -e "${GREEN}更新完成，重启服务...${NC}"
        rebuild_service
    fi
}

clean_cache() {
    echo -e "${YELLOW}清理图片缓存...${NC}"
    rm -rf "$PROJECT_DIR"/data/image_cache/*.jpg 2>/dev/null
    echo -e "${GREEN}缓存清理完成${NC}"
}

backup_data() {
    BACKUP_DIR="$PROJECT_DIR/backup-$(date +%Y%m%d_%H%M%S)"
    echo -e "${YELLOW}备份到 $BACKUP_DIR${NC}"
    mkdir -p "$BACKUP_DIR"
    if [ -d "$PROJECT_DIR/data" ]; then
        cp -r "$PROJECT_DIR/data" "$BACKUP_DIR/"
    else
        mkdir -p "$BACKUP_DIR/data"
    fi
    if [ -f "$PROJECT_DIR/.env" ]; then
        cp "$PROJECT_DIR/.env" "$BACKUP_DIR/"
    fi
    echo -e "${GREEN}备份完成${NC}"
}

preflight_check() {
    sh "$PROJECT_DIR/scripts/preflight.sh" --env
}

health_check() {
    echo -e "${BLUE}=== 健康检查 ===${NC}"
    failed=0

    if docker ps --format '{{.Names}}' | grep -qx "$CONTAINER_NAME"; then
        echo -e "${GREEN}✓ 容器运行中${NC}"
    else
        echo -e "${RED}✗ 容器未运行${NC}"
        failed=1
    fi

    if curl -fsS "http://localhost:${HEALTH_PORT}/health" > /dev/null 2>&1; then
        echo -e "${GREEN}✓ HTTP API 正常${NC}"
    else
        echo -e "${RED}✗ HTTP API 异常${NC}"
        failed=1
    fi

    ERROR_COUNT=$(docker logs "$CONTAINER_NAME" --tail 100 2>&1 | grep -ciE 'error|panic' || true)
    if [ "$ERROR_COUNT" -eq 0 ]; then
        echo -e "${GREEN}✓ 最近日志无 error/panic${NC}"
    else
        echo -e "${YELLOW}⚠ 最近日志发现 $ERROR_COUNT 个 error/panic 记录${NC}"
    fi

    return "$failed"
}

case "${1:-help}" in
    status)     show_status ;;
    logs|logs-f) show_logs ;;
    logs-error)  show_error_logs ;;
    shell)       open_shell ;;
    exec)        shift; exec_in_container "$@" ;;
    restart)     restart_service ;;
    build)       build_service ;;
    rebuild)     rebuild_service ;;
    stop)       stop_service ;;
    start)      start_service ;;
    update)     update_code ;;
    clean)      clean_cache ;;
    backup)     backup_data ;;
    preflight)  preflight_check ;;
    health)     health_check ;;
    help|--help|-h) show_help ;;
    *)
        echo -e "${RED}未知命令: $1${NC}"
        show_help
        exit 1
        ;;
esac
