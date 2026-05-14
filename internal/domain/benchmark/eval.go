package benchmark

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// 평가기 결정 (C5 + 리서치):
//   - Sealed interface: 모든 노드는 unexported `isNode()` marker — 외부 패키지가 새 노드 못 만듦
//   - 화이트리스트 9 operators + 3 logical(and/or/not) — nrobotcheck 호환
//   - 3-값 결과 Status (PASS/FAIL/INDETERMINATE) — 시스템 오류와 거짓 조건 분리 (감사 가능성)
//   - regex 안전성: 패턴 256B 한도 + RE2(Go regexp 표준) — catastrophic backtracking 없음

// EvalStatus는 평가 결과 3-값입니다.
type EvalStatus string

const (
	StatusPass          EvalStatus = "PASS"
	StatusFail          EvalStatus = "FAIL"
	StatusIndeterminate EvalStatus = "INDETERMINATE"
)

// EvalInput은 평가 입력입니다 (SSH 명령 실행 결과).
type EvalInput struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// EvalResult는 평가 결과입니다. Reason은 사람 읽기용.
type EvalResult struct {
	Status EvalStatus
	Reason string
}

// EvalNode는 sealed interface입니다 — `isNode()` unexported marker.
//
// 외부 패키지는 새 노드 타입을 정의할 수 없습니다. 새 op 추가는 이 패키지 안에서만.
type EvalNode interface {
	isNode()
	// Eval은 input에 대한 평가 결과를 반환합니다. 시스템 오류는 error 반환.
	Eval(in EvalInput) (EvalResult, error)
}

// 노드 타입들. 모두 isNode()를 unexported로 구현.

type EqualsNode struct{ Expected string }
type NotEqualsNode struct{ Expected string }
type ContainsNode struct{ Value string }
type RegexNode struct {
	Re      *regexp.Regexp
	Pattern string
}
type EmptyNode struct{}
type NotEmptyNode struct{}
type GTNode struct{ Value float64 }
type GTENode struct{ Value float64 }
type LTNode struct{ Value float64 }
type LTENode struct{ Value float64 }
type AndNode struct{ Args []EvalNode }
type OrNode struct{ Args []EvalNode }
type NotNode struct{ Arg EvalNode }

// ManualNode는 운영자 정성 검토 결과를 표현합니다.
//
// audit 명령 실행 결과(Stdout/Stderr/ExitCode)는 무시 — DefaultVerdict 그대로 반환.
// 운영자가 fixture 작성 시 prompt(CIS audit text의 review 절)를 보고 환경에 맞춰
// PASS/FAIL/REVIEW를 직접 판정. selftest fixture는 expectedOutcome으로 검증.
type ManualNode struct {
	Prompt         string
	DefaultVerdict EvalStatus // PASS/FAIL/INDETERMINATE 중 하나(REVIEW = INDETERMINATE).
}

func (EqualsNode) isNode()    {}
func (NotEqualsNode) isNode() {}
func (ContainsNode) isNode()  {}
func (RegexNode) isNode()     {}
func (EmptyNode) isNode()     {}
func (NotEmptyNode) isNode()  {}
func (GTNode) isNode()        {}
func (GTENode) isNode()       {}
func (LTNode) isNode()        {}
func (LTENode) isNode()       {}
func (AndNode) isNode()       {}
func (OrNode) isNode()        {}
func (NotNode) isNode()       {}
func (ManualNode) isNode()    {}

// ----- 평가 구현 -----

func (n EqualsNode) Eval(in EvalInput) (EvalResult, error) {
	if strings.TrimRight(in.Stdout, "\n") == n.Expected {
		return pass(), nil
	}
	return fail(fmt.Sprintf("equals: stdout!=%q", n.Expected)), nil
}

func (n NotEqualsNode) Eval(in EvalInput) (EvalResult, error) {
	if strings.TrimRight(in.Stdout, "\n") != n.Expected {
		return pass(), nil
	}
	return fail(fmt.Sprintf("not_equals: stdout==%q", n.Expected)), nil
}

func (n ContainsNode) Eval(in EvalInput) (EvalResult, error) {
	if strings.Contains(in.Stdout, n.Value) {
		return pass(), nil
	}
	return fail(fmt.Sprintf("contains: %q not found", n.Value)), nil
}

func (n RegexNode) Eval(in EvalInput) (EvalResult, error) {
	if n.Re == nil {
		return EvalResult{}, errors.New("regex: pattern not compiled")
	}
	if n.Re.MatchString(in.Stdout) {
		return pass(), nil
	}
	return fail(fmt.Sprintf("regex: %q no match", n.Pattern)), nil
}

func (EmptyNode) Eval(in EvalInput) (EvalResult, error) {
	if strings.TrimSpace(in.Stdout) == "" {
		return pass(), nil
	}
	return fail("empty: stdout has content"), nil
}

func (NotEmptyNode) Eval(in EvalInput) (EvalResult, error) {
	if strings.TrimSpace(in.Stdout) != "" {
		return pass(), nil
	}
	return fail("not_empty: stdout is empty"), nil
}

