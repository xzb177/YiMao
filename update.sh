#!/bin/bash
# YiMao Bot 更新脚本
# 这是 ./manage.sh update 的快捷方式，更符合用户习惯

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# 颜色
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

echo -e "${GREEN}======================================${NC}"
echo -e "${GREEN}   YiMao Bot 更新脚本${NC}"
echo -e "${GREEN}======================================${NC}"
echo ""

# 检查备份目录
BACKUP_DIR="./backup-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$BACKUP_DIR"

echo -e "${YELLOW}[1/4] 备份数据...${NC}"
cp -r data/ "$BACKUP_DIR/" 2>/dev/null || true
cp preferences.json "$BACKUP_DIR/" 2>/dev/null || true
cp user_quotas.json "$BACKUP_DIR/" 2>/dev/null || true
cp user_mappings.json "$BACKUP_DIR/" 2>/dev/null || true
cp binding_requests.json "$BACKUP_DIR/" 2>/dev/null || true
cp review_requests.json "$BACKUP_DIR/" 2>/dev/null || true
echo -e "   ✅ 备份完成: $BACKUP_DIR"
echo ""

echo -e "${YELLOW}[2/4] 拉取最新代码...${NC}"
git pull origin master || echo -e "   ${RED}Git pull 失败，继续...${NC}"
echo ""

echo -e "${YELLOW}[3/4] 查看更新内容...${NC}"
git log HEAD@{1}..HEAD --oneline 2>/dev/null || echo "   (首次安装或无法获取历史)"
echo ""

echo -e "${YELLOW}[4/4] 重新构建并启动...${NC}"
docker compose build
docker compose up -d --force-recreate
echo ""

# 等待容器启动
sleep 3

# 检查状态
echo -e "${GREEN}======================================${NC}"
echo -e "${GREEN}   更新完成！${NC}"
echo -e "${GREEN}======================================${NC}"
echo ""

echo "容器状态:"
docker ps --filter "name=emby-telegram-bot" --format "table {{.Names}}\t{{.Status}}"

echo ""
echo "查看日志:"
echo "  ./manage.sh logs-f"
echo ""
echo "如遇问题，请查看更新指南: https://github.com/xzb177/YiMao/blob/master/UPDATE.md"
