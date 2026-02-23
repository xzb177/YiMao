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

echo -e "\n\n=== 测试 4: 发送 Emby Webhook (电影) ==="
curl -s -X POST http://localhost:8080/webhook/emby?type=emby \
  -H "Content-Type: application/json" \
  -d '{"NotificationType": "ItemAdded", "ItemType": "Movie", "ItemName": "奥本海默", "Year": 2023, "ItemID": "test123", "LibraryName": "电影库"}'

echo -e "\n\n=== 测试 5: 发送 Emby Webhook (剧集单集) ==="
curl -s -X POST http://localhost:8080/webhook/emby?type=emby \
  -H "Content-Type: application/json" \
  -d '{"NotificationType": "ItemAdded", "ItemType": "Episode", "ItemName": "第1集", "ParentName": "绝命毒师", "SeasonName": "第一季", "IndexNumber": 1, "ItemID": "test456", "SeriesName": "绝命毒师", "SeasonNumber": 1, "SeriesId": "test789"}'

echo -e "\n\n=== 测试 6: 发送 Emby Webhook (系列) ==="
curl -s -X POST http://localhost:8080/webhook/emby?type=emby \
  -H "Content-Type: application/json" \
  -d '{"NotificationType": "SeriesAdded", "ItemType": "Series", "Name": "测试剧集", "Year": 2024, "SeriesId": "test789", "SeasonNumber": 1}'

echo -e "\n\n=== 测试 7: 发送 Emby Webhook (模拟真实 Emby 格式) ==="
curl -s -X POST http://localhost:8080/webhook/emby?type=emby \
  -H "Content-Type: application/json" \
  -d '{
    "NotificationType": "ItemAdded",
    "ItemId": "1271549",
    "ItemType": "Movie",
    "ItemName": "沙丘",
    "Kind": "Movie",
    "RunTimeTicks": 99470710000,
    "ProductionYear": 2024,
    "PremiereDate": "2024-03-01T00:00:00Z",
    "ExternalUrls": {
      "https://emby.oceancloud.asia/items/1271549"
    }
  }'

echo -e "\n\n=== 完成 ==="
