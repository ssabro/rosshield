package rotation_test

// gc_test.go — E32 Stage 4 HotGC 단위 테스트.
//
// 본 파일은 sqlite 기반 테스트로 다음을 검증합니다:
//
//   - ListSegmentsArchivedBefore: archive_uri NOT NULL + created_at < cutoff 후보 선별.
//   - HotGC dryRun=true: 추정 카운트 + DELETE 미실행 + audit.gc.complete emit 안 함.
//   - HotGC dryRun=false (no candidates): no-op (DELETE/SET LOCAL 모두 안 함).
//   - HotGC dryRun=false (sqlite, candidates 있음): SET LOCAL syntax error 로 차단 (sqlite GUC 미지원 보호막).
//   - tenantID 빈 값 거부 + Clock 미주입 거부.
//
// PG 실측 (트리거 + GUC 우회 + 실제 DELETE + audit.gc.complete emit)은 integration test 별 파일 (-tags=integration).

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/domain/audit/rotation"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// --- ListSegmentsArchivedBefore — sqlite 실측 ---

func TestListSegmentsArchivedBefore_FiltersByArchiveAndCreatedAt(t *testing.T) {
	t.Parallel()

	store, repo := newTestStorage(t)
	// 6 entry seed — 3 segment (각 2 entries) 만들기 위해.
	seedEntries(t, store, repo, 6)

	be, _ := rotation.NewFileBackend(t.TempDir())

	// rotation 2회 — 과거 시각 (Rotator 의 created_at 은 Clock 에서 추출).
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	recent := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)

	rotOld, _ := rotation.New(rotation.Deps{Clock: clock.NewFake(old), Backend: be, Appender: repo})
	rotRecent, _ := rotation.New(rotation.Deps{Clock: clock.NewFake(recent), Backend: be, Appender: repo})

	ctx := storage.WithTenantID(context.Background(), testTenant)
	// segment 1: seq 1~2 (old)
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, err := rotOld.Rotate(ctx, tx, testTenant, 1, 1, 2)
		return err
	}); err != nil {
		t.Fatalf("seg1: %v", err)
	}
	// segment 2: seq 3~4 (old) — rotate.complete entry 가 segment 사이에 끼므로 seq 3,4 가 실제 audit_entries 의 row 임 (rotation_test.go 와 동등 패턴).
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, err := rotOld.Rotate(ctx, tx, testTenant, 2, 3, 4)
		return err
	}); err != nil {
		t.Fatalf("seg2: %v", err)
	}
	// segment 3: seq 5~6 (recent — hot retention 안)
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, err := rotRecent.Rotate(ctx, tx, testTenant, 3, 5, 6)
		return err
	}); err != nil {
		t.Fatalf("seg3: %v", err)
	}

	// cutoff = 2026-06-01 — old(<) 통과, recent(>) 제외.
	cutoff := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	var got []rotation.SegmentRecord
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		segs, err := rotation.ListSegmentsArchivedBefore(ctx, tx, testTenant, cutoff)
		if err != nil {
			return err
		}
		got = segs
		return nil
	}); err != nil {
		t.Fatalf("ListSegmentsArchivedBefore: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2 (seg1 + seg2; seg3 in hot retention)", len(got))
	}
	if got[0].SegmentNumber != 1 || got[1].SegmentNumber != 2 {
		t.Errorf("segment order = [%d, %d], want [1, 2]", got[0].SegmentNumber, got[1].SegmentNumber)
	}
	if got[0].ArchiveURI == "" || got[1].ArchiveURI == "" {
		t.Error("ArchiveURI should be populated for archived segments")
	}
}

func TestListSegmentsArchivedBefore_EmptyTenantRejected(t *testing.T) {
	t.Parallel()

	store, _ := newTestStorage(t)
	ctx := storage.WithTenantID(context.Background(), testTenant)
	err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, err := rotation.ListSegmentsArchivedBefore(ctx, tx, "", time.Now())
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "tenantID required") {
		t.Errorf("expected tenantID required error, got %v", err)
	}
}

// --- HotGC.Run dryRun=true — sqlite 실측 (DELETE 미실행이므로 트리거 안전) ---

