//go:build rosshield_enterprise

// jsonpath_test.go — 경량 JSONPath extractor 단위 테스트.

package multihash

import (
	"errors"
	"strings"
	"testing"
)

func TestParseJSONPath_root_단독(t *testing.T) {
	steps, err := parseJSONPath("$")
	if err != nil {
		t.Fatalf("$ parse err: %v", err)
	}
	if len(steps) != 0 {
		t.Errorf("root path expected 0 steps, got %d", len(steps))
	}
}

func TestParseJSONPath_simple_field(t *testing.T) {
	steps, err := parseJSONPath("$.foo")
	if err != nil {
		t.Fatalf("parse err: %v", err)
	}
	if len(steps) != 1 || steps[0].key != "foo" || steps[0].index != -1 {
		t.Errorf("got %+v, want [{foo,-1}]", steps)
	}
}

func TestParseJSONPath_nested_field(t *testing.T) {
	steps, err := parseJSONPath("$.foo.bar.baz")
	if err != nil {
		t.Fatalf("parse err: %v", err)
	}
	if len(steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(steps))
	}
	wantKeys := []string{"foo", "bar", "baz"}
	for i, k := range wantKeys {
		if steps[i].key != k {
			t.Errorf("step %d key = %q, want %q", i, steps[i].key, k)
		}
	}
}

func TestParseJSONPath_array_index(t *testing.T) {
	steps, err := parseJSONPath("$.items[0]")
	if err != nil {
		t.Fatalf("parse err: %v", err)
	}
	if len(steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(steps))
	}
	if steps[0].key != "items" {
		t.Errorf("step 0 key = %q, want items", steps[0].key)
	}
	if steps[1].index != 0 || steps[1].key != "" {
		t.Errorf("step 1 = %+v, want {index:0}", steps[1])
	}
}

func TestParseJSONPath_mixed_index_field(t *testing.T) {
	steps, err := parseJSONPath("$.foo[2].bar")
	if err != nil {
		t.Fatalf("parse err: %v", err)
	}
	if len(steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(steps))
	}
	if steps[1].index != 2 {
		t.Errorf("step 1 index = %d, want 2", steps[1].index)
	}
	if steps[2].key != "bar" {
		t.Errorf("step 2 key = %q, want bar", steps[2].key)
	}
}

func TestParseJSONPath_invalid(t *testing.T) {
	cases := map[string]string{
		"empty":          "",
		"no_dollar":      "foo.bar",
		"trailing_dot":   "$.",
		"double_dot":     "$..foo",
		"unterm_bracket": "$.foo[0",
		"empty_index":    "$.foo[]",
		"negative_idx":   "$.foo[-1]",
		"non_int_idx":    "$.foo[abc]",
	}
	for name, path := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := parseJSONPath(path)
			if err == nil {
				t.Fatalf("expected error for %q, got nil", path)
			}
			if !errors.Is(err, ErrInvalidJSONPath) {
				t.Errorf("err = %v, want wraps ErrInvalidJSONPath", err)
			}
		})
	}
}

func TestExtractByPath_root(t *testing.T) {
	ev := []byte(`{"a":1,"b":2}`)
	got, err := extractByPath(ev, "$")
	if err != nil {
		t.Fatalf("extract err: %v", err)
	}
	// canonical: keys 사전식 정렬 + UseNumber로 1, 2가 json.Number → "1","2"로 직렬화.
	want := `{"a":1,"b":2}`
	if string(got) != want {
		t.Errorf("got %q, want %q", string(got), want)
	}
}

func TestExtractByPath_simple_field(t *testing.T) {
	ev := []byte(`{"foo":"hello","bar":42}`)
	got, err := extractByPath(ev, "$.foo")
	if err != nil {
		t.Fatalf("extract err: %v", err)
	}
	if string(got) != `"hello"` {
		t.Errorf("got %q, want %q", string(got), `"hello"`)
	}
}

func TestExtractByPath_nested(t *testing.T) {
	ev := []byte(`{"a":{"b":{"c":"deep"}}}`)
	got, err := extractByPath(ev, "$.a.b.c")
	if err != nil {
		t.Fatalf("extract err: %v", err)
	}
	if string(got) != `"deep"` {
		t.Errorf("got %q, want %q", string(got), `"deep"`)
	}
}

