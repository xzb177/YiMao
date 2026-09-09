#!/bin/sh
set -eu

# The entrypoint starts as root only to reconcile named-volume ownership.
# PUID=0 remains an explicit compatibility override; it is never the default.
PUID=${PUID:-10001}
PGID=${PGID:-10001}

case "$PUID" in
    ''|*[!0-9]*)
        echo "[Entrypoint] PUID must be numeric" >&2
        exit 1
        ;;
esac
case "$PGID" in
    ''|*[!0-9]*)
        echo "[Entrypoint] PGID must be numeric" >&2
        exit 1
        ;;
esac

echo "[Entrypoint] PUID=$PUID PGID=$PGID"

if [ "$PUID" = "0" ]; then
    if [ "$PGID" != "0" ]; then
        echo "[Entrypoint] PGID must also be 0 when PUID=0" >&2
        exit 1
    fi
    chown -R root:root /app/data
    echo "[Entrypoint] Running as root (explicit PUID=0 compatibility override)"
    exec "$@"
fi
if [ "$PGID" = "0" ]; then
    echo "[Entrypoint] PGID=0 is not allowed when PUID is non-root" >&2
    exit 1
fi

existing_group=$(getent group "$PGID" | cut -d: -f1 || true)
if [ -n "$existing_group" ] && [ "$existing_group" != "yimao" ]; then
    echo "[Entrypoint] requested PGID $PGID already belongs to group $existing_group" >&2
    exit 1
fi
existing_user=$(getent passwd "$PUID" | cut -d: -f1 || true)
if [ -n "$existing_user" ] && [ "$existing_user" != "yimao" ]; then
    echo "[Entrypoint] requested PUID $PUID already belongs to user $existing_user" >&2
    exit 1
fi

if ! getent group yimao >/dev/null 2>&1; then
    groupadd -g "$PGID" yimao
elif [ "$(getent group yimao | cut -d: -f3)" != "$PGID" ]; then
    groupmod -g "$PGID" yimao
fi

if ! getent passwd yimao >/dev/null 2>&1; then
    useradd -u "$PUID" -g "$PGID" -d /app -s /bin/sh yimao
elif [ "$(id -u yimao)" != "$PUID" ]; then
    usermod -u "$PUID" yimao
fi

# Ensure an existing user follows a changed primary group as well.
usermod -g "$PGID" yimao
chown -R yimao:yimao /app/data

echo "[Entrypoint] Running as $(id yimao)"
exec su-exec yimao "$@"
