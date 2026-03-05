#!/bin/bash
# YiMao 一键部署脚本
# 用于快速部署 Golem 运维助手

set -e

echo "======================================"
echo "   YiMao 运维助手 - 一键部署"
echo "======================================"
echo ""

# 检查是否为 root 用户
if [ "$EUID" -ne 0 ]; then
    echo "请使用 root 权限运行此脚本"
    exit 1
fi

# 获取项目目录
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# 检查 Node.js
echo "1. 检查环境..."
if ! command -v node &> /dev/null; then
    echo "错误: 未安装 Node.js，请先安装"
    exit 1
fi
echo "   Node.js 版本: $(node -v)"

# 安装依赖
echo ""
echo "2. 安装依赖..."
npm install

# 检查 .env 文件
echo ""
echo "3. 配置环境变量..."
if [ ! -f .env ]; then
    if [ -f .env.example ]; then
        cp .env.example .env
        echo "   已创建 .env 文件，请填入你的 TELEGRAM_BOT_TOKEN"
        echo "   编辑命令: nano .env"
    else
        echo "   警告: 未找到 .env.example"
    fi
else
    echo "   .env 文件已存在"
fi

# 设置可执行权限
echo ""
echo "4. 设置权限..."
chmod +x yimao.sh
chmod +x deploy.sh
chmod +x update.sh

# 检查 Golem 配置
echo ""
echo "5. 检查 Golem 配置..."
if [ -f golem.yaml ]; then
    if grep -q "\${TELEGRAM_BOT_TOKEN}" golem.yaml; then
        echo "   golem.yaml 配置正确（使用环境变量）"
    else
        echo "   警告: golem.yaml 可能包含硬编码的 Token"
    fi
else
    echo "   错误: 未找到 golem.yaml"
    exit 1
fi

echo ""
echo "======================================"
echo "   部署完成！"
echo "======================================"
echo ""
echo "下一步操作："
echo "1. 编辑 .env 文件，填入 TELEGRAM_BOT_TOKEN"
echo "2. 启动服务: npm start"
echo "3. 或使用更新脚本: ./update.sh"
echo ""
