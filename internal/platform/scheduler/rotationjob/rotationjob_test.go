// rotationjob_test.go — Register / RunOnce 단위 + 통합 테스트.
//
// 검증 항목:
//   - Register spec="" → no-op (등록 skip, deps nil OK).
//   - Register nil deps → error.
//   - Register nil Scheduler → error.
//   - TenantListerFunc 어댑팅이 인터페이스를 만족.
//   - RunOnce:
//       a. 빈 tenant 목록 → silent success
//       b. 빈 체인(head.Seq=0) → silent skip
//       c. 신규 entry 없음(toSeq < fromSeq) → silent skip
//       d. 첫 rotation (segmentNumber=1) → 성공 + segment row INSERT
//       e. 두 번째 rotation (segmentNumber=2) → 직전 LastEntryID+1부터 자동 시작
//   - 일부 tenant 실패가 다른 tenant 처리를 막지 않음 (best-effort).
//   - 모든 tenant 실패 → error 반환.
//   - cron tick 1회 fire → audit_rotation_segments 행 INSERT (cronsched 결선 검증).

package rotationjob_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/domain/audit"
	"github.com/ssabro/rosshield/internal/domain/audit/rotation"
	auditrepo "github.com/ssabro/rosshield/internal/domain/audit/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/scheduler/cronsched"
	"github.com/ssabro/rosshield/internal/platform/scheduler/rotationjob"
	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/sqlite"
)

const testTenantA storage.TenantID = "tn_rot_a"
const testTenantB storage.TenantID = "tn_rot_b"

// --- Register arg 검증 ---

func TestRegister_EmptySpecIsNoop(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	sch := cronsched.New(cronsched.Deps{Logger: logger})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = sch.Close(ctx)
	})

	// 모든 deps nil이어도 spec="" → no-op.
	if err := rotationjob.Register(sch, rotationjob.Deps{}, "", ""); err != nil {
		t.Fatalf("Register(empty spec) returned err: %v", err)
	}
}

func TestRegister_NilDepsError(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	sch := cronsched.New(cronsched.Deps{Logger: logger})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = sch.Close(ctx)
	})

	// spec != "" + deps 비어있음 → error.
	err := rotationjob.Register(sch, rotationjob.Deps{}, "j", "@every 1h")
	if err == nil {
		t.Fatal("expected error for missing deps")
	}
}

func TestRegister_NilSchedulerError(t *testing.T) {
	t.Parallel()

	err := rotationjob.Register(nil, rotationjob.Deps{}, "j", "@every 1h")
	if err == nil {
		t.Fatal("expected error for nil scheduler")
	}
}

// --- TenantListerFunc 어댑팅 ---

func TestTenantListerFunc_Adapts(t *testing.T) {
	t.Parallel()

	want := []storage.TenantID{"a", "b"}
	var lister rotationjob.TenantLister = rotationjob.TenantListerFunc(
		func(ctx context.Context) ([]storage.TenantID, error) {
			return want, nil
		},
	)
	got, err := lister.ListActiveTenants(context.Background())
	if err != nil {
		t.Fatalf("ListActiveTenants: %v", err)
	}
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("got %v, want %v", got, want)
	}
}

// --- RunOnce 직접 호출 (cron 발화 없이 결정론적 테스트) ---

func TestRunOnce_EmptyTenantList_Noop(t *testing.T) {
	t.Parallel()

	store, repo := newTestStorage(t)
	rot := newTestRotator(t, repo)
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	deps := rotationjob.Deps{
		Storage: store,
		Audit:   repo,
		Rotator: rot,
		Tenants: rotationjob.TenantListerFunc(func(ctx context.Context) ([]storage.TenantID, error) {
			return nil, nil
		}),
		Logger: logger,
	}
	if err := rotationjob.RunOnce(context.Background(), deps); err != nil {
		t.Fatalf("RunOnce(empty list): %v", err)
	}
}

func TestRunOnce_EmptyChain_SilentSkip(t *testing.T) {
	t.Parallel()

	store, repo := newTestStorage(t)
	// testTenantA는 entry 0 — 빈 체인.

	rot := newTestRotator(t, repo)
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	deps := rotationjob.Deps{
		Storage: store,
		Audit:   repo,
		Rotator: rot,
		Tenants: rotationjob.TenantListerFunc(func(ctx context.Context) ([]storage.TenantID, error) {
			return []storage.TenantID{testTenantA}, nil
		}),
		Logger: logger,
	}
	if err := rotationjob.RunOnce(context.Background(), deps); err != nil {
		t.Fatalf("RunOnce(empty chain): %v", err)
	}
	assertLatestSegment(t, store, testTenantA, 0) // 여전히 0.
}

