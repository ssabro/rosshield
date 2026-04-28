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

// === E12 Stage A 핵심 — 라운드트립: WriteToDir 출력이 benchmark.Parse* 모두 통과 ===
//
// 변환기가 만든 pack.yaml/checks/*.yaml은 production loader가 그대로 수용해야 합니다.
// 자체 packYAML/checkYAML 정의가 benchmark의 그것과 schema drift되면 이 테스트가 실패합니다.
func TestWriteRoundtripsThroughBenchmarkLoader(t *testing.T) {
	pack := converter.Pack{
		Name:        "cis-ubuntu-2404",
		Version:     "1.0.0",
		Vendor:      "rosshield",
		Description: "CIS Ubuntu 24.04 baseline (Stage A roundtrip fixture)",
		Checks: []converter.Check{
			{
				ID:             "CIS-1.1.1.1",
				Title:          "Ensure cramfs kernel module is not available",
				Description:    "cramfs is not needed; disable it.",
				Severity:       "high",
				AuditCommand:   "bash -c 'lsmod | grep cramfs && echo \"** FAIL **\" || echo \"** PASS **\"'",
				EvaluationRule: json.RawMessage(`{"op":"contains","value":"** PASS **"}`),
				Rationale:      "Reduces local attack surface.",
				FixGuidance:    "Add modprobe blacklist entry.",
			},
			{
				ID:           "CIS-2.3",
				Title:        "Composite eval rule check",
				Severity:     "medium",
				AuditCommand: "bash -c 'cat /etc/ssh/sshd_config'",
				EvaluationRule: json.RawMessage(
					`{"op":"and","args":[{"op":"contains","value":"PermitRootLogin no"},{"op":"not","arg":{"op":"contains","value":"AllowUsers root"}}]}`,
				),
			},
		},
	}

	outDir := filepath.Join(t.TempDir(), "out")
	if err := converter.WriteToDir(pack, outDir); err != nil {
		t.Fatalf("WriteToDir: %v", err)
	}

	// pack.yaml — benchmark.ParsePackYAML로 동등 파싱.
	packBytes, err := os.ReadFile(filepath.Join(outDir, "pack.yaml"))
	if err != nil {
		t.Fatalf("read pack.yaml: %v", err)
	}
	parsed, err := benchmark.ParsePackYAML(packBytes)
	if err != nil {
		t.Fatalf("ParsePackYAML: %v", err)
	}
	if parsed.Name != pack.Name {
		t.Errorf("pack.Name = %q, want %q", parsed.Name, pack.Name)
	}
	if parsed.Version != pack.Version {
		t.Errorf("pack.Version = %q, want %q", parsed.Version, pack.Version)
	}
	if parsed.Vendor != pack.Vendor {
		t.Errorf("pack.Vendor = %q, want %q", parsed.Vendor, pack.Vendor)
	}
	if parsed.Description != pack.Description {
		t.Errorf("pack.Description = %q, want %q", parsed.Description, pack.Description)
	}
	if parsed.SchemaVersion != 1 {
		t.Errorf("pack.SchemaVersion = %d, want 1 (default)", parsed.SchemaVersion)
	}

	// checks/*.yaml — benchmark.ParseCheckYAML + ParseEvalRule 둘 다 통과.
	for _, c := range pack.Checks {
		path := filepath.Join(outDir, "checks", c.ID+".yaml")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read check %s: %v", c.ID, err)
		}
		check, err := benchmark.ParseCheckYAML(data)
		if err != nil {
			t.Fatalf("ParseCheckYAML %s: %v", c.ID, err)
		}
		if check.CheckID != c.ID {
			t.Errorf("CheckID = %q, want %q", check.CheckID, c.ID)
		}
		if check.Title != c.Title {
			t.Errorf("Title = %q, want %q", check.Title, c.Title)
		}
		if string(check.Severity) != c.Severity {
			t.Errorf("Severity = %q, want %q", check.Severity, c.Severity)
		}
		if check.AuditCommand != c.AuditCommand {
			t.Errorf("AuditCommand mismatch for %s", c.ID)
		}
		// EvaluationRule도 production AST 파서로 검증.
		if _, err := benchmark.ParseEvalRule(check.EvaluationRule); err != nil {
			t.Errorf("ParseEvalRule %s: %v", c.ID, err)
		}
	}
}

// === MarshalPack 음의 케이스 ===

