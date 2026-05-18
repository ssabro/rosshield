// CIS 6.2.3.x auditd 합성 — Stage 1 인식기·추출기 단위 테스트.
//
// design doc: docs/design/notes/cis-6-2-3-auditd-design.md §7 Stage 1.
// 12 unit:
//   - positive(isAuditctlAuditText=true) 8건: 6.2.3.{1,4,5,7,8,11,15,19} (대표 패턴 cover)
//   - negative(false) 2건: 6.2.3.{20,21} (각각 grep + Manual)
//   - 비-audit text 2건: 7.2.4 grep verify(다른 도메인) + 빈 string
//
// extractAuditctlExpectedRules 라인 수 검증은 positive 4건 sample(실 baseline 인용)에 한정.

package converter

import (
	"strings"
	"testing"
)

// 6.2.3.1 — 단일 awk + sudoers watch (단순). expected: on-disk 2 / running 2.
const audit_6_2_3_1 = `On disk configuration
Run the following command to check the on disk rules:
# awk '/^ *-w/ \
&&/\/etc\/sudoers/ \
&&/ +-p *wa/ \
&&(/ key= *[!-~]* *$/||/ -k *[!-~]* *$/)' /etc/audit/rules.d/*.rules
Verify the output matches:
-w /etc/sudoers -p wa -k scope
-w /etc/sudoers.d -p wa -k scope
Running configuration
Run the following command to check loaded rules:
# auditctl -l | awk '/^ *-w/ \
&&/\/etc\/sudoers/ \
&&/ +-p *wa/ \
&&(/ key= *[!-~]* *$/||/ -k *[!-~]* *$/)'
Verify the output matches:
-w /etc/sudoers -p wa -k scope
-w /etc/sudoers.d -p wa -k scope`

// 6.2.3.4 — multi-cmd { ... } block + "Verify output of matches" 변형 + 5/5 lines.
const audit_6_2_3_4 = `On disk configuration
Run the following command to check the on disk rules:
# {
awk '/^ *-a *always,exit/ \
&&/ -F *arch=b(32|64)/ \
&&/ -S/' /etc/audit/rules.d/*.rules
awk '/^ *-w/' /etc/audit/rules.d/*.rules
}
Verify output of matches:
-a always,exit -F arch=b64 -S adjtimex,settimeofday -k time-change
-a always,exit -F arch=b32 -S adjtimex,settimeofday -k time-change
-a always,exit -F arch=b64 -S clock_settime -F a0=0x0 -k time-change
-a always,exit -F arch=b32 -S clock_settime -F a0=0x0 -k time-change
-w /etc/localtime -p wa -k time-change
Running configuration
Run the following command to check loaded rules:
# {
auditctl -l | awk '/^ *-a *always,exit/'
auditctl -l | awk '/^ *-w/'
}
Verify the output includes:
-a always,exit -F arch=b64 -S adjtimex,settimeofday -F key=time-change
-a always,exit -F arch=b32 -S settimeofday,adjtimex -F key=time-change
-a always,exit -F arch=b64 -S clock_settime -F a0=0x0 -F key=time-change
-a always,exit -F arch=b32 -S clock_settime -F a0=0x0 -F key=time-change
-w /etc/localtime -p wa -k time-change`

// 6.2.3.7 — UID_MIN 변수 + multi-cmd block + 4/4 (multi-line wrap continuation).
const audit_6_2_3_7 = `On disk configuration
Run the following command to check the on disk rules:
# {
UID_MIN=$(awk '/^\s*UID_MIN/{print $2}' /etc/login.defs)
[ -n "${UID_MIN}" ] && awk "/^ *-a *always,exit/ \
&&/ -F *arch=b(32|64)/ \
&&/ -F *auid>=${UID_MIN}/ \
&&/ -S/ \
&&/creat/" /etc/audit/rules.d/*.rules
}
Verify the output includes:
-a always,exit -F arch=b64 -S creat,open,openat,truncate,ftruncate -F auid>=1000 -F auid!=unset -k access
-a always,exit -F arch=b32 -S creat,open,openat,truncate,ftruncate -F auid>=1000 -F auid!=unset -k access
Running configuration
Run the following command to check loaded rules:
# {
UID_MIN=$(awk '/^\s*UID_MIN/{print $2}' /etc/login.defs)
[ -n "${UID_MIN}" ] && auditctl -l | awk "/^ *-a *always,exit/"
}
Verify the output includes:
-a always,exit -F arch=b64 -S open,truncate,ftruncate,creat,openat -F auid>=1000 -F auid!=-1 -F key=access
-a always,exit -F arch=b32 -S open,truncate,ftruncate,creat,openat -F auid>=1000 -F auid!=-1 -F key=access`