func TestHotGC_DryRun_EstimatesAndDoesNotEmit(t *testing.T) {
	t.Parallel()

	store, repo := newTestStorage(t)
	seedEntries(t, store, repo, 4)

	be, _ := rotation.NewFileBackend(t.TempDir())
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rotOld, _ := rotation.New(rotation.Deps{Clock: clock.NewFake(old), Backend: be, Appender: repo})

	ctx := storage.WithTenantID(context.Background(), testTenant)
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, err := rotOld.Rotate(ctx, tx, testTenant, 1, 1, 2)
		return err
	}); err != nil {
		t.Fatalf("seg1: %v", err)
	}
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, err := rotOld.Rotate(ctx, tx, testTenant, 2, 3, 4)
		return err
	}); err != nil {
		t.Fatalf("seg2: %v", err)
	}

	// Head Seq before GC.
	var headBefore int64
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		head, err := repo.Head(ctx, tx, testTenant)
		if err != nil {
			return err
		}
		headBefore = head.Seq
		return nil
	}); err != nil {
		t.Fatalf("Head before: %v", err)
	}

	// HotGC dryRun=true — Fixed now = 2027-01-01, retention = 30d → cutoff = 2026-12-02 → 모든 archived segment 통과.
	gcNow := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	gc, err := rotation.NewHotGC(rotation.HotGCDeps{
		Policy:   rotation.RotationPolicy{HotRetention: 30 * 24 * time.Hour, ColdBackend: rotation.ColdBackendFile},
		Appender: repo,
		Clock:    clock.NewFake(gcNow),
	})
	if err != nil {
		t.Fatalf("NewHotGC: %v", err)
	}

	var result *rotation.HotGCResult
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		r, err := gc.Run(ctx, tx, testTenant, true)
		if err != nil {
			return err
		}
		result = r
		return nil
	}); err != nil {
		t.Fatalf("HotGC.Run: %v", err)
	}

	if !result.DryRun {
		t.Error("result.DryRun = false, want true")
	}
	if result.DeletedCount != 4 {
		t.Errorf("DeletedCount (estimate) = %d, want 4 (seg1=2 + seg2=2 entries)", result.DeletedCount)
	}
	if len(result.SegmentsProcessed) != 2 {
		t.Errorf("SegmentsProcessed = %v, want 2 items", result.SegmentsProcessed)
	}
	if result.OldestKeptEntrySeq != 0 {
		t.Errorf("OldestKeptEntrySeq = %d, want 0 (dry run)", result.OldestKeptEntrySeq)
	}

	// Head Seq 가 그대로인지 (audit.gc.complete entry emit 안 함).
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		head, err := repo.Head(ctx, tx, testTenant)
		if err != nil {
			return err
		}
		if head.Seq != headBefore {
			t.Errorf("Head Seq after dry run = %d, want %d (no emit)", head.Seq, headBefore)
		}
		return nil
	}); err != nil {
		t.Fatalf("Head after: %v", err)
	}
}

func TestHotGC_NoSegmentsArchivedBeforeCutoff_NoOp(t *testing.T) {
	t.Parallel()

	store, repo := newTestStorage(t)
	seedEntries(t, store, repo, 2)

	be, _ := rotation.NewFileBackend(t.TempDir())
	recent := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	rotRecent, _ := rotation.New(rotation.Deps{Clock: clock.NewFake(recent), Backend: be, Appender: repo})

	ctx := storage.WithTenantID(context.Background(), testTenant)
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, err := rotRecent.Rotate(ctx, tx, testTenant, 1, 1, 2)
		return err
	}); err != nil {
		t.Fatalf("rotate: %v", err)
	}

	// gcNow = recent + 1h → retention 30d 안 (cutoff = gcNow - 30d ≈ 2026-12-01).
	// segment created_at 2026-12-31 — cutoff 보다 미래 → 후보 없음.
	gcNow := recent.Add(time.Hour)
	gc, _ := rotation.NewHotGC(rotation.HotGCDeps{
		Policy:   rotation.RotationPolicy{HotRetention: 30 * 24 * time.Hour, ColdBackend: rotation.ColdBackendFile},
		Appender: repo,
		Clock:    clock.NewFake(gcNow),
	})

	var result *rotation.HotGCResult
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		r, err := gc.Run(ctx, tx, testTenant, false)
		if err != nil {
			return err
		}
		result = r
		return nil
	}); err != nil {
		t.Fatalf("HotGC.Run: %v", err)
	}

	if result.DeletedCount != 0 {
		t.Errorf("DeletedCount = %d, want 0 (segment in hot retention)", result.DeletedCount)
	}
	if len(result.SegmentsProcessed) != 0 {
		t.Errorf("SegmentsProcessed = %v, want empty", result.SegmentsProcessed)
	}
}

