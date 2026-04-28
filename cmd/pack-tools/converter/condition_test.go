package converter_test

import (
	"errors"
	"testing"

	"github.com/ssabro/rosshield/cmd/pack-tools/converter"
)

func TestConditionToBashTest_Operators(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		c    converter.ROS2Condition
		want string
	}{
		{"empty", converter.ROS2Condition{ID: "C1", Operator: "empty"}, `[ -z "$C1_OUT" ]`},
		{"not_empty", converter.ROS2Condition{ID: "C2", Operator: "not_empty"}, `[ -n "$C2_OUT" ]`},
		{"equals string", converter.ROS2Condition{ID: "C3", Operator: "equals", Expected: "yes"}, `[ "$C3_OUT" = 'yes' ]`},
		{"equals integer expected", converter.ROS2Condition{ID: "C4", Operator: "equals", Expected: 1}, `[ "$C4_OUT" = '1' ]`},
		{"not_equals", converter.ROS2Condition{ID: "C5", Operator: "not_equals", Expected: "n/a"}, `[ "$C5_OUT" != 'n/a' ]`},
		{"contains", converter.ROS2Condition{ID: "C6", Operator: "contains", Expected: "PermitRootLogin no"}, `printf '%s' "$C6_OUT" | grep -qF 'PermitRootLogin no'`},
		{"regex", converter.ROS2Condition{ID: "C7", Operator: "regex", Expected: "^Port\\s+22$"}, `printf '%s' "$C7_OUT" | grep -qE '^Port\s+22$'`},
		{"single quote escape in expected", converter.ROS2Condition{ID: "C8", Operator: "equals", Expected: "it's me"}, `[ "$C8_OUT" = 'it'\''s me' ]`},
		{"case insensitive operator", converter.ROS2Condition{ID: "C9", Operator: "EMPTY"}, `[ -z "$C9_OUT" ]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := converter.ConditionToBashTest(tc.c)
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if got != tc.want {
				t.Errorf("got = %q\nwant %q", got, tc.want)
			}
		})
	}
}

func TestConditionToBashTest_DegradedReasons(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		c      converter.ROS2Condition
		reason string
	}{
		{
			"extract_pattern unsupported",
			converter.ROS2Condition{ID: "C1", Operator: "equals", Expected: 1, ExtractPattern: "^(\\d+)$"},
			converter.ReasonExtractPattern,
		},
		{
			"numeric value_type unsupported",
			converter.ROS2Condition{ID: "C2", Operator: "equals", ValueType: "number", Expected: 1},
			converter.ReasonNumberValue,
		},
		{
			"numeric op gt",
			converter.ROS2Condition{ID: "C3", Operator: "gt", Expected: 5},
			converter.ReasonNumericOp,
		},
		{
			"numeric op lte",
			converter.ROS2Condition{ID: "C4", Operator: "lte", Expected: 10},
			converter.ReasonNumericOp,
		},
		{
			"unknown operator",
			converter.ROS2Condition{ID: "C5", Operator: "weird"},
			converter.ReasonUnknownOp + ": weird",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := converter.ConditionToBashTest(tc.c)
			var cte *converter.ConditionTranslateError
			if !errors.As(err, &cte) {
				t.Fatalf("err = %v, want ConditionTranslateError", err)
			}
			if cte.ConditionID != tc.c.ID {
				t.Errorf("ConditionID = %q, want %q", cte.ConditionID, tc.c.ID)
			}
			if cte.Reason != tc.reason {
				t.Errorf("Reason = %q, want %q", cte.Reason, tc.reason)
			}
		})
	}
}

func TestConditionSetupLine(t *testing.T) {
	t.Parallel()
	c := converter.ROS2Condition{
		ID:      "C1",
		Command: "lsmod | grep cramfs",
	}
	got := converter.ConditionSetupLine(c)
	want := `C1_OUT="$(lsmod | grep cramfs 2>&1)"`
	if got != want {
		t.Errorf("got = %q\nwant %q", got, want)
	}
}
