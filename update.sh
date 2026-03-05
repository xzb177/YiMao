#!/bin/bash
# YiMao 项目一键更新脚本
# 用于快速更新并重启服务

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}======================================${NC}"
echo -e "${GREEN}   YiMao 项目一键更新脚本${NC}"
echo -e "${GREEN}======================================${NC}"
echo ""

# 检查是否有未提交的更改
if [ -n "$(git status --porcelain)" ]; then
    echo -e "${YELLOW}警告: 检测到未提交的更改${NC}"
    echo "正在备份当前更改..."
    BACKUP_DIR="backup_$(date +%Y%m%d_%H%M%S)"
    mkdir -p "$BACKUP_DIR"
    cp -r . "$BACKUP_DIR/" 2>/dev/null || true
    echo -e "${GREEN}备份已保存至: $BACKUP_DIR${NC}"
    echo ""
fi

# 拉取最新代码
echo -e "${YELLOW}[1/4] 拉取最新代码...${NC}"
git fetch origin
git reset --hard origin/master
echo -e "${GREEN}✓ 代码已更新${NC}"
echo ""

# 停止服务
echo -e "${YELLOW}[2/4] 停止当前服务...${NC}"
if command -v docker-compose &> /dev/null; then
    docker-compose down
elif command -v docker &> /dev/null; then
    docker compose down
else
    echo -e "${RED}错误: 未找到 docker-compose 或 docker 命令${NC}"
    exit 1
fi
echo -e "${GREEN}✓ 服务已停止${NC}"
echo ""

# 构建镜像
echo -e "${YELLOW}[3/4] 构建新镜像...${NC}"
if command -v docker-compose &> /dev/null; then
    docker-compose build --no-cache
elif command -v docker &> /dev/null; then
    docker compose build --no-cache
fi
echo -e "${GREEN}✓ 镜像构建完成${NC}"
echo ""

# 启动服务
echo -e "${YELLOW}[4/4] 启动服务...${NC}"
if command -v docker-compose &> /dev/null; then
    docker-compose up -d
elif command -v docker &> /dev/null; then
    docker compose up -d
fi
echo -e "${GREEN}✓ 服务已启动${NC}"
echo ""

# 等待服务就绪
echo -e "${YELLOW}等待服务就绪...${NC}"
sleep 5

# 显示服务状态
echo ""
echo -e "${GREEN}======================================${NC}"
echo -e "${GREEN}   服务状态${NC}"
echo -e "${GREEN}======================================${NC}"
if command -v docker-compose &> /dev/null; then
    docker-compose ps
elif command -v docker &> /dev/null; then
    docker compose ps
fi

echo ""
echo -e "${GREEN}======================================${NC}"
echo -e "${GREEN}   最新日志 (最近15行)${NC}"
echo -e "${GREEN}======================================${NC}"
docker logs --tail 15 emby-telegram-bot 2>&1

echo ""
echo -e "${GREEN}✓ 更新完成！${NC}"
