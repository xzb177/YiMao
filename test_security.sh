#!/bin/bash
# Test script for Emby link leak detection

# Group chat ID (from .env)
CHAT_ID="-1002306960410"
BOT_TOKEN="8419558809:AAH7oe0_PWRWbhpos3zUvZOp5cbVk-SG59Q"

echo "=== Testing Emby Link Leak Detection ==="
echo ""
echo "This will test the security checker by sending a test message with an Emby link in the caption."
echo "The message should be automatically deleted and a warning sent."
echo ""

# Test 1: Send photo with Emby link in caption
echo "Test 1: Sending photo with Emby link in caption..."

# Note: We can't easily test the actual webhook without a real file,
# but we can test by sending a message directly to the group

# Send a test text message with the link to see if it gets detected
# (The current implementation only checks media captions, not text messages)
echo ""
echo "Sending test message to group..."
echo ""

curl -s -X POST "https://api.telegram.org/bot${BOT_TOKEN}/sendMessage" \
  -H "Content-Type: application/json" \
  -d "{
    \"chat_id\": \"${CHAT_ID}\",
    \"text\": \"🧪 测试消息：emby.oceancloud.asia\"
  }" | jq .

echo ""
echo "Note: The security checker only monitors media content (photo/video/document) with captions."
echo "Text messages are not currently checked."
echo ""
echo "To fully test this feature, you would need to:"
echo "1. Upload an image to the Telegram group"
echo "2. Add a caption containing 'emby.oceancloud.asia' or ':8096'"
echo "3. Verify the message gets deleted"
echo ""
