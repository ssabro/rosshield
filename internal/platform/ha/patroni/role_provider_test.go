package patroni

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// --- helpers ---

func discardLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

// newFakePatroni는 customer-provided clusterResponse를 반환하는 httptest server를 만듭니다.
//
// resp가 nil이면 500 반환 — error path 검증.
func newFakePatroni(t *testing.T, resp *clusterResponse) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var callCount atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		if r.URL.Path != "/cluster" {
			http.NotFound(w, r)
			return
		}
		if resp == nil {
			http.Error(w, "internal", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv, &callCount
}

// --- New validation ---

func TestNew_RequiresPatroniURL(t *testing.T) {
	t.Parallel()
	_, err := New(Deps{LocalHostname: "pod-0"})
	if err == nil || !strings.Contains(err.Error(), "PatroniURL required") {
		t.Errorf("err = %v, want PatroniURL required", err)
	}
}

func TestNew_RequiresLocalHostname(t *testing.T) {
	t.Parallel()
	_, err := New(Deps{PatroniURL: "http://patroni:8008"})
	if err == nil || !strings.Contains(err.Error(), "LocalHostname required") {
		t.Errorf("err = %v, want LocalHostname required", err)
	}
}

func TestNew_AppliesDefaults(t *testing.T) {
	t.Parallel()
	rp, err := New(Deps{
		PatroniURL:    "http://patroni:8008",
		LocalHostname: "pod-0",
		Logger:        discardLogger(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if rp.deps.PollInterval != DefaultPollInterval {
		t.Errorf("PollInterval = %v, want %v", rp.deps.PollInterval, DefaultPollInterval)
	}
	if rp.deps.RequestTimeout != DefaultRequestTimeout {
		t.Errorf("RequestTimeout = %v, want %v", rp.deps.RequestTimeout, DefaultRequestTimeout)
	}
	if rp.deps.HTTPClient == nil {
		t.Error("HTTPClient should fall back to default")
	}
}

func TestNew_TrimsTrailingSlash(t *testing.T) {
	t.Parallel()
	rp, err := New(Deps{
		PatroniURL:    "http://patroni:8008/",
		LocalHostname: "pod-0",
		Logger:        discardLogger(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if rp.url != "http://patroni:8008" {
		t.Errorf("url = %q, want trailing slash trimmed", rp.url)
	}
}

// --- pollOnce ---

func TestPollOnce_LeaderMatchesLocalHostname(t *testing.T) {
	t.Parallel()
	srv, _ := newFakePatroni(t, &clusterResponse{
		Leader:   "pod-0",
		Timeline: 42,
		Members: []memberInfo{
			{Name: "pod-0", Role: "master", State: "running"},
			{Name: "pod-1", Role: "replica", State: "streaming"},
		},
	})

	rp, err := New(Deps{
		PatroniURL:    srv.URL,
		LocalHostname: "pod-0",
		Logger:        discardLogger(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rp.pollOnce(context.Background())

	if !rp.IsLeader() {
		t.Error("IsLeader = false, want true (pod-0 = leader)")
	}
	if rp.CurrentEpoch() != 42 {
		t.Errorf("CurrentEpoch = %d, want 42", rp.CurrentEpoch())
	}
}

func TestPollOnce_FollowerWhenLeaderDiffers(t *testing.T) {
	t.Parallel()
	srv, _ := newFakePatroni(t, &clusterResponse{
		Leader:   "pod-0",
		Timeline: 10,
	})

	rp, err := New(Deps{
		PatroniURL:    srv.URL,
		LocalHostname: "pod-1", // pod-0이 leader라 본 노드는 follower
		Logger:        discardLogger(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rp.pollOnce(context.Background())

	if rp.IsLeader() {
		t.Error("IsLeader = true, want false (pod-1 ≠ leader pod-0)")
	}
	if rp.CurrentEpoch() != 10 {
		t.Errorf("CurrentEpoch = %d, want 10 (epoch는 leader 무관 추적)", rp.CurrentEpoch())
	}
}

func TestPollOnce_FallbackToMembersMasterRole(t *testing.T) {
	t.Parallel()
	// Leader field 없이 members[].role=="master"로 식별 (일부 Patroni 버전 호환)
	srv, _ := newFakePatroni(t, &clusterResponse{
		Timeline: 5,
		Members: []memberInfo{
			{Name: "pod-1", Role: "replica", State: "streaming"},
			{Name: "pod-2", Role: "master", State: "running"},
		},
	})

	rp, err := New(Deps{
		PatroniURL:    srv.URL,
		LocalHostname: "pod-2",
		Logger:        discardLogger(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rp.pollOnce(context.Background())

	if !rp.IsLeader() {
		t.Error("IsLeader = false, want true (pod-2 has role=master)")
	}
}

func TestPollOnce_GracefulOnHTTPError(t *testing.T) {
	t.Parallel()
	// 500 응답 — atomic 값 변경 안 함
	srv, _ := newFakePatroni(t, nil)

	rp, err := New(Deps{
		PatroniURL:    srv.URL,
		LocalHostname: "pod-0",
		Logger:        discardLogger(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rp.leader.Store(true)
	rp.epoch.Store(99)
	rp.pollOnce(context.Background())

	// 500이라 atomic 직전 값 유지
	if !rp.IsLeader() {
		t.Error("IsLeader changed after HTTP error — should preserve last known state")
	}
	if rp.CurrentEpoch() != 99 {
		t.Errorf("CurrentEpoch changed after HTTP error — should preserve last known state, got %d", rp.CurrentEpoch())
	}
}

func TestPollOnce_GracefulOnNetworkError(t *testing.T) {
	t.Parallel()
	// 존재하지 않는 endpoint
	rp, err := New(Deps{
		PatroniURL:     "http://patroni-unreachable.invalid:8008",
		LocalHostname:  "pod-0",
		RequestTimeout: 100 * time.Millisecond,
		Logger:         discardLogger(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// panic 안 함 + atomic 직전 값(default false/0) 유지
	rp.pollOnce(context.Background())

	if rp.IsLeader() {
		t.Error("IsLeader = true on unreachable URL (should remain default false)")
	}
}

// --- Start / Close lifecycle ---

func TestStart_CleansUpOnContextCancel(t *testing.T) {
	t.Parallel()
	srv, callCount := newFakePatroni(t, &clusterResponse{
		Leader:   "pod-0",
		Timeline: 1,
	})

	rp, err := New(Deps{
		PatroniURL:    srv.URL,
		LocalHostname: "pod-0",
		PollInterval:  50 * time.Millisecond,
		Logger:        discardLogger(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	rp.Start(ctx)

	// 200ms 동안 collector 동작 — 최소 3 poll (immediate + 2 ticker)
	time.Sleep(200 * time.Millisecond)
	cancel()

	// graceful shutdown 대기
	done := make(chan struct{})
	go func() {
		rp.Close()
		close(done)
	}()
	select {
	case <-done:
		// OK
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not return within 2s after context cancel")
	}

	calls := callCount.Load()
	if calls < 1 {
		t.Errorf("expected >=1 poll calls, got %d", calls)
	}
}

// --- resolveLeader ---

func TestResolveLeader_PrefersExplicitField(t *testing.T) {
	got := resolveLeader(clusterResponse{
		Leader: "explicit-leader",
		Members: []memberInfo{
			{Name: "pod-0", Role: "master"},
		},
	})
	if got != "explicit-leader" {
		t.Errorf("got %q, want explicit-leader", got)
	}
}

func TestResolveLeader_FallbackToMembersMaster(t *testing.T) {
	got := resolveLeader(clusterResponse{
		Members: []memberInfo{
			{Name: "pod-0", Role: "replica"},
			{Name: "pod-1", Role: "MASTER"}, // case-insensitive
		},
	})
	if got != "pod-1" {
		t.Errorf("got %q, want pod-1", got)
	}
}

func TestResolveLeader_FallbackToMembersPrimary(t *testing.T) {
	got := resolveLeader(clusterResponse{
		Members: []memberInfo{
			{Name: "pod-2", Role: "primary"},
		},
	})
	if got != "pod-2" {
		t.Errorf("got %q, want pod-2 (primary alias for master)", got)
	}
}

func TestResolveLeader_EmptyWhenNoLeader(t *testing.T) {
	got := resolveLeader(clusterResponse{
		Members: []memberInfo{
			{Name: "pod-0", Role: "replica"},
		},
	})
	if got != "" {
		t.Errorf("got %q, want empty (no master found)", got)
	}
}
