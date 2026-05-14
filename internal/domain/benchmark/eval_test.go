package benchmark_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ssabro/rosshield/internal/domain/benchmark"
)

func mustParse(t *testing.T, jsonStr string) benchmark.EvalNode {
	t.Helper()
	node, err := benchmark.ParseEvalRule(json.RawMessage(jsonStr))
	if err != nil {
		t.Fatalf("ParseEvalRule(%q): %v", jsonStr, err)
	}
	return node
}

func mustEval(t *testing.T, node benchmark.EvalNode, in benchmark.EvalInput) benchmark.EvalResult {
	t.Helper()
	r, err := node.Eval(in)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	return r
}

// E4.T5 본체 — 화이트리스트 외 op 거부.
func TestEvaluationRuleExpressionSafeSubset(t *testing.T) {
	t.Parallel()

	// 화이트리스트 op는 모두 통과해야 함.
	allowed := map[string]string{
		"equals":     `{"op":"equals","expected":"ok"}`,
		"not_equals": `{"op":"not_equals","expected":"ok"}`,
		"contains":   `{"op":"contains","value":"foo"}`,
		"regex":      `{"op":"regex","pattern":"^v[0-9]+$"}`,
		"empty":      `{"op":"empty"}`,
		"not_empty":  `{"op":"not_empty"}`,
		"gt":         `{"op":"gt","value":10}`,
		"gte":        `{"op":"gte","value":10}`,
		"lt":         `{"op":"lt","value":10}`,
		"lte":        `{"op":"lte","value":10}`,
		"and":        `{"op":"and","args":[{"op":"empty"}]}`,
		"or":         `{"op":"or","args":[{"op":"empty"}]}`,
		"not":        `{"op":"not","arg":{"op":"empty"}}`,
		"manual":     `{"op":"manual","prompt":"site policy review","defaultVerdict":"review"}`,
	}
	for name, body := range allowed {
		t.Run("allow_"+name, func(t *testing.T) {
			if _, err := benchmark.ParseEvalRule(json.RawMessage(body)); err != nil {
				t.Errorf("%s should be allowed: %v", name, err)
			}
		})
	}

	// 거부되어야 하는 dynamic / 화이트리스트 외 op들.
	forbidden := []string{
		`{"op":"eval","code":"shell"}`,
		`{"op":"require","module":"fs"}`,
		`{"op":"exec","cmd":"rm -rf /"}`,
		`{"op":"import","path":"http://attacker"}`,
		`{"op":"http","url":"http://attacker"}`,
		`{"op":"shell","cmd":"id"}`,
		`{"op":"unknown_future_op"}`,
	}
	for _, body := range forbidden {
		t.Run("deny_"+body[6:16], func(t *testing.T) {
			_, err := benchmark.ParseEvalRule(json.RawMessage(body))
			if !errors.Is(err, benchmark.ErrUnknownOp) {
				t.Errorf("%s should be denied with ErrUnknownOp, got %v", body, err)
			}
		})
	}
}

// 평가 의미 검증.
func TestEvalSemantics(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		jsonStr string
		input   benchmark.EvalInput
		want    benchmark.EvalStatus
	}{
		{"equals pass", `{"op":"equals","expected":"ok"}`, benchmark.EvalInput{Stdout: "ok\n"}, benchmark.StatusPass},
		{"equals fail", `{"op":"equals","expected":"ok"}`, benchmark.EvalInput{Stdout: "ng"}, benchmark.StatusFail},
		{"contains pass", `{"op":"contains","value":"foo"}`, benchmark.EvalInput{Stdout: "barfoobar"}, benchmark.StatusPass},
		{"contains fail", `{"op":"contains","value":"foo"}`, benchmark.EvalInput{Stdout: "bar"}, benchmark.StatusFail},
		{"regex pass", `{"op":"regex","pattern":"^v[0-9]+$"}`, benchmark.EvalInput{Stdout: "v123"}, benchmark.StatusPass},
		{"regex fail", `{"op":"regex","pattern":"^v[0-9]+$"}`, benchmark.EvalInput{Stdout: "alpha"}, benchmark.StatusFail},
		{"empty pass", `{"op":"empty"}`, benchmark.EvalInput{Stdout: "  \n"}, benchmark.StatusPass},
		{"not_empty pass", `{"op":"not_empty"}`, benchmark.EvalInput{Stdout: "foo"}, benchmark.StatusPass},
		{"gt pass", `{"op":"gt","value":10}`, benchmark.EvalInput{Stdout: "42"}, benchmark.StatusPass},
		{"gt fail", `{"op":"gt","value":10}`, benchmark.EvalInput{Stdout: "5"}, benchmark.StatusFail},
		{"gt indet (NaN)", `{"op":"gt","value":10}`, benchmark.EvalInput{Stdout: "abc"}, benchmark.StatusIndeterminate},
		{"and all pass", `{"op":"and","args":[{"op":"contains","value":"a"},{"op":"contains","value":"b"}]}`, benchmark.EvalInput{Stdout: "ab"}, benchmark.StatusPass},
		{"and short circuit fail", `{"op":"and","args":[{"op":"contains","value":"x"},{"op":"contains","value":"y"}]}`, benchmark.EvalInput{Stdout: "abc"}, benchmark.StatusFail},
		{"or any pass", `{"op":"or","args":[{"op":"empty"},{"op":"contains","value":"a"}]}`, benchmark.EvalInput{Stdout: "abc"}, benchmark.StatusPass},
		{"or all fail", `{"op":"or","args":[{"op":"empty"},{"op":"contains","value":"x"}]}`, benchmark.EvalInput{Stdout: "abc"}, benchmark.StatusFail},
		{"not pass→fail", `{"op":"not","arg":{"op":"empty"}}`, benchmark.EvalInput{Stdout: "x"}, benchmark.StatusPass},
		{"not fail→pass", `{"op":"not","arg":{"op":"empty"}}`, benchmark.EvalInput{Stdout: ""}, benchmark.StatusFail},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			node := mustParse(t, tc.jsonStr)
			got := mustEval(t, node, tc.input)
			if got.Status != tc.want {
				t.Errorf("status = %s (reason=%q), want %s", got.Status, got.Reason, tc.want)
			}
		})
	}
}

