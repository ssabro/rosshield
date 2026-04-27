package scan_test

import (
	"errors"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/domain/scan"
)

// TestSessionFSMValidTransitions는 R5-4·R5-5 FSM이 허용된 전이를 받아주는지 확인합니다.
func TestSessionFSMValidTransitions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name                     string
		from                     scan.SessionStatus
		to                       scan.SessionStatus
		setStarted, setCompleted bool
	}{
		{"pending→running", scan.StatusPending, scan.StatusRunning, true, false},
		{"pending→cancelled (R5-5)", scan.StatusPending, scan.StatusCancelled, false, true},
		{"pending→failed (early)", scan.StatusPending, scan.StatusFailed, false, true},
		{"running→completed", scan.StatusRunning, scan.StatusCompleted, false, true},
		{"running→failed", scan.StatusRunning, scan.StatusFailed, false, true},
		{"running→cancelled", scan.StatusRunning, scan.StatusCancelled, false, true},
	}

	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s := scan.ScanSession{
				ID:        "scan_test",
				Status:    tc.from,
				CreatedAt: now.Add(-time.Hour),
				UpdatedAt: now.Add(-time.Hour),
			}
			next, err := s.TransitionTo(tc.to, now)
			if err != nil {
				t.Fatalf("TransitionTo(%s): %v", tc.to, err)
			}
			if next.Status != tc.to {
				t.Errorf("Status = %s, want %s", next.Status, tc.to)
			}
			if !next.UpdatedAt.Equal(now) {
				t.Errorf("UpdatedAt = %v, want %v", next.UpdatedAt, now)
			}
			if tc.setStarted && (next.StartedAt == nil || !next.StartedAt.Equal(now)) {
				t.Errorf("StartedAt = %v, want %v", next.StartedAt, now)
			}
			if !tc.setStarted && next.StartedAt != nil {
				t.Errorf("StartedAt set unexpectedly: %v", next.StartedAt)
			}
			if tc.setCompleted && (next.CompletedAt == nil || !next.CompletedAt.Equal(now)) {
				t.Errorf("CompletedAt = %v, want %v", next.CompletedAt, now)
			}
			if !tc.setCompleted && next.CompletedAt != nil {
				t.Errorf("CompletedAt set unexpectedly: %v", next.CompletedAt)
			}

			// 원본은 불변(P9).
			if s.Status != tc.from {
				t.Errorf("original Status mutated: %s, want %s", s.Status, tc.from)
			}
			if s.UpdatedAt.Equal(now) {
				t.Errorf("original UpdatedAt mutated")
			}
		})
	}
}

// TestSessionFSMRejectsInvalid는 FSM이 잘못된 전이를 거부하는지 확인합니다.
func TestSessionFSMRejectsInvalid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		from scan.SessionStatus
		to   scan.SessionStatus
	}{
		{"pending→completed (skipping running)", scan.StatusPending, scan.StatusCompleted},
		{"pending→pending (self)", scan.StatusPending, scan.StatusPending},
		{"running→pending (backward)", scan.StatusRunning, scan.StatusPending},
		{"running→running (self)", scan.StatusRunning, scan.StatusRunning},
		{"completed→running (terminal)", scan.StatusCompleted, scan.StatusRunning},
		{"completed→cancelled (terminal)", scan.StatusCompleted, scan.StatusCancelled},
		{"failed→running (terminal)", scan.StatusFailed, scan.StatusRunning},
		{"cancelled→running (terminal)", scan.StatusCancelled, scan.StatusRunning},
		{"unknown source", scan.SessionStatus("bogus"), scan.StatusRunning},
		{"unknown target", scan.StatusPending, scan.SessionStatus("bogus")},
	}

	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s := scan.ScanSession{Status: tc.from, UpdatedAt: now.Add(-time.Hour)}
			_, err := s.TransitionTo(tc.to, now)
			if !errors.Is(err, scan.ErrInvalidTransition) {
				t.Errorf("err = %v, want ErrInvalidTransition", err)
			}
		})
	}
}

// TestSessionFSMPreservesEarlierStartedAt는 running으로 두 번 (불가능하지만 model 레벨)
// 이전 StartedAt이 보존되는지 확인 — running→running은 거부되므로 직접 검증은 어려우니,
// running→cancelled에서 StartedAt이 유지되는지로 대체.
func TestSessionFSMPreservesStartedAt(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2026, 4, 27, 10, 0, 0, 0, time.UTC)
	t1 := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)

	s := scan.ScanSession{
		Status:    scan.StatusRunning,
		StartedAt: &t0,
	}
	next, err := s.TransitionTo(scan.StatusCompleted, t1)
	if err != nil {
		t.Fatalf("TransitionTo: %v", err)
	}
	if next.StartedAt == nil || !next.StartedAt.Equal(t0) {
		t.Errorf("StartedAt = %v, want %v (preserved)", next.StartedAt, t0)
	}
	if next.CompletedAt == nil || !next.CompletedAt.Equal(t1) {
		t.Errorf("CompletedAt = %v, want %v", next.CompletedAt, t1)
	}
}

func TestIsTerminal(t *testing.T) {
	t.Parallel()
	cases := []struct {
		s    scan.SessionStatus
		want bool
	}{
		{scan.StatusPending, false},
		{scan.StatusRunning, false},
		{scan.StatusCompleted, true},
		{scan.StatusFailed, true},
		{scan.StatusCancelled, true},
		{scan.SessionStatus("bogus"), false},
	}
	for _, tc := range cases {
		if got := tc.s.IsTerminal(); got != tc.want {
			t.Errorf("IsTerminal(%s) = %v, want %v", tc.s, got, tc.want)
		}
	}
}

func TestValidateOutcome(t *testing.T) {
	t.Parallel()

	for _, o := range []scan.Outcome{
		scan.OutcomePass, scan.OutcomeFail,
		scan.OutcomeIndeterminate, scan.OutcomeError, scan.OutcomeSkipped,
	} {
		if err := scan.ValidateOutcome(o); err != nil {
			t.Errorf("ValidateOutcome(%s) = %v, want nil", o, err)
		}
	}

	for _, bad := range []scan.Outcome{"", "PASS", "passed", "unknown"} {
		if err := scan.ValidateOutcome(bad); !errors.Is(err, scan.ErrResultInvalidOutcome) {
			t.Errorf("ValidateOutcome(%q) = %v, want ErrResultInvalidOutcome", bad, err)
		}
	}
}

func TestValidateTrigger(t *testing.T) {
	t.Parallel()

	for _, trg := range []scan.SessionTrigger{
		scan.TriggerManual, scan.TriggerSchedule, scan.TriggerEvent,
	} {
		if err := scan.ValidateTrigger(trg); err != nil {
			t.Errorf("ValidateTrigger(%s) = %v, want nil", trg, err)
		}
	}

	for _, bad := range []scan.SessionTrigger{"", "MANUAL", "auto", "unknown"} {
		if err := scan.ValidateTrigger(bad); !errors.Is(err, scan.ErrSessionInvalidTrigger) {
			t.Errorf("ValidateTrigger(%q) = %v, want ErrSessionInvalidTrigger", bad, err)
		}
	}
}
