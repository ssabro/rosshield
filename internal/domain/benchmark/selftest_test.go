package benchmark_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ssabro/rosshield/internal/domain/benchmark"
)

const validSelfTestYAML = `apiVersion: rosshield.io/v1
kind: SelfTest
metadata:
  checkId: CIS-1.1.1.1
spec:
  cases:
    - name: "passes when stdout empty"
      input:
        stdout: ""
      expectedOutcome: PASS
    - name: "fails when stdout has content"
      input:
        stdout: "cramfs 12345 0"
      expectedOutcome: FAIL
`

func sampleCheck() benchmark.Check {
	return benchmark.Check{
		CheckID:        "CIS-1.1.1.1",
		Title:          "Disable cramfs",
		Severity:       benchmark.SeverityHigh,
		EvaluationRule: json.RawMessage(`{"op":"empty"}`),
	}
}

// E4.T6 본체 — fixture pass + fail 모두 정확히 판정.
func TestSelfTestFixturePassAndFail(t *testing.T) {
	t.Parallel()

	res, err := benchmark.RunCheckSelfTest(sampleCheck(), []byte(validSelfTestYAML))
	if err != nil {
		t.Fatalf("RunCheckSelfTest: %v", err)
	}

	if res.Total != 2 {
		t.Errorf("Total = %d, want 2", res.Total)
	}
	if res.Passed != 2 {
		t.Errorf("Passed = %d, want 2 (both cases pass — empty case PASS, content case FAIL — expected outcomes match)", res.Passed)
	}
	if len(res.Failures) != 0 {
		t.Errorf("Failures = %v, want none", res.Failures)
	}
	if !res.AllPassed() {
		t.Error("AllPassed() = false, want true")
	}
}

// fixture가 없으면 Degraded.
func TestSelfTestDegradedWhenFixtureMissing(t *testing.T) {
	t.Parallel()

	res, err := benchmark.RunCheckSelfTest(sampleCheck(), nil)
	if err != nil {
		t.Fatalf("RunCheckSelfTest: %v", err)
	}
	if !res.Degraded {
		t.Error("Degraded = false, want true")
	}
	if res.AllPassed() {
		t.Error("AllPassed() should be false when degraded")
	}
}

// expectedOutcome이 안 맞으면 Failure 기록.
func TestSelfTestRecordsMismatch(t *testing.T) {
	t.Parallel()

	// 둘 다 PASS 기대로 잘못된 fixture (실제로는 두 번째가 FAIL이어야 함).
	bad := strings.Replace(validSelfTestYAML, "expectedOutcome: FAIL", "expectedOutcome: PASS", 1)
	res, err := benchmark.RunCheckSelfTest(sampleCheck(), []byte(bad))
	if err != nil {
		t.Fatalf("RunCheckSelfTest: %v", err)
	}
	if res.Passed != 1 {
		t.Errorf("Passed = %d, want 1", res.Passed)
	}
	if len(res.Failures) != 1 {
		t.Fatalf("Failures = %d, want 1", len(res.Failures))
	}
	if res.Failures[0].Got != benchmark.StatusFail || res.Failures[0].Want != benchmark.StatusPass {
		t.Errorf("failure = %+v, want Got=FAIL Want=PASS", res.Failures[0])
	}
	if res.AllPassed() {
		t.Error("AllPassed() should be false with failures")
	}
}

// metadata.checkId가 check.CheckID와 다르면 거부.
func TestSelfTestRejectsCheckIDMismatch(t *testing.T) {
	t.Parallel()

	bad := strings.Replace(validSelfTestYAML, "checkId: CIS-1.1.1.1", "checkId: CIS-OTHER", 1)
	_, err := benchmark.RunCheckSelfTest(sampleCheck(), []byte(bad))
	if !errors.Is(err, benchmark.ErrSchemaViolation) {
		t.Errorf("err = %v, want ErrSchemaViolation", err)
	}
}

// expectedOutcome이 PASS/FAIL/INDETERMINATE가 아니면 거부.
func TestSelfTestRejectsInvalidExpectedOutcome(t *testing.T) {
	t.Parallel()

	bad := strings.Replace(validSelfTestYAML, "expectedOutcome: FAIL", "expectedOutcome: WEIRD", 1)
	_, err := benchmark.RunCheckSelfTest(sampleCheck(), []byte(bad))
	if !errors.Is(err, benchmark.ErrSchemaViolation) {
		t.Errorf("err = %v, want ErrSchemaViolation", err)
	}
}

// EvaluationRule이 비어 있으면 ErrEmptyEvalRule.
func TestSelfTestEmptyEvalRule(t *testing.T) {
	t.Parallel()

	check := sampleCheck()
	check.EvaluationRule = nil
	_, err := benchmark.RunCheckSelfTest(check, []byte(validSelfTestYAML))
	if !errors.Is(err, benchmark.ErrEmptyEvalRule) {
		t.Errorf("err = %v, want ErrEmptyEvalRule", err)
	}
}

// case 0개면 Degraded.
func TestSelfTestNoCasesIsDegraded(t *testing.T) {
	t.Parallel()

	noCases := `apiVersion: rosshield.io/v1
kind: SelfTest
metadata:
  checkId: CIS-1.1.1.1
spec:
  cases: []
`
	res, err := benchmark.RunCheckSelfTest(sampleCheck(), []byte(noCases))
	if err != nil {
		t.Fatalf("RunCheckSelfTest: %v", err)
	}
	if !res.Degraded {
		t.Error("Degraded should be true with 0 cases")
	}
}

// RunPackSelfTests — 여러 check의 self-test 일괄 실행.
func TestRunPackSelfTestsMultipleChecks(t *testing.T) {
	t.Parallel()

	pack := benchmark.Pack{
		Checks: []benchmark.Check{
			{CheckID: "CIS-A", EvaluationRule: json.RawMessage(`{"op":"empty"}`)},
			{CheckID: "CIS-B", EvaluationRule: json.RawMessage(`{"op":"not_empty"}`)},
			{CheckID: "CIS-C", EvaluationRule: json.RawMessage(`{"op":"empty"}`)}, // fixture 없음 → degraded
		},
	}
	fixtures := map[string][]byte{
		"CIS-A": []byte(`apiVersion: rosshield.io/v1
kind: SelfTest
metadata: { checkId: CIS-A }
spec:
  cases:
    - name: "ok"
      input: { stdout: "" }
      expectedOutcome: PASS
`),
		"CIS-B": []byte(`apiVersion: rosshield.io/v1
kind: SelfTest
metadata: { checkId: CIS-B }
spec:
  cases:
    - name: "ok"
      input: { stdout: "x" }
      expectedOutcome: PASS
`),
	}

	results, err := benchmark.RunPackSelfTests(pack, fixtures)
	if err != nil {
		t.Fatalf("RunPackSelfTests: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("results = %d, want 3", len(results))
	}
	if !results[0].AllPassed() || !results[1].AllPassed() {
		t.Error("CIS-A/B should both AllPassed")
	}
	if !results[2].Degraded {
		t.Error("CIS-C should be Degraded (no fixture)")
	}
}
