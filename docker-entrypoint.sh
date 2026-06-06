#!/bin/sh
set -e

# PUID/PGID 动态适配：根据环境变量创建运行用户，解决宿主机权限错配
PUID=${PUID:-1000}
PGID=${PGID:-1000}

echo "[Entrypoint] PUID=$PUID PGID=$PGID"

# 如果 PGID 组不存在则创建
if ! getent group yimao >/dev/null 2>&1; then
    groupadd -g "$PGID" yimao
else
    existing_gid=$(getent group yimao | cut -d: -f3)
    if [ "$existing_gid" != "$PGID" ]; then
        groupmod -g "$PGID" yimao
    fi
fi

# 如果 PUID 用户不存在则创建
if ! getent passwd yimao >/dev/null 2>&1; then
    useradd -u "$PUID" -g "$PGID" -d /app -s /bin/sh yimao
else
    existing_uid=$(id -u yimao 2>/dev/null || echo "0")
    if [ "$existing_uid" != "$PUID" ]; then
        usermod -u "$PUID" yimao
    fi
fi

# 确保 /app/data 归属正确
chown -R yimao:yimao /app/data 2>/dev/null || true

echo "[Entrypoint] Running as $(id yimao)"

# 切换到 yimao 用户执行
exec su-exec yimao "$@"
