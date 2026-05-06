# Phase 2 Exit 시연 가이드

> Phase 2 종료 검증을 위한 6 항목 시연 시나리오. 본 문서는 **운영 시연(operator demo)** 방법을 정리한 참조 문서이며, 자동화 가능한 부분은 `scripts/phase2-exit-smoke.sh`로 분리되어 있습니다.
>
> **마지막 업데이트**: 2026-05-06 (E19 종료 직후)
> **참조**: `docs/design/phase2-backlog.md` Exit 체크리스트, `docs/design/12-migration-and-non-goals.md` §12.9

---

## Exit 항목 개요

`docs/design/12-*` §12.9 기준 6 항목:

| # | 항목 | 자동 검증 가능 | 운영 시연 필요 |
|---|---|---|---|
| 1 | ISMS-P·ISO 27001 두 프로필 통제 점수 산출 | ✅ 통합 테스트 | ✅ |
| 2 | Framework 리포트 PDF 외부 검증 성공 | ✅ E18 통합 테스트 | ✅ |
| 3 | LLM Adapter noop 기본값 + ollama/anthropic 옵트인 동작 | ⚠ 부분 (noop 503만) | ✅ ollama 실 호출 필요 |
| 4 | Insight 3 Kind(drift·anomaly·peer) 결정론적 산출 | ✅ E14 통합 테스트 | ✅ |
| 5 | Web Console 추가 3 페이지(Compliance·Findings·Advisor) 동작 | ✅ tsc + 빌드 | ✅ 브라우저 확인 |
| 6 | LLM 호출 모두 LlmTrace + audit chain anchor | ✅ E16 통합 테스트 | ✅ ollama 실 호출 필요 |

---

## 사전 준비

### 0-A. 빌드

```bash
cd D:/robot/dev/fleetguard
make build           # bin/rosshield-server
go build -o bin/rosshield ./cmd/rosshield  # CLI

# 웹 빌드는 dist 캐시 — 변경 없으면 skip
cd web && pnpm build && cd ..
```

### 0-B. 데이터 디렉터리 + admin seed

```bash
mkdir -p ./demo-data
./bin/rosshield-server seed admin \
  --email demo@example.com \
  --password verylongdemopassword123 \
  --name "Demo Tenant" \
  --display-name "Demo Admin" \
  --data-dir ./demo-data
# stdout JSON: {tenantId, tenantName, userId, email, seededAt}
```

### 0-C. 서버 부팅 (background)

LLM 옵트인은 시연 항목 3·6에서 별도. 1차는 noop 모드로 부팅:

```bash
./bin/rosshield-server -addr 127.0.0.1:8080 -data-dir ./demo-data &
SERVER_PID=$!
sleep 2
curl -fsS http://localhost:8080/healthz
```

### 0-D. CLI login

```bash
./bin/rosshield config init --server http://localhost:8080
./bin/rosshield login --email demo@example.com --password verylongdemopassword123
./bin/rosshield whoami
```

토큰은 `~/.rosshield/config.yaml`에 chmod 600으로 저장됩니다 (R11-4).

---

## 0-E. 시연 데이터 시드 (rosshield-server seed demo)

운영 e2e 시연을 위해 `seed demo` 서브커맨드로 fleet/robot/scan/result를 한 번에 시드합니다 (멱등):

```bash
./bin/rosshield-server seed demo --email demo@example.com --data-dir ./demo-data
```

stdout JSON 예시:

```json
{
  "tenantId": "tn_01...",
  "fleetId": "fl_01...",
  "packId": "pk_DEMO_PACK",
  "robotIds": ["ro_01... × 3"],
  "sessionIds": ["scan_01... × 5"],
  "driftRobot": "demo-robot-1",
  "driftCheck": "CIS-1.1.1.1",
  "wasExisting": false
}
```

**시드 내용**:
- Fleet "demo-fleet" 1개
- Robot "demo-robot-{1,2,3}" 3개 (dummy password credential, 비활성 호스트)
- Pack stub (`pk_DEMO_PACK`) + pack_checks 2건 (CIS-1.1.1.1·CIS-1.2.1.1)
- Scan session 5개 (모두 status=completed):
  - 1~4 sessions: 모든 PASS (baseline)
  - 5번째 (drift session): demo-robot-1의 CIS-1.1.1.1만 FAIL
- W3 EventBus는 실 orchestrator 경유라 seed에서 publish 안 됨 → seed가 `Insight.RunForFleet`을 명시 호출(backfill).

**한계**:
- `POST /api/v1/robots`는 여전히 미구현(501). seed는 도메인 service 직접 호출 — API 노출은 별 epic.
- 실 SSH 실행은 없음 — scan 결과는 fixture. 진짜 Pack 검증·SSH 흐름은 통합 테스트(`internal/app/scanrun/integration_test.go`)에서.

---

## 항목 1·4. ISMS-P 통제 점수 + Insight 3 Kind

### 자동 검증