func TestExtractByPath_array_index(t *testing.T) {
	ev := []byte(`{"items":["x","y","z"]}`)
	got, err := extractByPath(ev, "$.items[1]")
	if err != nil {
		t.Fatalf("extract err: %v", err)
	}
	if string(got) != `"y"` {
		t.Errorf("got %q, want %q", string(got), `"y"`)
	}
}

func TestExtractByPath_mixed(t *testing.T) {
	ev := []byte(`{"list":[{"k":"v0"},{"k":"v1"}]}`)
	got, err := extractByPath(ev, "$.list[1].k")
	if err != nil {
		t.Fatalf("extract err: %v", err)
	}
	if string(got) != `"v1"` {
		t.Errorf("got %q, want %q", string(got), `"v1"`)
	}
}

func TestExtractByPath_key_absent(t *testing.T) {
	ev := []byte(`{"foo":1}`)
	_, err := extractByPath(ev, "$.bar")
	if err == nil {
		t.Fatal("expected ErrPathNotFound, got nil")
	}
	if !errors.Is(err, ErrPathNotFound) {
		t.Errorf("err = %v, want wraps ErrPathNotFound", err)
	}
}

func TestExtractByPath_array_oob(t *testing.T) {
	ev := []byte(`{"x":[1,2]}`)
	_, err := extractByPath(ev, "$.x[5]")
	if err == nil {
		t.Fatal("expected ErrPathNotFound, got nil")
	}
	if !errors.Is(err, ErrPathNotFound) {
		t.Errorf("err = %v, want wraps ErrPathNotFound", err)
	}
}

func TestExtractByPath_traverse_nonobject(t *testing.T) {
	ev := []byte(`{"x":"scalar"}`)
	_, err := extractByPath(ev, "$.x.y")
	if err == nil {
		t.Fatal("expected ErrPathNotFound, got nil")
	}
	if !errors.Is(err, ErrPathNotFound) {
		t.Errorf("err = %v, want wraps ErrPathNotFound", err)
	}
}

func TestExtractByPath_invalid_json_evidence(t *testing.T) {
	ev := []byte(`{not json`)
	_, err := extractByPath(ev, "$.foo")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrInvalidJSONPath) {
		t.Errorf("err = %v, want wraps ErrInvalidJSONPath", err)
	}
}

func TestExtractByPath_canonical_key_order(t *testing.T) {
	// 입력은 b,a 순이지만 추출 결과는 a,b 사전식.
	ev := []byte(`{"obj":{"b":2,"a":1}}`)
	got, err := extractByPath(ev, "$.obj")
	if err != nil {
		t.Fatalf("extract err: %v", err)
	}
	want := `{"a":1,"b":2}`
	if string(got) != want {
		t.Errorf("got %q, want %q (key 사전식 정렬)", string(got), want)
	}
}

func TestExtractByPath_determinism_under_input_reorder(t *testing.T) {
	// 같은 의미·다른 key 순서 입력 → 추출 결과 같아야 한다.
	ev1 := []byte(`{"o":{"k":"v","n":1}}`)
	ev2 := []byte(`{"o":{"n":1,"k":"v"}}`)
	out1, err := extractByPath(ev1, "$.o")
	if err != nil {
		t.Fatalf("extract err: %v", err)
	}
	out2, err := extractByPath(ev2, "$.o")
	if err != nil {
		t.Fatalf("extract err: %v", err)
	}
	if string(out1) != string(out2) {
		t.Errorf("결정론 깨짐: %q vs %q", out1, out2)
	}
}

func TestExtractByPath_error_wraps_path_for_context(t *testing.T) {
	ev := []byte(`{"x":1}`)
	_, err := extractByPath(ev, "$.absent")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "absent") {
		t.Errorf("err message should mention key, got %q", err.Error())
	}
}

// =============================================================================
// v2 — wildcard `[*]` JSONPath expansion 테스트
// =============================================================================

