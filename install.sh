#!/bin/sh
set -eu

# YiMao 一键安装：克隆仓库（如需）并交给 ./manage.sh install 完成校验、构建与启动。
#
#   curl -fsSL https://raw.githubusercontent.com/xzb177/YiMao/master/install.sh | sh
#   或： git clone https://github.com/xzb177/YiMao.git /opt/YiMao && cd /opt/YiMao && ./install.sh
#
# 可用环境变量：
#   INSTALL_DIR            安装目录，默认 /opt/YiMao
#   YIMAO_REPOSITORY_URL   仓库地址
#   YIMAO_BRANCH           分支，默认 master

REPOSITORY_URL=${YIMAO_REPOSITORY_URL:-https://github.com/xzb177/YiMao.git}
INSTALL_DIR=${INSTALL_DIR:-/opt/YiMao}
BRANCH=${YIMAO_BRANCH:-master}

info() { printf '[YiMao] %s\n' "$*" >&2; }
die() { printf '[YiMao] 错误：%s\n' "$*" >&2; exit 1; }

# 前置依赖：先一次性报全，避免用户改一个再撞一个。
missing=""
for command in docker git curl sha256sum; do
    command -v "$command" >/dev/null 2>&1 || missing="$missing $command"
done
[ -z "$missing" ] || die "缺少依赖：$missing。请先安装后重试。"

docker info >/dev/null 2>&1 || die "Docker daemon 不可用，或当前用户无权访问。请启动 Docker，或把用户加入 docker 组后重新登录。"

if [ ! -d "$INSTALL_DIR/.git" ]; then
    info "克隆仓库到 $INSTALL_DIR"
    parent=$(dirname "$INSTALL_DIR")
    if [ -w "$parent" ]; then
        mkdir -p "$parent"
        git clone --branch "$BRANCH" --single-branch "$REPOSITORY_URL" "$INSTALL_DIR"
    else
        command -v sudo >/dev/null 2>&1 || die "$parent 不可写且没有 sudo。请手动创建目录或改用 INSTALL_DIR 指定可写路径。"
        sudo mkdir -p "$parent"
        sudo git clone --branch "$BRANCH" --single-branch "$REPOSITORY_URL" "$INSTALL_DIR"
        sudo chown -R "$(id -u):$(id -g)" "$INSTALL_DIR"
    fi
else
    info "检测到已有安装：$INSTALL_DIR"
fi

cd "$INSTALL_DIR"

# 首次运行时 manage.sh install 会生成 .env（权限 0600）并以退出码 2 停下，
# 提示需要填写哪些必填项。这里把后续步骤说清楚，避免用户以为安装失败。
if [ ! -f .env ]; then
    info "首次安装：接下来会生成 .env 模板，填好必填项后再执行 ./manage.sh install"
fi

exec ./manage.sh install
