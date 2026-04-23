package cronsched_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/platform/scheduler"
	"github.com/ssabro/rosshield/internal/platform/scheduler/cronsched"
)

// 기본 발화 주기 — robfig/cron의 ConstantDelaySchedule이 second-precision으로 truncate하므로
// 1초 미만 스펙(@every 100ms 등)은 의도대로 동작하지 않음. 따라서 모든 발화 테스트는 @every 1s 사용.
const tickSpec = "@every 1s"

// waitForFires는 fires가 want에 도달할 때까지 또는 timeout까지 대기.
func waitForFires(t *testing.T, fires *atomic.Int32, want int32, timeout time.Duration) int32 {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fires.Load() >= want {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fires.Load()
}

func newTestScheduler(t *testing.T) *cronsched.Scheduler {
	t.Helper()
	s := cronsched.New(cronsched.Deps{
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.Close(ctx)
	})
	return s
}

func TestSchedulerFiresAtSpec(t *testing.T) {
	t.Parallel()

	s := newTestScheduler(t)

	fired := atomic.Int32{}
	if err := s.Schedule("test-fires", tickSpec, func(ctx context.Context) error {
		fired.Add(1)
		return nil
	}); err != nil {
		t.Fatalf("Schedule: %v", err)
	}

	got := waitForFires(t, &fired, 2, 3500*time.Millisecond)
	if got < 2 {
		t.Errorf("fired = %d, want ≥ 2", got)
	}
}

func TestSchedulerCancelStopsJob(t *testing.T) {
	t.Parallel()

	s := newTestScheduler(t)

	fired := atomic.Int32{}
	if err := s.Schedule("test-cancel", tickSpec, func(ctx context.Context) error {
		fired.Add(1)
		return nil
	}); err != nil {
		t.Fatalf("Schedule: %v", err)
	}

	got := waitForFires(t, &fired, 2, 3500*time.Millisecond)
	if got < 2 {
		t.Fatalf("expected ≥ 2 fires before cancel, got %d", got)
	}
	beforeCancel := got

	s.Cancel("test-cancel")
	time.Sleep(2200 * time.Millisecond) // cancel 후 2초 + grace, 추가 발화 없어야 함.

	afterCancel := fired.Load()
	if afterCancel > beforeCancel+1 { // 1건 in-flight grace 허용
		t.Errorf("after Cancel, fires increased by %d (before=%d, after=%d) — Cancel did not stop job",
			afterCancel-beforeCancel, beforeCancel, afterCancel)
	}
}

func TestSchedulerScheduleDuplicateID(t *testing.T) {
	t.Parallel()

	s := newTestScheduler(t)
	noop := func(ctx context.Context) error { return nil }

	if err := s.Schedule("dup-id", "@every 1m", noop); err != nil {
		t.Fatalf("first Schedule: %v", err)
	}

	err := s.Schedule("dup-id", "@every 1m", noop)
	if !errors.Is(err, scheduler.ErrJobExists) {
		t.Errorf("err = %v, want ErrJobExists", err)
	}
}

func TestSchedulerInvalidSpec(t *testing.T) {
	t.Parallel()

	s := newTestScheduler(t)
	noop := func(ctx context.Context) error { return nil }

	err := s.Schedule("bad-spec", "this-is-not-a-cron-spec", noop)
	if err == nil {
		t.Error("expected error for invalid spec")
	}
}

func TestSchedulerHandlesJobError(t *testing.T) {
	t.Parallel()

	s := newTestScheduler(t)

	fired := atomic.Int32{}
	if err := s.Schedule("err-job", tickSpec, func(ctx context.Context) error {
		fired.Add(1)
		return errors.New("intentional")
	}); err != nil {
		t.Fatalf("Schedule: %v", err)
	}

	got := waitForFires(t, &fired, 2, 3500*time.Millisecond)
	if got < 2 {
		t.Errorf("fired = %d, want ≥ 2 (error must not stop subsequent runs)", got)
	}
}

func TestSchedulerCancelNonExistentIsNoOp(t *testing.T) {
	t.Parallel()

	s := newTestScheduler(t)
	s.Cancel("never-registered") // panic·error 없이 통과해야 함.
}

func TestSchedulerHandlesJobPanic(t *testing.T) {
	t.Parallel()

	s := newTestScheduler(t)

	fired := atomic.Int32{}
	if err := s.Schedule("panic-job", tickSpec, func(ctx context.Context) error {
		fired.Add(1)
		panic("boom")
	}); err != nil {
		t.Fatalf("Schedule: %v", err)
	}

	got := waitForFires(t, &fired, 2, 3500*time.Millisecond)
	if got < 2 {
		t.Errorf("fired = %d, want ≥ 2 (panic must be recovered, subsequent runs continue)", got)
	}
}