func TestParseJSONPath_wildcard_token(t *testing.T) {
	steps, err := parseJSONPath("$.foo[*]")
	if err != nil {
		t.Fatalf("parse err: %v", err)
	}
	if len(steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(steps))
	}
	if steps[0].key != "foo" {
		t.Errorf("step 0 key = %q, want foo", steps[0].key)
	}
	if !steps[1].wildcard {
		t.Errorf("step 1 wildcard = false, want true")
	}
	if steps[1].index != -1 || steps[1].key != "" {
		t.Errorf("wildcard step should have index=-1 key=\"\", got %+v", steps[1])
	}
}

func TestParseJSONPath_wildcard_then_field(t *testing.T) {
	steps, err := parseJSONPath("$.foo[*].bar")
	if err != nil {
		t.Fatalf("parse err: %v", err)
	}
	if len(steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(steps))
	}
	if !steps[1].wildcard {
		t.Errorf("step 1 should be wildcard, got %+v", steps[1])
	}
	if steps[2].key != "bar" {
		t.Errorf("step 2 key = %q, want bar", steps[2].key)
	}
}

func TestParseJSONPath_nested_wildcard_token(t *testing.T) {
	steps, err := parseJSONPath("$.a[*].b[*].c")
	if err != nil {
		t.Fatalf("parse err: %v", err)
	}
	if len(steps) != 5 {
		t.Fatalf("expected 5 steps, got %d", len(steps))
	}
	if !steps[1].wildcard || !steps[3].wildcard {
		t.Errorf("steps 1 and 3 should be wildcard, got %+v", steps)
	}
}

func TestExpandJSONPath_no_wildcard_passthrough(t *testing.T) {
	ev := []byte(`{"foo":{"bar":1}}`)
	got, err := expandJSONPath(ev, "$.foo.bar")
	if err != nil {
		t.Fatalf("expand err: %v", err)
	}
	if len(got) != 1 || got[0] != "$.foo.bar" {
		t.Errorf("got %v, want [$.foo.bar]", got)
	}
}

func TestExpandJSONPath_single_wildcard_array_of_three(t *testing.T) {
	ev := []byte(`{"checks":[{"status":"ok"},{"status":"fail"},{"status":"warn"}]}`)
	got, err := expandJSONPath(ev, "$.checks[*].status")
	if err != nil {
		t.Fatalf("expand err: %v", err)
	}
	want := []string{"$.checks[0].status", "$.checks[1].status", "$.checks[2].status"}
	if len(got) != len(want) {
		t.Fatalf("got %d expansions, want %d (%v)", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("expansion[%d] = %q, want %q", i, got[i], w)
		}
	}
}

func TestExpandJSONPath_wildcard_terminal(t *testing.T) {
	// path가 wildcard로 끝나는 경우 — 각 element 자체가 sub-hash 단위.
	ev := []byte(`{"items":["x","y","z"]}`)
	got, err := expandJSONPath(ev, "$.items[*]")
	if err != nil {
		t.Fatalf("expand err: %v", err)
	}
	want := []string{"$.items[0]", "$.items[1]", "$.items[2]"}
	if len(got) != len(want) {
		t.Fatalf("got %d, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("got[%d] = %q, want %q", i, got[i], w)
		}
	}
}

