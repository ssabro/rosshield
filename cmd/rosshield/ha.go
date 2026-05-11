package main

// ha.go — `rosshield ha status` 서브커맨드 (E29 후속, Phase 5).
//
// /healthz를 fetch하여 ha 필드가 있으면 leader/follower role + epoch + leaderId +
// lastHeartbeatAt 출력. ha 필드가 없으면 "HA disabled (single instance)" 안내.
//
// promote/demote 명령은 PG advisory lock 강제 조작이 위험(split-brain 유발 가능)이라
// 미제공 — 운영자가 systemctl restart로 leader 강제 교체.

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"encoding/json"
)

// haHealthView는 /healthz 응답의 ha 필드 (있으면) 형식입니다.
//
// /healthz는 인증 없이 노출 (snap install + healthz 검증 패턴과 일관). access token
// 미사용 — 본 명령은 server URL만 필요.
type haHealthView struct {
	Enabled         bool   `json:"enabled"`
	Role            string `json:"role"`
	Epoch           int64  `json:"epoch"`
	LeaderID        string `json:"leaderId"`
	LastHeartbeatAt string `json:"lastHeartbeatAt"`
}

type healthzView struct {
	Status string        `json:"status"`
	HA     *haHealthView `json:"ha,omitempty"`
}

func runHA(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: rosshield ha status [-o table|json]")
		return 2
	}
	switch args[0] {
	case "status":
		return runHAStatus(args[1:])
	case "-h", "--help", "help":
		fmt.Fprintln(os.Stderr, `ha 서브커맨드 — High Availability (E25) leader/follower 상태 조회

사용법:
  rosshield ha status [-o table|json] [--server URL]

옵션:
  --server URL     /healthz 노출 server URL (기본 config의 server)
  -o table|json    출력 포맷 (기본 table)
  --config <path>  config 파일 경로

비제공 명령 (의도적):
  ha promote / ha demote     — PG advisory lock 강제 조작은 split-brain 유발 위험.
                              운영자가 systemctl restart로 leader 강제 교체.

후속:
  ha failover-history        — leader_epoch 테이블 조회 (별 endpoint 추가 필요)`)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "ha: unknown sub-command %q\n", args[0])
		return 2
	}
}

func runHAStatus(args []string) int {
	fs := flag.NewFlagSet("ha status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var (
		configPath = fs.String("config", "", "config 파일 경로")
		server     = fs.String("server", "", "/healthz 노출 server URL (기본 config의 server)")
		output     = fs.String("o", "table", "출력 포맷")
	)
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "rosshield ha status: %v\n", err)
		return 2
	}
	format, err := ParseOutputFormat(*output)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rosshield ha status: %v\n", err)
		return 2
	}

	baseURL, code := resolveHealthzServer(*configPath, *server)
	if code != 0 {
		return code
	}

	healthzURL := strings.TrimRight(baseURL, "/") + "/healthz"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthzURL, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rosshield ha status: build request: %v\n", err)
		return 1
	}
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rosshield ha status: transport: %v\n", err)
		return 1
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rosshield ha status: read body: %v\n", err)
		return 1
	}
	// /healthz는 503도 정상 응답 (shutting_down 상태 등) — body 파싱 시도.
	var view healthzView
	if jerr := json.Unmarshal(body, &view); jerr != nil {
		fmt.Fprintf(os.Stderr, "rosshield ha status: parse json: %v (status=%d, body=%s)\n", jerr, resp.StatusCode, body)
		return 1
	}

	switch format {
	case OutputJSON:
		_ = PrintJSON(view)
	default:
		if view.HA == nil {
			fmt.Println("HA disabled (single instance — server is running without --ha-enabled)")
			return 0
		}
		PrintTable([]string{"KEY", "VALUE"}, [][]string{
			{"role", view.HA.Role},
			{"epoch", strconv.FormatInt(view.HA.Epoch, 10)},
			{"leaderId", view.HA.LeaderID},
			{"lastHeartbeatAt", view.HA.LastHeartbeatAt},
		})
	}
	return 0
}

// resolveHealthzServer는 --server flag 또는 config의 server URL을 반환합니다.
//
// /healthz는 인증 불필요이므로 access token은 무시. config 파일이 부재이고
// --server도 없으면 에러.
func resolveHealthzServer(configPath, override string) (string, int) {
	if override != "" {
		// URL 검증.
		if _, err := url.Parse(override); err != nil {
			fmt.Fprintf(os.Stderr, "rosshield ha status: invalid --server URL: %v\n", err)
			return "", 2
		}
		return override, 0
	}
	cfg, err := LoadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rosshield ha status: %v\n", err)
		fmt.Fprintln(os.Stderr, "  hint: --server URL 명시 또는 'rosshield config init' 실행")
		return "", 2
	}
	if cfg.ServerURL == "" {
		fmt.Fprintln(os.Stderr, "rosshield ha status: server URL not configured (--server 또는 config에 등록)")
		return "", 2
	}
	return cfg.ServerURL, 0
}
