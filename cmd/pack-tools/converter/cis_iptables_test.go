// CIS iptables chain policy 합성 — G4 4.4.2.1 단위 테스트.
//
// 5 unit:
//   - positive 1건 (4.4.2.1)
//   - negative 3건 (4.4.2.2 -v -n / non-iptables / empty)
//   - synthesize 1건 (output substring snapshot)

package converter

import (
	"strings"
	"testing"
)

const audit_4_4_2_1 = `Run the following command and verify that the policy for the INPUT , OUTPUT , and FORWARD chains is DROP or REJECT :
# iptables -L
Chain INPUT (policy DROP)
Chain FORWARD (policy DROP)
Chain OUTPUT (policy DROP)`

// 4.4.2.2: -v -n + multi-line table — 본 합성기 미지원(별 epic).
const audit_4_4_2_2 = `Run the following commands and verify output includes the listed rules in order:
# iptables -L INPUT -v -n
Chain INPUT (policy DROP 0 packets, 0 bytes)
pkts bytes target prot opt in out source destination
0 0 ACCEPT all -- lo * 0.0.0.0/0 0.0.0.0/0`

func TestIsIptablesChainPolicyAuditText_Positive(t *testing.T) {
	t.Parallel()
	if !isIptablesChainPolicyAuditText(audit_4_4_2_1) {
		t.Errorf("isIptablesChainPolicyAuditText(4.4.2.1) = false, want true")
	}
}

func TestIsIptablesChainPolicyAuditText_Negative(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, audit string }{
		{"4.4.2.2 (-v -n + multi-line table, 미지원)", audit_4_4_2_2},
		{"non-iptables (nftables)", audit_4_3_8},
		{"empty", ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if isIptablesChainPolicyAuditText(tc.audit) {
				t.Errorf("isIptablesChainPolicyAuditText = true, want false")
			}
		})
	}
}

func TestExtractIptablesChainExpecteds(t *testing.T) {
	t.Parallel()
	cmd, exps, ok := extractIptablesChainExpecteds(audit_4_4_2_1)
	if !ok {
		t.Fatal("ok = false")
	}
	if cmd != "iptables -L" {
		t.Errorf("cmd = %q, want %q", cmd, "iptables -L")
	}
	if len(exps) != 3 {
		t.Fatalf("expecteds count = %d, want 3 (got: %#v)", len(exps), exps)
	}
	wantChains := []string{"INPUT", "FORWARD", "OUTPUT"}
	for i, ch := range wantChains {
		if !strings.Contains(exps[i], ch) {
			t.Errorf("exps[%d] = %q, want substring %q", i, exps[i], ch)
		}
	}
}

func TestSynthesizeIptablesChainPolicy_Output(t *testing.T) {
	t.Parallel()
	bash, ok := synthesizeIptablesChainPolicy(audit_4_4_2_1)
	if !ok {
		t.Fatal("ok = false")
	}
	want := []string{
		"out=$(iptables -L 2>/dev/null)",
		`grep -qF -- "Chain INPUT (policy DROP)"`,
		`grep -qF -- "Chain FORWARD (policy DROP)"`,
		`grep -qF -- "Chain OUTPUT (policy DROP)"`,
		`** PASS **`,
		`** FAIL **`,
	}
	for _, w := range want {
		if !strings.Contains(bash, w) {
			t.Errorf("output missing %q\n  bash=%s", w, bash)
		}
	}
}
