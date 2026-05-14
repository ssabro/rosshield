// CIS nftables include + awk hook block scan 자동 변환 — G3 (4.3.10, 90% 도달).
//
// audit text 패턴: 3 cmd `awk '/hook (input|forward|output)/,/}/' $(awk ... /etc/nftables.conf)`
// + "Output should be similar to:" phrase × 3 + 각 expected block에 `policy drop` 포함.
//
// 합성 단순화: cmd substitution + multi-line wrap 추출 회피, hardcoded 3 hook check (input/
// forward/output) + nftables.conf의 include된 conf 파일에서 각 hook block 추출 + policy drop
// substring 검증.
//
// 인식 narrow: `/etc/nftables.conf` + 3 `hook (input|forward|output)` + `policy drop` +
// `Output should be similar to:` phrase 모두 포함 → 4.3.10 specific.
//
// 잠재 변환률: 4.3.10 1건 → +0.3%p (312 기준). 90% 도달 마지막 epic.
//
// 참조: docs/design/notes/cis-nomarker-31-analysis.md §3 G3.

package converter

import (
	"regexp"
	"strings"
)

// regexpNftablesConfPath는 audit text에 `/etc/nftables.conf` 포함 검사.
var regexpNftablesConfPath = regexp.MustCompile(`/etc/nftables\.conf`)

// regexpHookInputForwardOutput는 3 hook 키워드 모두 등장 검사 (각각 별 매칭).
var regexpHookInput = regexp.MustCompile(`hook\s+input\b`)
var regexpHookForward = regexp.MustCompile(`hook\s+forward\b`)
var regexpHookOutput = regexp.MustCompile(`hook\s+output\b`)

// regexpPolicyDrop은 expected에 policy drop 포함 검사.
var regexpPolicyDrop = regexp.MustCompile(`policy\s+drop`)

// regexpOutputShouldBeSimilar는 "Output should be similar to" phrase 검사.
var regexpOutputShouldBeSimilar = regexp.MustCompile(`(?i)Output\s+should\s+be\s+similar\s+to`)

// isNftIncludeAuditText는 G3 합성 대상인지 판정 — 5 조건 AND.
func isNftIncludeAuditText(audit string) bool {
	return regexpNftablesConfPath.MatchString(audit) &&
		regexpHookInput.MatchString(audit) &&
		regexpHookForward.MatchString(audit) &&
		regexpHookOutput.MatchString(audit) &&
		regexpPolicyDrop.MatchString(audit) &&
		regexpOutputShouldBeSimilar.MatchString(audit)
}

// synthesizeNftInclude는 G3 4.3.10 합성 bash 생성.
//
// hardcoded 3 hook (input/forward/output) check:
//  1. /etc/nftables.conf의 include된 conf 파일 추출
//  2. 각 hook block 추출 (awk `/hook X/,/}/`)
//  3. 출력에 policy drop substring 포함이면 hook PASS
//  4. 모든 hook PASS이면 최종 PASS
//
// audit text가 hardcoded 가정과 일치하는지는 isNftIncludeAuditText narrow 조건이 보장.
func synthesizeNftInclude(audit string) (string, bool) {
	if !isNftIncludeAuditText(audit) {
		return "", false
	}
	const body = `config_files=$(awk '$1 ~ /^\s*include/ { gsub("\"","",$2); print $2 }' /etc/nftables.conf 2>/dev/null)
if [ -z "$config_files" ]; then printf 'fail: no include in /etc/nftables.conf\n'; printf '** FAIL **\n'; exit 0; fi
missing=0
for hook in input forward output; do
  block=$(awk "/hook $hook/,/}/" $config_files 2>/dev/null)
  printf '%s' "$block" | grep -qF -- "policy drop" || { printf 'miss-%s: policy drop\n' "$hook"; missing=$((missing+1)); }
done
if [ "$missing" -eq 0 ]; then printf '** PASS **\n'; else printf '** FAIL **\n'; fi`
	return body, true
}

// strings package import 가드 (미사용 회피 — 향후 helper 추가 시 활용).
var _ = strings.TrimSpace
