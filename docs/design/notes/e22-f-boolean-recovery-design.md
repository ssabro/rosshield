# E22-F BOOLEAN 회수 — PG-native 타입 분리 2차 (R30-1.2)

> **상태**: Phase 0 design (다음 세션 진입점). 본 문서는 코드 0줄 / 마이그레이션 0 — 후보 식별 + driver mismatch 위험 분석 + 옵션 비교 + 점진 적용 Stage 분해 + 결정 항목 권장 default까지만 마감.
> **참조**: 직전 design doc `docs/design/notes/e22-f-pg-native-design.md` (R30-1=C 하이브리드, 1차 핫 path 회수 commit `f3bf23f`).
> **R 식별자**: R30-1.2 (R30-1 하위 — 1차는 R30-1.1 = TIMESTAMPTZ+JSONB 핫 path).

---

## 1. 배경

E22-F 1차에서 **TIMESTAMPTZ 2 + JSONB 1, 총 3 컬럼**만 PG-native로 회수했습니다 (마이그레이션 0024). BOOLEAN 회수는 E22-E follow-up commit `3c06290`에서 관찰된 driver type mismatch (`unable to encode 1 into binary format for bool`)로 보류했습니다. 본 design doc은 그 보류를 **재평가**하고 점진 적용 가능 여부를 판정합니다.

- **잠재 효과**: 미미. BOOLEAN ↔ SMALLINT는 query plan 차이 거의 0(둘 다 1바이트 비교, 인덱스 plan 동일). **회수 가치는 "schema 의미 명확성 + driver-native API 호환"** 한정.
- **추정 시간**: 0.5일(옵션 A) ~ 1.0일(옵션 B) ~ 0.0일(옵션 C 보류 유지).
- **회귀 위험**: 중. driver type 강제 변환은 모든 호출 site에서 INSERT/SELECT 인자·타겟 타입 점검 필요. testcontainers PG-integration job이 1차 방벽.

---

## 2. 현재 상태 진단

### 2.1 후보 컬럼 식별 (5개)

| # | 테이블·컬럼 | 현재 PG | 현재 SQLite | sqliterepo 패턴 | 도메인 |
|---|---|---|---|---|---|
| 1 | `roles.is_system` | SMALLINT | INTEGER | `isSystem int` SCAN, `IsSystem: isSystem == 1` 변환, INSERT는 `isSystem := 0; if r.IsSystem { isSystem = 1 }` | tenant |
| 2 | `compliance_frameworks.enabled` | SMALLINT | INTEGER | `enabled int` SCAN, `Enabled: enabled != 0` | compliance |
| 3 | `webhooks.enabled` | SMALLINT | INTEGER | `enabledInt int` SCAN, `Enabled: enabledInt != 0` | integration/webhook |
| 4 | `webhook_deliveries.succeeded` | SMALLINT | INTEGER | `succeededInt int` SCAN, `Succeeded: succeededInt != 0` + WHERE 절 `succeeded = 0` (ListDueDeliveries) + UPDATE `succeeded = 1` (MarkDeliverySucceeded) | integration/webhook |
| 5 | `sso_providers.enabled` | SMALLINT | INTEGER | `enabledInt int` SCAN, `Enabled: enabledInt != 0` | tenant/sso |

**비후보 (의도적 제외)**:
- `audit_chain_heads.current` (0022_leader_epoch) — 0/1 boolean이지만 `WHERE current = 1` 조건이 partial unique index의 핵심. BOOLEAN 회수 시 PG-side는 `WHERE current` 또는 `WHERE current = TRUE`로 변경 필요 → driver-aware repo 진입 트리거. **본 epic 비대상 (별 commit 또는 R30-1.3)**.
- `webhook_deliveries.attempt_count` / `last_response_status` — 진짜 정수(0~5, HTTP status), boolean 아님.
- `webhooks.format`/`events` 등 TEXT — boolean 아님.
- 0026_scan_severity_aggregate `severity_*_failed` 카운터들 — 진짜 정수.

