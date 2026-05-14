// CIS dpkg-query 합성 — G9 단위 테스트.

package converter

import (
	"strings"
	"testing"
)

const audit_1_7_1 = `Run the following command and verify gdm3 is not installed:
# dpkg-query -W -f='${binary:Package}\t${Status}\t${db:Status-Status}\n' gdm3
gdm3 unknown ok not-installed not-installed`

const audit_5_3_1_1 = `Run the following command to verify the version of libpam-runtime on the system:
# dpkg-query -s libpam-runtime | grep -P -- '^(Status|Version)\b'
The output should be similar to:
Status: install ok installed
Version: 1.5.3-5`

// 2.1.20: cmd wrap + "Nothing should be returned" — 본 합성기 미지원(emptyOutput mode 별 epic).
const audit_2_1_20 = `dpkg-query -s xserver-common &>/dev/null && echo "xserver-common is
installed"
Nothing should be returned`

func TestIsDpkgQueryAuditText_Positive(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, audit string }{
		{"1.7.1 not-installed", audit_1_7_1},
		{"5.3.1.1 Status install ok installed", audit_5_3_1_1},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if !isDpkgQueryAuditText(tc.audit) {
				t.Errorf("isDpkgQueryAuditText = false, want true")
			}
		})
	}
}

func TestIsDpkgQueryAuditText_Negative(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, audit string }{
		{"2.1.20 (`#` 없는 cmd, 미지원)", audit_2_1_20},
		{"non-dpkg (sshd)", audit_nonGsettings},
		{"empty", ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if isDpkgQueryAuditText(tc.audit) {
				t.Errorf("isDpkgQueryAuditText = true, want false")
			}
		})
	}
}

func TestExtractDpkgChecks(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		audit       string
		wantCmdSub  string
		wantExpsLen int
		wantExpSubs []string
	}{
		{
			name:        "1.7.1 — single expected",
			audit:       audit_1_7_1,
			wantCmdSub:  "dpkg-query -W -f=",
			wantExpsLen: 1,
			wantExpSubs: []string{"not-installed"},
		},
		{
			name:        "5.3.1.1 — Status + Version 2 lines (phrase skip)",
			audit:       audit_5_3_1_1,
			wantCmdSub:  "dpkg-query -s libpam-runtime",
			wantExpsLen: 2,
			wantExpSubs: []string{"Status: install ok installed", "Version: 1.5.3-5"},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cmd, exps, ok := extractDpkgChecks(tc.audit)
			if !ok {
				t.Fatal("ok = false")
			}
			if !strings.Contains(cmd, tc.wantCmdSub) {
				t.Errorf("cmd = %q, want substring %q", cmd, tc.wantCmdSub)
			}
			if len(exps) != tc.wantExpsLen {
				t.Errorf("exps count = %d, want %d (%#v)", len(exps), tc.wantExpsLen, exps)
			}
			for _, w := range tc.wantExpSubs {
				found := false
				for _, e := range exps {
					if strings.Contains(e, w) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("exps missing substring %q (%#v)", w, exps)
				}
			}
		})
	}
}

func TestSynthesizeDpkgQuery_Output(t *testing.T) {
	t.Parallel()
	bash, ok := synthesizeDpkgQuery(audit_5_3_1_1)
	if !ok {
		t.Fatal("ok = false")
	}
	want := []string{
		"out=$(dpkg-query -s libpam-runtime | grep -P -- '^(Status|Version)\\b' 2>/dev/null)",
		`grep -qF -- "Status: install ok installed"`,
		`grep -qF -- "Version: 1.5.3-5"`,
		`** PASS **`,
		`** FAIL **`,
	}
	for _, w := range want {
		if !strings.Contains(bash, w) {
			t.Errorf("output missing %q\n  bash=%s", w, bash)
		}
	}
}
