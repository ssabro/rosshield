package converter

// cis.go — CIS Ubuntu 24.04 baseline JSON을 rosshield pack으로 변환합니다 (E12 Stage C).
//
// 입력 형식: nrobotcheck `resources/baselines/cis_ubuntu_2404_benchmark.json` (312 items).
//
// R8-3' (보정 후): CIS audit 필드는 사실 자연어 가이드 + bash 블록이 혼합된 매뉴얼 텍스트.
// 312 items 분석 결과:
//
//   - PASS/FAIL 마커 + bash hashbang 모두 갖춘 자동 변환 가능 items: ~61
//   - 마커 없거나 자연어만 있는 items: ~250 → degraded marker
//   - assessment_status="Manual" items: 21 → degraded marker
//
// 자동 변환 가능 items는 `#!/usr/bin/env bash`(또는 `#!/bin/bash`) 이후의 bash 본문만 추출,
// `bash -c '<extracted body>'`로 wrap. evaluationRule은 단순 `{"op":"contains","value":"** PASS **"}`
// — bash 스크립트가 항상 PASS 또는 FAIL 마커 중 하나만 출력하기 때문.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// CISConvertOptions는 변환 시 pack 메타를 지정합니다.
type CISConvertOptions struct {
	PackName        string // 미지정 시 "cis-ubuntu-2404"
	PackVersion     string // 미지정 시 "1.0.0"
	PackVendor      string // 미지정 시 "rosshield"
	PackDescription string
}

// CISConvertReport는 변환 통계와 degraded check 목록을 담습니다.
type CISConvertReport struct {
	TotalItems       int
	Converted        int      // 자동 변환된 check 수
	DegradedManual   int      // assessment_status=Manual로 분류된 수
	DegradedNoMarker int      // PASS/FAIL 마커 또는 hashbang 부재로 분류된 수
	Degraded         []string // "<check_id>: <reason>"
}

// CIS JSON 와이어 형식 — 변환에 필요한 필드만 정의 (unknown 필드는 무시).
type cisDocument struct {
	Benchmark string    `json:"benchmark"`
	Version   string    `json:"version"`
	Date      string    `json:"date"`
	Items     []cisItem `json:"items"`
}

type cisItem struct {
	ID                   string   `json:"id"`
	Title                string   `json:"title"`
	AssessmentStatus     string   `json:"assessment_status"`
	ProfileApplicability []string `json:"profile_applicability"`
	Description          string   `json:"description"`
	Rationale            string   `json:"rationale"`
	Audit                string   `json:"audit"`
	Remediation          string   `json:"remediation"`
}

// CIS 변환 에러.
var (
	ErrCISDecodeFailed     = errors.New("converter: cis JSON decode failed")
	ErrCISNoItems          = errors.New("converter: cis document has no items")
	ErrCISDuplicateCheckID = errors.New("converter: cis duplicate check ID")
)

// cisAutoEvalRuleJSON은 자동 변환된 CIS check의 evaluationRule입니다.
//
// CIS bash 스크립트는 항상 `printf '%s\n' "" "- Audit Result:" " ** PASS **"` 또는
// `... " ** FAIL **"` 둘 중 하나만 출력하므로 단순 contains로 충분.
var cisAutoEvalRuleJSON = json.RawMessage(`{"op":"contains","value":"** PASS **"}`)

