package converter

// ros2.go — ROS2 framework JSON(security baseline)을 rosshield pack으로 변환합니다 (E12 Stage B).
//
// 입력 형식: nrobotcheck `resources/baselines/ros2_*_security_baseline_framework_*.json`.
// 다국어 필드(name/name_en, description/description_en, ...)와 4계층 audit_command
// (conditions[] + pass_logic 트리)를 가진 도메인 특화 스키마.
//
// R8-4' 결정: 다중 conditions를 모두 single bash로 합치고 pass_logic을 bash if문으로
// 직번역. stdout에 `** PASS **`/`** FAIL **` 마커를 출력하고 evaluationRule은 단순
// `{"op":"contains","value":"** PASS **"}` 매핑 — CIS Stage C와 동일 패턴.
//
// 자동 변환 불가능한 condition(extract_pattern·numeric)은 *ConditionTranslateError로
// 신호되며, 호출자는 해당 check를 degraded marker(auditCommand="true" + INDETERMINATE
// evaluationRule)로 fallback합니다.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ROS2ConvertOptions는 변환 시 pack 메타와 언어 선호를 지정합니다.
type ROS2ConvertOptions struct {
	PackName        string // 미지정 시 ros2 document에서 fallback
	PackVersion     string // 미지정 시 "1.0.0"
	PackVendor      string // 미지정 시 "rosshield"
	PackDescription string
	PreferEnglish   bool // true면 *_en 필드 우선, false면 한국어(원본) 우선
}

// ROS2ConvertReport는 변환 통계와 degraded check 목록을 담습니다.
type ROS2ConvertReport struct {
	TotalItems int
	Converted  int      // 자동 변환 성공한 check 수
	Degraded   []string // "<check_id>: <reason>"
}

// ros2 framework JSON 와이어 형식 — 변환에 필요한 필드만 정의 (unknown 필드는 무시).
type ros2Document struct {
	SchemaVersion string     `json:"schema_version"`
	Items         []ros2Item `json:"items"`
}

type ros2Item struct {
	ID                 string            `json:"id"`
	Name               string            `json:"name"`
	NameEn             string            `json:"name_en"`
	Severity           string            `json:"severity"`
	Description        string            `json:"description"`
	DescriptionEn      string            `json:"description_en"`
	Rationale          string            `json:"rationale"`
	RationaleEn        string            `json:"rationale_en"`
	Remediation        string            `json:"remediation"`
	RemediationEn      string            `json:"remediation_en"`
	RemediationCommand string            `json:"remediation_command"`
	AuditCommand       *ros2AuditCommand `json:"audit_command"`
	IsAuto             bool              `json:"is_auto"`
}

type ros2AuditCommand struct {
	Conditions []ros2RawCondition `json:"conditions"`
	PassLogic  json.RawMessage    `json:"pass_logic"`
}

type ros2RawCondition struct {
	ID             string  `json:"id"`
	Description    string  `json:"description"`
	Command        string  `json:"command"`
	ExtractPattern *string `json:"extract_pattern"`
	ValueType      string  `json:"value_type"`
	Operator       string  `json:"operator"`
	Expected       any     `json:"expected"`
}

// 변환 에러.
var (
	ErrROS2DecodeFailed     = errors.New("converter: ros2 JSON decode failed")
	ErrROS2NoItems          = errors.New("converter: ros2 document has no items")
	ErrROS2DuplicateCheckID = errors.New("converter: ros2 duplicate check ID")
)

// degraded marker 상수 — 비자동 변환 check의 evaluationRule.
//
// auditCommand는 단순 `true`(no-op, exit 0) 출력. evaluationRule은 stdout에서 절대 매칭되지
// 않는 sentinel 문자열을 contains하므로 항상 FAIL이지만, 의도는 "수동 검수 필요"임을 알리는 것.
// Stage D Self-Test가 별도 selftest/cases.yaml로 degraded entries를 분류 — Phase 2에서
// 수동 fixture 추가 시 INDETERMINATE 또는 정상 평가로 전환.
var degradedEvalRuleJSON = json.RawMessage(`{"op":"contains","value":"<degraded — Phase 2 fixture required>"}`)

