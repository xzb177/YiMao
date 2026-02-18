#!/bin/bash

# 测试 Telegram 发送
BOT_TOKEN="8531551566:AAEXi5BSyFNmrZniZlK28kr8QU4bbLOLymI"
CHAT_ID="-1002306960410"

echo "=== 测试 1: 发送简单消息 ==="
curl -s -X POST "https://api.telegram.org/bot$BOT_TOKEN/sendMessage" \
  -H "Content-Type: application/json" \
  -d "{\"chat_id\": \"$CHAT_ID\", \"text\": \"🧪 测试消息 1 - 简单文本\"}" | head -c 200

echo -e "\n\n=== 测试 2: 发送 Markdown 消息 ==="
curl -s -X POST "https://api.telegram.org/bot$BOT_TOKEN/sendMessage" \
  -H "Content-Type: application/json" \
  -d "{\"chat_id\": \"$CHAT_ID\", \"text\": \"🎬 *新电影入库*\n\n📦 名称: 测试电影\n📅 年份: 2024\", \"parse_mode\": \"Markdown\"}" | head -c 200

echo -e "\n\n=== 测试 3: Webhook 服务健康检查 ==="
curl -s http://localhost:8080/health

echo -e "\n\n=== 测试 4: 发送 Webhook (电影) ==="
curl -s -X POST http://localhost:8080/webhook \
  -H "Content-Type: application/json" \
  -d '{"Event": "item.added", "ItemType": "Movie", "ItemName": "奥本海默", "Year": 2023, "ItemID": "test123"}'

echo -e "\n\n=== 测试 5: 发送 Webhook (剧集) ==="
curl -s -X POST http://localhost:8080/webhook \
  -H "Content-Type: application/json" \
  -d '{"Event": "item.added", "ItemType": "Episode", "ItemName": "试播集", "ParentName": "绝命毒师", "SeasonName": "第一季", "IndexNumber": 1, "ItemID": "test456"}'

echo -e "\n\n=== 完成 ==="
