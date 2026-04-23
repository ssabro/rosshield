# E1 Storage Deep-Dive — 저장소 레이어 설계 노트

> **상태**: Reference material (Phase 1 E1.T4/T5 구현 전 사전 검토)
> **작성일**: 2026-04-23
> **범위**: `internal/platform/storage/` 패키지 설계. SQLite 드라이버·PRAGMA·마이그레이션·Tx 추상·테넌시 격리·Audit append-only·테스트 전략.
> **참조**: `§01` P4·P9·P12, `§03.5`·`§03.10`, `§04` 전반, `§10.8`, `§11.5`, `phase1-backlog.md` E1/E2.
> **비목표**: PostgreSQL 운영(Phase 3), 분리 모드 샤딩, Blob Store 어댑터(E7 별도).

---

## 1. SQLite 드라이버 선택

### 후보 비교

| 항목 | `modernc.org/sqlite` | `mattn/go-sqlite3` |
|---|---|---|
| 구현 | Pure Go (C → Go 트랜스파일) | CGO 바인딩 |
| Windows 빌드 | 툴체인만 있으면 클린 | MinGW/MSVC + gcc 필요 |
| 크로스 컴파일 | `GOOS/GOARCH`만 바꾸면 됨 | `CC=<cross-gcc>` 매트릭스 복잡 |
| Goroutine 락 | DB pool 안전, serialized 기본 | 동일, C 레벨 mutex |
| 성능(write-heavy) | CGO 대비 10~30% 느림 | 기준점 |
| 바이너리 크기 | +8~10 MB | +3~4 MB |
| SQL 확장 | `JSON1`·`FTS5` 지원 | 동등 |

### 결정: `modernc.org/sqlite` 채택

**근거**:

1. **크로스 컴파일 = 릴리스 파이프라인의 단순성**. §11.10은 `fg-cli × {Linux/macOS/Windows} × {amd64/arm64}` 6개 아티팩트 + 어플라이언스 ARM을 요구합니다. CGO가 들어가면 cross-toolchain 매트릭스가 6개에서 12개로 늘어납니다. Phase 4 어플라이언스(arm64) 빌드가 CGO에서는 macOS 러너에서 cross-build 시 말썽을 부립니다.
2. **§11.2 "단일 정적 바이너리" 원칙**과 정합. `CGO_ENABLED=0`이 의미있게 유지됩니다.
3. **성능 격차는 Phase 1 스케일에서 무의미**. 감사 도구의 쓰기 경로는 초당 수십~수백 건 수준(스캔 결과·Evidence refs·Audit). 수만 TPS가 필요한 제품이 아닙니다.
4. **Windows 데스크톱 개발자 경험**. Tauri 개발 환경에서 MSVC 툴체인 의존을 제거하면 신규 기여자의 첫 빌드까지 걸리는 시간이 줄어듭니다.

### 트레이드오프 (받아들임)

- 바이너리 크기 +6 MB는 어플라이언스 이미지(수백 MB) 대비 미미.
- write-heavy 성능 저하는 마이그레이션 부트스트랩 1회 비용이므로 수용.
- 벤치마크에서 병목이 증명되면 `mattn/go-sqlite3`로 교체하되, `§9` 인터페이스 유지 — 드라이버는 `sql.Register` 수준 교체.

### 드라이버 래핑 원칙

- `database/sql` 표준 인터페이스만 사용, 드라이버 특수 확장 금지.
- `sql.Open("sqlite", dsn)` (modernc 기본 이름).

---

## 2. 필수 PRAGMA

SQLite는 **PRAGMA 설정에 따라 완전히 다른 엔진**이 됩니다. 커넥션 오픈 직후 아래 블록을 무조건 실행합니다.

```sql
PRAGMA foreign_keys = ON;
PRAGMA journal_mode = WAL;
PRAGMA synchronous = NORMAL;
PRAGMA busy_timeout = 5000;
PRAGMA temp_store = MEMORY;
PRAGMA cache_size = -20000;   -- 약 20 MB (음수는 KiB 단위)
PRAGMA wal_autocheckpoint = 1000;
```

### 항목별 근거

