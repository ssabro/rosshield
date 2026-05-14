// CIS 6.2.3.x auditd rules 자동 변환 — Stage 1 (인식기·추출기).
//
// 본 파일은 cis.go의 합성 dispatch에서 호출되는 auditctl 전용 인식기·추출기를 분리합니다.
// cis.go가 800줄 한계 임박이라 D-N-8 결정에 따라 신규 파일로 분리. 향후 6.2.4.x 확장
// 시 동일 파일에 추가.
//
// 6.2.3.x 21건 중 신규 cover 19건(6.2.3.20 이미 합성 + 6.2.3.21 Manual). 합성 함수
// (synthesizeAuditctlMatch + normalize)는 Stage 2 후속.
//
// 참조: docs/design/notes/cis-6-2-3-auditd-design.md §6.1 Stage 1.

package converter

import (
	"regexp"
	"strings"
)

// regexpAuditctlList는 audit text 어디든 `auditctl -l` 명령이 포함되어 있는지 감지합니다.
// 6.2.3.x running 검증의 시그니처 — 합성 대상 1차 게이트. 7.2.x 등 다른 grep verify
// 패턴(auditctl 미사용)을 negative로 거름. prefix 다양(`# auditctl -l`, `{`, `&& auditctl -l`,
// `[ -n "${UID_MIN}" ] && auditctl -l |` 등)이라 단어 경계만 검사.
var regexpAuditctlList = regexp.MustCompile(`\bauditctl\s+-l\b`)

// regexpAuditctlVerifyMatches는 "Verify the output (matches|includes)" 또는
// "Verify output of (matches|includes)" phrase를 검출합니다(6.2.3.4 변형 cover).
// 6.2.3.x audit text는 on-disk + running 각각 1회씩 등장 → 2회 매칭 기대.
var regexpAuditctlVerifyMatches = regexp.MustCompile(`(?i)Verify\s+(the\s+)?output\s+(of\s+)?(matches|includes)\b`)

// regexpAuditctlRule은 expected 라인이 audit rule 형식인지 검증합니다.
//
//	-w /path -p X -k key                         (file watch)
//	-a always,exit -F arch=... -S ... -k key     (syscall rule)
var regexpAuditctlRule = regexp.MustCompile(`^(-w\s+/\S+|-a\s+(always|never),(exit|entry|user|task|exclude)\b)`)

// isAuditctlAuditText는 audit text가 6.2.3.x auditd 합성 대상인지 판정합니다.
//
// 3 조건 AND:
//  1. `auditctl -l` 명령 포함 (running 검증 존재)
//  2. "Verify the output matches/includes" phrase 1+회
//  3. audit rule 형식 라인 1+ 존재 (`-w /...` 또는 `-a always,exit ...`)
//
// 7.2.x grep verify 등 auditctl 미사용 패턴은 negative.
func isAuditctlAuditText(audit string) bool {
	if !regexpAuditctlList.MatchString(audit) {
		return false
	}
	if !regexpAuditctlVerifyMatches.MatchString(audit) {
		return false
	}
	for _, raw := range strings.Split(audit, "\n") {
		if regexpAuditctlRule.MatchString(strings.TrimSpace(raw)) {
			return true
		}
	}
	return false
}

// extractAuditctlExpectedRules는 audit text에서 on-disk + running expected 라인을 추출합니다.
//
// audit text 구조:
//
//	On disk configuration
//	... # cmd ...
//	Verify the output matches:
//	  <rules>           ← block 1 (on-disk)
//	Running configuration
//	... # auditctl -l ...
//	Verify the output matches:
//	  <rules>           ← block 2 (running)
//
// 두 phrase 직후 라인부터 다음 빈 줄·heading·"#" 시작 라인 직전까지 audit rule 라인만 수집.
// 멀티라인 wrap은 Stage 2 normalize에서 처리(본 함수는 raw 라인만 추출).
//
// 반환:
//   - onDisk, running []string: 각각 expected 라인 슬라이스
//   - ok bool: 두 block 모두 1+ 라인 추출 시 true
func extractAuditctlExpectedRules(audit string) (onDisk, running []string, ok bool) {
	indexes := regexpAuditctlVerifyMatches.FindAllStringIndex(audit, -1)
	if len(indexes) < 2 {
		return nil, nil, false
	}
	blocks := make([]string, 0, len(indexes))
	for i, idx := range indexes {
		start := idx[1]
		// trailing colon ":" skip
		if start < len(audit) && audit[start] == ':' {
			start++
		}
		end := len(audit)
		if i+1 < len(indexes) {
			end = indexes[i+1][0]
		}
		blocks = append(blocks, audit[start:end])
	}
	onDisk = collectAuditRuleLines(blocks[0])
	running = collectAuditRuleLines(blocks[1])
	if len(onDisk) == 0 || len(running) == 0 {
		return nil, nil, false
	}
	return onDisk, running, true
}

// collectAuditRuleLines는 verify block에서 audit rule 라인만 추출합니다.
// 종료 조건: 빈 줄(rule 1+ 수집 후) / "#" 시작 라인 / audit rule 패턴 미매칭 라인.
// 단, expected 라인이 multi-line wrap된 continuation(공백 시작 + audit rule prefix 부재)은
// 이전 라인에 join — Stage 2 normalize에서 정밀 처리하지만 본 단계에서도 안전한 join 시도.
func collectAuditRuleLines(block string) []string {
	var out []string
	for _, raw := range strings.Split(block, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			if len(out) > 0 {
				break
			}
			continue
		}
		if strings.HasPrefix(line, "#") {
			break
		}
		if regexpAuditctlRule.MatchString(line) {
			out = append(out, line)
			continue
		}
		// continuation: 직전 라인이 audit rule이고 본 라인이 새 rule이 아니면 join.
		if len(out) > 0 {
			out[len(out)-1] = out[len(out)-1] + " " + line
			continue
		}
		// rule이 시작되기 전에 비-rule 라인 → 종료.
		break
	}
	return out
}
