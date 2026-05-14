// CIS apparmor count 검증 자동 변환 — E-3 epic G8 (1.3.1.3 + 1.3.1.4).
//
// audit text 패턴: 2 cmd `apparmor_status | grep profiles` + `apparmor_status | grep processes`
// + count expected 라인 (`N profiles are loaded.` / `M profiles are in complain mode.` /
// `K processes are unconfined ...`).
//
// 합성: apparmor_status 출력에서 카운트 추출 + 비교:
//   - profiles loaded > 0 (둘 다 공통)
//   - unconfined == 0 (둘 다 공통)
//   - complain == 0 (1.3.1.4 strict mode만, D-E3-2 phrase 자동 판정)
//
// mode 판정 (D-E3-2 권장 default): audit text phrase "not in complain" 매칭 시 strict,
// 그 외 ("either enforce or complain") either. ID 매칭은 baseline 변경 깨질 위험 회피.
//
// 잠재 변환률: 1.3.1.3 + 1.3.1.4 = 2건 → +0.6%p (312 기준).
//
// 참조: docs/design/notes/cis-stat-apparmor-dpkg-design.md §3.2 G8 + §6 D-E3-2.

package converter

import (
	"regexp"
	"strings"
)

// regexpApparmorStatusCmd는 `apparmor_status | grep` 명령 시그니처 감지 (`#` 옵셔널).
var regexpApparmorStatusCmd = regexp.MustCompile(`apparmor_status\s*\|\s*grep`)

// regexpApparmorStrictMode는 strict mode phrase ("not in complain mode" / "not in complain")
// 감지. 매칭 시 complain == 0 검증 추가.
var regexpApparmorStrictMode = regexp.MustCompile(`(?i)not\s+in\s+complain(\s+mode)?\b`)

// apparmorMode는 mode phrase 자동 판정 결과.
type apparmorMode int

const (
	apparmorEitherMode apparmorMode = iota // either enforce or complain (1.3.1.3)
	apparmorStrictMode                     // not in complain (1.3.1.4)
)

// extractApparmorAudit는 audit text에서 mode를 판정.
//
// 인식 조건: `apparmor_status | grep` cmd 시그니처 1+. mode는 phrase 자동 판정 (D-E3-2).
func extractApparmorAudit(audit string) (apparmorMode, bool) {
	if !regexpApparmorStatusCmd.MatchString(audit) {
		return 0, false
	}
	if regexpApparmorStrictMode.MatchString(audit) {
		return apparmorStrictMode, true
	}
	return apparmorEitherMode, true
}

// isApparmorCountAuditText는 G8 합성 대상인지 판정.
func isApparmorCountAuditText(audit string) bool {
	_, ok := extractApparmorAudit(audit)
	return ok
}

// synthesizeApparmorCount는 apparmor count 검증 합성 bash를 생성.
//
// apparmor_status 출력에서 awk로 카운트 추출 → 비교. mode == strict이면 complain == 0 추가.
// missing 카운트 0이면 PASS.
func synthesizeApparmorCount(audit string) (string, bool) {
	mode, ok := extractApparmorAudit(audit)
	if !ok {
		return "", false
	}
	var sb strings.Builder
	sb.WriteString("profiles_out=$(apparmor_status 2>/dev/null | grep profiles)\n")
	sb.WriteString("processes_out=$(apparmor_status 2>/dev/null | grep processes)\n")
	sb.WriteString("loaded=$(printf '%s\\n' \"$profiles_out\" | awk '/profiles are loaded/{print $1; exit}')\n")
	sb.WriteString("complain=$(printf '%s\\n' \"$profiles_out\" | awk '/profiles are in complain mode/{print $1; exit}')\n")
	sb.WriteString("unconfined=$(printf '%s\\n' \"$processes_out\" | awk '/processes are unconfined/{print $1; exit}')\n")
	sb.WriteString("loaded=${loaded:-0}\n")
	sb.WriteString("complain=${complain:-0}\n")
	sb.WriteString("unconfined=${unconfined:-0}\n")
	sb.WriteString("missing=0\n")
	sb.WriteString("[ \"$loaded\" -gt 0 ] || { printf 'fail: profiles loaded=%s\\n' \"$loaded\"; missing=$((missing+1)); }\n")
	sb.WriteString("[ \"$unconfined\" -eq 0 ] || { printf 'fail: unconfined=%s\\n' \"$unconfined\"; missing=$((missing+1)); }\n")
	if mode == apparmorStrictMode {
		sb.WriteString("[ \"$complain\" -eq 0 ] || { printf 'fail: complain=%s\\n' \"$complain\"; missing=$((missing+1)); }\n")
	}
	sb.WriteString("if [ \"$missing\" -eq 0 ]; then printf '** PASS **\\n'; else printf '** FAIL **\\n'; fi")
	return sb.String(), true
}
