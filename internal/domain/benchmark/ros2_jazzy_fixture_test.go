// ros2-jazzy pack(packs/ros2-jazzy/) Round 1 Stage 1 check + selftest fixture가
// ParsePackYAML + ParseCheckYAML + ParseSelfTestYAML + RunCheckSelfTest round-trip을
// 통과하는지 검증.
//
// design doc: docs/design/notes/ros2-baseline-pack-design.md §6 R1 + §7.1.
// D-ROS2-1 옵션 B Round 1 MVP — C1·C6 첫 진척(4~6 check).
// fixture 추가 시 본 테스트가 자동으로 cover.

package benchmark_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ssabro/rosshield/internal/domain/benchmark"
)

func TestROS2JazzyPackYAMLValid(t *testing.T) {
	t.Parallel()
	packPath := filepath.Join("..", "..", "..", "packs", "ros2-jazzy", "pack.yaml")
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
	if pack.Name != "ros2-jazzy" {
		t.Errorf("pack.Name = %q, want %q", pack.Name, "ros2-jazzy")
	}
	if pack.Version != "0.1.0" {
		t.Errorf("pack.Version = %q, want %q (Round 1 Stage 1)", pack.Version, "0.1.0")
	}
}

func TestROS2JazzyChecksRoundTrip(t *testing.T) {
	t.Parallel()
	checksDir := filepath.Join("..", "..", "..", "packs", "ros2-jazzy", "checks")
	selftestDir := filepath.Join("..", "..", "..", "packs", "ros2-jazzy", "selftest")

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
	if yamlCount < 4 {
		t.Fatalf("ros2-jazzy R1 Stage 1: want ≥4 check yaml, got %d", yamlCount)
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
			// id naming convention: 본 pack은 "ros2." prefix + category(C1/C6) + snake_case.
			if !strings.HasPrefix(check.CheckID, "ros2.") {
				t.Errorf("checkID=%q, want prefix %q", check.CheckID, "ros2.")
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
				t.Fatalf("read selftest fixture (mock 작성 필수 — D-ROS2-9 default): %v", err)
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