// ConvertCIS는 CIS Ubuntu baseline JSON 바이트를 rosshield pack으로 변환합니다.
//
// items 배열의 각 entry → 1 converter.Check (1:1 매핑).
// 자동 변환 불가능한 items(자연어 audit·Manual 분류)는 degraded marker로 fallback —
// 결코 error로 abort 안 함.
func ConvertCIS(jsonBytes []byte, opts CISConvertOptions) (Pack, CISConvertReport, error) {
	dec := json.NewDecoder(bytes.NewReader(jsonBytes))
	dec.UseNumber()
	var doc cisDocument
	if err := dec.Decode(&doc); err != nil {
		return Pack{}, CISConvertReport{}, fmt.Errorf("%w: %v", ErrCISDecodeFailed, err)
	}
	if len(doc.Items) == 0 {
		return Pack{}, CISConvertReport{}, ErrCISNoItems
	}

	pack := Pack{
		Name:        firstNonEmpty(opts.PackName, "cis-ubuntu-2404"),
		Version:     firstNonEmpty(opts.PackVersion, "1.0.0"),
		Vendor:      firstNonEmpty(opts.PackVendor, "rosshield"),
		Description: firstNonEmpty(opts.PackDescription, doc.Benchmark),
	}
	report := CISConvertReport{TotalItems: len(doc.Items)}
	seen := make(map[string]struct{}, len(doc.Items))

	for _, it := range doc.Items {
		if it.ID == "" {
			continue
		}
		if _, dup := seen[it.ID]; dup {
			return Pack{}, CISConvertReport{}, fmt.Errorf("%w: %q", ErrCISDuplicateCheckID, it.ID)
		}
		seen[it.ID] = struct{}{}

		check, degradedReason := convertCISItem(it)
		pack.Checks = append(pack.Checks, check)
		switch {
		case degradedReason == "":
			report.Converted++
		case strings.Contains(degradedReason, "Manual"):
			report.DegradedManual++
		default:
			report.DegradedNoMarker++
		}
		if degradedReason != "" {
			report.Degraded = append(report.Degraded, fmt.Sprintf("%s: %s", it.ID, degradedReason))
		}
	}
	return pack, report, nil
}

// convertCISItem은 단일 cis item을 converter.Check + (degraded reason 또는 빈 string)으로 변환.
//
// 자동 변환 조건: assessment_status=Automated AND PASS 마커 AND bash hashbang 추출 가능.
// 그 외는 degraded marker — audit/remediation 텍스트는 rationale·fixGuidance에 보존되므로
// 사용자가 수동으로 evaluationRule을 추가할 수 있음.
func convertCISItem(it cisItem) (Check, string) {
	check := Check{
		ID:          it.ID,
		Title:       it.Title,
		Description: it.Description,
		Severity:    DefaultSeverity, // CIS는 명시 severity 없음
		Rationale:   it.Rationale,
		FixGuidance: it.Remediation,
	}

	if it.AssessmentStatus == "Manual" {
		check.AuditCommand = "true"
		check.EvaluationRule = degradedEvalRuleJSON
		return check, "assessment_status=Manual (manual review required)"
	}

	// 자동 변환 우선순위:
	//  1) 표준 PASS 마커 + bash hashbang (기존 로직)
	//  2) "Nothing should be returned" 패턴 + 마지막 shell line 추출 (CIS 자연어 가이드 다수)
	//  3) "is installed" 또는 dpkg-query &&/echo 긍정 기대 패턴
	//  4) stat/ls 파일 권한 검증 (octal mode + Uid:( 0/root))
	//  5) sshd -T | grep <option> 동적 설정 검증 (yes/no)
	//  6) sshd -T | grep <option> 수치 검증 (-le N / -ge N / -gt 0)
	if strings.Contains(it.Audit, "** PASS **") {
		body, ok := extractCISBashBody(it.Audit)
		if !ok {
			check.AuditCommand = "true"
			check.EvaluationRule = degradedEvalRuleJSON
			return check, "audit lacks bash hashbang"
		}
		check.AuditCommand = wrapBash(body)
		check.EvaluationRule = cisAutoEvalRuleJSON
		return check, ""
	}

	// Pattern 2/3: 자연어 가이드 + 마지막 shell line + 기대 결과 키워드 추출.
	if synthesized, ok := synthesizeCISShellAssertion(it.Audit); ok {
		check.AuditCommand = wrapBash(synthesized)
		check.EvaluationRule = cisAutoEvalRuleJSON
		return check, ""
	}

	check.AuditCommand = "true"
	check.EvaluationRule = degradedEvalRuleJSON
	return check, "audit lacks ** PASS ** marker (natural-language manual)"
}

