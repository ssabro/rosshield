package converter_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ssabro/rosshield/cmd/pack-tools/converter"
	"github.com/ssabro/rosshield/internal/domain/benchmark"
)

// === 단일 condition + 명시 pass_logic 없는 경우 ===
//
// 가장 단순한 케이스 — conditions 1개, pass_logic 생략.
const ros2SingleCondition = `{
  "schema_version": "1.0",
  "items": [
    {
      "id": "SVC_TIME_002",
      "name": "SSH 데몬 활성화",
      "name_en": "Ensure SSH daemon is enabled",
      "severity": "중",
      "audit_command": {
        "conditions": [
          {
            "id": "C1",
            "command": "systemctl is-enabled ssh",
            "extract_pattern": null,
            "value_type": "string",
            "operator": "equals",
            "expected": "enabled"
          }
        ]
      },
      "is_auto": true
    }
  ]
}`

// === 다중 condition + AND pass_logic (R8-4' 핵심 시나리오) ===
const ros2MultiAND = `{
  "schema_version": "1.0",
  "items": [
    {
      "id": "INIT_FSMOD_001",
      "name": "cramfs 모듈 비활성",
      "name_en": "Ensure cramfs kernel module is not available",
      "severity": "low",
      "audit_command": {
        "conditions": [
          {"id": "C1", "command": "lsmod | grep cramfs", "value_type": "string", "operator": "empty", "expected": null},
          {"id": "C2", "command": "modprobe --showconfig | grep -P '\\binstall\\h+cramfs'", "value_type": "string", "operator": "not_empty", "expected": null},
          {"id": "C3", "command": "modprobe --showconfig | grep blacklist", "value_type": "string", "operator": "not_empty", "expected": null}
        ],
        "pass_logic": {"AND": ["C1", "C2", "C3"]}
      },
      "is_auto": true
    }
  ]
}`

// === extract_pattern 있는 condition → degraded fallback ===
const ros2DegradedExtractPattern = `{
  "schema_version": "1.0",
  "items": [
    {
      "id": "SVC_TIME_001",
      "name": "Time sync count",
      "severity": "medium",
      "audit_command": {
        "conditions": [
          {
            "id": "C1",
            "command": "count=0; systemctl is-enabled foo; echo $count",
            "extract_pattern": "^(\\d+)$",
            "value_type": "string",
            "operator": "equals",
            "expected": 1
          }
        ]
      },
      "is_auto": true
    }
  ]
}`

// === numeric op gt → degraded ===
const ros2DegradedNumericOp = `{
  "schema_version": "1.0",
  "items": [
    {
      "id": "LOG_AUDIT_005",
      "name": "Audit max_log_file >= 8",
      "severity": "medium",
      "audit_command": {
        "conditions": [
          {"id": "C1", "command": "grep max_log /etc/audit/auditd.conf", "value_type": "number", "operator": "gte", "expected": 8}
        ]
      },
      "is_auto": true
    }
  ]
}`

// === audit_command 없는 manual review 항목 ===
const ros2NoAuditCommand = `{
  "schema_version": "1.0",
  "items": [
    {
      "id": "POLICY_001",
      "name": "Manual policy review",
      "name_en": "Manual policy review",
      "severity": "high",
      "is_auto": false
    }
  ]
}`

// === 모든 시나리오 통합 — 변환 통계 검증 ===
const ros2MixedFixture = `{
  "schema_version": "1.0",
  "items": [
    {"id": "A", "name": "single ok", "severity": "low",
     "audit_command": {"conditions":[{"id":"C1","command":"echo a","value_type":"string","operator":"equals","expected":"a"}]},
     "is_auto": true},
    {"id": "B", "name": "AND ok", "severity": "medium",
     "audit_command": {"conditions":[
       {"id":"C1","command":"echo b","value_type":"string","operator":"empty","expected":null},
       {"id":"C2","command":"echo bb","value_type":"string","operator":"not_empty","expected":null}
     ],"pass_logic":{"AND":["C1","C2"]}},
     "is_auto": true},
    {"id": "C", "name": "extract degraded", "severity": "high",
     "audit_command": {"conditions":[
       {"id":"C1","command":"echo c","extract_pattern":"^.","value_type":"string","operator":"equals","expected":"x"}
     ]},
     "is_auto": true},
    {"id": "D", "name": "manual", "severity": "critical", "is_auto": false}
  ]
}`