// 6.2.3.19 — hashbang + multi-block + UID_MIN + 2/2 lines (symlink check는 별 block).
const audit_6_2_3_19 = `On disk configuration
Run the following script to check the on disk rules:
#!/usr/bin/env bash
{
awk '/^ *-a *always,exit/' /etc/audit/rules.d/*.rules
UID_MIN=$(awk '/^\s*UID_MIN/{print $2}' /etc/login.defs)
[ -n "${UID_MIN}" ] && awk "/^ *-a *always,exit/" /etc/audit/rules.d/*.rules
}
Verify the output matches:
-a always,exit -F arch=b64 -S init_module,finit_module,delete_module,create_module,query_module -F auid>=1000 -F auid!=unset -k kernel_modules
-a always,exit -F path=/usr/bin/kmod -F perm=x -F auid>=1000 -F auid!=unset -k kernel_modules
Running configuration
Run the following script to check loaded rules:
#!/usr/bin/env bash
{
auditctl -l | awk '/^ *-a *always,exit/'
UID_MIN=$(awk '/^\s*UID_MIN/{print $2}' /etc/login.defs)
[ -n "${UID_MIN}" ] && auditctl -l | awk "/^ *-a *always,exit/"
}
Verify the output includes:
-a always,exit -F arch=b64 -S create_module,init_module,delete_module,query_module,finit_module -F auid>=1000 -F auid!=-1 -F key=kernel_modules
-a always,exit -S all -F path=/usr/bin/kmod -F perm=x -F auid>=1000 -F auid!=-1 -F key=kernel_modules`

// minimal mock (positive 4건 — 인식만 검증, 라인 수는 검증 안 함):
// 6.2.3.{5,8,11,15} 패턴 단순화. 실 baseline의 모든 변형 cover는 Stage 2 integration test로 미룸.
const auditMockMinimal = `On disk
# awk '/^ *-w/ ...' /etc/audit/rules.d/*.rules
Verify the output matches:
-w /etc/foo -p wa -k baz
Running
# auditctl -l | awk '/^ *-w/'
Verify the output matches:
-w /etc/foo -p wa -k baz`

// 6.2.3.20 — 단순 grep + immutable check, auditctl -l 미사용.
const audit_6_2_3_20 = `Run the following command and verify output matches:
# grep -Ph -- '^\h*-e\h+2\b' /etc/audit/rules.d/*.rules | tail -1
-e 2`

// 6.2.3.21 — Manual augenrules --check, auditctl -l 미사용.
const audit_6_2_3_21 = `Run the following command and verify the output indicates "No change":
# augenrules --check
/usr/sbin/augenrules: No change`

// 7.2.4 — grep verify(다른 도메인, auditctl 미사용).
const audit_7_2_4 = `Run the following command and verify no results are returned:
# awk -F: '($1=="shadow") {print $NF}' /etc/group
# awk -F: '($4=="42") {print $1}' /etc/passwd`

// === isAuditctlAuditText positive (8건) ===

func TestIsAuditctlAuditTextPositive(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		audit string
	}{
		{"6.2.3.1 sudoers watch", audit_6_2_3_1},
		{"6.2.3.4 multi-cmd block", audit_6_2_3_4},
		{"6.2.3.5 mock", auditMockMinimal},
		{"6.2.3.7 UID_MIN block", audit_6_2_3_7},
		{"6.2.3.8 mock", auditMockMinimal},
		{"6.2.3.11 mock", auditMockMinimal},
		{"6.2.3.15 mock", auditMockMinimal},
		{"6.2.3.19 hashbang multi-block", audit_6_2_3_19},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if !isAuditctlAuditText(tc.audit) {
				t.Errorf("isAuditctlAuditText = false, want true")
			}
		})
	}
}

