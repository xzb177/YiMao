#!/bin/sh
set -eu

REPOSITORY_URL=${YIMAO_REPOSITORY_URL:-https://github.com/xzb177/YiMao.git}
INSTALL_DIR=${INSTALL_DIR:-/opt/YiMao}
BRANCH=${YIMAO_BRANCH:-master}

command -v docker >/dev/null 2>&1 || { echo "Docker is required" >&2; exit 1; }
command -v git >/dev/null 2>&1 || { echo "Git is required" >&2; exit 1; }

if [ ! -d "$INSTALL_DIR/.git" ]; then
    parent=$(dirname "$INSTALL_DIR")
    if [ -w "$parent" ]; then
        git clone --branch "$BRANCH" --single-branch "$REPOSITORY_URL" "$INSTALL_DIR"
    else
        command -v sudo >/dev/null 2>&1 || { echo "$parent is not writable and sudo is unavailable" >&2; exit 1; }
        sudo mkdir -p "$parent"
        sudo git clone --branch "$BRANCH" --single-branch "$REPOSITORY_URL" "$INSTALL_DIR"
        sudo chown -R "$(id -u):$(id -g)" "$INSTALL_DIR"
    fi
fi

cd "$INSTALL_DIR"
exec ./manage.sh install