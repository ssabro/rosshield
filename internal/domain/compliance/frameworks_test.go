package compliance_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ssabro/rosshield/internal/domain/compliance"
)

func TestLoadFrameworkISMSPHasControls(t *testing.T) {
	t.Parallel()

	controls, version, err := compliance.LoadFramework(compliance.FrameworkISMSP)
	if err != nil {
		t.Fatalf("LoadFramework(isms-p): %v", err)
	}
	if version != "2024" {
		t.Errorf("version = %q, want 2024", version)
	}
	if len(controls) < 30 {
		t.Errorf("len(controls) = %d, want >= 30 (Phase 2 baseline)", len(controls))
	}
	for _, c := range controls {
		if !strings.HasPrefix(c.ID, "ISMS-P:") {
			t.Errorf("control ID %q does not have ISMS-P: prefix", c.ID)
		}
		if c.Title == "" {
			t.Errorf("control %s has empty title", c.ID)
		}
		if !strings.HasPrefix(c.ReferenceURL, "https://") {
			t.Errorf("control %s has invalid referenceUrl %q", c.ID, c.ReferenceURL)
		}
	}
}

func TestLoadFrameworkISO27001(t *testing.T) {
	t.Parallel()

	controls, version, err := compliance.LoadFramework(compliance.FrameworkISO27001)
	if err != nil {
		t.Fatalf("LoadFramework(iso27001-2022): %v", err)
	}
	if version != "2022" {
		t.Errorf("version = %q, want 2022", version)
	}
	if len(controls) < 30 {
		t.Errorf("len(controls) = %d, want >= 30", len(controls))
	}
	for _, c := range controls {
		if !strings.HasPrefix(c.ID, "ISO27001:A.") {
			t.Errorf("control ID %q does not have ISO27001:A. prefix", c.ID)
		}
	}
}

func TestLoadFrameworkNIST(t *testing.T) {
	t.Parallel()

	controls, version, err := compliance.LoadFramework(compliance.FrameworkNIST)
	if err != nil {
		t.Fatalf("LoadFramework(nist-800-53-rev5): %v", err)
	}
	if version == "" {
		t.Errorf("version is empty")
	}
	if len(controls) < 30 {
		t.Errorf("len(controls) = %d, want >= 30", len(controls))
	}

	families := map[string]int{}
	for _, c := range controls {
		if !strings.HasPrefix(c.ID, "NIST:") {
			t.Errorf("control ID %q does not have NIST: prefix", c.ID)
			continue
		}
		// "NIST:AC-1" → family = "AC"
		body := strings.TrimPrefix(c.ID, "NIST:")
		dash := strings.Index(body, "-")
		if dash <= 0 {
			t.Errorf("control ID %q malformed (no - after family)", c.ID)
			continue
		}
		families[body[:dash]]++
	}
	// Phase 2 baseline: 5 family × 6 controls.
	wantFamilies := []string{"AC", "AU", "CP", "IA", "SC"}
	for _, f := range wantFamilies {
		if families[f] == 0 {
			t.Errorf("missing NIST family %s", f)
		}
	}
}

func TestLoadFrameworkUnknownReturnsError(t *testing.T) {
	t.Parallel()

	_, _, err := compliance.LoadFramework("nope-not-a-framework")
	if !errors.Is(err, compliance.ErrUnknownFramework) {
		t.Errorf("err = %v, want ErrUnknownFramework", err)
	}
}

func TestFrameworkYAMLValidatesRequiredFields(t *testing.T) {
	t.Parallel()

	// 모든 framework에 대해 ID·Title이 비어있지 않은지 검증.
	frameworks := []compliance.Framework{
		compliance.FrameworkISMSP,
		compliance.FrameworkISO27001,
		compliance.FrameworkNIST,
	}
	for _, f := range frameworks {
		controls, _, err := compliance.LoadFramework(f)
		if err != nil {
			t.Errorf("%s: load: %v", f, err)
			continue
		}
		seen := map[string]bool{}
		for _, c := range controls {
			if strings.TrimSpace(c.ID) == "" {
				t.Errorf("%s: empty control ID", f)
			}
			if strings.TrimSpace(c.Title) == "" {
				t.Errorf("%s/%s: empty title", f, c.ID)
			}
			if seen[c.ID] {
				t.Errorf("%s: duplicate control ID %s", f, c.ID)
			}
			seen[c.ID] = true
			// R14-2 저작권 안전: 요약 200자 한도.
			if n := len([]rune(c.Summary)); n > 200 {
				t.Errorf("%s/%s: summary %d chars > 200", f, c.ID, n)
			}
		}
	}
}
