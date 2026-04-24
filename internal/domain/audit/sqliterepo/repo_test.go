package sqliterepo_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/domain/audit"
	"github.com/ssabro/rosshield/internal/domain/audit/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/sqlite"
)

const testTenant storage.TenantID = "tn_test"

func newTestRepo(t *testing.T) (*sqliterepo.Repo, storage.Storage) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "audit.db")
	store, err := sqlite.Open(storage.Config{Driver: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	repo := sqliterepo.New(sqliterepo.Deps{Clock: clock.System()})
	return repo, store
}

func sampleReq(action string) audit.AppendRequest {
	return audit.AppendRequest{
		TenantID: testTenant,
		Actor:    audit.Actor{Type: audit.ActorUser, ID: "us_test"},
		Action:   action,
		Target:   audit.Target{Type: "robot", ID: "ro_test"},
		Payload:  []byte(`{"name":"r1"}`),
		Outcome:  audit.OutcomeSuccess,
	}
}

func appendOne(t *testing.T, store storage.Storage, repo *sqliterepo.Repo, req audit.AppendRequest) audit.Entry {
	t.Helper()
	var out audit.Entry
	ctx := storage.WithTenantID(context.Background(), req.TenantID)
	err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		e, err := repo.Append(ctx, tx, req)
		if err != nil {
			return err
		}
		out = e
		return nil
	})
	if err != nil {
		t.Fatalf("Tx/Append: %v", err)
	}
	return out
}

// E2.T1
func TestAppendInitializesGenesis(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)

	entry := appendOne(t, store, repo, sampleReq("robot.create"))

	if entry.Seq != 1 {
		t.Errorf("Seq = %d, want 1", entry.Seq)
	}
	if !entry.PrevHash.IsZero() {
		t.Errorf("first entry PrevHash = %x, want zero", entry.PrevHash)
	}
	if entry.Hash.IsZero() {
		t.Error("entry Hash should not be zero")
	}
	if entry.OccurredAt.IsZero() {
		t.Error("OccurredAt should be set by Service")
	}
	if entry.PayloadDigest == (audit.Hash{}) {
		t.Error("PayloadDigest should be sha256 of payload, not zero")
	}
}

// E2.T2
func TestAppendChainsHashes(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)

	first := appendOne(t, store, repo, sampleReq("robot.create"))
	second := appendOne(t, store, repo, sampleReq("robot.update"))

	if second.Seq != 2 {
		t.Errorf("second.Seq = %d, want 2", second.Seq)
	}
	if second.PrevHash != first.Hash {
		t.Errorf("second.PrevHash = %x, want %x (= first.Hash)", second.PrevHash, first.Hash)
	}

	expected, err := audit.ComputeEntryHash(second.PrevHash, second.PayloadDigest, second)
	if err != nil {
		t.Fatalf("ComputeEntryHash: %v", err)
	}
	if expected != second.Hash {
		t.Errorf("second.Hash mismatch:\n got  %x\n want %x", second.Hash, expected)
	}

	// Head 갱신 검증.
	ctx := storage.WithTenantID(context.Background(), testTenant)
	var head audit.ChainHead
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		h, err := repo.Head(ctx, tx, testTenant)
		head = h
		return err
	}); err != nil {
		t.Fatalf("Head: %v", err)
	}
	if head.Seq != 2 || head.Hash != second.Hash {
		t.Errorf("head = (seq=%d, hash=%x), want (seq=2, hash=%x)", head.Seq, head.Hash, second.Hash)
	}
}

