package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ssabro/rosshield/internal/platform/storage"
)

type sqliteStorage struct {
	db       *sql.DB
	lockPath string
}

// Open은 SQLite 백엔드 Storage를 엽니다. PRAGMA는 매 connection에 자동 적용됩니다.
func Open(cfg storage.Config) (storage.Storage, error) {
	if cfg.Driver != "sqlite" && cfg.Driver != "" {
		return nil, fmt.Errorf("sqlite.Open: unsupported driver %q", cfg.Driver)
	}
	if cfg.DSN == "" {
		return nil, fmt.Errorf("sqlite.Open: DSN is required")
	}

	db := sql.OpenDB(newConnector(cfg.DSN))

	maxOpen := cfg.MaxOpen
	if maxOpen <= 0 {
		maxOpen = 1
	}
	db.SetMaxOpenConns(maxOpen)

	if err := db.PingContext(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlite.Open: ping: %w", err)
	}
	return &sqliteStorage{db: db, lockPath: cfg.DSN + ".migration.lock"}, nil
}

func (s *sqliteStorage) Tx(ctx context.Context, fn func(ctx context.Context, tx storage.Tx) error) error {
	tenantID := storage.TenantIDFromContext(ctx)
	if tenantID == "" {
		return storage.ErrTenantMissing
	}
	return s.runTx(ctx, tenantID, fn)
}

func (s *sqliteStorage) Bootstrap(ctx context.Context, fn func(ctx context.Context, tx storage.Tx) error) error {
	return s.runTx(ctx, "", fn)
}

func (s *sqliteStorage) runTx(ctx context.Context, tenantID storage.TenantID, fn func(ctx context.Context, tx storage.Tx) error) (retErr error) {
	rawTx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlite: BeginTx: %w", err)
	}
	tx := &sqliteTx{tx: rawTx, tenantID: tenantID}

	defer func() {
		if r := recover(); r != nil {
			_ = rawTx.Rollback()
			panic(r)
		}
		if retErr != nil {
			if rbErr := rawTx.Rollback(); rbErr != nil && rbErr != sql.ErrTxDone {
				retErr = fmt.Errorf("%w (rollback: %v)", retErr, rbErr)
			}
			return
		}
		if cmErr := rawTx.Commit(); cmErr != nil {
			retErr = fmt.Errorf("sqlite: Commit: %w", cmErr)
		}
	}()

	return fn(ctx, tx)
}

// Migrate는 embed된 SQLite 마이그레이션을 적용합니다. 실제 구현은 migrate.go.
func (s *sqliteStorage) Migrate(ctx context.Context) error {
	return s.migrate(ctx)
}

func (s *sqliteStorage) Close() error {
	return s.db.Close()
}
