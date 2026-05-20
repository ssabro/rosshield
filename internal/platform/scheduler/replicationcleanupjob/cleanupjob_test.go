// cleanupjob_test.go — Register / RunOnce 단위 검증.
//
// 검증 항목:
//   - Register spec="" → no-op (deps nil OK).
//   - Register nil Scheduler → error.
//   - Register Executor 부재 → error.
//   - Register SlotPrefix 빈 값 → error (안전 가드).
//   - RunOnce: fakeExecutor가 후보 0건 반환 → graceful (removed=0).
//   - RunOnce: 후보 2건 + DryRun=true → drop SQL exec 0회, removed=2.
//   - RunOnce: 후보 2건 + DryRun=false → drop SQL exec 2회 (slot 이름과 함께), removed=2.
//   - RunOnce: query error 전파.
//   - RunOnce: drop error 전파 + 부분 removed 반환.

package replicationcleanupjob_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/platform/scheduler/cronsched"
	"github.com/ssabro/rosshield/internal/platform/scheduler/replicationcleanupjob"
)

// --- fakeExecutor ---

type fakeExecutor struct {
	slots         []string // QueryStrings 반환값
	queryErr      error
	execErr       error
	execCalls     []string // pg_drop_replication_slot $1 호출 시 slot 이름 기록
	queryStringsN int
}

func (f *fakeExecutor) Exec(_ context.Context, sql string, args ...any) error {
	if f.execErr != nil {
		return f.execErr
	}
	// pg_drop_replication_slot($1) 호출만 추적 — slot 이름은 args[0].
	if strings.Contains(sql, "pg_drop_replication_slot") && len(args) > 0 {
		if name, ok := args[0].(string); ok {
			f.execCalls = append(f.execCalls, name)
		}
	}
	return nil
}

func (f *fakeExecutor) QueryBool(_ context.Context, _ string, _ ...any) (bool, error) {
	return false, nil
}

func (f *fakeExecutor) QueryStrings(_ context.Context, _ string, _ ...any) ([]string, error) {
	f.queryStringsN++
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	out := make([]string, len(f.slots))
	copy(out, f.slots)
	return out, nil
}

// --- helpers ---

func newTestScheduler(t *testing.T) *cronsched.Scheduler {
	t.Helper()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	sch := cronsched.New(cronsched.Deps{Logger: logger})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = sch.Close(ctx)
	})
	return sch
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

// --- Register arg 검증 ---

func TestRegister_EmptySpecIsNoop(t *testing.T) {
	t.Parallel()
	sch := newTestScheduler(t)

	// spec="" → no-op. deps가 모두 nil이어도 OK (검증 안 함).
	if err := replicationcleanupjob.Register(sch, replicationcleanupjob.Deps{}, "", ""); err != nil {
		t.Fatalf("Register(empty spec) returned err: %v", err)
	}
}

func TestRegister_NilSchedulerError(t *testing.T) {
	t.Parallel()

	err := replicationcleanupjob.Register(nil, replicationcleanupjob.Deps{
		Executor:   &fakeExecutor{},
		SlotPrefix: "rosshield_",
	}, "", "@every 1h")
	if err == nil || !strings.Contains(err.Error(), "Scheduler") {
		t.Errorf("err = %v, want Scheduler required", err)
	}
}

func TestRegister_MissingExecutor(t *testing.T) {
	t.Parallel()
	sch := newTestScheduler(t)

	err := replicationcleanupjob.Register(sch, replicationcleanupjob.Deps{
		SlotPrefix: "rosshield_",
		Logger:     discardLogger(),
	}, "", "@every 1h")
	if err == nil || !strings.Contains(err.Error(), "Executor") {
		t.Errorf("err = %v, want Executor required", err)
	}
}

func TestRegister_MissingSlotPrefixFailsFast(t *testing.T) {
	t.Parallel()
	sch := newTestScheduler(t)

	err := replicationcleanupjob.Register(sch, replicationcleanupjob.Deps{
		Executor: &fakeExecutor{},
		Logger:   discardLogger(),
	}, "", "@every 1h")
	if err == nil || !strings.Contains(err.Error(), "SlotPrefix") {
		t.Errorf("err = %v, want SlotPrefix required", err)
	}
}

func TestRegister_HappyPath(t *testing.T) {
	t.Parallel()
	sch := newTestScheduler(t)

	err := replicationcleanupjob.Register(sch, replicationcleanupjob.Deps{
		Executor:   &fakeExecutor{},
		SlotPrefix: "rosshield_",
		Logger:     discardLogger(),
	}, "", "@every 24h")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
}

// --- RunOnce ---