// cisShellLineRe는 audit 텍스트에서 한 줄 shell 명령(`# <cmd>`)을 매칭합니다.
//
// CIS audit 가이드는 공식적으로 `# <command>` 줄로 명령을 표시 — `# Run the following...`
// 같은 자연어 줄은 제외(자체 휴리스틱).
var cisShellLineRe = regexp.MustCompile(`(?m)^\s*#\s+(\S.*\S|\S)\s*$`)

// shellLineSkipRe는 audit 텍스트의 `# ...` 줄 중 자연어/주석 텍스트를 건너뜁니다.
//
// CIS는 `# Run the following`, `# Example output:` 등 자연어를 같은 prefix로 사용.
var shellLineSkipRe = regexp.MustCompile(`(?i)^(run\s|example|verify|the\s|note:|to\s|nothing\s|or\s|where\s|if\s)`)

// extractCISLastShellLine은 audit 본문 마지막 shell line을 반환합니다 (자연어 줄 제외).
//
// 마지막 명령을 우선시하는 이유: CIS audit 가이드는 일반적으로 자연어 설명 → 명령
// 순서로 작성됨. 마지막 명령이 실제 검증 명령일 가능성 가장 높음.
//
// multi-line 흡수: 첫 # 줄 cmd가 dangling token(`--`/`\\`/`|`/`&`) 또는 unmatched quote를
// 가지면 다음 줄들을 close 또는 자연어 경계까지 흡수(no space join — quoted regex 의미 보존).
// CIS audit은 PDF rendering 한계로 단일 PCRE regex가 여러 줄로 분할되어 작성된 케이스
// 다수(5.1.6 Ciphers · 5.1.12 KexAlgorithms · 5.1.15 MACs 등). 단일 줄만 추출하면 grep
// 인자 누락 false PASS 위험.
func extractCISLastShellLine(audit string) (string, bool) {
	indices := cisShellLineRe.FindAllStringSubmatchIndex(audit, -1)
	matches := cisShellLineRe.FindAllStringSubmatch(audit, -1)
	for i := len(matches) - 1; i >= 0; i-- {
		cand := strings.TrimSpace(matches[i][1])
		if cand == "" || shellLineSkipRe.MatchString(cand) {
			continue
		}
		// '#' 시작 자연어 줄 후속(예: `# Example output:`) 회피 — 명령처럼 보이는 줄만 사용.
		// 실 명령은 `findmnt`, `dpkg-query`, `grep`, `stat`, `sshd -T |` 등 식별자로 시작.
		if !looksLikeShellCommand(cand) {
			continue
		}
		// 첫 줄 cmd가 incomplete이면 다음 줄들 흡수 시도 (matches[i][3]은 첫 줄 cmd 끝 offset).
		afterFirst := audit[indices[i][3]:]
		return absorbCISContinuation(cand, afterFirst), true
	}
	return "", false
}

// absorbCISContinuation은 첫 # 줄 cmd가 incomplete(dangling token 또는 unmatched quote)이면
// 다음 줄들을 close 또는 자연어/주석 경계까지 흡수 후 join합니다.
//
// 종료 조건 (먼저 매치되는 것):
//  1. cmd가 complete(quote balanced + no dangling token) — 정상 흡수 완료
//  2. 다음 줄이 # 또는 - 시작 — 다음 명령 또는 자연어 (5.1.6의 "- IF -" 등)
//  3. 다음 줄이 자연어 prefix(Run/Note/Verify/...) — shellLineSkipRe 매칭
//  4. safety limit (8 lines / 4096 chars) — runaway 방어
//
// join 전략 (quote-balance 기반):
//  - accumulated가 unmatched quote(quoted regex 안) → newline 제거 no-space join
//    예: `aes(128|192|256))-` + `cbc|arcfour...` → `aes(128|192|256))-cbc|arcfour...` 정확 복원
//  - accumulated가 balanced quote(보통 dangling flag/operator로 끝남) → space join
//    예: `sshd -T | grep -Pi --` + `'^ciphers...` → `sshd -T | grep -Pi -- '^ciphers...`
//        no-space join 시 `--'^ciphers'` 가 grep `--^ciphers` 단일 패턴으로 파싱되어 false PASS
func absorbCISContinuation(first, after string) string {
	if isCISCmdComplete(first) {
		return first
	}
	const maxLines = 8
	const maxLen = 4096
	accumulated := first
	consumed := 0
	for _, line := range strings.Split(after, "\n") {
		if consumed >= maxLines || len(accumulated) > maxLen {
			break
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "-") {
			break
		}
		if shellLineSkipRe.MatchString(trimmed) {
			break
		}
		if quotedContext(accumulated) {
			accumulated += trimmed
		} else {
			accumulated += " " + trimmed
		}
		consumed++
		if isCISCmdComplete(accumulated) {
			break
		}
	}
	return accumulated
}

