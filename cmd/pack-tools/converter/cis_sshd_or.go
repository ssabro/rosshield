// CIS sshd -T multi-line OR 자동 변환 — E-1 epic G11 (5.1.4 + 5.1.14).
//
// audit text 패턴: `# sshd -T | grep <key>` 명령 + "matches at least one" 또는
// "matches X or Y" phrase + 2+ expected 라인(separator `-OR-`/`- OR -`).
//
// 합성: cmd 실행 → 각 expected 라인을 case insensitive substring 매칭 → 1+ 매칭이면 PASS.
// expected 라인의 `<placeholder>`는 제거 후 prefix substring 사용. case insensitive로
// sshd -T 출력의 lowercase keyword 매칭 (예: "loglevel VERBOSE" expected ↔ "loglevel verbose"
// 출력).
//
// 잠재 변환률: 5.1.4 + 5.1.14 = 2건 → +0.6%p (312 기준).
//
// 참조: docs/design/notes/cis-nomarker-31-analysis.md §3 G11 + §4 E-1.

package converter

import (
	"fmt"
	"regexp"
	"strings"
)

// regexpSshdGrepCmd는 `# sshd -T | grep ...` 명령 라인 감지.
var regexpSshdGrepCmd = regexp.MustCompile(`^#\s+(sshd\s+-T\s+\|\s+grep\s+.+)$`)

// regexpSshdMatchesPhrase는 "matches at least one" / "matches loglevel" / "matches X or Y" /
// "matches the following" phrase 검출.
var regexpSshdMatchesPhrase = regexp.MustCompile(`(?i)\bmatches\b.*?\b(at\s+least\s+one|loglevel|or|the\s+following)\b`)

// regexpOrSeparator는 multi-line OR separator (-OR-, - OR -) 감지.
var regexpOrSeparator = regexp.MustCompile(`^-\s*OR\s*-\s*$`)

// regexpPlaceholder는 expected 라인의 `<placeholder>` 부분 매칭(제거 대상).
var regexpPlaceholder = regexp.MustCompile(`<[^>]+>`)

// extractSshdGrepOrChecks는 audit text에서 cmd + expected alternations를 추출.
//
// 인식 조건:
//   - 1+ `# sshd -T | grep ...` 명령
//   - "matches X" phrase (위 regex)
//   - 2+ expected 라인 (placeholder 제거 후 non-empty)
//
// expected 수집은 phrase 직후 라인부터 separator/IF-block/Note/Review/Example/빈 줄까지.
// `<placeholder>`는 제거 후 trim — case insensitive substring 매칭에 사용.
func extractSshdGrepOrChecks(audit string) (cmd string, alternations []string, ok bool) {
	lines := strings.Split(audit, "\n")
	phraseIdx := -1
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		if cmd == "" {
			if m := regexpSshdGrepCmd.FindStringSubmatch(line); m != nil {
				cmd = m[1]
			}
		}
		if phraseIdx < 0 && regexpSshdMatchesPhrase.MatchString(line) {
			phraseIdx = i
		}
	}
	if cmd == "" || phraseIdx < 0 {
		return "", nil, false
	}
	for i := phraseIdx + 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			if len(alternations) > 0 {
				break
			}
			continue
		}
		if regexpOrSeparator.MatchString(line) {
			continue
		}
		if strings.HasPrefix(line, "#") {
			// shell prompt — phrase가 cmd보다 위에 등장한 경우(5.1.14 첫 라인). 알파벳 라인 본격 시작 전 skip.
			continue
		}
		if strings.HasPrefix(line, "- IF -") || strings.HasPrefix(line, "Note:") ||
			strings.HasPrefix(line, "Review") || strings.HasPrefix(line, "Example") {
			break
		}
		cleaned := strings.TrimSpace(regexpPlaceholder.ReplaceAllString(line, ""))
		if cleaned == "" {
			continue
		}
		alternations = append(alternations, cleaned)
	}
	if len(alternations) < 2 {
		return "", nil, false
	}
	return cmd, alternations, true
}

// isSshdGrepOrAuditText는 G11 합성 대상인지 판정.
func isSshdGrepOrAuditText(audit string) bool {
	_, _, ok := extractSshdGrepOrChecks(audit)
	return ok
}

// synthesizeSshdGrepOr는 cmd 실행 + 각 expected substring 매칭 합성 bash 생성.
//
// 1+ alternation 매칭이면 PASS, 모두 미매칭이면 FAIL. case insensitive (sshd -T 출력 lowercase
// 매칭). expected 라인은 grep -qiF로 정확 substring 검사 (regex 특수문자 escape 자동).
func synthesizeSshdGrepOr(audit string) (string, bool) {
	cmd, alternations, ok := extractSshdGrepOrChecks(audit)
	if !ok {
		return "", false
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "out=$(%s 2>/dev/null)\n", cmd)
	sb.WriteString("found=0\n")
	sb.WriteString("for token in")
	for _, a := range alternations {
		fmt.Fprintf(&sb, " %q", a)
	}
	sb.WriteString("; do\n")
	sb.WriteString("  printf '%s\\n' \"$out\" | grep -qiF -- \"$token\" && { found=1; break; }\n")
	sb.WriteString("done\n")
	sb.WriteString("if [ \"$found\" -eq 1 ]; then printf '** PASS **\\n'; else printf '** FAIL **\\n'; fi")
	return sb.String(), true
}
