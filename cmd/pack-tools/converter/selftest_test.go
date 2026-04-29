package converter_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/ssabro/rosshield/cmd/pack-tools/converter"
	"github.com/ssabro/rosshield/internal/domain/benchmark"
)

// E12 Stage D · T5 — Self-Test skeleton 생성기.
//
// 자동 변환된 check(`{"op":"contains","value":"** PASS **"}` rule)는 stdout 마커가
// 결정론적이라서 fixture를 자동 생성 가능 — PASS / FAIL 두 케이스. degraded check는
// 별도 보고서 항목(checks/<id>.yaml에 fixture 미생성)으로 남겨 사용자가 수동 보강.

func TestGenerateSelfTestSkeletonsForAutoConverted(t *testing.T) {
	pack := converter.Pack{
		Name: "ros2-baseline", Version: "1.0.0", Vendor: "rosshield",
		Checks: []converter.Check{
			{
				ID: "ROS2-001", Title: "auto", Severity: "medium",
				AuditCommand:   "bash -c 'systemctl is-enabled ssh'",
				EvaluationRule: json.RawMessage(`{"op":"contains","value":"** PASS **"}`),
			},
			{
				ID: "ROS2-002", Title: "degraded", Severity: "medium",
				AuditCommand: "true",
				EvaluationRule: json.RawMessage(
					`{"op":"contains","value":"<degraded — Phase 2 fixture required>"}`),
			},
		},
	}

	report := converter.GenerateSelfTestSkeletons(pack)
	if len(report.Skeletons) != 1 {
		t.Fatalf("Skeletons want 1 (auto-converted ROS2-001), got %d", len(report.Skeletons))
	}
	if report.Skeletons[0].CheckID != "ROS2-001" {
		t.Fatalf("Skeleton.CheckID = %q, want ROS2-001", report.Skeletons[0].CheckID)
	}
	if len(report.Degraded) != 1 || report.Degraded[0] != "ROS2-002" {
		t.Fatalf("Degraded want [ROS2-002], got %v", report.Degraded)
	}

	// 실제 fixture YAML이 benchmark.ParseSelfTestYAML로 파싱되어야 한다.
	checkID, cases, err := benchmark.ParseSelfTestYAML(report.Skeletons[0].YAML)
	if err != nil {
		t.Fatalf("ParseSelfTestYAML: %v", err)
	}
	if checkID != "ROS2-001" {
		t.Fatalf("parsed checkID=%q, want ROS2-001", checkID)
	}
	if len(cases) != 2 {
		t.Fatalf("cases len=%d, want 2 (PASS + FAIL)", len(cases))
	}

	// PASS case: stdout에 "** PASS **" 포함 → benchmark eval로 PASS 판정.
	rule, err := benchmark.ParseEvalRule(pack.Checks[0].EvaluationRule)
	if err != nil {
		t.Fatalf("ParseEvalRule: %v", err)
	}
	for _, c := range cases {
		got, err := rule.Eval(benchmark.EvalInput{Stdout: c.Input.Stdout, ExitCode: c.Input.ExitCode})
		if err != nil {
			t.Fatalf("Eval %q: %v", c.Name, err)
		}
		if got.Status != c.ExpectedOutcome {
			t.Fatalf("case %q: Eval=%s, expected=%s — fixture가 evaluationRule과 모순",
				c.Name, got.Status, c.ExpectedOutcome)
		}
	}
}

// degraded check는 fixture를 생성하지 않는다 — sentinel rule이 stdout에서 절대 매칭되지
// 않으므로 PASS case가 무의미하기 때문. 사용자가 수동 fixture를 추가하기 전까지는
// benchmark.RunCheckSelfTest가 Degraded=true로 분류.
func TestGenerateSkipsDegradedChecks(t *testing.T) {
	pack := converter.Pack{
		Name: "x", Version: "1", Vendor: "v",
		Checks: []converter.Check{
			{ID: "X-1", Severity: "low", AuditCommand: "true",
				EvaluationRule: json.RawMessage(
					`{"op":"contains","value":"<degraded — Phase 2 fixture required>"}`)},
			{ID: "X-2", Severity: "low", AuditCommand: "true",
				EvaluationRule: json.RawMessage(
					`{"op":"contains","value":"<degraded — Phase 2 fixture required>"}`)},
		},
	}
	report := converter.GenerateSelfTestSkeletons(pack)
	if len(report.Skeletons) != 0 {
		t.Fatalf("expected 0 skeletons (모두 degraded), got %d", len(report.Skeletons))
	}
	sort.Strings(report.Degraded)
	if !equalStrings(report.Degraded, []string{"X-1", "X-2"}) {
		t.Fatalf("Degraded want [X-1 X-2], got %v", report.Degraded)
	}
}

