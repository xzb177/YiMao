#!/bin/bash
# YiMao 快捷运维脚本
set -e

# 工作目录
WORK_DIR="/root/YiMao"
cd "$WORK_DIR" || { echo "错误: 无法进入 $WORK_DIR"; exit 1; }

# 日志函数
log() { echo "[$(date '+%Y-%m-%d %H:%M:%S')] $*"; }

# 执行命令
log "执行命令: $*"
"$@"
log "命令完成: $*"
