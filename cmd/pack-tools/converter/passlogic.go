package converter

// passlogic.go — ROS2 framework JSON의 pass_logic 트리를 bash 조건 표현식으로 변환합니다.
//
// pass_logic 형태 (관찰):
//
//	{"AND": ["C1", "C2", "C3"]}                       // 단순 AND (단일 조건들의 합)
//	{"OR":  ["C1", "C2"]}                              // 단순 OR
//	{"NOT": [{"AND": ["C1", "C2"]}]}                   // NOT은 단일 자식
//	{"AND": [{"OR": ["C1", "C2"]}, "C3"]}              // 중첩 AND/OR
//
// 각 leaf는 condition ID 문자열("C1") — leafEvaluator가 bash test 표현식으로 변환.
// AND·OR는 args.Length ≥ 1, NOT은 단일 자식. 그 외는 ErrUnknownPassLogicOp.

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// LeafEvaluator는 condition ID(예: "C1")를 받아 bash test 표현식을 반환합니다.
//
// 반환 표현식은 사전 실행된 condition 결과 변수($<id>_OUT)에 대한 단일 test 명령이어야 합니다.
// 예: `[ -z "$C1_OUT" ]` 또는 `printf '%s' "$C1_OUT" | grep -qF 'expected'`.
type LeafEvaluator func(conditionID string) (string, error)

// 에러.
var (
	ErrUnknownPassLogicOp    = errors.New("converter: unknown pass_logic operator")
	ErrInvalidPassLogicShape = errors.New("converter: invalid pass_logic node shape")
	ErrEmptyPassLogicArgs    = errors.New("converter: pass_logic AND/OR requires at least one arg")
	ErrInvalidNotArgs        = errors.New("converter: pass_logic NOT requires exactly one arg")
)

// PassLogicToBash는 pass_logic JSON 트리를 bash 조건 표현식으로 변환합니다.
//
// 결과는 if 본문에 그대로 들어갈 수 있는 bash test 표현식입니다 (괄호 없는 raw form).
// AND는 `&&`, OR는 `||`, NOT은 `! { ... ; }`로 직번역.
//
// 각 leaf 변환은 LeafEvaluator가 책임 — 이 함수는 트리 구조만 처리.
func PassLogicToBash(passLogic json.RawMessage, leaf LeafEvaluator) (string, error) {
	if len(passLogic) == 0 {
		return "", ErrInvalidPassLogicShape
	}
	var node any
	if err := json.Unmarshal(passLogic, &node); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidPassLogicShape, err)
	}
	return passLogicNodeToBash(node, leaf)
}

func passLogicNodeToBash(node any, leaf LeafEvaluator) (string, error) {
	switch n := node.(type) {
	case string:
		return leaf(n)
	case map[string]any:
		if len(n) != 1 {
			return "", fmt.Errorf("%w: map must have exactly one operator key, got %d", ErrInvalidPassLogicShape, len(n))
		}
		for op, args := range n {
			switch strings.ToUpper(op) {
			case "AND":
				return joinPassLogicChildren(args, leaf, " && ")
			case "OR":
				return joinPassLogicChildren(args, leaf, " || ")
			case "NOT":
				return notPassLogicChild(args, leaf)
			default:
				return "", fmt.Errorf("%w: %q", ErrUnknownPassLogicOp, op)
			}
		}
	}
	return "", fmt.Errorf("%w: invalid node type %T", ErrInvalidPassLogicShape, node)
}

func joinPassLogicChildren(args any, leaf LeafEvaluator, sep string) (string, error) {
	argList, ok := args.([]any)
	if !ok {
		return "", fmt.Errorf("%w: AND/OR args must be array, got %T", ErrInvalidPassLogicShape, args)
	}
	if len(argList) == 0 {
		return "", ErrEmptyPassLogicArgs
	}
	parts := make([]string, 0, len(argList))
	for i, a := range argList {
		s, err := passLogicNodeToBash(a, leaf)
		if err != nil {
			return "", fmt.Errorf("args[%d]: %w", i, err)
		}
		parts = append(parts, "{ "+s+"; }")
	}
	return strings.Join(parts, sep), nil
}

func notPassLogicChild(args any, leaf LeafEvaluator) (string, error) {
	argList, ok := args.([]any)
	if !ok {
		return "", fmt.Errorf("%w: NOT args must be array, got %T", ErrInvalidPassLogicShape, args)
	}
	if len(argList) != 1 {
		return "", ErrInvalidNotArgs
	}
	inner, err := passLogicNodeToBash(argList[0], leaf)
	if err != nil {
		return "", err
	}
	return "! { " + inner + "; }", nil
}