// WriteToDir이 selftest/<id>.yaml로 자동 변환된 check의 fixture를 함께 쓴다.
// degraded check은 selftest 파일을 만들지 않음.
func TestWriteToDirEmitsSelfTestFixtures(t *testing.T) {
	pack := converter.Pack{
		Name: "p", Version: "1", Vendor: "v",
		Checks: []converter.Check{
			{ID: "AUTO-1", Title: "auto", Severity: "medium",
				AuditCommand:   "bash -c 'true'",
				EvaluationRule: json.RawMessage(`{"op":"contains","value":"** PASS **"}`)},
			{ID: "DEGR-1", Title: "deg", Severity: "medium",
				AuditCommand: "true",
				EvaluationRule: json.RawMessage(
					`{"op":"contains","value":"<degraded — Phase 2 fixture required>"}`)},
		},
	}
	outDir := filepath.Join(t.TempDir(), "out")
	if err := converter.WriteToDir(pack, outDir); err != nil {
		t.Fatalf("WriteToDir: %v", err)
	}

	body, err := os.ReadFile(filepath.Join(outDir, "selftest", "AUTO-1.yaml"))
	if err != nil {
		t.Fatalf("read AUTO-1 selftest: %v", err)
	}
	checkID, cases, err := benchmark.ParseSelfTestYAML(body)
	if err != nil {
		t.Fatalf("ParseSelfTestYAML: %v", err)
	}
	if checkID != "AUTO-1" || len(cases) != 2 {
		t.Fatalf("checkID=%q cases=%d, want AUTO-1 / 2", checkID, len(cases))
	}

	if _, err := os.Stat(filepath.Join(outDir, "selftest", "DEGR-1.yaml")); !os.IsNotExist(err) {
		t.Fatalf("DEGR-1 selftest는 생성되지 않아야 함 (got err=%v)", err)
	}
}

// degraded marker 보고서가 콘솔 출력에 활용될 수 있게 정렬된 ID 슬라이스로 노출된다.
func TestSelfTestReportDegradedSorted(t *testing.T) {
	pack := converter.Pack{
		Name: "p", Version: "1", Vendor: "v",
		Checks: []converter.Check{
			{ID: "Z-1", Severity: "low", AuditCommand: "true",
				EvaluationRule: json.RawMessage(
					`{"op":"contains","value":"<degraded — Phase 2 fixture required>"}`)},
			{ID: "A-1", Severity: "low", AuditCommand: "true",
				EvaluationRule: json.RawMessage(
					`{"op":"contains","value":"<degraded — Phase 2 fixture required>"}`)},
			{ID: "M-1", Severity: "low", AuditCommand: "true",
				EvaluationRule: json.RawMessage(
					`{"op":"contains","value":"<degraded — Phase 2 fixture required>"}`)},
		},
	}
	report := converter.GenerateSelfTestSkeletons(pack)
	if !equalStrings(report.Degraded, []string{"A-1", "M-1", "Z-1"}) {
		t.Fatalf("Degraded should be sorted, got %v", report.Degraded)
	}
}

// custom rule(자동 변환 마커가 아닌)은 skeleton 자동 생성 대상이 아님 — 호출자가
// fixture를 직접 작성해야 함. 보고서는 이를 "Custom"으로 분류해 사용자가 인지하게 함.
func TestGenerateClassifiesCustomRulesAsManualReview(t *testing.T) {
	pack := converter.Pack{
		Name: "p", Version: "1", Vendor: "v",
		Checks: []converter.Check{
			{ID: "CUSTOM-1", Severity: "low",
				AuditCommand: "bash -c 'cat /etc/ssh/sshd_config'",
				EvaluationRule: json.RawMessage(
					`{"op":"and","args":[{"op":"contains","value":"PermitRootLogin no"}]}`)},
		},
	}
	report := converter.GenerateSelfTestSkeletons(pack)
	if len(report.Skeletons) != 0 {
		t.Fatalf("Custom rule should NOT auto-generate skeleton, got %d", len(report.Skeletons))
	}
	if len(report.Custom) != 1 || report.Custom[0] != "CUSTOM-1" {
		t.Fatalf("Custom want [CUSTOM-1], got %v", report.Custom)
	}
	if len(report.Degraded) != 0 {
		t.Fatalf("Custom는 Degraded와 별개로 분류되어야 함, got Degraded=%v", report.Degraded)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// fixture YAML에는 production check의 sample stdout 그대로 (PASS 마커 포함) — 외부
// 검증자가 텍스트 검색만으로도 의도를 이해할 수 있어야 한다.
func TestSkeletonYAMLContainsPassMarker(t *testing.T) {
	pack := converter.Pack{
		Name: "p", Version: "1", Vendor: "v",
		Checks: []converter.Check{
			{ID: "M-1", Severity: "medium", AuditCommand: "bash -c 'true'",
				EvaluationRule: json.RawMessage(`{"op":"contains","value":"** PASS **"}`)},
		},
	}
	report := converter.GenerateSelfTestSkeletons(pack)
	yamlText := string(report.Skeletons[0].YAML)
	if !strings.Contains(yamlText, "** PASS **") {
		t.Fatalf("skeleton YAML missing PASS marker: %q", yamlText)
	}
	if !strings.Contains(yamlText, "** FAIL **") {
		t.Fatalf("skeleton YAML missing FAIL marker: %q", yamlText)
	}
	if !strings.Contains(yamlText, "expectedOutcome: PASS") {
		t.Fatalf("skeleton YAML missing expectedOutcome PASS: %q", yamlText)
	}
}