func TestMarshalPackRejectsEmptyName(t *testing.T) {
	_, err := converter.MarshalPack(converter.Pack{Version: "1.0.0", Vendor: "x"})
	if !errors.Is(err, converter.ErrEmptyPackName) {
		t.Errorf("err = %v, want ErrEmptyPackName", err)
	}
}

func TestMarshalPackDefaultsSchemaVersion(t *testing.T) {
	out, err := converter.MarshalPack(converter.Pack{Name: "n", Version: "1", Vendor: "v"})
	if err != nil {
		t.Fatalf("MarshalPack: %v", err)
	}
	if !strings.Contains(string(out), "schemaVersion: 1") {
		t.Errorf("output missing schemaVersion: 1\n%s", out)
	}
}

// === MarshalCheck 음의 케이스 ===

func TestMarshalCheckRejectsEmptyID(t *testing.T) {
	_, err := converter.MarshalCheck(converter.Check{Title: "t"})
	if !errors.Is(err, converter.ErrEmptyCheckID) {
		t.Errorf("err = %v, want ErrEmptyCheckID", err)
	}
}

func TestMarshalCheckRejectsInvalidEvalRule(t *testing.T) {
	_, err := converter.MarshalCheck(converter.Check{
		ID: "X", Title: "t",
		EvaluationRule: json.RawMessage(`{not-valid-json`),
	})
	if !errors.Is(err, converter.ErrInvalidEvalRule) {
		t.Errorf("err = %v, want ErrInvalidEvalRule", err)
	}
}

func TestMarshalCheckDefaultsSeverity(t *testing.T) {
	out, err := converter.MarshalCheck(converter.Check{
		ID: "X", Title: "t",
		EvaluationRule: json.RawMessage(`{"op":"empty"}`),
	})
	if err != nil {
		t.Fatalf("MarshalCheck: %v", err)
	}
	if !strings.Contains(string(out), "severity: medium") {
		t.Errorf("output missing severity: medium (default)\n%s", out)
	}
}

func TestMarshalCheckOmitsEmptyOptionalFields(t *testing.T) {
	out, err := converter.MarshalCheck(converter.Check{
		ID:             "X",
		Title:          "t",
		Severity:       "low",
		AuditCommand:   "echo ok",
		EvaluationRule: json.RawMessage(`{"op":"contains","value":"ok"}`),
		// Description·Rationale·FixGuidance 모두 빈 string
	})
	if err != nil {
		t.Fatalf("MarshalCheck: %v", err)
	}
	s := string(out)
	for _, banned := range []string{"description:", "rationale:", "fixGuidance:"} {
		if strings.Contains(s, banned) {
			t.Errorf("output contains %q (omitempty 기대)\n%s", banned, s)
		}
	}
}

// === WriteToDir 디렉터리 안전성 ===

func TestWriteToDirRefusesExistingOutputDir(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatalf("setup mkdir: %v", err)
	}
	err := converter.WriteToDir(converter.Pack{Name: "n", Version: "1", Vendor: "v"}, out)
	if !errors.Is(err, converter.ErrOutputExists) {
		t.Errorf("err = %v, want ErrOutputExists", err)
	}
}

func TestWriteToDirRejectsDuplicateCheckID(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out")
	pack := converter.Pack{
		Name: "n", Version: "1", Vendor: "v",
		Checks: []converter.Check{
			{ID: "X", Title: "first", EvaluationRule: json.RawMessage(`{"op":"empty"}`)},
			{ID: "X", Title: "second (dup)", EvaluationRule: json.RawMessage(`{"op":"empty"}`)},
		},
	}
	err := converter.WriteToDir(pack, out)
	if !errors.Is(err, converter.ErrDuplicateCheckID) {
		t.Errorf("err = %v, want ErrDuplicateCheckID", err)
	}
}

func TestWriteToDirCreatesExpectedLayout(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out")
	pack := converter.Pack{
		Name: "n", Version: "1", Vendor: "v",
		Checks: []converter.Check{
			{ID: "A", Title: "a", EvaluationRule: json.RawMessage(`{"op":"empty"}`)},
			{ID: "B", Title: "b", EvaluationRule: json.RawMessage(`{"op":"empty"}`)},
		},
	}
	if err := converter.WriteToDir(pack, out); err != nil {
		t.Fatalf("WriteToDir: %v", err)
	}
	for _, rel := range []string{"pack.yaml", "checks/A.yaml", "checks/B.yaml"} {
		if _, err := os.Stat(filepath.Join(out, rel)); err != nil {
			t.Errorf("expected file %s missing: %v", rel, err)
		}
	}
}
