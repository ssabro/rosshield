#!/bin/sh
# E11 Compose — first-boot init + server 기동.
# - data dir이 비었으면 admin 시드 실행 (env vars 강제)
# - server는 마지막에 exec (PID 1 신호 처리)
set -eu

DATA_DIR="${ROSSHIELD_DATA_DIR:-/var/lib/rosshield}"
DB_FILE="$DATA_DIR/data.db"
LISTEN="${ROSSHIELD_LISTEN_ADDR:-0.0.0.0:8080}"

# 첫 부팅 감지: data.db 부재 시 admin 시드 시도.
if [ ! -f "$DB_FILE" ]; then
    : "${ROSSHIELD_ADMIN_EMAIL:?ROSSHIELD_ADMIN_EMAIL is required for first boot}"
    : "${ROSSHIELD_ADMIN_PASSWORD:?ROSSHIELD_ADMIN_PASSWORD is required for first boot}"
    echo "[entrypoint] first boot — seeding admin user $ROSSHIELD_ADMIN_EMAIL"
    rosshield-server seed admin \
        --email "$ROSSHIELD_ADMIN_EMAIL" \
        --password "$ROSSHIELD_ADMIN_PASSWORD" \
        --name "${ROSSHIELD_TENANT_NAME:-Default Tenant}" \
        --data-dir "$DATA_DIR"
    echo "[entrypoint] seed complete — data persists at $DATA_DIR"
else
    echo "[entrypoint] existing data.db detected — skipping seed"
fi

# server 기동 — exec로 PID 1 자리 양도.
exec rosshield-server -addr "$LISTEN" -data-dir "$DATA_DIR"
