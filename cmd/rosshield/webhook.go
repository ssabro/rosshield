package main

// webhook.go — `rosshield webhook list` 서브커맨드 (E29 Phase 4).
//
// 본 stage는 read-only `list`만. CRUD·`test` (one-off ping)은 backend endpoint 추가 필요 → 후속.
//
// backend endpoint:
//
//	GET /api/v1/webhooks → {endpoints: WebhookEndpoint[]}

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// webhookEndpointView 는 backend webhook handler 응답 schema와 일치합니다.
//
// 정확한 필드는 internal/api/handlers/webhook.go의 endpointView와 동일 — 본 CLI는 표시용.
type webhookEndpointView struct {
	ID          string   `json:"id"`
	URL         string   `json:"url"`
	SecretLast4 string   `json:"secretLast4"`
	Events      []string `json:"events"`
	Format      string   `json:"format"`
	Enabled     bool     `json:"enabled"`
	CreatedAt   string   `json:"createdAt"`
	UpdatedAt   string   `json:"updatedAt"`
}

// runWebhook 는 `webhook ...` 서브커맨드를 분기합니다.
func runWebhook(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: rosshield webhook list [-o table|json]")
		return 2
	}
	switch args[0] {
	case "list":
		return runWebhookList(args[1:])
	case "-h", "--help", "help":
		fmt.Fprintln(os.Stderr, `webhook 서브커맨드 — Webhook endpoint 조회 (read-only)

사용법:
  rosshield webhook list [-o table|json]

옵션:
  -o table|json    출력 포맷 (기본 table)
  --config <path>  config 파일 경로

후속 stage:
  rosshield webhook test <id>     one-off ping (backend POST /api/v1/webhooks/{id}/test 필요)
  rosshield webhook create/update/delete   (backend CRUD 활용)`)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "webhook: unknown sub-command %q\n", args[0])
		return 2
	}
}

func runWebhookList(args []string) int {
	fs := flag.NewFlagSet("webhook list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var (
		configPath = fs.String("config", "", "config 파일 경로")
		output     = fs.String("o", "table", "출력 포맷")
	)
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "rosshield webhook list: %v\n", err)
		return 2
	}
	format, err := ParseOutputFormat(*output)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rosshield webhook list: %v\n", err)
		return 2
	}

	client, code := newAuthenticatedClient(*configPath, "rosshield webhook list")
	if client == nil {
		return code
	}

	var resp struct {
		Endpoints []webhookEndpointView `json:"endpoints"`
	}
	if err := client.Get(context.Background(), "/api/v1/webhooks", nil, &resp); err != nil {
		fmt.Fprintf(os.Stderr, "rosshield webhook list: %v\n", err)
		return HTTPErrorToExitCode(err)
	}

	switch format {
	case OutputJSON:
		_ = PrintJSON(resp.Endpoints)
	default:
		rows := make([][]string, 0, len(resp.Endpoints))
		for _, e := range resp.Endpoints {
			rows = append(rows, []string{
				e.ID, e.URL, e.Format,
				strconv.FormatBool(e.Enabled),
				strings.Join(e.Events, ","),
			})
		}
		PrintTable([]string{"ID", "URL", "FORMAT", "ENABLED", "EVENTS"}, rows)
	}
	return 0
}
