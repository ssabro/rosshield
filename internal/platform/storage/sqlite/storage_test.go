package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/sqlite"
)

func newTestStorage(t *testing.T) storage.Storage {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := sqlite.Open(storage.Config{Driver: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return s
}

// createFooTable은 Bootstrap 경로로 단순한 테스트 테이블을 만듭니다.
func createFooTable(t *testing.T, s storage.Storage) {
	t.Helper()
	err := s.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		_, err := tx.Exec(ctx, `CREATE TABLE foo (id TEXT PRIMARY KEY, val TEXT NOT NULL)`)
		return err
	})
	if err != nil {
		t.Fatalf("createFooTable: %v", err)
	}
}

func TestStorageTxCommitAndRollback(t *testing.T) {
	t.Parallel()

	s := newTestStorage(t)
	createFooTable(t, s)

	ctx := storage.WithTenantID(context.Background(), "tn_test")

	// Commit: insert in Tx, then row exists.
	err := s.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO foo (id, val) VALUES (?, ?)`, "id_keep", "kept")
		return err
	})
	if err != nil {
		t.Fatalf("commit Tx: %v", err)
	}

	// Rollback: insert then return error.
	rollbackErr := errors.New("intentional")
	err = s.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO foo (id, val) VALUES (?, ?)`, "id_drop", "dropped"); err != nil {
			return err
		}
		return rollbackErr
	})
	if !errors.Is(err, rollbackErr) {
		t.Errorf("rollback Tx err = %v, want intentional", err)
	}

	// Verify: id_keep present, id_drop absent.
	err = s.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		var val string
		if err := tx.QueryRow(ctx, `SELECT val FROM foo WHERE id = ?`, "id_keep").Scan(&val); err != nil {
			return err
		}
		if val != "kept" {
			t.Errorf("id_keep val = %q, want kept", val)
		}
		err := tx.QueryRow(ctx, `SELECT val FROM foo WHERE id = ?`, "id_drop").Scan(&val)
		if err == nil {
			t.Errorf("id_drop should be absent, but got val=%q", val)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("verify Tx: %v", err)
	}
}

func TestStorageTxRequiresTenantID(t *testing.T) {
	t.Parallel()

	s := newTestStorage(t)

	err := s.Tx(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		t.Errorf("fn should not be called when tenant is missing")
		return nil
	})
	if !errors.Is(err, storage.ErrTenantMissing) {
		t.Errorf("err = %v, want ErrTenantMissing", err)
	}
}

func TestStorageBootstrapAllowsTenantLess(t *testing.T) {
	t.Parallel()

	s := newTestStorage(t)

	err := s.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		if got := tx.TenantID(); got != "" {
			t.Errorf("Bootstrap tx.TenantID = %q, want empty", got)
		}
		_, err := tx.Exec(ctx, `CREATE TABLE bar (id TEXT)`)
		return err
	})
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
}

func TestStorageTxPropagatesTenantID(t *testing.T) {
	t.Parallel()

	s := newTestStorage(t)
	createFooTable(t, s)

	const want storage.TenantID = "tn_propagate"
	ctx := storage.WithTenantID(context.Background(), want)

	err := s.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		if got := tx.TenantID(); got != want {
			t.Errorf("tx.TenantID = %q, want %q", got, want)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Tx: %v", err)
	}
}

func TestStoragePragmasApplied(t *testing.T) {
	t.Parallel()

	s := newTestStorage(t)

	err := s.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		var journalMode string
		if err := tx.QueryRow(ctx, `PRAGMA journal_mode`).Scan(&journalMode); err != nil {
			return err
		}
		if !strings.EqualFold(journalMode, "wal") {
			t.Errorf("journal_mode = %q, want wal", journalMode)
		}

		var foreignKeys int
		if err := tx.QueryRow(ctx, `PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
			return err
		}
		if foreignKeys != 1 {
			t.Errorf("foreign_keys = %d, want 1", foreignKeys)
		}

		var busyTimeout int
		if err := tx.QueryRow(ctx, `PRAGMA busy_timeout`).Scan(&busyTimeout); err != nil {
			return err
		}
		if busyTimeout != 5000 {
			t.Errorf("busy_timeout = %d, want 5000", busyTimeout)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Bootstrap pragma check: %v", err)
	}
}

func TestStorageTxRollbackOnPanic(t *testing.T) {
	t.Parallel()

	s := newTestStorage(t)
	createFooTable(t, s)

	ctx := storage.WithTenantID(context.Background(), "tn_panic")

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("Tx should re-panic")
		}
		if msg, ok := r.(string); !ok || msg != "boom" {
			t.Errorf("recovered = %v, want boom", r)
		}

		// After panic+rollback, the inserted row must NOT exist.
		readCtx := storage.WithTenantID(context.Background(), "tn_panic")
		err := s.Tx(readCtx, func(ctx context.Context, tx storage.Tx) error {
			var val string
			err := tx.QueryRow(ctx, `SELECT val FROM foo WHERE id = ?`, "id_panic").Scan(&val)
			if err == nil {
				t.Errorf("id_panic should be absent after panic rollback, got val=%q", val)
			}
			return nil
		})
		if err != nil {
			t.Errorf("post-panic verify Tx: %v", err)
		}
	}()

	_ = s.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO foo (id, val) VALUES (?, ?)`, "id_panic", "should-rollback"); err != nil {
			t.Fatalf("insert in panicking Tx: %v", err)
		}
		panic("boom")
	})
}
