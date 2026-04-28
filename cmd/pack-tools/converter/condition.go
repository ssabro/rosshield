package converter

// condition.go — ROS2 framework의 단일 condition을 bash test 표현식으로 변환합니다 (R8-4').
//
// 변환 전제:
//
//   - 각 condition은 미리 실행되어 결과가 bash 변수 `<id>_OUT`에 저장된 상태.
//   - test 표현식은 if 본문에 직접 들어갈 수 있어야 함 (괄호 없는 raw form).
//
// 미지원 operator/특성은 ConditionTranslateError로 신호 — 호출자(ros2.go)가 degraded 마커로 처리.
//
// 1차 미지원 (Phase 2 후속):
//   - extract_pattern (정규식 추출)        ← 11 items
//   - value_type=number 또는 numeric op    ← gt/gte/lt/lte 약 30 items

import (
	"fmt"
	"strings"
)

// ConditionTranslateError는 단일 condition을 bash로 변환할 수 없을 때 반환됩니다.
type ConditionTranslateError struct {
	ConditionID string
	Reason      string
}

func (e *ConditionTranslateError) Error() string {
	return fmt.Sprintf("converter: condition %s untranslatable: %s", e.ConditionID, e.Reason)
}

// 미지원 사유 sentinel (errors.Is 비교용은 아니지만 reason 표준화).
const (
	ReasonExtractPattern = "extract_pattern not supported (Phase 2)"
	ReasonNumericOp      = "numeric operator not supported (Phase 2)"
	ReasonNumberValue    = "value_type=number not supported (Phase 2)"
	ReasonUnknownOp      = "unknown operator"
)

// ROS2Condition은 변환에 필요한 condition 필드만 포함합니다 (ros2 도메인 무관 표현).
type ROS2Condition struct {
	ID             string
	Command        string
	ExtractPattern string // 빈 string이면 사용 안 함
	ValueType      string // "string"|"number"
	Operator       string
	Expected       any // string|int|null
}

// ConditionToBashTest는 미리 실행된 결과 변수($<ID>_OUT)에 대한 bash test 표현식을 반환합니다.
//
// 자동 변환이 불가능하면 *ConditionTranslateError — 호출자가 degraded marker로 처리.
func ConditionToBashTest(c ROS2Condition) (string, error) {
	if c.ExtractPattern != "" {
		return "", &ConditionTranslateError{ConditionID: c.ID, Reason: ReasonExtractPattern}
	}
	if c.ValueType == "number" {
		return "", &ConditionTranslateError{ConditionID: c.ID, Reason: ReasonNumberValue}
	}
	varRef := `"$` + c.ID + `_OUT"`
	expectedStr := ""
	if c.Expected != nil {
		expectedStr = fmt.Sprintf("%v", c.Expected)
	}
	switch strings.ToLower(c.Operator) {
	case "empty":
		return fmt.Sprintf(`[ -z %s ]`, varRef), nil
	case "not_empty":
		return fmt.Sprintf(`[ -n %s ]`, varRef), nil
	case "equals":
		return fmt.Sprintf(`[ %s = %s ]`, varRef, shellSingleQuote(expectedStr)), nil
	case "not_equals":
		return fmt.Sprintf(`[ %s != %s ]`, varRef, shellSingleQuote(expectedStr)), nil
	case "contains":
		return fmt.Sprintf(`printf '%%s' %s | grep -qF %s`, varRef, shellSingleQuote(expectedStr)), nil
	case "regex":
		return fmt.Sprintf(`printf '%%s' %s | grep -qE %s`, varRef, shellSingleQuote(expectedStr)), nil
	case "gt", "gte", "lt", "lte":
		return "", &ConditionTranslateError{ConditionID: c.ID, Reason: ReasonNumericOp}
	default:
		return "", &ConditionTranslateError{
			ConditionID: c.ID,
			Reason:      ReasonUnknownOp + ": " + c.Operator,
		}
	}
}

// ConditionSetupLine은 condition.command를 미리 실행하여 $<id>_OUT에 저장하는 bash 라인입니다.
//
// command를 명령 치환 안에 그대로 두므로(`$(...)`) command 안의 `$`·backtick은 평가됩니다 —
// 이것이 audit 스크립트의 의도된 동작 (예: lsmod | grep cramfs는 동적 출력).
//
// stderr는 `2>&1`로 stdout에 합쳐 변수 한 개로 통합 — empty/not_empty 평가 직관성.
func ConditionSetupLine(c ROS2Condition) string {
	return fmt.Sprintf(`%s_OUT="$(%s 2>&1)"`, c.ID, c.Command)
}

// shellSingleQuote는 bash single-quote 문자열로 안전하게 감쌉니다.
//
// 'foo'  → 'foo'
// "it's" → 'it'\”s'   (POSIX standard escape)
//
// 결과는 shell이 그대로 해석 — 변수 확장·command substitution 모두 차단.
func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// 컴파일 타임 인터페이스 만족 보장 (errors.As 활용에 필요).
var _ error = (*ConditionTranslateError)(nil)
