// CIS passwd/group awk 합성 — G16 단위 테스트.

package converter

import (
	"strings"
	"testing"
)

const audit_5_4_2_2 = `Run the following command to verify the root user's primary GID is 0, and no other user's have GID 0 as their primary GID:
# awk -F: '($1 !~ /^(sync|shutdown|halt|operator)/ && $4=="0") {print $1":"$4}' /etc/passwd
root:0
Note: User's: sync, shutdown, halt, and operator are excluded from the check`

const audit_5_4_2_3 = `Run the following command to verify no group other than root is assigned GID 0:
# awk -F: '$3=="0"{print $1":"$3}' /etc/group
root:0`

const audit_5_4_2_4 = `Run the following command to verify that either the root user's password is set or the root user's account is locked:
# passwd -S root | awk '$2 ~ /^(P|L)/ {print "User: \"" $1 "\" Password is status: " $2}'
Verify the output is either:
User: "root" Password is status: P
- OR -
User: "root" Password is status: L
Note:
• P - Password is set
• L - Password is locked`

func TestIsPasswdAwkAuditText_Positive(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, audit string }{
		{"5.4.2.2 exactRoot (passwd GID)", audit_5_4_2_2},
		{"5.4.2.3 exactRoot (group GID)", audit_5_4_2_3},
		{"5.4.2.4 alternation (passwd -S)", audit_5_4_2_4},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if !isPasswdAwkAuditText(tc.audit) {
				t.Errorf("isPasswdAwkAuditText = false, want true")
			}
		})
	}
}

func TestIsPasswdAwkAuditText_Negative(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, audit string }{
		{"non-passwd (apparmor)", audit_1_3_1_3},
		{"non-passwd (sshd)", audit_nonGsettings},
		{"empty", ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if isPasswdAwkAuditText(tc.audit) {
				t.Errorf("isPasswdAwkAuditText = true, want false")
			}
		})
	}
}

func TestExtractPasswdCheck_ModeDetection(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		audit       string
		wantMode    passwdMode
		wantExpsLen int
		wantExp0Sub string
	}{
		{"5.4.2.2 exactRoot", audit_5_4_2_2, passwdExactRootMode, 1, "root:0"},
		{"5.4.2.3 exactRoot", audit_5_4_2_3, passwdExactRootMode, 1, "root:0"},
		{"5.4.2.4 alternation 2 lines", audit_5_4_2_4, passwdAlternationMode, 2, "Password is status: P"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pc, ok := extractPasswdCheck(tc.audit)
			if !ok {
				t.Fatal("ok = false")
			}
			if pc.mode != tc.wantMode {
				t.Errorf("mode = %v, want %v", pc.mode, tc.wantMode)
			}
			if len(pc.expecteds) != tc.wantExpsLen {
				t.Errorf("expecteds count = %d, want %d (%#v)", len(pc.expecteds), tc.wantExpsLen, pc.expecteds)
			}
			if !strings.Contains(pc.expecteds[0], tc.wantExp0Sub) {
				t.Errorf("expecteds[0] = %q, want substring %q", pc.expecteds[0], tc.wantExp0Sub)
			}
		})
	}
}

func TestSynthesizePasswdAwk_ExactRootMode(t *testing.T) {
	t.Parallel()
	bash, ok := synthesizePasswdAwk(audit_5_4_2_3)
	if !ok {
		t.Fatal("ok = false")
	}
	want := []string{
		"out=$(awk -F:",
		`trimmed=$(printf '%s' "$out" | awk '{$1=$1};1')`,
		`if [ "$trimmed" = "root:0" ]`,
		`** PASS **`,
		`** FAIL **`,
	}
	for _, w := range want {
		if !strings.Contains(bash, w) {
			t.Errorf("output missing %q\n  bash=%s", w, bash)
		}
	}
}

func TestSynthesizePasswdAwk_AlternationMode(t *testing.T) {
	t.Parallel()
	bash, ok := synthesizePasswdAwk(audit_5_4_2_4)
	if !ok {
		t.Fatal("ok = false")
	}
	want := []string{
		"out=$(passwd -S root",
		"for token in",
		`"User: \"root\" Password is status: P"`,
		`"User: \"root\" Password is status: L"`,
		"grep -qF",
		`** PASS **`,
	}
	for _, w := range want {
		if !strings.Contains(bash, w) {
			t.Errorf("output missing %q\n  bash=%s", w, bash)
		}
	}
}
