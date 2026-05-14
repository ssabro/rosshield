// CIS apparmor count 합성 — G8 단위 테스트.

package converter

import (
	"strings"
	"testing"
)

const audit_1_3_1_3 = `Run the following command and verify that profiles are loaded, and are in either enforce or complain mode:
# apparmor_status | grep profiles
Review output and ensure that profiles are loaded, and in either enforce or complain mode:
37 profiles are loaded.
35 profiles are in enforce mode.
2 profiles are in complain mode.
4 processes have profiles defined.
Run the following command and verify no processes are unconfined
# apparmor_status | grep processes
Review the output and ensure no processes are unconfined:
4 processes have profiles defined.
4 processes are in enforce mode.
0 processes are in complain mode.
0 processes are unconfined but have a profile defined.`

const audit_1_3_1_4 = `Run the following commands and verify that profiles are loaded and are not in complain mode:
# apparmor_status | grep profiles
Review output and ensure that profiles are loaded, and in enforce mode:
34 profiles are loaded.
34 profiles are in enforce mode.
0 profiles are in complain mode.
2 processes have profiles defined.
Run the following command and verify that no processes are unconfined:
apparmor_status | grep processes
Review the output and ensure no processes are unconfined:
2 processes have profiles defined.
2 processes are in enforce mode.
0 processes are in complain mode.
0 processes are unconfined but have a profile defined.`

func TestIsApparmorCountAuditText_Positive(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, audit string }{
		{"1.3.1.3 either", audit_1_3_1_3},
		{"1.3.1.4 strict (not in complain)", audit_1_3_1_4},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if !isApparmorCountAuditText(tc.audit) {
				t.Errorf("isApparmorCountAuditText = false, want true")
			}
		})
	}
}

func TestIsApparmorCountAuditText_Negative(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, audit string }{
		{"non-apparmor (sshd)", audit_nonGsettings},
		{"non-apparmor (gsettings)", audit_1_7_6},
		{"empty", ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if isApparmorCountAuditText(tc.audit) {
				t.Errorf("isApparmorCountAuditText = true, want false")
			}
		})
	}
}

func TestExtractApparmorAudit_ModeDetection(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		audit    string
		wantMode apparmorMode
	}{
		{"1.3.1.3 either mode", audit_1_3_1_3, apparmorEitherMode},
		{"1.3.1.4 strict mode (not in complain)", audit_1_3_1_4, apparmorStrictMode},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mode, ok := extractApparmorAudit(tc.audit)
			if !ok {
				t.Fatal("ok = false")
			}
			if mode != tc.wantMode {
				t.Errorf("mode = %v, want %v", mode, tc.wantMode)
			}
		})
	}
}

func TestSynthesizeApparmorCount_Output(t *testing.T) {
	t.Parallel()
	t.Run("either mode (1.3.1.3) — complain check 부재", func(t *testing.T) {
		t.Parallel()
		bash, ok := synthesizeApparmorCount(audit_1_3_1_3)
		if !ok {
			t.Fatal("ok = false")
		}
		want := []string{
			"profiles_out=$(apparmor_status",
			`[ "$loaded" -gt 0 ]`,
			`[ "$unconfined" -eq 0 ]`,
			`** PASS **`,
		}
		for _, w := range want {
			if !strings.Contains(bash, w) {
				t.Errorf("output missing %q\n  bash=%s", w, bash)
			}
		}
		// either mode는 complain check 미포함.
		if strings.Contains(bash, `[ "$complain" -eq 0 ]`) {
			t.Errorf("either mode unexpected complain check:\n  bash=%s", bash)
		}
	})

	t.Run("strict mode (1.3.1.4) — complain check 포함", func(t *testing.T) {
		t.Parallel()
		bash, ok := synthesizeApparmorCount(audit_1_3_1_4)
		if !ok {
			t.Fatal("ok = false")
		}
		if !strings.Contains(bash, `[ "$complain" -eq 0 ]`) {
			t.Errorf("strict mode missing complain check:\n  bash=%s", bash)
		}
	})
}
