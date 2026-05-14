// ManualNode 단위 — D-MAN-3 schema(prompt + defaultVerdict pass/fail/review).

package benchmark_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/ssabro/rosshield/internal/domain/benchmark"
)

func TestParseManualNode_DefaultVerdict(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		json string
		want benchmark.EvalStatus
	}{
		{"pass", `{"op":"manual","prompt":"p","defaultVerdict":"pass"}`, benchmark.StatusPass},
		{"fail", `{"op":"manual","prompt":"p","defaultVerdict":"fail"}`, benchmark.StatusFail},
		{"review", `{"op":"manual","prompt":"p","defaultVerdict":"review"}`, benchmark.StatusIndeterminate},
		{"missing → review default", `{"op":"manual","prompt":"p"}`, benchmark.StatusIndeterminate},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			node, err := benchmark.ParseEvalRule(json.RawMessage(tc.json))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			mn, ok := node.(benchmark.ManualNode)
			if !ok {
				t.Fatalf("got %T, want ManualNode", node)
			}
			if mn.DefaultVerdict != tc.want {
				t.Errorf("DefaultVerdict=%q, want %q", mn.DefaultVerdict, tc.want)
			}
			if mn.Prompt != "p" {
				t.Errorf("Prompt=%q, want %q", mn.Prompt, "p")
			}
		})
	}
}

func TestParseManualNode_Errors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		json string
	}{
		{"prompt missing", `{"op":"manual","defaultVerdict":"pass"}`},
		{"prompt empty", `{"op":"manual","prompt":"","defaultVerdict":"pass"}`},
		{"defaultVerdict invalid", `{"op":"manual","prompt":"p","defaultVerdict":"bogus"}`},
		{"unknown field", `{"op":"manual","prompt":"p","extra":1}`},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := benchmark.ParseEvalRule(json.RawMessage(tc.json))
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.Is(err, benchmark.ErrInvalidEvalRule) {
				t.Errorf("got %v, want ErrInvalidEvalRule", err)
			}
		})
	}
}

func TestManualNode_EvalIgnoresInput(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		verdict benchmark.EvalStatus
		want    benchmark.EvalStatus
	}{
		{"pass verdict", benchmark.StatusPass, benchmark.StatusPass},
		{"fail verdict", benchmark.StatusFail, benchmark.StatusFail},
		{"review verdict", benchmark.StatusIndeterminate, benchmark.StatusIndeterminate},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			node := benchmark.ManualNode{Prompt: "review", DefaultVerdict: tc.verdict}
			// audit 입력이 무엇이든 결과는 동일.
			for _, in := range []benchmark.EvalInput{
				{Stdout: ""},
				{Stdout: "noise", ExitCode: 1},
				{Stderr: "err"},
			} {
				r, err := node.Eval(in)
				if err != nil {
					t.Fatalf("eval: %v", err)
				}
				if r.Status != tc.want {
					t.Errorf("input=%+v Status=%q, want %q", in, r.Status, tc.want)
				}
			}
		})
	}
}