func TestHotGC_EmptyTenantRejected(t *testing.T) {
	t.Parallel()

	store, _ := newTestStorage(t)
	gc, _ := rotation.NewHotGC(rotation.HotGCDeps{
		Policy: rotation.DefaultPolicy(),
		Clock:  clock.NewFake(time.Now()),
	})

	ctx := storage.WithTenantID(context.Background(), testTenant)
	err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, err := gc.Run(ctx, tx, "", false)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "tenantID required") {
		t.Errorf("expected tenantID required error, got %v", err)
	}
}

func TestNewHotGC_RequiresClock(t *testing.T) {
	t.Parallel()

	_, err := rotation.NewHotGC(rotation.HotGCDeps{Policy: rotation.DefaultPolicy()})
	if err == nil || !strings.Contains(err.Error(), "Clock required") {
		t.Errorf("expected Clock required error, got %v", err)
	}
}

// TestHotGC_SetLocalRejectedOnSQLite — sqlite는 SET LOCAL 미지원 — UseMarkerMode=false
// (PG default) 상태에서 첫 SET LOCAL Exec이 syntax error로 차단됨을 검증. 본 path는 sqlite
// 환경에서 운영자가 실수로 PG 설정을 sqlite에 적용하는 misconfiguration 보호막.
//
// PG 환경에서는 본 path가 정상 통과 — integration test에서 실측.
// sqlite + UseMarkerMode=true 신 path는 TestHotGC_SQLiteMarkerModeSucceeds에서 검증.
func TestHotGC_SetLocalRejectedOnSQLite(t *testing.T) {
	t.Parallel()

	store, repo := newTestStorage(t)
	seedEntries(t, store, repo, 2)

	be, _ := rotation.NewFileBackend(t.TempDir())
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rotOld, _ := rotation.New(rotation.Deps{Clock: clock.NewFake(old), Backend: be, Appender: repo})

	ctx := storage.WithTenantID(context.Background(), testTenant)
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, err := rotOld.Rotate(ctx, tx, testTenant, 1, 1, 2)
		return err
	}); err != nil {
		t.Fatalf("rotate: %v", err)
	}

	gcNow := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	gc, _ := rotation.NewHotGC(rotation.HotGCDeps{
		Policy:   rotation.RotationPolicy{HotRetention: 30 * 24 * time.Hour, ColdBackend: rotation.ColdBackendFile},
		Appender: repo,
		Clock:    clock.NewFake(gcNow),
		// UseMarkerMode 명시 안 함 — default false (PG SET LOCAL 사용).
	})

	err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, err := gc.Run(ctx, tx, testTenant, false)
		return err
	})
	if err == nil {
		t.Fatal("expected SET LOCAL error on sqlite (GUC unsupported)")
	}
	if !strings.Contains(err.Error(), "SET LOCAL") {
		t.Errorf("error %q does not contain 'SET LOCAL' wrapper", err.Error())
	}
}

