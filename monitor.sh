#!/bin/bash
# ========================================
# YiMao 监控告警脚本
# ========================================

PROJECT_DIR="/root/YiMao"
CONTAINER_NAME="yimao"
LOG_FILE="/var/log/yimao_monitor.log"

# 告警阈值
ERROR_THRESHOLD=10        # 错误日志阈值
CPU_THRESHOLD=80          # CPU 阈值 %
MEM_THRESHOLD=80          # 内存阈值 %
DISK_THRESHOLD=90         # 磁盘阈值 %

# 颜色
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log_message() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1" >> "$LOG_FILE"
}

# 检查容器状态
check_container() {
    if ! docker ps | grep -q "$CONTAINER_NAME"; then
        log_message "[ERROR] 容器 $CONTAINER_NAME 未运行"
        echo -e "${RED}✗ 容器未运行${NC}"
        return 1
    fi
    echo -e "${GREEN}✓ 容器运行中${NC}"
    return 0
}

# 检查错误日志
check_errors() {
    local error_count=$(docker logs $CONTAINER_NAME 2>&1 --since 1h | grep -ci "error")
    if [ $error_count -gt $ERROR_THRESHOLD ]; then
        log_message "[WARNING] 检测到 $error_count 个错误日志"
        echo -e "${YELLOW}⚠ 检测到 $error_count 个错误日志${NC}"
    else
        echo -e "${GREEN}✓ 错误日志正常 ($error_count)${NC}"
    fi
}

# 检查资源使用
check_resources() {
    local cpu=$(docker stats $CONTAINER_NAME --no-stream --format "{{.CPUPerc}}" | sed 's/%//')
    local mem_percent=$(docker stats $CONTAINER_NAME --no-stream --format "{{.MemPerc}}" | sed 's/%//')

    # CPU 检查
    if (( $(echo "$cpu > $CPU_THRESHOLD" | bc -l 2>/dev/null || echo 0) )); then
        log_message "[WARNING] CPU 使用率过高: ${cpu}%"
        echo -e "${YELLOW}⚠ CPU 使用率: ${cpu}%${NC}"
    else
        echo -e "${GREEN}✓ CPU 使用率: ${cpu}%${NC}"
    fi

    # 内存检查
    if (( $(echo "$mem_percent > $MEM_THRESHOLD" | bc -l 2>/dev/null || echo 0) )); then
        log_message "[WARNING] 内存使用率过高: ${mem_percent}%"
        echo -e "${YELLOW}⚠ 内存使用率: ${mem_percent}%${NC}"
    else
        echo -e "${GREEN}✓ 内存使用率: ${mem_percent}%${NC}"
    fi
}

# 检查磁盘使用
check_disk() {
    local disk_usage=$(df $PROJECT_DIR | tail -1 | awk '{print $5}' | sed 's/%//')
    if [ $disk_usage -gt $DISK_THRESHOLD ]; then
        log_message "[ERROR] 磁盘使用率过高: ${disk_usage}%"
        echo -e "${RED}✗ 磁盘使用率: ${disk_usage}%${NC}"
    else
        echo -e "${GREEN}✓ 磁盘使用率: ${disk_usage}%${NC}"
    fi
}

# 检查 API 连接
check_apis() {
    source $PROJECT_DIR/.env 2>/dev/null

    # MoviePilot
    if curl -s --connect-timeout 5 "$MOVIEPILOT_URL/api/v1/system/progress" > /dev/null 2>&1; then
        echo -e "${GREEN}✓ MoviePilot 连接正常${NC}"
    else
        log_message "[ERROR] MoviePilot 连接失败"
        echo -e "${RED}✗ MoviePilot 连接失败${NC}"
    fi

    # Emby
    if curl -s --connect-timeout 5 "$EMBY_URL" > /dev/null 2>&1; then
        echo -e "${GREEN}✓ Emby 连接正常${NC}"
    else
        log_message "[ERROR] Emby 连接失败"
        echo -e "${RED}✗ Emby 连接失败${NC}"
    fi
}

# 检查最近的 webhook 活动
check_webhook_activity() {
    local recent=$(docker logs $CONTAINER_NAME 2>&1 --since 10m | grep -c "Detected Emby webhook")
    if [ $recent -eq 0 ]; then
        echo -e "${YELLOW}⚠ 最近 10 分钟无 webhook 活动${NC}"
    else
        echo -e "${GREEN}✓ Webhook 活跃 (${recent} 事件)${NC}"
    fi
}

# 主检查函数
main() {
    echo -e "\n=== YiMao 监控检查 ==="
    echo "时间: $(date '+%Y-%m-%d %H:%M:%S')"
    echo ""

    check_container
    check_errors
    check_resources
    check_disk
    check_apis
    check_webhook_activity

    echo ""
}

# 如果带参数 --loop，则循环监控
if [ "${1:-}" == "--loop" ]; then
    while true; do
        main
        sleep 300  # 5 分钟检查一次
    done
else
    main
fi
