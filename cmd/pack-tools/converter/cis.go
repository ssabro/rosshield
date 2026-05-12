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
func extractCISLastShellLine(audit string) (string, bool) {
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
		return cand, true
	}
	return "", false
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
// 매핑:
//  1. "Nothing should be returned" / "Nothing is returned" → expect 빈 출력 → cmd 출력 비어야 PASS
//  2. "<X> is installed" 같은 긍정 echo 기대 → cmd 출력 비어 있지 않아야 PASS
//
// 기타 패턴(stat, find, getfacl 등)은 향후 epic. 현재는 두 패턴이 CIS 232건 중 ~100건 cover.
func synthesizeCISShellAssertion(audit string) (string, bool) {
	cmd, ok := extractCISLastShellLine(audit)
	if !ok {
		return "", false
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
	// regexpExpectEmpty은 "Nothing should be returned" 류 자연어를 매칭 (대소문자 무시).
	regexpExpectEmpty = regexp.MustCompile(`(?i)nothing\s+(should|is)\s+(be\s+)?returned|no\s+output\s+(should|is)\s+(be\s+)?returned`)
	// regexpExpectNonEmpty은 "<X> is installed" 같은 긍정 echo 기대 패턴 (대소문자 무시).
	// 같은 audit에 "Nothing should be returned"가 있으면 우선순위로 expect-empty가 잡힘 (위 switch).
	regexpExpectNonEmpty = regexp.MustCompile(`(?i)(is\s+installed|is\s+enabled|is\s+active)\b`)
)

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
