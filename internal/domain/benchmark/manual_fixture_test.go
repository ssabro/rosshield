// Manual fixture(packs/cis-ubuntu-2404/checks/manual/*.yaml + selftest/manual/*.yaml)가
// ParseCheckYAML + ParseSelfTestYAML + RunCheckSelfTest round-trip을 통과하는지 검증.
//
// D-MAN-1·2·3 권장 default(checks/manual + selftest/manual + op="manual")의 통합 검증.
// fixture 추가 시 본 테스트가 자동으로 cover.

package benchmark_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ssabro/rosshield/internal/domain/benchmark"
)

func TestManualFixturesRoundTrip(t *testing.T) {
	t.Parallel()
	checksDir := filepath.Join("..", "..", "..", "packs", "cis-ubuntu-2404", "checks", "manual")
	selftestDir := filepath.Join("..", "..", "..", "packs", "cis-ubuntu-2404", "selftest", "manual")

	entries, err := os.ReadDir(checksDir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", checksDir, err)
	}
	if len(entries) == 0 {
		t.Fatalf("no manual fixtures found in %s", checksDir)
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
			check, err := benchmark.ParseCheckYAML(checkBytes)
			if err != nil {
				t.Fatalf("ParseCheckYAML: %v", err)
			}
			if check.AuditCommand != "true" {
				t.Errorf("auditCommand=%q, want \"true\" (manual fixture는 audit 실행 의미 없음)",
					check.AuditCommand)
			}
			node, err := benchmark.ParseEvalRule(check.EvaluationRule)
			if err != nil {
				t.Fatalf("ParseEvalRule: %v", err)
			}
			if _, ok := node.(benchmark.ManualNode); !ok {
				t.Fatalf("evalRule type = %T, want ManualNode", node)
			}

			fixBytes, err := os.ReadFile(filepath.Join(selftestDir, name))
			if err != nil {
				t.Fatalf("read selftest: %v", err)
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
