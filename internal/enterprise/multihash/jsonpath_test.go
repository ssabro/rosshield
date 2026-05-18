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
