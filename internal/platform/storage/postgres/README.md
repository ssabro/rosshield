# PostgreSQL Storage Adapter (E22 — scaffold)

> 본 디렉터리는 **E22 Stage A** scaffold 결과물입니다. SQLite 어댑터와 같은
> `storage.Storage` 인터페이스를 구현하지만, 도메인 마이그레이션 변환과 도메인
> repository PG 백엔드는 **후속 stage**에서 채워집니다.

## 위치

- 패키지: `github.com/ssabro/rosshield/internal/platform/storage/postgres`
- 라이브러리: [`github.com/jackc/pgx/v5`](https://pkg.go.dev/github.com/jackc/pgx/v5) (+ `pgxpool`, `pgconn`)
- 마이그레이션 도구(R20-5 결정): [`golang-migrate`](https://github.com/golang-migrate/migrate)

## Stage A 스코프

- ✅ `Postgres` struct: `storage.Storage` 인터페이스 컴파일 만족 (`var _ storage.Storage = (*Postgres)(nil)`).
- ✅ `Tx` / `Bootstrap`: tenant scope 분리 (SQLite 어댑터와 동일 시맨틱).
- ✅ Connection pool sizing 옵션 (`PoolConfig.MinConns/MaxConns/Lifetime/IdleTime/HealthCheckPeriod`).
- ✅ pgx 에러 → `storage.Err*` 매핑 (NotFound, Conflict, ForeignKey).
- ✅ `?` placeholder → `$N` rebind (따옴표 인지).
- ✅ 첫 마이그레이션 `0001_tenant_init.up.sql / .down.sql` (SQLite 0001+0003 변환).
- ✅ Makefile 타겟: `make pg-migrate-up` / `pg-migrate-down` / `pg-migrate-status` / `pg-migrate-create`.

## Stage A 한계 (의도적 미구현)

본 stage 는 scaffold 가 목적이라 다음은 **명시적 미구현 + 명확한 에러**로 남겨두었습니다.

| 영역 | 현재 상태 | 후속 stage |
|---|---|---|
| `Tx.Query` (`*sql.Rows` 반환) | `errors.New("...not yet supported in scaffold...")` | Tx 인터페이스 일반화 또는 `pgx.Rows` 노출 |
| `Tx.QueryRow` (`*sql.Row` 반환) | `panic("...not yet supported in scaffold...")` | 위와 동일 |
| `Postgres.Migrate(ctx)` | 명시적 에러 (golang-migrate CLI 사용 안내) | golang-migrate Go API 통합 |
| 0002~0019 마이그레이션 | 미작성 | 도메인별 변환 worktree |
| 도메인 repository PG 구현 | 없음 | 0002~0019 변환 후 |
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

## SQLite → PG 변환 노트 (0001 적용분)

| SQLite | PostgreSQL | 사유 |
|---|---|---|
| `INTEGER PRIMARY KEY AUTOINCREMENT` | `BIGSERIAL PRIMARY KEY` 또는 ULID `TEXT` | 본 마이그레이션은 ULID `TEXT` PK 유지 |
| `TEXT` (RFC3339Nano UTC) | `TIMESTAMPTZ` | 시간대 명시 + 인덱스 효율 |
| `TEXT` (JSON 본문) | `JSONB` | 인덱스/연산자 지원 |
| `BLOB` | `BYTEA` | 본 0001 에는 미사용. 0002 audit 변환 시 적용 |
| `?` placeholder | `$1, $2, …` | `rebind()` 가 자동 변환 |
| `LAST_INSERT_ROWID()` | `RETURNING id` | `Result.LastInsertId()` 명시 에러 |
| `CREATE TRIGGER ... BEFORE UPDATE ... RAISE(ABORT, ...)` | `CREATE RULE` 또는 PL/pgSQL trigger | 0002 audit 변환에서 처리 |
| `PRAGMA foreign_keys=ON` | (PG 기본 ON) | N/A |
| `PRAGMA journal_mode=WAL` | (PG WAL 기본) | N/A |

## 테스트

- `pg_test.go`: 인터페이스 매칭(컴파일 검증) + `rebind` 단위 테스트 + `Open` 입력 검증.
- 실제 PG 인스턴스가 필요한 통합 테스트는 본 stage 비목표.
  - 후속 stage 에서 `testcontainers-go` 또는 `deploy/compose/postgres.yml` 사용.

## 후속 stage 요약

1. **E22-B**: 0002~0019 SQLite 마이그레이션 PG 변환 (audit trigger, jsonb 인덱스, 외래키 cascade 정책 점검).
2. **E22-C**: `Tx` 인터페이스 일반화 (driver-agnostic Rows/Row) + 도메인 repo PG 구현.
3. **E22-D**: main.go 에 `--storage=postgres` flag + env 변수 결선.
4. **E22-E**: docker compose pgcontainer 통합 테스트 + 같은 도메인 테스트 PG 통과.
5. **E22-F**: helm chart 1차 (deploy/k8s/).
