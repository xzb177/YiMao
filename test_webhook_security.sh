#!/bin/bash
# Test script to simulate webhook with Emby link in media caption

BOT_TOKEN="8419558809:AAH7oe0_PWRWbhpos3zUvZOp5cbVk-SG59Q"
WEBHOOK_URL="http://localhost:8080/telegram-webhook"

echo "=== Testing Emby Link Detection via Webhook ==="
echo ""
echo "Simulating a Telegram webhook update with a photo containing Emby link in caption..."
echo ""

# Create a mock Telegram update with media containing Emby link
# This simulates what Telegram sends when someone posts a photo with caption
MOCK_UPDATE=$(cat <<EOF
{
  "update_id": 1234567,
  "message": {
    "message_id": 9999,
    "from": {
      "id": 5779291957,
      "first_name": "Test",
      "username": "test_user"
    },
    "chat": {
      "id": -1002306960410,
      "type": "supergroup",
      "title": "云海Emby 交流群"
    },
    "photo": [
      {
        "file_id": "AgACAgIAAxkBAAI...",
        "file_size": 1234,
        "width": 800,
        "height": 600
      }
    ],
    "caption": "测试服务器地址 emby.oceancloud.asia:8096 请访问"
  }
}
EOF
)

echo "Sending mock webhook to local server..."
echo ""

# Send the mock update to our local webhook handler
RESPONSE=$(curl -s -X POST "$WEBHOOK_URL" \
  -H "Content-Type: application/json" \
  -d "$MOCK_UPDATE")

echo "Server response: $RESPONSE"
echo ""

# Check if the security checker detected and deleted the message
echo "=== Checking for security logs ==="
tail -20 /tmp/emby-debug.log | grep -i "security\|ocr\|emby\|delete" || echo "No security-related logs found"

echo ""
echo "Note: The security checker performs OCR asynchronously, so:"
echo "1. It will first check the caption (fast path)"
echo "2. If caption contains link, it deletes immediately"
echo "3. Otherwise, it performs OCR on the image (async)"
