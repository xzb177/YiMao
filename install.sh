#!/bin/bash
# YiMao 一键安装脚本
# 使用方法: curl -fsSL https://raw.githubusercontent.com/xzb177/YiMao/master/install.sh | bash

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 检查命令是否存在
command_exists() {
    command -v "$1" >/dev/null 2>&1
}

# 打印信息
info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

error() {
    echo -e "${RED}[ERROR]${NC} $1"
    exit 1
}

# 检查依赖
info "检查依赖..."
if ! command_exists docker; then
    error "Docker 未安装，请先安装 Docker"
fi

# 检查 docker compose (支持 v1 和 v2 语法)
DOCKER_COMPOSE_CMD=""
if docker compose version >/dev/null 2>&1; then
    DOCKER_COMPOSE_CMD="docker compose"
    info "检测到 Docker Compose v2"
elif command_exists docker-compose; then
    DOCKER_COMPOSE_CMD="docker-compose"
    info "检测到 Docker Compose v1"
else
    error "Docker Compose 未安装，请先安装 Docker Compose"
fi

if ! command_exists git; then
    error "Git 未安装，请先安装 Git"
fi

# 克隆仓库
INSTALL_DIR="${INSTALL_DIR:-/opt/YiMao}"
if [ -d "$INSTALL_DIR" ]; then
    warn "目录 $INSTALL_DIR 已存在，正在更新..."
    cd "$INSTALL_DIR"
    git pull
else
    info "克隆仓库到 $INSTALL_DIR ..."
    sudo mkdir -p "$INSTALL_DIR"
    sudo git clone https://github.com/xzb177/YiMao.git "$INSTALL_DIR"
    cd "$INSTALL_DIR"
fi

# 创建 .env 文件
if [ ! -f .env ]; then
    info "创建配置文件 .env ..."
    cp .env.example .env

    # 自动设置 PUID/PGID 为当前用户
    CURRENT_UID=$(id -u)
    CURRENT_GID=$(id -g)
    echo "" >> .env
    echo "# 自动设置的用户权限" >> .env
    echo "PUID=$CURRENT_UID" >> .env
    echo "PGID=$CURRENT_GID" >> .env

    echo ""
    warn "请编辑 .env 文件，填写以下必需配置："
    echo "  - TELEGRAM_BOT_TOKEN (从 @BotFather 获取)"
    echo "  - MOVIEPILOT_URL (MoviePilot 地址)"
    echo "  - MOVIEPILOT_API_KEY (MoviePilot API Key)"
    echo ""
    info "管理员配置：启动机器人后，私聊使用 /link 用户名 密码 绑定账号"
    echo ""
    info "已自动配置 PUID=$CURRENT_UID PGID=$CURRENT_GID"
    echo ""
    read -p "按回车键继续，或按 Ctrl+C 取消..." </dev/tty
else
    info ".env 文件已存在，跳过"
fi

# 创建数据目录和初始化文件
info "初始化数据目录..."
sudo mkdir -p data

# 获取当前用户 UID/GID
CURRENT_UID=$(id -u)
CURRENT_GID=$(id -g)
CURRENT_USER=$(whoami)

sudo chown -R $CURRENT_USER:$CURRENT_USER data

info "数据目录权限：$CURRENT_USER (UID:$CURRENT_UID, GID:$CURRENT_GID)"

# 初始化 JSON 文件（如果不存在）
init_json_file() {
    local file="$1"
    local content="$2"
    if [ ! -f "$file" ]; then
        echo "$content" | sudo tee "$file" >/dev/null
        sudo chown $(whoami):$(whoami) "$file"
    fi
}

init_json_file "data/admins.json" '{"admins":{},"root_id":0}'
init_json_file "data/user_mappings.json" '{"user_mappings":{},"usernames":{},"reverse_mappings":{}}'
init_json_file "data/user_quotas.json" '{"quotas":{}}'
init_json_file "data/preferences.json" '{"preferences":{}}'
init_json_file "data/binding_requests.json" '{}'
init_json_file "data/media_notifications.json" '{"settings":{}}'
init_json_file "data/feedback.json" '{}'
init_json_file "data/review_requests.json" '{}'
init_json_file "data/search_history.json" '{}'

# 启动服务
info "启动 Docker 服务..."
$DOCKER_COMPOSE_CMD up -d --build

info ""
info "=========================================="
info "安装完成！"
info "=========================================="
info "查看日志: docker logs -f yimao"
info "停止服务: $DOCKER_COMPOSE_CMD down"
info "重启服务: $DOCKER_COMPOSE_CMD restart"
info "查看状态: $DOCKER_COMPOSE_CMD ps"
info "=========================================="
