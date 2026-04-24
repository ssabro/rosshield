package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/ssabro/rosshield/internal/platform/storage"
)

type sqliteTx struct {
	tx       *sql.Tx
	tenantID storage.TenantID
}

func (t *sqliteTx) Exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	res, err := t.tx.ExecContext(ctx, query, args...)
	return res, mapErr(err)
}

func (t *sqliteTx) Query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	rows, err := t.tx.QueryContext(ctx, query, args...)
	return rows, mapErr(err)
}

func (t *sqliteTx) QueryRow(ctx context.Context, query string, args ...any) *sql.Row {
	return t.tx.QueryRowContext(ctx, query, args...)
}

func (t *sqliteTx) TenantID() storage.TenantID {
	return t.tenantID
}

// mapErr는 SQLite 어댑터에서 발생한 에러를 storage 공통 에러로 매핑합니다.
// 현재 매핑: trigger의 RAISE(ABORT, ...)에서 "immutable"을 포함하면 ErrImmutable.
// (audit_entries/checkpoints의 BEFORE UPDATE/DELETE trigger 메시지가 이 컨벤션을 따름.)
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if strings.Contains(msg, "immutable") {
		return fmt.Errorf("%w: %s", storage.ErrImmutable, msg)
	}
	return err
}
