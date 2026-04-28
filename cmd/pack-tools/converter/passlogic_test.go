package converter_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ssabro/rosshield/cmd/pack-tools/converter"
)

// === E12 T3 — pass_logic 트리 → bash 표현식 ===

// 단순 leaf evaluator: ID를 그대로 `<id>_TEST` 식의 placeholder로 변환 — 트리 구조만 검증.
func placeholderLeaf(id string) (string, error) {
	return id + "_TEST", nil
}

func TestPassLogicSingleConditionLeaf(t *testing.T) {
	t.Parallel()
	out, err := converter.PassLogicToBash([]byte(`"C1"`), placeholderLeaf)
	if err != nil {
		t.Fatalf("PassLogicToBash: %v", err)
	}
	if out != "C1_TEST" {
		t.Errorf("out = %q, want %q", out, "C1_TEST")
	}
}

func TestPassLogicAND(t *testing.T) {
	t.Parallel()
	out, err := converter.PassLogicToBash([]byte(`{"AND":["C1","C2","C3"]}`), placeholderLeaf)
	if err != nil {
		t.Fatalf("PassLogicToBash: %v", err)
	}
	want := "{ C1_TEST; } && { C2_TEST; } && { C3_TEST; }"
	if out != want {
		t.Errorf("out = %q\nwant %q", out, want)
	}
}

func TestPassLogicOR(t *testing.T) {
	t.Parallel()
	out, err := converter.PassLogicToBash([]byte(`{"OR":["C1","C2"]}`), placeholderLeaf)
	if err != nil {
		t.Fatalf("PassLogicToBash: %v", err)
	}
	want := "{ C1_TEST; } || { C2_TEST; }"
	if out != want {
		t.Errorf("out = %q\nwant %q", out, want)
	}
}

func TestPassLogicNOT(t *testing.T) {
	t.Parallel()
	out, err := converter.PassLogicToBash([]byte(`{"NOT":["C1"]}`), placeholderLeaf)
	if err != nil {
		t.Fatalf("PassLogicToBash: %v", err)
	}
	want := "! { C1_TEST; }"
	if out != want {
		t.Errorf("out = %q\nwant %q", out, want)
	}
}

func TestPassLogicNested(t *testing.T) {
	t.Parallel()
	// (C1 OR C2) AND C3
	out, err := converter.PassLogicToBash([]byte(`{"AND":[{"OR":["C1","C2"]},"C3"]}`), placeholderLeaf)
	if err != nil {
		t.Fatalf("PassLogicToBash: %v", err)
	}
	want := "{ { C1_TEST; } || { C2_TEST; }; } && { C3_TEST; }"
	if out != want {
		t.Errorf("out = %q\nwant %q", out, want)
	}
}

func TestPassLogicCaseInsensitiveOp(t *testing.T) {
	t.Parallel()
	// 실제 데이터는 항상 대문자였으나, 소문자도 받아들이는 게 robust.
	out, err := converter.PassLogicToBash([]byte(`{"and":["C1","C2"]}`), placeholderLeaf)
	if err != nil {
		t.Fatalf("PassLogicToBash: %v", err)
	}
	if !strings.Contains(out, "&&") {
		t.Errorf("expected && operator, got %q", out)
	}
}

func TestPassLogicLeafErrorBubbles(t *testing.T) {
	t.Parallel()
	failing := func(id string) (string, error) {
		return "", errors.New("leaf error: " + id)
	}
	_, err := converter.PassLogicToBash([]byte(`{"AND":["C1","C2"]}`), failing)
	if err == nil {
		t.Fatal("expected error from leaf")
	}
	if !strings.Contains(err.Error(), "leaf error: C1") {
		t.Errorf("err = %v, expected to contain 'leaf error: C1'", err)
	}
}

func TestPassLogicRejectsUnknownOp(t *testing.T) {
	t.Parallel()
	_, err := converter.PassLogicToBash([]byte(`{"XOR":["C1","C2"]}`), placeholderLeaf)
	if !errors.Is(err, converter.ErrUnknownPassLogicOp) {
		t.Errorf("err = %v, want ErrUnknownPassLogicOp", err)
	}
}

func TestPassLogicRejectsEmptyArgs(t *testing.T) {
	t.Parallel()
	_, err := converter.PassLogicToBash([]byte(`{"AND":[]}`), placeholderLeaf)
	if !errors.Is(err, converter.ErrEmptyPassLogicArgs) {
		t.Errorf("err = %v, want ErrEmptyPassLogicArgs", err)
	}
}

func TestPassLogicRejectsBadNotArgs(t *testing.T) {
	t.Parallel()
	cases := [][]byte{
		[]byte(`{"NOT":["C1","C2"]}`), // multi
		[]byte(`{"NOT":[]}`),          // empty
	}
	for _, c := range cases {
		_, err := converter.PassLogicToBash(c, placeholderLeaf)
		if !errors.Is(err, converter.ErrInvalidNotArgs) {
			t.Errorf("input %s — err = %v, want ErrInvalidNotArgs", c, err)
		}
	}
}

func TestPassLogicRejectsMultipleOpsInMap(t *testing.T) {
	t.Parallel()
	_, err := converter.PassLogicToBash([]byte(`{"AND":["C1"],"OR":["C2"]}`), placeholderLeaf)
	if !errors.Is(err, converter.ErrInvalidPassLogicShape) {
		t.Errorf("err = %v, want ErrInvalidPassLogicShape", err)
	}
}

func TestPassLogicRejectsEmptyInput(t *testing.T) {
	t.Parallel()
	_, err := converter.PassLogicToBash([]byte{}, placeholderLeaf)
	if !errors.Is(err, converter.ErrInvalidPassLogicShape) {
		t.Errorf("err = %v, want ErrInvalidPassLogicShape", err)
	}
}

func TestPassLogicHandlesUnmarshalledJSONNumber(t *testing.T) {
	t.Parallel()
	// Defensive: 잘못된 입력(숫자)을 무시하지 않고 명확히 거부.
	_, err := converter.PassLogicToBash(json.RawMessage(`123`), placeholderLeaf)
	if !errors.Is(err, converter.ErrInvalidPassLogicShape) {
		t.Errorf("err = %v, want ErrInvalidPassLogicShape", err)
	}
}
