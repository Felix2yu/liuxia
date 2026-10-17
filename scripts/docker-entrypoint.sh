#!/bin/sh

PUID=${PUID:-1000}
PGID=${PGID:-1000}

if command -v groupmod >/dev/null 2>&1; then
    groupmod -o -g "$PGID" appuser
fi
if command -v usermod >/dev/null 2>&1; then
    usermod  -o -u "$PUID" appuser
fi

chown -R appuser:appuser /app

exec gosu appuser /app/liuxia