// === isAuditctlAuditText negative (4건: 6.2.3.20, 6.2.3.21, 7.2.4 grep, 빈 string) ===

func TestIsAuditctlAuditTextNegative(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		audit string
	}{
		{"6.2.3.20 grep immutable", audit_6_2_3_20},
		{"6.2.3.21 augenrules manual", audit_6_2_3_21},
		{"7.2.4 grep verify (다른 도메인)", audit_7_2_4},
		{"empty input", ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if isAuditctlAuditText(tc.audit) {
				t.Errorf("isAuditctlAuditText = true, want false")
			}
		})
	}
}

// === extractAuditctlExpectedRules 라인 수 검증 (4건 실 baseline + 1건 short-circuit edge) ===

func TestExtractAuditctlExpectedRules(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		audit       string
		wantOnDisk  int
		wantRunning int
	}{
		{"6.2.3.1 (2/2)", audit_6_2_3_1, 2, 2},
		{"6.2.3.4 (5/5)", audit_6_2_3_4, 5, 5},
		{"6.2.3.7 (2/2)", audit_6_2_3_7, 2, 2},
		{"6.2.3.19 (2/2)", audit_6_2_3_19, 2, 2},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			on, run, ok := extractAuditctlExpectedRules(tc.audit)
			if !ok {
				t.Fatalf("ok = false, want true")
			}
			if len(on) != tc.wantOnDisk {
				t.Errorf("on-disk lines = %d, want %d (lines: %#v)", len(on), tc.wantOnDisk, on)
			}
			if len(run) != tc.wantRunning {
				t.Errorf("running lines = %d, want %d (lines: %#v)", len(run), tc.wantRunning, run)
			}
		})
	}
}

// === 6.2.3.21 augenrules --check (E-4) — audit_6_2_3_21 const는 line 128 재사용 ===

func TestIsAugenrulesCheckAuditText_Positive(t *testing.T) {
	t.Parallel()
	if !isAugenrulesCheckAuditText(audit_6_2_3_21) {
		t.Errorf("isAugenrulesCheckAuditText(6.2.3.21) = false, want true")
	}
}

func TestIsAugenrulesCheckAuditText_Negative(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, audit string }{
		{"phrase 없음", `# augenrules --check`},
		{"non-augenrules (sshd)", audit_nonGsettings},
		{"empty", ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if isAugenrulesCheckAuditText(tc.audit) {
				t.Errorf("isAugenrulesCheckAuditText = true, want false")
			}
		})
	}
}

func TestSynthesizeAugenrulesCheck_Output(t *testing.T) {
	t.Parallel()
	bash, ok := synthesizeAugenrulesCheck(audit_6_2_3_21)
	if !ok {
		t.Fatal("ok = false")
	}
	want := []string{
		`out=$(augenrules --check`,
		`grep -qF -- "No change"`,
		`** PASS **`,
		`** FAIL **`,
	}
	for _, w := range want {
		if !strings.Contains(bash, w) {
			t.Errorf("output missing %q\n  bash=%s", w, bash)
		}
	}
}

// extractAuditctlExpectedRules: phrase 1회만 등장하면 ok=false (block 2개 필요).
func TestExtractAuditctlExpectedRulesRejectsSinglePhrase(t *testing.T) {
	t.Parallel()
	single := `Verify the output matches:
-w /etc/foo -p wa -k bar`
	if _, _, ok := extractAuditctlExpectedRules(single); ok {
		t.Errorf("ok = true, want false (single phrase block)")
	}
}

// === normalizeAuditctlRule (4 case) ===

