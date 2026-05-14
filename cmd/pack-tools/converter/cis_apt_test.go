// CIS apt updates 합성 — 1.2.2.1 단위 테스트.

package converter

import (
	"strings"
	"testing"
)

const audit_1_2_2_1 = `Verify there are no updates or patches to install:
# apt update
# apt -s upgrade`

func TestIsAptNoUpdatesAuditText_Positive(t *testing.T) {
	t.Parallel()
	if !isAptNoUpdatesAuditText(audit_1_2_2_1) {
		t.Errorf("isAptNoUpdatesAuditText(1.2.2.1) = false, want true")
	}
}

func TestIsAptNoUpdatesAuditText_Negative(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, audit string }{
		{"phrase 없음", `# apt update
# apt -s upgrade`},
		{"non-apt (sshd)", audit_nonGsettings},
		{"empty", ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if isAptNoUpdatesAuditText(tc.audit) {
				t.Errorf("isAptNoUpdatesAuditText = true, want false")
			}
		})
	}
}

func TestSynthesizeAptNoUpdates_Output(t *testing.T) {
	t.Parallel()
	bash, ok := synthesizeAptNoUpdates(audit_1_2_2_1)
	if !ok {
		t.Fatal("ok = false")
	}
	want := []string{
		`count=$(apt -s upgrade 2>/dev/null | grep -cE '^Inst\s+')`,
		`if [ "$count" -eq 0 ]`,
		`** PASS **`,
		`** FAIL **`,
	}
	for _, w := range want {
		if !strings.Contains(bash, w) {
			t.Errorf("output missing %q\n  bash=%s", w, bash)
		}
	}
}