// quotedContext는 cmd가 unmatched quote(quoted regex/string 안) 상태인지 반환.
// true면 다음 줄 join 시 newline 제거 no-space (regex 분할 token 복원).
// false면 space join (dangling flag 다음 인자가 별 토큰으로 분리되어야 안전).
func quotedContext(cmd string) bool {
	return strings.Count(cmd, "'")%2 != 0 || strings.Count(cmd, `"`)%2 != 0
}

// isCISCmdComplete는 cmd가 complete(흡수 종료 가능)인지 판정.
//
// incomplete 조건:
//   - dangling getopt `--` (인자 필요, 5.1.6 `sshd -T | grep -Pi --`)
//   - dangling backslash `\\` (POSIX line continuation)
//   - dangling pipe `|` 또는 `&` (다음 명령 필요)
//   - unmatched single/double quote (quoted regex 미닫힘)
//
// trailing single hyphen `-`은 quoted regex alt 안의 hyphen 가능성으로 별도 검사 안 함
// — 이 경우 quote balance 검사가 unmatched로 잡아 줌.
func isCISCmdComplete(cmd string) bool {
	trimmed := strings.TrimRight(cmd, " \t")
	if strings.HasSuffix(trimmed, "--") ||
		strings.HasSuffix(trimmed, "\\") ||
		strings.HasSuffix(trimmed, "|") ||
		strings.HasSuffix(trimmed, "&") {
		return false
	}
	return strings.Count(cmd, "'")%2 == 0 && strings.Count(cmd, `"`)%2 == 0
}

// looksLikeShellCommand는 한 줄 텍스트가 실제 shell 명령인지 휴리스틱으로 판정합니다.
//
// 첫 토큰이 알파벳·숫자·`/`·`_`로 시작하면 명령으로 간주. 자연어 prefix는 별도 skip.
func looksLikeShellCommand(line string) bool {
	if line == "" {
		return false
	}
	c := line[0]
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '/' || c == '_'
}

// synthesizeCISShellAssertion은 자연어 가이드에서 expected outcome을 추론해 bash를 합성합니다.
//
// 매핑 (sshd 분기는 일반 expect-empty/non-empty보다 우선 — 같은 audit에 "Nothing should be
// returned"가 동시 존재해도 sshd 옵션 검증이 더 정확):
//  1. sshd 수치 옵션 — `is N or less`/`greater than zero` → 모든 출력 라인 마지막 토큰 정수 비교
//  2. sshd boolean — "set to (yes|no)" → 마지막 토큰 lowercase 비교
//  3. stat/ls 파일 권한 — octal mode → ≤ expected 비교 + Uid 검증
//  4. "Nothing should be returned" → expect 빈 출력
//  5. "<X> is installed/enabled/active" → 비어 있지 않아야 PASS
func synthesizeCISShellAssertion(audit string) (string, bool) {
	cmd, ok := extractCISLastShellLine(audit)
	if !ok {
		return "", false
	}
	if isSSHDCommand(cmd) {
		if op, threshold, ok2 := extractExpectedSSHDNumeric(audit); ok2 {
			return synthesizeExpectSSHDNumeric(cmd, op, threshold), true
		}
		if val, ok2 := extractExpectedSSHDValue(audit); ok2 {
			return synthesizeExpectSSHDOption(cmd, val), true
		}
	}
	if isStatCommand(cmd) {
		if mode, ok2 := extractExpectedOctalMode(audit); ok2 {
			return synthesizeExpectStatPerm(cmd, mode), true
		}
	}
	switch {
	case regexpExpectEmpty.MatchString(audit):
		return synthesizeExpectEmpty(cmd), true
	case regexpExpectNonEmpty.MatchString(audit):
		return synthesizeExpectNonEmpty(cmd), true
	}
	return "", false
}