func TestNormalizeAuditctlRule(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, in, want string
	}{
		{
			name: "file watch — 변경 0(syscall 무, key 본래 short만)",
			// `-w /path -p X -k key`도 -k → -F key= 변환은 적용. 단 일부 audit text가 `-k key`로 끝나는 형식이라 통일.
			in:   `-w /etc/sudoers -p wa -k scope`,
			want: `-w /etc/sudoers -p wa -F key=scope`,
		},
		{
			name: "syscall set 정렬 + key 통일 (6.2.3.4 running 라인)",
			in:   `-a always,exit -F arch=b32 -S settimeofday,adjtimex -F key=time-change`,
			want: `-a always,exit -F arch=b32 -S adjtimex,settimeofday -F key=time-change`,
		},
		{
			name: "UID_MIN placeholder + auid!= 동치 통일 (6.2.3.7 running 라인)",
			in:   `-a always,exit -F arch=b64 -S open,truncate,ftruncate,creat,openat -F exit=-EACCES -F auid>=1000 -F auid!=-1 -F key=access`,
			want: `-a always,exit -F arch=b64 -S creat,ftruncate,open,openat,truncate -F exit=-EACCES -F auid>=__UID_MIN__ -F auid!=unset -F key=access`,
		},
		{
			name: "auid!=4294967295 동치 + -k 양쪽 통일",
			in:   `-a always,exit -F arch=b64 -S unlink -F auid>=1000 -F auid!=4294967295 -k delete`,
			want: `-a always,exit -F arch=b64 -S unlink -F auid>=__UID_MIN__ -F auid!=unset -F key=delete`,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := normalizeAuditctlRule(tc.in)
			if got != tc.want {
				t.Errorf("normalizeAuditctlRule:\n  in   = %s\n  got  = %s\n  want = %s", tc.in, got, tc.want)
			}
		})
	}
}

// === synthesizeAuditctlMatch (2 case — 출력 substring snapshot) ===

func TestSynthesizeAuditctlMatch(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name           string
		audit          string
		wantSubstrings []string
		wantRuleCount  int // need_disk + need_run 라인 수 합
	}{
		{
			name:  "6.2.3.1 — 단순 watch 2/2",
			audit: audit_6_2_3_1,
			wantSubstrings: []string{
				"#!/usr/bin/env bash",
				"UID_MIN=${UID_MIN:-1000}",
				"need_disk=(",
				"need_run=(",
				`-w /etc/sudoers -p wa -F key=scope`, // -k → -F key= normalize
				`-w /etc/sudoers.d -p wa -F key=scope`,
				"normalize_fn() {",
				"grep -qxF --",
				"** PASS **",
				"** FAIL **",
			},
			wantRuleCount: 4, // 2 on-disk + 2 running
		},
		{
			name:  "6.2.3.4 — multi-cmd 5/5 + syscall sort + key 통일",
			audit: audit_6_2_3_4,
			wantSubstrings: []string{
				`-a always,exit -F arch=b64 -S adjtimex,settimeofday -F key=time-change`, // 정렬 후
				`-a always,exit -F arch=b32 -S adjtimex,settimeofday -F key=time-change`, // running settimeofday,adjtimex → 정렬
				`-w /etc/localtime -p wa -F key=time-change`,
			},
			wantRuleCount: 10, // 5 on-disk + 5 running
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			bash, ok := synthesizeAuditctlMatch(tc.audit)
			if !ok {
				t.Fatalf("synthesizeAuditctlMatch ok = false")
			}
			for _, sub := range tc.wantSubstrings {
				if !contains(bash, sub) {
					t.Errorf("output missing substring: %q\n  bash=%s", sub, bash)
				}
			}
			// 라인 수 검증 — bash array 안 quote된 라인 카운트.
			ruleLines := countQuotedRules(bash)
			if ruleLines != tc.wantRuleCount {
				t.Errorf("rule line count = %d, want %d\n  bash=%s", ruleLines, tc.wantRuleCount, bash)
			}
		})
	}
}

// contains는 strings.Contains의 래퍼(테스트 가독성).
func contains(haystack, needle string) bool {
	return len(needle) == 0 || (len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

// countQuotedRules는 bash array 안 `  "..."` 패턴 라인 수를 셉니다.
// 합성 출력에서 `\n  "rule"\n` 형식만 카운트 — 다른 string literal과 충돌 없음(2-space indent + quote).
func countQuotedRules(bash string) int {
	count := 0
	for _, line := range splitLines(bash) {
		trimmed := trimSpace(line)
		if len(trimmed) >= 2 && trimmed[0] == '"' && trimmed[len(trimmed)-1] == '"' {
			count++
		}
	}
	return count
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

func trimSpace(s string) string {
	start := 0
	for start < len(s) && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	end := len(s)
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}
