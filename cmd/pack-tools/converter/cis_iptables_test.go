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

// === 4.3.3 iptables/ip6tables empty (E-4) ===

const audit_4_3_3 = `Run the following commands to ensure no iptables rules exist
For iptables:
# iptables -L
No rules should be returned
For ip6tables:
# ip6tables -L
No rules should be returned`

func TestIsIptablesEmptyAuditText_Positive(t *testing.T) {
	t.Parallel()
	if !isIptablesEmptyAuditText(audit_4_3_3) {
		t.Errorf("isIptablesEmptyAuditText(4.3.3) = false, want true")
	}
}

func TestIsIptablesEmptyAuditText_Negative(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, audit string }{
		{"4.4.2.1 (chain policy 다른 패턴)", audit_4_4_2_1},
		{"non-iptables (sshd)", audit_nonGsettings},
		{"empty", ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if isIptablesEmptyAuditText(tc.audit) {
				t.Errorf("isIptablesEmptyAuditText = true, want false")
			}
		})
	}
}

func TestSynthesizeIptablesEmpty_Output(t *testing.T) {
	t.Parallel()
	bash, ok := synthesizeIptablesEmpty(audit_4_3_3)
	if !ok {
		t.Fatal("ok = false")
	}
	want := []string{
		`out4=$(iptables -L`,
		`out6=$(ip6tables -L`,
		`grep -qE '^(ACCEPT|DROP|REJECT)\s+\S+\s+--'`,
		`** PASS **`,
		`** FAIL **`,
	}
	for _, w := range want {
		if !strings.Contains(bash, w) {
			t.Errorf("output missing %q\n  bash=%s", w, bash)
		}
	}
}

// === 4.4.2.2 iptables -L X -v -n + multi-line table ===

const audit_4_4_2_2_full = `Run the following commands and verify output includes the listed rules in order (pkts and bytes counts may differ, prot may be all or 0):
# iptables -L INPUT -v -n
Chain INPUT (policy DROP 0 packets, 0 bytes)
pkts bytes target prot opt in out source destination
0 0 ACCEPT all -- lo * 0.0.0.0/0 0.0.0.0/0
0 0 DROP all -- * * 127.0.0.0/8 0.0.0.0/0
# iptables -L OUTPUT -v -n
Chain OUTPUT (policy DROP 0 packets, 0 bytes)
pkts bytes target prot opt in out source destination
0 0 ACCEPT all -- * lo 0.0.0.0/0 0.0.0.0/0`

func TestIsIptablesVerboseAuditText_Positive(t *testing.T) {
	t.Parallel()
	if !isIptablesVerboseAuditText(audit_4_4_2_2_full) {
		t.Errorf("isIptablesVerboseAuditText(4.4.2.2) = false, want true")
	}
}

func TestIsIptablesVerboseAuditText_Negative(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, audit string }{
		{"4.4.2.1 (단순 -L, -v -n 부재)", audit_4_4_2_1},
		{"non-iptables (sshd)", audit_nonGsettings},
		{"empty", ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if isIptablesVerboseAuditText(tc.audit) {
				t.Errorf("isIptablesVerboseAuditText = true, want false")
			}
		})
	}
}

func TestExtractIptablesVerboseChecks(t *testing.T) {
	t.Parallel()
	checks, ok := extractIptablesVerboseChecks(audit_4_4_2_2_full)
	if !ok {
		t.Fatal("ok = false")
	}
	if len(checks) != 2 {
		t.Fatalf("checks count = %d, want 2", len(checks))
	}
	// cmd 1 INPUT 2 token (ACCEPT lo + DROP 127.0.0.0)
	if !strings.Contains(checks[0].cmd, "INPUT") {
		t.Errorf("cmd[0] missing INPUT: %q", checks[0].cmd)
	}
	if len(checks[0].tokens) != 2 {
		t.Errorf("INPUT tokens = %d, want 2 (%#v)", len(checks[0].tokens), checks[0].tokens)
	}
	// cmd 2 OUTPUT 1 token (ACCEPT lo)
	if !strings.Contains(checks[1].cmd, "OUTPUT") {
		t.Errorf("cmd[1] missing OUTPUT: %q", checks[1].cmd)
	}
	if len(checks[1].tokens) != 1 {
		t.Errorf("OUTPUT tokens = %d, want 1 (%#v)", len(checks[1].tokens), checks[1].tokens)
	}
}

func TestSynthesizeIptablesVerbose_Output(t *testing.T) {
	t.Parallel()
	bash, ok := synthesizeIptablesVerbose(audit_4_4_2_2_full)
	if !ok {
		t.Fatal("ok = false")
	}
	want := []string{
		"out_0=$(iptables -L INPUT -v -n",
		"out_1=$(iptables -L OUTPUT -v -n",
		`grep -qF -- "ACCEPT all -- lo`,
		`grep -qF -- "DROP all -- * * 127.0.0.0/8`,
		`** PASS **`,
		`** FAIL **`,
	}
	for _, w := range want {
		if !strings.Contains(bash, w) {
			t.Errorf("output missing %q\n  bash=%s", w, bash)
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
