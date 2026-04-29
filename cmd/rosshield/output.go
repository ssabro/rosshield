package main

// output.go — rosshield CLI 표 + JSON 출력 helper (E9 Stage A, R11-5).
//
// 모든 사용자 가시 출력은 본 모듈 경유 — table은 text/tabwriter, JSON은 indent 2.
// 디자인 의도: subcommand 핸들러가 row 슬라이스만 만들고 출력 형식은 -o 플래그로 결정.

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"text/tabwriter"
)

// OutputFormat은 -o 플래그 값입니다.
type OutputFormat string

const (
	OutputTable OutputFormat = "table"
	OutputJSON  OutputFormat = "json"
)

// ParseOutputFormat은 -o 플래그를 검증·정규화합니다.
//
// 빈 문자열은 OutputTable로 normalize — caller가 default를 별도로 set하지 않아도 안전.
func ParseOutputFormat(s string) (OutputFormat, error) {
	switch s {
	case "", "table":
		return OutputTable, nil
	case "json":
		return OutputJSON, nil
	default:
		return "", fmt.Errorf("unknown output format %q (allowed: table, json)", s)
	}
}

// PrintTable은 헤더 + 행들을 tabwriter로 stdout에 출력합니다.
//
// 빈 rows는 헤더만 출력(empty result 표 — 사용자가 "비었음"을 인지 가능).
func PrintTable(headers []string, rows [][]string) {
	writeTable(os.Stdout, headers, rows)
}

func writeTable(w io.Writer, headers []string, rows [][]string) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if len(headers) > 0 {
		_, _ = fmt.Fprintln(tw, joinTabs(headers))
	}
	for _, row := range rows {
		_, _ = fmt.Fprintln(tw, joinTabs(row))
	}
	_ = tw.Flush()
}

// joinTabs는 셀들을 tab으로 연결합니다 (tabwriter 입력 형식).
func joinTabs(cells []string) string {
	if len(cells) == 0 {
		return ""
	}
	out := cells[0]
	for _, c := range cells[1:] {
		out += "\t" + c
	}
	return out
}

// PrintJSON은 v를 indented JSON으로 stdout에 출력합니다.
func PrintJSON(v any) error {
	return writeJSON(os.Stdout, v)
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
