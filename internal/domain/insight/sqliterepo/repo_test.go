package sqliterepo_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/domain/insight"
	"github.com/ssabro/rosshield/internal/domain/insight/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/idgen"
	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/sqlite"
)

// fakeAuditEmitter는 audit emit 호출을 기록하는 mock입니다 (P5 격리).
type fakeAuditEmitter struct {
	mu              sync.Mutex
	createdCount    int
	dismissedCount  int
	lastDismissedID string
	lastReason      string
}

func (a *fakeAuditEmitter) EmitInsightCreated(_ context.Context, _ storage.Tx, _ insight.Insight) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.createdCount++
	return nil
}
func (a *fakeAuditEmitter) EmitInsightDismissed(_ context.Context, _ storage.Tx, in insight.Insight, reason string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.dismissedCount++
	a.lastDismissedID = in.ID
	a.lastReason = reason
	return nil
}

// fakeScanReader는 ScanReader 인터페이스를 충족하는 in-memory mock입니다.
//
// scan 도메인 import 회피 — 테스트 harness 수준에서 fake로 격리.
type fakeScanReader struct {
	sessionsByFleet  map[string][]insight.ScanSessionView
	resultsBySession map[string][]insight.ScanResultView
}

func (f *fakeScanReader) ListRecentSessions(_ context.Context, _ storage.Tx, fleetID string, limit int) ([]insight.ScanSessionView, error) {
	all := f.sessionsByFleet[fleetID]
	if len(all) <= limit {
		return all, nil
	}
	return all[:limit], nil
}
func (f *fakeScanReader) ListResultsForSession(_ context.Context, _ storage.Tx, sessionID string) ([]insight.ScanResultView, error) {
	return f.resultsBySession[sessionID], nil
}

