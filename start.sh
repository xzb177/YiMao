#!/bin/bash

# Emby Telegram Bot 启动脚本
# 这个脚本会正确加载环境变量并启动服务

cd /root/emby-telegram-bot

# 从 .env 文件读取并设置环境变量（忽略注释和空行）
export TELEGRAM_BOT_TOKEN=$(grep '^TELEGRAM_BOT_TOKEN=' .env | cut -d'=' -f2-)
export TELEGRAM_CHAT_ID=$(grep '^TELEGRAM_CHAT_ID=' .env | cut -d'=' -f2-)
export PORT=$(grep '^PORT=' .env | cut -d'=' -f2-)
export JELLYSEERR_URL=$(grep '^JELLYSEERR_URL=' .env | cut -d'=' -f2-)
export JELLYSEERR_API_KEY=$(grep '^JELLYSEERR_API_KEY=' .env | cut -d'=' -f2-)
export ADMINS=$(grep '^ADMINS=' .env | cut -d'=' -f2-)

# 检查服务是否已在运行
if [ -f /tmp/emby-bot.pid ]; then
    OLDPID=$(cat /tmp/emby-bot.pid)
    if ps -p $OLDPID > /dev/null 2>&1; then
        echo "Service already running with PID: $OLDPID"
        exit 0
    fi
fi

# 停止旧进程
pkill -f "emby-telegram-bot" 2>/dev/null
sleep 1

# 启动服务（优先使用新版本）
if [ -f "./emby-telegram-bot-new" ]; then
    ./emby-telegram-bot-new > /tmp/emby-debug.log 2>&1 &
else
    ./emby-telegram-bot > /tmp/emby-debug.log 2>&1 &
fi
PID=$!

# 保存 PID
echo $PID > /tmp/emby-bot.pid

echo "Emby Telegram Bot started with PID: $PID"
echo "Log file: /tmp/emby-debug.log"

# 等待2秒验证启动
sleep 2
if ps -p $PID > /dev/null 2>&1; then
    echo "Service is running!"
    exit 0
else
    echo "Service failed to start. Check log file."
    exit 1
fi
