package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestParseOutputFormatDefaultsToTable(t *testing.T) {
	got, err := ParseOutputFormat("")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if got != OutputTable {
		t.Fatalf("got=%q, want %q", got, OutputTable)
	}
}

func TestParseOutputFormatJSON(t *testing.T) {
	got, err := ParseOutputFormat("json")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if got != OutputJSON {
		t.Fatalf("got=%q, want %q", got, OutputJSON)
	}
}

func TestParseOutputFormatRejectsUnknown(t *testing.T) {
	_, err := ParseOutputFormat("yaml")
	if err == nil {
		t.Fatalf("err=nil, want unknown format")
	}
	if !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("err=%v, want 'unknown' substring", err)
	}
}

func TestWriteTableHandlesEmptyRows(t *testing.T) {
	var buf bytes.Buffer
	writeTable(&buf, []string{"KEY", "VALUE"}, nil)
	out := buf.String()
	if !strings.Contains(out, "KEY") {
		t.Fatalf("missing header KEY: %q", out)
	}
	if !strings.Contains(out, "VALUE") {
		t.Fatalf("missing header VALUE: %q", out)
	}
}

func TestWriteTableAlignsColumns(t *testing.T) {
	var buf bytes.Buffer
	writeTable(&buf, []string{"KEY", "VALUE"}, [][]string{
		{"short", "1"},
		{"longerkey", "12345"},
	})
	out := buf.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("lines=%d (%q), want 3", len(lines), out)
	}
	// 두 row의 KEY 컬럼 너비가 같아야 한다 — tabwriter 정렬 확인.
	if len(lines[1]) < len("longerkey") {
		t.Fatalf("row1 too short: %q", lines[1])
	}
}

func TestWriteJSONEmitsIndented(t *testing.T) {
	var buf bytes.Buffer
	if err := writeJSON(&buf, map[string]any{"ok": true, "n": 42}); err != nil {
		t.Fatalf("writeJSON: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("unmarshal: %v\nraw=%s", err, buf.String())
	}
	if !strings.Contains(buf.String(), "\n") {
		t.Fatalf("output not multi-line: %q", buf.String())
	}
}
