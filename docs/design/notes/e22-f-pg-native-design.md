# E22-F PG-native repo 분리 — R30-1 결정 + 핫 path 회수

> **상태**: 1차 핫 path 회수 완료 (마이그레이션 0024). 사용자 R30-1 결정 = C 하이브리드 (2026-05-11).
> **범위**: PG schema의 일부 컬럼만 native type으로 회수. sqliterepo 단일 코드베이스 유지.
> **참조**: `phase5-backlog.md` Phase 4 carryover E22-F, E22-E follow-up(`3c06290`)에서 sqlite-equivalent로 통일했던 PG schema.

---

## 1. R30-1 결정 비교

| 옵션 | 작업량 | PG 이득 | 코드베이스 부담 |
|---|---|---|---|
| **A Big bang** (모든 도메인 pgrepo 별도) | 1주+ | 100% | 두 코드베이스 영구 관리 |
| **B 점진적 schema-only** (BOOL/JSONB/TIMESTAMPTZ 전부 복원) | 1~2일 | ~70% (driver type mismatch 위험) | 결국 driver-aware repo 필요 |
| **C 하이브리드** (핫 path만 복원) | 3~4일 | ~80% | sqliterepo 단일 유지 |
| 4 deferred | 0 | 0 | sqlite-equivalent 유지 |

**선택**: C 하이브리드 (사용자 결정 2026-05-11).

**근거**:
- B의 BOOLEAN 회수는 driver type 강제로 인한 mismatch 위험 (E22-E follow-up에서 본 "unable to encode 1 into binary format for bool" 사례). 결국 driver-aware repo가 필요해져 작업이 A로 수렴.
- C는 driver-agnostic 호환성을 유지하는 TIMESTAMPTZ + JSONB만 회수. sqliterepo 단일 코드베이스가 PG·sqlite 양쪽에서 정상 동작.
- A는 first paying customer 진입 시점에 query optimization·성능 요구가 명확해지면 그 때 점진 확장.

---

## 2. 1차 회수 핫 path (마이그레이션 0024)

| 테이블·컬럼 | TEXT → | 이득 |
|---|---|---|
| `audit_entries.occurred_at` | TIMESTAMPTZ | `BETWEEN ? AND ?` Verify·Export range query 인덱스 plan 개선 |
| `audit_chain_heads.updated_at` | TIMESTAMPTZ | checkpoint 시간 비교 정확도 + range query |
| `insights.evidence_json` | JSONB | GIN 인덱스 옵션 + JSONB query (`->`, `?`, `@>`) 잠재 활용 |

**비포함 (1차)**:
- 다른 `_at TEXT` 컬럼 (`created_at`·`updated_at`·`started_at`·`signed_at` 등 모든 도메인) — 점진 확장 후보. Customer query plan 분석 후 회수 우선순위 결정.
- `evidence_json` 외 다른 JSON 컬럼 (`encryption_meta`·`source_meta_json` 등) — 1차 GIN 활용 가능성 분석 필요.
- BOOLEAN 회수 (모든 `SMALLINT` flag 컬럼) — driver type mismatch 위험으로 보류.

---

## 3. 호환성 보장 메커니즘

### 3.1 sqliterepo가 PG-native 컬럼에서 정상 동작하는 이유

**TIMESTAMPTZ ↔ string**:
- INSERT: sqliterepo가 `time.Now().UTC().Format(time.RFC3339Nano)` 문자열을 인자로 전달 → PG는 RFC3339 형식을 TIMESTAMPTZ로 자동 캐스트
- SELECT: sqliterepo가 `var s string; rows.Scan(&s)`로 받음 → pgx driver가 TIMESTAMPTZ를 텍스트 표현으로 직렬화 (정확한 포맷은 driver 의존, RFC3339-like)
- `time.Parse(time.RFC3339Nano, s)`는 약간 다른 포맷도 관대하게 파싱 (`2006-01-02T15:04:05.999999999Z07:00`)

**JSONB ↔ string**:
- INSERT: JSON 문자열 인자 → PG JSONB로 자동 파싱
- SELECT: JSONB → string 또는 []byte SCAN target 모두 지원
- 주의: JSONB는 입력을 normalize (공백 제거·키 정렬 등). round-trip 시 byte-equal X — 의미 동등성만 보장

### 3.2 검증

`internal/platform/storage/postgres/pgnative_hotpath_integration_test.go` 3 testcontainers 테스트:
- `TestIntegrationPGNativeAuditOccurredAtRoundTrip` — RFC3339Nano INSERT + string SELECT 동작
- `TestIntegrationPGNativeAuditChainHeadUpdatedAt` — 동일 패턴 head 테이블
- `TestIntegrationPGNativeInsightsEvidenceJSON` — JSON 문자열 INSERT, JSONB normalize 후 SELECT (키 존재 확인)

CI `pg-integration` job이 실 docker로 실행.

---

## 4. 점진 확장 후보 (Phase 5+ 또는 Customer query plan 분석 후)

| Stage | 후보 | 이득 |
|---|---|---|
| 2 | `scan_sessions.started_at`·`completed_at` TIMESTAMPTZ | 운영 모니터링 dashboard query |
| 2 | `auth_refresh_tokens.expires_at` TIMESTAMPTZ | TTL cleanup job index plan |
| 3 | `webhooks.deliveries.next_retry_at` TIMESTAMPTZ | dispatcher polling query plan |
| 3 | `credentials.encryption_meta` TEXT → JSONB | (작음, 거의 read 빈도 없음) |
| 4 | BOOLEAN 회수 (`api_keys.is_active`·`webhooks.enabled` 등) | driver-aware repo 도입 (A 옵션) 필요 — 별 epic |

---

## 5. 롤백 시나리오

마이그레이션 0024 down:
- TIMESTAMPTZ → TEXT는 `to_char(... AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')` — RFC3339-like 복원
- JSONB → TEXT는 `evidence_json::text` 자동 — JSON 문자열로 복원

운영 환경에서 down migration이 필요한 시나리오는 거의 없음 (forward-only 정책). 본 down은 dev/CI rollback 가정.

---

## 6. 향후 결정 요청

본 문서가 1차 핫 path 회수 완료를 기록. Customer paying 진입 시점에:
- Query plan 분석 보고 (어떤 BETWEEN/JSON query가 가장 빈번한지)
- 결과에 따라 Stage 2~4 확장 (R30-1.2·R30-1.3 ID 부여)
- BOOLEAN 회수 필요성 재평가 (A Big bang 진입 트리거)