var (
	// regexpExpectEmpty은 "Nothing should be returned" / "no output should be returned" /
	// "should not be returned/in use" / "No <X...> should be returned" 류 자연어를 매칭
	// (대소문자 무시).
	//
	// 추가 변형 cover:
	//  - "should not be returned/in use" — 직접 부정 표현
	//  - "No <subject>...should be returned" — 5.1.6 Ciphers / 5.1.15 MACs / 5.1.12 KexAlgorithms
	//    "No ciphers in the list below should be returned" 형태 (subject 사이 단어 0~10개)
	//
	// multi-line cmd 흡수(absorbCISContinuation) 적용 후 안전 — 이전엔 첫 줄만 추출돼
	// grep 인자 누락 false PASS 위험으로 비활성화 상태였음.
	regexpExpectEmpty = regexp.MustCompile(`(?i)nothing\s+(should|is)\s+(be\s+)?returned|no\s+output\s+(should|is)\s+(be\s+)?returned|should\s+not\s+be\s+(returned|in\s+use)|no\s+\S+(?:\s+\S+){0,10}\s+should\s+be\s+returned`)
	// regexpExpectNonEmpty은 "<X> is installed" 같은 긍정 echo 기대 패턴 (대소문자 무시).
	// 같은 audit에 "Nothing should be returned"가 있으면 우선순위로 expect-empty가 잡힘 (위 switch).
	regexpExpectNonEmpty = regexp.MustCompile(`(?i)(is\s+installed|is\s+enabled|is\s+active)\b`)
	// regexpExpectedOctalMode는 audit 텍스트에서 첫 8진수 mode를 추출합니다.
	// CIS 7.x 시스템 파일 권한 가이드는 보통 "Access: (0640/-rw-r-----)" 또는
	// "0640 or more restrictive" 형태로 expected mode를 명시.
	regexpExpectedOctalMode = regexp.MustCompile(`0[0-7]{3,4}`)
	// regexpExpectedSSHDValue는 "set to yes" / "set to no" / "set to a value of yes" 등에서
	// expected boolean 값을 추출합니다 (대소문자 무시, 첫 매치 사용).
	regexpExpectedSSHDValue = regexp.MustCompile(`(?i)set\s+to\s+(?:a\s+value\s+of\s+)?["']?(yes|no)["']?`)
	// regexpExpectedSSHDLessOrEqual는 "is N or less" / "verify ... is N or less" 형태에서
	// 임계값 N을 추출합니다 (5.1.16 MaxAuthTries · 5.1.17 MaxSessions 등).
	regexpExpectedSSHDLessOrEqual = regexp.MustCompile(`(?i)is\s+(\d+)\s+or\s+less`)
	// regexpExpectedSSHDGreaterOrEqual는 "is N or more" 형태에서 N을 추출합니다.
	regexpExpectedSSHDGreaterOrEqual = regexp.MustCompile(`(?i)is\s+(\d+)\s+or\s+more`)
	// regexpExpectedSSHDPositive는 "greater than zero" / "are greater than zero" 형태를 매칭
	// (5.1.7 ClientAliveInterval/CountMax 등 양수 검증).
	regexpExpectedSSHDPositive = regexp.MustCompile(`(?i)(?:are|is)\s+greater\s+than\s+zero`)
)

// isStatCommand는 cmd line이 `stat ` 으로 시작하는지 검사 (LS 가이드도 일부 stat 명령으로 정규화됨).
func isStatCommand(cmd string) bool {
	return strings.HasPrefix(cmd, "stat ")
}

// isSSHDCommand는 cmd line이 `sshd ` 또는 `sshd\t` 로 시작하는지 검사.
func isSSHDCommand(cmd string) bool {
	return strings.HasPrefix(cmd, "sshd ")
}

