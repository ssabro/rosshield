// CIS ufw status default policy 자동 변환 — G6 부분 cover (4.2.7).
//
// audit text 패턴: 단일 `# ufw status verbose | grep Default:` cmd + "Example output:" phrase
// + `Default: deny (incoming), deny (outgoing), disabled (routed)` example.
//
// 합성: cmd 실행 → "Default:" 시작 라인에 alternation `deny|reject|disabled` 1+ 매칭 → PASS.
// "deny, reject, or disabled" 의미를 반영(audit text phrase에 명시).
//
// 4.2.4 (multi-line table + 2 cmd 복잡)는 별 epic 후속.
//
// 잠재 변환률: 4.2.7 1건 → +0.3%p (312 기준).
//
// 참조: docs/design/notes/cis-nomarker-31-analysis.md §3 G6.

package converter

import (
	"regexp"
	"strings"
)

// regexpUfwStatusDefaultCmd는 `# ufw status verbose | grep Default:` 명령 라인 감지.
var regexpUfwStatusDefaultCmd = regexp.MustCompile(`^#\s+(ufw\s+status\s+verbose\s*\|\s*grep\s+Default:?)\s*$`)

// regexpUfwExampleOutput는 "Example output:" phrase 감지(cmd 직후 위치 확인용).
var regexpUfwExampleOutput = regexp.MustCompile(`(?i)Example\s+output:?`)

// regexpUfwDefaultLine는 audit text의 example "Default:" 라인 감지(인식 강도 ↑).
var regexpUfwDefaultLine = regexp.MustCompile(`(?i)^Default:\s+(deny|reject|allow|disabled)`)

// isUfwStatusDefaultAuditText는 G6 4.2.7 합성 대상인지 판정.
//
// 인식 조건: 3가지 모두 매칭
//  1. `# ufw status verbose | grep Default:` cmd
//  2. "Example output:" phrase
//  3. "Default:" example 라인 (audit text 안)
func isUfwStatusDefaultAuditText(audit string) bool {
	if !regexpUfwExampleOutput.MatchString(audit) {
		return false
	}
	hasCmd := false
	hasDefaultLine := false
	for _, raw := range strings.Split(audit, "\n") {
		line := strings.TrimSpace(raw)
		if regexpUfwStatusDefaultCmd.MatchString(line) {
			hasCmd = true
		}
		if regexpUfwDefaultLine.MatchString(line) {
			hasDefaultLine = true
		}
	}
	return hasCmd && hasDefaultLine
}

// synthesizeUfwStatusDefault는 cmd 실행 + "Default:" 라인 alternation 매칭 합성 bash 생성.
//
// `ufw status verbose | grep Default:` 출력에서 `^Default:` 시작 + (deny|reject|disabled)
// 알파벳 매칭이면 PASS, 그 외 FAIL. allow는 제외 — audit text 의도 (deny/reject/disabled만 허용).
func synthesizeUfwStatusDefault(audit string) (string, bool) {
	if !isUfwStatusDefaultAuditText(audit) {
		return "", false
	}
	const body = `out=$(ufw status verbose 2>/dev/null | grep -i '^Default:')
if [ -z "$out" ]; then printf 'fail: no Default line\n'; printf '** FAIL **\n'; exit 0; fi
if printf '%s' "$out" | grep -qiE '(deny|reject|disabled)'; then
  printf '** PASS **\n'
else
  printf 'fail: %s\n' "$out"
  printf '** FAIL **\n'
fi`
	return body, true
}