// === E12 T2 — ROS2 framework 변환 단위 ===

func TestConvertROS2SingleCondition(t *testing.T) {
	t.Parallel()
	pack, report, err := converter.ConvertROS2([]byte(ros2SingleCondition), converter.ROS2ConvertOptions{
		PackName: "ros2-jazzy", PackVersion: "1.1.0", PackVendor: "rosshield",
	})
	if err != nil {
		t.Fatalf("ConvertROS2: %v", err)
	}
	if report.TotalItems != 1 || report.Converted != 1 || len(report.Degraded) != 0 {
		t.Errorf("report = %+v, want {Total:1 Converted:1 Degraded:[]}", report)
	}
	if len(pack.Checks) != 1 {
		t.Fatalf("checks = %d, want 1", len(pack.Checks))
	}
	c := pack.Checks[0]
	if c.ID != "SVC_TIME_002" {
		t.Errorf("ID = %q", c.ID)
	}
	// AuditCommand는 `bash -c '...'`로 wrap되어 inner single-quote가 `'\''` escape됨.
	// 따라서 raw script form 대신 핵심 키워드만 검증.
	for _, want := range []string{"C1_OUT", "systemctl is-enabled ssh", "enabled", "** PASS **", "** FAIL **"} {
		if !strings.Contains(c.AuditCommand, want) {
			t.Errorf("AuditCommand missing %q\nfull: %s", want, c.AuditCommand)
		}
	}
	if string(c.EvaluationRule) != `{"op":"contains","value":"** PASS **"}` {
		t.Errorf("EvaluationRule = %s, want PASS marker contains", c.EvaluationRule)
	}
}

func TestConvertROS2MultiConditionAND(t *testing.T) {
	t.Parallel()
	pack, report, err := converter.ConvertROS2([]byte(ros2MultiAND), converter.ROS2ConvertOptions{})
	if err != nil {
		t.Fatalf("ConvertROS2: %v", err)
	}
	if report.Converted != 1 {
		t.Errorf("Converted = %d, want 1", report.Converted)
	}
	c := pack.Checks[0]
	// 모든 3 condition이 setup phase에 등장.
	for _, condID := range []string{"C1_OUT", "C2_OUT", "C3_OUT"} {
		if !strings.Contains(c.AuditCommand, condID) {
			t.Errorf("AuditCommand missing %s setup: %q", condID, c.AuditCommand)
		}
	}
	// AND 트리는 두 개의 && 연결자를 생성.
	andCount := strings.Count(c.AuditCommand, " && ")
	if andCount != 2 {
		t.Errorf("&& count = %d, want 2 (C1 && C2 && C3)", andCount)
	}
}

func TestConvertROS2DegradedExtractPattern(t *testing.T) {
	t.Parallel()
	pack, report, err := converter.ConvertROS2([]byte(ros2DegradedExtractPattern), converter.ROS2ConvertOptions{})
	if err != nil {
		t.Fatalf("ConvertROS2: %v", err)
	}
	if report.Converted != 0 {
		t.Errorf("Converted = %d, want 0 (extract_pattern degraded)", report.Converted)
	}
	if len(report.Degraded) != 1 {
		t.Fatalf("Degraded = %v, want 1 entry", report.Degraded)
	}
	if !strings.Contains(report.Degraded[0], "extract_pattern not supported") {
		t.Errorf("degraded reason = %q", report.Degraded[0])
	}
	c := pack.Checks[0]
	if c.AuditCommand != "true" {
		t.Errorf("degraded AuditCommand = %q, want \"true\"", c.AuditCommand)
	}
}

func TestConvertROS2DegradedNumericOp(t *testing.T) {
	t.Parallel()
	_, report, err := converter.ConvertROS2([]byte(ros2DegradedNumericOp), converter.ROS2ConvertOptions{})
	if err != nil {
		t.Fatalf("ConvertROS2: %v", err)
	}
	if report.Converted != 0 || len(report.Degraded) != 1 {
		t.Errorf("report = %+v, want {Converted:0 Degraded:1}", report)
	}
	if !strings.Contains(report.Degraded[0], "value_type=number") &&
		!strings.Contains(report.Degraded[0], "numeric") {
		t.Errorf("degraded reason missing numeric mention: %q", report.Degraded[0])
	}
}

