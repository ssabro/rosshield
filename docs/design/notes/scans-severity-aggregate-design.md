# Scans severity aggregate column — Design

> **상태**: Phase 5 design (다음 세션 진입점). 본 문서는 사용자 합의(2026-05-13)에 따라 본 세션 context 한도 회피로 design만 마감, 구현은 다음 세션 진입.

## 1. 배경

scans list API는 session 별 progress(total/completed/failed)만 반환한다. 운영자는 list 화면에서 "어느 session에 critical/high failed가 몇 건인지" 볼 수 없어 매 session detail로 진입해야 한다. CIS pack severity classification(`75dc9e0`)이 312 checks를 critical 0 / high 98 / medium 146 / low 68로 분류한 이상, list 화면에서도 severity 분포 노출 가치 큼.

목표: scan_sessions list 응답에 each session의 **severity별 failed count 4종**(critical/high/medium/low) 추가.

## 2. 현재 구조

### 2.1 Schema

`internal/platform/storage/migrations/sqlite/0011_scan.sql`

```sql
CREATE TABLE scan_sessions (
    id, tenant_id, fleet_id, pack_id,
    trigger, status,
    progress_total, progress_completed, progress_failed,
    failure_reason, created_at, updated_at, started_at, completed_at,
    PRIMARY KEY (id)
);

CREATE TABLE scan_results (
    id, session_id, tenant_id, robot_id,
    check_id, pack_check_id,
    outcome,             -- 'pass'|'fail'|'indeterminate'|'error'|'skipped'
    eval_reason, evidence_ref, duration_ms, executed_at, created_at,
    PRIMARY KEY (id)
);

-- pack_checks (0007_packs.sql §31)
severity TEXT NOT NULL DEFAULT 'medium'  -- low|medium|high|critical
```

severity는 pack_checks에 있음 — scan_results는 pack_check_id FK 보유. 집계는 scan_results JOIN pack_checks 필요.

### 2.2 Code path

- `internal/domain/scan/scan.go` ListSessions interface → `sqliterepo/repo.go` ListSessions 구현(SELECT + DESC + LIMIT)
- `internal/api/handlers/scan.go` GET /api/v1/scans → ScanSession array 응답
- `internal/app/scanrun/scanrun.go` terminal transition 시 EventBus `scan.completed` publish (이미 결선)
- `internal/platform/metrics/eventbridge.go` handleScanCompleted 등록(metric 갱신용)
- web `routes/_authenticated/scans.tsx` SessionGroup 헤더 + progress display

## 3. 옵션 비교

### 옵션 A — Ad-hoc JOIN (list 시점 매번 집계)

**Pros**: schema 변경 없음 / backfill 불필요 / 항상 최신.

**Cons**: list 호출마다 scan_results × pack_checks JOIN GROUP BY × N sessions 비용. 5분마다 polling 시 누적 부하. 큰 fleet(1000+ robots × 312 checks = 312k rows / session × 100 sessions list) 시 100ms+ p95 위험. SQLite EXPLAIN으로 인덱스 활용도 검증 필요.

### 옵션 B — 영속 컬럼 + scanrun terminal transition 시 집계 (권장)

**Pros**: list 호출 cost 0(컬럼 직접 SELECT) / atomic with terminal transition / 단일 트랜잭션 일관성.

**Cons**: schema 변경(마이그레이션 0026 또는 다음 번호) / backfill 필요(기존 sessions) / scanrun TransitionSession 흐름 약간 복잡.

### 옵션 C — Materialized view (별도 테이블 + EventBus async 갱신)

**Pros**: scanrun 분리 / async cost 흡수.

**Cons**: 결정론적 일관성 손실(view 갱신 lag) / 운영 복잡 / B 대비 이득 희박.

## 4. 권장 — 옵션 B

근거:
- list cost 영구 0 — 운영 dashboard polling 부담 0
- scanrun terminal transition은 이미 모든 scan_results를 DB에 commit한 시점 — 같은 트랜잭션에서 집계 가능, drift 0
- backfill은 1회 SQL — 운영 부담 최소
- 결정론적 일관성(원칙 §1·§9) — view lag 없음

