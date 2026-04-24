package benchmark_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ssabro/rosshield/internal/domain/benchmark"
)

// E4.T4 본체.
func TestCheckDefinitionSchemaValidation(t *testing.T) {
	t.Parallel()

	// 정상 — schema 통과.
	if err := benchmark.ValidateCheckYAMLBytes([]byte(validCheckYAML)); err != nil {
		t.Errorf("valid check schema: %v", err)
	}
	if err := benchmark.ValidatePackYAMLBytes([]byte(validPackYAML)); err != nil {
		t.Errorf("valid pack schema: %v", err)
	}

	cases := map[string]string{
		"missing id":         strings.Replace(validCheckYAML, "  id: CIS-1.1.1.1\n", "", 1),
		"missing title":      strings.Replace(validCheckYAML, `  title: "Disable cramfs kernel module"`+"\n", "", 1),
		"unknown field":      validCheckYAML + "extraTopLevel: bad\n",
		"wrong apiVersion":   strings.Replace(validCheckYAML, "rosshield.io/v1", "rosshield.io/v999", 1),
		"wrong kind":         strings.Replace(validCheckYAML, "kind: Check", "kind: NotACheck", 1),
		"invalid severity":   strings.Replace(validCheckYAML, "severity: high", "severity: catastrophic", 1),
		"invalid id pattern": strings.Replace(validCheckYAML, "id: CIS-1.1.1.1", "id: $$$invalid$$$", 1),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			err := benchmark.ValidateCheckYAMLBytes([]byte(body))
			if err == nil {
				t.Errorf("expected schema violation, got nil")
			}
			if err != nil && !errors.Is(err, benchmark.ErrSchemaViolation) && !errors.Is(err, benchmark.ErrInvalidYAML) {
				t.Errorf("err = %v, want ErrSchemaViolation or ErrInvalidYAML", err)
			}
		})
	}
}

func TestPackSchemaRejectsInvalidVersion(t *testing.T) {
	t.Parallel()

	bad := strings.Replace(validPackYAML, "version: v1.0.0", "version: not-a-semver", 1)
	err := benchmark.ValidatePackYAMLBytes([]byte(bad))
	if err == nil {
		t.Error("expected schema violation for invalid version pattern")
	}
}

func TestPackSchemaRejectsInvalidName(t *testing.T) {
	t.Parallel()

	bad := strings.Replace(validPackYAML, "name: cis-ubuntu-2404", "name: UPPER_CASE_BAD", 1)
	err := benchmark.ValidatePackYAMLBytes([]byte(bad))
	if err == nil {
		t.Error("expected schema violation for uppercase name")
	}
}
