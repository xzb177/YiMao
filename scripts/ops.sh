#!/bin/bash
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

show_help() {
    echo -e "${BLUE}=== YiMao 运维管理脚本 ===${NC}"
    echo ""
    echo "用法: ./scripts/ops.sh [命令]"
    echo ""
    echo "命令:"
    echo "  status      查看服务状态"
    echo "  logs        查看实时日志"
    echo "  logs-error  只看错误日志"
    echo "  restart     重启服务"
    echo "  rebuild     重新构建并启动"
    echo "  stop        停止服务"
    echo "  start       启动服务"
    echo "  update      更新代码并重启"
    echo "  clean       清理图片缓存"
    echo "  backup      备份配置和数据"
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
    docker logs "$CONTAINER_NAME" 2>&1 | grep -i "error\|fail\|panic" | tail -20
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
    docker start "$CONTAINER_NAME" 2>/dev/null || docker-compose up -d
    sleep 3
    show_status
}

rebuild_service() {
    echo -e "${YELLOW}重新构建服务...${NC}"
    docker-compose down
    docker-compose build --no-cache
    docker-compose up -d
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
        git pull origin master
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
    BACKUP_DIR="$PROJECT_DIR/backup_$(date +%Y%m%d_%H%M%S)"
    echo -e "${YELLOW}备份到 $BACKUP_DIR${NC}"
    mkdir -p "$BACKUP_DIR"
    cp -r "$PROJECT_DIR/data" "$BACKUP_DIR/"
    cp "$PROJECT_DIR/.env" "$BACKUP_DIR/" 2>/dev/null
    echo -e "${GREEN}备份完成${NC}"
}

health_check() {
    echo -e "${BLUE}=== 健康检查 ===${NC}"

    if docker ps | grep -q "$CONTAINER_NAME"; then
        echo -e "${GREEN}✓ 容器运行中${NC}"
    else
        echo -e "${RED}✗ 容器未运行${NC}"
    fi

    if curl -s http://localhost:8080/health > /dev/null 2>&1; then
        echo -e "${GREEN}✓ HTTP API 正常${NC}"
    else
        echo -e "${RED}✗ HTTP API 异常${NC}"
    fi

    ERROR_COUNT=$(docker logs "$CONTAINER_NAME" 2>&1 --tail 100 | grep -ci "error")
    if [ "$ERROR_COUNT" -eq 0 ]; then
        echo -e "${GREEN}✓ 最近日志无错误${NC}"
    else
        echo -e "${YELLOW}⚠ 最近日志发现 $ERROR_COUNT 个错误${NC}"
    fi
}

case "${1:-help}" in
    status)     show_status ;;
    logs)       show_logs ;;
    logs-error) show_error_logs ;;
    restart)    restart_service ;;
    rebuild)    rebuild_service ;;
    stop)       stop_service ;;
    start)      start_service ;;
    update)     update_code ;;
    clean)      clean_cache ;;
    backup)     backup_data ;;
    health)     health_check ;;
    help|--help|-h) show_help ;;
    *)
        echo -e "${RED}未知命令: $1${NC}"
        show_help
        exit 1
        ;;
esac
