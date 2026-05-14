// CIS sudo cache timeout 합성 — G13 5.2.6 단위 테스트.

package converter

import (
	"strings"
	"testing"
)

const audit_5_2_6 = `Ensure that the caching timeout is no more than 15 minutes.
Example:
# grep -roP "timestamp_timeout=\K[0-9]*" /etc/sudoers*
If there is no timestamp_timeout configured in /etc/sudoers* then the default is 15 minutes. This default can be checked with:
# sudo -V | grep "Authentication timestamp timeout:"`

func TestIsSudoTimestampTimeoutAuditText_Positive(t *testing.T) {
	t.Parallel()
	if !isSudoTimestampTimeoutAuditText(audit_5_2_6) {
		t.Errorf("isSudoTimestampTimeoutAuditText(5.2.6) = false, want true")
	}
}

func TestIsSudoTimestampTimeoutAuditText_Negative(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, audit string }{
		{"timestamp_timeout 키워드 없음", `# grep -roP "foo=\K[0-9]*" /etc/sudoers*
no more than 15 minutes`},
		{"phrase 없음", `# grep -roP "timestamp_timeout=\K[0-9]*" /etc/sudoers*`},
		{"non-sudo (sshd)", audit_nonGsettings},
		{"empty", ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if isSudoTimestampTimeoutAuditText(tc.audit) {
				t.Errorf("isSudoTimestampTimeoutAuditText = true, want false")
			}
		})
	}
}

func TestSynthesizeSudoTimestampTimeout_Output(t *testing.T) {
	t.Parallel()
	bash, ok := synthesizeSudoTimestampTimeout(audit_5_2_6)
	if !ok {
		t.Fatal("ok = false")
	}
	want := []string{
		`val=$(grep -roP "timestamp_timeout=\K[0-9-]+" /etc/sudoers*`,
		`[ -z "$val" ] && val=15`,
		`[ "$val" -le 15 ]`,
		`** PASS **`,
		`** FAIL **`,
		`-*|*[!0-9-]*)`, // invalid case
	}
	for _, w := range want {
		if !strings.Contains(bash, w) {
			t.Errorf("output missing %q\n  bash=%s", w, bash)
		}
	}
}