**총 회수 대상: 5 컬럼** (audit `current` 제외).

### 2.2 sqliterepo의 실제 read/write 패턴 공통점

5 컬럼 모두 동일 패턴:

```go
// WRITE
isSystem := 0
if role.IsSystem {
    isSystem = 1
}
tx.Exec(ctx, `INSERT INTO roles (..., is_system, ...) VALUES (..., ?, ...)`, ..., isSystem, ...)

// READ
var isSystem int
row.Scan(..., &isSystem, ...)
return Role{..., IsSystem: isSystem == 1}

// WHERE
tx.Query(ctx, `... WHERE succeeded = 0 AND attempt_count < ? ...`, ...)
```

### 2.3 Driver 호환성 분석

**SQLite (modernc.org/sqlite)**:
- INTEGER ↔ Go int 양방향 자유.
- BOOLEAN type 미지원 (CREATE 시 BOOLEAN 키워드 허용되나 INTEGER로 affinity 변환 — 0/1 저장).

**PG (jackc/pgx v5)**:
- BOOLEAN ↔ Go bool 강제. **int → BOOLEAN INSERT 시 "unable to encode N into binary format for bool" 에러** (E22-E follow-up `3c06290` 사례).
- BOOLEAN → int SELECT도 마찬가지 에러 (text format이면 "true"/"false" string 반환, binary format이면 plan 부재).
- **단**: WHERE 절에서 `WHERE col = 1` 식 SQL literal 비교는 PG에서 type mismatch ("operator does not exist: boolean = integer") — 별도 fix 필요 (`WHERE col` 또는 `WHERE col = TRUE`).
- **결정적**: 현재 sqliterepo의 `int` SCAN/BIND 패턴은 PG BOOLEAN과 **양방향 모두 비호환**.

### 2.4 호환성 회복 전략 (3종)

회수 시 sqliterepo가 PG에서 동작하려면 다음 중 하나:

**전략 1 — sqliterepo를 bool 패턴으로 변환** (본 epic 적용 시 권장):
- WRITE: `tx.Exec(..., role.IsSystem, ...)` (Go bool 직접 BIND).
- READ: `var isSystem bool; row.Scan(..., &isSystem, ...); return Role{..., IsSystem: isSystem}`.
- WHERE: `WHERE succeeded = ?` + bool 인자, 또는 `WHERE NOT succeeded` (parameterless).
- SQLite는 bool BIND 시 0/1 INTEGER로 자동 캐스트(modernc.org/sqlite 지원), bool SCAN도 INTEGER 0/1을 bool로 캐스트.

**전략 2 — driver-aware 분기 도입** (옵션 B에서 사용):
- repo 내부 `isPostgres bool` 플래그 또는 별도 pgrepo 분리.
- 코드 분기 비용 큼 — 본 epic의 5 컬럼만으로 driver-aware repo 도입은 ROI 낮음.

**전략 3 — 회수 보류 (현 상태 유지)**.

전략 1의 검증 필요 사항:
- modernc.org/sqlite의 bool BIND/SCAN 동작 확인 (예상: BOOLEAN affinity 없으나 driver가 0/1 INTEGER로 캐스트). Stage 1 진단 항목.
- WHERE literal 비교 변경 — `WHERE succeeded = 0` → `WHERE succeeded = ?` (false BIND) 또는 `WHERE NOT succeeded`.

---

## 3. 합성 전략 옵션 (≥3)

### 3.1 옵션 A — sqliterepo bool 패턴 변환 + PG BOOLEAN 회수 (본 epic 5 컬럼) 

**범위**: 5 컬럼 PG schema BOOLEAN 회수 + sqliterepo 5 site int → bool 패턴 변환.

**Pros**:
- driver-native API. Go bool ↔ PG BOOLEAN ↔ SQLite (auto-cast) 일관.
- Schema 의미 명확화 (`enabled SMALLINT` 보다 `enabled BOOLEAN`이 의도 직관).
- sqliterepo 단일 코드베이스 유지(driver-aware 회피).

