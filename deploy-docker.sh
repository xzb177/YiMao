#!/bin/bash

# Docker 部署脚本
# 用于将 emby-telegram-bot 迁移到 Docker 容器

set -e

echo "🐳 Emby Telegram Bot Docker 部署脚本"
echo "======================================"

# 检查 Docker 是否安装
if ! command -v docker &> /dev/null; then
    echo "❌ Docker 未安装，请先安装 Docker"
    exit 1
fi

# 检查 Docker Compose 是否安装
if ! command -v docker-compose &> /dev/null && ! docker compose version &> /dev/null; then
    echo "❌ Docker Compose 未安装，请先安装 Docker Compose"
    exit 1
fi

# 检查 .env 文件
if [ ! -f .env ]; then
    echo "❌ .env 文件不存在，请先创建 .env 文件"
    echo "提示: 可以参考 .env.example 创建"
    exit 1
fi

# 停止旧的 systemd 服务
if systemctl is-active --quiet emby-telegram-bot; then
    echo "⏹️  停止旧的 systemd 服务..."
    sudo systemctl stop emby-telegram-bot
    sudo systemctl disable emby-telegram-bot
fi

# 备份数据
echo "💾 备份现有数据..."
BACKUP_DIR="./backup-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$BACKUP_DIR"
cp -f preferences.json "$BACKUP_DIR/" 2>/dev/null || true
cp -f user_quotas.json "$BACKUP_DIR/" 2>/dev/null || true
cp -f user_mapping.json "$BACKUP_DIR/" 2>/dev/null || true
echo "✅ 数据已备份到: $BACKUP_DIR"

# 创建数据目录
mkdir -p data

# 构建镜像
echo "🔨 构建 Docker 镜像..."
docker-compose build

# 启动容器
echo "🚀 启动容器..."
docker-compose up -d

# 显示状态
echo ""
echo "📊 容器状态:"
docker-compose ps

echo ""
echo "✅ 部署完成！"
echo ""
echo "📝 有用的命令:"
echo "  查看日志: docker-compose logs -f"
echo "  停止容器: docker-compose stop"
echo "  重启容器: docker-compose restart"
echo "  查看状态: docker-compose ps"
echo ""
