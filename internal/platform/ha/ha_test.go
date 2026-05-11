package ha_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/platform/ha"
)

// fakeLock은 Lock 인터페이스의 in-memory mock입니다.
//
// 같은 fakeLock 인스턴스를 두 Manager에 공유시키면 advisory lock의 single-holder
// 의미가 시뮬레이션됩니다.
type fakeLock struct {
	mu           sync.Mutex
	heldBy       string // 비어있으면 unlocked
	heldEpoch    int64
	nextEpoch    int64
	heartbeatErr error // non-nil이면 다음 Heartbeat 호출에서 반환

	tryAcquireCalls int
	heartbeatCalls  int
	releaseCalls    int
}

func newFakeLock() *fakeLock {
	return &fakeLock{}
}

func (f *fakeLock) TryAcquire(_ context.Context, leaderID string) (bool, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tryAcquireCalls++
	if f.heldBy != "" && f.heldBy != leaderID {
		return false, 0, nil
	}
	f.nextEpoch++
	f.heldBy = leaderID
	f.heldEpoch = f.nextEpoch
	return true, f.nextEpoch, nil
}

func (f *fakeLock) Heartbeat(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.heartbeatCalls++
	if f.heartbeatErr != nil {
		return f.heartbeatErr
	}
	if f.heldBy == "" {
		return errors.New("not held")
	}
	return nil
}

func (f *fakeLock) Release(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.releaseCalls++
	f.heldBy = ""
	f.heldEpoch = 0
	return nil
}

// killHolder simulates leader crash — conn lost, Heartbeat returns error.
func (f *fakeLock) killHolder(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.heldBy = ""
	f.heldEpoch = 0
	f.heartbeatErr = err
}

// clearHeartbeatErr resets heartbeat error so subsequent ticks succeed once acquired.
func (f *fakeLock) clearHeartbeatErr() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.heartbeatErr = nil
}

func TestManagerStartPromotesToLeaderOnFirstTick(t *testing.T) {
	t.Parallel()

	lock := newFakeLock()
	mgr := ha.NewManager(lock, "host-a:1234", 50*time.Millisecond, nil)

	var promoted atomic.Bool
	mgr.OnLeaderAcquired(func() { promoted.Store(true) })

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	mgr.Start(ctx)

	if !waitFor(t, 500*time.Millisecond, func() bool { return mgr.IsLeader() && promoted.Load() }) {
		t.Fatalf("expected promotion to leader within 500ms, role=%s, promoted=%v", mgr.Role(), promoted.Load())
	}
	if got := mgr.CurrentEpoch(); got != 1 {
		t.Errorf("CurrentEpoch = %d, want 1", got)
	}
	if got := mgr.LeaderID(); got != "host-a:1234" {
		t.Errorf("LeaderID = %q, want %q", got, "host-a:1234")
	}

	if err := mgr.Stop(context.Background()); err != nil {
		t.Errorf("Stop: %v", err)
	}
}

func TestManagerStopReleasesLock(t *testing.T) {
	t.Parallel()

	lock := newFakeLock()
	mgr := ha.NewManager(lock, "host-a:1234", 50*time.Millisecond, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr.Start(ctx)
	if !waitFor(t, 500*time.Millisecond, mgr.IsLeader) {
		t.Fatalf("expected leader within 500ms")
	}

	if err := mgr.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if lock.releaseCalls == 0 {
		t.Errorf("expected at least 1 Release call, got %d", lock.releaseCalls)
	}
}

func TestManagerHeartbeatFailureDemotesToFollower(t *testing.T) {
	t.Parallel()

	lock := newFakeLock()
	mgr := ha.NewManager(lock, "host-a:1234", 50*time.Millisecond, nil)

	var lostCount atomic.Int32
	mgr.OnLeaderLost(func() { lostCount.Add(1) })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	mgr.Start(ctx)
	if !waitFor(t, 500*time.Millisecond, mgr.IsLeader) {
		t.Fatalf("expected leader within 500ms")
	}

	// Simulate leader conn loss.
	lock.killHolder(errors.New("connection refused"))

	if !waitFor(t, 500*time.Millisecond, func() bool {
		return !mgr.IsLeader() && lostCount.Load() >= 1
	}) {
		t.Fatalf("expected demotion within 500ms, role=%s, lostCount=%d", mgr.Role(), lostCount.Load())
	}
	if got := mgr.CurrentEpoch(); got != 0 {
		t.Errorf("CurrentEpoch after demotion = %d, want 0", got)
	}

	// 다음 tick에서 follower가 다시 lock 획득 가능.
	lock.clearHeartbeatErr()
	if !waitFor(t, 500*time.Millisecond, mgr.IsLeader) {
		t.Fatalf("expected re-promotion within 500ms after recovery")
	}
	if got := mgr.CurrentEpoch(); got != 2 {
		t.Errorf("CurrentEpoch after re-promotion = %d, want 2 (incremented)", got)
	}

	_ = mgr.Stop(context.Background())
}

func TestManagerSingleLeaderAcrossInstances(t *testing.T) {
	t.Parallel()

	lock := newFakeLock()

	mgrA := ha.NewManager(lock, "host-a:1111", 50*time.Millisecond, nil)
	mgrB := ha.NewManager(lock, "host-b:2222", 50*time.Millisecond, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	mgrA.Start(ctx)
	// A가 leader 잡을 시간 줌.
	if !waitFor(t, 500*time.Millisecond, mgrA.IsLeader) {
		t.Fatalf("expected A to become leader")
	}

	mgrB.Start(ctx)
	// B는 leader 잡으면 안 됨.
	time.Sleep(300 * time.Millisecond)
	if mgrB.IsLeader() {
		t.Errorf("expected B to remain follower while A is leader")
	}
	if !mgrA.IsLeader() {
		t.Errorf("expected A to remain leader")
	}

	_ = mgrA.Stop(context.Background())
	_ = mgrB.Stop(context.Background())
}

func TestRoleStringValues(t *testing.T) {
	t.Parallel()

	if got := ha.RoleLeader.String(); got != "leader" {
		t.Errorf("RoleLeader.String() = %q, want %q", got, "leader")
	}
	if got := ha.RoleFollower.String(); got != "follower" {
		t.Errorf("RoleFollower.String() = %q, want %q", got, "follower")
	}
	if got := ha.Role(99).String(); got != "unknown" {
		t.Errorf("Role(99).String() = %q, want %q", got, "unknown")
	}
}

func TestErrNotLeaderSentinel(t *testing.T) {
	t.Parallel()
	wrapped := errors.Join(errors.New("higher level"), ha.ErrNotLeader)
	if !errors.Is(wrapped, ha.ErrNotLeader) {
		t.Errorf("errors.Is failed for ErrNotLeader sentinel chain")
	}
}

func waitFor(t *testing.T, max time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(max)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return cond()
}
