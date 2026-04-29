package main

// robot.go — `rosshield robot list` 명령 (E9 Stage C).
//
// GET /api/v1/robots[?fleetId=...] → tenant scope robot 목록. AccessToken 부재 시 exit 2.
//
// `robot add`는 credential wrap·KEK 결선 등이 서버 endpoint에 필요해 Phase 1에서 보류
// (Stage D 또는 E10 Web UI에서 후속).

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type robotListResponseBody struct {
	Robots []robotItem `json:"robots"`
}

type robotItem struct {
	ID          string   `json:"id"`
	TenantID    string   `json:"tenantId"`
	FleetID     string   `json:"fleetId"`
	Name        string   `json:"name"`
	Host        string   `json:"host"`
	Port        int      `json:"port"`
	AuthType    string   `json:"authType"`
	OSDistro    string   `json:"osDistro,omitempty"`
	ROSDistro   string   `json:"rosDistro,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Role        string   `json:"role,omitempty"`
	Criticality string   `json:"criticality"`
}

func runRobot(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "rosshield robot: missing subcommand (list)")
		return 2
	}
	switch args[0] {
	case "list":
		return runRobotList(args[1:])
	case "help", "--help", "-h":
		fmt.Fprintln(os.Stderr, `rosshield robot — robot 인벤토리 명령

사용법:
  rosshield robot list [--fleet ID] [-o table|json]`)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "rosshield robot: unknown sub-command %q\n", args[0])
		return 2
	}
}

func runRobotList(args []string) int {
	fs := flag.NewFlagSet("robot list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var (
		fleetID    = fs.String("fleet", "", "fleet ID 필터 (옵션)")
		configPath = fs.String("config", "", "config 파일 경로")
		output     = fs.String("o", "table", "출력 포맷: table | json")
	)
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "rosshield robot list: %v\n", err)
		return 2
	}

	format, err := ParseOutputFormat(*output)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rosshield robot list: %v\n", err)
		return 2
	}

	cfgPath := *configPath
	if cfgPath == "" {
		cfgPath = DefaultConfigPath()
	}
	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rosshield robot list: load config: %v\n", err)
		return 1
	}
	if cfg.AccessToken == "" {
		fmt.Fprintln(os.Stderr, "rosshield robot list: no access token (run `rosshield login` first)")
		return 2
	}

	q := url.Values{}
	if *fleetID != "" {
		q.Set("fleetId", *fleetID)
	}

	client := NewClient(cfg)
	var resp robotListResponseBody
	if err := client.Get(context.Background(), "/api/v1/robots", q, &resp); err != nil {
		fmt.Fprintf(os.Stderr, "rosshield robot list: %v\n", err)
		return HTTPErrorToExitCode(err)
	}

	switch format {
	case OutputJSON:
		_ = PrintJSON(resp)
	default:
		rows := make([][]string, 0, len(resp.Robots))
		for _, r := range resp.Robots {
			rows = append(rows, []string{
				r.ID, r.Name, r.Host + ":" + strconv.Itoa(r.Port),
				r.AuthType, r.Criticality, strings.Join(r.Tags, ","),
			})
		}
		PrintTable(
			[]string{"ID", "NAME", "ENDPOINT", "AUTH", "CRIT", "TAGS"},
			rows,
		)
		if len(resp.Robots) == 0 {
			fmt.Fprintln(os.Stderr, "(no robots)")
		}
	}
	return 0
}