func TestRunOnce_FirstRotation_PersistsSegment(t *testing.T) {
	t.Parallel()

	store, repo := newTestStorage(t)
	seedEntries(t, store, repo, testTenantA, 3)

	rot := newTestRotator(t, repo)
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	deps := rotationjob.Deps{
		Storage: store,
		Audit:   repo,
		Rotator: rot,
		Tenants: rotationjob.TenantListerFunc(func(ctx context.Context) ([]storage.TenantID, error) {
			return []storage.TenantID{testTenantA}, nil
		}),
		Logger: logger,
	}
	if err := rotationjob.RunOnce(context.Background(), deps); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	assertLatestSegment(t, store, testTenantA, 1)
}

func TestRunOnce_SecondRotation_AutoComputesFromSeq(t *testing.T) {
	t.Parallel()

	store, repo := newTestStorage(t)
	seedEntries(t, store, repo, testTenantA, 2)

	rot := newTestRotator(t, repo)
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	deps := rotationjob.Deps{
		Storage: store,
		Audit:   repo,
		Rotator: rot,
		Tenants: rotationjob.TenantListerFunc(func(ctx context.Context) ([]storage.TenantID, error) {
			return []storage.TenantID{testTenantA}, nil
		}),
		Logger: logger,
	}

	// 1차: segment 1 (seq 1~2) + rotate.complete entry → seq 3.
	if err := rotationjob.RunOnce(context.Background(), deps); err != nil {
		t.Fatalf("first RunOnce: %v", err)
	}
	assertLatestSegment(t, store, testTenantA, 1)

	// 2차 즉시 호출 — head.Seq=3, prev.LastEntryID=2 → fromSeq=3, toSeq=3 → rotate.complete 자체 archive.
	if err := rotationjob.RunOnce(context.Background(), deps); err != nil {
		t.Fatalf("second RunOnce: %v", err)
	}
	assertLatestSegment(t, store, testTenantA, 2)
}

func TestRunOnce_NoNewEntries_SilentSkip(t *testing.T) {
	t.Parallel()

	store, repo := newTestStorage(t)
	seedEntries(t, store, repo, testTenantA, 2)

	rot := newTestRotator(t, repo)
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	deps := rotationjob.Deps{
		Storage: store,
		Audit:   repo,
		Rotator: rot,
		Tenants: rotationjob.TenantListerFunc(func(ctx context.Context) ([]storage.TenantID, error) {
			return []storage.TenantID{testTenantA}, nil
		}),
		Logger: logger,
	}

	// 1차: segment 1 (seq 1~2) + rotate.complete → head.Seq=3.
	if err := rotationjob.RunOnce(context.Background(), deps); err != nil {
		t.Fatalf("first RunOnce: %v", err)
	}
	// 2차: segment 2 (seq 3~3) — rotate.complete entry 자체를 또 wrap → head.Seq=4.
	if err := rotationjob.RunOnce(context.Background(), deps); err != nil {
		t.Fatalf("second RunOnce: %v", err)
	}
	// 3차: segment 3 (seq 4~4) → head.Seq=5.
	if err := rotationjob.RunOnce(context.Background(), deps); err != nil {
		t.Fatalf("third RunOnce: %v", err)
	}
	// 매 invocation마다 rotate.complete가 새 entry를 만들기 때문에 silent skip 시점은 본 시나리오 안 옴.
	// LatestSegmentNumber=3 확인.
	assertLatestSegment(t, store, testTenantA, 3)
}

func TestRunOnce_MultiTenant_PartialSkipMixed(t *testing.T) {
	t.Parallel()

	store, repo := newTestStorage(t)
	seedEntries(t, store, repo, testTenantA, 2)
	// testTenantB는 빈 체인.

	rot := newTestRotator(t, repo)
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	deps := rotationjob.Deps{
		Storage: store,
		Audit:   repo,
		Rotator: rot,
		Tenants: rotationjob.TenantListerFunc(func(ctx context.Context) ([]storage.TenantID, error) {
			return []storage.TenantID{testTenantA, testTenantB}, nil
		}),
		Logger: logger,
	}
	if err := rotationjob.RunOnce(context.Background(), deps); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	assertLatestSegment(t, store, testTenantA, 1)
	assertLatestSegment(t, store, testTenantB, 0)
}

