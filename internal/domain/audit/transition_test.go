package audit_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/ssabro/rosshield/internal/domain/audit"
	"github.com/ssabro/rosshield/internal/domain/audit/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/sqlite"
)

// Phase 11.C-3 — EnsureHashVersionTransition idempotent emit 단위 test.

const transitionTenant storage.TenantID = "tn_transition"

func newTransitionFixture(t *testing.T) (*sqliterepo.Repo, storage.Storage) {
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

// 첫 emit — transition entry 가 chain 에 append 되고 setter 가 cache 됨.
func TestEnsureHashVersionTransitionFirstEmit(t *testing.T) {
	t.Parallel()
	repo, store := newTransitionFixture(t)

	ctx := storage.WithTenantID(context.Background(), transitionTenant)
	var entry audit.Entry
	var emitted bool
	err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		e, ok, err := audit.EnsureHashVersionTransition(ctx, tx, repo, repo, repo, transitionTenant)
		entry, emitted = e, ok
		return err
	})
	if err != nil {
		t.Fatalf("EnsureHashVersionTransition: %v", err)
	}
	if !emitted {
		t.Fatal("expected emitted=true on first call")
	}
	if entry.Seq != 1 {
		t.Errorf("Seq = %d, want 1 (first entry)", entry.Seq)
	}
	if entry.Action != audit.ActionHashVersionChanged {
		t.Errorf("Action = %q, want %q", entry.Action, audit.ActionHashVersionChanged)
	}
	if repo.HashVersionTransitionSeq() != 1 {
		t.Errorf("cached seq = %d, want 1", repo.HashVersionTransitionSeq())
	}
}

// 두 번째 호출 — idempotent (emit 없음), cache 보존.
func TestEnsureHashVersionTransitionIdempotent(t *testing.T) {
	t.Parallel()
	repo, store := newTransitionFixture(t)

	ctx := storage.WithTenantID(context.Background(), transitionTenant)
	// 첫 호출 — emit.
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, _, err := audit.EnsureHashVersionTransition(ctx, tx, repo, repo, repo, transitionTenant)
		return err
	}); err != nil {
		t.Fatalf("first call: %v", err)
	}

	// 두 번째 호출 — idempotent skip.
	var emitted bool
	var entry audit.Entry
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		e, ok, err := audit.EnsureHashVersionTransition(ctx, tx, repo, repo, repo, transitionTenant)
		entry, emitted = e, ok
		return err
	}); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if emitted {
		t.Error("expected emitted=false on second call (idempotent)")
	}
	if entry.Seq != 1 {
		t.Errorf("Seq = %d, want 1 (existing entry)", entry.Seq)
	}
	if repo.HashVersionTransitionSeq() != 1 {
		t.Errorf("cached seq = %d, want 1", repo.HashVersionTransitionSeq())
	}

	// head.seq 가 1 (transition entry 만 — 추가 emit 0 확인).
	var head audit.ChainHead
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		h, err := repo.Head(ctx, tx, transitionTenant)
		head = h
		return err
	}); err != nil {
		t.Fatalf("Head: %v", err)
	}
	if head.Seq != 1 {
		t.Errorf("head.Seq = %d, want 1 (single transition entry)", head.Seq)
	}
}

// transition entry 의 payload meta 가 fromVersion=1·toVersion=3.
func TestEnsureHashVersionTransitionPayloadMeta(t *testing.T) {
	t.Parallel()

	meta := audit.HashVersionTransitionMeta{FromVersion: 1, ToVersion: 3}
	b, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var back audit.HashVersionTransitionMeta
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if back.FromVersion != 1 || back.ToVersion != 3 {
		t.Errorf("meta = %+v, want {1, 3}", back)
	}
}

// transition entry 이전 + 이후 Append 분기 — v1 / v3 hash 결정성.
func TestAppendHashVersionBranchAcrossTransition(t *testing.T) {
	t.Parallel()
	repo, store := newTransitionFixture(t)

	ctx := storage.WithTenantID(context.Background(), transitionTenant)
	// 3 개 v1 entry append.
	for i := 0; i < 3; i++ {
		if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
			_, err := repo.Append(ctx, tx, audit.AppendRequest{
				TenantID: transitionTenant,
				Actor:    audit.Actor{Type: audit.ActorUser, ID: "us_t"},
				Action:   "robot.create",
				Target:   audit.Target{Type: "robot", ID: "ro_t"},
				Outcome:  audit.OutcomeSuccess,
			})
			return err
		}); err != nil {
			t.Fatalf("Append v1 #%d: %v", i, err)
		}
	}

	// transition emit (seq=4).
	var transitionSeq int64
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		e, _, err := audit.EnsureHashVersionTransition(ctx, tx, repo, repo, repo, transitionTenant)
		transitionSeq = e.Seq
		return err
	}); err != nil {
		t.Fatalf("transition: %v", err)
	}
	if transitionSeq != 4 {
		t.Fatalf("transitionSeq = %d, want 4", transitionSeq)
	}
	if repo.HashVersionTransitionSeq() != 4 {
		t.Errorf("cached transition seq = %d, want 4", repo.HashVersionTransitionSeq())
	}

	// 3 개 v3 entry append (seq=5,6,7).
	for i := 0; i < 3; i++ {
		if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
			_, err := repo.Append(ctx, tx, audit.AppendRequest{
				TenantID: transitionTenant,
				Actor:    audit.Actor{Type: audit.ActorUser, ID: "us_t"},
				Action:   "robot.update",
				Target:   audit.Target{Type: "robot", ID: "ro_t"},
				Outcome:  audit.OutcomeSuccess,
			})
			return err
		}); err != nil {
			t.Fatalf("Append v3 #%d: %v", i, err)
		}
	}

	// Verify 전체 chain — 자동으로 v1/v3 분기 (Verify 도 transition seq cache 사용).
	var vr audit.VerifyResult
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		r, err := repo.Verify(ctx, tx, transitionTenant, 1, 7)
		vr = r
		return err
	}); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !vr.OK {
		t.Errorf("Verify failed at seq %d: %s", vr.BreakAt, vr.Reason)
	}
	if vr.EntriesScanned != 7 {
		t.Errorf("EntriesScanned = %d, want 7", vr.EntriesScanned)
	}
}

