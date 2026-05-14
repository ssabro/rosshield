// CIS gsettings get scalar 합성 — E-1 G7-bool 단위 테스트.
//
// 5 unit:
//   - positive 2건: 1.7.6 (2 cmd × false) + 1.7.8 (1 cmd × true)
//   - negative 3건: 1.7.4 (uint32 N — boolean 아님 → 보류) + 비-gsettings + empty

package converter

import (
	"strings"
	"testing"
)

const audit_1_7_6 = `Run the following commands to verify automatic mounting is disabled:
# gsettings get org.gnome.desktop.media-handling automount
false
# gsettings get org.gnome.desktop.media-handling automount-open
false`

const audit_1_7_8 = `Run the following command to verify that autorun-never is set to true for GDM:
# gsettings get org.gnome.desktop.media-handling autorun-never
true`

// 1.7.4: uint32 N — boolean 아님 → 인식 보류(false positive 회피).
const audit_1_7_4 = `Run the following commands to verify that the screen locks when the user is idle:
# gsettings get org.gnome.desktop.screensaver lock-delay
uint32 5
# gsettings get org.gnome.desktop.session idle-delay
uint32 900`

// 비-gsettings: 다른 도메인 (sshd numeric).
const audit_nonGsettings = `Run the following:
# sshd -T | grep clientaliveinterval
clientaliveinterval 15`

func TestIsGsettingsBoolAuditText_Positive(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, audit string
	}{
		{"1.7.6 (2 cmd × false)", audit_1_7_6},
		{"1.7.8 (1 cmd × true)", audit_1_7_8},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if !isGsettingsBoolAuditText(tc.audit) {
				t.Errorf("isGsettingsBoolAuditText = false, want true")
			}
		})
	}
}

func TestIsGsettingsBoolAuditText_Negative(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, audit string
	}{
		{"1.7.4 uint32 (boolean 아님)", audit_1_7_4},
		{"non-gsettings (sshd)", audit_nonGsettings},
		{"empty", ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if isGsettingsBoolAuditText(tc.audit) {
				t.Errorf("isGsettingsBoolAuditText = true, want false")
			}
		})
	}
}

func TestSynthesizeGsettingsBool_OutputContainsExpected(t *testing.T) {
	t.Parallel()
	bash, ok := synthesizeGsettingsBool(audit_1_7_6)
	if !ok {
		t.Fatal("ok = false")
	}
	want := []string{
		"val=$(gsettings get org.gnome.desktop.media-handling automount 2>/dev/null)",
		"val=$(gsettings get org.gnome.desktop.media-handling automount-open 2>/dev/null)",
		`[ "$val" = "false" ]`,
		`** PASS **`,
		`** FAIL **`,
	}
	for _, w := range want {
		if !strings.Contains(bash, w) {
			t.Errorf("output missing %q\n  bash=%s", w, bash)
		}
	}
}

// === G7-uint32 (1.7.4 — 2 cmd × uint32 N) ===

func TestIsGsettingsUint32AuditText_Positive(t *testing.T) {
	t.Parallel()
	if !isGsettingsUint32AuditText(audit_1_7_4) {
		t.Errorf("isGsettingsUint32AuditText(1.7.4) = false, want true")
	}
}

func TestIsGsettingsUint32AuditText_Negative(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, audit string }{
		{"1.7.6 boolean (uint32 아님)", audit_1_7_6},
		{"1.7.8 boolean (uint32 아님)", audit_1_7_8},
		{"non-gsettings (sshd)", audit_nonGsettings},
		{"empty", ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if isGsettingsUint32AuditText(tc.audit) {
				t.Errorf("isGsettingsUint32AuditText = true, want false")
			}
		})
	}
}

func TestExtractGsettingsUint32Checks_Thresholds(t *testing.T) {
	t.Parallel()
	checks, ok := extractGsettingsUint32Checks(audit_1_7_4)
	if !ok {
		t.Fatal("ok = false")
	}
	if len(checks) != 2 {
		t.Fatalf("checks count = %d, want 2", len(checks))
	}
	want := []gsettingsUint32Check{
		{schema: "org.gnome.desktop.screensaver", key: "lock-delay", threshold: 5},
		{schema: "org.gnome.desktop.session", key: "idle-delay", threshold: 900},
	}
	for i, w := range want {
		if checks[i] != w {
			t.Errorf("check[%d] = %+v, want %+v", i, checks[i], w)
		}
	}
}

func TestSynthesizeGsettingsUint32_Output(t *testing.T) {
	t.Parallel()
	bash, ok := synthesizeGsettingsUint32(audit_1_7_4)
	if !ok {
		t.Fatal("ok = false")
	}
	want := []string{
		"raw=$(gsettings get org.gnome.desktop.screensaver lock-delay 2>/dev/null)",
		"raw=$(gsettings get org.gnome.desktop.session idle-delay 2>/dev/null)",
		`val=${raw#uint32 }`,
		`[ "$val" -le 5 ]`,
		`[ "$val" -le 900 ]`,
		`** PASS **`,
		`** FAIL **`,
	}
	for _, w := range want {
		if !strings.Contains(bash, w) {
			t.Errorf("output missing %q\n  bash=%s", w, bash)
		}
	}
}