func TestConvertROS2NoAuditCommand(t *testing.T) {
	t.Parallel()
	pack, report, err := converter.ConvertROS2([]byte(ros2NoAuditCommand), converter.ROS2ConvertOptions{})
	if err != nil {
		t.Fatalf("ConvertROS2: %v", err)
	}
	if report.Converted != 0 || len(report.Degraded) != 1 {
		t.Errorf("report = %+v", report)
	}
	if !strings.Contains(report.Degraded[0], "manual review") {
		t.Errorf("reason: %q", report.Degraded[0])
	}
	c := pack.Checks[0]
	if c.AuditCommand != "true" {
		t.Errorf("AuditCommand = %q", c.AuditCommand)
	}
}

func TestConvertROS2MixedFixtureStatistics(t *testing.T) {
	t.Parallel()
	pack, report, err := converter.ConvertROS2([]byte(ros2MixedFixture), converter.ROS2ConvertOptions{})
	if err != nil {
		t.Fatalf("ConvertROS2: %v", err)
	}
	if report.TotalItems != 4 {
		t.Errorf("TotalItems = %d, want 4", report.TotalItems)
	}
	if report.Converted != 2 {
		t.Errorf("Converted = %d, want 2 (A·B)", report.Converted)
	}
	if len(report.Degraded) != 2 {
		t.Errorf("Degraded count = %d, want 2 (C·D)", len(report.Degraded))
	}
	if len(pack.Checks) != 4 {
		t.Errorf("checks = %d, want 4 (모든 item이 출력에 포함)", len(pack.Checks))
	}
}

func TestConvertROS2RejectsInvalidJSON(t *testing.T) {
	t.Parallel()
	_, _, err := converter.ConvertROS2([]byte(`{not-json`), converter.ROS2ConvertOptions{})
	if !errors.Is(err, converter.ErrROS2DecodeFailed) {
		t.Errorf("err = %v, want ErrROS2DecodeFailed", err)
	}
}

func TestConvertROS2RejectsNoItems(t *testing.T) {
	t.Parallel()
	_, _, err := converter.ConvertROS2([]byte(`{"items":[]}`), converter.ROS2ConvertOptions{})
	if !errors.Is(err, converter.ErrROS2NoItems) {
		t.Errorf("err = %v, want ErrROS2NoItems", err)
	}
}

func TestConvertROS2LanguagePreference(t *testing.T) {
	t.Parallel()
	en, _, err := converter.ConvertROS2([]byte(ros2SingleCondition), converter.ROS2ConvertOptions{PreferEnglish: true})
	if err != nil {
		t.Fatalf("ConvertROS2 en: %v", err)
	}
	if en.Checks[0].Title != "Ensure SSH daemon is enabled" {
		t.Errorf("en.Title = %q, want English", en.Checks[0].Title)
	}

	ko, _, err := converter.ConvertROS2([]byte(ros2SingleCondition), converter.ROS2ConvertOptions{PreferEnglish: false})
	if err != nil {
		t.Fatalf("ConvertROS2 ko: %v", err)
	}
	if ko.Checks[0].Title != "SSH 데몬 활성화" {
		t.Errorf("ko.Title = %q, want Korean", ko.Checks[0].Title)
	}
}

// === T2 통합 — 변환 결과가 benchmark 로더로 라운드트립 (Stage A 라운드트립과 동일 보장) ===
func TestConvertROS2RoundTripsThroughBenchmarkLoader(t *testing.T) {
	t.Parallel()
	pack, _, err := converter.ConvertROS2([]byte(ros2MixedFixture), converter.ROS2ConvertOptions{
		PackName: "ros2-mixed", PackVersion: "1.0.0", PackVendor: "rosshield",
	})
	if err != nil {
		t.Fatalf("ConvertROS2: %v", err)
	}

	out := filepath.Join(t.TempDir(), "out")
	if err := converter.WriteToDir(pack, out); err != nil {
		t.Fatalf("WriteToDir: %v", err)
	}

	packYAML, err := os.ReadFile(filepath.Join(out, "pack.yaml"))
	if err != nil {
		t.Fatalf("read pack.yaml: %v", err)
	}
	if _, err := benchmark.ParsePackYAML(packYAML); err != nil {
		t.Fatalf("ParsePackYAML: %v", err)
	}

	for _, c := range pack.Checks {
		data, err := os.ReadFile(filepath.Join(out, "checks", c.ID+".yaml"))
		if err != nil {
			t.Fatalf("read check %s: %v", c.ID, err)
		}
		check, err := benchmark.ParseCheckYAML(data)
		if err != nil {
			t.Fatalf("ParseCheckYAML %s: %v", c.ID, err)
		}
		if _, err := benchmark.ParseEvalRule(check.EvaluationRule); err != nil {
			t.Errorf("ParseEvalRule %s: %v", c.ID, err)
		}
	}
}

