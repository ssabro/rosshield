// CIS multi-cmd grep alternation 합성 — G15 단위 테스트.
//
// 5 unit:
//   - positive 1건: 6.2.2.3 (2 cmd × auditd.conf, path continuation join)
//   - negative 4건: 단일 cmd / 다른 path / non-grep / empty

package converter

import (
	"strings"
	"testing"
)

// 6.2.2.3 audit text 발췌 (path 다음 라인으로 wrap)
const audit_6_2_2_3 = `Run the following command and verify the disk_full_action is set to either halt or single:
# grep -Pi -- '^\h*disk_full_action\h*=\h*(halt|single)\b'
/etc/audit/auditd.conf
disk_full_action = <halt|single>
Run the following command and verify the disk_error_action is set to syslog, single, or halt:
# grep -Pi -- '^\h*disk_error_action\h*=\h*(syslog|single|halt)\b'
/etc/audit/auditd.conf
disk_error_action = <syslog|single|halt>`

func TestIsMultiGrepAuditdAuditText_Positive(t *testing.T) {
	t.Parallel()
	if !isMultiGrepAuditdAuditText(audit_6_2_2_3) {
		t.Errorf("isMultiGrepAuditdAuditText = false, want true")
	}
}

func TestIsMultiGrepAuditdAuditText_Negative(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, audit string
	}{
		{
			name: "단일 cmd (cmd 1개만)",
			audit: `# grep -Pi -- '^\h*disk_full_action\h*=\h*(halt|single)\b'
/etc/audit/auditd.conf`,
		},
		{
			name: "다른 path (auditd.conf 아님)",
			audit: `# grep -Pi -- 'foo' /etc/passwd
# grep -Pi -- 'bar' /etc/passwd`,
		},
		{
			name:  "non-grep (sshd)",
			audit: `# sshd -T | grep loglevel`,
		},
		{
			name:  "empty",
			audit: "",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if isMultiGrepAuditdAuditText(tc.audit) {
				t.Errorf("isMultiGrepAuditdAuditText = true, want false")
			}
		})
	}
}

func TestSynthesizeMultiGrepAuditd_Output(t *testing.T) {
	t.Parallel()
	bash, ok := synthesizeMultiGrepAuditd(audit_6_2_2_3)
	if !ok {
		t.Fatal("ok = false")
	}
	want := []string{
		`out_0=$(grep -Pi -- '^\h*disk_full_action\h*=\h*(halt|single)\b' /etc/audit/auditd.conf 2>/dev/null)`,
		`out_1=$(grep -Pi -- '^\h*disk_error_action\h*=\h*(syslog|single|halt)\b' /etc/audit/auditd.conf 2>/dev/null)`,
		`[ -n "$out_0" ]`,
		`[ -n "$out_1" ]`,
		`** PASS **`,
		`** FAIL **`,
	}
	for _, w := range want {
		if !strings.Contains(bash, w) {
			t.Errorf("output missing %q\n  bash=%s", w, bash)
		}
	}
}
