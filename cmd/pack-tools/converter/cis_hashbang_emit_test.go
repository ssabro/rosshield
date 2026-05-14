// CIS hashbang body PASSED/FAILED emit 합성 — G10 5.4.3.2 단위 테스트.

package converter

import (
	"strings"
	"testing"
)

const audit_5_4_3_2 = `Run the following script to verify that TMOUT is configured to: include a timeout of no more than 900 seconds, to be readonly, to be exported, and is not being changed to a longer timeout.
#!/usr/bin/env bash
{
output1="" output2=""
[ -f /etc/bashrc ] && BRC="/etc/bashrc"
for f in "$BRC" /etc/profile /etc/profile.d/*.sh ; do
grep -Pq '^\s*([^#]+\s+)?TMOUT=(900|[1-8][0-9][0-9]|[1-9][0-9]|[1-9])\b' "$f" && output1="$f"
done
if [ -n "$output1" ] && [ -z "$output2" ]; then
echo -e "\nPASSED\n\nTMOUT is configured in: \"$output1\"\n"
else
[ -z "$output1" ] && echo -e "\nFAILED\n\nTMOUT is not configured\n"
[ -n "$output2" ] && echo -e "\nFAILED\n\nTMOUT is incorrectly configured\n"
fi
}`

// 5.4.1.6: shebang 없는 `{}` block + expect-empty — 본 합성기 미지원 (G10 별 fix).
const audit_5_4_1_6 = `Run the following command and verify nothing is returned
{
while IFS= read -r l_user; do
echo "User: \"$l_user\""
done
}`

func TestIsHashbangPassFailEmitAuditText_Positive(t *testing.T) {
	t.Parallel()
	if !isHashbangPassFailEmitAuditText(audit_5_4_3_2) {
		t.Errorf("isHashbangPassFailEmitAuditText(5.4.3.2) = false, want true")
	}
}

func TestIsHashbangPassFailEmitAuditText_Negative(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, audit string }{
		{"5.4.1.6 shebang-less (별 fix)", audit_5_4_1_6},
		{"non-hashbang (sshd)", audit_nonGsettings},
		{"empty", ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if isHashbangPassFailEmitAuditText(tc.audit) {
				t.Errorf("isHashbangPassFailEmitAuditText = true, want false")
			}
		})
	}
}

func TestSynthesizeHashbangPassFailEmit_Output(t *testing.T) {
	t.Parallel()
	bash, ok := synthesizeHashbangPassFailEmit(audit_5_4_3_2)
	if !ok {
		t.Fatal("ok = false")
	}
	want := []string{
		`out=$(printf '%s'`,
		`base64 -d | bash 2>/dev/null)`,
		`case "$out" in`,
		`*PASSED*) printf '** PASS **\n'`,
		`** FAIL **`,
	}
	for _, w := range want {
		if !strings.Contains(bash, w) {
			t.Errorf("output missing %q\n  bash=%s", w, bash)
		}
	}
}
