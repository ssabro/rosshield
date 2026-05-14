// CIS apt updates 검증 자동 변환 — E-4 1.2.2.1.
//
// audit text 패턴: 2 cmd `# apt update` + `# apt -s upgrade` + "Verify there are no updates
// or patches to install" phrase.
//
// 합성: `apt -s upgrade` 출력에 "Inst" prefix 라인 0건이면 PASS (설치 대기 패키지 없음).
//
// 잠재 변환률: 1.2.2.1 1건 → +0.3%p (312 기준).
//
// 참조: docs/design/notes/cis-manual-21-fixture-design.md §3 자동 후보 4건.

package converter

import "regexp"

// regexpAptUpgradeCmd는 `# apt -s upgrade` 명령 시그니처 감지.
var regexpAptUpgradeCmd = regexp.MustCompile(`(?m)^\s*#\s+apt\s+-s\s+upgrade\s*$`)

// regexpVerifyNoUpdatesPhrase는 "Verify there are no updates" / "no patches to install" phrase 감지.
var regexpVerifyNoUpdatesPhrase = regexp.MustCompile(`(?i)Verify\s+there\s+are\s+no\s+updates\b|no\s+patches\s+to\s+install`)

// isAptNoUpdatesAuditText는 1.2.2.1 합성 대상 판정.
func isAptNoUpdatesAuditText(audit string) bool {
	return regexpAptUpgradeCmd.MatchString(audit) &&
		regexpVerifyNoUpdatesPhrase.MatchString(audit)
}

// synthesizeAptNoUpdates는 apt -s upgrade 출력에 "Inst" 라인 0건 검증 합성 bash 생성.
//
// `apt -s upgrade` simulate 출력에서 "Inst" prefix 라인은 설치될 패키지. count 0이면
// no updates pending → PASS, > 0이면 FAIL (운영자가 apt upgrade 실행 필요).
func synthesizeAptNoUpdates(audit string) (string, bool) {
	if !isAptNoUpdatesAuditText(audit) {
		return "", false
	}
	const body = `count=$(apt -s upgrade 2>/dev/null | grep -cE '^Inst\s+')
if [ "$count" -eq 0 ]; then
  printf '** PASS **\n'
else
  printf 'fail: %s package(s) pending update\n' "$count"
  printf '** FAIL **\n'
fi`
	return body, true
}
