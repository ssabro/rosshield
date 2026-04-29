package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/gofrs/flock"

	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/sqlite"
)

func TestStorageMigrateAppliesSchema(t *testing.T) {
	t.Parallel()

	s := newTestStorage(t)

	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// platform_info 테이블이 생성되었어야 함.
	err := s.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		var count int
		err := tx.QueryRow(ctx,
			`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='platform_info'`,
		).Scan(&count)
		if err != nil {
			return err
		}
		if count != 1 {
			t.Errorf("platform_info table count = %d, want 1", count)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("verify platform_info: %v", err)
	}
}

func TestStorageMigrateIdempotent(t *testing.T) {
	t.Parallel()

	s := newTestStorage(t)

	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}

	// 두 번째 Migrate도 성공해야 하고, 이미 적용된 마이그레이션이 재적용되지 않아야 함.
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}

	// 모든 마이그레이션이 정확히 한 번씩만 적용되어야 함.
	// 위 두 번의 Migrate 호출이 통과한 것 자체가 idempotency 증명.
	// goose 버전 테이블의 max version_id는 현재 마이그레이션 개수와 일치.
	err := s.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		var maxVersion int
		err := tx.QueryRow(ctx, `SELECT MAX(version_id) FROM goose_db_version`).Scan(&maxVersion)
		if err != nil {
			return err
		}
		const wantVersion = 12 // 0001~0006 + 0007 packs + 0008 fleets + 0009 credentials + 0010 robots + 0011 scan + 0012 evidence
		if maxVersion != wantVersion {
			t.Errorf("max version_id = %d, want %d", maxVersion, wantVersion)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("verify goose state: %v", err)
	}
}

func TestStorageMigrateReturnsErrMigrationLockedWhenHeldExternally(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := sqlite.Open(storage.Config{Driver: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	// 외부에서 마이그레이션 락 파일을 선점.
	externalLock := flock.New(dbPath + ".migration.lock")
	if locked, err := externalLock.TryLock(); err != nil || !locked {
		t.Fatalf("external pre-acquire: locked=%v err=%v", locked, err)
	}
	t.Cleanup(func() { _ = externalLock.Unlock() })

	// 짧은 timeout ctx로 Migrate 호출 → ErrMigrationLocked 기대.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err = s.Migrate(ctx)
	if !errors.Is(err, storage.ErrMigrationLocked) {
		t.Errorf("err = %v, want ErrMigrationLocked", err)
	}
}
