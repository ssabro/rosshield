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

	if !strings.Contains(it.Audit, "** PASS **") {
		check.AuditCommand = "true"
		check.EvaluationRule = degradedEvalRuleJSON
		return check, "audit lacks ** PASS ** marker (natural-language manual)"
	}

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