**Cons**:
- modernc.org/sqlite의 bool BIND/SCAN 호환성 검증 필요 (Stage 1 진단). 예상 호환이지만 확정 전 실험 필수.
- WHERE literal 변경: 모든 `WHERE col = 0`·`WHERE col = 1`을 parameterized 또는 `WHERE col`/`WHERE NOT col`로.
- INSERT/UPDATE도 0/1 literal이 있으면 `?` parameterized로.
- 도메인 repo 5 사이트 + 단위 테스트 5 사이트 수정.

**회귀 위험**: 중. testcontainers PG-integration job이 1차 방벽 (PG에서 mismatch 시 즉시 발견). SQLite-only 회귀는 단위 테스트 + selftest로 검증.

**코드 변경 추정**:
- 마이그레이션 1개 (0027_boolean_recovery up/down).
- sqliterepo 5 파일 수정 (각 ~10 lines).
- 단위 테스트 fixture 5건 수정 (bool 직접 비교).
- 통합 테스트 신규 1개 (testcontainers boolean round-trip).
- 총 ~150 lines (코드) + ~80 lines (테스트).

**운영 영향**: 0. 마이그레이션은 ALTER TYPE BOOLEAN USING (col != 0) 형식, 데이터 손실 없음. 기존 PG 인스턴스 in-place 변환.

### 3.2 옵션 B — Big bang driver-aware repo 분리 (R30-1 옵션 A 진입)

**범위**: 5 컬럼 + audit `current` + 향후 모든 BOOLEAN 후보. sqliterepo와 별도로 postgresqlrepo 도입.

**Pros**:
- 모든 PG-native 타입 자유 활용 (BOOLEAN·partial index 조건·JSONB query 등).
- 향후 query optimization 자유도 ↑.

**Cons**:
- 두 repo 영구 관리. 도메인별 5+ 디렉토리 신규.
- 1주+ 작업.
- **첫 paying customer 진입 전이면 ROI 낮음**(현 단계는 spec 결정·core 동작 우선).
- R30-1=C 하이브리드 결정과 정면 충돌 — 결정 번복 합의 필요.

**회귀 위험**: 큼. repo 분리 시 양 코드베이스 동기화 누락 위험.

**코드 변경 추정**: ~1500 lines + 인터페이스 추상화.

**운영 영향**: 0 (스키마 변경 없이 코드만 분리도 가능).

### 3.3 옵션 C — 회수 보류 + 현재 SMALLINT 유지

**범위**: 변경 0. 1차 design doc §4 Stage 4(`BOOLEAN 회수 → A Big bang 진입 트리거`)와 일관.

**Pros**:
- 작업 0. 기존 패턴 보존.
- driver-aware repo 도입까지 BOOLEAN 회수 비용 0.

**Cons**:
- Schema 의미 모호 (`SMALLINT NOT NULL DEFAULT 1`이 boolean인지 enum인지 가독성 ↓).
- driver-native API 미활용.

**회귀 위험**: 0.

**코드 변경 추정**: 0.

**운영 영향**: 0.

### 3.4 옵션 D — Schema BOOLEAN 회수 + sqliterepo는 int 유지 (driver mismatch 미해결)

**범위**: 마이그레이션만 적용. 코드 변경 0.

**검증**: testcontainers에서 즉시 실패 — `unable to encode 1 into binary format for bool`. **즉시 기각**(E22-E follow-up `3c06290` 사례 재현). 본 옵션은 분석 완전성을 위한 기재만, 실제 선택지 아님.

---

## 4. 권장 옵션

**권장 default: 옵션 C — 회수 보류**.