// not(INDETERMINATE) = INDETERMINATE 보존 — 검사 자체 실패는 부정 의미 없음.
func TestNotPreservesIndeterminate(t *testing.T) {
	t.Parallel()

	node := mustParse(t, `{"op":"not","arg":{"op":"gt","value":10}}`)
	r := mustEval(t, node, benchmark.EvalInput{Stdout: "not-a-number"})
	if r.Status != benchmark.StatusIndeterminate {
		t.Errorf("status = %s, want INDETERMINATE", r.Status)
	}
}

// or에 INDETERMINATE 섞이고 PASS 없으면 INDETERMINATE.
func TestOrIndeterminatePropagation(t *testing.T) {
	t.Parallel()

	body := `{"op":"or","args":[{"op":"contains","value":"x"},{"op":"gt","value":10}]}`
	node := mustParse(t, body)
	r := mustEval(t, node, benchmark.EvalInput{Stdout: "abc"}) // contains fail, gt indet
	if r.Status != benchmark.StatusIndeterminate {
		t.Errorf("status = %s, want INDETERMINATE", r.Status)
	}
}

// strict JSON: unknown field 거부.
func TestParseEvalRuleRejectsUnknownFields(t *testing.T) {
	t.Parallel()

	_, err := benchmark.ParseEvalRule(json.RawMessage(`{"op":"equals","expected":"ok","extra":"bad"}`))
	if !errors.Is(err, benchmark.ErrInvalidEvalRule) {
		t.Errorf("err = %v, want ErrInvalidEvalRule", err)
	}
}

// regex pattern 길이 제한.
func TestParseEvalRuleRejectsLongRegex(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("a", 300)
	body := `{"op":"regex","pattern":"` + long + `"}`
	_, err := benchmark.ParseEvalRule(json.RawMessage(body))
	if !errors.Is(err, benchmark.ErrInvalidEvalRule) {
		t.Errorf("err = %v, want ErrInvalidEvalRule (length limit)", err)
	}
}

// regex compile 실패.
func TestParseEvalRuleRejectsInvalidRegex(t *testing.T) {
	t.Parallel()

	_, err := benchmark.ParseEvalRule(json.RawMessage(`{"op":"regex","pattern":"[unclosed"}`))
	if !errors.Is(err, benchmark.ErrInvalidEvalRule) {
		t.Errorf("err = %v, want ErrInvalidEvalRule", err)
	}
}

// 빈 rule.
func TestParseEvalRuleRejectsEmpty(t *testing.T) {
	t.Parallel()

	for _, body := range []string{"", "null", "  "} {
		_, err := benchmark.ParseEvalRule(json.RawMessage(body))
		if !errors.Is(err, benchmark.ErrEmptyEvalRule) && !errors.Is(err, benchmark.ErrInvalidEvalRule) {
			t.Errorf("body %q: err = %v, want ErrEmptyEvalRule or ErrInvalidEvalRule", body, err)
		}
	}
}

// 중첩 트리 — and(or(equals, contains), not(empty)).
func TestParseEvalRuleNested(t *testing.T) {
	t.Parallel()

	body := `{
		"op": "and",
		"args": [
			{"op": "or", "args": [
				{"op": "equals", "expected": "ok"},
				{"op": "contains", "value": "active"}
			]},
			{"op": "not", "arg": {"op": "empty"}}
		]
	}`
	node := mustParse(t, body)

	// stdout="active" — or pass + not(empty) pass → and pass.
	r := mustEval(t, node, benchmark.EvalInput{Stdout: "active"})
	if r.Status != benchmark.StatusPass {
		t.Errorf("status = %s, want PASS", r.Status)
	}

	// stdout="" — or fail (둘 다 fail) → and fail (단락).
	r = mustEval(t, node, benchmark.EvalInput{Stdout: ""})
	if r.Status != benchmark.StatusFail {
		t.Errorf("status = %s, want FAIL", r.Status)
	}
}