| PRAGMA | 값 | 근거 |
|---|---|---|
| `foreign_keys` | `ON` | 기본값 OFF — 함정. `§04.3` 스키마는 `REFERENCES tenants(id)` 등 FK 제약이 많고, `§04.9` 테넌시 격리의 보조 방어선. 테스트가 의도치 않게 유령 레코드를 만드는 것을 차단. |
| `journal_mode` | `WAL` | 읽기·쓰기 동시성 핵심. `§03.12` 동시성 모델에서 스캔 executor가 evidence를 배치 쓰기하는 동안 UI 읽기가 블록되지 않아야 함. DELETE journal보다 약 2~5배 빠른 쓰기. |
| `synchronous` | `NORMAL` | `FULL`은 매 커밋 `fsync` — 감사 도구에는 과함(WAL 체크포인트 때 sync). `NORMAL` + WAL은 전원 단절 시 **커밋된 트랜잭션 유지**, 체크포인트 진행 중인 것만 롤백. Audit 무결성은 트랜잭션 원자성에 의존하므로 안전. `OFF`는 금지(데이터 유실 가능). |
| `busy_timeout` | `5000` (ms) | `SQLITE_BUSY` 오류를 5초까지 자동 재시도. Go 레벨에서 리트라이 루프를 쓰는 것보다 드라이버 내부 스핀이 더 효율적. 스캔 병렬 write 버스트 흡수. |
| `temp_store` | `MEMORY` | 임시 테이블·정렬 버퍼를 메모리에 둠. 데스크톱·어플라이언스에서 디스크 temp 파일이 OS 백업 도구에 노출되지 않게. |
| `cache_size` | `-20000` (≈20 MB) | 기본 2 MB는 `§04` 조인이 잦은 리포트 쿼리에서 부족. 20 MB는 데스크톱에서 무리 없음. 어플라이언스·서버는 config로 override 가능. |
| `wal_autocheckpoint` | `1000` (pages) | 기본값. WAL 파일이 무한 커지는 것을 방지. Audit 테이블 write 버스트 후 조용한 시간에 자연 체크포인트. |

### PRAGMA 적용 방식

- `sql.Open`은 **lazy**이므로 `db.Conn(ctx)` 시점에 PRAGMA를 실행해야 함. `database/sql`의 `ConnectHook`(드라이버 의존) 대신, **커스텀 `driver.Connector` 래퍼**를 구현해 매 연결 확립 직후 PRAGMA 블록을 실행합니다.
- 단위 테스트: `TestPragmasApplied` — 새 연결에서 `PRAGMA journal_mode;`·`PRAGMA foreign_keys;` 조회 후 기대값 검증.
- PostgreSQL은 대응 개념이 대부분 서버 설정. 드라이버 내부는 `§3` 참조.

---

## 3. SQLite ↔ PostgreSQL 공존 전략

### 전략: "교집합 DDL + 소수 분기"

완전 단일 DDL은 허상입니다. 둘 다 SQL-92 초과를 쓰는 순간 달라집니다. 현실적인 노선은:

1. **공통 부분 집합 먼저** — `TEXT`·`INTEGER`·`REAL`·`TIMESTAMP`·`PRIMARY KEY`·`UNIQUE`·`FOREIGN KEY`·`INDEX`·`CHECK`는 양쪽 동일 문법.
2. **마이그레이션 파일 분기** — `0001_init.up.sqlite.sql` / `0001_init.up.pg.sql` 접미사로 로더가 선택. `§4`의 도구가 이를 지원해야 함.
3. **빌드 플래그 불필요** — DB 선택은 **런타임 설정**(`config.db.driver`). 빌드 타임 `USE_PG`는 피함. 같은 바이너리가 SQLite/PG 양쪽을 모두 열 수 있어야 `§11.2` 단일 바이너리 원칙과 §2.2(데스크톱 → 온프렘 전환) 시나리오가 자연스러움.

### 주요 차이점 매핑 표