**근거**:
1. **ROI 부재**: BOOLEAN ↔ SMALLINT의 query plan·storage·인덱스 효율 차이는 사실상 0. 회수의 유일한 이득은 schema 가독성 + driver-native API. 둘 다 "있으면 좋음" 수준.
2. **회귀 위험 vs 이득 비대칭**: 옵션 A는 5 사이트 코드 변경 + WHERE literal 패턴 일괄 정리 + modernc.org/sqlite bool 호환성 실증 필요. 한 사이트 누락 시 testcontainers job에서 즉시 실패하나 fix 비용은 작지 않음. 이 비용을 schema 가독성 한 줄을 위해 지불할 가치 미흡.
3. **R30-1=C 하이브리드 결정과 일관**: 1차 design doc §4 Stage 4가 명시한 "BOOLEAN 회수는 driver-aware repo 도입(A) 트리거" 정책. 본 epic만으로는 driver-aware 진입 가치 없음 — paying customer 진입 시 query optimization 요구가 명확해질 때 옵션 B로 일괄 처리.
4. **선례**: E22-E follow-up `3c06290`이 동일 결론(BOOLEAN → SMALLINT 통일)에 도달. 그 결정 번복 사유가 "1차 핫 path 회수 후에도 변하지 않음" — 핫 path 회수 commit `f3bf23f`는 BOOLEAN 회수 ROI에 영향 미치지 않음.

**대안 default**: 사용자가 schema 가독성을 명시 우선시할 경우 → **옵션 A** (Stage 분해 §6 적용).

---

## 5. 변경 사항 outline (옵션 A 채택 시)

### 5.1 마이그레이션 0027

```
internal/platform/storage/migrations/sqlite/0027_boolean_recovery.sql
internal/platform/storage/postgres/migrations/0027_boolean_recovery.up.sql
internal/platform/storage/postgres/migrations/0027_boolean_recovery.down.sql
```

**SQLite up**: SQLite는 BOOLEAN affinity 없음 → no-op marker(주석만). 또는 column rename 회피 위해 변경 0.

**PG up**:
```sql
ALTER TABLE roles                  ALTER COLUMN is_system  TYPE BOOLEAN USING (is_system  != 0);
ALTER TABLE compliance_frameworks  ALTER COLUMN enabled    TYPE BOOLEAN USING (enabled    != 0);
ALTER TABLE webhooks               ALTER COLUMN enabled    TYPE BOOLEAN USING (enabled    != 0);
ALTER TABLE webhook_deliveries     ALTER COLUMN succeeded  TYPE BOOLEAN USING (succeeded  != 0);
ALTER TABLE sso_providers          ALTER COLUMN enabled    TYPE BOOLEAN USING (enabled    != 0);

-- DEFAULT 변경 (SMALLINT 0/1 → BOOLEAN false/true)
ALTER TABLE roles                  ALTER COLUMN is_system  SET DEFAULT FALSE;
ALTER TABLE compliance_frameworks  ALTER COLUMN enabled    SET DEFAULT TRUE;
ALTER TABLE webhooks               ALTER COLUMN enabled    SET DEFAULT TRUE;
ALTER TABLE webhook_deliveries     ALTER COLUMN succeeded  SET DEFAULT FALSE;
ALTER TABLE sso_providers          ALTER COLUMN enabled    SET DEFAULT TRUE;
```

**PG down**: `ALTER COLUMN ... TYPE SMALLINT USING (CASE WHEN col THEN 1 ELSE 0 END)` + DEFAULT 복원.

### 5.2 sqliterepo 5 사이트 수정

각 사이트 패턴 (예: `internal/domain/tenant/sqliterepo/repo.go::createRole/scanRole`):

**Before**:
```go
isSystem := 0
if role.IsSystem { isSystem = 1 }
tx.Exec(ctx, `INSERT ... is_system ...`, ..., isSystem, ...)

var isSystem int
row.Scan(..., &isSystem, ...)
return Role{..., IsSystem: isSystem == 1}
```

**After**:
```go
tx.Exec(ctx, `INSERT ... is_system ...`, ..., role.IsSystem, ...)

var isSystem bool
row.Scan(..., &isSystem, ...)
return Role{..., IsSystem: isSystem}
```

### 5.3 WHERE literal 패턴 정리 (webhook_deliveries)

