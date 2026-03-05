#!/bin/bash
# ========================================
# YiMao 运维管理脚本
# ========================================

PROJECT_DIR="/root/YiMao"
CONTAINER_NAME="emby-telegram-bot"
COMPOSE_FILE="$PROJECT_DIR/docker-compose.yml"

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

cd "$PROJECT_DIR" || exit 1

# 显示帮助信息
show_help() {
    echo -e "${BLUE}=== YiMao 运维管理脚本 ===${NC}"
    echo ""
    echo "用法: ./ops.sh [命令]"
    echo ""
    echo "命令:"
    echo "  status      - 查看服务状态"
    echo "  logs        - 查看实时日志"
    echo "  logs-error  - 只查看错误日志"
    echo "  restart     - 重启服务"
    echo "  rebuild     - 重新构建并启动"
    echo "  stop        - 停止服务"
    echo "  start       - 启动服务"
    echo "  update      - 更新代码并重启"
    echo "  clean       - 清理图片缓存"
    echo "  backup      - 备份配置和数据"
    echo "  health      - 健康检查"
    echo "  test        - 测试 API 连接"
    echo ""
}

# 查看状态
show_status() {
    echo -e "${BLUE}=== 服务状态 ===${NC}"
    docker ps -a --filter "name=$CONTAINER_NAME" --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"
    echo ""

    echo -e "${BLUE}=== 资源使用 ===${NC}"
    docker stats $CONTAINER_NAME --no-stream --format "table {{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}\t{{.NetIO}}"
    echo ""

    echo -e "${BLUE}=== 磁盘使用 ===${NC}"
    du -sh $PROJECT_DIR/data 2>/dev/null || echo "数据目录不存在"
    echo ""
}

# 查看日志
show_logs() {
    echo -e "${BLUE}=== 最近 50 条日志 ===${NC}"
    docker logs $CONTAINER_NAME --tail 50 -f
}

# 查看错误日志
show_error_logs() {
    echo -e "${RED}=== 错误日志 ===${NC}"
    docker logs $CONTAINER_NAME 2>&1 | grep -i "error\|fail\|panic" | tail -20
}

# 重启服务
restart_service() {
    echo -e "${YELLOW}重启服务...${NC}"
    docker restart $CONTAINER_NAME
    sleep 3
    show_status
}

# 停止服务
stop_service() {
    echo -e "${YELLOW}停止服务...${NC}"
    docker stop $CONTAINER_NAME
}

# 启动服务
start_service() {
    echo -e "${YELLOW}启动服务...${NC}"
    docker start $CONTAINER_NAME || docker-compose up -d
    sleep 3
    show_status
}

# 重新构建
rebuild_service() {
    echo -e "${YELLOW}重新构建服务...${NC}"
    docker-compose down
    docker-compose build --no-cache
    docker-compose up -d
    echo -e "${GREEN}构建完成！${NC}"
    show_status
}

# 更新代码
update_code() {
    echo -e "${YELLOW}更新代码...${NC}"
    git fetch origin
    LOCAL=$(git rev-parse @)
    REMOTE=$(git rev-parse @{u})

    if [ $LOCAL = $REMOTE ]; then
        echo -e "${GREEN}已是最新版本${NC}"
    else
        echo -e "${YELLOW}发现更新，正在拉取...${NC}"
        git pull origin master
        echo -e "${GREEN}更新完成，重启服务...${NC}"
        rebuild_service
    fi
}

# 清理缓存
clean_cache() {
    echo -e "${YELLOW}清理图片缓存...${NC}"
    rm -rf $PROJECT_DIR/data/image_cache/*.jpg 2>/dev/null
    echo -e "${GREEN}缓存清理完成${NC}"
}

# 备份
backup_data() {
    BACKUP_DIR="$PROJECT_DIR/backup_$(date +%Y%m%d_%H%M%S)"
    echo -e "${YELLOW}备份到 $BACKUP_DIR${NC}"

    mkdir -p $BACKUP_DIR
    cp -r $PROJECT_DIR/data $BACKUP_DIR/
    cp $PROJECT_DIR/.env $BACKUP_DIR/

    echo -e "${GREEN}备份完成${NC}"
}

# 健康检查
health_check() {
    echo -e "${BLUE}=== 健康检查 ===${NC}"

    # 容器状态
    if docker ps | grep -q $CONTAINER_NAME; then
        echo -e "${GREEN}✓ 容器运行中${NC}"
    else
        echo -e "${RED}✗ 容器未运行${NC}"
    fi

    # HTTP 检查
    if curl -s http://localhost:8080/health > /dev/null 2>&1; then
        echo -e "${GREEN}✓ HTTP API 正常${NC}"
    else
        echo -e "${RED}✗ HTTP API 异常${NC}"
    fi

    # 最近错误
    ERROR_COUNT=$(docker logs $CONTAINER_NAME 2>&1 --tail 100 | grep -ci "error")
    if [ $ERROR_COUNT -eq 0 ]; then
        echo -e "${GREEN}✓ 最近日志无错误${NC}"
    else
        echo -e "${YELLOW}⚠ 最近日志发现 $ERROR_COUNT 个错误${NC}"
    fi
}

# 测试 API 连接
test_api() {
    echo -e "${BLUE}=== API 连接测试 ===${NC}"

    source $PROJECT_DIR/.env 2>/dev/null

    echo -n "MoviePilot: "
    if curl -s --connect-timeout 5 "$MOVIEPILOT_URL/api/v1/system/progress" > /dev/null 2>&1; then
        echo -e "${GREEN}✓ 正常${NC}"
    else
        echo -e "${RED}✗ 异常${NC}"
    fi

    echo -n "Emby: "
    if curl -s --connect-timeout 5 "$EMBY_URL" > /dev/null 2>&1; then
        echo -e "${GREEN}✓ 正常${NC}"
    else
        echo -e "${RED}✗ 异常${NC}"
    fi

    echo -n "TMDB: "
    if curl -s --connect-timeout 5 "https://api.themoviedb.org/3/movie/550?api_key=$TMDB_API_KEY" > /dev/null 2>&1; then
        echo -e "${GREEN}✓ 正常${NC}"
    else
        echo -e "${RED}✗ 异常${NC}"
    fi
}

# 主逻辑
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
    test)       test_api ;;
    help|--help|-h) show_help ;;
    *)
        echo -e "${RED}未知命令: $1${NC}"
        show_help
        exit 1
        ;;
esac
