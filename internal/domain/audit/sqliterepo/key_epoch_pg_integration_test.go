//go:build integration

package sqliterepo_test

// key_epoch_pg_integration_test.go — Phase 10.D-2 PG 통합 테스트.
//
// 본 파일은 `-tags=integration` 빌드 태그가 붙어야 컴파일됩니다.
// docker 미가용 시 t.Skip.
//
// 실행:
//
//	go test -tags=integration -count=1 ./internal/domain/audit/sqliterepo/...
//
// 검증:
//   1. 0037 적용 후 audit_chain_keys 테이블 + epoch=1 bootstrap row 존재.
//   2. 4 메서드 (List/Current/Append/Revoke) happy path PASS.
//   3. PG 트리거 — revoked_at 외 column 변경 시도 → RAISE EXCEPTION.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcpg "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/ssabro/rosshield/internal/domain/audit"
	"github.com/ssabro/rosshield/internal/domain/audit/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/postgres"
)

const pgKeyEpochTenant storage.TenantID = "system"

func newPGKeyEpochFixture(t *testing.T) storage.Storage {
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

// TestIntegrationPG_KeyEpoch_BootstrapRow — 0037 적용 후 epoch=1 bootstrap row 존재.
func TestIntegrationPG_KeyEpoch_BootstrapRow(t *testing.T) {
	t.Parallel()
	store := newPGKeyEpochFixture(t)
	repo := sqliterepo.NewKeyEpochRepo()

	ctx := storage.WithTenantID(context.Background(), pgKeyEpochTenant)
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		list, err := repo.ListChainKeyEpochs(ctx, tx, pgKeyEpochTenant)
		if err != nil {
			return err
		}
		if len(list) != 1 || list[0].Epoch != 1 || list[0].CreatedBy != "migration:0037" {
			t.Errorf("bootstrap row not as expected: %+v", list)
		}
		return nil
	}); err != nil {
		t.Fatalf("Tx: %v", err)
	}
}

// TestIntegrationPG_KeyEpoch_AppendCurrentRevoke — 4 메서드 happy path.
func TestIntegrationPG_KeyEpoch_AppendCurrentRevoke(t *testing.T) {
	t.Parallel()
	store := newPGKeyEpochFixture(t)
	repo := sqliterepo.NewKeyEpochRepo()
	ctx := storage.WithTenantID(context.Background(), pgKeyEpochTenant)

	var newEpoch int64
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		a, err := repo.AppendChainKeyEpoch(ctx, tx, audit.ChainKeyEpoch{
			TenantID:       pgKeyEpochTenant,
			KeyID:          "key_pg_test",
			PublicKeyHex:   strings.Repeat("ab", 32),
			KeystoreHandle: "audit-chain-2.key",
			CreatedAt:      time.Now().UTC(),
			CreatedBy:      "scheduler",
			AuditEntrySeq:  17,
		})
		newEpoch = a
		return err
	}); err != nil {
		t.Fatalf("Append Tx: %v", err)
	}
	if newEpoch <= 1 {
		t.Fatalf("assigned epoch = %d, want > 1", newEpoch)
	}

	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		cur, err := repo.CurrentChainKeyEpoch(ctx, tx, pgKeyEpochTenant)
		if err != nil {
			return err
		}
		if cur.Epoch != newEpoch {
			t.Errorf("current epoch = %d, want %d", cur.Epoch, newEpoch)
		}
		return nil
	}); err != nil {
		t.Fatalf("Current Tx: %v", err)
	}

	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		return repo.RevokeChainKeyEpoch(ctx, tx, pgKeyEpochTenant, newEpoch, time.Now().UTC())
	}); err != nil {
		t.Fatalf("Revoke Tx: %v", err)
	}

	// revoke 후 Current 는 bootstrap epoch=1.
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		cur, err := repo.CurrentChainKeyEpoch(ctx, tx, pgKeyEpochTenant)
		if err != nil {
			return err
		}
		if cur.Epoch != 1 {
			t.Errorf("current after revoke = %d, want 1", cur.Epoch)
		}
		return nil
	}); err != nil {
		t.Fatalf("Current after revoke Tx: %v", err)
	}
}

// TestIntegrationPG_KeyEpoch_ImmutabilityTrigger — revoked_at 외 column UPDATE → RAISE EXCEPTION.
func TestIntegrationPG_KeyEpoch_ImmutabilityTrigger(t *testing.T) {
	t.Parallel()
	store := newPGKeyEpochFixture(t)
	ctx := storage.WithTenantID(context.Background(), pgKeyEpochTenant)

	// key_id 변경 시도 → 트리거 차단.
	err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE audit_chain_keys SET key_id = $1 WHERE tenant_id = $2 AND epoch = 1`,
			"tampered_key", string(pgKeyEpochTenant))
		return e
	})
	if err == nil {
		t.Fatal("expected trigger to block UPDATE")
	}
	if !strings.Contains(err.Error(), "immutable") {
		t.Errorf("expected immutable error, got: %v", err)
	}

	// DELETE 시도 → 트리거 차단.
	err = store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, e := tx.Exec(ctx, `DELETE FROM audit_chain_keys WHERE tenant_id = $1 AND epoch = 1`,
			string(pgKeyEpochTenant))
		return e
	})
	if err == nil {
		t.Fatal("expected trigger to block DELETE")
	}
}
