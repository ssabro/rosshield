// CIS hashbang body PASSED/FAILED emit 자동 변환 — G10 부분 cover (5.4.3.2).
//
// audit text 패턴: hashbang body가 자체적으로 `echo -e "\nPASSED\n..."` 또는
// `echo -e "\nFAILED\n..."` 출력 emit. body 실행 → 출력에 "PASSED" substring이면 PASS,
// "FAILED" substring이면 FAIL.
//
// 5.4.1.6 (shebang 없는 `{}` block + expect-empty)은 별 합성기 필요(extractCISBashBody가
// shebang 강제) — 본 합성기 비대상, 별 fix epic.
//
// 잠재 변환률: 5.4.3.2 1건 → +0.3%p (312 기준).
//
// 참조: docs/design/notes/cis-nomarker-31-analysis.md §3 G10.

package converter

import (
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"
)

// regexpHashbangBodyPassedEmit는 body 안 PASSED emit 키워드 substring 검사.
// audit text raw string은 `\nPASSED\n` 형식이라 word boundary 가드는 미작동(`n` 앞 letter)
// → 단순 substring 매칭(uppercase 정확).
var regexpHashbangBodyPassedEmit = regexp.MustCompile(`PASSED`)

// regexpHashbangBodyFailedEmit는 FAILED emit substring 검사.
var regexpHashbangBodyFailedEmit = regexp.MustCompile(`FAILED`)

// regexpVerifyNothingReturned는 "verify nothing is returned" phrase 감지 (5.4.1.6).
var regexpVerifyNothingReturned = regexp.MustCompile(`(?i)verify\s+nothing\s+is\s+returned`)

// regexpAuditPassed는 body 안 "Audit Passed" / "Audit Result: ** PASS **" emit 감지 (4.2.6).
var regexpAuditPassedEmit = regexp.MustCompile(`(?i)Audit\s+Passed`)

// regexpAuditFailEmit는 body 안 "** FAIL **" / "Audit Result:" 키워드 감지 (4.2.6).
var regexpAuditFailEmit = regexp.MustCompile(`\*\*\s*FAIL\s*\*\*`)

// regexpAllOutputIsOK는 "all output is OK" / "Verify that all output is OK" phrase 감지 (6.2.3.6).
var regexpAllOutputIsOK = regexp.MustCompile(`(?i)all\s+output\s+is\s+OK`)

// regexpIPv6NotEnabledEmit는 body 안 "is not enabled" emit 감지 (3.1.1).
var regexpIPv6NotEnabledEmit = regexp.MustCompile(`(?i)is\s+not\s+enabled`)

// regexpIPv6EnabledEmit는 body 안 "is enabled" emit 감지 (3.1.1).
var regexpIPv6EnabledEmit = regexp.MustCompile(`(?i)is\s+enabled`)

// regexpHashbangBodyOKEmit는 body 안 `OK:` printf emit 감지 (6.2.3.6 시그니처).
var regexpHashbangBodyOKEmit = regexp.MustCompile(`printf\s+"OK:`)

// regexpHashbangBodyWarningEmit는 body 안 `Warning:` printf emit 감지.
var regexpHashbangBodyWarningEmit = regexp.MustCompile(`printf\s+"Warning:`)

// regexpBraceBlockStart는 `{` 단독 라인(블록 시작) 감지.
var regexpBraceBlockStart = regexp.MustCompile(`^\s*\{\s*$`)

// regexpBraceBlockEnd는 `}` 단독 라인(블록 끝) 감지.
var regexpBraceBlockEnd = regexp.MustCompile(`^\s*\}\s*$`)

// isHashbangPassFailEmitAuditText는 G10 5.4.3.2 합성 대상인지 판정.
//
// 인식 조건:
//   - extractCISBashBody가 hashbang body 추출 가능
//   - body 안 "PASSED" + "FAILED" 둘 다 substring 포함 (자체 emit 시그니처)
func isHashbangPassFailEmitAuditText(audit string) bool {
	body, ok := extractCISBashBody(audit)
	if !ok {
		return false
	}
	return regexpHashbangBodyPassedEmit.MatchString(body) &&
		regexpHashbangBodyFailedEmit.MatchString(body)
}