func (n GTNode) Eval(in EvalInput) (EvalResult, error) {
	got, ok := parseNumber(in.Stdout)
	if !ok {
		return indeterminate(fmt.Sprintf("gt: stdout %q is not a number", trim(in.Stdout, 32))), nil
	}
	if got > n.Value {
		return pass(), nil
	}
	return fail(fmt.Sprintf("gt: %g not > %g", got, n.Value)), nil
}

func (n GTENode) Eval(in EvalInput) (EvalResult, error) {
	got, ok := parseNumber(in.Stdout)
	if !ok {
		return indeterminate(fmt.Sprintf("gte: stdout %q is not a number", trim(in.Stdout, 32))), nil
	}
	if got >= n.Value {
		return pass(), nil
	}
	return fail(fmt.Sprintf("gte: %g not >= %g", got, n.Value)), nil
}

func (n LTNode) Eval(in EvalInput) (EvalResult, error) {
	got, ok := parseNumber(in.Stdout)
	if !ok {
		return indeterminate(fmt.Sprintf("lt: stdout %q is not a number", trim(in.Stdout, 32))), nil
	}
	if got < n.Value {
		return pass(), nil
	}
	return fail(fmt.Sprintf("lt: %g not < %g", got, n.Value)), nil
}

func (n LTENode) Eval(in EvalInput) (EvalResult, error) {
	got, ok := parseNumber(in.Stdout)
	if !ok {
		return indeterminate(fmt.Sprintf("lte: stdout %q is not a number", trim(in.Stdout, 32))), nil
	}
	if got <= n.Value {
		return pass(), nil
	}
	return fail(fmt.Sprintf("lte: %g not <= %g", got, n.Value)), nil
}

// AndNode: 단락 평가 — 첫 FAIL/INDETERMINATE에서 즉시 종료.
func (n AndNode) Eval(in EvalInput) (EvalResult, error) {
	if len(n.Args) == 0 {
		return EvalResult{}, errors.New("and: requires at least one arg")
	}
	for _, arg := range n.Args {
		r, err := arg.Eval(in)
		if err != nil {
			return EvalResult{}, err
		}
		if r.Status != StatusPass {
			return r, nil
		}
	}
	return pass(), nil
}

// OrNode: 첫 PASS에서 즉시 종료. 모두 FAIL이면 FAIL, INDETERMINATE 섞이면 INDETERMINATE 우선.
func (n OrNode) Eval(in EvalInput) (EvalResult, error) {
	if len(n.Args) == 0 {
		return EvalResult{}, errors.New("or: requires at least one arg")
	}
	indet := false
	var lastFail EvalResult
	for _, arg := range n.Args {
		r, err := arg.Eval(in)
		if err != nil {
			return EvalResult{}, err
		}
		switch r.Status {
		case StatusPass:
			return pass(), nil
		case StatusIndeterminate:
			indet = true
		case StatusFail:
			lastFail = r
		}
	}
	if indet {
		return indeterminate("or: no PASS, at least one INDETERMINATE"), nil
	}
	return lastFail, nil
}

// NotNode: PASS↔FAIL 전환, INDETERMINATE는 그대로 (검사 자체가 실패한 것을 부정해도 의미 없음).
func (n NotNode) Eval(in EvalInput) (EvalResult, error) {
	if n.Arg == nil {
		return EvalResult{}, errors.New("not: requires arg")
	}
	r, err := n.Arg.Eval(in)
	if err != nil {
		return EvalResult{}, err
	}
	switch r.Status {
	case StatusPass:
		return fail("not: inner was PASS"), nil
	case StatusFail:
		return pass(), nil
	default: // INDETERMINATE
		return r, nil
	}
}

// ManualNode: audit 입력 무시 + DefaultVerdict 그대로 반환.
//
// PASS → 운영자 fixture가 "PASS" 명시(자동 변환 비대상이지만 환경상 항상 만족).
// FAIL → 운영자 fixture가 "FAIL" 명시(환경상 항상 미흡).
// INDETERMINATE(REVIEW) → 운영자 정성 검토 필요(default).
func (n ManualNode) Eval(in EvalInput) (EvalResult, error) {
	switch n.DefaultVerdict {
	case StatusPass:
		return pass(), nil
	case StatusFail:
		return fail("manual: defaultVerdict=fail (" + trim(n.Prompt, 64) + ")"), nil
	case StatusIndeterminate:
		return indeterminate("manual: review required (" + trim(n.Prompt, 64) + ")"), nil
	default:
		return EvalResult{}, fmt.Errorf("manual: invalid defaultVerdict %q", n.DefaultVerdict)
	}
}

// ----- 헬퍼 -----

func pass() EvalResult              { return EvalResult{Status: StatusPass} }
func fail(reason string) EvalResult { return EvalResult{Status: StatusFail, Reason: reason} }
func indeterminate(reason string) EvalResult {
	return EvalResult{Status: StatusIndeterminate, Reason: reason}
}

func parseNumber(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	var v float64
	if _, err := fmt.Sscanf(s, "%g", &v); err != nil {
		return 0, false
	}
	return v, true
}

func trim(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