`internal/domain/integration/webhook/sqliterepo/repo.go`의 두 site:

**Before**:
```go
WHERE succeeded = 0 AND attempt_count < ? AND next_attempt_at <= ?
SET succeeded = 1, attempt_count = ?, ...
```

**After**:
```go
WHERE NOT succeeded AND attempt_count < ? AND next_attempt_at <= ?
SET succeeded = TRUE, attempt_count = ?, ...
```

또는 parameterized:
```go
WHERE succeeded = ? AND attempt_count < ? AND next_attempt_at <= ?
// args: false, max, now

SET succeeded = ?, attempt_count = ?, ...
// args: true, attempts, ...
```

**선택**: parameterized가 SQLite·PG 양 driver에서 type 호환 자유로움 (literal `TRUE`는 SQLite에서 INTEGER 1로 fallback되나 명시적 인자가 명확). **권장 parameterized**.

### 5.4 단위 테스트 fixture 수정

5 도메인 repo 단위 테스트에서 `enabled = 0`/`succeeded = 1` 형태 SQL literal을 parameterized로 일괄 정리. 도메인 객체 비교는 변경 0 (Go bool 그대로).

### 5.5 통합 테스트 신규

`internal/platform/storage/postgres/pgnative_boolean_integration_test.go`:
- 5 컬럼 각각 round-trip (INSERT bool true/false, SELECT bool — pgx native).
- WHERE 절: parameterized bool 인자, partial 결과 검증.
- testcontainers PG 16에서 실행, CI `pg-integration` job 자동.

---

## 6. TDD Stage 분해 (옵션 A 채택 시)

각 Stage 별 commit. 권장 분리 — **3 commit**.

### Stage 1 — modernc.org/sqlite bool 호환성 진단 + 1 컬럼 시범 회수 — 1 commit

- 진단 단위 테스트 신규: `internal/platform/storage/sqlite/bool_compat_test.go` — CREATE TABLE INTEGER + bool BIND/SCAN 양방향 동작 확인.
- 결과에 따라 D-E22F-1 결정.
- 만약 호환 OK → `roles.is_system` 1개 컬럼 시범 회수 (마이그레이션 0027 PG up 일부 + sqliterepo 1 site + 단위 테스트 fixture 갱신).
- 만약 호환 NX → 옵션 A 기각, 옵션 C로 fallback (본 design doc 결론으로 commit).

### Stage 2 — 잔여 4 컬럼 일괄 회수 + WHERE literal 정리 — 1 commit

- 마이그레이션 0027 완성 (5 ALTER + DEFAULT 5).
- sqliterepo 4 추가 site (compliance, webhooks×2, sso) + WHERE literal parameterized.
- 단위 테스트 fixture 4 도메인 갱신.
- `make ci` PASS 확인 (SQLite-only 경로).

### Stage 3 — testcontainers integration test + handoff — 1 commit

- `pgnative_boolean_integration_test.go` 신규 (5 컬럼 round-trip + WHERE 절 partial + UPDATE).
- CI `pg-integration` job 실행 → PASS 확인.
- `SESSION_HANDOFF.md` 업데이트, 결정 로그 한 줄.
- 1차 design doc(`e22-f-pg-native-design.md`) §4 Stage 4를 "완료" 마킹 + R30-1.2 commit 참조.

**총 ~0.5일** (Stage 1: 0.2일 — 진단이 핵심, Stage 2: 0.2일, Stage 3: 0.1일).

---

## 7. 결정 항목 (D-E22F-N)

각 항목 권장 default 명시 — 다음 세션 즉시 진입 부담 0.

### D-E22F-1 — 본 epic 추진 vs 보류

**선택지**:
1. **옵션 C 보류 (현 SMALLINT 유지)** ← **권장 default**
2. 옵션 A 추진 (5 컬럼 회수 + sqliterepo 5 site bool 패턴 변환)
3. 옵션 B Big bang driver-aware repo 진입 (R30-1 결정 번복)