// synthesizeHashbangPassFailEmit는 hashbang body를 base64 wrap → 실행 → PASSED/FAILED
// substring 매칭 합성 bash 생성.
//
// body 안 single quote/escape sequence를 base64 인코딩으로 안전하게 보존(synthesizeBashBodyExpectEmpty
// 패턴 일관). 출력에 "PASSED"이면 PASS, 그 외(FAILED 또는 출력 부재)이면 FAIL.
func synthesizeHashbangPassFailEmit(audit string) (string, bool) {
	if !isHashbangPassFailEmitAuditText(audit) {
		return "", false
	}
	body, _ := extractCISBashBody(audit)
	encoded := base64.StdEncoding.EncodeToString([]byte(body))
	var sb strings.Builder
	fmt.Fprintf(&sb, "out=$(printf '%%s' %q | base64 -d | bash 2>/dev/null)\n", encoded)
	sb.WriteString("case \"$out\" in\n")
	sb.WriteString("  *PASSED*) printf '** PASS **\\n' ;;\n")
	sb.WriteString("  *) printf 'fail: %s\\n' \"$out\"; printf '** FAIL **\\n' ;;\n")
	sb.WriteString("esac")
	return sb.String(), true
}

// isHashbangAllOKAuditText는 6.2.3.6 합성 대상인지 판정.
//
// 인식 조건:
//   - extractCISBashBody hashbang body 추출 가능
//   - body 안 `printf "OK:` + `printf "Warning:` 둘 다 substring 포함
//   - audit text에 "all output is OK" phrase
func isHashbangAllOKAuditText(audit string) bool {
	if !regexpAllOutputIsOK.MatchString(audit) {
		return false
	}
	body, ok := extractCISBashBody(audit)
	if !ok {
		return false
	}
	return regexpHashbangBodyOKEmit.MatchString(body) &&
		regexpHashbangBodyWarningEmit.MatchString(body)
}

// synthesizeHashbangAllOK는 6.2.3.6 합성 bash 생성.
//
// hashbang body 그대로 base64 wrap → 실행 → 출력에 "Warning:" substring 미포함이면 PASS.
// "all output is OK" 의도(Warning 없으면 통과).
func synthesizeHashbangAllOK(audit string) (string, bool) {
	if !isHashbangAllOKAuditText(audit) {
		return "", false
	}
	body, _ := extractCISBashBody(audit)
	encoded := base64.StdEncoding.EncodeToString([]byte(body))
	var sb strings.Builder
	fmt.Fprintf(&sb, "out=$(printf '%%s' %q | base64 -d | bash 2>/dev/null)\n", encoded)
	sb.WriteString("if printf '%s' \"$out\" | grep -qF -- \"Warning:\"; then\n")
	sb.WriteString("  printf 'fail: %s\\n' \"$out\"; printf '** FAIL **\\n'\n")
	sb.WriteString("else\n")
	sb.WriteString("  printf '** PASS **\\n'\n")
	sb.WriteString("fi")
	return sb.String(), true
}

// isHashbangIPv6DisabledAuditText는 3.1.1 합성 대상 판정.
//
// 인식 조건:
//   - extractCISBashBody hashbang body 추출
//   - body 안 "is not enabled" + "is enabled" 둘 다 substring 포함 (자체 emit)
//   - audit text에 "ipv6" 키워드 (case insensitive) 포함
func isHashbangIPv6DisabledAuditText(audit string) bool {
	body, ok := extractCISBashBody(audit)
	if !ok {
		return false
	}
	if !regexpIPv6NotEnabledEmit.MatchString(body) || !regexpIPv6EnabledEmit.MatchString(body) {
		return false
	}
	// audit text 또는 body에 IPv6 키워드 (false trigger 회피).
	combined := audit + body
	return regexp.MustCompile(`(?i)\bIPv6\b`).MatchString(combined)
}

// synthesizeHashbangIPv6Disabled는 3.1.1 합성 — IPv6 disabled가 PASS, enabled가 FAIL.
//
// audit text body는 "- IPv6 is not enabled" 또는 "- IPv6 is enabled" emit. CIS 보안 권장은
// IPv6 disabled (또는 운영자 정책에 따라). 본 합성은 disabled stance — 출력에 "is not enabled"
// substring이면 PASS, "is enabled"이면 FAIL. 운영자가 IPv6 enabled 정책이면 FAIL → manual 확인.
func synthesizeHashbangIPv6Disabled(audit string) (string, bool) {
	if !isHashbangIPv6DisabledAuditText(audit) {
		return "", false
	}
	body, _ := extractCISBashBody(audit)
	encoded := base64.StdEncoding.EncodeToString([]byte(body))
	var sb strings.Builder
	fmt.Fprintf(&sb, "out=$(printf '%%s' %q | base64 -d | bash 2>/dev/null)\n", encoded)
	sb.WriteString("if printf '%s' \"$out\" | grep -qiE 'is not enabled'; then\n")
	sb.WriteString("  printf '** PASS **\\n'\n")
	sb.WriteString("else\n")
	sb.WriteString("  printf 'fail: %s\\n' \"$out\"\n")
	sb.WriteString("  printf '** FAIL **\\n'\n")
	sb.WriteString("fi")
	return sb.String(), true
}