| 개념 | SQLite | PostgreSQL | 채택 |
|---|---|---|---|
| 자동 증가 PK | `AUTOINCREMENT` | `GENERATED AS IDENTITY` | 양쪽 모두 회피 — `§04.4` ULID 문자열 사용. `audit.seq`는 `BIGINT` + 앱이 `MAX(seq)+1` 트랜잭션 내 계산. |
| 바이너리 | `BLOB` | `BYTEA` | 분기(해시·서명 수준). 대형 Evidence는 blob store이므로 실제 빈도 낮음. |
| JSON | `TEXT` + `json_*()` | `JSONB` | 분기. 도메인은 `[]byte` 원문만 주고받음. JSON path 쿼리 미사용. |
| 배열 | 없음 | `TEXT[]` | **SQLite로 통일** — `tags`·`permissions`는 JSON 문자열. `§04.3`의 PG `TEXT[]` 예시는 교집합으로 재수렴. |
| 타임스탬프 | `TEXT` ISO8601 | `TIMESTAMPTZ` | `TEXT` RFC3339Nano UTC. Go `time.Time` 변환. |
| `now()` 기본값 | `CURRENT_TIMESTAMP` | `now()` | DB 기본값 **사용 금지** — 앱이 `Clock.Now()` 주입. |
| 업서트 | `ON CONFLICT DO UPDATE` | 동일 | 공통 사용. |
| 외래키 강제 | PRAGMA 필요 | 기본 ON | `§2`에서 정렬. |
| 대소문자 | BINARY 기본 | 로케일 의존 | email은 앱에서 lowercase 정규화 후 저장. |

### Dialect 분기 규칙 (코드 레벨)

- 마이그레이션 도구가 디렉터리로 dialect 자동 선택(`§4`).
- Go 쿼리는 `sqlc`로 생성하되 dialect별로 같은 인터페이스·다른 구현. 도메인 서비스는 `Repository` 인터페이스만 보므로 dialect 무지.
- 예외 — Audit append-only: SQLite TRIGGER, PG RULE/TRIGGER는 마이그레이션 파일에서 분기(`§6`).

---

## 4. 마이그레이션 툴 평가

### 후보

| 도구 | 장점 | 단점 |
|---|---|---|
| `golang-migrate/migrate` | 업계 표준, up/down, 다양한 드라이버 | 의존성 큼, `embed.FS` 지원 투박 |
| `pressly/goose` | `embed.FS` 친화, Go 마이그레이션 가능 | SQL-only 쓰면 migrate와 동급 |
| `rubenv/sql-migrate` | 가벼움, 라이브러리 먼저 | 활동 감소, dialect 분기 약함 |
| Minimal custom | 딱 필요한 만큼 | "또 하나의 바퀴" — 버그·테스트 부담 |

### 결정: `pressly/goose` 채택

**근거**:

1. **`embed.FS` 통합이 깔끔**. 단일 바이너리에 마이그레이션을 포함해야 함(어플라이언스 오프라인 배포). `goose.SetBaseFS(embedFS)`로 끝.
2. **Dialect 분기 전략과 호환**. `0001_init.sql`에 SQLite·PG 문법 차이를 `-- +goose StatementBegin`·환경변수 분기로 처리 가능, 또는 별도 디렉터리(`migrations/sqlite/`, `migrations/pg/`)를 런타임에 선택.
3. **Go 마이그레이션 탈출구**. 데이터 백필(예: `§04.6`의 "N 릴리스 deprecate")처럼 SQL로 표현하기 어려운 변경은 Go 함수로 작성 가능. `migrate`는 SQL-only 경향.
4. **CLI가 얇고 라이브러리가 주력** — 우리는 CLI보다 **서버 부팅 시 자동 적용**(`§phase1-backlog E1.T5`)이 주 용도. goose가 이 use case에 자연스러움.
5. **"Minimal custom"을 거부한 이유** — Phase 0 코드 제로 상태이므로 유혹이 강하지만, 후행 기능(checksum 검증·lock·버전 이력 테이블)을 결국 다시 만들게 됩니다. §12.7 리스크 "NIH에 시간 낭비"를 피함.

### 파일 포맷 규약

```
internal/platform/storage/migrations/
  ├─ sqlite/
  │   ├─ 0001_init.sql
  │   ├─ 0002_audit_triggers.sql
  │   └─ 0003_benchmark_packs.sql
  └─ pg/
      ├─ 0001_init.sql
      ├─ 0002_audit_rules.sql
      └─ 0003_benchmark_packs.sql
```

