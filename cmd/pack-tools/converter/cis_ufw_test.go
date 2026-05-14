// CIS ufw status default policy 합성 — G6 4.2.7 단위 테스트.

package converter

import (
	"strings"
	"testing"
)

const audit_4_2_7 = `Run the following command and verify that the default policy for incoming, outgoing, and routed directions is deny, reject, or disabled:
# ufw status verbose | grep Default:
Example output:
Default: deny (incoming), deny (outgoing), disabled (routed)`

// 4.2.4: multi-line table + 2 cmd — 본 합성기 미지원(별 epic).
const audit_4_2_4 = `Run: # grep -P -- 'lo|127.0.0.0' /etc/ufw/before.rules
Output includes:
-A ufw-before-input -i lo -j ACCEPT`

func TestIsUfwStatusDefaultAuditText_Positive(t *testing.T) {
	t.Parallel()
	if !isUfwStatusDefaultAuditText(audit_4_2_7) {
		t.Errorf("isUfwStatusDefaultAuditText(4.2.7) = false, want true")
	}
}

func TestIsUfwStatusDefaultAuditText_Negative(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, audit string }{
		{"4.2.4 (다른 cmd 형식, 별 epic)", audit_4_2_4},
		{"non-ufw (sshd)", audit_nonGsettings},
		{"empty", ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if isUfwStatusDefaultAuditText(tc.audit) {
				t.Errorf("isUfwStatusDefaultAuditText = true, want false")
			}
		})
	}
}

func TestSynthesizeUfwStatusDefault_Output(t *testing.T) {
	t.Parallel()
	bash, ok := synthesizeUfwStatusDefault(audit_4_2_7)
	if !ok {
		t.Fatal("ok = false")
	}
	want := []string{
		`out=$(ufw status verbose 2>/dev/null | grep -i '^Default:')`,
		`grep -qiE '(deny|reject|disabled)'`,
		`** PASS **`,
		`** FAIL **`,
	}
	for _, w := range want {
		if !strings.Contains(bash, w) {
			t.Errorf("output missing %q\n  bash=%s", w, bash)
		}
	}
}
