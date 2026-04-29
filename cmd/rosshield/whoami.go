package main

// whoami.go — `rosshield whoami` 명령 (E9 Stage C).
//
// GET /api/v1/auth/me → 현재 토큰의 user 메타 표시. 토큰 부재 시 stderr 안내 후 exit 2.

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
)

type meResponseBody struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
	TenantID    string `json:"tenantId"`
}

func runWhoami(args []string) int {
	fs := flag.NewFlagSet("whoami", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var (
		configPath = fs.String("config", "", "config 파일 경로")
		output     = fs.String("o", "table", "출력 포맷: table | json")
	)
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "rosshield whoami: %v\n", err)
		return 2
	}

	format, err := ParseOutputFormat(*output)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rosshield whoami: %v\n", err)
		return 2
	}

	cfgPath := *configPath
	if cfgPath == "" {
		cfgPath = DefaultConfigPath()
	}
	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rosshield whoami: load config: %v\n", err)
		return 1
	}
	if cfg.AccessToken == "" {
		fmt.Fprintln(os.Stderr, "rosshield whoami: no access token (run `rosshield login` first)")
		return 2
	}

	client := NewClient(cfg)
	var me meResponseBody
	if err := client.Get(context.Background(), "/api/v1/auth/me", nil, &me); err != nil {
		fmt.Fprintf(os.Stderr, "rosshield whoami: %v\n", err)
		return HTTPErrorToExitCode(err)
	}

	switch format {
	case OutputJSON:
		_ = PrintJSON(me)
	default:
		PrintTable(
			[]string{"KEY", "VALUE"},
			[][]string{
				{"id", me.ID},
				{"email", me.Email},
				{"displayName", me.DisplayName},
				{"tenantId", me.TenantID},
			},
		)
	}
	return 0
}