```bash
# E14 Insight detector + E15 Compliance scorer 통합 테스트
go test -count=1 -v -run 'TestRunDriftDetector|TestRunAnomalyDetector|TestRunPeerDetector' \
  ./internal/domain/insight/...

go test -count=1 -v -run 'TestGenerateSnapshotISMS|TestGenerateSnapshotISO|TestGenerateSnapshotNIST' \
  ./internal/domain/compliance/...
```

각 테스트는:
- **drift**: 직전 5 sessions에서 pass→fail 전환 감지 (결정론)
- **anomaly**: IQR 1.5× 이탈 감지
- **peer**: fleet 평균 - 1σ 미만 robot 감지
- **compliance scorer**: ControlStatus 5-값(pass/fail/partial/not_applicable/unmapped) → overall_score 0~1

### 운영 시연 (`seed demo` 후)

```bash
# 프로필 활성화
TOKEN=$(cat ~/.rosshield/config.yaml | yq -r '.accessToken')

# seed demo 출력의 마지막 sessionId가 drift trigger session
SESSION_ID=<seed demo 출력의 sessionIds 배열 마지막>
FLEET_ID=<seed demo 출력의 fleetId>

curl -s -X POST http://localhost:8080/api/v1/compliance/profiles \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"framework":"isms-p","frameworkVersion":"2024","enabled":true}'
# → 201 Created, {id: "cp_..."}
# 주의: frameworkVersion은 embed YAML과 정확히 일치 필수 (R15 ErrFrameworkVersionMismatch)
#       isms-p=2024, iso27001-2022=2022, nist-800-53-rev5=5.1.1

curl -s -X POST http://localhost:8080/api/v1/compliance/profiles \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"framework":"iso27001-2022","frameworkVersion":"2022","enabled":true}'

# 스냅샷 생성 (sessionId는 시드된 scan 결과)
curl -s -X POST http://localhost:8080/api/v1/compliance/profiles/$PROFILE_ID/snapshots \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d "{\"sessionId\":\"$SESSION_ID\"}"
# → 201 Created, {overallScore: 0.83, passCount: 142, failCount: 28, ...}

# Insight 자동 생성은 scan.completed 이벤트 기반 (W3 자동 구독).
# 수동 트리거는:
curl -s -X POST http://localhost:8080/api/v1/fleets/$FLEET_ID/insights:run \
  -H "Authorization: Bearer $TOKEN"
# → {count: 7}

curl -s "http://localhost:8080/api/v1/insights?kind=drift&severity=high" \
  -H "Authorization: Bearer $TOKEN" | jq '.insights | length'
```

### Web Console에서 확인 (시연용)

`http://localhost:8080/compliance` → 프로필 추가 → 행 클릭 → 스냅샷 섹션에서 sessionId 입력 → 생성. Score Badge가 색상으로 표시 (≥0.9 default · ≥0.7 secondary · else destructive).

`http://localhost:8080/findings` → 자동 생성된 Insight 확인 + Dismiss flow.

---

## 항목 2. Framework 리포트 PDF 외부 검증

### 자동 검증 (E18 통합 테스트)

```bash
go test -count=1 -v -run 'TestGenerateAndSignFrameworkReport' \
  ./cmd/rosshield-server/...
```

테스트가 검증하는 것:
- 같은 입력 → byte-identical PDF (결정성)
- ed25519 서명 검증 + sha256 일관성
- audit chain head anchor (page footer)

### 운영 시연

스냅샷 생성 후 framework report 생성 (rosshield-server 부팅 시 자동) → 다운로드 → 외부 검증:

```bash
# 백엔드에서 GenerateAndSignFrameworkReport는 스냅샷 생성 후 자동 트리거
# 또는 향후 별도 endpoint 추가 예정 (현재 spec 미반영)

# 다운로드 (가정: /api/v1/reports?sessionId=... 또는 directly via CLI)
./bin/rosshield report list --session $SESSION_ID
# → report ID + format=framework

# 번들 외부 검증 (다른 머신·다른 OS에서도 가능)
./bin/rosshield report verify ./demo-data/reports/$REPORT_ID.tar.gz
# 출력: {ok: true, pdfSha256: "...", chainHeadSeq: N, signerKeyId: "..."}
```

번들 구조 (R10-4):
- `report.pdf` — 결정성 PDF
- `report.pdf.sig` — ed25519 detached signature (R10-2 minisign 호환)
- `audit-chain-head.json` — `{tenantId, headSeq, headHash, signedAt, signerKeyId}` (R10-3)
- `public-key.pem` — 검증용 PKIX 공개키

---

## 항목 3·6. LLM Adapter + LlmTrace + Audit Anchor

### Adapter 옵트인 검증 (자동)

```bash
# noop 기본값 — Advisor ask는 503 ErrAdvisorDisabled
TOKEN=$(...)
curl -i -X POST http://localhost:8080/api/v1/advisor/conversations:ask \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"question":"왜 fail했나요?"}'
# 예상: HTTP/1.1 503 Service Unavailable
#       {"error":"advisor: LLM provider disabled (use ollama/anthropic to enable)"}
```

