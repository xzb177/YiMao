#!/bin/bash
# YiMao Bot 更新脚本
# 新用户友好：自动备份、安全检查、详细提示

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# 颜色
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

# 打印带颜色的消息
print_info() { echo -e "${BLUE}ℹ️  $1${NC}"; }
print_success() { echo -e "${GREEN}✅ $1${NC}"; }
print_warning() { echo -e "${YELLOW}⚠️  $1${NC}"; }
print_error() { echo -e "${RED}❌ $1${NC}"; }
print_step() { echo -e "${CYAN}▶️  $1${NC}"; }

# 检查必要的命令
check_requirements() {
    print_step "检查运行环境..."

    if ! command -v git &> /dev/null; then
        print_error "未安装 git，请先安装: apt-get install git"
        exit 1
    fi

    if ! command -v docker &> /dev/null; then
        print_error "未安装 docker，请先安装 Docker"
        exit 1
    fi

    print_success "运行环境检查通过"
}

# 检查是否是首次运行
check_first_run() {
    if [ ! -f ".env" ]; then
        echo ""
        print_warning "检测到这是首次运行！"
        echo ""
        echo "📋 新用户快速开始："
        echo "   1. 复制配置文件: cp .env.example .env"
        echo "   2. 编辑配置: nano .env"
        echo "   3. 启动服务: docker compose up -d"
        echo ""
        echo "📘 详细指南请查看: https://github.com/xzb177/YiMao#-3-分钟快速上手"
        echo ""
        return 1
    fi
    return 0
}

# 备份数据
backup_data() {
    BACKUP_DIR="./backup-$(date +%Y%m%d-%H%M%S)"
    mkdir -p "$BACKUP_DIR"

    print_step "创建数据备份..."
    echo ""

    local backed_up=false

    # 备份 data 目录
    if [ -d "data" ] && [ "$(ls -A data 2>/dev/null)" ]; then
        cp -r data/ "$BACKUP_DIR/"
        print_success "已备份 data/ 目录"
        backed_up=true
    fi

    # 备份 JSON 配置文件
    for file in preferences.json user_quotas.json user_mappings.json binding_requests.json review_requests.json; do
        if [ -f "$file" ]; then
            cp "$file" "$BACKUP_DIR/"
            print_success "已备份 $file"
            backed_up=true
        fi
    done

    # 备份 .env 文件
    if [ -f ".env" ]; then
        cp .env "$BACKUP_DIR/"
        print_success "已备份 .env 配置"
        backed_up=true
    fi

    if [ "$backed_up" = true ]; then
        echo ""
        print_info "备份位置: $BACKUP_DIR"
    else
        print_warning "没有找到需要备份的数据文件（可能是首次安装）"
    fi
    echo ""
}

# 拉取最新代码
pull_code() {
    print_step "拉取最新代码..."
    echo ""

    # 检查是否有未提交的更改
    if ! git diff-index --quiet HEAD -- 2>/dev/null; then
        print_warning "检测到本地有未提交的更改"
        print_info "这些更改不会被覆盖，但建议在更新前提交"
        echo ""
    fi

    # 获取当前版本
    CURRENT_VERSION=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")

    # 拉取代码
    if git pull origin master 2>/dev/null; then
        NEW_VERSION=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
        print_success "代码已更新"
        echo ""
        print_info "从 $CURRENT_VERSION 更新到 $NEW_VERSION"
    else
        print_error "拉取代码失败"
        print_info "请检查网络连接或仓库地址"
        exit 1
    fi
    echo ""
}

# 显示更新内容
show_changes() {
    print_step "本次更新内容:"
    echo ""

    # 获取提交日志
    COMMITS=$(git log HEAD@{1}..HEAD --oneline 2>/dev/null || true)

    if [ -z "$COMMITS" ]; then
        print_info "已经是最新版本（或首次安装）"
    else
        echo "$COMMITS" | while read -r line; do
            echo "  • $line"
        done
    fi
    echo ""
}

