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

// === 4.2.6 Audit Result emit (G5 부분 cover) ===

const audit_4_2_6 = `Run the following script to verify a firewall rule exists for all open ports:
#!/usr/bin/env bash
{
unset a_ufwout;unset a_openports
while read -r l_ufwport; do
[ -n "$l_ufwport" ] && a_ufwout+=("$l_ufwport")
done < <(ufw status verbose | grep -Po '^\h*\d+\b' | sort -u)
while read -r l_openport; do
[ -n "$l_openport" ] && a_openports+=("$l_openport")
done < <(ss -tuln | awk '($5!~/%lo:/) {split($5, a, ":"); print a[2]}' | sort -u)
a_diff=("$(printf '%s\n' "${a_openports[@]}" "${a_ufwout[@]}" "${a_ufwout[@]}" | sort | uniq -u)")
if [[ -n "${a_diff[*]}" ]]; then
echo -e "\n- Audit Result:\n ** FAIL **\n- ports without rule\n"
else
echo -e "\n - Audit Passed -\n- All open ports have a rule\n"
fi
}`

func TestIsAuditResultEmitAuditText_Positive(t *testing.T) {
	t.Parallel()
	if !isAuditResultEmitAuditText(audit_4_2_6) {
		t.Errorf("isAuditResultEmitAuditText(4.2.6) = false, want true")
	}
}

func TestIsAuditResultEmitAuditText_Negative(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, audit string }{
		{"5.4.3.2 (PASSED/FAILED, 다른 키워드)", audit_5_4_3_2},
		{"non-hashbang (sshd)", audit_nonGsettings},
		{"hashbang body but no Audit Passed", `#!/usr/bin/env bash
echo "** FAIL **"`},
		{"empty", ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if isAuditResultEmitAuditText(tc.audit) {
				t.Errorf("isAuditResultEmitAuditText = true, want false")
			}
		})
	}
}

func TestSynthesizeAuditResultEmit_Output(t *testing.T) {
	t.Parallel()
	bash, ok := synthesizeAuditResultEmit(audit_4_2_6)
	if !ok {
		t.Fatal("ok = false")
	}
	want := []string{
		`out=$(printf '%s'`,
		`base64 -d | bash 2>/dev/null)`,
		`case "$out" in`,
		`*Audit?Passed*) printf '** PASS **\n'`,
		`** FAIL **`,
	}
	for _, w := range want {
		if !strings.Contains(bash, w) {
			t.Errorf("output missing %q\n  bash=%s", w, bash)
		}
	}
}

// === 5.4.1.6 brace block + expect-empty ===

func TestIsBraceBlockEmptyAuditText_Positive(t *testing.T) {
	t.Parallel()
	if !isBraceBlockEmptyAuditText(audit_5_4_1_6) {
		t.Errorf("isBraceBlockEmptyAuditText(5.4.1.6) = false, want true")
	}
}

func TestIsBraceBlockEmptyAuditText_Negative(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, audit string }{
		{"verify phrase 없음", `# foo
{
echo bar
}`},
		{"{} block 없음", `Run the following command and verify nothing is returned
echo "x"`},
		{"5.4.3.2 (PASSED/FAILED emit, 별 분기)", audit_5_4_3_2},
		{"empty", ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if isBraceBlockEmptyAuditText(tc.audit) {
				t.Errorf("isBraceBlockEmptyAuditText = true, want false")
			}
		})
	}
}

func TestExtractBraceBlock(t *testing.T) {
	t.Parallel()
	block, ok := extractBraceBlock(audit_5_4_1_6)
	if !ok {
		t.Fatal("ok = false")
	}
	if !strings.HasPrefix(strings.TrimSpace(block), "{") {
		t.Errorf("block prefix mismatch: %q", block)
	}
	if !strings.HasSuffix(strings.TrimSpace(block), "}") {
		t.Errorf("block suffix mismatch: %q", block)
	}
	if !strings.Contains(block, "while IFS=") {
		t.Errorf("block missing body content: %q", block)
	}
}

func TestSynthesizeBraceBlockEmpty_Output(t *testing.T) {
	t.Parallel()
	bash, ok := synthesizeBraceBlockEmpty(audit_5_4_1_6)
	if !ok {
		t.Fatal("ok = false")
	}
	want := []string{
		`out=$(printf '%s'`,
		`base64 -d | bash 2>/dev/null)`,
		`if [ -z "$out" ]`,
		`** PASS **`,
		`** FAIL **`,
	}
	for _, w := range want {
		if !strings.Contains(bash, w) {
			t.Errorf("output missing %q\n  bash=%s", w, bash)
		}
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
