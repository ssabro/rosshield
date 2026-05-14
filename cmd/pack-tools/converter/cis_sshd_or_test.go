// CIS sshd -T multi-line OR 합성 — G11 단위 테스트.
//
// 6 unit:
//   - positive 2건: 5.1.4 (4 alternation, placeholder `<userlist>` 제거) + 5.1.14 (2 alternation)
//   - negative 3건: 단일 expected (alternation X) / non-sshd / empty
//   - synthesize 1건: output substring snapshot

package converter

import (
	"strings"
	"testing"
)

const audit_5_1_4 = `Run the following command and verify the output:
# sshd -T | grep -Pi -- '^\h*(allow|deny)(users|groups)\h+\H+'
Verify that the output matches at least one of the following lines:
allowusers <userlist>
-OR-
allowgroups <grouplist>
-OR-
denyusers <userlist>
-OR-
denygroups <grouplist>
Review the list(s) to ensure included users and/or groups follow local site policy
- IF - Match set statements are used in your environment, specify the connection
parameters to use for the -T extended test mode and run the audit to verify the setting`

const audit_5_1_14 = `Run the following command and verify that output matches loglevel VERBOSE or loglevel INFO:
# sshd -T | grep loglevel
loglevel VERBOSE
- OR -
loglevel INFO
- IF - Match set statements are used in your environment`

func TestIsSshdGrepOrAuditText_Positive(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, audit string }{
		{"5.1.4 (4 alternation + placeholder)", audit_5_1_4},
		{"5.1.14 (2 alternation)", audit_5_1_14},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if !isSshdGrepOrAuditText(tc.audit) {
				t.Errorf("isSshdGrepOrAuditText = false, want true")
			}
		})
	}
}

func TestIsSshdGrepOrAuditText_Negative(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, audit string }{
		{
			name: "단일 expected (alternation X)",
			audit: `Verify the following:
# sshd -T | grep clientaliveinterval
clientaliveinterval 15`,
		},
		{
			name: "non-sshd (gsettings)",
			audit: `# gsettings get foo bar
true
- OR -
false`,
		},
		{name: "empty", audit: ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if isSshdGrepOrAuditText(tc.audit) {
				t.Errorf("isSshdGrepOrAuditText = true, want false")
			}
		})
	}
}

func TestExtractSshdGrepOrChecks_AlternationCount(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name           string
		audit          string
		wantAltCount   int
		wantCmdSubstr  string
		wantAltSubstrs []string
	}{
		{
			name:          "5.1.4 (4 alt, placeholder 제거)",
			audit:         audit_5_1_4,
			wantAltCount:  4,
			wantCmdSubstr: "sshd -T | grep -Pi",
			wantAltSubstrs: []string{
				"allowusers", "allowgroups", "denyusers", "denygroups",
			},
		},
		{
			name:           "5.1.14 (2 alt, placeholder 없음)",
			audit:          audit_5_1_14,
			wantAltCount:   2,
			wantCmdSubstr:  "sshd -T | grep loglevel",
			wantAltSubstrs: []string{"loglevel VERBOSE", "loglevel INFO"},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cmd, alts, ok := extractSshdGrepOrChecks(tc.audit)
			if !ok {
				t.Fatal("ok = false")
			}
			if !strings.Contains(cmd, tc.wantCmdSubstr) {
				t.Errorf("cmd = %q, want substring %q", cmd, tc.wantCmdSubstr)
			}
			if len(alts) != tc.wantAltCount {
				t.Errorf("alt count = %d, want %d (alts: %#v)", len(alts), tc.wantAltCount, alts)
			}
			for _, want := range tc.wantAltSubstrs {
				found := false
				for _, a := range alts {
					if strings.Contains(a, want) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("alts missing substring %q (alts: %#v)", want, alts)
				}
			}
		})
	}
}

func TestSynthesizeSshdGrepOr_Output(t *testing.T) {
	t.Parallel()
	bash, ok := synthesizeSshdGrepOr(audit_5_1_14)
	if !ok {
		t.Fatal("ok = false")
	}
	want := []string{
		`out=$(sshd -T | grep loglevel 2>/dev/null)`,
		`for token in`,
		`"loglevel VERBOSE"`,
		`"loglevel INFO"`,
		`grep -qiF -- "$token"`,
		`** PASS **`,
		`** FAIL **`,
	}
	for _, w := range want {
		if !strings.Contains(bash, w) {
			t.Errorf("output missing %q\n  bash=%s", w, bash)
		}
	}
}