// extractExpectedOctalMode는 audit 텍스트에서 첫 8진수 mode(예 "0640")를 반환.
func extractExpectedOctalMode(audit string) (string, bool) {
	m := regexpExpectedOctalMode.FindString(audit)
	if m == "" {
		return "", false
	}
	return m, true
}

// extractExpectedSSHDValue는 audit 텍스트에서 expected boolean 값("yes" 또는 "no")을 반환.
func extractExpectedSSHDValue(audit string) (string, bool) {
	m := regexpExpectedSSHDValue.FindStringSubmatch(audit)
	if len(m) < 2 {
		return "", false
	}
	return strings.ToLower(m[1]), true
}

// extractExpectedSSHDNumeric은 audit 텍스트에서 sshd 옵션의 수치 비교 op·threshold를 반환.
//
// 우선순위: "is N or less" → ("le", N) / "is N or more" → ("ge", N) / "greater than zero" → ("gt", 0).
// 같은 audit에 둘 이상 매칭 시 첫 발견 우선(보수적). 미매칭 시 ok=false.
func extractExpectedSSHDNumeric(audit string) (op string, threshold int, ok bool) {
	if m := regexpExpectedSSHDLessOrEqual.FindStringSubmatch(audit); len(m) >= 2 {
		n, err := strconv.Atoi(m[1])
		if err == nil {
			return "le", n, true
		}
	}
	if m := regexpExpectedSSHDGreaterOrEqual.FindStringSubmatch(audit); len(m) >= 2 {
		n, err := strconv.Atoi(m[1])
		if err == nil {
			return "ge", n, true
		}
	}
	if regexpExpectedSSHDPositive.MatchString(audit) {
		return "gt", 0, true
	}
	return "", 0, false
}

// synthesizeExpectEmpty는 cmd 출력이 비어 있으면 PASS, 아니면 FAIL 출력하는 bash를 생성.
func synthesizeExpectEmpty(cmd string) string {
	return "out=\"$(" + cmd + " 2>/dev/null)\"\n" +
		"if [ -z \"$out\" ]; then\n" +
		"  printf '%s\\n' \"** PASS **\"\n" +
		"else\n" +
		"  printf '%s\\n' \"** FAIL **\"\n" +
		"fi\n"
}

// synthesizeExpectNonEmpty는 cmd 출력이 비어 있지 않으면 PASS, 비어 있으면 FAIL 출력하는 bash 생성.
func synthesizeExpectNonEmpty(cmd string) string {
	return "out=\"$(" + cmd + " 2>/dev/null)\"\n" +
		"if [ -n \"$out\" ]; then\n" +
		"  printf '%s\\n' \"** PASS **\"\n" +
		"else\n" +
		"  printf '%s\\n' \"** FAIL **\"\n" +
		"fi\n"
}

// synthesizeExpectStatPerm는 stat 명령 출력에서 octal mode를 추출해 expectedMode 이하인지(즉
// 더 제한적이거나 같음) 확인 + Uid:( 0/ root) 가 포함되어야 PASS 출력.
//
// CIS 7.x 시스템 파일 권한 가이드는 거의 모두 "owned by root, group root, mode ≤ X" 형태이므로
// expectedOwner는 "root"로 고정. 향후 group 일치(shadow 등)는 별도 epic.
//
// stat 명령 형식 가정: `stat -Lc 'Access: (%#a/%A) Uid: ( %u/ %U) Gid: ( %g/ %G)' /path`
// 이 형식은 CIS audit 가이드의 표준 출력 형태.
func synthesizeExpectStatPerm(cmd, expectedMode string) string {
	return "out=\"$(" + cmd + " 2>/dev/null)\"\n" +
		"mode=\"$(printf '%s\\n' \"$out\" | sed -n 's|.*Access: (\\([0-7]\\{3,4\\}\\)/.*|\\1|p' | head -1)\"\n" +
		"if [ -n \"$mode\" ] && [ \"$((8#$mode))\" -le \"$((8#" + expectedMode + "))\" ] " +
		"&& printf '%s\\n' \"$out\" | grep -qE 'Uid: \\(\\s*0/'; then\n" +
		"  printf '%s\\n' \"** PASS **\"\n" +
		"else\n" +
		"  printf '%s\\n' \"** FAIL **\"\n" +
		"fi\n"
}

