#!/bin/sh
set -eu

PUID="${PUID:-}"
PGID="${PGID:-}"

ensure_group() {
    if ! grep -q '^gonzb:' /etc/group 2>/dev/null; then
        addgroup -g "$PGID" -S gonzb || true
    fi
}

ensure_user() {
    if ! grep -q '^gonzb:' /etc/passwd 2>/dev/null; then
        adduser -u "$PUID" -G gonzb -S -D gonzb || true
    fi
}

fix_ownership() {
    for path in /config /downloads /store /completed; do
        if [ -e "$path" ]; then
            chown -R "$PUID:$PGID" "$path" || true
        fi
    done
}

if [ "$(id -u)" = "0" ] && [ -n "$PUID" ] && [ -n "$PGID" ]; then
    ensure_group
    ensure_user
    fix_ownership
    exec su-exec "$PUID:$PGID" /app/gonzb "$@"
fi

exec /app/gonzb "$@"
