package main

// scan.go — `rosshield scan run` 명령 (E9 Stage C).
//
// POST /api/v1/scans → 새 ScanSession을 pending 상태로 생성. tenant scope.
//
// `scan status <id>` 같은 GET endpoint는 현재 서버 미구현 — Stage D 또는 후속.

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
)

type startScanRequestBody struct {
	FleetID string `json:"fleetId"`
	PackID  string `json:"packId"`
	Trigger string `json:"trigger,omitempty"`
	Total   int    `json:"total,omitempty"`
}

type scanSessionResponseBody struct {
	SessionID string `json:"sessionId"`
	TenantID  string `json:"tenantId"`
	FleetID   string `json:"fleetId"`
	PackID    string `json:"packId"`
	Trigger   string `json:"trigger"`
	Status    string `json:"status"`
	Total     int    `json:"total"`
	Completed int    `json:"completed"`
	Failed    int    `json:"failed"`
}

func runScan(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "rosshield scan: missing subcommand (run)")
		return 2
	}
	switch args[0] {
	case "run":
		return runScanRun(args[1:])
	case "help", "--help", "-h":
		fmt.Fprintln(os.Stderr, `rosshield scan — scan 세션 명령

사용법:
  rosshield scan run --fleet ID --pack ID [--trigger manual|schedule|event] [--total N] [-o table|json]`)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "rosshield scan: unknown sub-command %q\n", args[0])
		return 2
	}
}

func runScanRun(args []string) int {
	fs := flag.NewFlagSet("scan run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var (
		fleetID    = fs.String("fleet", "", "fleet ID (필수)")
		packID     = fs.String("pack", "", "pack ID (필수)")
		trigger    = fs.String("trigger", "manual", "trigger: manual | schedule | event")
		total      = fs.Int("total", 0, "예상 총 작업 수 (옵션)")
		configPath = fs.String("config", "", "config 파일 경로")
		output     = fs.String("o", "table", "출력 포맷: table | json")
	)
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "rosshield scan run: %v\n", err)
		return 2
	}
	if *fleetID == "" || *packID == "" {
		fmt.Fprintln(os.Stderr, "rosshield scan run: --fleet and --pack are required")
		return 2
	}

	format, err := ParseOutputFormat(*output)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rosshield scan run: %v\n", err)
		return 2
	}

	cfgPath := *configPath
	if cfgPath == "" {
		cfgPath = DefaultConfigPath()
	}
	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rosshield scan run: load config: %v\n", err)
		return 1
	}
	if cfg.AccessToken == "" {
		fmt.Fprintln(os.Stderr, "rosshield scan run: no access token (run `rosshield login` first)")
		return 2
	}

	client := NewClient(cfg)
	var resp scanSessionResponseBody
	if err := client.Post(context.Background(), "/api/v1/scans",
		startScanRequestBody{FleetID: *fleetID, PackID: *packID, Trigger: *trigger, Total: *total},
		&resp); err != nil {
		fmt.Fprintf(os.Stderr, "rosshield scan run: %v\n", err)
		return HTTPErrorToExitCode(err)
	}

	switch format {
	case OutputJSON:
		_ = PrintJSON(resp)
	default:
		PrintTable(
			[]string{"KEY", "VALUE"},
			[][]string{
				{"sessionId", resp.SessionID},
				{"status", resp.Status},
				{"fleetId", resp.FleetID},
				{"packId", resp.PackID},
				{"trigger", resp.Trigger},
			},
		)
	}
	return 0
}
