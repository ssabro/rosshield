// CIS nftables hook 검증 합성 — G1 단위 테스트.
//
// 5 unit:
//   - positive 2건 (4.3.5 chain 존재 / 4.3.8 policy drop)
//   - negative 2건 (단일 cmd / non-nftables)
//   - synthesize 1건 (output substring snapshot)

package converter

import (
	"strings"
	"testing"
)

const audit_4_3_5 = `Run the following commands and verify that base chains exist for INPUT.
# nft list ruleset | grep 'hook input'
type filter hook input priority 0;
Run the following commands and verify that base chains exist for FORWARD.
# nft list ruleset | grep 'hook forward'
type filter hook forward priority 0;
Run the following commands and verify that base chains exist for OUTPUT.
# nft list ruleset | grep 'hook output'
type filter hook output priority 0;`

const audit_4_3_8 = `Run the following commands and verify that base chains contain a policy of DROP.
# nft list ruleset | grep 'hook input'
type filter hook input priority 0; policy drop;
# nft list ruleset | grep 'hook forward'
type filter hook forward priority 0; policy drop;
# nft list ruleset | grep 'hook output'
type filter hook output priority 0; policy drop;`

func TestIsNftHookAuditText_Positive(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, audit string }{
		{"4.3.5 chain existence", audit_4_3_5},
		{"4.3.8 policy drop", audit_4_3_8},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if !isNftHookAuditText(tc.audit) {
				t.Errorf("isNftHookAuditText = false, want true")
			}
		})
	}
}

func TestIsNftHookAuditText_Negative(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, audit string }{
		{
			name: "단일 cmd (3 미달)",
			audit: `# nft list ruleset | grep 'hook input'
type filter hook input priority 0;`,
		},
		{
			name:  "non-nftables (gsettings)",
			audit: audit_1_7_6,
		},
		{name: "empty", audit: ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if isNftHookAuditText(tc.audit) {
				t.Errorf("isNftHookAuditText = true, want false")
			}
		})
	}
}

func TestExtractNftHookChecks_Pairs(t *testing.T) {
	t.Parallel()
	checks, ok := extractNftHookChecks(audit_4_3_8)
	if !ok {
		t.Fatal("ok = false")
	}
	if len(checks) != 3 {
		t.Fatalf("len = %d, want 3", len(checks))
	}
	wantHooks := []string{"hook input", "hook forward", "hook output"}
	for i, h := range wantHooks {
		if !strings.Contains(checks[i].cmd, h) {
			t.Errorf("check[%d].cmd missing %q (cmd=%q)", i, h, checks[i].cmd)
		}
		if !strings.Contains(checks[i].expected, "policy drop") {
			t.Errorf("check[%d].expected missing 'policy drop' (exp=%q)", i, checks[i].expected)
		}
	}
}

func TestSynthesizeNftHook_Output(t *testing.T) {
	t.Parallel()
	bash, ok := synthesizeNftHook(audit_4_3_8)
	if !ok {
		t.Fatal("ok = false")
	}
	want := []string{
		"out_0=$(nft list ruleset | grep 'hook input' 2>/dev/null)",
		"out_1=$(nft list ruleset | grep 'hook forward' 2>/dev/null)",
		"out_2=$(nft list ruleset | grep 'hook output' 2>/dev/null)",
		`grep -qF -- "type filter hook input priority 0; policy drop;"`,
		`** PASS **`,
		`** FAIL **`,
	}
	for _, w := range want {
		if !strings.Contains(bash, w) {
			t.Errorf("output missing %q\n  bash=%s", w, bash)
		}
	}
}