// === 실제 nrobotcheck JSON 파일 e2e (옵트인) ===
//
// 환경변수 ROSSHIELD_NROBOTCHECK_DIR이 설정되면 실제 1.3MB 파일로 변환 시도.
// CI에서는 unset이므로 SKIP — 개발자가 수동 실행 시 데이터 검증.
func TestConvertROS2RealJazzyV1_1(t *testing.T) {
	dir := os.Getenv("ROSSHIELD_NROBOTCHECK_DIR")
	if dir == "" {
		t.Skip("set ROSSHIELD_NROBOTCHECK_DIR to enable e2e (real 1.3MB JSON)")
	}
	path := filepath.Join(dir, "resources", "baselines", "ros2_jazzy_security_baseline_framework_v1.1.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("real ROS2 JSON not found: %v", err)
	}
	jsonBytes := stripBOM(data)

	pack, report, err := converter.ConvertROS2(jsonBytes, converter.ROS2ConvertOptions{
		PackName: "ros2-jazzy", PackVersion: "1.1.0", PackVendor: "rosshield",
		PreferEnglish: true,
	})
	if err != nil {
		t.Fatalf("ConvertROS2: %v", err)
	}
	t.Logf("ROS2 jazzy v1.1: total=%d converted=%d degraded=%d (%.1f%% auto)",
		report.TotalItems, report.Converted, len(report.Degraded),
		float64(report.Converted)/float64(report.TotalItems)*100)
	if report.TotalItems != 329 {
		t.Errorf("TotalItems = %d, want 329", report.TotalItems)
	}
	if report.Converted < 250 {
		t.Errorf("Converted = %d, want ≥ 250 (R8-4' 예상 ~280)", report.Converted)
	}
	if len(pack.Checks) != report.TotalItems {
		t.Errorf("output checks = %d, want %d (degraded도 포함)", len(pack.Checks), report.TotalItems)
	}

	// 변환 결과를 디스크에 펼쳐 라운드트립.
	out := filepath.Join(t.TempDir(), "ros2-jazzy")
	if err := converter.WriteToDir(pack, out); err != nil {
		t.Fatalf("WriteToDir: %v", err)
	}
	packYAML, _ := os.ReadFile(filepath.Join(out, "pack.yaml"))
	if _, err := benchmark.ParsePackYAML(packYAML); err != nil {
		t.Errorf("loaded pack invalid: %v", err)
	}
}

// stripBOM은 utf-8 BOM(0xEF 0xBB 0xBF)을 제거합니다 — 일부 외부 파일에 있음.
func stripBOM(data []byte) []byte {
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		return data[3:]
	}
	return data
}

// === 보조 — JSON 디코드 시 expected 필드가 다양한 타입 ===
func TestConvertROS2ExpectedTypeVariants(t *testing.T) {
	t.Parallel()
	body := `{"items":[
       {"id":"X","name":"x","severity":"low",
        "audit_command":{"conditions":[
          {"id":"C1","command":"echo 1","value_type":"string","operator":"equals","expected":"text"}
        ]}}
     ]}`
	_, _, err := converter.ConvertROS2([]byte(body), converter.ROS2ConvertOptions{})
	if err != nil {
		t.Fatalf("string expected: %v", err)
	}
}

// === EvaluationRule이 production AST로도 파싱 가능한지 — passEvalRuleJSON 정합 ===
func TestConvertROS2EvalRuleParsableByBenchmark(t *testing.T) {
	t.Parallel()
	pack, _, err := converter.ConvertROS2([]byte(ros2SingleCondition), converter.ROS2ConvertOptions{})
	if err != nil {
		t.Fatalf("ConvertROS2: %v", err)
	}
	c := pack.Checks[0]
	node, err := benchmark.ParseEvalRule(json.RawMessage(c.EvaluationRule))
	if err != nil {
		t.Fatalf("ParseEvalRule: %v", err)
	}
	if node == nil {
		t.Fatal("node = nil")
	}
}