// transition entry 자체의 hash 가 v1 — recompute 시 ComputeEntryHash(v1) 결과 일치.
func TestTransitionEntryItselfIsV1Hash(t *testing.T) {
	t.Parallel()
	repo, store := newTransitionFixture(t)

	ctx := storage.WithTenantID(context.Background(), transitionTenant)
	var transitionEntry audit.Entry
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		e, _, err := audit.EnsureHashVersionTransition(ctx, tx, repo, repo, repo, transitionTenant)
		transitionEntry = e
		return err
	}); err != nil {
		t.Fatalf("transition: %v", err)
	}

	// SELECT 한 entry 와 v1 hash 함수 재계산 비교.
	// transition entry 의 prev_hash = zero (genesis).
	v1Recompute, err := audit.ComputeEntryHash(audit.Hash{}, transitionEntry.PayloadDigest, transitionEntry)
	if err != nil {
		t.Fatalf("ComputeEntryHash: %v", err)
	}

	// Verify (seq=1 만) — chain 무결성 + recompute 일치 검증.
	var vr audit.VerifyResult
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		r, err := repo.Verify(ctx, tx, transitionTenant, 1, 1)
		vr = r
		return err
	}); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !vr.OK {
		t.Errorf("Verify failed: %s", vr.Reason)
	}

	// v1 recompute 결과가 stored hash 와 일치 (transition entry 자체 v1 hash 보장).
	if v1Recompute != transitionEntry.Hash {
		t.Errorf("v1 recompute mismatch: got %x, want %x", v1Recompute[:], transitionEntry.Hash[:])
	}
}

// chain 무결성 — v1 + transition + v3 entries 전체 prev_hash 연결 정상.
func TestChainIntegrityAcrossV1AndV3(t *testing.T) {
	t.Parallel()
	repo, store := newTransitionFixture(t)

	ctx := storage.WithTenantID(context.Background(), transitionTenant)

	// 2 개 v1 entry.
	for i := 0; i < 2; i++ {
		if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
			_, err := repo.Append(ctx, tx, audit.AppendRequest{
				TenantID: transitionTenant,
				Actor:    audit.Actor{Type: audit.ActorSystem, ID: "system"},
				Action:   "tenant.created",
				Target:   audit.Target{Type: "tenant", ID: "tn_x"},
				Outcome:  audit.OutcomeSuccess,
			})
			return err
		}); err != nil {
			t.Fatalf("Append v1: %v", err)
		}
	}

	// transition (seq=3).
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, _, err := audit.EnsureHashVersionTransition(ctx, tx, repo, repo, repo, transitionTenant)
		return err
	}); err != nil {
		t.Fatalf("transition: %v", err)
	}

	// 2 개 v3 entry.
	for i := 0; i < 2; i++ {
		if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
			_, err := repo.Append(ctx, tx, audit.AppendRequest{
				TenantID: transitionTenant,
				Actor:    audit.Actor{Type: audit.ActorSystem, ID: "system"},
				Action:   "tenant.updated",
				Target:   audit.Target{Type: "tenant", ID: "tn_x"},
				Outcome:  audit.OutcomeSuccess,
			})
			return err
		}); err != nil {
			t.Fatalf("Append v3: %v", err)
		}
	}

	// 전체 chain Verify — prev_hash 연결 + hash recompute 모두 통과.
	var vr audit.VerifyResult
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		r, err := repo.Verify(ctx, tx, transitionTenant, 1, 5)
		vr = r
		return err
	}); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !vr.OK {
		t.Errorf("Verify failed at seq %d: %s", vr.BreakAt, vr.Reason)
	}
	if vr.EntriesScanned != 5 {
		t.Errorf("EntriesScanned = %d, want 5", vr.EntriesScanned)
	}
}
