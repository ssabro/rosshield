// CIS grub.cfg multi-line verify 합성 — G14 단위 테스트.

package converter

import (
	"strings"
	"testing"
)

const audit_1_4_1 = `Run the following commands and verify output matches:
# grep "^set superusers" /boot/grub/grub.cfg
set superusers="<username>"
# awk -F. '/^\s*password/ {print $1"."$2"."$3}' /boot/grub/grub.cfg
password_pbkdf2 <username> grub.pbkdf2.sha512`

func TestIsGrubCfgAuditText_Positive(t *testing.T) {
	t.Parallel()
	if !isGrubCfgAuditText(audit_1_4_1) {
		t.Errorf("isGrubCfgAuditText(1.4.1) = false, want true")
	}
}

func TestIsGrubCfgAuditText_Negative(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, audit string }{
		{
			name: "단일 cmd grub.cfg (2 미달)",
			audit: `# grep "^set superusers" /boot/grub/grub.cfg
set superusers="x"`,
		},
		{name: "non-grub (sshd)", audit: audit_nonGsettings},
		{name: "non-grub (passwd)", audit: audit_5_4_2_3},
		{name: "empty", audit: ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if isGrubCfgAuditText(tc.audit) {
				t.Errorf("isGrubCfgAuditText = true, want false")
			}
		})
	}
}

func TestSplitByPlaceholder(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"single placeholder", `set superusers="<username>"`, []string{`set superusers="`, `"`}},
		{"placeholder middle", `password_pbkdf2 <username> grub.pbkdf2.sha512`, []string{"password_pbkdf2 ", " grub.pbkdf2.sha512"}},
		{"no placeholder", "plain text", []string{"plain text"}},
		{"placeholder only", "<x>", []string{"", ""}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := splitByPlaceholder(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("len = %d, want %d (got: %#v)", len(got), len(tc.want), got)
			}
			for i, w := range tc.want {
				if got[i] != w {
					t.Errorf("got[%d] = %q, want %q", i, got[i], w)
				}
			}
		})
	}
}

func TestExtractGrubChecks(t *testing.T) {
	t.Parallel()
	checks, ok := extractGrubChecks(audit_1_4_1)
	if !ok {
		t.Fatal("ok = false")
	}
	if len(checks) != 2 {
		t.Fatalf("len = %d, want 2", len(checks))
	}
	// cmd 1 — set superusers
	if !strings.Contains(checks[0].cmd, "set superusers") {
		t.Errorf("checks[0].cmd missing 'set superusers' (cmd=%q)", checks[0].cmd)
	}
	// 토큰: `set superusers="`, `"` (둘 다 trim 후 non-empty)
	if len(checks[0].tokens) != 2 {
		t.Errorf("checks[0].tokens count = %d, want 2 (%#v)", len(checks[0].tokens), checks[0].tokens)
	}
	// cmd 2 — password_pbkdf2
	if !strings.Contains(checks[1].tokens[0], "password_pbkdf2") {
		t.Errorf("checks[1].tokens[0] missing 'password_pbkdf2' (%q)", checks[1].tokens[0])
	}
}

func TestSynthesizeGrubCfg_Output(t *testing.T) {
	t.Parallel()
	bash, ok := synthesizeGrubCfg(audit_1_4_1)
	if !ok {
		t.Fatal("ok = false")
	}
	want := []string{
		`out_0=$(grep "^set superusers" /boot/grub/grub.cfg`,
		`out_1=$(awk -F.`,
		`grep -qF -- "set superusers=\""`,
		`grep -qF -- "password_pbkdf2"`,
		`** PASS **`,
		`** FAIL **`,
	}
	for _, w := range want {
		if !strings.Contains(bash, w) {
			t.Errorf("output missing %q\n  bash=%s", w, bash)
		}
	}
}