// passEvalRuleJSON은 single-bash combine 성공 시 적용되는 단순 마커 매칭 규칙.
var passEvalRuleJSON = json.RawMessage(`{"op":"contains","value":"** PASS **"}`)

// ConvertROS2는 ROS2 framework JSON 바이트를 rosshield pack으로 변환합니다.
//
// items 배열의 각 entry → 1 converter.Check (1:1 매핑).
// 변환 실패한 condition이 있는 item은 degraded marker로 fallback — 결코 error로 abort 안 함.
//
// 결과 Pack은 WriteToDir로 디스크에 펼치면 됨. ConvertROS2 자체는 디스크에 쓰지 않음.
func ConvertROS2(jsonBytes []byte, opts ROS2ConvertOptions) (Pack, ROS2ConvertReport, error) {
	dec := json.NewDecoder(bytes.NewReader(jsonBytes))
	dec.UseNumber()
	var doc ros2Document
	if err := dec.Decode(&doc); err != nil {
		return Pack{}, ROS2ConvertReport{}, fmt.Errorf("%w: %v", ErrROS2DecodeFailed, err)
	}
	if len(doc.Items) == 0 {
		return Pack{}, ROS2ConvertReport{}, ErrROS2NoItems
	}

	pack := Pack{
		Name:        firstNonEmpty(opts.PackName, "ros2-baseline"),
		Version:     firstNonEmpty(opts.PackVersion, "1.0.0"),
		Vendor:      firstNonEmpty(opts.PackVendor, "rosshield"),
		Description: opts.PackDescription,
	}
	report := ROS2ConvertReport{TotalItems: len(doc.Items)}
	seen := make(map[string]struct{}, len(doc.Items))

	for _, it := range doc.Items {
		if it.ID == "" {
			continue // skip — 잘못된 entry
		}
		if _, dup := seen[it.ID]; dup {
			return Pack{}, ROS2ConvertReport{}, fmt.Errorf("%w: %q", ErrROS2DuplicateCheckID, it.ID)
		}
		seen[it.ID] = struct{}{}

		check, degradedReason := convertROS2Item(it, opts.PreferEnglish)
		pack.Checks = append(pack.Checks, check)
		if degradedReason != "" {
			report.Degraded = append(report.Degraded, fmt.Sprintf("%s: %s", it.ID, degradedReason))
		} else {
			report.Converted++
		}
	}

	return pack, report, nil
}

// convertROS2Item은 단일 ros2 item을 converter.Check + (degraded reason 또는 빈 string)으로 변환.
//
// degraded reason이 비어있지 않으면 자동 변환 실패 — Phase 2 fixture 필요.
func convertROS2Item(it ros2Item, en bool) (Check, string) {
	title := pickLang(it.Name, it.NameEn, en)
	if title == "" {
		title = it.ID
	}
	description := pickLang(it.Description, it.DescriptionEn, en)
	rationale := pickLang(it.Rationale, it.RationaleEn, en)
	fix := pickLang(it.Remediation, it.RemediationEn, en)
	severity := normalizeSeverity(it.Severity)

	check := Check{
		ID:          it.ID,
		Title:       title,
		Description: description,
		Severity:    severity,
		Rationale:   rationale,
		FixGuidance: fix,
	}

	// audit_command 처리.
	if it.AuditCommand == nil || len(it.AuditCommand.Conditions) == 0 {
		check.AuditCommand = "true"
		check.EvaluationRule = degradedEvalRuleJSON
		return check, "no audit_command (manual review)"
	}

	// Single-bash combine 시도.
	script, degraded, err := buildBashCombine(it.AuditCommand.Conditions, it.AuditCommand.PassLogic)
	if err != nil {
		check.AuditCommand = "true"
		check.EvaluationRule = degradedEvalRuleJSON
		return check, fmt.Sprintf("translation failed: %v", err)
	}
	check.AuditCommand = wrapBash(script)
	check.EvaluationRule = passEvalRuleJSON
	return check, degraded
}

