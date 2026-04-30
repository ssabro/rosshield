package compliance_test

import (
	"math"
	"testing"

	"github.com/ssabro/rosshield/internal/domain/compliance"
)

func TestAggregateAllPassReturnsPass(t *testing.T) {
	t.Parallel()

	controls := []compliance.ControlDefinition{
		{ID: "C1", MappedCheckIDs: []string{"chk1", "chk2"}},
	}
	results := []compliance.ScanResultView{
		{CheckID: "chk1", Outcome: "pass"},
		{CheckID: "chk2", Outcome: "pass"},
	}
	got := compliance.AggregateControlStatuses(controls, results)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Status != compliance.StatusPass {
		t.Errorf("status = %s, want pass", got[0].Status)
	}
	if got[0].PassCount != 2 || got[0].FailCount != 0 {
		t.Errorf("counts pass=%d fail=%d, want 2/0", got[0].PassCount, got[0].FailCount)
	}
}

func TestAggregateAllFailReturnsFail(t *testing.T) {
	t.Parallel()

	controls := []compliance.ControlDefinition{
		{ID: "C1", MappedCheckIDs: []string{"chk1", "chk2"}},
	}
	results := []compliance.ScanResultView{
		{CheckID: "chk1", Outcome: "fail"},
		{CheckID: "chk2", Outcome: "error"}, // error도 fail로 산입.
	}
	got := compliance.AggregateControlStatuses(controls, results)
	if got[0].Status != compliance.StatusFail {
		t.Errorf("status = %s, want fail", got[0].Status)
	}
	if got[0].FailCount != 2 {
		t.Errorf("FailCount = %d, want 2 (fail + error)", got[0].FailCount)
	}
}

func TestAggregateMixedReturnsPartial(t *testing.T) {
	t.Parallel()

	controls := []compliance.ControlDefinition{
		{ID: "C1", MappedCheckIDs: []string{"chk1", "chk2"}},
	}
	results := []compliance.ScanResultView{
		{CheckID: "chk1", Outcome: "pass"},
		{CheckID: "chk2", Outcome: "fail"},
	}
	got := compliance.AggregateControlStatuses(controls, results)
	if got[0].Status != compliance.StatusPartial {
		t.Errorf("status = %s, want partial", got[0].Status)
	}
}

func TestAggregateUnmappedControl(t *testing.T) {
	t.Parallel()

	controls := []compliance.ControlDefinition{
		{ID: "Cu", MappedCheckIDs: nil},                       // 매핑 없음.
		{ID: "Cm", MappedCheckIDs: []string{"missing-check"}}, // 매핑은 있으나 result 없음.
	}
	results := []compliance.ScanResultView{
		{CheckID: "other", Outcome: "pass"},
	}
	got := compliance.AggregateControlStatuses(controls, results)
	if got[0].Status != compliance.StatusUnmapped {
		t.Errorf("Cu status = %s, want unmapped", got[0].Status)
	}
	if got[1].Status != compliance.StatusUnmapped {
		t.Errorf("Cm status = %s, want unmapped (no mapped result)", got[1].Status)
	}
}

func TestAggregateNotApplicableOnly(t *testing.T) {
	t.Parallel()

	controls := []compliance.ControlDefinition{
		{ID: "C1", MappedCheckIDs: []string{"chk1", "chk2"}},
	}
	results := []compliance.ScanResultView{
		{CheckID: "chk1", Outcome: "not_applicable"},
		{CheckID: "chk2", Outcome: "skipped"},
	}
	got := compliance.AggregateControlStatuses(controls, results)
	if got[0].Status != compliance.StatusNotApplicable {
		t.Errorf("status = %s, want not_applicable", got[0].Status)
	}
}

func TestAggregateIndeterminateCountsAsFail(t *testing.T) {
	t.Parallel()

	controls := []compliance.ControlDefinition{
		{ID: "C1", MappedCheckIDs: []string{"chk1"}},
	}
	results := []compliance.ScanResultView{
		{CheckID: "chk1", Outcome: "indeterminate"},
	}
	got := compliance.AggregateControlStatuses(controls, results)
	if got[0].Status != compliance.StatusFail {
		t.Errorf("status = %s, want fail (indeterminate is conservative)", got[0].Status)
	}
}

func TestScoreFullPass100Percent(t *testing.T) {
	t.Parallel()

	statuses := []compliance.ControlStatus{
		{ControlID: "C1", Status: compliance.StatusPass},
		{ControlID: "C2", Status: compliance.StatusPass},
	}
	got := compliance.ScoreFromStatuses(statuses)
	if math.Abs(got-1.0) > 1e-9 {
		t.Errorf("score = %v, want 1.0", got)
	}
}

func TestScoreHalfPassFiftyPercent(t *testing.T) {
	t.Parallel()

	statuses := []compliance.ControlStatus{
		{ControlID: "C1", Status: compliance.StatusPass},
		{ControlID: "C2", Status: compliance.StatusFail},
	}
	got := compliance.ScoreFromStatuses(statuses)
	if math.Abs(got-0.5) > 1e-9 {
		t.Errorf("score = %v, want 0.5", got)
	}
}

func TestScoreIgnoresUnmappedAndNotApplicable(t *testing.T) {
	t.Parallel()

	statuses := []compliance.ControlStatus{
		{ControlID: "C1", Status: compliance.StatusPass},
		{ControlID: "C2", Status: compliance.StatusFail},
		{ControlID: "C3", Status: compliance.StatusUnmapped},
		{ControlID: "C4", Status: compliance.StatusNotApplicable},
	}
	got := compliance.ScoreFromStatuses(statuses)
	// 분모 = 2 (pass + fail), 분자 = 1.0 → 0.5.
	if math.Abs(got-0.5) > 1e-9 {
		t.Errorf("score = %v, want 0.5 (unmapped/NA excluded)", got)
	}
}

func TestScorePartialContributesHalf(t *testing.T) {
	t.Parallel()

	statuses := []compliance.ControlStatus{
		{ControlID: "C1", Status: compliance.StatusPass},
		{ControlID: "C2", Status: compliance.StatusPartial},
	}
	got := compliance.ScoreFromStatuses(statuses)
	// 1.0 + 0.5 = 1.5 / 2 = 0.75.
	if math.Abs(got-0.75) > 1e-9 {
		t.Errorf("score = %v, want 0.75", got)
	}
}

func TestCountStatuses(t *testing.T) {
	t.Parallel()

	statuses := []compliance.ControlStatus{
		{Status: compliance.StatusPass},
		{Status: compliance.StatusPass},
		{Status: compliance.StatusFail},
		{Status: compliance.StatusPartial},
		{Status: compliance.StatusNotApplicable},
		{Status: compliance.StatusUnmapped},
		{Status: compliance.StatusUnmapped},
	}
	pass, fail, partial, na, unmapped := compliance.CountStatuses(statuses)
	if pass != 2 || fail != 1 || partial != 1 || na != 1 || unmapped != 2 {
		t.Errorf("counts = pass=%d fail=%d partial=%d na=%d unmapped=%d, want 2/1/1/1/2",
			pass, fail, partial, na, unmapped)
	}
}