// synthesizeExpectSSHDOption은 sshd -T grep 출력의 마지막 토큰을 lowercase로 expectedValue와
// 비교해 PASS/FAIL 출력. CIS 5.1.x sshd 옵션 검증의 표준 패턴.
//
// sshd -T 출력은 `<option> <value>` 한 줄 형태이고, grep으로 옵션 필터된 후 awk가
// 마지막 필드 추출. expectedValue는 "yes" 또는 "no" (extractExpectedSSHDValue가 lowercase로 정규화).
func synthesizeExpectSSHDOption(cmd, expectedValue string) string {
	return "out=\"$(" + cmd + " 2>/dev/null)\"\n" +
		"val=\"$(printf '%s\\n' \"$out\" | awk '{print tolower($NF)}')\"\n" +
		"if [ \"$val\" = \"" + expectedValue + "\" ]; then\n" +
		"  printf '%s\\n' \"** PASS **\"\n" +
		"else\n" +
		"  printf '%s\\n' \"** FAIL **\"\n" +
		"fi\n"
}

// synthesizeExpectSSHDNumeric은 sshd -T grep 출력의 모든 라인 마지막 토큰을 정수로 추출해
// op·threshold와 비교 — 모든 라인이 비교 통과해야 PASS, 출력 비어 있으면 FAIL.
//
// op는 bash test operator: "le" (≤) / "ge" (≥) / "gt" (>) / "lt" (<).
//
// 두 옵션 동시 검증(5.1.7 ClientAliveInterval+CountMax) 케이스도 cover — grep 한 번에 두 라인
// 출력, 둘 다 임계값 비교 통과해야 PASS.
//
// 비정수 마지막 토큰(예: 옵션이 string 값)은 즉시 FAIL — false positive 회피.
func synthesizeExpectSSHDNumeric(cmd, op string, threshold int) string {
	return "out=\"$(" + cmd + " 2>/dev/null)\"\n" +
		"if [ -z \"$out\" ]; then\n" +
		"  printf '%s\\n' \"** FAIL **\"\n" +
		"  exit 0\n" +
		"fi\n" +
		"fail=0\n" +
		"while IFS= read -r line; do\n" +
		"  [ -z \"$line\" ] && continue\n" +
		"  val=\"$(printf '%s' \"$line\" | awk '{print $NF}')\"\n" +
		"  case \"$val\" in *[!0-9]*|\"\") fail=1; break ;; esac\n" +
		"  if ! [ \"$val\" -" + op + " " + strconv.Itoa(threshold) + " ]; then\n" +
		"    fail=1\n" +
		"    break\n" +
		"  fi\n" +
		"done <<EOF\n" +
		"$out\n" +
		"EOF\n" +
		"if [ \"$fail\" -eq 0 ]; then\n" +
		"  printf '%s\\n' \"** PASS **\"\n" +
		"else\n" +
		"  printf '%s\\n' \"** FAIL **\"\n" +
		"fi\n"
}

// extractCISBashBody는 CIS audit 텍스트에서 bash hashbang 이후 본문을 추출합니다.
//
// CIS audit 필드는 자연어 가이드 + bash 블록이 혼합된 형태:
//
//	"Run the following script to verify:\n- IF - the cramfs ... \n#!/usr/bin/env bash\n{...}"
//
// hashbang 위치를 찾아 그 이후만 반환 — bash -c는 첫 줄이 #로 시작하면 comment로 무시.
//
// 두 hashbang 모두 미존재 시 ok=false (degraded marker로 분류).
func extractCISBashBody(audit string) (string, bool) {
	for _, hashbang := range []string{"#!/usr/bin/env bash", "#!/bin/bash"} {
		if idx := strings.Index(audit, hashbang); idx >= 0 {
			return audit[idx:], true
		}
	}
	return "", false
}
