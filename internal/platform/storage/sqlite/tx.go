package sqlite

import (
	"context"
	"database/sql"

	"github.com/ssabro/rosshield/internal/platform/storage"
)

type sqliteTx struct {
	tx       *sql.Tx
	tenantID storage.TenantID
}

func (t *sqliteTx) Exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return t.tx.ExecContext(ctx, query, args...)
}

func (t *sqliteTx) Query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return t.tx.QueryContext(ctx, query, args...)
}

func (t *sqliteTx) QueryRow(ctx context.Context, query string, args ...any) *sql.Row {
	return t.tx.QueryRowContext(ctx, query, args...)
}

func (t *sqliteTx) TenantID() storage.TenantID {
	return t.tenantID
}