// buildBashCombine은 conditions + pass_logic을 단일 bash 스크립트로 합칩니다.
//
// 결과 형태:
//
//	set -o pipefail; C1_OUT="$(<command1> 2>&1)"; C2_OUT="$(<command2> 2>&1)";
//	if { <C1_test>; } && { <C2_test>; }; then echo '** PASS **'; else echo '** FAIL **'; fi
//
// degraded reason은 일부 conditions가 미지원 일 때 채워지며, 전체 변환을 abort하지는 않고
// 호출자가 마지막에 degraded marker로 fallback합니다 (즉, 부분 실패는 error로 매핑).
func buildBashCombine(conds []ros2RawCondition, passLogic json.RawMessage) (string, string, error) {
	condMap := make(map[string]ros2RawCondition, len(conds))
	for _, c := range conds {
		condMap[c.ID] = c
	}

	leaf := func(id string) (string, error) {
		c, ok := condMap[id]
		if !ok {
			return "", fmt.Errorf("pass_logic references unknown condition %q", id)
		}
		test, err := ConditionToBashTest(toROS2Condition(c))
		if err != nil {
			return "", err
		}
		return test, nil
	}

	var passLogicBash string
	if len(passLogic) == 0 {
		// pass_logic이 없으면 condition이 정확히 1개여야.
		if len(conds) != 1 {
			return "", "", fmt.Errorf("no pass_logic but %d conditions", len(conds))
		}
		test, err := leaf(conds[0].ID)
		if err != nil {
			return "", "", err
		}
		passLogicBash = test
	} else {
		bash, err := PassLogicToBash(passLogic, leaf)
		if err != nil {
			return "", "", err
		}
		passLogicBash = bash
	}

	var sb strings.Builder
	sb.WriteString("set -o pipefail; ")
	for _, c := range conds {
		sb.WriteString(ConditionSetupLine(toROS2Condition(c)))
		sb.WriteString("; ")
	}
	sb.WriteString("if ")
	sb.WriteString(passLogicBash)
	sb.WriteString("; then echo '** PASS **'; else echo '** FAIL **'; fi")
	return sb.String(), "", nil
}

// wrapBash는 bash script를 `bash -c '<script>'` 단일 string으로 감쌉니다.
//
// 결과는 rosshield Check.auditCommand로 직접 사용 가능 — script 안의 single-quote는
// POSIX 표준 escape(`'\”`)로 처리하여 outer single-quote와 충돌 없음.
func wrapBash(script string) string {
	return "bash -c " + shellSingleQuote(script)
}

// toROS2Condition은 wire-format ros2RawCondition을 변환기 표면 ROS2Condition로 매핑.
func toROS2Condition(c ros2RawCondition) ROS2Condition {
	pattern := ""
	if c.ExtractPattern != nil {
		pattern = *c.ExtractPattern
	}
	return ROS2Condition{
		ID:             c.ID,
		Command:        c.Command,
		ExtractPattern: pattern,
		ValueType:      c.ValueType,
		Operator:       c.Operator,
		Expected:       c.Expected,
	}
}

// pickLang은 한국어 우선(en=false) 또는 영어 우선(en=true)으로 둘 중 하나를 반환합니다.
//
// 선호 언어가 비어있으면 다른 언어로 fallback — 둘 다 비면 빈 string.
func pickLang(ko, en string, preferEnglish bool) string {
	if preferEnglish {
		if en != "" {
			return en
		}
		return ko
	}
	if ko != "" {
		return ko
	}
	return en
}

// normalizeSeverity는 ros2 severity(원본 텍스트)를 rosshield severity로 매핑합니다.
//
// ros2 데이터는 한국어 severity(상/중/하·치명적·...)일 수 있어 정규화가 필요. 인식 못 하면
// DefaultSeverity("medium").
func normalizeSeverity(s string) string {
	low := strings.ToLower(strings.TrimSpace(s))
	switch low {
	case "low", "low-medium", "minor":
		return "low"
	case "medium", "moderate":
		return "medium"
	case "high", "high-severity":
		return "high"
	case "critical", "severe":
		return "critical"
	}
	// 한국어 severity 매핑 (관찰: '상'·'중'·'하'·'치명적' 등).
	switch s {
	case "상", "높음":
		return "high"
	case "중", "보통":
		return "medium"
	case "하", "낮음":
		return "low"
	case "치명적", "심각":
		return "critical"
	}
	return DefaultSeverity
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
