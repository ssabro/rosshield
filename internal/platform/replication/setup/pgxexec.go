package setup

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PgxExecutor는 pgxpool.Pool 위에서 Executor를 구현합니다.
//
// CREATE PUBLICATION / CREATE SUBSCRIPTION은 transactional이지만 일부 PG 버전에서
// CREATE SUBSCRIPTION은 transaction block 안에서 실행 불가 — pool 직접 Exec으로
// auto-commit. 본 Executor는 audit Tx와 별 connection.
type PgxExecutor struct {
	pool *pgxpool.Pool
}

// NewPgxExecutor는 pgxpool.Pool을 wrap합니다.
func NewPgxExecutor(pool *pgxpool.Pool) *PgxExecutor {
	return &PgxExecutor{pool: pool}
}

// Exec은 결과 row가 없는 SQL을 실행합니다.
func (e *PgxExecutor) Exec(ctx context.Context, sql string, args ...any) error {
	if e.pool == nil {
		return fmt.Errorf("setup: PgxExecutor pool is nil")
	}
	if _, err := e.pool.Exec(ctx, sql, args...); err != nil {
		return err
	}
	return nil
}

// QueryBool은 단일 boolean 값을 반환하는 SELECT를 실행합니다.
func (e *PgxExecutor) QueryBool(ctx context.Context, sql string, args ...any) (bool, error) {
	if e.pool == nil {
		return false, fmt.Errorf("setup: PgxExecutor pool is nil")
	}
	var b bool
	if err := e.pool.QueryRow(ctx, sql, args...).Scan(&b); err != nil {
		return false, err
	}
	return b, nil
}

// QueryStrings는 단일 string 컬럼을 여러 row로 반환하는 SELECT를 실행합니다.
//
// publication tables 동기화·replication slot cleanup에서 사용. row가 없으면
// 빈 slice + nil error.
func (e *PgxExecutor) QueryStrings(ctx context.Context, sql string, args ...any) ([]string, error) {
	if e.pool == nil {
		return nil, fmt.Errorf("setup: PgxExecutor pool is nil")
	}
	rows, err := e.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]string, 0)
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