**근거**: §4 권장 옵션 분석. ROI 부재 + 회귀 위험 vs 이득 비대칭. paying customer 진입 후 query optimization 요구 명확해질 때 옵션 B로 일괄 처리가 더 합리적.

### D-E22F-2 — (옵션 A 채택 시) WHERE literal 처리 방식

**선택지**:
1. **parameterized (`WHERE succeeded = ?`, false BIND)** ← **권장 default**
2. SQL literal `WHERE NOT succeeded` / `WHERE succeeded = TRUE`
3. mixed (read는 parameterized, UPDATE는 literal)

**근거**: parameterized가 SQLite·PG 양 driver에서 type 호환 자유. literal `TRUE`는 SQLite도 동작하나 명시적 인자가 코드 의도 명확. mixed는 일관성 ↓.

### D-E22F-3 — (옵션 A 채택 시) audit `current` 포함 여부

**선택지**:
1. **본 epic 비대상 (R30-1.3 별 commit 또는 driver-aware repo 진입 시 처리)** ← **권장 default**
2. 본 epic 포함 (partial unique index 조건 `WHERE current = 1` → `WHERE current` 동시 변환)

**근거**: `audit_chain_heads.current`는 partial unique index의 핵심 조건. PG-side는 `WHERE current` 또는 `WHERE current = TRUE`로 변경 필요. SQLite는 partial index에 boolean literal 미지원(boolean 컬럼 자체는 INTEGER affinity로 처리되나 인덱스 조건 expression 호환성 별도 검증 필요). 본 epic 5 컬럼과 무관한 추가 위험 — 분리 권장.

### D-E22F-4 — (옵션 A 채택 시) Stage 분리 단위

**선택지**:
1. **3 commit (Stage 1 진단+1컬럼 / Stage 2 4컬럼 / Stage 3 통합테스트+handoff)** ← **권장 default**
2. 2 commit (Stage 1+2 합 / Stage 3)
3. 단일 commit

**근거**: Stage 1 진단 결과에 따라 D-E22F-1이 옵션 A → C로 변할 수 있음. Stage 1을 commit 단위로 분리하면 진단만 남기고 후속 폐기 시 작업량 0. bisect 효율도 우위.

### D-E22F-5 — (옵션 A 채택 시) 마이그레이션 down 정책

**선택지**:
1. **down 작성 (`SMALLINT USING CASE WHEN col THEN 1 ELSE 0 END` + DEFAULT 복원)** ← **권장 default**
2. forward-only (down 미작성)

**근거**: 1차 design doc §5 forward-only 정책과 일관하나, dev/CI rollback 가정으로 down 작성. 운영 환경 down은 거의 없음. 비용 작음.

### D-E22F-6 — sqliterepo 단위 테스트 fixture 갱신 범위

**선택지**:
1. **5 도메인 repo 테스트의 모든 SQL literal 0/1 → parameterized 일괄 정리** ← **권장 default**
2. 호출 site만 부분 정리 (테스트 SQL은 sqlite-only로 SMALLINT-equivalent 유지 가능)

**근거**: testcontainers PG-integration job이 동일 fixture를 PG에서 실행 — 부분 정리 시 일부 PG에서 type mismatch. 일괄 정리가 안전.

---

## 8. 회귀 위험 / 운영 고려

### 8.1 modernc.org/sqlite의 bool 지원 (Stage 1 진단 핵심)

- 예상: bool BIND는 INTEGER 0/1로 자동 캐스트, bool SCAN은 INTEGER 0/1을 bool로 캐스트. SQLite 자체는 BOOLEAN affinity 없으나 driver layer 변환.
- **만약 비호환 시**: 옵션 A 기각, 옵션 C 유지. Stage 1 진단 commit 자체가 그 결론을 design doc 갱신으로 마감.
- 진단 비용: ~0.2일.

### 8.2 testcontainers PG-integration job 의존