func newTestRepo(t *testing.T, scan *fakeScanReader) (*sqliterepo.Repo, *fakeAuditEmitter, storage.Storage) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "insight.db")
	store, err := sqlite.Open(storage.Config{Driver: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	emitter := &fakeAuditEmitter{}
	repo := sqliterepo.New(sqliterepo.Deps{
		Clock: clock.System(),
		IDGen: idgen.NewULID(),
		Audit: emitter,
		Scan:  scan,
	})
	return repo, emitter, store
}

func seedTenant(t *testing.T, store storage.Storage, tenantID string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO tenants (id, name, plan, created_at) VALUES (?, 'test', 'desktop_free', ?)`,
			tenantID, now)
		return err
	}); err != nil {
		t.Fatalf("seedTenant: %v", err)
	}
}

func tenantCtx(tenantID string) context.Context {
	return storage.WithTenantID(context.Background(), storage.TenantID(tenantID))
}

func tsAt(year int, month time.Month, day int) *time.Time {
	t := time.Date(year, month, day, 12, 0, 0, 0, time.UTC)
	return &t
}

// makeDriftScenario는 직전 5 sessions × 1 robot × 1 check (pass→fail 전이) 시나리오 fake를 만듭니다.
func makeDriftScenario(tenantID, fleetID, robotID, checkID string) *fakeScanReader {
	outcomes := []string{"pass", "pass", "pass", "pass", "fail"}
	var sessions []insight.ScanSessionView
	resultsBySession := make(map[string][]insight.ScanResultView)
	for i, o := range outcomes {
		sid := "ss_drift_" + string(rune('A'+i))
		sessions = append(sessions, insight.ScanSessionView{
			ID:          sid,
			TenantID:    storage.TenantID(tenantID),
			FleetID:     fleetID,
			Status:      "completed",
			CompletedAt: tsAt(2026, 4, 20+i),
		})
		resultsBySession[sid] = []insight.ScanResultView{{
			ID: "scr_" + sid, SessionID: sid, RobotID: robotID, CheckID: checkID,
			Outcome: o, DurationMs: 100,
		}}
	}
	return &fakeScanReader{
		sessionsByFleet:  map[string][]insight.ScanSessionView{fleetID: sessions},
		resultsBySession: resultsBySession,
	}
}

func TestRunForFleetInsertsAndEmitsAudit(t *testing.T) {
	t.Parallel()
	const tenantID, fleetID = "tn_RUN_1", "fl_RUN_1"
	scanFake := makeDriftScenario(tenantID, fleetID, "ro_R1", "ck_R1")
	repo, emitter, store := newTestRepo(t, scanFake)
	seedTenant(t, store, tenantID)

	var inserted []insight.Insight
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		ins, err := repo.RunForFleet(ctx, tx, fleetID)
		inserted = ins
		return err
	}); err != nil {
		t.Fatalf("RunForFleet: %v", err)
	}

	if len(inserted) < 1 {
		t.Fatalf("inserted = %d, want >= 1 (drift detected)", len(inserted))
	}
	for i, in := range inserted {
		if in.ID == "" || len(in.ID) < 4 || in.ID[:4] != "ins_" {
			t.Errorf("inserted[%d].ID = %q, want ins_ prefix", i, in.ID)
		}
		if in.TenantID != storage.TenantID(tenantID) {
			t.Errorf("inserted[%d].TenantID = %q, want %q", i, in.TenantID, tenantID)
		}
	}
	if emitter.createdCount != len(inserted) {
		t.Errorf("audit createdCount = %d, want %d", emitter.createdCount, len(inserted))
	}
}

func TestRunForFleetDedupSkipsExistingActive(t *testing.T) {
	t.Parallel()
	const tenantID, fleetID = "tn_DUP_1", "fl_DUP_1"
	scanFake := makeDriftScenario(tenantID, fleetID, "ro_D1", "ck_D1")
	repo, emitter, store := newTestRepo(t, scanFake)
	seedTenant(t, store, tenantID)

	var firstCount int
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		ins, err := repo.RunForFleet(ctx, tx, fleetID)
		firstCount = len(ins)
		return err
	}); err != nil {
		t.Fatalf("RunForFleet first: %v", err)
	}
	if firstCount < 1 {
		t.Fatalf("first run inserted %d, want >= 1", firstCount)
	}

	// 두 번째 실행 — 같은 detector 산출 → dedup으로 0건 INSERT.
	var secondCount int
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		ins, err := repo.RunForFleet(ctx, tx, fleetID)
		secondCount = len(ins)
		return err
	}); err != nil {
		t.Fatalf("RunForFleet second: %v", err)
	}
	if secondCount != 0 {
		t.Errorf("second run inserted %d, want 0 (dedup)", secondCount)
	}
	if emitter.createdCount != firstCount {
		t.Errorf("audit createdCount = %d, want %d (no new emits)", emitter.createdCount, firstCount)
	}
}

func TestListActiveExcludesDismissed(t *testing.T) {
	t.Parallel()
	const tenantID, fleetID = "tn_LIST_1", "fl_LIST_1"
	scanFake := makeDriftScenario(tenantID, fleetID, "ro_L1", "ck_L1")
	repo, _, store := newTestRepo(t, scanFake)
	seedTenant(t, store, tenantID)

	var firstID string
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		ins, err := repo.RunForFleet(ctx, tx, fleetID)
		if err != nil {
			return err
		}
		if len(ins) == 0 {
			t.Fatal("no insights produced")
		}
		firstID = ins[0].ID
		return nil
	}); err != nil {
		t.Fatalf("RunForFleet: %v", err)
	}

	// dismiss.
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		_, err := repo.Dismiss(ctx, tx, firstID, "user_X", "false positive")
		return err
	}); err != nil {
		t.Fatalf("Dismiss: %v", err)
	}

	// ListActive — dismissed 제외.
	var active []insight.Insight
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		a, err := repo.ListActive(ctx, tx, insight.ListFilter{})
		active = a
		return err
	}); err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	for _, in := range active {
		if in.ID == firstID {
			t.Errorf("ListActive returned dismissed insight %s", firstID)
		}
	}
}

func TestDismissUpdatesDismissedAt(t *testing.T) {
	t.Parallel()
	const tenantID, fleetID = "tn_DIS_1", "fl_DIS_1"
	scanFake := makeDriftScenario(tenantID, fleetID, "ro_DI1", "ck_DI1")
	repo, _, store := newTestRepo(t, scanFake)
	seedTenant(t, store, tenantID)

	var firstID string
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		ins, err := repo.RunForFleet(ctx, tx, fleetID)
		if err != nil {
			return err
		}
		firstID = ins[0].ID
		return nil
	}); err != nil {
		t.Fatalf("RunForFleet: %v", err)
	}

	var dismissed insight.Insight
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		d, err := repo.Dismiss(ctx, tx, firstID, "user_Y", "ok")
		dismissed = d
		return err
	}); err != nil {
		t.Fatalf("Dismiss: %v", err)
	}
	if dismissed.DismissedAt == nil {
		t.Error("DismissedAt should be set")
	}
	if dismissed.DismissedBy != "user_Y" {
		t.Errorf("DismissedBy = %q, want user_Y", dismissed.DismissedBy)
	}

	// 두 번째 dismiss — already dismissed, ErrInsightNotFound.
	err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		_, err := repo.Dismiss(ctx, tx, firstID, "user_Z", "again")
		return err
	})
	if !errors.Is(err, insight.ErrInsightNotFound) {
		t.Errorf("err = %v, want ErrInsightNotFound", err)
	}
}

func TestDismissEmitsAudit(t *testing.T) {
	t.Parallel()
	const tenantID, fleetID = "tn_DIS_2", "fl_DIS_2"
	scanFake := makeDriftScenario(tenantID, fleetID, "ro_DI2", "ck_DI2")
	repo, emitter, store := newTestRepo(t, scanFake)
	seedTenant(t, store, tenantID)

	var firstID string
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		ins, err := repo.RunForFleet(ctx, tx, fleetID)
		if err != nil {
			return err
		}
		firstID = ins[0].ID
		return nil
	}); err != nil {
		t.Fatalf("RunForFleet: %v", err)
	}

	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		_, err := repo.Dismiss(ctx, tx, firstID, "user_E", "verified")
		return err
	}); err != nil {
		t.Fatalf("Dismiss: %v", err)
	}
	if emitter.dismissedCount != 1 {
		t.Errorf("dismissedCount = %d, want 1", emitter.dismissedCount)
	}
	if emitter.lastDismissedID != firstID {
		t.Errorf("lastDismissedID = %q, want %q", emitter.lastDismissedID, firstID)
	}
	if emitter.lastReason != "verified" {
		t.Errorf("lastReason = %q, want verified", emitter.lastReason)
	}
}

func TestCrossTenantInsightsIsolated(t *testing.T) {
	t.Parallel()
	const tenantA, tenantB = "tn_X_A", "tn_X_B"
	const fleetA, fleetB = "fl_X_A", "fl_X_B"

	scanA := makeDriftScenario(tenantA, fleetA, "ro_XA", "ck_XA")
	repo, _, store := newTestRepo(t, scanA)
	seedTenant(t, store, tenantA)
	seedTenant(t, store, tenantB)

	// tenant A에서 insight INSERT.
	var aID string
	if err := store.Tx(tenantCtx(tenantA), func(ctx context.Context, tx storage.Tx) error {
		ins, err := repo.RunForFleet(ctx, tx, fleetA)
		if err != nil {
			return err
		}
		aID = ins[0].ID
		return nil
	}); err != nil {
		t.Fatalf("Run A: %v", err)
	}

	// tenant B 컨텍스트에서 ListActive — A 데이터 노출 X.
	if err := store.Tx(tenantCtx(tenantB), func(ctx context.Context, tx storage.Tx) error {
		got, err := repo.ListActive(ctx, tx, insight.ListFilter{})
		if err != nil {
			return err
		}
		for _, in := range got {
			if in.ID == aID {
				t.Errorf("ListActive in B returned A's insight %s", in.ID)
			}
			if in.TenantID != storage.TenantID(tenantB) {
				t.Errorf("ListActive in B returned tenant %s row", in.TenantID)
			}
		}
		// Dismiss A's id from B → ErrInsightNotFound.
		_, err = repo.Dismiss(ctx, tx, aID, "user_B", "x")
		if !errors.Is(err, insight.ErrInsightNotFound) {
			t.Errorf("Dismiss A's id from B: err = %v, want ErrInsightNotFound", err)
		}
		// RunForFleet for fleetA from B → 빈 결과 (B의 fake에 없음 — 사실 같은 fake지만 tenant_id 격리는 INSERT 시 적용).
		// 별도 fake가 아니므로 이 경로는 skip — 위 두 케이스가 isolation 핵심.
		_ = fleetB
		return nil
	}); err != nil {
		t.Fatalf("ListActive B: %v", err)
	}
}

func TestRunForFleetWithEmptyHistoryReturnsNothing(t *testing.T) {
	t.Parallel()
	const tenantID, fleetID = "tn_EMPTY", "fl_EMPTY"
	scanFake := &fakeScanReader{
		sessionsByFleet:  map[string][]insight.ScanSessionView{},
		resultsBySession: map[string][]insight.ScanResultView{},
	}
	repo, emitter, store := newTestRepo(t, scanFake)
	seedTenant(t, store, tenantID)

	var inserted []insight.Insight
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		ins, err := repo.RunForFleet(ctx, tx, fleetID)
		inserted = ins
		return err
	}); err != nil {
		t.Fatalf("RunForFleet: %v", err)
	}
	if len(inserted) != 0 {
		t.Errorf("inserted = %d, want 0 (no history)", len(inserted))
	}
	if emitter.createdCount != 0 {
		t.Errorf("createdCount = %d, want 0", emitter.createdCount)
	}
}

func TestListActiveFilterByKindAndRobot(t *testing.T) {
	t.Parallel()
	const tenantID, fleetID = "tn_F1", "fl_F1"
	scanFake := makeDriftScenario(tenantID, fleetID, "ro_F1", "ck_F1")
	repo, _, store := newTestRepo(t, scanFake)
	seedTenant(t, store, tenantID)

	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		_, err := repo.RunForFleet(ctx, tx, fleetID)
		return err
	}); err != nil {
		t.Fatalf("RunForFleet: %v", err)
	}

	// drift kind 필터 — 1건 이상.
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		got, err := repo.ListActive(ctx, tx, insight.ListFilter{Kind: insight.KindDrift})
		if err != nil {
			return err
		}
		if len(got) == 0 {
			t.Errorf("Kind=drift filter returned 0, want >= 1")
		}
		for _, in := range got {
			if in.Kind != insight.KindDrift {
				t.Errorf("filter leak: got kind=%s", in.Kind)
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("ListActive drift: %v", err)
	}

	// 다른 robot 필터 — 0건.
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		got, err := repo.ListActive(ctx, tx, insight.ListFilter{RobotID: "ro_OTHER"})
		if err != nil {
			return err
		}
		if len(got) != 0 {
			t.Errorf("RobotID=ro_OTHER filter returned %d, want 0", len(got))
		}
		return nil
	}); err != nil {
		t.Fatalf("ListActive robot filter: %v", err)
	}

	// 같은 robot 필터 — 1건 이상.
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		got, err := repo.ListActive(ctx, tx, insight.ListFilter{RobotID: "ro_F1"})
		if err != nil {
			return err
		}
		if len(got) == 0 {
			t.Errorf("RobotID=ro_F1 filter returned 0, want >= 1")
		}
		return nil
	}); err != nil {
		t.Fatalf("ListActive robot filter match: %v", err)
	}
}
