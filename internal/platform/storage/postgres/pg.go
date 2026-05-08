package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4"
	migratepg "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/ssabro/rosshield/internal/platform/storage"
)

// Postgres는 storage.Storage의 PostgreSQL 구현입니다.
//
// 트랜잭션 진입점은 SQLite 어댑터와 동일한 두 가지로 분리됩니다 (R1-2):
//   - Tx(ctx, fn): tenant-scoped. ctx에 TenantID 없으면 ErrTenantMissing.
//   - Bootstrap(ctx, fn): tenant-less. 마이그레이션·system seed 전용.
//
// tenant 격리 전략은 SQLite와 동일: ctx 에서 tenant_id 를 꺼내 Tx.TenantID 로
// 노출하고, repository 계층의 모든 쿼리가 WHERE tenant_id 강제를 유지합니다.
// (RLS 전환은 후속 — 본 stage 비목표.)
//
// E22-C — stdlib bridge 추가:
//
//	pgx/v5/stdlib.OpenDBFromPool 로 *sql.DB 핸들을 동시에 보유. 이 핸들이 발급하는
//	*sql.Tx 를 통해 storage.Tx.Query/QueryRow 가 *sql.Rows·*sql.Row 를 그대로
//	반환할 수 있어 도메인 레포지토리(SQLite 시절 작성)가 코드 변경 없이 동작.
type Postgres struct {
	pool  *pgxpool.Pool
	sqlDB *sql.DB
}

// PoolConfig는 pgxpool 사이징 옵션입니다. 기본값은 보수적(1~10).
type PoolConfig struct {
	MinConns          int32
	MaxConns          int32
	MaxConnLifetime   time.Duration
	MaxConnIdleTime   time.Duration
	HealthCheckPeriod time.Duration
}

func (p PoolConfig) withDefaults() PoolConfig {
	if p.MinConns <= 0 {
		p.MinConns = 1
	}
	if p.MaxConns <= 0 {
		p.MaxConns = 10
	}
	if p.MaxConnLifetime <= 0 {
		p.MaxConnLifetime = time.Hour
	}
	if p.MaxConnIdleTime <= 0 {
		p.MaxConnIdleTime = 30 * time.Minute
	}
	if p.HealthCheckPeriod <= 0 {
		p.HealthCheckPeriod = time.Minute
	}
	return p
}

// Open은 PG storage 인스턴스를 엽니다.
// cfg.Driver 는 "postgres" 또는 "pg" 만 허용합니다 (빈 값은 호출자 실수 방지를 위해 거절).
// cfg.MaxOpen > 0 이면 pool MaxConns 로 사용됩니다.
func Open(cfg storage.Config) (*Postgres, error) {
	switch cfg.Driver {
	case "postgres", "pg":
	default:
		return nil, fmt.Errorf("postgres.Open: unsupported driver %q", cfg.Driver)
	}
	if cfg.DSN == "" {
		return nil, fmt.Errorf("postgres.Open: DSN is required")
	}
	pool := PoolConfig{}
	if cfg.MaxOpen > 0 {
		pool.MaxConns = int32(cfg.MaxOpen)
	}
	return OpenWithPool(cfg.DSN, pool)
}

// OpenWithPool은 명시적 PoolConfig 로 PG storage 를 엽니다 (테스트·튜닝 용도).
func OpenWithPool(dsn string, pc PoolConfig) (*Postgres, error) {
	pc = pc.withDefaults()

	pgxCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres: parse config: %w", err)
	}
	pgxCfg.MinConns = pc.MinConns
	pgxCfg.MaxConns = pc.MaxConns
	pgxCfg.MaxConnLifetime = pc.MaxConnLifetime
	pgxCfg.MaxConnIdleTime = pc.MaxConnIdleTime
	pgxCfg.HealthCheckPeriod = pc.HealthCheckPeriod

	pool, err := pgxpool.NewWithConfig(context.Background(), pgxCfg)
	if err != nil {
		return nil, fmt.Errorf("postgres: pool init: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: ping: %w", err)
	}

	// E22-C — stdlib bridge: 도메인 repo가 *sql.Rows·*sql.Row를 받을 수 있게
	// pgxpool 위에 database/sql 어댑터를 얹는다. Pool 자원은 단일 — Close 시 한 번만 닫힘.
	sqlDB := stdlib.OpenDBFromPool(pool)

	return &Postgres{pool: pool, sqlDB: sqlDB}, nil
}

// 컴파일 시점 인터페이스 매칭 보증.
var _ storage.Storage = (*Postgres)(nil)

func (p *Postgres) Tx(ctx context.Context, fn func(ctx context.Context, tx storage.Tx) error) error {
	tenantID := storage.TenantIDFromContext(ctx)
	if tenantID == "" {
		return storage.ErrTenantMissing
	}
	return p.runTx(ctx, tenantID, fn)
}

func (p *Postgres) Bootstrap(ctx context.Context, fn func(ctx context.Context, tx storage.Tx) error) error {
	return p.runTx(ctx, "", fn)
}

