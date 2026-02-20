#!/bin/bash
# Emby Telegram Bot Monitor Script
# Monitors the bot service and auto-restarts on failure

BOT_DIR="/root/emby-telegram-bot"
PID_FILE="/tmp/emby-bot.pid"
LOG_FILE="/tmp/emby-debug.log"
CHECK_INTERVAL=300  # Check every 5 minutes

echo "[$(date)] Starting bot monitor..."

while true; do
    # Check if process is running
    if [ -f "$PID_FILE" ]; then
        PID=$(cat "$PID_FILE")
        if ps -p "$PID" > /dev/null 2>&1; then
            # Process is running, check health endpoint
            if curl -s http://127.0.0.1:8080/health > /dev/null 2>&1; then
                echo "[$(date)] Bot is healthy (PID: $PID)"
            else
                echo "[$(date)] WARNING: Health check failed, restarting..."
                kill "$PID" 2>/dev/null
                sleep 2
                cd "$BOT_DIR" && ./start.sh
            fi
        else
            echo "[$(date)] ERROR: Bot process died, restarting..."
            cd "$BOT_DIR" && ./start.sh
        fi
    else
        echo "[$(date)] ERROR: PID file not found, starting bot..."
        cd "$BOT_DIR" && ./start.sh
    fi

    # Check for critical errors in logs
    if tail -100 "$LOG_FILE" | grep -q "panic\|fatal"; then
        echo "[$(date)] CRITICAL: Found panic/fatal in logs, restarting..."
        if [ -f "$PID_FILE" ]; then
            kill $(cat "$PID_FILE") 2>/dev/null
        fi
        sleep 2
        cd "$BOT_DIR" && ./start.sh
    fi

    sleep "$CHECK_INTERVAL"
done
