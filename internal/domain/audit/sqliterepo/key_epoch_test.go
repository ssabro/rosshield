package sqliterepo_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/domain/audit"
	"github.com/ssabro/rosshield/internal/domain/audit/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/sqlite"
)

const keyEpochTestTenant storage.TenantID = "system"

// newKeyEpochFixture는 0037 마이그레이션이 적용된 fresh SQLite store + repo 를 반환합니다.
func newKeyEpochFixture(t *testing.T) (*sqliterepo.KeyEpochRepo, storage.Storage) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "audit_chain_keys.db")
	store, err := sqlite.Open(storage.Config{Driver: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return sqliterepo.NewKeyEpochRepo(), store
}

// runBootstrapTx 는 tenant 없이 Bootstrap Tx 안에서 fn 실행. audit_chain_keys 는 system tenant 전제이나
// SQLite 어댑터의 Tx 는 tenant 강제이므로 system tenant 컨텍스트로 실행.
func runWithTenantTx(t *testing.T, store storage.Storage, tenantID storage.TenantID, fn func(ctx context.Context, tx storage.Tx) error) {
	t.Helper()
	ctx := storage.WithTenantID(context.Background(), tenantID)
	if err := store.Tx(ctx, fn); err != nil {
		t.Fatalf("Tx: %v", err)
	}
}

// TestKeyEpoch_BootstrapRowExists — 0037 마이그레이션 적용 시 epoch=1 bootstrap row 자동 insert.
func TestKeyEpoch_BootstrapRowExists(t *testing.T) {
	t.Parallel()
	repo, store := newKeyEpochFixture(t)

	runWithTenantTx(t, store, keyEpochTestTenant, func(ctx context.Context, tx storage.Tx) error {
		list, err := repo.ListChainKeyEpochs(ctx, tx, keyEpochTestTenant)
		if err != nil {
			t.Fatalf("ListChainKeyEpochs: %v", err)
		}
		if len(list) != 1 {
			t.Fatalf("epoch list len = %d, want 1", len(list))
		}
		if list[0].Epoch != 1 {
			t.Errorf("bootstrap epoch = %d, want 1", list[0].Epoch)
		}
		if list[0].KeyID != "__bootstrap__" || list[0].PublicKeyHex != "__bootstrap__" {
			t.Errorf("bootstrap placeholder mismatch: keyID=%q pub=%q", list[0].KeyID, list[0].PublicKeyHex)
		}
		if list[0].CreatedBy != "migration:0037" {
			t.Errorf("CreatedBy = %q, want migration:0037", list[0].CreatedBy)
		}
		if list[0].IsRevoked() {
			t.Error("bootstrap row should not be revoked")
		}
		return nil
	})
}

// TestKeyEpoch_CurrentReturnsActive — Current 는 revoked_at IS NULL 의 가장 최근 epoch 반환.
func TestKeyEpoch_CurrentReturnsActive(t *testing.T) {
	t.Parallel()
	repo, store := newKeyEpochFixture(t)

	runWithTenantTx(t, store, keyEpochTestTenant, func(ctx context.Context, tx storage.Tx) error {
		cur, err := repo.CurrentChainKeyEpoch(ctx, tx, keyEpochTestTenant)
		if err != nil {
			t.Fatalf("CurrentChainKeyEpoch: %v", err)
		}
		if cur.Epoch != 1 {
			t.Errorf("current epoch = %d, want 1 (bootstrap)", cur.Epoch)
		}
		return nil
	})
}

// TestKeyEpoch_AppendAndCurrent — append 시 새 epoch 자동 할당 + Current 가 새 epoch 반환.
func TestKeyEpoch_AppendAndCurrent(t *testing.T) {
	t.Parallel()
	repo, store := newKeyEpochFixture(t)

	var assigned int64
	runWithTenantTx(t, store, keyEpochTestTenant, func(ctx context.Context, tx storage.Tx) error {
		ep := audit.ChainKeyEpoch{
			TenantID:       keyEpochTestTenant,
			KeyID:          "key_abcdef01",
			PublicKeyHex:   strings.Repeat("ab", 32),
			KeystoreHandle: "audit-chain-2.key",
			CreatedAt:      time.Now().UTC(),
			CreatedBy:      "scheduler",
			AuditEntrySeq:  123,
		}
		a, err := repo.AppendChainKeyEpoch(ctx, tx, ep)
		if err != nil {
			return err
		}
		assigned = a
		return nil
	})
	if assigned <= 1 {
		t.Fatalf("assigned epoch = %d, want > 1", assigned)
	}

	runWithTenantTx(t, store, keyEpochTestTenant, func(ctx context.Context, tx storage.Tx) error {
		cur, err := repo.CurrentChainKeyEpoch(ctx, tx, keyEpochTestTenant)
		if err != nil {
			t.Fatalf("CurrentChainKeyEpoch: %v", err)
		}
		if cur.Epoch != assigned {
			t.Errorf("current epoch = %d, want %d", cur.Epoch, assigned)
		}
		if cur.KeyID != "key_abcdef01" {
			t.Errorf("current keyID = %q", cur.KeyID)
		}
		if cur.AuditEntrySeq != 123 {
			t.Errorf("AuditEntrySeq = %d, want 123", cur.AuditEntrySeq)
		}
		return nil
	})
}

// TestKeyEpoch_AppendRequiredFields — 필수 필드 누락 시 에러.
func TestKeyEpoch_AppendRequiredFields(t *testing.T) {
	t.Parallel()
	repo, store := newKeyEpochFixture(t)

	cases := []struct {
		name string
		ep   audit.ChainKeyEpoch
	}{
		{"missing TenantID", audit.ChainKeyEpoch{KeyID: "k", PublicKeyHex: "p", KeystoreHandle: "h", CreatedBy: "x"}},
		{"missing KeyID", audit.ChainKeyEpoch{TenantID: keyEpochTestTenant, PublicKeyHex: "p", KeystoreHandle: "h", CreatedBy: "x"}},
		{"missing PublicKeyHex", audit.ChainKeyEpoch{TenantID: keyEpochTestTenant, KeyID: "k", KeystoreHandle: "h", CreatedBy: "x"}},
		{"missing KeystoreHandle", audit.ChainKeyEpoch{TenantID: keyEpochTestTenant, KeyID: "k", PublicKeyHex: "p", CreatedBy: "x"}},
		{"missing CreatedBy", audit.ChainKeyEpoch{TenantID: keyEpochTestTenant, KeyID: "k", PublicKeyHex: "p", KeystoreHandle: "h"}},
		{"negative epoch", audit.ChainKeyEpoch{Epoch: -1, TenantID: keyEpochTestTenant, KeyID: "k", PublicKeyHex: "p", KeystoreHandle: "h", CreatedBy: "x"}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			ctx := storage.WithTenantID(context.Background(), keyEpochTestTenant)
			err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
				_, e := repo.AppendChainKeyEpoch(ctx, tx, tc.ep)
				return e
			})
			if err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

// TestKeyEpoch_Revoke — revoke 후 Current 는 이전 epoch 반환 (bootstrap 으로 fallback).
func TestKeyEpoch_Revoke(t *testing.T) {
	t.Parallel()
	repo, store := newKeyEpochFixture(t)

	var newEpoch int64
	runWithTenantTx(t, store, keyEpochTestTenant, func(ctx context.Context, tx storage.Tx) error {
		ep := audit.ChainKeyEpoch{
			TenantID:       keyEpochTestTenant,
			KeyID:          "key_to_revoke",
			PublicKeyHex:   strings.Repeat("cd", 32),
			KeystoreHandle: "audit-chain-2.key",
			CreatedAt:      time.Now().UTC(),
			CreatedBy:      "scheduler",
			AuditEntrySeq:  42,
		}
		a, err := repo.AppendChainKeyEpoch(ctx, tx, ep)
		if err != nil {
			return err
		}
		newEpoch = a
		return nil
	})

	revokedAt := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	runWithTenantTx(t, store, keyEpochTestTenant, func(ctx context.Context, tx storage.Tx) error {
		return repo.RevokeChainKeyEpoch(ctx, tx, keyEpochTestTenant, newEpoch, revokedAt)
	})

	// Current 는 bootstrap epoch=1 으로 fallback (newEpoch 가 revoke 됨).
	runWithTenantTx(t, store, keyEpochTestTenant, func(ctx context.Context, tx storage.Tx) error {
		cur, err := repo.CurrentChainKeyEpoch(ctx, tx, keyEpochTestTenant)
		if err != nil {
			t.Fatalf("CurrentChainKeyEpoch: %v", err)
		}
		if cur.Epoch != 1 {
			t.Errorf("current after revoke = %d, want 1 (bootstrap)", cur.Epoch)
		}
		// revoked epoch 의 RevokedAt 확인.
		list, err := repo.ListChainKeyEpochs(ctx, tx, keyEpochTestTenant)
		if err != nil {
			return err
		}
		var revoked *audit.ChainKeyEpoch
		for i := range list {
			if list[i].Epoch == newEpoch {
				revoked = &list[i]
				break
			}
		}
		if revoked == nil {
			t.Fatalf("revoked epoch %d not in list", newEpoch)
		}
		if !revoked.IsRevoked() {
			t.Error("revoked epoch IsRevoked = false")
		}
		if !revoked.RevokedAt.Equal(revokedAt) {
			t.Errorf("RevokedAt = %v, want %v", revoked.RevokedAt, revokedAt)
		}
		return nil
	})
}

// TestKeyEpoch_RevokeAlreadyRevoked — 이미 revoke 된 epoch 재 revoke 시 ErrChainKeyAlreadyRevoked.
func TestKeyEpoch_RevokeAlreadyRevoked(t *testing.T) {
	t.Parallel()
	repo, store := newKeyEpochFixture(t)

	var ep1 int64
	runWithTenantTx(t, store, keyEpochTestTenant, func(ctx context.Context, tx storage.Tx) error {
		a, err := repo.AppendChainKeyEpoch(ctx, tx, audit.ChainKeyEpoch{
			TenantID:       keyEpochTestTenant,
			KeyID:          "key_double_revoke",
			PublicKeyHex:   strings.Repeat("ef", 32),
			KeystoreHandle: "h",
			CreatedAt:      time.Now().UTC(),
			CreatedBy:      "cli",
		})
		ep1 = a
		return err
	})

	runWithTenantTx(t, store, keyEpochTestTenant, func(ctx context.Context, tx storage.Tx) error {
		return repo.RevokeChainKeyEpoch(ctx, tx, keyEpochTestTenant, ep1, time.Now().UTC())
	})

	ctx := storage.WithTenantID(context.Background(), keyEpochTestTenant)
	err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		return repo.RevokeChainKeyEpoch(ctx, tx, keyEpochTestTenant, ep1, time.Now().UTC())
	})
	if !errors.Is(err, audit.ErrChainKeyAlreadyRevoked) {
		t.Fatalf("expected ErrChainKeyAlreadyRevoked, got %v", err)
	}
}