## 5. 변경 사항

### 5.1 Schema (마이그레이션 0026 또는 다음 번호)

```sql
-- +goose Up
ALTER TABLE scan_sessions ADD COLUMN severity_critical_failed INTEGER NOT NULL DEFAULT 0;
ALTER TABLE scan_sessions ADD COLUMN severity_high_failed     INTEGER NOT NULL DEFAULT 0;
ALTER TABLE scan_sessions ADD COLUMN severity_medium_failed   INTEGER NOT NULL DEFAULT 0;
ALTER TABLE scan_sessions ADD COLUMN severity_low_failed      INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE scan_sessions DROP COLUMN severity_low_failed;
ALTER TABLE scan_sessions DROP COLUMN severity_medium_failed;
ALTER TABLE scan_sessions DROP COLUMN severity_high_failed;
ALTER TABLE scan_sessions DROP COLUMN severity_critical_failed;
```

PG 버전도 동일(SQLite/PG 둘 다 ALTER TABLE ADD COLUMN 호환). info 컬럼은 CIS info=0 + Findings 페이지가 5-tier(`info`)지만, 여기는 4-tier(critical/high/medium/low)로 — info는 CIS pack에 없고, low로 흡수해도 정보 손실 0.

### 5.2 Domain

`scan.ScanSession` struct에 4 int 필드 추가(SeverityCriticalFailed/High/Medium/Low). DB 컬럼명과 mirror.

### 5.3 Repo

- ListSessions / GetSession SELECT에 4 컬럼 포함
- `RecomputeSeverityAggregate(ctx, tx, sessionID)` 신규 — scan_results JOIN pack_checks WHERE outcome='fail' GROUP BY severity → SUM 4종 → scan_sessions UPDATE

```sql
WITH counts AS (
    SELECT pc.severity, COUNT(*) AS cnt
    FROM scan_results sr
    JOIN pack_checks pc ON pc.id = sr.pack_check_id
    WHERE sr.session_id = ? AND sr.outcome = 'fail'
    GROUP BY pc.severity
)
UPDATE scan_sessions
SET severity_critical_failed = COALESCE((SELECT cnt FROM counts WHERE severity='critical'), 0),
    severity_high_failed     = COALESCE((SELECT cnt FROM counts WHERE severity='high'), 0),
    severity_medium_failed   = COALESCE((SELECT cnt FROM counts WHERE severity='medium'), 0),
    severity_low_failed      = COALESCE((SELECT cnt FROM counts WHERE severity='low'), 0)
WHERE id = ?
```

### 5.4 Application

`internal/app/scanrun/scanrun.go` terminal 전이(completed/failed/cancelled) 직전에 Repo.RecomputeSeverityAggregate 호출 — TransitionSession 트랜잭션 안에서 실행, atomic 보장.

EventBus 후처리 옵션 X — atomic 일관성을 위해 trans 안.

### 5.5 API

`internal/api/handlers/scan.go` sessionResponse(또는 ScanSession 직렬화 helper)에 4 필드 추가 — `severityCriticalFailed`/`severityHighFailed`/.../Low. omitempty=false(0도 표시 — 운영자가 "0건이라 안전" 식별 가능).

openapi.yaml ScanSession schema 4 필드 추가 → Go gen + TS types regen.

### 5.6 Web

`routes/_authenticated/scans.tsx` SessionGroup 헤더에 severity Badge 4종(0이면 muted, >0이면 색상 강조 — packs.$packKey의 SeverityStats 패턴 일관). 클릭 토글로 scoped filter는 보류(detail 페이지 필터로 충분).

i18n 4 키(severity.critical/high/medium/low short label).

### 5.7 Backfill

마이그레이션 0026 직후 running 서버 재시작 시 1회 SQL로 기존 sessions 채움. 마이그레이션 자체에 포함 가능:

```sql
-- 0026 마이그레이션 안에 포함
UPDATE scan_sessions
SET severity_critical_failed = COALESCE((SELECT COUNT(*) FROM scan_results sr JOIN pack_checks pc ON pc.id=sr.pack_check_id WHERE sr.session_id=scan_sessions.id AND sr.outcome='fail' AND pc.severity='critical'), 0),
    severity_high_failed     = COALESCE((SELECT COUNT(*) FROM scan_results sr JOIN pack_checks pc ON pc.id=sr.pack_check_id WHERE sr.session_id=scan_sessions.id AND sr.outcome='fail' AND pc.severity='high'), 0),
    severity_medium_failed   = COALESCE((SELECT COUNT(*) FROM scan_results sr JOIN pack_checks pc ON pc.id=sr.pack_check_id WHERE sr.session_id=scan_sessions.id AND sr.outcome='fail' AND pc.severity='medium'), 0),
    severity_low_failed      = COALESCE((SELECT COUNT(*) FROM scan_results sr JOIN pack_checks pc ON pc.id=sr.pack_check_id WHERE sr.session_id=scan_sessions.id AND sr.outcome='fail' AND pc.severity='low'), 0);
```

대규모 customer는 별 maintenance window — 본 시점에 customer 0이라 부담 없음.

## 6. TDD 분해 (다음 세션 stage 단위)

| Stage | 산출 | 검증 |
|---|---|---|
| 1 | 마이그레이션 0026 + scan domain struct 4 필드 | sqlite + PG up/down 회귀 0, struct field round-trip |
| 2 | Repo SELECT 4 필드 + RecomputeSeverityAggregate 함수 | repo_test.go +1 fixture(severity 분포 mix) + 2 단위(빈 결과 0 / 4 severity mix) |
| 3 | scanrun TransitionSession 결선 | scanrun_test.go +1(terminal 전이 후 sessions row 4 필드 검증) |
| 4 | handler + openapi spec + Go gen + TS types | api/handlers test 1 + make openapi |
| 5 | Web SessionGroup severity Badge + i18n + tests | scans.tsx 컴포넌트 + 단위 |
| 6 | SESSION_HANDOFF 갱신 + commit chain | — |

추정: Stage 1~2 0.5일 / Stage 3 0.25일 / Stage 4 0.25일 / Stage 5 0.25일 = **1.25일**.

## 7. 회귀 위험

- 기존 ListSessions/GetSession SELECT 시그니처 변경 → 테스트 다수 영향 가능 → SELECT * 회피, 명시 컬럼 sequence 유지
- 마이그레이션 backfill 시 큰 customer는 lock 위험 — 본 시점 customer 0이라 무관
- scanrun TransitionSession 트랜잭션 길이 증가 → 단일 SQL이라 미세
- pack_checks DELETE/UPDATE 시 severity 분포 stale 위험 → packs는 immutable 패턴(R5-7)이라 무관

## 8. 결정 필요 항목 (다음 세션 진입 시)

- D26-1 — info severity 컬럼 추가 여부: **No(권장)** — CIS pack 0건, Findings 페이지의 info는 별 도메인(insight)
- D26-2 — backfill을 마이그레이션에 포함 vs 별 admin command: **마이그레이션 포함(권장)** — 본 시점 customer 0
- D26-3 — outcome=indeterminate/error/skipped도 카운트 분리 여부: **No(권장)** — failed만으로 충분, 별 컬럼 추가 시 cost > value
- D26-4 — Web Badge UX: **packs.$packKey SeverityStats 패턴 재사용(권장)**

## 9. 참조

- `docs/design/04-domain-and-data-model.md` §4.2 ScanSession/ScanResult
- `docs/design/07-scan-engine-and-benchmarks.md` §7.2~7.3
- `internal/platform/storage/migrations/sqlite/0011_scan.sql` (현 schema)
- `internal/platform/storage/migrations/sqlite/0007_packs.sql` (severity 출처)
- `cmd/pack-tools/converter/cis.go` `classifyCISSeverity` (severity 채움 로직, `75dc9e0`)
- 직전 PR `7e43713` (findings severity 통계 카드 — 5-tier UX 참고)
