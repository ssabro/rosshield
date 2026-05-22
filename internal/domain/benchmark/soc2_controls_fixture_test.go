// soc2-controls pack(packs/soc2-controls/) Phase 11.B-7 check + selftest fixture가
// ParsePackYAML + ParseCheckYAML + ParseSelfTestYAML + RunCheckSelfTest round-trip을
// 통과하는지 검증.
//
// design doc: docs/design/notes/soc2-readiness-design.md §13.1 Stage 11.B-7.
// D-P11B-4 = 자동 검증 pack 권장 (CIS-style yaml).
// fixture 추가 시 본 테스트가 자동으로 cover (ros2-jazzy 패턴 일관).

package benchmark_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ssabro/rosshield/internal/domain/benchmark"
)

func TestSOC2ControlsPackYAMLValid(t *testing.T) {
	t.Parallel()
	packPath := filepath.Join("..", "..", "..", "packs", "soc2-controls", "pack.yaml")
	data, err := os.ReadFile(packPath)
	if err != nil {
		t.Fatalf("read pack.yaml: %v", err)
	}
	if err := benchmark.ValidatePackYAMLBytes(data); err != nil {
		t.Errorf("pack.yaml schema validation: %v", err)
	}
	pack, err := benchmark.ParsePackYAML(data)
	if err != nil {
		t.Fatalf("ParsePackYAML: %v", err)
	}
	if pack.Name != "soc2-controls" {
		t.Errorf("pack.Name = %q, want %q", pack.Name, "soc2-controls")
	}
	if pack.Version != "0.1.0" {
		t.Errorf("pack.Version = %q, want %q (Phase 11.B-7 Round 1)", pack.Version, "0.1.0")
	}
}

func TestSOC2ControlsChecksRoundTrip(t *testing.T) {
	t.Parallel()
	checksDir := filepath.Join("..", "..", "..", "packs", "soc2-controls", "checks")
	selftestDir := filepath.Join("..", "..", "..", "packs", "soc2-controls", "selftest")

	entries, err := os.ReadDir(checksDir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", checksDir, err)
	}
	yamlCount := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".yaml") {
			yamlCount++
		}
	}
	if yamlCount < 50 {
		t.Fatalf("soc2-controls: want ≥50 check yaml (design §13.1 baseline), got %d", yamlCount)
	}
	if yamlCount > 80 {
		t.Fatalf("soc2-controls: want ≤80 check yaml (design §13.1 upper bound), got %d", yamlCount)
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		name := e.Name()
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			checkBytes, err := os.ReadFile(filepath.Join(checksDir, name))
			if err != nil {
				t.Fatalf("read check: %v", err)
			}
			if err := benchmark.ValidateCheckYAMLBytes(checkBytes); err != nil {
				t.Fatalf("check schema validation: %v", err)
			}
			check, err := benchmark.ParseCheckYAML(checkBytes)
			if err != nil {
				t.Fatalf("ParseCheckYAML: %v", err)
			}
			// id naming convention: 본 pack은 "soc2." prefix + category(CC1~9 / A1~5) + snake_case.
			if !strings.HasPrefix(check.CheckID, "soc2.") {
				t.Errorf("checkID=%q, want prefix %q", check.CheckID, "soc2.")
			}
			node, err := benchmark.ParseEvalRule(check.EvaluationRule)
			if err != nil {
				t.Fatalf("ParseEvalRule: %v", err)
			}
			if node == nil {
				t.Fatal("ParseEvalRule returned nil node")
			}

			fixBytes, err := os.ReadFile(filepath.Join(selftestDir, name))
			if err != nil {
				t.Fatalf("read selftest fixture: %v", err)
			}
			res, err := benchmark.RunCheckSelfTest(check, fixBytes)
			if err != nil {
				t.Fatalf("RunCheckSelfTest: %v", err)
			}
			if !res.AllPassed() {
				t.Errorf("selftest not all pass: total=%d passed=%d failures=%+v",
					res.Total, res.Passed, res.Failures)
			}
		})
	}
}
