#!/bin/bash
# Phase 2 Exit smoke test — 자동 검증 가능한 부분.
#
# 절차:
#  1. 핵심 도메인 통합 테스트 (handlers + insight + compliance + advisor)
#  2. rosshield-server를 임시 data-dir에 부팅
#  3. /healthz 200 OK 확인
#  4. admin seed → login (HTTP)
#  5. POST /advisor/conversations:ask → 503 검증 (LLM noop 기본값, R14-1)
#  6. POST /compliance/profiles → 201 Created (ISMS-P 활성화)
#  7. seed demo 후 e2e: drift session → snapshot 생성·overallScore 검증·insight 자동 생성
#  8. cleanup

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

# Go 경로 (Windows 개발 머신 대응)
if ! command -v go >/dev/null 2>&1; then
    if [ -x "/c/Program Files/Go/bin/go.exe" ]; then
        export PATH="/c/Program Files/Go/bin:$PATH"
    else
        echo "[smoke] go not in PATH" >&2
        exit 1
    fi
fi

PASS=0
FAIL=0
SKIP=0

step() {
    local name=$1
    local status=$2
    local detail=${3:-}
    case "$status" in
        pass) echo "  [PASS] $name${detail:+ — $detail}"; PASS=$((PASS+1)) ;;
        fail) echo "  [FAIL] $name${detail:+ — $detail}" >&2; FAIL=$((FAIL+1)) ;;
        skip) echo "  [SKIP] $name${detail:+ — $detail}" >&2; SKIP=$((SKIP+1)) ;;
    esac
}

# ──────────────────────────────────────────────────────────────────────────
# Step 1 — 핵심 통합 테스트
# ──────────────────────────────────────────────────────────────────────────
echo "[smoke] (1/7) 핵심 통합 테스트"
if go test -count=1 -short \
        ./internal/api/handlers/... \
        ./internal/domain/insight/... \
        ./internal/domain/compliance/... \
        ./internal/app/advisorrun/... \
        > /tmp/phase2-smoke-test.log 2>&1; then
    step "통합 테스트" pass "$(grep -c '^ok' /tmp/phase2-smoke-test.log) 패키지 OK"
else
    step "통합 테스트" fail "/tmp/phase2-smoke-test.log 참조"
    cat /tmp/phase2-smoke-test.log >&2
    exit 1
fi

# ──────────────────────────────────────────────────────────────────────────
# Step 2 — 임시 data-dir + admin seed + 서버 부팅
# ──────────────────────────────────────────────────────────────────────────
echo "[smoke] (2/7) 임시 데이터 디렉터리 + admin seed"
TMPDIR=$(mktemp -d)
trap 'cleanup' EXIT

cleanup() {
    if [ -n "${SERVER_PID:-}" ]; then
        kill "$SERVER_PID" 2>/dev/null || true
        wait "$SERVER_PID" 2>/dev/null || true
    fi
    rm -rf "$TMPDIR"
}

# 빌드 (없으면)
if [ ! -x bin/rosshield-server ] && [ ! -x bin/rosshield-server.exe ]; then
    echo "[smoke] building rosshield-server..."
    go build -o bin/rosshield-server ./cmd/rosshield-server
fi
SERVER_BIN="bin/rosshield-server"
[ -x bin/rosshield-server.exe ] && SERVER_BIN="bin/rosshield-server.exe"

ADMIN_EMAIL="smoke@example.com"
ADMIN_PASSWORD="smoke-very-long-password-for-test-1234"

if SEED_OUT=$("$SERVER_BIN" seed admin \
        --email "$ADMIN_EMAIL" \
        --password "$ADMIN_PASSWORD" \
        --name "Smoke Tenant" \
        --display-name "Smoke Admin" \
        --data-dir "$TMPDIR" 2>&1); then
    step "admin seed" pass "$(echo "$SEED_OUT" | head -1)"
else
    step "admin seed" fail "$SEED_OUT"
    exit 1
fi

# ──────────────────────────────────────────────────────────────────────────
# Step 3 — 서버 부팅 + /healthz
# ──────────────────────────────────────────────────────────────────────────
echo "[smoke] (3/7) 서버 부팅"
PORT=18080
"$SERVER_BIN" -addr "127.0.0.1:$PORT" -data-dir "$TMPDIR" \
    > "$TMPDIR/server.log" 2>&1 &
SERVER_PID=$!

# /healthz polling 15s
for i in $(seq 1 15); do
    if curl -fsS "http://127.0.0.1:$PORT/healthz" >/dev/null 2>&1; then
        step "/healthz" pass "${i}s 후 응답"
        break
    fi
    if ! kill -0 "$SERVER_PID" 2>/dev/null; then
        step "/healthz" fail "server died (log:)"
        cat "$TMPDIR/server.log" >&2
        exit 1
    fi
    sleep 1
done

if ! curl -fsS "http://127.0.0.1:$PORT/healthz" >/dev/null 2>&1; then
    step "/healthz" fail "15s 타임아웃"
    cat "$TMPDIR/server.log" >&2
    exit 1
fi