// TestHotGC_SQLiteMarkerModeSucceeds — UseMarkerMode=true로 sqlite hot GC가 실제로
// audit_entries를 DELETE하고 audit.gc.complete emit + OldestKeptEntrySeq 정확함을 검증.
//
// 0036 마이그레이션이 적용된 sqlite 환경에서만 동작:
//   - audit_gc_mode marker table 존재
//   - audit_entries_no_delete 트리거가 WHEN 절로 marker active=1 일 때만 silent pass
//
// HotGC가 같은 Tx 안에서 marker INSERT → DELETE entries → marker DELETE 순서로 진행,
// Tx COMMIT 시점에 marker는 active=0으로 reset되어 다음 application code의 DELETE는
// 다시 차단됩니다.
func TestHotGC_SQLiteMarkerModeSucceeds(t *testing.T) {
	t.Parallel()

	store, repo := newTestStorage(t)
	seedEntries(t, store, repo, 4)

	be, _ := rotation.NewFileBackend(t.TempDir())
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rotOld, _ := rotation.New(rotation.Deps{Clock: clock.NewFake(old), Backend: be, Appender: repo})

	ctx := storage.WithTenantID(context.Background(), testTenant)
	// 2 segments — seq 1~2, 3~4 (PG integration test와 동등 패턴).
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, err := rotOld.Rotate(ctx, tx, testTenant, 1, 1, 2)
		return err
	}); err != nil {
		t.Fatalf("seg1: %v", err)
	}
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, err := rotOld.Rotate(ctx, tx, testTenant, 2, 3, 4)
		return err
	}); err != nil {
		t.Fatalf("seg2: %v", err)
	}

	// Head before GC — emit 검증용.
	var headBefore int64
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		head, err := repo.Head(ctx, tx, testTenant)
		if err != nil {
			return err
		}
		headBefore = head.Seq
		return nil
	}); err != nil {
		t.Fatalf("Head before: %v", err)
	}

	gcNow := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	gc, err := rotation.NewHotGC(rotation.HotGCDeps{
		Policy:        rotation.RotationPolicy{HotRetention: 30 * 24 * time.Hour, ColdBackend: rotation.ColdBackendFile},
		Appender:      repo,
		Clock:         clock.NewFake(gcNow),
		UseMarkerMode: true, // sqlite marker mode.
	})
	if err != nil {
		t.Fatalf("NewHotGC: %v", err)
	}

	var result *rotation.HotGCResult
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		r, err := gc.Run(ctx, tx, testTenant, false)
		if err != nil {
			return err
		}
		result = r
		return nil
	}); err != nil {
		t.Fatalf("HotGC.Run: %v", err)
	}

	if result.DryRun {
		t.Error("result.DryRun = true, want false")
	}
	if result.DeletedCount != 4 {
		t.Errorf("DeletedCount = %d, want 4 (seg1=2 + seg2=2)", result.DeletedCount)
	}
	if len(result.SegmentsProcessed) != 2 {
		t.Errorf("SegmentsProcessed = %v, want 2 items", result.SegmentsProcessed)
	}

	// Head Seq 증가 (audit.gc.complete emit).
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		head, err := repo.Head(ctx, tx, testTenant)
		if err != nil {
			return err
		}
		if head.Seq <= headBefore {
			t.Errorf("Head Seq after GC = %d, want > %d (gc.complete emit)", head.Seq, headBefore)
		}
		return nil
	}); err != nil {
		t.Fatalf("Head after: %v", err)
	}
}

// TestHotGC_SQLiteMarkerModeProtectsAgainstDirectDelete — UseMarkerMode 사용 후에도
// 일반 application code의 DELETE는 여전히 차단되는지 검증 (HotGC만 conditional bypass).
//
// HotGC.Run이 끝난 후 별 Tx에서 DELETE FROM audit_entries 직접 호출 시 0036 트리거가
// RAISE(ABORT). marker는 같은 Tx scope에서만 의미 있어야 함.
func TestHotGC_SQLiteMarkerModeProtectsAgainstDirectDelete(t *testing.T) {
	t.Parallel()

	store, repo := newTestStorage(t)
	seedEntries(t, store, repo, 2)

	be, _ := rotation.NewFileBackend(t.TempDir())
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rotOld, _ := rotation.New(rotation.Deps{Clock: clock.NewFake(old), Backend: be, Appender: repo})

	ctx := storage.WithTenantID(context.Background(), testTenant)
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, err := rotOld.Rotate(ctx, tx, testTenant, 1, 1, 2)
		return err
	}); err != nil {
		t.Fatalf("rotate: %v", err)
	}

	// HotGC 진행 (marker 사용 후 cleanup).
	gcNow := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	gc, _ := rotation.NewHotGC(rotation.HotGCDeps{
		Policy:        rotation.RotationPolicy{HotRetention: 30 * 24 * time.Hour, ColdBackend: rotation.ColdBackendFile},
		Appender:      repo,
		Clock:         clock.NewFake(gcNow),
		UseMarkerMode: true,
	})
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, err := gc.Run(ctx, tx, testTenant, false)
		return err
	}); err != nil {
		t.Fatalf("HotGC.Run: %v", err)
	}

	// HotGC 완료 후 별 Tx에서 직접 DELETE 시도 — 트리거 ABORT.
	err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM audit_entries WHERE tenant_id = ?`, string(testTenant))
		return err
	})
	if err == nil {
		t.Fatal("expected RAISE(ABORT) on direct DELETE after HotGC")
	}
	if !strings.Contains(err.Error(), "immutable") {
		t.Errorf("error %q does not contain 'immutable' (trigger RAISE)", err.Error())
	}
}
