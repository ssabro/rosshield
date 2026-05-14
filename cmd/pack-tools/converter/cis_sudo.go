// CIS sudo cache timeout 자동 변환 — G13 (5.2.6).
//
// audit text 패턴: `# grep -roP "timestamp_timeout=\K[0-9]*" /etc/sudoers*` cmd +
// "no more than 15 minutes" phrase + "default is 15 minutes" 기본값 명시.
//
// 합성: timestamp_timeout 추출 → 미설정 시 default 15 → 정수 ≤ 15 검증.
// -1(disabled)은 명시적 FAIL (사용자 의도와 어긋남, audit text Note 참고).
//
// 잠재 변환률: 5.2.6 1건 → +0.3%p (312 기준).
//
// 참조: docs/design/notes/cis-nomarker-31-analysis.md §3 G13.

package converter

import (
	"regexp"
	"strings"
)

// regexpSudoTimestampTimeoutCmd는 `grep -roP "timestamp_timeout=...` 명령 검출.
var regexpSudoTimestampTimeoutCmd = regexp.MustCompile(`grep\s+-roP\s+"timestamp_timeout=`)

// regexpSudoNoMoreThan15는 "no more than 15 minutes" phrase 검출.
var regexpSudoNoMoreThan15 = regexp.MustCompile(`(?i)no\s+more\s+than\s+15\s+minutes?`)

// isSudoTimestampTimeoutAuditText는 G13 5.2.6 합성 대상인지 판정 (2 조건 AND).
func isSudoTimestampTimeoutAuditText(audit string) bool {
	return regexpSudoTimestampTimeoutCmd.MatchString(audit) &&
		regexpSudoNoMoreThan15.MatchString(audit)
}

// synthesizeSudoTimestampTimeout는 sudo cache timeout 검증 합성 bash 생성.
//
// /etc/sudoers* 에서 첫 timestamp_timeout 값 추출 → 미설정 시 default 15 → 정수 ≤ 15이면 PASS.
// -1 (disabled) 또는 비정수는 FAIL.
func synthesizeSudoTimestampTimeout(audit string) (string, bool) {
	if !isSudoTimestampTimeoutAuditText(audit) {
		return "", false
	}
	const body = `val=$(grep -roP "timestamp_timeout=\K[0-9-]+" /etc/sudoers* 2>/dev/null | head -1)
[ -z "$val" ] && val=15
case "$val" in
  -*|*[!0-9-]*) printf 'invalid: %s\n' "$val"; printf '** FAIL **\n' ;;
  *) if [ "$val" -le 15 ]; then printf '** PASS **\n'; else printf 'too high: %s\n' "$val"; printf '** FAIL **\n'; fi ;;
esac`
	_ = strings.TrimSpace // import guard
	return body, true
}
