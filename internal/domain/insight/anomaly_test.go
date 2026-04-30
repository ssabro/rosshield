package insight_test

import (
	"strings"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/domain/insight"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// anomalyFixture는 5 sessions × 1 robot × 1 check 의 duration_ms 시퀀스를 시간순(과거 → 현재)으로 만듭니다.
func anomalyFixture(robotID, checkID string, durations []int64) ([]insight.ScanSessionView, map[string][]insight.ScanResultView) {
	sessions := make([]insight.ScanSessionView, len(durations))
	resultsBySession := make(map[string][]insight.ScanResultView, len(durations))
	for i, d := range durations {
		sid := "ss_a" + string(rune('A'+i))
		sessions[i] = insight.ScanSessionView{
			ID:          sid,
			TenantID:    storage.TenantID("tn_anom"),
			FleetID:     "fl_X",
			Status:      "completed",
			CompletedAt: ts(2026, 4, 20+i),
		}
		resultsBySession[sid] = []insight.ScanResultView{{
			ID:         "scr_" + sid,
			SessionID:  sid,
			RobotID:    robotID,
			CheckID:    checkID,
			Outcome:    "pass",
			DurationMs: d,
		}}
	}
	return sessions, resultsBySession
}

func TestAnomalyDetectsHighOutlier(t *testing.T) {
	t.Parallel()
	// base = [100,110,120,130], last = 9999 → high outlier.
	sessions, results := anomalyFixture("ro_H", "ck_H", []int64{100, 110, 120, 130, 9999})
	out, err := insight.DetectAnomaly(time.Now(), sessions, results)
	if err != nil {
		t.Fatalf("DetectAnomaly: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("len = %d, want 1", len(out))
	}
	in := out[0]
	if in.Kind != insight.KindAnomaly {
		t.Errorf("Kind = %s, want anomaly", in.Kind)
	}
	if in.Severity != insight.SeverityMedium {
		t.Errorf("Severity = %s, want medium", in.Severity)
	}
	if !strings.Contains(in.Reasoning, "9999ms") {
		t.Errorf("Reasoning = %q, want 9999ms mention", in.Reasoning)
	}
	if !strings.Contains(in.Summary, "high") {
		t.Errorf("Summary = %q, want direction high", in.Summary)
	}
}

func TestAnomalyDetectsLowOutlier(t *testing.T) {
	t.Parallel()
	// base = [1000,1100,1200,1300], last = 1 → low outlier.
	sessions, results := anomalyFixture("ro_L", "ck_L", []int64{1000, 1100, 1200, 1300, 1})
	out, err := insight.DetectAnomaly(time.Now(), sessions, results)
	if err != nil {
		t.Fatalf("DetectAnomaly: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("len = %d, want 1", len(out))
	}
	if !strings.Contains(out[0].Summary, "low") {
		t.Errorf("Summary = %q, want direction low", out[0].Summary)
	}
}

func TestAnomalyIgnoresWithinIQR(t *testing.T) {
	t.Parallel()
	// base = [100,110,120,130], last = 115 → IQR 안.
	sessions, results := anomalyFixture("ro_W", "ck_W", []int64{100, 110, 120, 130, 115})
	out, err := insight.DetectAnomaly(time.Now(), sessions, results)
	if err != nil {
		t.Fatalf("DetectAnomaly: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("len = %d, want 0 (within IQR)", len(out))
	}
}

func TestAnomalyHandlesIdenticalDurations(t *testing.T) {
	t.Parallel()
	// IQR=0 edge — 모든 duration 동일.
	sessions, results := anomalyFixture("ro_E", "ck_E", []int64{500, 500, 500, 500, 500})
	out, err := insight.DetectAnomaly(time.Now(), sessions, results)
	if err != nil {
		t.Fatalf("DetectAnomaly: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("len = %d, want 0 (IQR=0)", len(out))
	}

	// IQR=0 + 다른 last 값 — outlier 없는 것으로 처리해야 함 (보수적).
	sessions2, results2 := anomalyFixture("ro_E2", "ck_E2", []int64{500, 500, 500, 500, 9999})
	out2, err := insight.DetectAnomaly(time.Now(), sessions2, results2)
	if err != nil {
		t.Fatalf("DetectAnomaly: %v", err)
	}
	if len(out2) != 0 {
		t.Errorf("len = %d, want 0 (IQR=0 base)", len(out2))
	}
}

func TestAnomalyConfidenceIsLessThanOne(t *testing.T) {
	t.Parallel()
	sessions, results := anomalyFixture("ro_C", "ck_C", []int64{100, 110, 120, 130, 9999})
	out, err := insight.DetectAnomaly(time.Now(), sessions, results)
	if err != nil {
		t.Fatalf("DetectAnomaly: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("len = %d, want 1", len(out))
	}
	if out[0].Confidence >= 1.0 {
		t.Errorf("Confidence = %f, want < 1.0 (statistical fluctuation)", out[0].Confidence)
	}
	if out[0].Confidence <= 0.0 {
		t.Errorf("Confidence = %f, want > 0.0", out[0].Confidence)
	}
}
