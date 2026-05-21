package keyrotationjob_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/domain/audit/keyrotation"
	auditrepo "github.com/ssabro/rosshield/internal/domain/audit/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/scheduler"
	"github.com/ssabro/rosshield/internal/platform/scheduler/cronsched"
	"github.com/ssabro/rosshield/internal/platform/scheduler/keyrotationjob"
	"github.com/ssabro/rosshield/internal/platform/signer"
	"github.com/ssabro/rosshield/internal/platform/signer/soft"
	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/sqlite"
)

func newTestRotator(t *testing.T) *keyrotation.KeyRotator {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "krj.db")
	store, err := sqlite.Open(storage.Config{Driver: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	clk := clock.NewFake(time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC))
	auditSvc := auditrepo.New(auditrepo.Deps{Clock: clk})
	chainKeyRepo := auditrepo.NewKeyEpochRepo()
	initial, _ := soft.New()
	swap := signer.NewSwappableSigner(initial, 1)
	allocator := keyrotation.AllocatorFunc(func(newEpoch int64) (string, ed25519.PrivateKey, error) {
		_, priv, _ := ed25519.GenerateKey(rand.Reader)
		return "h", priv, nil
	})
	r, err := keyrotation.New(keyrotation.Deps{
		Storage: store, Audit: auditSvc, ChainKeys: chainKeyRepo,
		Signer: swap, Allocator: allocator, Clock: clk,
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		MinInterval: 0,
		TenantID:    storage.TenantID("system"),
	})
	if err != nil {
		t.Fatalf("keyrotation.New: %v", err)
	}
	return r
}

func newTestScheduler(t *testing.T) scheduler.Scheduler {
	t.Helper()
	sch := cronsched.New(cronsched.Deps{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = sch.Close(ctx)
	})
	return sch
}

func TestRegister_EmptySpecNoOp(t *testing.T) {
	t.Parallel()
	sch := newTestScheduler(t)
	r := newTestRotator(t)
	if err := keyrotationjob.Register(sch, r, slog.New(slog.NewTextHandler(io.Discard, nil)),
		keyrotationjob.DefaultJobID, ""); err != nil {
		t.Errorf("empty spec should be no-op, got err=%v", err)
	}
}

func TestRegister_RotatorRequired(t *testing.T) {
	t.Parallel()
	sch := newTestScheduler(t)
	err := keyrotationjob.Register(sch, nil, slog.New(slog.NewTextHandler(io.Discard, nil)),
		keyrotationjob.DefaultJobID, "@every 1h")
	if err == nil {
		t.Error("expected error for nil rotator + non-empty spec")
	}
}

func TestRegister_SchedulerRequired(t *testing.T) {
	t.Parallel()
	r := newTestRotator(t)
	err := keyrotationjob.Register(nil, r, slog.New(slog.NewTextHandler(io.Discard, nil)),
		keyrotationjob.DefaultJobID, "@every 1h")
	if err == nil {
		t.Error("expected error for nil scheduler")
	}
}

func TestRegister_HappyPath(t *testing.T) {
	t.Parallel()
	sch := newTestScheduler(t)
	r := newTestRotator(t)
	if err := keyrotationjob.Register(sch, r, slog.New(slog.NewTextHandler(io.Discard, nil)),
		keyrotationjob.DefaultJobID, "@every 1h"); err != nil {
		t.Errorf("Register: %v", err)
	}
}
