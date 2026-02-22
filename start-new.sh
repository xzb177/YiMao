#!/bin/bash
# 设置环境变量
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

echo "启动新架构服务器 (端口 8081)..."
./emby-telegram-bot-new