- 본 epic의 1차 방벽은 CI `pg-integration` job (testcontainers PG 16). 본 job이 정상 동작 중인지 사전 확인 필요(직전 commit 지점 `b15a976` 기준 PASS 가정).
- **만약 testcontainers 환경 일시 비활성**(Docker 가용성 등) → 본 epic 진행 보류.

### 8.3 signed pack hash · audit chain 영향

- 본 epic은 마이그레이션·repo 코드만 변경 — pack/checks YAML 변경 0 → manifest hash 영향 0.
- audit_entries 테이블·payload 형식 변경 0 → hash chain 영향 0.

### 8.4 PG dev/prod 차이

- 본 epic은 in-place ALTER. 기존 데이터 0/1 → BOOLEAN false/true 무손실 변환.
- 인덱스 재생성 X (BOOLEAN 컬럼에 별도 인덱스 없음, 5 컬럼 모두). DEFAULT 변경은 schema 메타만.
- **운영 down time**: ALTER COLUMN TYPE은 PG에서 ACCESS EXCLUSIVE lock + 테이블 rewrite. 5 ALTER 순차 실행 시 짧은 lock window. 대용량 테이블 시 영향 검토 필요(현 단계는 paying customer 0 → 영향 0).

### 8.5 Customer 진입 후 BOOLEAN 회수 시점

- 옵션 C 채택 시 본 epic 보류. 향후 옵션 B(driver-aware repo) 진입 시점에 일괄 처리 자연스러움.
- 옵션 B 진입 트리거 시나리오:
  - paying customer query plan 분석 결과 BOOLEAN/JSONB 활용 query optimization 필요
  - 또는 audit `current` partial index 조건이 PG-native BOOLEAN 패턴 강제
  - 또는 multi-tenant scaling 단계에서 query plan 차이 측정 가능 수준 도달

---

## 9. 참조

- 직전 design doc: `docs/design/notes/e22-f-pg-native-design.md` (1차 핫 path 회수, R30-1=C 하이브리드)
- Phase 4 1차 회수 commit: `f3bf23f` (마이그레이션 0024)
- E22-E follow-up: `3c06290` (BOOLEAN/JSONB/TIMESTAMPTZ → SMALLINT/TEXT 통일 — driver mismatch 사례)
- 마이그레이션 디렉토리:
  - `internal/platform/storage/migrations/sqlite/` (0001~0026)
  - `internal/platform/storage/postgres/migrations/` (0001~0025, up/down 쌍)
- 후보 컬럼 정의:
  - `internal/platform/storage/migrations/sqlite/0004_roles.sql:11` — `roles.is_system`
  - `internal/platform/storage/migrations/sqlite/0015_compliance.sql:15` — `compliance_frameworks.enabled`
  - `internal/platform/storage/migrations/sqlite/0019_webhooks.sql:33,61` — `webhooks.enabled`, `webhook_deliveries.succeeded`
  - `internal/platform/storage/migrations/sqlite/0020_sso.sql:30` — `sso_providers.enabled`
- sqliterepo 호출 site:
  - `internal/domain/tenant/sqliterepo/repo.go:842,873,889,911` — roles
  - `internal/domain/compliance/sqliterepo/repo.go:89,311,334` — compliance_frameworks
  - `internal/domain/integration/webhook/sqliterepo/repo.go:88,134,175,196,347,372,424,445,458,480` — webhooks·deliveries
  - `internal/domain/tenant/sso/sqliterepo/repo.go:115,165,593,606` — sso_providers
- testcontainers integration: `internal/platform/storage/postgres/pgnative_hotpath_integration_test.go` (1차 패턴 reference)
- 설계 원칙: `docs/design/01-principles.md` §2(옵트인 지능화 — schema 단순성 우선) · §9(불변성 — append-only audit 영향 0) · §12(점진적 적용)
- phase5 backlog: `docs/design/phase5-backlog.md` E22-F (1차 완료 마킹, BOOLEAN 회수 carryover)
- 메모리 패턴: `feedback_design_doc_first.md` (0.5~1.0일 작업 — 본 design doc 우선 정책 적용 대상)