- 파일명: `<4-digit-sequence>_<snake_case_name>.sql`
- 각 파일은 goose 마커로 `-- +goose Up` / `-- +goose Down` 섹션 구분.
- 한 파일은 **한 논리적 변경**만 담음(§04.6 점진성).

### 롤백 전략

- 모든 마이그레이션은 **`Down` 섹션 작성 의무**. 테스트는 up→down→up 왕복이 모두 성공해야 함(`§01` P12 검증 수단).
- 단 **`audit_entries` 테이블은 예외** — `Down`에서 DROP 금지, 트리거 제거만 허용. 이미 Audit 엔트리가 있는 환경에서 down migration이 체인을 지우는 사고 방지(`§10.8`).
- 프로덕션에서 자동 `Down`은 금지. 서버 기동 시 `Up`만 실행. `Down`은 개발·CI용 수동 명령(`rosshield-admin migrate down --steps 1`).

---

## 5. Tx 추상 — "Tx를 잊을 수 없는" 인터페이스

### 목표

1. Repository 메서드는 **반드시 `Tx`를 받는다**.
2. `*sql.DB`를 도메인 코드가 직접 보지 못한다.
3. "깜빡하고 Tx 없이 DB에 직접 쿼리"를 **컴파일 타임**에 차단.

### 인터페이스 스케치

```go
package storage

import (
    "context"
    "database/sql"
)

// Storage는 트랜잭션 진입점. 도메인은 이것만 주입받는다.
type Storage interface {
    // Tx는 도메인 트랜잭션. ctx에서 TenantID를 추출, 없으면 ErrTenantMissing.
    // fn이 error를 반환하면 롤백, nil이면 커밋. panic 시 복구 후 롤백 + re-panic.
    Tx(ctx context.Context, fn func(ctx context.Context, tx Tx) error) error

    // Bootstrap은 tenant-less 트랜잭션. 마이그레이션·system seed 전용 진입점.
    // 일반 도메인 코드에서 호출 금지(린트 차단). 부트 경로(server bootstrap, CLI admin 명령)에서만.
    Bootstrap(ctx context.Context, fn func(ctx context.Context, tx Tx) error) error

    // 마이그레이션은 부팅 경로에서만 호출. 실패 시 caller는 fail-fast (exit non-zero).
    Migrate(ctx context.Context) error

    Close() error
}

// Tx는 트랜잭션 안에서만 유효한 쿼리 핸들.
// *sql.Tx를 노출하지 않는다 — queryer 인터페이스만.
type Tx interface {
    Exec(ctx context.Context, query string, args ...any) (sql.Result, error)
    Query(ctx context.Context, query string, args ...any) (*sql.Rows, error)
    QueryRow(ctx context.Context, query string, args ...any) *sql.Row

    // TenantID는 `§7` 테넌시 격리용 컨텍스트.
    // Tx() 진입점은 항상 채워짐 (없으면 ErrTenantMissing). Bootstrap() 진입점은 빈 값.
    // tenant-aware Repository 메서드는 빈 TenantID 호출 시 panic해야 한다 (Bootstrap 경로 보호).
    TenantID() TenantID
}

// Repository는 도메인별로 구현. 제네릭 패턴은 §9 참조.
type Repository[T any] interface {
    Get(ctx context.Context, tx Tx, id string) (T, error)
    List(ctx context.Context, tx Tx, filter Filter) ([]T, error)
    Insert(ctx context.Context, tx Tx, entity T) error
    // Update/Delete는 도메인별로 허용 여부 다름. 일반화하지 않는다.
}
```

### 사용 예시

```go
func (s *RobotService) Create(ctx context.Context, req CreateRobotRequest) (Robot, error) {
    var created Robot
    err := s.storage.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
        robot := buildRobot(req, s.idgen, s.clock)
        if err := s.robots.Insert(ctx, tx, robot); err != nil {
            return err
        }
        if err := s.audit.Append(ctx, tx, audit.RobotCreated(robot)); err != nil {
            return err
        }
        created = robot
        return nil
    })
    return created, err
}
```

