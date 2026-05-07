package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

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
type Postgres struct {
	pool *pgxpool.Pool
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
	return &Postgres{pool: pool}, nil
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
	rawTx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("postgres: BeginTx: %w", err)
	}
	tx := &postgresTx{tx: rawTx, tenantID: tenantID}

	defer func() {
		if r := recover(); r != nil {
			_ = rawTx.Rollback(context.Background())
			panic(r)
		}
		if retErr != nil {
			if rbErr := rawTx.Rollback(context.Background()); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
				retErr = fmt.Errorf("%w (rollback: %v)", retErr, rbErr)
			}
			return
		}
		if cmErr := rawTx.Commit(context.Background()); cmErr != nil {
			retErr = fmt.Errorf("postgres: Commit: %w", cmErr)
		}
	}()

	return fn(ctx, tx)
}

// Migrate는 PG 마이그레이션 적용 진입점입니다.
//
// 본 stage(E22-A) 는 scaffold 단계로, 0001 만 embed 되어 있습니다.
// 실제 적용 로직은 후속 stage(전체 0001~0019 변환 + golang-migrate 통합)에서
// 채워집니다. 호출 시 명시적 에러를 반환하여 "아직 미구현"임을 알립니다.
func (p *Postgres) Migrate(ctx context.Context) error {
	return errors.New("postgres: Migrate not yet implemented (E22-A scaffold). Use external golang-migrate CLI: see internal/platform/storage/postgres/README.md")
}

// Close 는 connection pool 을 안전하게 종료합니다.
func (p *Postgres) Close() error {
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
	tx       pgx.Tx
	tenantID storage.TenantID
}

// 컴파일 시점 인터페이스 매칭 보증.
var _ storage.Tx = (*postgresTx)(nil)

func (t *postgresTx) Exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	q := rebind(query)
	tag, err := t.tx.Exec(ctx, q, args...)
	if err != nil {
		return nil, mapErr(err)
	}
	return pgResult{tag: tag}, nil
}

func (t *postgresTx) Query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	// pgx.Tx.Query 는 *sql.Rows 가 아닌 pgx.Rows 를 반환합니다.
	// storage.Tx 인터페이스는 SQLite 시절의 *sql.Rows 를 노출합니다.
	// PG 어댑터에서 *sql.Rows 호환을 제공하려면 stdlib database/sql 어댑터
	// (jackc/pgx/v5/stdlib) 가 필요합니다 — 본 stage 비목표(repository 계층 미사용).
	//
	// 실제 PG 도메인 repository 구현은 후속 stage에서 진행되며, 그 시점에
	// 1) Tx 인터페이스를 driver-agnostic Rows 로 일반화하거나
	// 2) pgx 전용 메서드를 별도 인터페이스로 노출하는 결정이 필요합니다.
	// 본 stage 는 scaffold 만 수행하므로 명시적 미구현 에러를 반환합니다.
	_ = q(query) // 정적 사용 보장 (rebind 의존성 컴파일 검증).
	return nil, errors.New("postgres: Query returning *sql.Rows not yet supported in scaffold; see README §Limitations")
}

func (t *postgresTx) QueryRow(ctx context.Context, query string, args ...any) *sql.Row {
	// 위 Query 와 동일 사유로 본 stage 는 미구현. *sql.Row 의 zero value 는
	// 호출 시점에 .Scan 이 에러를 반환하지 않는 위험이 있어, 안전을 위해 panic 합니다.
	// 후속 stage 에서 Tx 인터페이스 일반화와 함께 정상 구현됩니다.
	panic("postgres: QueryRow not yet supported in scaffold; see README §Limitations")
}

func (t *postgresTx) TenantID() storage.TenantID {
	return t.tenantID
}

// q 는 컴파일 시 rebind 함수가 dead-code 로 제거되지 않도록 하는 trivial wrapper.
// (vet/staticcheck 친화 — 향후 Query 구현 시 제거 예정.)
func q(s string) string { return rebind(s) }

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

// pgResult 는 pgconn.CommandTag 를 sql.Result 로 어댑팅합니다.
// LastInsertId 는 PG 에 존재하지 않으므로 명시적 에러.
type pgResult struct {
	tag pgconn.CommandTag
}

func (r pgResult) LastInsertId() (int64, error) {
	return 0, errors.New("postgres: LastInsertId not supported (use RETURNING)")
}

func (r pgResult) RowsAffected() (int64, error) {
	return r.tag.RowsAffected(), nil
}

// mapErr 는 pgx 에러를 storage 공통 에러로 매핑합니다.
// 본 stage 는 최소 매핑(ErrNotFound·ErrConflict·ErrForeignKey)만 제공.
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
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
