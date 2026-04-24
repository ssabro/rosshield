package benchmark_test

import (
	"errors"
	"testing"

	"github.com/ssabro/rosshield/internal/domain/benchmark"
)

// E4.T7 본체 — 정상 전이 + 불법 전이 거부.
func TestPackLifecycleFSM(t *testing.T) {
	t.Parallel()

	allowed := []struct {
		from, to benchmark.State
	}{
		{benchmark.StateInstalled, benchmark.StateStaged},
		{benchmark.StateStaged, benchmark.StateActive},
		{benchmark.StateActive, benchmark.StateInactive},
		{benchmark.StateInactive, benchmark.StateActive},
		{benchmark.StateActive, benchmark.StateArchived},
		{benchmark.StateInactive, benchmark.StateArchived},
		{benchmark.StateInstalled, benchmark.StateArchived},
		{benchmark.StateStaged, benchmark.StateInstalled},
		{benchmark.StateStaged, benchmark.StateArchived},
		{benchmark.StateArchived, benchmark.StateRemoved},
	}
	for _, tc := range allowed {
		t.Run("allow_"+string(tc.from)+"_to_"+string(tc.to), func(t *testing.T) {
			if !benchmark.CanTransition(tc.from, tc.to) {
				t.Errorf("CanTransition(%s, %s) = false, want true", tc.from, tc.to)
			}
			got, err := benchmark.Transition(tc.from, tc.to)
			if err != nil {
				t.Errorf("Transition(%s, %s): %v", tc.from, tc.to, err)
			}
			if got != tc.to {
				t.Errorf("Transition returned %s, want %s", got, tc.to)
			}
		})
	}

	// 불법 전이 — 직접 Active 진입, Removed에서 다른 상태로, self-transition 등.
	illegal := []struct {
		from, to benchmark.State
	}{
		{benchmark.StateInstalled, benchmark.StateActive},   // Staged 거치지 않음
		{benchmark.StateInstalled, benchmark.StateInactive}, // 직접 비활성 X
		{benchmark.StateInstalled, benchmark.StateRemoved},  // 직접 제거 X
		{benchmark.StateActive, benchmark.StateRemoved},     // Archived 거치지 않음
		{benchmark.StateRemoved, benchmark.StateInstalled},  // 종착에서 복귀 불가
		{benchmark.StateRemoved, benchmark.StateActive},     // 종착에서 복귀 불가
		{benchmark.StateArchived, benchmark.StateActive},    // Archive 후 복귀 불가
		{benchmark.StateActive, benchmark.StateInstalled},   // 역방향
		{benchmark.StateActive, benchmark.StateActive},      // self-transition
	}
	for _, tc := range illegal {
		t.Run("deny_"+string(tc.from)+"_to_"+string(tc.to), func(t *testing.T) {
			if benchmark.CanTransition(tc.from, tc.to) {
				t.Errorf("CanTransition(%s, %s) = true, want false", tc.from, tc.to)
			}
			_, err := benchmark.Transition(tc.from, tc.to)
			if !errors.Is(err, benchmark.ErrIllegalTransition) {
				t.Errorf("Transition(%s, %s): err = %v, want ErrIllegalTransition", tc.from, tc.to, err)
			}
		})
	}
}

// IsTerminal: Removed만 종착.
func TestIsTerminalOnlyRemoved(t *testing.T) {
	t.Parallel()

	for _, s := range benchmark.AllStates() {
		got := benchmark.IsTerminal(s)
		want := s == benchmark.StateRemoved
		if got != want {
			t.Errorf("IsTerminal(%s) = %v, want %v", s, got, want)
		}
	}
}

// AllStates에 6개 상태 모두 포함.
func TestAllStatesContainsAll(t *testing.T) {
	t.Parallel()

	want := map[benchmark.State]bool{
		benchmark.StateInstalled: true,
		benchmark.StateStaged:    true,
		benchmark.StateActive:    true,
		benchmark.StateInactive:  true,
		benchmark.StateArchived:  true,
		benchmark.StateRemoved:   true,
	}
	got := benchmark.AllStates()
	if len(got) != len(want) {
		t.Errorf("AllStates len = %d, want %d", len(got), len(want))
	}
	for _, s := range got {
		if !want[s] {
			t.Errorf("unexpected state %q", s)
		}
		delete(want, s)
	}
	if len(want) != 0 {
		t.Errorf("missing states: %v", want)
	}
}

// AllowedNextStates — Active에서 갈 수 있는 다음 2개(Inactive, Archived).
func TestAllowedNextStatesActive(t *testing.T) {
	t.Parallel()

	next := benchmark.AllowedNextStates(benchmark.StateActive)
	if len(next) != 2 {
		t.Errorf("AllowedNextStates(Active) len = %d, want 2", len(next))
	}
	got := map[benchmark.State]bool{}
	for _, s := range next {
		got[s] = true
	}
	if !got[benchmark.StateInactive] || !got[benchmark.StateArchived] {
		t.Errorf("got = %v, want {Inactive, Archived}", got)
	}
}

// Removed에서는 갈 수 있는 곳이 없음.
func TestAllowedNextStatesRemovedIsEmpty(t *testing.T) {
	t.Parallel()

	next := benchmark.AllowedNextStates(benchmark.StateRemoved)
	if len(next) != 0 {
		t.Errorf("AllowedNextStates(Removed) = %v, want empty", next)
	}
}