### "Tx를 잊을 수 없다"의 강제

1. **`Storage`가 `Query`/`Exec`를 노출하지 않음**. 쓰려면 반드시 `Tx()` 콜백 안에 들어와야 함 → 컴파일 타임.
2. **Repository 메서드 시그니처에 `tx Tx` 필수**. 없으면 컴파일 에러.
3. **린트 규칙** — `depguard` + 자체 analyzer: `database/sql.DB` 또는 `*sql.DB`를 `internal/domain/*`·`internal/app/*`에서 import 금지. `internal/platform/storage` 내부에서만 허용.
4. **`sqlc` 생성 쿼리**는 `DBTX` 인터페이스를 받음. 우리의 `storage.Tx`를 `DBTX`에 adapt하는 래퍼를 platform이 제공, 도메인 코드는 `tx`만 전달.

### 중첩 트랜잭션

- 초기 단계에서는 **금지**. 중첩 `Tx()` 호출 시 패닉.
- Audit.Append가 서비스 안에서 호출되는 패턴은 **상위 Tx를 전달**하는 방식(위 예시)으로 해결 — 중첩 불필요.
- 나중에 saga(§03.10)가 도입되면 savepoint 기반 중첩을 별도 API로 추가.

---

## 6. Audit Append-Only 집행 (§10.8)

### 두 개의 방어선

1. **애플리케이션**: `AuditRepository`는 `Insert`만 노출. `Update`·`Delete` 메서드 자체가 없음.
2. **데이터베이스**: 트리거/룰이 직접 UPDATE/DELETE를 거부. 관리자가 실수로 `sqlite3` CLI로 접근해도 차단.

### SQLite 트리거 DDL (`0002_audit_triggers.sql`)

```sql
-- +goose Up
CREATE TABLE audit_entries (
    tenant_id       TEXT    NOT NULL REFERENCES tenants(id),
    seq             INTEGER NOT NULL,
    occurred_at     TEXT    NOT NULL,          -- ISO8601 UTC
    actor_type      TEXT    NOT NULL,
    actor_id        TEXT    NOT NULL,
    action          TEXT    NOT NULL,
    target_type     TEXT    NOT NULL,
    target_id       TEXT    NOT NULL,
    payload_digest  TEXT    NOT NULL,          -- hex sha256
    outcome         TEXT    NOT NULL,
    prev_hash       TEXT    NOT NULL,
    hash            TEXT    NOT NULL,
    PRIMARY KEY (tenant_id, seq)
);

CREATE INDEX audit_entries_tenant_time
  ON audit_entries(tenant_id, occurred_at);

CREATE TRIGGER audit_entries_no_update
BEFORE UPDATE ON audit_entries
BEGIN
    SELECT RAISE(ABORT, 'audit log is immutable (§10.8)');
END;

CREATE TRIGGER audit_entries_no_delete
BEFORE DELETE ON audit_entries
BEGIN
    SELECT RAISE(ABORT, 'audit log is immutable (§10.8)');
END;

-- +goose Down
-- 의도적으로 테이블을 DROP 하지 않는다 (§10.8).
-- 트리거만 제거하여 down 마이그레이션이 성공하게 한다.
DROP TRIGGER IF EXISTS audit_entries_no_update;
DROP TRIGGER IF EXISTS audit_entries_no_delete;
```

### PostgreSQL 대응 (`pg/0002_audit_triggers.sql`) — R1-3 결정 반영

```sql
-- +goose Up
CREATE TABLE audit_entries (/* 동일 컬럼 */);

CREATE OR REPLACE FUNCTION audit_entries_block_mutation() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'audit log is immutable (§10.8)';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER audit_entries_no_update
  BEFORE UPDATE ON audit_entries
  FOR EACH ROW EXECUTE FUNCTION audit_entries_block_mutation();

CREATE TRIGGER audit_entries_no_delete
  BEFORE DELETE ON audit_entries
  FOR EACH ROW EXECUTE FUNCTION audit_entries_block_mutation();
```

- R1-3 결정으로 `DO INSTEAD NOTHING`(조용한 무시)는 폐기. SQLite RAISE(ABORT)와 동일 의미론(에러 발생) 유지 — 백업 복원·악의적 수정 시도가 모두 명시적 실패로 드러납니다.

