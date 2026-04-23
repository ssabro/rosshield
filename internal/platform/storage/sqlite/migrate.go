package sqlite

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/gofrs/flock"
	"github.com/pressly/goose/v3"

	"github.com/ssabro/rosshield/internal/platform/storage"
)

const (
	migrationLockTimeout = 5 * time.Second
	migrationLockRetry   = 100 * time.Millisecond
)

// migrate은 SQLite 백엔드의 Storage.Migrate 구현입니다.
//
// R1-6 결정: 마이그레이션 직렬화를 위해 OS 파일 락을 5초 동안 시도합니다.
// 락 획득 실패 시 storage.ErrMigrationLocked.
// 마이그레이션 자체는 pressly/goose v3 + embed.FS로 적용 (R1-1·§4 결정).
// 실패 시 caller(부트 경로)는 fail-fast (R1-5).
func (s *sqliteStorage) migrate(ctx context.Context) error {
	lock := flock.New(s.lockPath)

	lockCtx, cancel := context.WithTimeout(ctx, migrationLockTimeout)
	defer cancel()

	locked, err := lock.TryLockContext(lockCtx, migrationLockRetry)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return storage.ErrMigrationLocked
		}
		return fmt.Errorf("sqlite: migration lock: %w", err)
	}
	if !locked {
		return storage.ErrMigrationLocked
	}
	defer func() { _ = lock.Unlock() }()

	dialectFS, err := fs.Sub(storage.MigrationsFS, "migrations/sqlite")
	if err != nil {
		return fmt.Errorf("sqlite: migrations sub-fs: %w", err)
	}

	provider, err := goose.NewProvider(goose.DialectSQLite3, s.db, dialectFS)
	if err != nil {
		return fmt.Errorf("sqlite: goose provider: %w", err)
	}

	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("sqlite: goose up: %w", err)
	}
	return nil
}
