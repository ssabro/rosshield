package benchmark_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ssabro/rosshield/internal/domain/benchmark"
)

const validPackYAML = `apiVersion: rosshield.io/v1
kind: Pack
metadata:
  name: cis-ubuntu-2404
  version: v1.0.0
  vendor: CIS
  description: "CIS Ubuntu 24.04 Benchmark"
spec:
  schemaVersion: 1
`

const validCheckYAML = `apiVersion: rosshield.io/v1
kind: Check
metadata:
  id: CIS-1.1.1.1
  title: "Disable cramfs kernel module"
  description: "Reduce attack surface."
  severity: high
spec:
  auditCommand: "lsmod | grep cramfs"
  evaluationRule:
    op: equals
    expected: ""
  rationale: "cramfs has known vulnerabilities."
  fixGuidance: "Add cramfs to /etc/modprobe.d/blacklist.conf"
`

// E4.T1 본체.
func TestPackLoadsValidManifest(t *testing.T) {
	t.Parallel()

	pack, err := benchmark.ParsePackYAML([]byte(validPackYAML))
	if err != nil {
		t.Fatalf("ParsePackYAML: %v", err)
	}
	if pack.Name != "cis-ubuntu-2404" {
		t.Errorf("Name = %q", pack.Name)
	}
	if pack.Version != "v1.0.0" {
		t.Errorf("Version = %q", pack.Version)
	}
	if pack.Vendor != "CIS" {
		t.Errorf("Vendor = %q", pack.Vendor)
	}
	if pack.PackKey != "cis-cis-ubuntu-2404-v1.0.0" {
		t.Errorf("PackKey = %q, want lowercased <vendor>-<name>-<version>", pack.PackKey)
	}
	if pack.SchemaVersion != 1 {
		t.Errorf("SchemaVersion = %d", pack.SchemaVersion)
	}
}

func TestParseCheckYAMLValid(t *testing.T) {
	t.Parallel()

	check, err := benchmark.ParseCheckYAML([]byte(validCheckYAML))
	if err != nil {
		t.Fatalf("ParseCheckYAML: %v", err)
	}
	if check.CheckID != "CIS-1.1.1.1" {
		t.Errorf("CheckID = %q", check.CheckID)
	}
	if check.Severity != benchmark.SeverityHigh {
		t.Errorf("Severity = %q", check.Severity)
	}
	if check.AuditCommand != "lsmod | grep cramfs" {
		t.Errorf("AuditCommand = %q", check.AuditCommand)
	}
	// EvaluationRule should be JSON-encoded.
	if len(check.EvaluationRule) == 0 {
		t.Fatal("EvaluationRule empty")
	}
	var rule map[string]any
	if err := json.Unmarshal(check.EvaluationRule, &rule); err != nil {
		t.Fatalf("EvaluationRule not JSON: %v\nbody: %s", err, string(check.EvaluationRule))
	}
	if rule["op"] != "equals" {
		t.Errorf("rule.op = %v, want equals", rule["op"])
	}
}

func TestParsePackYAMLRejectsUnknownAPIVersion(t *testing.T) {
	t.Parallel()
	bad := strings.Replace(validPackYAML, "rosshield.io/v1", "rosshield.io/v999", 1)
	_, err := benchmark.ParsePackYAML([]byte(bad))
	if !errors.Is(err, benchmark.ErrUnknownAPIVersion) {
		t.Errorf("err = %v, want ErrUnknownAPIVersion", err)
	}
}

func TestParsePackYAMLRejectsUnknownFields(t *testing.T) {
	t.Parallel()
	bad := validPackYAML + "extra: value\n"
	_, err := benchmark.ParsePackYAML([]byte(bad))
	if !errors.Is(err, benchmark.ErrInvalidYAML) {
		t.Errorf("err = %v, want ErrInvalidYAML (KnownFields strict)", err)
	}
}

func TestParseCheckYAMLDefaultsSeverityToMedium(t *testing.T) {
	t.Parallel()
	noSev := strings.Replace(validCheckYAML, "  severity: high\n", "", 1)
	check, err := benchmark.ParseCheckYAML([]byte(noSev))
	if err != nil {
		t.Fatalf("ParseCheckYAML: %v", err)
	}
	if check.Severity != benchmark.SeverityMedium {
		t.Errorf("Severity = %q, want medium (default)", check.Severity)
	}
}

func TestParseCheckYAMLRejectsInvalidSeverity(t *testing.T) {
	t.Parallel()
	bad := strings.Replace(validCheckYAML, "severity: high", "severity: catastrophic", 1)
	_, err := benchmark.ParseCheckYAML([]byte(bad))
	if !errors.Is(err, benchmark.ErrInvalidSeverity) {
		t.Errorf("err = %v, want ErrInvalidSeverity", err)
	}
}

func TestParsePackYAMLRequiresMetadata(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"missing name":    strings.Replace(validPackYAML, "  name: cis-ubuntu-2404\n", "", 1),
		"missing version": strings.Replace(validPackYAML, "  version: v1.0.0\n", "", 1),
		"missing vendor":  strings.Replace(validPackYAML, "  vendor: CIS\n", "", 1),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := benchmark.ParsePackYAML([]byte(body))
			if !errors.Is(err, benchmark.ErrSchemaViolation) {
				t.Errorf("err = %v, want ErrSchemaViolation", err)
			}
		})
	}
}
