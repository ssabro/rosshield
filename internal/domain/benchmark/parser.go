package benchmark

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
)

// 파서 결정 (C5 + 리서치):
//   - 2-phase 디코드: 먼저 op만 읽고, op별 strict struct에 DisallowUnknownFields로 재파싱
//   - 화이트리스트 op만 인정 — 모든 default(unknown op)는 ErrUnknownOp
//   - regex pattern 길이 제한 256B (정규식 폭탄 방지)

const maxRegexPatternLen = 256

// ParseEvalRule는 evaluation rule JSON을 검증된 AST 트리로 파싱합니다.
//
// 입력은 ParseCheckYAML이 저장한 EvaluationRule json.RawMessage.
// 화이트리스트 op만 통과 — 알 수 없는 op는 ErrUnknownOp.
func ParseEvalRule(raw json.RawMessage) (EvalNode, error) {
	if len(raw) == 0 || string(bytes.TrimSpace(raw)) == "null" {
		return nil, ErrEmptyEvalRule
	}
	return parseNode(raw)
}

func parseNode(raw json.RawMessage) (EvalNode, error) {
	// 1단계: op만 읽기.
	var disc struct {
		Op string `json:"op"`
	}
	if err := json.Unmarshal(raw, &disc); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidEvalRule, err)
	}
	if disc.Op == "" {
		return nil, fmt.Errorf("%w: op field required", ErrInvalidEvalRule)
	}

	// 2단계: op별 strict struct로 재파싱.
	switch disc.Op {
	case "equals":
		return parseStringNode(raw, "expected", func(v string) EvalNode { return EqualsNode{Expected: v} })
	case "not_equals":
		return parseStringNode(raw, "expected", func(v string) EvalNode { return NotEqualsNode{Expected: v} })
	case "contains":
		return parseStringNode(raw, "value", func(v string) EvalNode { return ContainsNode{Value: v} })
	case "regex":
		return parseRegexNode(raw)
	case "empty":
		return parseNoArgNode(raw, EmptyNode{})
	case "not_empty":
		return parseNoArgNode(raw, NotEmptyNode{})
	case "gt":
		return parseNumberNode(raw, func(v float64) EvalNode { return GTNode{Value: v} })
	case "gte":
		return parseNumberNode(raw, func(v float64) EvalNode { return GTENode{Value: v} })
	case "lt":
		return parseNumberNode(raw, func(v float64) EvalNode { return LTNode{Value: v} })
	case "lte":
		return parseNumberNode(raw, func(v float64) EvalNode { return LTENode{Value: v} })
	case "and":
		return parseLogicNode(raw, func(args []EvalNode) EvalNode { return AndNode{Args: args} })
	case "or":
		return parseLogicNode(raw, func(args []EvalNode) EvalNode { return OrNode{Args: args} })
	case "not":
		return parseNotNode(raw)
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnknownOp, disc.Op)
	}
}

func parseStringNode(raw json.RawMessage, field string, factory func(string) EvalNode) (EvalNode, error) {
	if field == "expected" {
		var s struct {
			Op       string `json:"op"`
			Expected string `json:"expected"`
		}
		if err := decodeJSONStrict(raw, &s); err != nil {
			return nil, err
		}
		return factory(s.Expected), nil
	}
	// "value" field
	var s struct {
		Op    string `json:"op"`
		Value string `json:"value"`
	}
	if err := decodeJSONStrict(raw, &s); err != nil {
		return nil, err
	}
	return factory(s.Value), nil
}

func parseNumberNode(raw json.RawMessage, factory func(float64) EvalNode) (EvalNode, error) {
	var s struct {
		Op    string      `json:"op"`
		Value json.Number `json:"value"`
	}
	if err := decodeJSONStrict(raw, &s); err != nil {
		return nil, err
	}
	if s.Value == "" {
		return nil, fmt.Errorf("%w: value required", ErrInvalidEvalRule)
	}
	v, err := s.Value.Float64()
	if err != nil {
		return nil, fmt.Errorf("%w: value not a number: %v", ErrInvalidEvalRule, err)
	}
	return factory(v), nil
}

func parseNoArgNode(raw json.RawMessage, node EvalNode) (EvalNode, error) {
	var s struct {
		Op string `json:"op"`
	}
	if err := decodeJSONStrict(raw, &s); err != nil {
		return nil, err
	}
	return node, nil
}

func parseRegexNode(raw json.RawMessage) (EvalNode, error) {
	var s struct {
		Op      string `json:"op"`
		Pattern string `json:"pattern"`
	}
	if err := decodeJSONStrict(raw, &s); err != nil {
		return nil, err
	}
	if s.Pattern == "" {
		return nil, fmt.Errorf("%w: regex pattern required", ErrInvalidEvalRule)
	}
	if len(s.Pattern) > maxRegexPatternLen {
		return nil, fmt.Errorf("%w: regex pattern length %d exceeds %d",
			ErrInvalidEvalRule, len(s.Pattern), maxRegexPatternLen)
	}
	re, err := regexp.Compile(s.Pattern)
	if err != nil {
		return nil, fmt.Errorf("%w: regex compile: %v", ErrInvalidEvalRule, err)
	}
	return RegexNode{Re: re, Pattern: s.Pattern}, nil
}

func parseLogicNode(raw json.RawMessage, factory func([]EvalNode) EvalNode) (EvalNode, error) {
	var s struct {
		Op   string            `json:"op"`
		Args []json.RawMessage `json:"args"`
	}
	if err := decodeJSONStrict(raw, &s); err != nil {
		return nil, err
	}
	if len(s.Args) == 0 {
		return nil, fmt.Errorf("%w: args required (at least 1)", ErrInvalidEvalRule)
	}
	nodes := make([]EvalNode, 0, len(s.Args))
	for i, ar := range s.Args {
		child, err := parseNode(ar)
		if err != nil {
			return nil, fmt.Errorf("args[%d]: %w", i, err)
		}
		nodes = append(nodes, child)
	}
	return factory(nodes), nil
}

func parseNotNode(raw json.RawMessage) (EvalNode, error) {
	var s struct {
		Op  string          `json:"op"`
		Arg json.RawMessage `json:"arg"`
	}
	if err := decodeJSONStrict(raw, &s); err != nil {
		return nil, err
	}
	if len(s.Arg) == 0 {
		return nil, fmt.Errorf("%w: arg required", ErrInvalidEvalRule)
	}
	child, err := parseNode(s.Arg)
	if err != nil {
		return nil, err
	}
	return NotNode{Arg: child}, nil
}

// decodeJSONStrict는 DisallowUnknownFields + UseNumber로 strict JSON 디코드.
// (yaml.go의 decodeStrict와 구분 — 같은 패키지 내 함수명 충돌 방지.)
func decodeJSONStrict(raw json.RawMessage, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	dec.UseNumber()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidEvalRule, err)
	}
	if dec.More() {
		return fmt.Errorf("%w: trailing data", ErrInvalidEvalRule)
	}
	return nil
}

// 평가 rule 관련 에러.
var (
	ErrEmptyEvalRule   = errors.New("benchmark: evaluation rule is empty")
	ErrInvalidEvalRule = errors.New("benchmark: invalid evaluation rule")
	ErrUnknownOp       = errors.New("benchmark: unknown op (not in whitelist)")
)
