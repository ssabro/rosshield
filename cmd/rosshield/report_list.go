package main

// report_list.go — `rosshield report list` 명령 (E9 Stage C).
//
// GET /api/v1/reports[?sessionId=...] → tenant scope report 목록.
// `report verify`(offline)는 별도 파일(report_verify.go) — Stage A 이미 결선.

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
)

type reportListResponseBody struct {
	Reports []reportItem `json:"reports"`
}

type reportItem struct {
	ID           string `json:"id"`
	TenantID     string `json:"tenantId"`
	SessionID    string `json:"sessionId"`
	Format       string `json:"format"`
	PDFSHA256    string `json:"pdfSha256"`
	PDFSizeBytes int64  `json:"pdfSizeBytes"`
	GeneratedAt  string `json:"generatedAt"`
	GeneratedBy  string `json:"generatedBy"`
	Signed       bool   `json:"signed"`
}

func runReportList(args []string) int {
	fs := flag.NewFlagSet("report list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var (
		sessionID  = fs.String("session", "", "session ID 필터 (옵션)")
		configPath = fs.String("config", "", "config 파일 경로")
		output     = fs.String("o", "table", "출력 포맷: table | json")
	)
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "rosshield report list: %v\n", err)
		return 2
	}
	format, err := ParseOutputFormat(*output)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rosshield report list: %v\n", err)
		return 2
	}
	cfgPath := *configPath
	if cfgPath == "" {
		cfgPath = DefaultConfigPath()
	}
	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rosshield report list: load config: %v\n", err)
		return 1
	}
	if cfg.AccessToken == "" {
		fmt.Fprintln(os.Stderr, "rosshield report list: no access token (run `rosshield login` first)")
		return 2
	}

	q := url.Values{}
	if *sessionID != "" {
		q.Set("sessionId", *sessionID)
	}

	client := NewClient(cfg)
	var resp reportListResponseBody
	if err := client.Get(context.Background(), "/api/v1/reports", q, &resp); err != nil {
		fmt.Fprintf(os.Stderr, "rosshield report list: %v\n", err)
		return HTTPErrorToExitCode(err)
	}

	switch format {
	case OutputJSON:
		_ = PrintJSON(resp)
	default:
		rows := make([][]string, 0, len(resp.Reports))
		for _, r := range resp.Reports {
			signed := "no"
			if r.Signed {
				signed = "yes"
			}
			rows = append(rows, []string{
				r.ID, r.SessionID, r.Format, signed, r.GeneratedAt, r.PDFSHA256[:min(16, len(r.PDFSHA256))],
			})
		}
		PrintTable(
			[]string{"ID", "SESSION", "FORMAT", "SIGNED", "GENERATED_AT", "SHA256(16)"},
			rows,
		)
		if len(resp.Reports) == 0 {
			fmt.Fprintln(os.Stderr, "(no reports)")
		}
	}
	return 0
}