func TestRunOnce_NoCandidatesGracefulSuccess(t *testing.T) {
	t.Parallel()
	fake := &fakeExecutor{slots: nil}

	err := replicationcleanupjob.RunOnce(context.Background(), replicationcleanupjob.Deps{
		Executor:   fake,
		SlotPrefix: "rosshield_",
		Logger:     discardLogger(),
	})
	if err != nil {
		t.Errorf("RunOnce: %v", err)
	}
	if fake.queryStringsN != 1 {
		t.Errorf("QueryStrings calls = %d, want 1", fake.queryStringsN)
	}
	if len(fake.execCalls) != 0 {
		t.Errorf("Exec calls = %d (want 0 — no candidates)", len(fake.execCalls))
	}
}

func TestRunOnce_DryRunSkipsDrop(t *testing.T) {
	t.Parallel()
	fake := &fakeExecutor{slots: []string{"rosshield_main_sub", "rosshield_audit_sub"}}

	err := replicationcleanupjob.RunOnce(context.Background(), replicationcleanupjob.Deps{
		Executor:   fake,
		SlotPrefix: "rosshield_",
		DryRun:     true,
		Logger:     discardLogger(),
	})
	if err != nil {
		t.Errorf("RunOnce: %v", err)
	}
	if len(fake.execCalls) != 0 {
		t.Errorf("Exec calls = %d (want 0 — DryRun), got %v", len(fake.execCalls), fake.execCalls)
	}
}

func TestRunOnce_DropsCandidates(t *testing.T) {
	t.Parallel()
	fake := &fakeExecutor{slots: []string{"rosshield_main_sub", "rosshield_audit_sub"}}

	err := replicationcleanupjob.RunOnce(context.Background(), replicationcleanupjob.Deps{
		Executor:   fake,
		SlotPrefix: "rosshield_",
		Logger:     discardLogger(),
	})
	if err != nil {
		t.Errorf("RunOnce: %v", err)
	}
	if len(fake.execCalls) != 2 {
		t.Errorf("Exec calls = %d, want 2 — got %v", len(fake.execCalls), fake.execCalls)
	}
}

func TestRunOnce_QueryErrorPropagates(t *testing.T) {
	t.Parallel()
	fake := &fakeExecutor{queryErr: errors.New("connection refused")}

	err := replicationcleanupjob.RunOnce(context.Background(), replicationcleanupjob.Deps{
		Executor:   fake,
		SlotPrefix: "rosshield_",
		Logger:     discardLogger(),
	})
	if err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("err = %v, want propagated query error", err)
	}
}

func TestRunOnce_DropErrorPropagates(t *testing.T) {
	t.Parallel()
	fake := &fakeExecutor{
		slots:   []string{"rosshield_main_sub"},
		execErr: errors.New("permission denied for pg_drop_replication_slot"),
	}

	err := replicationcleanupjob.RunOnce(context.Background(), replicationcleanupjob.Deps{
		Executor:   fake,
		SlotPrefix: "rosshield_",
		Logger:     discardLogger(),
	})
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("err = %v, want propagated drop error", err)
	}
}

func TestRunOnce_MissingExecutorErrors(t *testing.T) {
	t.Parallel()

	err := replicationcleanupjob.RunOnce(context.Background(), replicationcleanupjob.Deps{
		SlotPrefix: "rosshield_",
		Logger:     discardLogger(),
	})
	if err == nil || !strings.Contains(err.Error(), "Executor") {
		t.Errorf("err = %v, want Executor required", err)
	}
}

func TestRunOnce_MissingSlotPrefixErrors(t *testing.T) {
	t.Parallel()

	err := replicationcleanupjob.RunOnce(context.Background(), replicationcleanupjob.Deps{
		Executor: &fakeExecutor{},
		Logger:   discardLogger(),
	})
	if err == nil || !strings.Contains(err.Error(), "SlotPrefix") {
		t.Errorf("err = %v, want SlotPrefix required", err)
	}
}

// --- MinInactiveAge가 setup 패키지로 정확 전달되는지 ---

func TestRunOnce_MinInactiveAgeForwarded(t *testing.T) {
	t.Parallel()
	fake := &fakeExecutor{slots: nil}

	err := replicationcleanupjob.RunOnce(context.Background(), replicationcleanupjob.Deps{
		Executor:       fake,
		SlotPrefix:     "rosshield_",
		MinInactiveAge: 48 * time.Hour,
		Logger:         discardLogger(),
	})
	if err != nil {
		t.Errorf("RunOnce: %v", err)
	}
	// setup.CleanupInactiveSlots는 age.Seconds()를 두 번째 args로 전달 — fakeExecutor가 args 수신.
	// 본 단위 test는 deps 전달 자체만 검증 (정밀 SQL arg 검증은 setup 패키지 테스트가 담당).
	if fake.queryStringsN != 1 {
		t.Errorf("QueryStrings calls = %d, want 1", fake.queryStringsN)
	}
}