// extractBraceBlock은 audit text에서 `{` ~ `}` block을 추출 (shebang 없이).
//
// 인식 조건: `{` 단독 라인 + 1+ body 라인 + `}` 단독 라인.
// 첫 발견된 block만 추출(audit text의 단일 검증 script 가정).
func extractBraceBlock(audit string) (string, bool) {
	lines := strings.Split(audit, "\n")
	startIdx := -1
	for i, raw := range lines {
		if regexpBraceBlockStart.MatchString(raw) {
			startIdx = i
			break
		}
	}
	if startIdx < 0 {
		return "", false
	}
	for j := startIdx + 1; j < len(lines); j++ {
		if regexpBraceBlockEnd.MatchString(lines[j]) {
			block := strings.Join(lines[startIdx:j+1], "\n")
			return block, true
		}
	}
	return "", false
}

// isBraceBlockEmptyAuditText는 5.4.1.6 합성 대상인지 판정.
//
// 인식 조건:
//   - "verify nothing is returned" phrase
//   - `{` ~ `}` block (shebang 없는)
func isBraceBlockEmptyAuditText(audit string) bool {
	if !regexpVerifyNothingReturned.MatchString(audit) {
		return false
	}
	_, ok := extractBraceBlock(audit)
	return ok
}

// isAuditResultEmitAuditText는 G5 4.2.6 합성 대상인지 판정.
//
// 인식 조건:
//   - extractCISBashBody가 hashbang body 추출 가능
//   - body 안 "Audit Passed" + "** FAIL **" 둘 다 substring 포함 (자체 emit 시그니처)
func isAuditResultEmitAuditText(audit string) bool {
	body, ok := extractCISBashBody(audit)
	if !ok {
		return false
	}
	return regexpAuditPassedEmit.MatchString(body) &&
		regexpAuditFailEmit.MatchString(body)
}

// synthesizeAuditResultEmit는 hashbang body를 base64 wrap → 실행 → "Audit Passed" substring
// 매칭 합성 bash 생성 (4.2.6 specific).
//
// "Audit Passed" 출력이면 PASS, 그 외(`** FAIL **` 또는 출력 부재)이면 FAIL.
// G10의 synthesizeHashbangPassFailEmit과 동일 base64 wrap + case 분기 패턴.
func synthesizeAuditResultEmit(audit string) (string, bool) {
	if !isAuditResultEmitAuditText(audit) {
		return "", false
	}
	body, _ := extractCISBashBody(audit)
	encoded := base64.StdEncoding.EncodeToString([]byte(body))
	var sb strings.Builder
	fmt.Fprintf(&sb, "out=$(printf '%%s' %q | base64 -d | bash 2>/dev/null)\n", encoded)
	sb.WriteString("case \"$out\" in\n")
	sb.WriteString("  *Audit?Passed*) printf '** PASS **\\n' ;;\n")
	sb.WriteString("  *) printf 'fail: %s\\n' \"$out\"; printf '** FAIL **\\n' ;;\n")
	sb.WriteString("esac")
	return sb.String(), true
}

// synthesizeBraceBlockEmpty는 `{}` block을 base64 wrap → 실행 → 출력 비어있으면 PASS 합성.
//
// audit text 의도 (5.4.1.6): block 안 echo 발동 시 비-PASS 사용자 발견 → 출력 non-empty이면
// FAIL. 출력 비어있으면 PASS.
func synthesizeBraceBlockEmpty(audit string) (string, bool) {
	if !isBraceBlockEmptyAuditText(audit) {
		return "", false
	}
	block, _ := extractBraceBlock(audit)
	encoded := base64.StdEncoding.EncodeToString([]byte(block))
	var sb strings.Builder
	fmt.Fprintf(&sb, "out=$(printf '%%s' %q | base64 -d | bash 2>/dev/null)\n", encoded)
	sb.WriteString("if [ -z \"$out\" ]; then printf '** PASS **\\n'; else printf 'fail: %s\\n' \"$out\"; printf '** FAIL **\\n'; fi")
	return sb.String(), true
}
