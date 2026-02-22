#!/bin/bash

# 颜色定义
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${YELLOW}=== Emby Telegram Bot 新架构测试 ===${NC}"
echo ""

# 检查二进制文件
if [ ! -f "./emby-telegram-bot-new" ]; then
    echo -e "${RED}❌ 二进制文件不存在${NC}"
    exit 1
fi
echo -e "${GREEN}✅ 二进制文件存在${NC}"

# 检查端口 8081 是否被占用
if lsof -i :8081 > /dev/null 2>&1; then
    echo -e "${YELLOW}⚠️  端口 8081 已被占用，尝试停止旧进程...${NC}"
    pkill -f "emby-telegram-bot-new" 2>/dev/null
    sleep 2
fi

# 设置环境变量并启动服务器（后台运行）
echo ""
echo -e "${YELLOW}启动新架构服务器...${NC}"
export TELEGRAM_BOT_TOKEN=8419558809:AAH7oe0_PWRWbhpos3zUvZOp5cbVk-SG59Q
export TELEGRAM_CHAT_ID=-1002306960410
export PORT=8081
export HOST=0.0.0.0
export JELLYSEERR_URL=https://embyrequest.oceancloud.asia
export JELLYSEERR_API_KEY=MTc0MTc0MjU1NTg4MzU4MTY2YjUwLTUwNTgtNGYzYy1iNDgxLWI2OGUyODBjYWVjNA==
export EMBY_URL=https://emby.oceancloud.asia
export EMBY_API_KEY=af3fd5f8bb4247f696db24d9471d40d9
export DATA_DIR=/app/data
export ENABLE_AI=true
export ZHIPU_API_KEY=5c266097bade4e3cbc5bc80804431c52.c0Q8VQh8NOHioyf6

./emby-telegram-bot-new > /tmp/bot-new.log 2>&1 &
BOT_PID=$!

echo "服务器 PID: $BOT_PID"

# 等待服务器启动
sleep 3

# 检查进程是否运行
if ps -p $BOT_PID > /dev/null; then
    echo -e "${GREEN}✅ 服务器已启动${NC}"
else
    echo -e "${RED}❌ 服务器启动失败${NC}"
    cat /tmp/bot-new.log
    exit 1
fi

# 测试健康检查
echo ""
echo -e "${YELLOW}测试健康检查接口...${NC}"
HEALTH=$(curl -s http://localhost:8081/health)
if [ "$HEALTH" = "OK" ]; then
    echo -e "${GREEN}✅ 健康检查通过${NC}"
else
    echo -e "${RED}❌ 健康检查失败: $HEALTH${NC}"
fi

# 测试调试接口
echo ""
echo -e "${YELLOW}测试调试接口...${NC}"
DEBUG=$(curl -s http://localhost:8081/debug)
echo "会话信息: $DEBUG"

# 显示日志
echo ""
echo -e "${YELLOW}=== 服务器日志 (最近20行) ===${NC}"
tail -20 /tmp/bot-new.log

echo ""
echo -e "${GREEN}=== 测试完成 ===${NC}"
echo -e "服务器运行中，PID: $BOT_PID"
echo -e "停止命令: kill $BOT_PID"