func (p *Postgres) runTx(ctx context.Context, tenantID storage.TenantID, fn func(ctx context.Context, tx storage.Tx) error) (retErr error) {
	// E22-C — stdlib bridge 사용. Tx 안에서 Query/QueryRow가 *sql.Rows·*sql.Row를
	// 반환해야 도메인 repo(SQLite 시절 작성)와 호환.
	rawTx, err := p.sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("postgres: BeginTx: %w", err)
	}
	tx := &postgresTx{tx: rawTx, tenantID: tenantID}

	defer func() {
		if r := recover(); r != nil {
			_ = rawTx.Rollback()
			panic(r)
		}
		if retErr != nil {
			if rbErr := rawTx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
				retErr = fmt.Errorf("%w (rollback: %v)", retErr, rbErr)
			}
			return
		}
		if cmErr := rawTx.Commit(); cmErr != nil {
			retErr = fmt.Errorf("postgres: Commit: %w", cmErr)
		}
	}()

	return fn(ctx, tx)
}

// Migrate는 embed된 PG 마이그레이션을 적용합니다 (E22-D).
//
// 사용 도구: golang-migrate/migrate/v4 (R20-5 결정).
// source: iofs(MigrationsFS의 "migrations" 디렉터리).
// driver: postgres (lib/pq 기반 — pgx와는 별개의 connection으로 동작).
//
// 멱등: 이미 적용된 마이그레이션은 건너뜀. ErrNoChange는 정상 동작.
// 실패: 부팅 fail-fast 권장.
func (p *Postgres) Migrate(ctx context.Context) error {
	src, err := iofs.New(MigrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("postgres: open embed migrations: %w", err)
	}
	defer func() { _ = src.Close() }()

	// stdlib bridge sqlDB를 그대로 사용 — pgx 기반 conn이지만 lib/pq 호환 driver.
	// golang-migrate postgres driver는 *sql.DB를 받음.
	dbDrv, err := migratepg.WithInstance(p.sqlDB, &migratepg.Config{})
	if err != nil {
		return fmt.Errorf("postgres: init migrate driver: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", src, "postgres", dbDrv)
	if err != nil {
		return fmt.Errorf("postgres: init migrate: %w", err)
	}
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("postgres: migrate up: %w", err)
	}
	return nil
}

// Close 는 connection pool 을 안전하게 종료합니다.
//
// stdlib bridge 가 같은 pool 을 공유하므로 sqlDB.Close 만으로 자원이 정리됩니다.
// pool.Close 는 idempotent 가 아니므로 한 번만 호출(p.sqlDB.Close 가 내부에서 처리).
func (p *Postgres) Close() error {
	if p.sqlDB != nil {
		return p.sqlDB.Close()
	}
	p.pool.Close()
	return nil
}

// Pool 은 마이그레이션 도구 / 헬스체크 등 어댑터 외부에서 풀을 직접 다뤄야 할 때
// 사용하는 escape hatch 입니다. 일반 도메인 코드는 사용 금지.
func (p *Postgres) Pool() *pgxpool.Pool {
	return p.pool
}

// ----------------------------------------------------------------------------
// Tx 구현
// ----------------------------------------------------------------------------

type postgresTx struct {
	tx       *sql.Tx
	tenantID storage.TenantID
}

// 컴파일 시점 인터페이스 매칭 보증.
var _ storage.Tx = (*postgresTx)(nil)

func (t *postgresTx) Exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	res, err := t.tx.ExecContext(ctx, rebind(query), args...)
	if err != nil {
		return nil, mapErr(err)
	}
	return res, nil
}

func (t *postgresTx) Query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	rows, err := t.tx.QueryContext(ctx, rebind(query), args...)
	if err != nil {
		return nil, mapErr(err)
	}
	return rows, nil
}

func (t *postgresTx) QueryRow(ctx context.Context, query string, args ...any) *sql.Row {
	return t.tx.QueryRowContext(ctx, rebind(query), args...)
}

func (t *postgresTx) TenantID() storage.TenantID {
	return t.tenantID
}

// rebind 는 SQLite 의 ? placeholder 를 PG 의 $1, $2, … 로 변환합니다.
// 따옴표 안의 ? 는 보존합니다. 매우 보수적인 구현(엣지 케이스는 후속 stage 에서 강화).
func rebind(query string) string {
	if !strings.ContainsRune(query, '?') {
		return query
	}
	var b strings.Builder
	b.Grow(len(query) + 8)
	n := 0
	inSingle := false
	inDouble := false
	for i := 0; i < len(query); i++ {
		c := query[i]
		switch c {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
			b.WriteByte(c)
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
			b.WriteByte(c)
		case '?':
			if inSingle || inDouble {
				b.WriteByte(c)
				continue
			}
			n++
			b.WriteByte('$')
			b.WriteString(itoa(n))
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// mapErr 는 PG 에러를 storage 공통 에러로 매핑합니다.
//
// stdlib bridge 가 sql.ErrNoRows 와 pgconn.PgError 둘 다 노출하므로 두 경로 모두 처리.
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, pgx.ErrNoRows) {
		return storage.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			return fmt.Errorf("%w: %s", storage.ErrConflict, pgErr.Message)
		case "23503": // foreign_key_violation
			return fmt.Errorf("%w: %s", storage.ErrForeignKey, pgErr.Message)
		}
	}
	return err
}
