package insight_test

import (
	"strings"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/domain/insight"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// peerFixture는 fleet 내 robotPasses (robotID → (passCount, failCount)) 마지막 session 결과를 만듭니다.
//
// 각 robot은 별도 session 1개씩 가져 latestByRobot이 유일하게 결정되도록 합니다.
func peerFixture(fleetID string, robotPasses map[string][2]int) ([]insight.ScanSessionView, map[string][]insight.ScanResultView) {
	var sessions []insight.ScanSessionView
	resultsBySession := make(map[string][]insight.ScanResultView)
	day := 20
	for robotID, pf := range robotPasses {
		passes, fails := pf[0], pf[1]
		sid := "ss_p_" + robotID
		sessions = append(sessions, insight.ScanSessionView{
			ID:          sid,
			TenantID:    storage.TenantID("tn_peer"),
			FleetID:     fleetID,
			Status:      "completed",
			CompletedAt: ts(2026, 4, day),
		})
		day++
		var rs []insight.ScanResultView
		for i := 0; i < passes; i++ {
			rs = append(rs, insight.ScanResultView{
				ID: sid + "_p" + string(rune('a'+i)), SessionID: sid, RobotID: robotID,
				CheckID: "ck_p" + string(rune('a'+i)), Outcome: "pass", DurationMs: 100,
			})
		}
		for i := 0; i < fails; i++ {
			rs = append(rs, insight.ScanResultView{
				ID: sid + "_f" + string(rune('a'+i)), SessionID: sid, RobotID: robotID,
				CheckID: "ck_f" + string(rune('a'+i)), Outcome: "fail", DurationMs: 100,
			})
		}
		resultsBySession[sid] = rs
	}
	return sessions, resultsBySession
}

func TestPeerDetectsRobotBelowFleetAvg(t *testing.T) {
	t.Parallel()
	// 4 robots: 3 robots = 90% pass, 1 robot = 50% pass — 50% < μ - σ.
	robots := map[string][2]int{
		"ro_A": {9, 1}, // 90%
		"ro_B": {9, 1}, // 90%
		"ro_C": {9, 1}, // 90%
		"ro_D": {5, 5}, // 50% — outlier
	}
	sessions, results := peerFixture("fl_X", robots)
	out, err := insight.DetectPeer(time.Now(), "fl_X", sessions, results)
	if err != nil {
		t.Fatalf("DetectPeer: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("len = %d, want 1", len(out))
	}
	in := out[0]
	if in.Kind != insight.KindPeer {
		t.Errorf("Kind = %s, want peer", in.Kind)
	}
	if in.Scope.RobotID != "ro_D" {
		t.Errorf("Scope.RobotID = %s, want ro_D", in.Scope.RobotID)
	}
	if in.Scope.FleetID != "fl_X" {
		t.Errorf("Scope.FleetID = %s, want fl_X", in.Scope.FleetID)
	}
	if in.Severity != insight.SeverityMedium {
		t.Errorf("Severity = %s, want medium", in.Severity)
	}
}

func TestPeerIgnoresAboveAvg(t *testing.T) {
	t.Parallel()
	// 4 robots: 3 robots = 50% pass, 1 robot = 90% pass — 위쪽은 outlier 아님.
	robots := map[string][2]int{
		"ro_A": {5, 5}, // 50%
		"ro_B": {5, 5}, // 50%
		"ro_C": {5, 5}, // 50%
		"ro_D": {9, 1}, // 90% — 위쪽 outlier
	}
	sessions, results := peerFixture("fl_Y", robots)
	out, err := insight.DetectPeer(time.Now(), "fl_Y", sessions, results)
	if err != nil {
		t.Fatalf("DetectPeer: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("len = %d, want 0 (above avg ignored)", len(out))
	}
}

func TestPeerHandlesSingleRobotFleet(t *testing.T) {
	t.Parallel()
	robots := map[string][2]int{
		"ro_solo": {5, 5},
	}
	sessions, results := peerFixture("fl_S", robots)
	out, err := insight.DetectPeer(time.Now(), "fl_S", sessions, results)
	if err != nil {
		t.Fatalf("DetectPeer: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("len = %d, want 0 (single robot — σ undefined)", len(out))
	}
}

func TestPeerHandlesIdenticalPassRatios(t *testing.T) {
	t.Parallel()
	// σ=0 edge — 모든 robot pass 비율 동일.
	robots := map[string][2]int{
		"ro_A": {5, 5},
		"ro_B": {5, 5},
		"ro_C": {5, 5},
	}
	sessions, results := peerFixture("fl_E", robots)
	out, err := insight.DetectPeer(time.Now(), "fl_E", sessions, results)
	if err != nil {
		t.Fatalf("DetectPeer: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("len = %d, want 0 (σ=0)", len(out))
	}
}

func TestPeerReasoningIncludesAvgAndSigma(t *testing.T) {
	t.Parallel()
	robots := map[string][2]int{
		"ro_A": {9, 1},
		"ro_B": {9, 1},
		"ro_C": {9, 1},
		"ro_D": {5, 5},
	}
	sessions, results := peerFixture("fl_R", robots)
	out, err := insight.DetectPeer(time.Now(), "fl_R", sessions, results)
	if err != nil {
		t.Fatalf("DetectPeer: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("len = %d, want 1", len(out))
	}
	r := out[0].Reasoning
	for _, want := range []string{"평균", "σ", "임계"} {
		if !strings.Contains(r, want) {
			t.Errorf("Reasoning = %q, want %q", r, want)
		}
	}
	if len(out[0].RulesApplied) != 1 || out[0].RulesApplied[0] != "peer_fleet_avg_1sigma" {
		t.Errorf("RulesApplied = %v, want [peer_fleet_avg_1sigma]", out[0].RulesApplied)
	}
}
