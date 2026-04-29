#!/bin/bash
# E11 Compose smoke test.
# - up → 30s 안에 /healthz 200 응답 → down
# - 실패 시 exit 1, 컨테이너 로그 stderr로 출력

set -euo pipefail

# docker 부재 시 graceful skip (예: 개발 머신에 docker 미설치).
if ! command -v docker >/dev/null 2>&1; then
    echo "[smoke] docker not installed — skipping smoke test" >&2
    exit 0
fi

if ! docker compose version >/dev/null 2>&1; then
    echo "[smoke] docker compose v2 not available — skipping smoke test" >&2
    exit 0
fi

cd "$(dirname "$0")/../deploy/compose"

# .env가 없으면 fixture 생성 (smoke test 전용).
if [ ! -f .env ]; then
    cat > .env <<EOF
ROSSHIELD_ADMIN_EMAIL=smoke@example.com
ROSSHIELD_ADMIN_PASSWORD=smoketest-very-long-password-12345
ROSSHIELD_TENANT_NAME=Smoke Test Tenant
EOF
    CLEANUP_ENV=1
fi

cleanup() {
    docker compose down -v --remove-orphans >/dev/null 2>&1 || true
    if [ "${CLEANUP_ENV:-0}" = "1" ]; then
        rm -f .env
    fi
}
trap cleanup EXIT

echo "[smoke] docker compose up -d --build"
docker compose up -d --build

echo "[smoke] waiting for /healthz (30s timeout)..."
for i in $(seq 1 30); do
    if curl -fsS http://localhost:8080/healthz >/dev/null 2>&1; then
        echo "[smoke] healthz OK after ${i}s"
        echo "[smoke] container logs (last 10 lines):"
        docker compose logs --tail=10
        exit 0
    fi
    sleep 1
done

echo "[smoke] healthz did not respond within 30s — dumping logs"
docker compose logs
exit 1