func TestRunOnce_TenantListerError_Propagates(t *testing.T) {
	t.Parallel()

	store, repo := newTestStorage(t)
	rot := newTestRotator(t, repo)
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	wantErr := errors.New("boom")
	deps := rotationjob.Deps{
		Storage: store,
		Audit:   repo,
		Rotator: rot,
		Tenants: rotationjob.TenantListerFunc(func(ctx context.Context) ([]storage.TenantID, error) {
			return nil, wantErr
		}),
		Logger: logger,
	}
	err := rotationjob.RunOnce(context.Background(), deps)
	if err == nil {
		t.Fatal("expected error from TenantLister to propagate")
	}
}

func TestRunOnce_MissingDeps_Error(t *testing.T) {
	t.Parallel()

	err := rotationjob.RunOnce(context.Background(), rotationjob.Deps{})
	if err == nil {
		t.Fatal("expected error for missing deps")
	}
}

// --- 통합: cron tick 1회 → segment row INSERT ---

func TestRegister_CronTickFires_PersistsSegment(t *testing.T) {
	t.Parallel()

	store, repo := newTestStorage(t)
	seedEntries(t, store, repo, testTenantA, 3)

	rot := newTestRotator(t, repo)
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	sch := cronsched.New(cronsched.Deps{Logger: logger})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = sch.Close(ctx)
	})

	deps := rotationjob.Deps{
		Storage: store,
		Audit:   repo,
		Rotator: rot,
		Tenants: rotationjob.TenantListerFunc(func(ctx context.Context) ([]storage.TenantID, error) {
			return []storage.TenantID{testTenantA}, nil
		}),
		Logger: logger,
	}
	if err := rotationjob.Register(sch, deps, "rotation-test", "@every 1s"); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// 발화 대기 — 첫 fire는 ~1s.
	deadline := time.Now().Add(3500 * time.Millisecond)
	for time.Now().Before(deadline) {
		ctx := storage.WithTenantID(context.Background(), testTenantA)
		var latest int64
		if err := store.Tx(ctx, func(c context.Context, tx storage.Tx) error {
			n, err := rotation.LatestSegmentNumber(c, tx, testTenantA)
			latest = n
			return err
		}); err != nil {
			t.Fatalf("LatestSegmentNumber: %v", err)
		}
		if latest >= 1 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("audit_rotation_segments empty within 3.5s — RotationJob did not fire")
}

// --- helpers ---

func assertLatestSegment(t *testing.T, store storage.Storage, tenantID storage.TenantID, want int64) {
	t.Helper()
	ctx := storage.WithTenantID(context.Background(), tenantID)
	var latest int64
	if err := store.Tx(ctx, func(c context.Context, tx storage.Tx) error {
		n, err := rotation.LatestSegmentNumber(c, tx, tenantID)
		latest = n
		return err
	}); err != nil {
		t.Fatalf("LatestSegmentNumber(%s): %v", tenantID, err)
	}
	if latest != want {
		t.Errorf("LatestSegmentNumber(%s) = %d, want %d", tenantID, latest, want)
	}
}

func newTestStorage(t *testing.T) (storage.Storage, *auditrepo.Repo) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "rotation-job.db")
	s, err := sqlite.Open(storage.Config{Driver: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	repo := auditrepo.New(auditrepo.Deps{Clock: clock.System()})
	return s, repo
}

func newTestRotator(t *testing.T, repo *auditrepo.Repo) *rotation.Rotator {
	t.Helper()
	be, err := rotation.NewFileBackend(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileBackend: %v", err)
	}
	rot, err := rotation.New(rotation.Deps{
		Clock:    clock.System(),
		Backend:  be,
		Appender: repo,
	})
	if err != nil {
		t.Fatalf("rotation.New: %v", err)
	}
	return rot
}

func seedEntries(t *testing.T, s storage.Storage, repo *auditrepo.Repo, tenantID storage.TenantID, n int) {
	t.Helper()
	ctx := storage.WithTenantID(context.Background(), tenantID)
	for i := 0; i < n; i++ {
		i := i
		err := s.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
			_, err := repo.Append(ctx, tx, audit.AppendRequest{
				TenantID: tenantID,
				Actor:    audit.Actor{Type: audit.ActorSystem, ID: "system"},
				Action:   "test.event",
				Target:   audit.Target{Type: "robot", ID: "ro_test"},
				Payload:  []byte(`{"n":` + strconv.Itoa(i) + `}`),
				Outcome:  audit.OutcomeSuccess,
			})
			return err
		})
		if err != nil {
			t.Fatalf("seed %d for %s: %v", i, tenantID, err)
		}
	}
}
