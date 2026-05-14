// CIS file stat 옵트 합성 — G12 단위 테스트.

package converter

import (
	"strings"
	"testing"
)

const audit_1_6_4 = `Run the following command and verify that if /etc/motd exists, Access is 644 or more restrictive, Uid and Gid are both 0/root:
# [ -e /etc/motd ] && stat -Lc 'Access: (%#a/%A) Uid: ( %u/ %U) Gid: ( %g/ %G)' /etc/motd
Access: (0644/-rw-r--r--) Uid: ( 0/ root) Gid: ( 0/ root)
-- OR --
Nothing is returned`

const audit_7_1_10 = `Run the following commands to verify /etc/security/opasswd and /etc/security/opasswd.old are mode 600 or more restrictive:
# [ -e "/etc/security/opasswd" ] && stat -Lc '%n Access: (%#a/%A) Uid: ( %u/ %U) Gid: ( %g/ %G)' /etc/security/opasswd
/etc/security/opasswd Access: (0600/-rw-------) Uid: ( 0/ root) Gid: ( 0/ root)
-OR-
Nothing is returned
# [ -e "/etc/security/opasswd.old" ] && stat -Lc '%n Access: (%#a/%A) Uid: ( %u/ %U) Gid: ( %g/ %G)' /etc/security/opasswd.old
/etc/security/opasswd.old Access: (0600/-rw-------) Uid: ( 0/ root) Gid: ( 0/ root)
-OR-
Nothing is returned`

func TestIsStatOptAuditText_Positive(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, audit string }{
		{"1.6.4 (single path)", audit_1_6_4},
		{"7.1.10 (multi path)", audit_7_1_10},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if !isStatOptAuditText(tc.audit) {
				t.Errorf("isStatOptAuditText = false, want true")
			}
		})
	}
}

func TestIsStatOptAuditText_Negative(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, audit string }{
		{
			name: "stat 있지만 [ -e ] 가드 없음",
			audit: `# stat -Lc '%a' /etc/foo
0644
Nothing is returned`,
		},
		{
			name: "Nothing returned phrase 없음",
			audit: `# [ -e /etc/motd ] && stat -Lc '%a' /etc/motd
0644`,
		},
		{name: "non-stat (sshd)", audit: audit_nonGsettings},
		{name: "empty", audit: ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if isStatOptAuditText(tc.audit) {
				t.Errorf("isStatOptAuditText = true, want false")
			}
		})
	}
}

func TestExtractStatOptChecks(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		audit       string
		wantNChecks int
		wantPaths   []string
	}{
		{"1.6.4 single", audit_1_6_4, 1, []string{"/etc/motd"}},
		{"7.1.10 multi", audit_7_1_10, 2, []string{"/etc/security/opasswd", "/etc/security/opasswd.old"}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			checks, ok := extractStatOptChecks(tc.audit)
			if !ok {
				t.Fatal("ok = false")
			}
			if len(checks) != tc.wantNChecks {
				t.Fatalf("checks count = %d, want %d", len(checks), tc.wantNChecks)
			}
			for i, p := range tc.wantPaths {
				if !strings.Contains(checks[i].cmd, p) {
					t.Errorf("check[%d].cmd missing %q (cmd=%q)", i, p, checks[i].cmd)
				}
				if len(checks[i].expecteds) == 0 {
					t.Errorf("check[%d].expecteds empty", i)
				}
			}
		})
	}
}

func TestSynthesizeStatOpt_Output(t *testing.T) {
	t.Parallel()
	bash, ok := synthesizeStatOpt(audit_1_6_4)
	if !ok {
		t.Fatal("ok = false")
	}
	want := []string{
		"out_0=$([ -e /etc/motd ] && stat -Lc",
		`if [ -n "$out_0" ]; then`,
		`grep -qF -- "Access: (0644/-rw-r--r--) Uid: ( 0/ root) Gid: ( 0/ root)"`,
		`** PASS **`,
		`** FAIL **`,
	}
	for _, w := range want {
		if !strings.Contains(bash, w) {
			t.Errorf("output missing %q\n  bash=%s", w, bash)
		}
	}
}