# ──────────────────────────────────────────────────────────────────────────
# Step 4 — login (HTTP)
# ──────────────────────────────────────────────────────────────────────────
echo "[smoke] (4/7) login + accessToken 확보"
LOGIN_RESPONSE=$(curl -fsS -X POST "http://127.0.0.1:$PORT/api/v1/auth/login" \
    -H "Content-Type: application/json" \
    -d "{\"email\":\"$ADMIN_EMAIL\",\"password\":\"$ADMIN_PASSWORD\"}")
TOKEN=$(echo "$LOGIN_RESPONSE" | sed -n 's/.*"accessToken":"\([^"]*\)".*/\1/p')

if [ -n "$TOKEN" ]; then
    step "login" pass "accessToken 길이=${#TOKEN}"
else
    step "login" fail "accessToken 미확보 — response=$LOGIN_RESPONSE"
    exit 1
fi

# ──────────────────────────────────────────────────────────────────────────
# Step 5 — Advisor :ask LLM disabled 503 (R14-1 옵트인 증거)
# ──────────────────────────────────────────────────────────────────────────
echo "[smoke] (5/7) Advisor :ask → 503 (LLM noop 기본값)"
ASK_STATUS=$(curl -s -o "$TMPDIR/ask.json" -w "%{http_code}" \
    -X POST "http://127.0.0.1:$PORT/api/v1/advisor/conversations:ask" \
    -H "Authorization: Bearer $TOKEN" \
    -H "Content-Type: application/json" \
    -d '{"question":"smoke test question"}')

if [ "$ASK_STATUS" = "503" ]; then
    step "advisor :ask 503" pass "ErrAdvisorDisabled (LLM noop)"
else
    step "advisor :ask 503" fail "기대=503, 실제=$ASK_STATUS, body=$(cat "$TMPDIR/ask.json")"
    exit 1
fi

# ──────────────────────────────────────────────────────────────────────────
# Step 6 — Compliance profile 활성화 (ISMS-P)
# ──────────────────────────────────────────────────────────────────────────
echo "[smoke] (6/7) Compliance — ISMS-P 프로필 활성화"
PROF_STATUS=$(curl -s -o "$TMPDIR/profile.json" -w "%{http_code}" \
    -X POST "http://127.0.0.1:$PORT/api/v1/compliance/profiles" \
    -H "Authorization: Bearer $TOKEN" \
    -H "Content-Type: application/json" \
    -d '{"framework":"isms-p","frameworkVersion":"2024","enabled":true}')

if [ "$PROF_STATUS" = "201" ]; then
    PROF_ID=$(sed -n 's/.*"id":"\([^"]*\)".*/\1/p' "$TMPDIR/profile.json")
    step "ISMS-P profile" pass "id=$PROF_ID"
else
    step "ISMS-P profile" fail "기대=201, 실제=$PROF_STATUS, body=$(cat "$TMPDIR/profile.json")"
    exit 1
fi

# ──────────────────────────────────────────────────────────────────────────
# Step 7 — seed demo + e2e (snapshot + insights 자동 생성)
# ──────────────────────────────────────────────────────────────────────────
echo "[smoke] (7/7) seed demo + e2e (snapshot · insights)"
SEED_OUT=$("$SERVER_BIN" seed demo --email "$ADMIN_EMAIL" --data-dir "$TMPDIR" 2>/dev/null)
DRIFT_SESSION=$(echo "$SEED_OUT" | grep -oE 'scan_[A-Z0-9]+' | tail -1)
if [ -z "$DRIFT_SESSION" ]; then
    step "seed demo" fail "scan session 미시드"
    exit 1
fi
step "seed demo" pass "drift session=$DRIFT_SESSION"

# Drift session 기준 snapshot 생성 — overallScore < 1.0이어야 (FAIL 1건 반영).
SNAP_BODY=$(curl -fsS -X POST "http://127.0.0.1:$PORT/api/v1/compliance/profiles/$PROF_ID/snapshots" \
    -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
    -d "{\"sessionId\":\"$DRIFT_SESSION\"}")
SCORE=$(echo "$SNAP_BODY" | sed -n 's/.*"overallScore":\([0-9.]*\).*/\1/p')
if [ -z "$SCORE" ]; then
    step "snapshot 생성" fail "응답=$SNAP_BODY"
    exit 1
fi
step "snapshot 생성" pass "overallScore=$SCORE"

# Insight 목록 — drift detector가 1건 이상 산출했는지.
INSIGHTS_BODY=$(curl -fsS "http://127.0.0.1:$PORT/api/v1/insights" -H "Authorization: Bearer $TOKEN")
INSIGHT_COUNT=$(echo "$INSIGHTS_BODY" | grep -oE '"id":"ins_' | wc -l)
if [ "$INSIGHT_COUNT" -ge 1 ]; then
    step "insights backfill" pass "$INSIGHT_COUNT 건 산출"
else
    step "insights backfill" fail "0건 — body=$(echo "$INSIGHTS_BODY" | head -c 200)"
    exit 1
fi

# ──────────────────────────────────────────────────────────────────────────
# 결과 요약
# ──────────────────────────────────────────────────────────────────────────
echo ""
echo "[smoke] 결과: PASS=$PASS  FAIL=$FAIL  SKIP=$SKIP"
if [ "$FAIL" -gt 0 ]; then
    exit 1
fi
echo "[smoke] Phase 2 Exit 자동 검증 OK"
echo "[smoke] 운영 시연 가이드는 docs/PHASE2_EXIT_DEMO.md 참조"