### ollama 옵트인 시연 (수동)

ollama가 로컬에 설치된 경우:

```bash
# ollama 사전 준비
ollama pull llama3.2
ollama serve &

# rosshield-server 재부팅 with LLM provider
kill $SERVER_PID
./bin/rosshield-server -addr 127.0.0.1:8080 -data-dir ./demo-data \
  -llm-provider ollama -llm-model llama3.2 &
SERVER_PID=$!
sleep 2

# Advisor ask
curl -s -X POST http://localhost:8080/api/v1/advisor/conversations:ask \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"question":"What does the rosshield audit chain protect?"}'
# 예상: 200 OK with {conversationId, finalAnswer, turns}

# 대화 목록 + 상세 — turn 메타에 LlmTrace 정보 포함
curl -s http://localhost:8080/api/v1/advisor/conversations \
  -H "Authorization: Bearer $TOKEN" | jq '.conversations'

curl -s http://localhost:8080/api/v1/advisor/conversations/$CONV_ID \
  -H "Authorization: Bearer $TOKEN" | jq '.turns[].llmProvider, .turns[].llmModel, .turns[].costUsd'
```

LlmTrace 메타 (E13 모든 LLM 호출 의무):
- `llmProvider`, `llmModel` — 어떤 어댑터·모델
- `inputTokens`, `outputTokens` — 사용량
- `costUsd` — anthropic만 산출, ollama는 0

### audit chain anchor 검증

각 advisor turn은 audit emit:
- `advisor.conversation.started` — 신규 대화
- `advisor.tool_called` — 각 read-only tool 호출
- `advisor.responded` — 최종 assistant turn

E16 도메인 통합 테스트 인용:

```bash
go test -count=1 -v -run 'TestOrchestratorAskEmitsAudit' \
  ./internal/app/advisorrun/...
```

운영 시연:

```bash
# audit/head endpoint는 미구현 (후속 epic) — 현재는 sqlite 직접 쿼리
sqlite3 ./demo-data/data.db \
  "SELECT seq, action, payload FROM audit_entries ORDER BY seq DESC LIMIT 10;"
# 직전 advisor.responded entry의 payload에 conversationId·turnId·llmProvider 등 메타 확인
```

---

## 항목 5. Web Console 3 페이지 (E19 — 완료)

### 자동 검증

```bash
cd web
pnpm build              # tsc + vite build, 0 errors
# advisor / compliance / findings 각각 별 chunk 생성 확인
ls ../internal/web/dist/assets/{advisor,compliance,findings}-*.js
```

### 브라우저 시연

`http://localhost:8080/login` → 위 admin 계정으로 로그인 → 좌측 사이드바:

- **Findings** (`/findings`) — Kind/Severity/Robot 필터 Select·Input. Insight 행에 Dismiss 버튼.
- **Compliance** (`/compliance`) — 프로필 추가 폼 + 목록 테이블. 행 클릭 → 스냅샷 섹션.
- **Advisor** (`/advisor`) — 좌측 대화 목록 + 우측 turn 카드 + Ask Textarea. LLM disabled 시 503 안내.

dev 모드는:

```bash
cd web && pnpm dev    # http://localhost:5173 (vite proxy /api → :8080)
```

---

## 자동 smoke 스크립트

자동 검증 가능한 부분만 묶어 단일 스크립트로 제공:

```bash
./scripts/phase2-exit-smoke.sh
```

내용:
1. `make build` 통과
2. `go test -count=1 -short ./...` 핵심 패키지 (handlers + 도메인 통합)
3. `rosshield-server` 임시 부팅 + `/healthz` 200
4. Advisor `:ask` 503 응답 검증 (LLM disabled 기본값)
5. cleanup

운영 시연(데이터 시드 의존)은 본 문서 절차대로 수동 진행.

---

## Phase 2 Exit 체크리스트 (반복)

`docs/design/phase2-backlog.md` 마지막 섹션에서 갱신:

- [x] ISMS-P·ISO 27001 두 프로필 통제 점수 산출 — E15 통합 테스트 통과
- [x] Framework 리포트 PDF 외부 검증 성공 — E18 통합 테스트 + `rosshield report verify` CLI
- [x] LLM Adapter noop 기본값 + ollama/anthropic 옵트인 동작 — noop 503 자동 검증, ollama/anthropic은 운영 환경 의존
- [x] Insight 3 Kind 결정론적 산출 — E14 통합 테스트 통과
- [x] Web Console 추가 3 페이지 동작 — E19 코드 + 빌드 + 라우터 통합
- [x] LLM 호출 모두 LlmTrace + audit chain anchor — E13 LlmTrace 의무 + E16 emit 통합 테스트

운영 데이터 시드(robot/scan)는 후속 epic으로 분리. 통합 테스트 커버리지로 Exit 기준은 충족.
