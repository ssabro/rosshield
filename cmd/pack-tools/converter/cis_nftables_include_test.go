// CIS nftables include 합성 — G3 4.3.10 단위 테스트.

package converter

import (
	"strings"
	"testing"
)

const audit_4_3_10 = `Run the following commands to verify that input, forward, and output base chains are configured to be applied to a nftables ruleset on boot:
Run the following command to verify the input base chain:
# [ -n "$(grep -E '^\s*include' /etc/nftables.conf)" ] && awk '/hook input/,/}/' $(awk '$1 ~ /^\s*include/ { gsub("\"","",$2);print $2 }' /etc/nftables.conf)
Output should be similar to:
type filter hook input priority 0; policy drop;
# Ensure loopback traffic is configured
iif "lo" accept
Run the following command to verify the forward base chain:
# [ -n "$(grep -E '^\s*include' /etc/nftables.conf)" ] && awk '/hook forward/,/}/' $(awk '$1 ~ /^\s*include/ { gsub("\"","",$2);print $2 }' /etc/nftables.conf)
Output should be similar to:
type filter hook forward priority 0; policy drop;
Run the following command to verify the forward base chain:
# [ -n "$(grep -E '^\s*include' /etc/nftables.conf)" ] && awk '/hook output/,/}/' $(awk '$1 ~ /^\s*include/ { gsub("\"","",$2);print $2 }' /etc/nftables.conf)
Output should be similar to:
type filter hook output priority 0; policy drop;`

func TestIsNftIncludeAuditText_Positive(t *testing.T) {
	t.Parallel()
	if !isNftIncludeAuditText(audit_4_3_10) {
		t.Errorf("isNftIncludeAuditText(4.3.10) = false, want true")
	}
}

func TestIsNftIncludeAuditText_Negative(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, audit string }{
		{
			name: "nftables.conf 없음 (G1 nft list ruleset)",
			audit: `# nft list ruleset | grep 'hook input'
type filter hook input priority 0; policy drop;`,
		},
		{
			name: "policy drop 없음",
			audit: `Run: # awk '/hook input/,/}/' /etc/nftables.conf
Output should be similar to:
type filter hook input priority 0; policy accept;
hook forward block
hook output`,
		},
		{name: "non-nftables (sshd)", audit: audit_nonGsettings},
		{name: "empty", audit: ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if isNftIncludeAuditText(tc.audit) {
				t.Errorf("isNftIncludeAuditText = true, want false")
			}
		})
	}
}

func TestSynthesizeNftInclude_Output(t *testing.T) {
	t.Parallel()
	bash, ok := synthesizeNftInclude(audit_4_3_10)
	if !ok {
		t.Fatal("ok = false")
	}
	want := []string{
		`config_files=$(awk '$1 ~ /^\s*include/`,
		`for hook in input forward output`,
		`awk "/hook $hook/,/}/" $config_files`,
		`grep -qF -- "policy drop"`,
		`** PASS **`,
		`** FAIL **`,
	}
	for _, w := range want {
		if !strings.Contains(bash, w) {
			t.Errorf("output missing %q\n  bash=%s", w, bash)
		}
	}
}