func TestExpandJSONPath_empty_array_yields_zero(t *testing.T) {
	ev := []byte(`{"checks":[]}`)
	got, err := expandJSONPath(ev, "$.checks[*].status")
	if err != nil {
		t.Fatalf("expand err: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("empty array should yield 0 expansions, got %v", got)
	}
}

func TestExpandJSONPath_non_array_wildcard_returns_invalid(t *testing.T) {
	// `$.foo[*]`인데 foo가 object → ErrInvalidJSONPath.
	ev := []byte(`{"foo":{"bar":1}}`)
	_, err := expandJSONPath(ev, "$.foo[*]")
	if err == nil {
		t.Fatal("expected ErrInvalidJSONPath for non-array wildcard, got nil")
	}
	if !errors.Is(err, ErrInvalidJSONPath) {
		t.Errorf("err = %v, want wraps ErrInvalidJSONPath", err)
	}
}

func TestExpandJSONPath_missing_key_before_wildcard(t *testing.T) {
	// $.absent[*] — absent key 부재 → ErrPathNotFound (object key 단계 실패).
	ev := []byte(`{"foo":[1,2]}`)
	_, err := expandJSONPath(ev, "$.absent[*]")
	if err == nil {
		t.Fatal("expected ErrPathNotFound, got nil")
	}
	if !errors.Is(err, ErrPathNotFound) {
		t.Errorf("err = %v, want wraps ErrPathNotFound", err)
	}
}

func TestExpandJSONPath_nested_wildcard_cartesian(t *testing.T) {
	// a[2] × b[3] = 6 concrete paths.
	ev := []byte(`{
		"a":[
			{"b":[{"v":0},{"v":1},{"v":2}]},
			{"b":[{"v":10},{"v":11},{"v":12}]}
		]
	}`)
	got, err := expandJSONPath(ev, "$.a[*].b[*].v")
	if err != nil {
		t.Fatalf("expand err: %v", err)
	}
	want := []string{
		"$.a[0].b[0].v",
		"$.a[0].b[1].v",
		"$.a[0].b[2].v",
		"$.a[1].b[0].v",
		"$.a[1].b[1].v",
		"$.a[1].b[2].v",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d expansions, want %d (got=%v)", len(got), len(want), got)
	}
	// 결정론적 정렬 보장 — 사전식 ordering.
	for i, w := range want {
		if got[i] != w {
			t.Errorf("expansion[%d] = %q, want %q", i, got[i], w)
		}
	}
}

func TestExpandJSONPath_determinism_across_calls(t *testing.T) {
	ev := []byte(`{"x":[{"y":[1,2]},{"y":[3,4,5]}]}`)
	out1, err := expandJSONPath(ev, "$.x[*].y[*]")
	if err != nil {
		t.Fatalf("expand1: %v", err)
	}
	out2, err := expandJSONPath(ev, "$.x[*].y[*]")
	if err != nil {
		t.Fatalf("expand2: %v", err)
	}
	if len(out1) != len(out2) {
		t.Fatalf("len differs: %d vs %d", len(out1), len(out2))
	}
	for i := range out1 {
		if out1[i] != out2[i] {
			t.Errorf("expansion[%d] differs across calls: %q vs %q", i, out1[i], out2[i])
		}
	}
}

func TestExpandJSONPath_large_array_100(t *testing.T) {
	// 100-element array — count + 일부 sample 검증.
	var b strings.Builder
	b.WriteString(`{"checks":[`)
	for i := 0; i < 100; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`{"status":"ok"}`)
	}
	b.WriteString(`]}`)
	got, err := expandJSONPath([]byte(b.String()), "$.checks[*].status")
	if err != nil {
		t.Fatalf("expand err: %v", err)
	}
	if len(got) != 100 {
		t.Fatalf("expected 100 expansions, got %d", len(got))
	}
	// 사전식 정렬 후: $.checks[0], $.checks[1], ..., $.checks[10], $.checks[11], ..., $.checks[99]
	// (zero-pad 안 함 → string sort에서 "10"이 "2"보다 먼저). 본 함수는 sort.Strings 사용하므로
	// 사전식 정렬 결과를 검증.
	if got[0] != "$.checks[0].status" {
		t.Errorf("first = %q, want $.checks[0].status", got[0])
	}
	// 사전식: $.checks[0]...$.checks[9]가 $.checks[10] 앞에 안 옴 — "$.checks[1" prefix가 "$.checks[2".
	// 확실한 invariant만 검증: 모든 expansion이 unique이고 "$.checks[" prefix를 가짐.
	seen := make(map[string]bool, 100)
	for _, p := range got {
		if !strings.HasPrefix(p, "$.checks[") {
			t.Errorf("unexpected path %q", p)
		}
		if seen[p] {
			t.Errorf("duplicate path %q", p)
		}
		seen[p] = true
	}
}

func TestExpandJSONPath_invalid_evidence(t *testing.T) {
	_, err := expandJSONPath([]byte(`{not json`), "$.x[*]")
	if err == nil {
		t.Fatal("expected error for invalid evidence, got nil")
	}
	if !errors.Is(err, ErrInvalidJSONPath) {
		t.Errorf("err = %v, want wraps ErrInvalidJSONPath", err)
	}
}
