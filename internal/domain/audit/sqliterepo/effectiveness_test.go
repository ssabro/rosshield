package sqliterepo_test

// effectiveness_test.go — Phase 11.B-6 CountActionsByWindows 단위 테스트.

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/domain/audit"
	"github.com/ssabro/rosshield/internal/domain/audit/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/sqlite"
)

// newTestRepoWithClock 는 newTestRepo 의 fake-clock 버전입니다.
// audit_entries.occurred_at 을 검증 가능한 값으로 시드하기 위해 별도 헬퍼.
func newTestRepoWithClock(t *testing.T, clk clock.Clock) (*sqliterepo.Repo, storage.Storage) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "audit-eff.db")
	store, err := sqlite.Open(storage.Config{Driver: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return sqliterepo.New(sqliterepo.Deps{Clock: clk}), store
}

// mustAppend 는 ctx 안에서 단일 Append 를 실행하고 에러 시 t.Fatalf.
func mustAppend(t *testing.T, store storage.Storage, repo *sqliterepo.Repo, ctx context.Context, req audit.AppendRequest) {
	t.Helper()
	if err := store.Tx(ctx, func(c context.Context, tx storage.Tx) error {
		_, err := repo.Append(c, tx, req)
		return err
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}
}

func TestCountActionsByWindows_EmptyActions(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)
	ctx := storage.WithTenantID(context.Background(), testTenant)
	var out []audit.ActionCountWindow
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		var err error
		out, err = repo.CountActionsByWindows(ctx, tx, testTenant, nil, time.Now())
		return err
	}); err != nil {
		t.Fatalf("Tx: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("len = %d, want 0", len(out))
	}
}

func TestCountActionsByWindows_NoEntries_ReturnsZeros(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)
	actions := []string{"audit.chain.key_rotated", "user_role.synced"}
	ctx := storage.WithTenantID(context.Background(), testTenant)
	var out []audit.ActionCountWindow
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		var err error
		out, err = repo.CountActionsByWindows(ctx, tx, testTenant, actions, time.Now())
		return err
	}); err != nil {
		t.Fatalf("Tx: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("len = %d, want 2", len(out))
	}
	for i, r := range out {
		if r.Action != actions[i] {
			t.Errorf("[%d].Action = %q, want %q", i, r.Action, actions[i])
		}
		if r.LastDay != 0 || r.Last7Days != 0 || r.Last30Days != 0 {
			t.Errorf("[%d] expected zero counts, got %d/%d/%d", i, r.LastDay, r.Last7Days, r.Last30Days)
		}
	}
}

func TestCountActionsByWindows_AggregatesByActionAndTimeRange(t *testing.T) {
	t.Parallel()

	// now = 5/21 12:00. 5/15 00:00, 5/19 00:00, 5/21 11:00 에 시드.
	// → 1d cutoff = 5/20 12:00 → 5/21 만 카운트.
	// → 7d cutoff = 5/14 12:00 → 3 entries 모두 카운트.
	// → 30d cutoff = 4/21 12:00 → 3 entries 모두 카운트.
	now := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	clk := clock.NewFake(now)
	repo, store := newTestRepoWithClock(t, clk)

	ctx := storage.WithTenantID(context.Background(), testTenant)

	clk.Set(time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC))
	mustAppend(t, store, repo, ctx, audit.AppendRequest{
		TenantID: testTenant,
		Actor:    audit.Actor{Type: audit.ActorSystem, ID: "system"},
		Action:   "audit.chain.key_rotated",
		Target:   audit.Target{Type: "audit_chain", ID: string(testTenant)},
		Outcome:  audit.OutcomeSuccess,
	})

	clk.Set(time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC))
	mustAppend(t, store, repo, ctx, audit.AppendRequest{
		TenantID: testTenant,
		Actor:    audit.Actor{Type: audit.ActorUser, ID: "us_admin"},
		Action:   "user_role.synced",
		Target:   audit.Target{Type: "user_role", ID: "us_a"},
		Outcome:  audit.OutcomeSuccess,
	})

	clk.Set(time.Date(2026, 5, 21, 11, 0, 0, 0, time.UTC))
	mustAppend(t, store, repo, ctx, audit.AppendRequest{
		TenantID: testTenant,
		Actor:    audit.Actor{Type: audit.ActorUser, ID: "us_admin"},
		Action:   "user_role.synced",
		Target:   audit.Target{Type: "user_role", ID: "us_b"},
		Outcome:  audit.OutcomeSuccess,
	})

	actions := []string{"audit.chain.key_rotated", "user_role.synced", "scan.completed"}
	var out []audit.ActionCountWindow
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		var err error
		out, err = repo.CountActionsByWindows(ctx, tx, testTenant, actions, now)
		return err
	}); err != nil {
		t.Fatalf("Tx: %v", err)
	}

	if len(out) != 3 {
		t.Fatalf("len = %d, want 3", len(out))
	}

	// audit.chain.key_rotated: 1d 밖 (0), 7d 안 (1), 30d 안 (1).
	if got := out[0]; got.LastDay != 0 || got.Last7Days != 1 || got.Last30Days != 1 {
		t.Errorf("key_rotated counts = %d/%d/%d, want 0/1/1", got.LastDay, got.Last7Days, got.Last30Days)
	}
	// user_role.synced: 5/19 (1d 밖, 7d 안) + 5/21 11:00 (1d 안).
	// → 1d = 1 (5/21), 7d = 2 (5/19 + 5/21), 30d = 2.
	if got := out[1]; got.LastDay != 1 || got.Last7Days != 2 || got.Last30Days != 2 {
		t.Errorf("user_role.synced counts = %d/%d/%d, want 1/2/2", got.LastDay, got.Last7Days, got.Last30Days)
	}
	// scan.completed: 미등록 → 모두 0.
	if got := out[2]; got.LastDay != 0 || got.Last7Days != 0 || got.Last30Days != 0 {
		t.Errorf("scan.completed counts = %d/%d/%d, want 0/0/0", got.LastDay, got.Last7Days, got.Last30Days)
	}
}