// TestKeyEpoch_RevokeNotFound — 존재하지 않는 epoch revoke 시 storage.ErrNotFound.
func TestKeyEpoch_RevokeNotFound(t *testing.T) {
	t.Parallel()
	repo, store := newKeyEpochFixture(t)

	ctx := storage.WithTenantID(context.Background(), keyEpochTestTenant)
	err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		return repo.RevokeChainKeyEpoch(ctx, tx, keyEpochTestTenant, 9999, time.Now().UTC())
	})
	if !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TestKeyEpoch_CurrentNotFound — 활성 epoch 가 없으면 storage.ErrNotFound.
func TestKeyEpoch_CurrentNotFound(t *testing.T) {
	t.Parallel()
	repo, store := newKeyEpochFixture(t)

	// bootstrap epoch=1 revoke → 활성 0.
	runWithTenantTx(t, store, keyEpochTestTenant, func(ctx context.Context, tx storage.Tx) error {
		return repo.RevokeChainKeyEpoch(ctx, tx, keyEpochTestTenant, 1, time.Now().UTC())
	})

	ctx := storage.WithTenantID(context.Background(), keyEpochTestTenant)
	err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, e := repo.CurrentChainKeyEpoch(ctx, tx, keyEpochTestTenant)
		return e
	})
	if !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TestKeyEpoch_ImmutabilityTriggers — revoked_at 외 column 변경 시도는 트리거가 차단.
func TestKeyEpoch_ImmutabilityTriggers(t *testing.T) {
	t.Parallel()
	_, store := newKeyEpochFixture(t)

	// key_id 변경 시도 → 트리거가 차단.
	ctx := storage.WithTenantID(context.Background(), keyEpochTestTenant)
	err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE audit_chain_keys SET key_id = ? WHERE tenant_id = ? AND epoch = 1`,
			"tampered_key", string(keyEpochTestTenant))
		return e
	})
	if err == nil {
		t.Fatal("expected trigger to block UPDATE, got nil")
	}
	if !strings.Contains(err.Error(), "immutable") {
		t.Errorf("expected immutable error, got: %v", err)
	}

	// DELETE 시도 → 트리거가 차단.
	err = store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, e := tx.Exec(ctx, `DELETE FROM audit_chain_keys WHERE tenant_id = ? AND epoch = 1`,
			string(keyEpochTestTenant))
		return e
	})
	if err == nil {
		t.Fatal("expected trigger to block DELETE, got nil")
	}
}