# 检查配置变更
check_config_changes() {
    print_step "检查配置变更..."
    echo ""

    if [ -f ".env.example" ] && [ -f ".env" ]; then
        if ! diff -q .env .env.example &>/dev/null; then
            print_warning "检测到 .env.example 有更新"
            echo ""
            print_info "建议检查是否有新的配置项："
            echo "   diff .env .env.example"
            echo ""
        else
            print_success "配置文件无变更"
        fi
    fi
}

# 构建并启动
build_and_start() {
    print_step "构建 Docker 镜像..."
    echo ""

    if docker compose build 2>&1; then
        print_success "镜像构建成功"
    else
        print_error "镜像构建失败"
        print_info "请查看上面的错误信息"
        exit 1
    fi
    echo ""

    print_step "重启容器..."
    echo ""

    if docker compose up -d --force-recreate 2>&1; then
        print_success "容器已启动"
    else
        print_error "容器启动失败"
        exit 1
    fi
    echo ""
}

# 检查容器状态
check_status() {
    print_step "检查容器状态..."
    echo ""

    sleep 3  # 等待容器启动

    # 获取容器状态
    CONTAINER_STATUS=$(docker ps --filter "name=yimao" --format "{{.Status}}" 2>/dev/null | head -1)

    if [ -n "$CONTAINER_STATUS" ]; then
        if echo "$CONTAINER_STATUS" | grep -q "healthy"; then
            print_success "容器运行正常 (healthy)"
        elif echo "$CONTAINER_STATUS" | grep -q "starting"; then
            print_info "容器正在启动中..."
            print_info "稍后可通过 ./manage.sh logs-f 查看日志"
        else
            print_info "容器状态: $CONTAINER_STATUS"
        fi
    else
        print_warning "未找到运行中的容器"
        print_info "使用 ./manage.sh logs-f 查看详细日志"
    fi
    echo ""
}

# 显示更新后提示
show_post_update_info() {
    echo -e "${GREEN}======================================${NC}"
    echo -e "${GREEN}   更新完成！${NC}"
    echo -e "${GREEN}======================================${NC}"
    echo ""

    echo "📊 容器状态:"
    docker ps --filter "name=yimao" --format "table {{.Names}}\t{{.Status}}" 2>/dev/null || echo "  无法获取状态"
    echo ""

    echo "🔧 常用命令:"
    echo "  ./manage.sh logs-f      # 查看实时日志"
    echo "  ./manage.sh restart     # 重启服务"
    echo "  ./manage.sh status      # 查看状态"
    echo ""

    echo "📱 验证更新:"
    echo "  1. 在 Telegram 中发送 /start 命令"
    echo "  2. 测试搜索功能是否正常"
    echo ""

    echo "📘 需要帮助？"
    echo "  更新指南: https://github.com/xzb177/YiMao/blob/master/UPDATE.md"
    echo "  问题反馈: https://github.com/xzb177/YiMao/issues"
    echo ""

    # 如果是首次安装
    if ! check_first_run 2>/dev/null; then
        echo ""
        print_info "首次运行提示:"
        echo "  1. 等待容器完全启动（约 10 秒）"
        echo "  2. 发送 /start 给你的 Bot"
        echo "  3. 如果管理员未配置，需要先在 .env 中设置 ADMINS"
        echo ""
    fi
}

# 主流程
main() {
    echo -e "${GREEN}======================================${NC}"
    echo -e "${GREEN}   YiMao Bot 更新脚本${NC}"
    echo -e "${GREEN}======================================${NC}"
    echo ""

    # 检查环境
    check_requirements

    # 检查首次运行
    if ! check_first_run; then
        print_info "请先完成首次配置后再运行更新脚本"
        exit 1
    fi

    # 执行更新步骤
    backup_data
    pull_code
    show_changes
    check_config_changes
    build_and_start
    check_status

    # 显示完成信息
    show_post_update_info
}

# 运行主流程
main