### 테스트 (E2.T4에서 구현)

```go
func TestAuditUpdateRejected(t *testing.T) {
    s := newTestStorage(t)
    seedAuditEntry(t, s)
    err := s.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
        _, e := tx.Exec(ctx, `UPDATE audit_entries SET hash='tampered' WHERE seq=1`)
        return e
    })
    require.ErrorContains(t, err, "audit log is immutable")
}
```

---

## 7. 테넌시 격리 (§04.9, P4)

### 설계 선택: 로우 레벨 격리 (Row-Level) + 향후 스키마 옵션

- Phase 1: 모든 테이블 `tenant_id` 컬럼. 모든 쿼리에 `WHERE tenant_id = :tenant_id`.
- Phase 3: 대형 고객 옵션으로 `schema_per_tenant` — PG에서 `tenant_<id>` 스키마 자동 생성. SQLite는 **단일 테넌트 전용 파일 DB**로 우회.

### 실패 모드 (회피해야 할 것)

1. **`WHERE tenant_id` 누락** → cross-tenant 유출.
2. **조인 중 한쪽만 필터** → 타 테넌트 robot 참조 가능.
3. **"admin 편의 조회"**가 프로덕션 경로에 섞임.

### 강제 메커니즘

1. **`Tx.TenantID()` 필수** — `§5`의 `Tx`가 tenant context를 소유. Repository가 쿼리 작성 시 `tx.TenantID()`를 직접 바인드. 서비스가 파라미터로 넘길 수 없음.
2. **Tenant context 전파** — HTTP 미들웨어가 JWT 검증 후 `ctx`에 tenant ID 주입 → `Storage.Tx(ctx, ...)`가 ctx에서 꺼내 `Tx` 객체에 심음. 서비스 코드는 tenant를 절대 **인자로** 받지 않음(주입을 우회할 수 없도록).
3. **sqlc 템플릿 검토** — 모든 SELECT·UPDATE·DELETE 쿼리는 PR 리뷰에서 `tenant_id = ?` 존재 여부를 체크. 자체 analyzer로 `§8` 테스트에서 fuzz.
4. **통합 테스트** (E3.T8 `TestTenantScopeBlocksCrossTenantRead`): 두 테넌트에 각기 데이터 삽입 후 A 테넌트 ctx로 모든 repo 메서드에 B의 ID를 요청 — 모두 `ErrNotFound` 기대(존재 누설 방지 — `§06` 원칙).
5. **Property-based fuzzer** (E3 exit 기준): 모든 repo 메서드에 랜덤 tenantA/tenantB · ID 조합을 던져 cross leakage 0건 검증.

### Cross-Tenant 접근이 필요한 경우

- 시스템 팩(`tenant_id = 'system'`): 전용 메서드 `ListSystemPacks`로 경로 분리.
- 관리자 콘솔(`§04.9`): Phase 3 범위, 본 문서 out of scope.

---

## 8. 테스트 전략

### 레이어별 선택

| 테스트 종류 | 저장소 구현 | 이유 |
|---|---|---|
| 단위 테스트 (repo 단위) | In-memory SQLite (`file::memory:?cache=shared`) | 빠름. 각 테스트 독립 DB. |
| 도메인 통합 테스트 | **파일 기반 SQLite**(tempdir) | WAL·트리거·FK가 실제와 동일. 마이그레이션까지 적용. |
| Audit 체인 테스트 | 파일 기반 SQLite | 트리거 집행 확인 필수. |
| 마이그레이션 왕복 테스트 | 파일 기반 SQLite | `§01` P12 검증: up→down→up 루프. |
| PG 통합 테스트 | `testcontainers-go` + postgres:16 | Phase 3부터. Phase 1은 "green 확인" 수준 스모크만. |

### In-memory vs file-backed 결정 기준

- **In-memory 선호**: 순수 CRUD 로직, 초당 수백개 테스트.
- **file-backed 필수**: WAL 동작 확인, 트리거 집행, 동시성, 마이그레이션 두 번 적용 후 상태.
- `modernc.org/sqlite`의 `:memory:`는 WAL을 지원하지만 PRAGMA 일부가 효과 없음 — 실전 파이프라인과 괴리. 프로젝트 기본값은 **`t.TempDir()`에 파일 생성**.

