#!/bin/bash
# YiMao 一键更新部署脚本
# 用于拉取最新代码并重新部署

set -e

echo "======================================"
echo "   YiMao 运维助手 - 一键更新"
echo "======================================"
echo ""

# 获取项目目录
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# 备份当前配置
echo "1. 备份配置..."
[ -f .env ] && cp .env .env.backup.$(date +%Y%m%d_%H%M%S)
[ -f golem.yaml ] && cp golem.yaml golem.yaml.backup.$(date +%Y%m%d_%H%M%S)
echo "   配置已备份"

# 拉取最新代码
echo ""
echo "2. 拉取最新代码..."
git fetch origin
git reset --hard origin/main
echo "   代码已更新"

# 安装/更新依赖
echo ""
echo "3. 更新依赖..."
npm install
echo "   依赖已更新"

# 恢复配置（如果需要）
echo ""
echo "4. 检查配置..."
if [ ! -f .env ] && [ -f .env.example ]; then
    cp .env.example .env
    echo "   已创建新的 .env 文件，请重新配置"
fi

# 设置权限
echo ""
echo "5. 设置权限..."
chmod +x yimao.sh
chmod +x deploy.sh
chmod +x update.sh

echo ""
echo "======================================"
echo "   更新完成！"
echo "======================================"
echo ""
echo "如需重启服务，请执行："
echo "  npm start"
echo ""
