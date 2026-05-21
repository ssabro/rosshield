package main

// audit.go — `rosshield audit` 서브커맨드 (Phase 10.D-6).
//
// 본 round 범위:
//
//	rosshield audit rotation abort --reason "<text>" [-o table|json]
//
// 운영자 emergency override — 진행 중 audit chain signer key rotation 을 차단하거나
// 다음 자동 rotation 1 회를 차단하고 audit.chain.rotation_aborted event 를 emit 합니다.
// 권한: admin (server 측 RequirePermission(tenant_admin, admin) middleware 통과 필요).
//
// 종료 코드 (R11-8):
//
//	0  성공
//	1  transport·config 실패
//	2  4xx (admin 권한 없음 / KeyRotator 미주입 / invalid args)
//	3  5xx (server 내부 오류)

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

// abortResponseView 는 POST /api/v1/audit/rotation/abort 응답입니다.
type abortResponseView struct {
	Aborted       bool   `json:"aborted"`
	AuditEntryID  int64  `json:"auditEntryId"`
	AbortedAt     string `json:"abortedAt"`
	PreviousEpoch int64  `json:"previousEpoch"`
	Reason        string `json:"reason"`
}

func runAudit(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: rosshield audit <rotation> ...")
		return 2
	}
	switch args[0] {
	case "rotation":
		return runAuditRotation(args[1:])
	case "-h", "--help", "help":
		fmt.Fprintln(os.Stderr, `audit 서브커맨드 — audit chain 운영 (Phase 10.D-6+)

사용법:
  rosshield audit rotation abort --reason "<text>" [-o table|json] [--config <path>]

audit chain signer key 의 자동 rotation 을 일시 차단하고 audit.chain.rotation_aborted
event 를 emit 합니다. admin 권한 필요. idempotent — 진행 중 rotation 없을 때도 호출
가능 (다음 자동 rotation 1 회를 건너뜀).`)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "audit: unknown sub-command %q\n", args[0])
		return 2
	}
}

func runAuditRotation(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: rosshield audit rotation <abort> ...")
		return 2
	}
	switch args[0] {
	case "abort":
		return runAuditRotationAbort(args[1:])
	case "-h", "--help", "help":
		fmt.Fprintln(os.Stderr, `audit rotation 서브커맨드:
  abort --reason "<text>"   진행 중/대기 중 rotation 차단 + audit emit`)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "audit rotation: unknown sub-command %q\n", args[0])
		return 2
	}
}

func runAuditRotationAbort(args []string) int {
	fs := flag.NewFlagSet("audit rotation abort", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var (
		configPath = fs.String("config", "", "config 파일 경로")
		reason     = fs.String("reason", "", "abort 사유 (운영자 메모 — audit_entries payload 에 기록)")
		output     = fs.String("o", "table", "출력 포맷 (table|json)")
	)
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "rosshield audit rotation abort: %v\n", err)
		return 2
	}
	if strings.TrimSpace(*reason) == "" {
		fmt.Fprintln(os.Stderr, "rosshield audit rotation abort: --reason is required")
		return 2
	}
	format, err := ParseOutputFormat(*output)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rosshield audit rotation abort: %v\n", err)
		return 2
	}

	client, code := newAuthenticatedClient(*configPath, "rosshield audit rotation abort")
	if client == nil {
		return code
	}

	body := map[string]string{"reason": *reason}
	var resp abortResponseView
	if err := client.Post(context.Background(), "/api/v1/audit/rotation/abort", body, &resp); err != nil {
		fmt.Fprintf(os.Stderr, "rosshield audit rotation abort: %v\n", err)
		return HTTPErrorToExitCode(err)
	}

	switch format {
	case OutputJSON:
		_ = PrintJSON(resp)
	default:
		rows := [][]string{
			{"aborted", fmt.Sprintf("%v", resp.Aborted)},
			{"auditEntryId", fmt.Sprintf("%d", resp.AuditEntryID)},
			{"previousEpoch", fmt.Sprintf("%d", resp.PreviousEpoch)},
			{"abortedAt", resp.AbortedAt},
			{"reason", resp.Reason},
		}
		PrintTable([]string{"FIELD", "VALUE"}, rows)
	}
	return 0
}
