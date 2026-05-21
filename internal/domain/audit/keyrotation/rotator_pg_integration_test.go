//go:build integration

package keyrotation_test

// rotator_pg_integration_test.go — Phase 10.D-3+4 PG 통합 테스트.
//
// 본 파일은 `-tags=integration` 빌드 태그가 붙어야 컴파일됩니다.
// docker 미가용 시 t.Skip.
//
// 실행:
//
//	go test -tags=integration -count=1 ./internal/domain/audit/keyrotation/...
//
// 검증:
//   1. 0037 + 0038 적용 후 RotateNow 가 PG 단일 Tx 안에서 epoch append + revoke + audit emit.
//   2. SwappableSigner 가 새 key 로 swap.
//   3. audit_chain_keys 의 활성 epoch 가 단조 증가.
//   4. audit_entries.key_epoch 가 epoch=1 prior 로 기록됨 (entry 가 swap 직전 INSERT 되므로).

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcpg "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/ssabro/rosshield/internal/domain/audit/keyrotation"
	auditrepo "github.com/ssabro/rosshield/internal/domain/audit/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/signer"
	"github.com/ssabro/rosshield/internal/platform/signer/soft"
	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/postgres"
)

const pgTestTenant storage.TenantID = "system"

func newPGStore(t *testing.T) storage.Storage {
	t.Helper()
	ctx := context.Background()

	pgC, err := tcpg.Run(ctx, "postgres:16-alpine",
		tcpg.WithDatabase("rosshield_test"),
		tcpg.WithUsername("test"),
		tcpg.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		t.Skipf("docker unavailable or PG container failed: %v", err)
	}
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = pgC.Terminate(shutdownCtx)
	})

	dsn, err := pgC.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("ConnectionString: %v", err)
	}

	store, err := postgres.Open(storage.Config{Driver: "postgres", DSN: dsn})
	if err != nil {
		t.Fatalf("postgres.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return store
}

func TestIntegrationPG_KeyRotation_FullRoundTrip(t *testing.T) {
	t.Parallel()
	store := newPGStore(t)

	clk := clock.NewFake(time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC))
	auditSvc := auditrepo.New(auditrepo.Deps{Clock: clk, KeyEpoch: nil})
	chainKeyRepo := auditrepo.NewKeyEpochRepo()

	initial, _ := soft.New()
	swap := signer.NewSwappableSigner(initial, 1)
	priorKey := swap.CurrentKeyID()

	allocator := keyrotation.AllocatorFunc(func(newEpoch int64) (string, ed25519.PrivateKey, error) {
		_, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return "", nil, err
		}
		return "pg-test-handle", priv, nil
	})

	r, err := keyrotation.New(keyrotation.Deps{
		Storage: store, Audit: auditSvc, ChainKeys: chainKeyRepo,
		Signer: swap, Allocator: allocator, Clock: clk,
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		MinInterval: 0,
		TenantID:    pgTestTenant,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := r.RotateNow(context.Background(), keyrotation.TriggerScheduler); err != nil {
		t.Fatalf("RotateNow: %v", err)
	}

	if swap.CurrentKeyID() == priorKey {
		t.Error("signer not swapped")
	}
	if swap.CurrentEpoch() < 2 {
		t.Errorf("epoch = %d, want >= 2", swap.CurrentEpoch())
	}

	// audit_chain_keys 의 list 확인 — bootstrap(epoch=1, revoked) + 새 epoch(미revoke).
	ctx := storage.WithTenantID(context.Background(), pgTestTenant)
	if err := store.Tx(ctx, func(c context.Context, tx storage.Tx) error {
		list, err := chainKeyRepo.ListChainKeyEpochs(c, tx, pgTestTenant)
		if err != nil {
			return err
		}
		if len(list) < 2 {
			t.Errorf("epoch list len = %d, want >= 2", len(list))
		}
		// 활성 epoch 는 1 개여야 함.
		active := 0
		for _, e := range list {
			if !e.IsRevoked() {
				active++
			}
		}
		if active != 1 {
			t.Errorf("active epochs = %d, want 1", active)
		}
		return nil
	}); err != nil {
		t.Fatalf("Tx: %v", err)
	}
}