// E2.T3 — 직접 raw INSERT로 같은 (tenant_id, seq) 시도 → UNIQUE 위반.
// (정상 Append 경로는 head로 자동 단조 — 우회 시도가 막히는지 검증.)
func TestAppendRejectsDuplicateSeq(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)

	first := appendOne(t, store, repo, sampleReq("robot.create"))

	// 같은 seq로 raw INSERT → 실패해야 함.
	ctx := storage.WithTenantID(context.Background(), testTenant)
	err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, e := tx.Exec(ctx, `
INSERT INTO audit_entries (
    tenant_id, seq, occurred_at,
    actor_type, actor_id, actor_ip, actor_ua,
    action, target_type, target_id,
    payload_digest, outcome, error_code, error_message,
    prev_hash, hash
) VALUES (?, ?, ?, ?, ?, NULL, NULL, ?, ?, ?, ?, ?, NULL, NULL, ?, ?)`,
			string(testTenant), first.Seq, first.OccurredAt.Format(time.RFC3339Nano),
			"user", "us_dup",
			"robot.create", "robot", "ro_test",
			make([]byte, audit.HashSize), "success",
			make([]byte, audit.HashSize), make([]byte, audit.HashSize))
		return e
	})
	if err == nil {
		t.Fatal("expected UNIQUE violation for duplicate (tenant_id, seq)")
	}
}

// E2.T4
func TestAppendIsAppendOnly(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)

	first := appendOne(t, store, repo, sampleReq("robot.create"))
	ctx := storage.WithTenantID(context.Background(), testTenant)

	// UPDATE 시도 → trigger ABORT → ErrImmutable.
	err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE audit_entries SET action = 'tampered' WHERE tenant_id = ? AND seq = ?`,
			string(testTenant), first.Seq)
		return e
	})
	if !errors.Is(err, storage.ErrImmutable) {
		t.Errorf("UPDATE: err = %v, want ErrImmutable", err)
	}

	// DELETE 시도 → trigger ABORT → ErrImmutable.
	err = store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, e := tx.Exec(ctx, `DELETE FROM audit_entries WHERE tenant_id = ? AND seq = ?`,
			string(testTenant), first.Seq)
		return e
	})
	if !errors.Is(err, storage.ErrImmutable) {
		t.Errorf("DELETE: err = %v, want ErrImmutable", err)
	}
}

// 보조: Head 미존재 → genesis 반환.
func TestHeadEmptyReturnsGenesis(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)

	ctx := storage.WithTenantID(context.Background(), testTenant)
	var head audit.ChainHead
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		h, err := repo.Head(ctx, tx, testTenant)
		head = h
		return err
	}); err != nil {
		t.Fatalf("Head: %v", err)
	}
	if head.Seq != 0 {
		t.Errorf("genesis Seq = %d, want 0", head.Seq)
	}
	if !head.Hash.IsZero() {
		t.Errorf("genesis Hash = %x, want zero", head.Hash)
	}
	if head.TenantID != testTenant {
		t.Errorf("genesis TenantID = %q, want %q", head.TenantID, testTenant)
	}
}

// 보조: 검증 에러 (Action 비어있음).
func TestAppendValidationErrors(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)

	cases := []struct {
		name    string
		mutate  func(r *audit.AppendRequest)
		wantErr error
	}{
		{"empty action", func(r *audit.AppendRequest) { r.Action = "" }, audit.ErrEmptyAction},
		{"empty target type", func(r *audit.AppendRequest) { r.Target.Type = "" }, audit.ErrEmptyTarget},
		{"empty target id", func(r *audit.AppendRequest) { r.Target.ID = "" }, audit.ErrEmptyTarget},
		{"invalid actor", func(r *audit.AppendRequest) { r.Actor.Type = "alien" }, audit.ErrInvalidActor},
		{"invalid outcome", func(r *audit.AppendRequest) { r.Outcome = "weird" }, audit.ErrInvalidOutcome},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := sampleReq("robot.create")
			tc.mutate(&req)
			ctx := storage.WithTenantID(context.Background(), testTenant)
			err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
				_, e := repo.Append(ctx, tx, req)
				return e
			})
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// 보조: tx.TenantID()와 req.TenantID 불일치 → ErrTenantMismatch.
func TestAppendTenantMismatch(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)

	ctx := storage.WithTenantID(context.Background(), "tn_a")
	err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		req := sampleReq("robot.create")
		req.TenantID = "tn_b" // tx는 tn_a, req는 tn_b
		_, e := repo.Append(ctx, tx, req)
		return e
	})
	if !errors.Is(err, audit.ErrTenantMismatch) {
		t.Errorf("err = %v, want ErrTenantMismatch", err)
	}
}
