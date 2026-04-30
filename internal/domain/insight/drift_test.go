package insight_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/domain/insight"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

func ts(year int, month time.Month, day int) *time.Time {
	t := time.Date(year, month, day, 12, 0, 0, 0, time.UTC)
	return &t
}

// driftFixture는 5 sessions × 1 robot × 1 check 의 결정론적 시나리오를 만듭니다.
// outcomes[i]는 session[i]의 outcome을 의미하며 sessions은 시간순(과거 → 현재).
func driftFixture(robotID, checkID string, outcomes []string) ([]insight.ScanSessionView, map[string][]insight.ScanResultView) {
	sessions := make([]insight.ScanSessionView, len(outcomes))
	resultsBySession := make(map[string][]insight.ScanResultView, len(outcomes))
	for i, o := range outcomes {
		sid := "ss_" + string(rune('A'+i))
		sessions[i] = insight.ScanSessionView{
			ID:          sid,
			TenantID:    storage.TenantID("tn_drift"),
			FleetID:     "fl_X",
			Status:      "completed",
			CompletedAt: ts(2026, 4, 20+i),
		}
		resultsBySession[sid] = []insight.ScanResultView{{
			ID:         "scr_" + sid,
			SessionID:  sid,
			RobotID:    robotID,
			CheckID:    checkID,
			Outcome:    o,
			DurationMs: 100,
		}}
	}
	return sessions, resultsBySession
}

func TestDriftDetectsPassToFail(t *testing.T) {
	t.Parallel()
	sessions, results := driftFixture("ro_1", "ck_1", []string{"pass", "pass", "pass", "pass", "fail"})
	out, err := insight.DetectDrift(time.Now(), sessions, results)
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("len = %d, want 1", len(out))
	}
	in := out[0]
	if in.Kind != insight.KindDrift {
		t.Errorf("Kind = %s, want drift", in.Kind)
	}
	if in.Severity != insight.SeverityHigh {
		t.Errorf("Severity = %s, want high (pass→fail)", in.Severity)
	}
	if in.Scope.RobotID != "ro_1" || in.Scope.CheckID != "ck_1" {
		t.Errorf("Scope = %+v, want ro_1/ck_1", in.Scope)
	}
	if in.Confidence != 1.0 {
		t.Errorf("Confidence = %f, want 1.0", in.Confidence)
	}
	if in.ProducedBy != insight.ProducedByRules {
		t.Errorf("ProducedBy = %s, want rules", in.ProducedBy)
	}
	if len(in.RulesApplied) != 1 || in.RulesApplied[0] != "drift_window_5" {
		t.Errorf("RulesApplied = %v, want [drift_window_5]", in.RulesApplied)
	}
}

func TestDriftDetectsFailToPass(t *testing.T) {
	t.Parallel()
	sessions, results := driftFixture("ro_2", "ck_2", []string{"fail", "fail", "fail", "fail", "pass"})
	out, err := insight.DetectDrift(time.Now(), sessions, results)
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("len = %d, want 1", len(out))
	}
	if out[0].Severity != insight.SeverityInfo {
		t.Errorf("Severity = %s, want info (fail→pass)", out[0].Severity)
	}
}

func TestDriftIgnoresStableResults(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		outcomes []string
	}{
		{"all pass", []string{"pass", "pass", "pass", "pass", "pass"}},
		{"all fail", []string{"fail", "fail", "fail", "fail", "fail"}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			sessions, results := driftFixture("ro_S", "ck_S", tc.outcomes)
			out, err := insight.DetectDrift(time.Now(), sessions, results)
			if err != nil {
				t.Fatalf("DetectDrift: %v", err)
			}
			if len(out) != 0 {
				t.Errorf("len = %d, want 0 (stable)", len(out))
			}
		})
	}
}

func TestDriftRequiresMinimumHistory(t *testing.T) {
	t.Parallel()
	// 4 sessions만 — N=5 미달.
	sessions, results := driftFixture("ro_M", "ck_M", []string{"pass", "pass", "pass", "fail"})
	_, err := insight.DetectDrift(time.Now(), sessions, results)
	if !errors.Is(err, insight.ErrInsufficientHistory) {
		t.Errorf("err = %v, want ErrInsufficientHistory", err)
	}
}

func TestDriftReasoningExplainsTransition(t *testing.T) {
	t.Parallel()
	sessions, results := driftFixture("ro_R", "ck_R", []string{"pass", "pass", "pass", "pass", "fail"})
	out, err := insight.DetectDrift(time.Now(), sessions, results)
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("len = %d, want 1", len(out))
	}
	r := out[0].Reasoning
	if !strings.Contains(r, "fail=1") {
		t.Errorf("Reasoning = %q, want fail=1 mention", r)
	}
	if !strings.Contains(r, "마지막 outcome=fail") {
		t.Errorf("Reasoning = %q, want last outcome mention", r)
	}
	if !strings.Contains(out[0].Summary, "pass → fail") {
		t.Errorf("Summary = %q, want 'pass → fail'", out[0].Summary)
	}
}

// TestDriftIgnoresIndeterminateAsNoise는 noise outcome이 끼어든 경우 medium severity로 처리됨을 검증.
func TestDriftIgnoresIndeterminateAsNoise(t *testing.T) {
	t.Parallel()
	// indeterminate → fail = noise 포함 → medium.
	sessions, results := driftFixture("ro_N", "ck_N", []string{"pass", "pass", "pass", "indeterminate", "fail"})
	out, err := insight.DetectDrift(time.Now(), sessions, results)
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("len = %d, want 1", len(out))
	}
	if out[0].Severity != insight.SeverityMedium {
		t.Errorf("Severity = %s, want medium (noise involved)", out[0].Severity)
	}
}