### 리셋 메커니즘

헬퍼 `newTestStorage(t)`는 `t.TempDir()/test.db`를 열고 `Migrate`까지 적용 후 `t.Cleanup`에 Close 등록.

- `t.TempDir()` 자동 삭제로 리셋 비용 0.
- `t.Parallel()` 테스트가 DB 파일을 공유하지 않음.
- 매 테스트 마이그레이션 ~50ms 비용 수용.

### 공통 헬퍼 위치

- `internal/platform/storage/storagetest` 패키지로 분리. 모든 도메인 테스트가 `storagetest.New(t)` 한 줄로 사용.

### Phase 3 PostgreSQL 통합

- `testcontainers-go`로 `postgres:16-alpine` 기동, 포트 자동 할당.
- CI에서 Docker-in-Docker 필요 → GitHub Actions `docker`가 이미 제공.
- 같은 통합 테스트 스위트를 **양쪽 드라이버로 두 번 실행**(`go test -tags=integration,pg`) — dialect drift 조기 감지.

---

## 9. Go 인터페이스 스케치

### 파일 레이아웃

```
internal/platform/storage/
  ├─ storage.go       # Storage, Tx, Config 인터페이스
  ├─ sqlite/          # driver.go(PRAGMA hook) · storage.go · tx.go
  ├─ pg/              # Phase 3
  ├─ migrations/      # sqlite/*.sql · pg/*.sql
  ├─ embed.go         # //go:embed migrations
  └─ storagetest/     # 테스트 헬퍼
```

### 핵심 타입

```go
package storage

import (
    "context"
    "database/sql"
    "errors"
    "time"
)

type TenantID string

type Config struct {
    Driver   string        // "sqlite" | "pg"
    DSN      string        // file path 또는 postgres:// URL
    MaxOpen  int           // default 1 for SQLite, 25 for PG
    BusyMS   int           // default 5000
    LogSlow  time.Duration // default 200ms
}

type Storage interface {
    // Tx는 tenant-scoped 트랜잭션. ctx에 TenantID 없으면 ErrTenantMissing.
    Tx(ctx context.Context, fn func(ctx context.Context, tx Tx) error) error
    // Bootstrap은 tenant-less 트랜잭션. 마이그레이션·system seed 전용.
    Bootstrap(ctx context.Context, fn func(ctx context.Context, tx Tx) error) error
    Migrate(ctx context.Context) error
    Close() error
}

type Tx interface {
    Exec(ctx context.Context, query string, args ...any) (sql.Result, error)
    Query(ctx context.Context, query string, args ...any) (*sql.Rows, error)
    QueryRow(ctx context.Context, query string, args ...any) *sql.Row
    // Tx() 진입점은 항상 채워짐. Bootstrap() 진입점은 빈 값 ("").
    TenantID() TenantID
}

// 공통 에러 — 드라이버별 에러를 도메인이 알 필요 없도록.
var (
    ErrNotFound        = errors.New("storage: not found")
    ErrConflict        = errors.New("storage: conflict")    // UNIQUE 위반
    ErrForeignKey      = errors.New("storage: foreign key violation")
    ErrImmutable       = errors.New("storage: target is immutable")  // audit 트리거
    ErrTenantMissing   = errors.New("storage: tenant context missing")
    ErrMigrationLocked = errors.New("storage: migration already in progress (file lock held)")
)

// Open은 Config 기반으로 Storage를 생성. 드라이버 선택은 여기서.
func Open(cfg Config) (Storage, error)
```

### Repository 제네릭 패턴

```go
package storage

// Repository는 명명된 CRUD의 공통 형태. 도메인 특수 메서드는 개별 인터페이스에 추가.
type Repository[T any, ID ~string] interface {
    Get(ctx context.Context, tx Tx, id ID) (T, error)
    Insert(ctx context.Context, tx Tx, entity T) error
    // Update/Delete는 각 도메인 인터페이스에서 명시적으로 추가 (audit은 없음).
}

// 도메인 예시 (internal/domain/robot/repository/repo.go):
type Repo interface {
    storage.Repository[Robot, RobotID]
    ListByFleet(ctx context.Context, tx storage.Tx, fleet FleetID) ([]Robot, error)
    SoftDelete(ctx context.Context, tx storage.Tx, id RobotID) error
}
```

