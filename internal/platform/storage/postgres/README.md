# PostgreSQL Storage Adapter (E22)

> **현재 상태**: Stage A scaffold (E22-A) + Stage B 마이그레이션 변환 (E22-B) 완료.
> 도메인 repository PG 구현(`Tx` 인터페이스 일반화 포함)은 **E22-C 이후**에서 진행합니다.

## 위치

- 패키지: `github.com/ssabro/rosshield/internal/platform/storage/postgres`
- 라이브러리: [`github.com/jackc/pgx/v5`](https://pkg.go.dev/github.com/jackc/pgx/v5) (+ `pgxpool`, `pgconn`)
- 마이그레이션 도구(R20-5 결정): [`golang-migrate`](https://github.com/golang-migrate/migrate)

## Stage A 스코프 (E22-A)

- ✅ `Postgres` struct: `storage.Storage` 인터페이스 컴파일 만족 (`var _ storage.Storage = (*Postgres)(nil)`).
- ✅ `Tx` / `Bootstrap`: tenant scope 분리 (SQLite 어댑터와 동일 시맨틱).
- ✅ Connection pool sizing 옵션 (`PoolConfig.MinConns/MaxConns/Lifetime/IdleTime/HealthCheckPeriod`).
- ✅ pgx 에러 → `storage.Err*` 매핑 (NotFound, Conflict, ForeignKey).
- ✅ `?` placeholder → `$N` rebind (따옴표 인지).
- ✅ 첫 마이그레이션 `0001_tenant_init.up.sql / .down.sql` (SQLite 0001+0003 변환).
- ✅ Makefile 타겟: `make pg-migrate-up` / `pg-migrate-down` / `pg-migrate-status` / `pg-migrate-create`.

## Stage B 스코프 (E22-B) — 마이그레이션 변환

- ✅ SQLite 0002~0019 (총 18개) → PG `NNNN_name.up.sql` / `NNNN_name.down.sql` 분리 변환.
- ✅ audit 테이블의 `RAISE(ABORT, ...)` SQLite trigger → PL/pgSQL `RAISE EXCEPTION` 함수 + `CREATE TRIGGER`.
- ✅ JSON 컬럼 → `JSONB` (`'{}'::jsonb`, `'[]'::jsonb` 기본값).
- ✅ `BLOB` → `BYTEA` (sha256 32B, Ed25519 64B, AES-GCM ciphertext, PDF body, payload).
- ✅ `TEXT` (RFC3339Nano) → `TIMESTAMPTZ`.
- ✅ `INTEGER` (boolean) → `BOOLEAN` (roles.is_system, compliance_profiles.enabled, webhook_endpoints.enabled, webhook_deliveries.succeeded).
- ✅ `INTEGER` audit/chain seq, pdf_size, sig_chain_head_seq → `BIGINT`.
- ✅ partial unique index (`WHERE deleted_at IS NULL` 등) — PG도 동일 syntax.
- ✅ `ALTER TABLE ... DROP COLUMN` (0012) — PG 표준.
- ✅ FK `ON DELETE CASCADE` (evidence_refs) — 그대로 보존.

### 변환 파일 목록 (Stage B)

| 시퀀스 | 파일 | 비고 |
|---|---|---|
| 0002 | `0002_audit.{up,down}.sql` | `RAISE(ABORT)` → PL/pgSQL trigger 함수 4개 |
| 0003 | `0003_tenant_user.{up,down}.sql` | **NO-OP** — SQLite 0003은 PG 0001_tenant_init에 이미 통합됨 |
| 0004 | `0004_roles.{up,down}.sql` | `permissions` JSONB, `is_system` BOOLEAN |
| 0005 | `0005_api_keys.{up,down}.sql` | `scopes` JSONB |
| 0006 | `0006_auth_refresh.{up,down}.sql` | TIMESTAMPTZ 변환만 |
| 0007 | `0007_packs.{up,down}.sql` | `manifest_hash` BYTEA, `evaluation_rule` JSONB |
| 0008 | `0008_fleets.{up,down}.sql` | `policy` JSONB + partial unique index |
| 0009 | `0009_credentials.{up,down}.sql` | `encrypted_payload` BYTEA, `encryption_meta` JSONB |
| 0010 | `0010_robots.{up,down}.sql` | `tags` JSONB + 두 partial unique index |
| 0011 | `0011_scan.{up,down}.sql` | CHECK 제약 그대로, 인덱스 DESC 보존 |
| 0012 | `0012_evidence.{up,down}.sql` | `redactions` JSONB, FK CASCADE, ALTER TABLE DROP COLUMN |
| 0013 | `0013_reports.{up,down}.sql` | `pdf_blob`/`sig_bytes` BYTEA, sig_chain_head_seq BIGINT |
| 0014 | `0014_insights.{up,down}.sql` | `evidence_json`/`rules_applied` JSONB, REAL→DOUBLE PRECISION |
| 0015 | `0015_compliance.{up,down}.sql` | `enabled` BOOLEAN, statuses_json JSONB |
| 0016 | `0016_framework_reports.{up,down}.sql` | `pdf_blob`/`sig_bytes` BYTEA |
| 0017 | `0017_mapping_suggestions.{up,down}.sql` | `confidence` DOUBLE PRECISION |
| 0018 | `0018_advisor.{up,down}.sql` | `args_json`/`result_json` JSONB |
| 0019 | `0019_webhooks.{up,down}.sql` | `events` JSONB, `enabled`/`succeeded` BOOLEAN, `payload` BYTEA |

### Stage B 정적 sanity 검증 (테스트)

`migrations_test.go` 가 다음을 강제합니다 (실 PG 인스턴스 없이 정적):

- `TestAllMigrationFilesEmbedded` — 0001~0019 의 up/down 38개 파일이 embed FS에 모두 존재.
- `TestNoUnexpectedMigrationFiles` — 예상 외 파일이 디렉터리에 없음 (중복/오타 가드).
- `TestUpDownPairsExist` — 모든 시퀀스가 비어있지 않은 up/down 짝 보유.
- `TestNoSQLiteRemnants` — SQL 본문(주석 제외)에 `WITHOUT ROWID`, `RAISE(ABORT`, `PRAGMA `, `AUTOINCREMENT`, `+goose `, `BLOB` 토큰 부재.
- `TestPGConversionMarkersPresent` — NO-OP 제외 모든 up.sql 이 `JSONB` / `TIMESTAMPTZ` / `BYTEA` / `BOOLEAN` 중 적어도 하나 보유 (변환 누락 가드).
- `TestParenthesisBalance` — 따옴표·달러 인용·라인 주석 인지 괄호 균형 검사 (PL/pgSQL `$$...$$` 본문 안전 처리).
- `TestStatementsTerminatedBySemicolon` — 마지막 비공백 문자가 `;` 인지.

⚠️ **본 stage 한계**: 실제 PostgreSQL 인스턴스를 통한 통합 검증은 **E22-E** (testcontainers-go 또는 `deploy/compose/postgres.yml`) 에서 수행합니다. 본 stage 의 sanity check 는 SQL 문법·변환 누락의 1차 가드일 뿐 PG planner 가 받아들이는지를 보장하지 않습니다.

## Stage A 한계 (의도적 미구현)

본 stage 는 scaffold 가 목적이라 다음은 **명시적 미구현 + 명확한 에러**로 남겨두었습니다.

| 영역 | 현재 상태 | 후속 stage |
|---|---|---|
| `Tx.Query` (`*sql.Rows` 반환) | `errors.New("...not yet supported in scaffold...")` | Tx 인터페이스 일반화 또는 `pgx.Rows` 노출 |
| `Tx.QueryRow` (`*sql.Row` 반환) | `panic("...not yet supported in scaffold...")` | 위와 동일 |
| `Postgres.Migrate(ctx)` | 명시적 에러 (golang-migrate CLI 사용 안내) | golang-migrate Go API 통합 |
| 0002~0019 마이그레이션 | ✅ Stage B (E22-B) 완료 — 본 README 위 §Stage B 참조 | (완료) |
| 도메인 repository PG 구현 | 없음 | E22-C (Tx 인터페이스 일반화 선행) |
| `--storage=postgres` 결선 (main.go) | 없음 | 도메인 repo 완료 후 |
| RLS / SET LOCAL 기반 tenant 격리 | 코드 레벨 WHERE 강제만 | 검토 필요 |
| docker compose 통합 테스트 | 없음 | 도메인 repo 통과 후 |

`Query` / `QueryRow` 는 **현 시점에 PG repository 가 존재하지 않으므로 호출되지 않는다**는 전제 위에서 의도적으로 차단되었습니다. 도메인 repo 가 PG 를 사용하기 시작하면 인터페이스 일반화가 선행되어야 합니다 (별도 ADR).

## 사용 예 (현 시점)

```go
import (
    "github.com/ssabro/rosshield/internal/platform/storage"
    "github.com/ssabro/rosshield/internal/platform/storage/postgres"
)

s, err := postgres.Open(storage.Config{
    Driver:  "postgres",
    DSN:     "postgres://user:pass@localhost:5432/rosshield?sslmode=disable",
    MaxOpen: 10,
})
if err != nil { /* ... */ }
defer s.Close()

// Bootstrap (tenant-less) 트랜잭션 — system seed 용도.
_ = s.Bootstrap(ctx, func(ctx context.Context, tx storage.Tx) error {
    _, err := tx.Exec(ctx, `SELECT 1`)
    return err
})
```

## 환경 변수 (제안 — 후속 stage 에서 main.go 결선)

| 변수 | 의미 | 예시 |
|---|---|---|
| `ROSSHIELD_STORAGE_DRIVER` | `sqlite` 또는 `postgres` | `postgres` |
| `ROSSHIELD_PG_DSN` | pgx 호환 DSN | `postgres://rosshield:secret@db:5432/rosshield?sslmode=verify-full` |
| `ROSSHIELD_PG_MAX_CONNS` | pgxpool MaxConns | `25` |
| `ROSSHIELD_PG_MIN_CONNS` | pgxpool MinConns | `2` |

## 마이그레이션 적용 (현 stage)

`Migrate()` 메서드는 미구현입니다. CLI 로 적용하세요.

```bash
# golang-migrate 설치 (CGO 없이)
go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest

# 적용 (Makefile 타겟 사용 권장)
make pg-migrate-up   PG_DSN='postgres://...?sslmode=disable'
make pg-migrate-down PG_DSN='postgres://...?sslmode=disable'
make pg-migrate-status PG_DSN='postgres://...?sslmode=disable'
```

`migrate` CLI 가 PATH 에 없으면 위 `go install` 결과(`$GOPATH/bin/migrate`)가
PATH 에 있는지 확인하세요.

## SQLite → PG 변환 노트 (Stage A·B 통합 표)

| SQLite | PostgreSQL | 사유 |
|---|---|---|
| `INTEGER PRIMARY KEY AUTOINCREMENT` | `BIGSERIAL PRIMARY KEY` 또는 ULID `TEXT` | 본 마이그레이션은 ULID `TEXT` PK 유지 |
| `TEXT` (RFC3339Nano UTC) | `TIMESTAMPTZ` | 시간대 명시 + 인덱스 효율 |
| `TEXT` (JSON 본문) | `JSONB` | 인덱스/연산자 지원 |
| `BLOB` | `BYTEA` | 0002·0007·0009·0013·0016·0019 적용 |
| `?` placeholder | `$1, $2, …` | `rebind()` 가 자동 변환 |
| `LAST_INSERT_ROWID()` | `RETURNING id` | `Result.LastInsertId()` 명시 에러 |
| `CREATE TRIGGER ... BEFORE UPDATE ... RAISE(ABORT, ...)` | PL/pgSQL `CREATE OR REPLACE FUNCTION ... LANGUAGE plpgsql AS $$ RAISE EXCEPTION '...' END; $$;` + `CREATE TRIGGER` | 0002 audit 적용 (4 함수 + 4 트리거) |
| `INTEGER` (boolean 0/1) | `BOOLEAN` (TRUE/FALSE) | 0004 is_system, 0015 enabled, 0019 enabled/succeeded |
| `INTEGER` (큰 시퀀스/사이즈) | `BIGINT` | audit seq, pdf_size_bytes, sig_chain_head_seq |
| `REAL` | `DOUBLE PRECISION` | 0014 confidence, 0015 overall_score, 0017 confidence, 0018 cost_usd |
| `PRAGMA foreign_keys=ON` | (PG 기본 ON) | N/A |
| `PRAGMA journal_mode=WAL` | (PG WAL 기본) | N/A |

## 테스트

- `pg_test.go`: 인터페이스 매칭(컴파일 검증) + `rebind` 단위 테스트 + `Open` 입력 검증.
- 실제 PG 인스턴스가 필요한 통합 테스트는 본 stage 비목표.
  - 후속 stage 에서 `testcontainers-go` 또는 `deploy/compose/postgres.yml` 사용.

## 후속 stage 요약

1. ✅ **E22-A**: scaffold + 0001 변환 + Makefile 타겟 (완료).
2. ✅ **E22-B**: 0002~0019 SQLite 마이그레이션 PG 변환 (완료 — 본 README §Stage B).
3. **E22-C**: `Tx` 인터페이스 일반화 (driver-agnostic Rows/Row) + 도메인 repo PG 구현.
4. **E22-D**: main.go 에 `--storage=postgres` flag + env 변수 결선.
5. **E22-E**: docker compose pgcontainer 통합 테스트 + 같은 도메인 테스트 PG 통과 (실 PG 인스턴스 검증).
6. **E22-F**: helm chart 1차 (deploy/k8s/).
