// CIS file stat 옵트 자동 변환 — E-3 epic G12 (1.6.4 + 7.1.10).
//
// audit text 패턴: 1+ `# [ -e <path> ] && stat -Lc '...' <path>` cmd + 각 expected 1+ 라인
// + "Nothing is returned" phrase (파일 미존재 시 PASS 의미).
//
// 합성: 각 cmd 실행 → 출력 비어있으면 PASS(파일 미존재) → 출력 있으면 각 expected line
// substring 매칭. 모든 cmd 통과해야 최종 PASS.
//
// expected가 audit text의 multi-line wrap된 라인은 raw 그대로 추출하여 각각 substring 매칭
// — cmd 출력의 단일 라인에 wrap 라인 일부가 substring 포함되면 PASS (정확도 트레이드오프).
//
// 잠재 변환률: 1.6.4 + 7.1.10 = 2건 → +0.6%p (312 기준).
//
// 참조: docs/design/notes/cis-stat-apparmor-dpkg-design.md §3.1 G12 + §6 D-E3-5.

package converter

import (
	"fmt"
	"regexp"
	"strings"
)

// regexpStatOptCmd는 `# [ -e <path> ] && stat -Lc '...' <path>` 명령 라인 감지.
// 옵트 가드(`[ -e ]`) + stat 명령 둘 다 필수.
var regexpStatOptCmd = regexp.MustCompile(`^#\s+(\[\s*-e\s+"?[^\s"]+"?\s*\]\s*&&\s*stat\s+-Lc\s+.+)$`)

// regexpOrSeparatorStat는 expected 사이 separator (`-- OR --`, `-OR-`, `- OR -`) 감지.
var regexpOrSeparatorStat = regexp.MustCompile(`^-+\s*OR\s*-+\s*$`)

// regexpNothingReturnedPhrase는 "Nothing is returned" / "Nothing returned" phrase 감지.
var regexpNothingReturnedPhrase = regexp.MustCompile(`(?i)Nothing\s+(is\s+)?returned`)

// statOptCheck는 단일 cmd × expected 라인 슬라이스 쌍.
type statOptCheck struct {
	cmd       string
	expecteds []string
}

// extractStatOptChecks는 audit text에서 stat 옵트 cmd + 각 expected 라인을 추출.
//
// 인식 조건:
//   - 1+ `# [ -e ... ] && stat -Lc '...' <path>` 명령
//   - 각 cmd 직후 expected 라인 (separator/heading/Nothing returned phrase 만나면 종료)
//   - "Nothing is returned" phrase 1+회 등장 (G12 시그니처)
//
// expected wrap 라인은 raw 그대로 추출 (substring 매칭 시 cmd 출력에 부분 포함되면 매칭).
func extractStatOptChecks(audit string) (checks []statOptCheck, ok bool) {
	if !regexpNothingReturnedPhrase.MatchString(audit) {
		return nil, false
	}
	lines := strings.Split(audit, "\n")
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		m := regexpStatOptCmd.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		check := statOptCheck{cmd: m[1]}
		for j := i + 1; j < len(lines); j++ {
			next := strings.TrimSpace(lines[j])
			if next == "" {
				if len(check.expecteds) > 0 {
					break
				}
				continue
			}
			if strings.HasPrefix(next, "#") {
				break
			}
			if regexpOrSeparatorStat.MatchString(next) {
				continue
			}
			if regexpNothingReturnedPhrase.MatchString(next) {
				break
			}
			check.expecteds = append(check.expecteds, next)
		}
		if len(check.expecteds) > 0 {
			checks = append(checks, check)
		}
	}
	if len(checks) == 0 {
		return nil, false
	}
	return checks, true
}

// isStatOptAuditText는 G12 합성 대상인지 판정.
func isStatOptAuditText(audit string) bool {
	_, ok := extractStatOptChecks(audit)
	return ok
}

// synthesizeStatOpt는 stat 옵트 합성 bash를 생성.
//
// 각 cmd 실행 → 출력 비어있으면 (`[ -e ]` false 또는 stat 미실행) PASS 처리, 그 외 모든
// expected line substring 매칭 검사. 모든 cmd 통과해야 최종 PASS.
// audit text expected의 mode/owner/group이 환경별 다르면 false FAIL — 운영자 manual 확인.
func synthesizeStatOpt(audit string) (string, bool) {
	checks, ok := extractStatOptChecks(audit)
	if !ok {
		return "", false
	}
	var sb strings.Builder
	sb.WriteString("missing=0\n")
	for i, c := range checks {
		fmt.Fprintf(&sb, "out_%d=$(%s 2>/dev/null)\n", i, c.cmd)
		fmt.Fprintf(&sb, "if [ -n \"$out_%d\" ]; then\n", i)
		for _, exp := range c.expecteds {
			fmt.Fprintf(&sb,
				"  printf '%%s' \"$out_%d\" | grep -qF -- %q || { printf 'miss-%d: %%s\\n' %q; missing=$((missing+1)); }\n",
				i, exp, i, exp)
		}
		sb.WriteString("fi\n")
	}
	sb.WriteString("if [ \"$missing\" -eq 0 ]; then printf '** PASS **\\n'; else printf '** FAIL **\\n'; fi")
	return sb.String(), true
}