### 마이그레이션 실행

`Storage.Migrate`는 goose에 위임 — `goose.SetBaseFS(embedMigrations)` + `goose.SetDialect(...)` + `goose.UpContext`. `Migration` 구조체는 goose에게 숨기고 외부로 노출하지 않음(교체 가능성 유지).

### Clock·IDGen 의존성 주입

- `Storage`는 `Clock`·`IDGen`을 **직접 쓰지 않음**. 엔터티는 도메인 서비스가 생성 후 Repository에 전달. Storage는 dumb persistence.
- 이유: 저장소가 ID·시간을 생성하면 테스트가 이를 mock하기 위해 Storage 전체를 mock해야 함. 경계를 좁게 유지.

---

## 10. 결정 사항 (2026-04-23 합의)

> Phase 1 E1.T4/T5 착수 전 7건 미해결 질문에 대한 결정. 이 결정들이 본 노트의 §5·§6·§9 본문과 충돌하면 본문이 갱신되었습니다. 변경 이력은 commit log 참조.

1. **R1-1 · `sqlc` 도입 타이밍 = E5(Robot)부터** — E1~E4(Storage·Audit·Tenant·Pack)는 쿼리가 단순하므로 수작업이 빠릅니다. E5에서 도메인 CRUD 본격화 시 sqlc 도입해 학습·세팅 비용 회수. `Repository[T]` 제네릭 패턴은 E1~E4 동안 검증 후 sqlc-generated 쿼리와 어떻게 합치할지 E5 진입 시 재설계.

2. **R1-2 · tenant 누락 시 동작 = 두 진입점 분리** — `Storage.Bootstrap(ctx, fn)` (tenant-less, 마이그레이션·seed 전용) + `Storage.Tx(ctx, fn)` (ctx에 tenant 없으면 `ErrTenantMissing` 반환). "system" 특수값은 누설 위험 + 의미 모호로 회피. §5·§9 본문 인터페이스에 반영됨.

3. **R1-3 · PG audit append-only 정책 = TRIGGER + RAISE EXCEPTION** — `DO INSTEAD NOTHING`(조용한 무시)는 디버깅 악몽 + 백업 복원 추적 불가로 폐기. SQLite RAISE(ABORT)와 동일 의미론(에러 발생) 유지. §6 본문 PG 마이그레이션 예시 갱신됨.

4. **R1-4 · `ReadOnly` Tx 불필요 = `Tx()` 하나만** — SQLite WAL이 RO 최적화 이점 미미. 인터페이스 단순화. Phase 3 PG 도입 시 재검토. §5·§9 본문에서 `ReadOnly` 시그니처 제거됨.

5. **R1-5 · 마이그레이션 실패 시 부팅 = fail-fast (exit non-zero)** — 불완전 상태로 켜진 게 더 위험. 어플라이언스는 systemd restart로 자연 회복. "rollback last migration + retry" 옵션은 Phase 4 어플라이언스 진입 시 검토.

6. **R1-6 · 멀티프로세스 락 = 마이그레이션에만 OS file lock** — 일반 쿼리는 WAL이 멀티프로세스 안전. 마이그레이션은 OS-level lock(`flock` / `LockFileEx`) 획득, 5초 대기 후 `ErrMigrationLocked`. Phase 1은 "단일 프로세스 가정" 명시 + CLI 직접 DB 접근은 비권장 (server HTTP 경유 권장).

7. **R1-7 · SQLCipher = Phase 1 스코프 밖** — 컬럼 단위 암호화(Credential KEK/DEK §06)로 Phase 1 충분. FDE는 OS·어플라이언스 책임. 엔터프라이즈 SKU에서 DB 파일 자체 암호화 요구가 들어오면 Phase 3에서 드라이버 재결정 + SQLCipher 또는 SEE(SQLite Encryption Extension) 평가.
