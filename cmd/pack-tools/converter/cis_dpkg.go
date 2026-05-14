// CIS dpkg-query 설치 상태 검증 자동 변환 — E-3 epic G9 부분 cover (1.7.1 + 5.3.1.1).
//
// audit text 패턴:
//   - 1.7.1: `# dpkg-query -W -f='...' <pkg>` + expected 1줄 (`pkg unknown ok not-installed`)
//   - 5.3.1.1: `# dpkg-query -s <pkg> | grep -P ...` + expected 2+줄 (`Status: install ok installed`)
//
// 합성: cmd 실행 → 각 expected 라인을 cmd 출력에 substring 매칭 → 모두 통과 PASS.
// "should be similar to" semantic이라 Version 등 환경 의존 라인은 false FAIL 위험 — 운영자
// manual 확인. 본 합성기는 모든 expected 라인 일괄 매칭(false FAIL 발생 시 운영자 판단).
//
// 2.1.20 (`&>/dev/null && echo "..."` + Nothing returned)는 별 합성기 후속 — cmd wrap +
// emptyOutput mode 필요.
//
// 잠재 변환률: 1.7.1 + 5.3.1.1 = 2건 → +0.6%p (312 기준).
//
// 참조: docs/design/notes/cis-stat-apparmor-dpkg-design.md §3.3 G9 옵션 A.

package converter

import (
	"fmt"
	"regexp"
	"strings"
)

// regexpDpkgQueryCmd는 `# dpkg-query ...` 명령 라인 감지 (단일 라인, multi-line wrap 미지원).
var regexpDpkgQueryCmd = regexp.MustCompile(`^#\s+(dpkg-query\s+.+)$`)

// regexpDpkgQueryEmptyCheck는 2.1.20 패턴 — `dpkg-query -s <pkg>` (`#` prefix 부재) +
// `&>/dev/null && echo` 조합 + "Nothing should be returned" phrase.
// pkg name 추출용.
var regexpDpkgQueryDashS = regexp.MustCompile(`(?m)^\s*#?\s*dpkg-query\s+-s\s+(\S+)\s*&>/dev/null\s*&&\s*echo`)

// regexpNothingShouldBeReturned는 "Nothing should be returned" phrase 감지.
var regexpNothingShouldBeReturned = regexp.MustCompile(`(?i)Nothing\s+should\s+be\s+returned`)

// extractDpkgChecks는 audit text에서 dpkg-query cmd + expected 라인을 추출합니다.
//
// 인식 조건:
//   - 1+ `# dpkg-query ...` 명령
//   - 각 명령 직후 1+ expected 라인 (heading/빈 라인 종료, "should be similar to:" phrase는 skip)
//
// expected가 비어있으면 ok=false (cmd만 있는 경우는 emptyOutput mode 별 epic).
func extractDpkgChecks(audit string) (cmd string, expecteds []string, ok bool) {
	lines := strings.Split(audit, "\n")
	cmdIdx := -1
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		if cmd == "" {
			if m := regexpDpkgQueryCmd.FindStringSubmatch(line); m != nil {
				cmd = m[1]
				cmdIdx = i
			}
		}
	}
	if cmd == "" {
		return "", nil, false
	}
	for i := cmdIdx + 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			if len(expecteds) > 0 {
				break
			}
			continue
		}
		// "The output should be similar to:" 등 phrase skip.
		if strings.HasPrefix(line, "The output") || strings.HasPrefix(line, "Run") ||
			strings.HasPrefix(line, "Note:") || strings.HasPrefix(line, "Verify") ||
			strings.HasPrefix(line, "Example:") {
			continue
		}
		if strings.HasPrefix(line, "#") {
			break
		}
		expecteds = append(expecteds, line)
	}
	if len(expecteds) == 0 {
		return "", nil, false
	}
	return cmd, expecteds, true
}

// isDpkgQueryAuditText는 G9 합성 대상인지 판정합니다 (multi-line cmd wrap + emptyOutput mode 비대상).
func isDpkgQueryAuditText(audit string) bool {
	_, _, ok := extractDpkgChecks(audit)
	return ok
}

// synthesizeDpkgQuery는 cmd 실행 + 각 expected line substring 매칭 합성 bash를 생성합니다.
//
// missing 카운트 0이면 PASS, 그 외 FAIL + miss-N diagnostic. expected 라인은 grep -qF로
// 정확 substring 검사 (audit text의 "Version: 1.5.3-5" 등 환경 의존 라인은 false FAIL 위험,
// 운영자 manual 확인 책임).
func synthesizeDpkgQuery(audit string) (string, bool) {
	cmd, expecteds, ok := extractDpkgChecks(audit)
	if !ok {
		return "", false
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "out=$(%s 2>/dev/null)\n", cmd)
	sb.WriteString("missing=0\n")
	for i, exp := range expecteds {
		fmt.Fprintf(&sb,
			"printf '%%s' \"$out\" | grep -qF -- %q || { printf 'miss-%d: %%s\\n' %q; missing=$((missing+1)); }\n",
			exp, i, exp)
	}
	sb.WriteString("if [ \"$missing\" -eq 0 ]; then printf '** PASS **\\n'; else printf '** FAIL **\\n'; fi")
	return sb.String(), true
}

// extractDpkgEmptyCheck는 2.1.20 패턴에서 패키지 이름을 추출.
//
// 인식 조건: `dpkg-query -s <pkg> &>/dev/null && echo` substring + "Nothing should be returned"
// phrase 둘 다. 매칭 시 pkg 이름 반환.
func extractDpkgEmptyCheck(audit string) (pkg string, ok bool) {
	if !regexpNothingShouldBeReturned.MatchString(audit) {
		return "", false
	}
	m := regexpDpkgQueryDashS.FindStringSubmatch(audit)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// isDpkgQueryEmptyAuditText는 2.1.20 합성 대상인지 판정.
func isDpkgQueryEmptyAuditText(audit string) bool {
	_, ok := extractDpkgEmptyCheck(audit)
	return ok
}

// synthesizeDpkgQueryEmpty는 2.1.20 합성 bash 생성.
//
// 의미 합성: dpkg-query -s <pkg>가 미설치이면 stderr → null + && echo 미발동 → 출력 부재 = PASS.
// 출력에 "is installed" substring 포함이면 패키지 설치된 상태 = FAIL.
func synthesizeDpkgQueryEmpty(audit string) (string, bool) {
	pkg, ok := extractDpkgEmptyCheck(audit)
	if !ok {
		return "", false
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "out=$(dpkg-query -s %s 2>/dev/null && echo \"%s is installed\")\n", pkg, pkg)
	sb.WriteString("if [ -z \"$out\" ]; then printf '** PASS **\\n'; else printf 'fail: %s\\n' \"$out\"; printf '** FAIL **\\n'; fi")
	return sb.String(), true
}
