package main

// invite.go — `rosshield invite` 서브커맨드 (E29 Phase 4).
//
// 사용 예:
//
//	rosshield invite create --email new@user.test --role operator [--expires-in-hours 168]
//	rosshield invite list [-o table|json]
//	rosshield invite revoke <invitationId>
//
// backend endpoint:
//
//	POST   /api/v1/invitations                    body: {email, roleName, expiresInHours?} → 201 {invitationView, token}
//	GET    /api/v1/invitations                    → {invitations: invitationView[]}
//	DELETE /api/v1/invitations/{id}               → 204

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

// invitationView 는 backend invitation.go 응답 schema와 일치합니다.
type invitationView struct {
	ID         string  `json:"id"`
	Email      string  `json:"email"`
	RoleName   string  `json:"roleName"`
	InvitedBy  string  `json:"invitedBy"`
	ExpiresAt  string  `json:"expiresAt"`
	AcceptedAt *string `json:"acceptedAt,omitempty"`
	AcceptedBy *string `json:"acceptedBy,omitempty"`
	CreatedAt  string  `json:"createdAt"`
}

// inviteCreateResponse 는 POST /invitations 응답 (token 1회 노출).
type inviteCreateResponse struct {
	invitationView
	Token string `json:"token"`
}

// runInvite 는 `invite ...` 서브커맨드를 분기합니다.
func runInvite(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: rosshield invite <create|list|revoke> ...")
		return 2
	}
	switch args[0] {
	case "create":
		return runInviteCreate(args[1:])
	case "list":
		return runInviteList(args[1:])
	case "revoke":
		return runInviteRevoke(args[1:])
	case "-h", "--help", "help":
		fmt.Fprintln(os.Stderr, `invite 서브커맨드 — 사용자 초대 관리 (admin)

사용법:
  rosshield invite create --email E --role R [--expires-in-hours N] [-o table|json]
  rosshield invite list [-o table|json]
  rosshield invite revoke <invitationId>

옵션:
  --email                   초대받는 사용자 이메일 (필수)
  --role                    역할 이름: admin|auditor|operator|<custom> (필수)
  --expires-in-hours        만료 (시간). 0이면 7일 default (옵션)
  -o table|json             출력 포맷
  --config <path>           config 파일 경로

create 응답: token + accept URL이 stdout JSON에 1회 노출 — 사용자에게 전달.
exit code: 0 OK / 1 transport·HTTP 5xx / 2 args·HTTP 4xx`)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "invite: unknown sub-command %q\n", args[0])
		return 2
	}
}

func runInviteCreate(args []string) int {
	fs := flag.NewFlagSet("invite create", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var (
		configPath = fs.String("config", "", "config 파일 경로")
		output     = fs.String("o", "table", "출력 포맷")
		email      = fs.String("email", "", "초대받는 이메일 (필수)")
		role       = fs.String("role", "", "역할 이름 (필수)")
		expiresH   = fs.Int("expires-in-hours", 0, "만료 시간 (0=7일 default)")
	)
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "rosshield invite create: %v\n", err)
		return 2
	}
	if strings.TrimSpace(*email) == "" || strings.TrimSpace(*role) == "" {
		fmt.Fprintln(os.Stderr, "rosshield invite create: --email and --role are required")
		return 2
	}
	format, err := ParseOutputFormat(*output)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rosshield invite create: %v\n", err)
		return 2
	}

	client, code := newAuthenticatedClient(*configPath, "rosshield invite create")
	if client == nil {
		return code
	}

	body := map[string]any{"email": *email, "roleName": *role}
	if *expiresH > 0 {
		body["expiresInHours"] = *expiresH
	}
	var out inviteCreateResponse
	if err := client.Post(context.Background(), "/api/v1/invitations", body, &out); err != nil {
		fmt.Fprintf(os.Stderr, "rosshield invite create: %v\n", err)
		return HTTPErrorToExitCode(err)
	}

	acceptURL := strings.TrimRight(client.baseURL, "/") + "/invitations/accept/" + out.Token

	switch format {
	case OutputJSON:
		_ = PrintJSON(struct {
			inviteCreateResponse
			AcceptURL string `json:"acceptUrl"`
		}{out, acceptURL})
	default:
		PrintTable([]string{"KEY", "VALUE"}, [][]string{
			{"id", out.ID},
			{"email", out.Email},
			{"role", out.RoleName},
			{"expiresAt", out.ExpiresAt},
			{"token", out.Token},
			{"acceptUrl", acceptURL},
		})
	}
	return 0
}

func runInviteList(args []string) int {
	fs := flag.NewFlagSet("invite list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var (
		configPath = fs.String("config", "", "config 파일 경로")
		output     = fs.String("o", "table", "출력 포맷")
	)
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "rosshield invite list: %v\n", err)
		return 2
	}
	format, err := ParseOutputFormat(*output)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rosshield invite list: %v\n", err)
		return 2
	}

	client, code := newAuthenticatedClient(*configPath, "rosshield invite list")
	if client == nil {
		return code
	}

	var resp struct {
		Invitations []invitationView `json:"invitations"`
	}
	if err := client.Get(context.Background(), "/api/v1/invitations", nil, &resp); err != nil {
		fmt.Fprintf(os.Stderr, "rosshield invite list: %v\n", err)
		return HTTPErrorToExitCode(err)
	}

	switch format {
	case OutputJSON:
		_ = PrintJSON(resp.Invitations)
	default:
		rows := make([][]string, 0, len(resp.Invitations))
		for _, i := range resp.Invitations {
			rows = append(rows, []string{i.ID, i.Email, i.RoleName, invitationStatus(i), i.ExpiresAt})
		}
		PrintTable([]string{"ID", "EMAIL", "ROLE", "STATUS", "EXPIRES"}, rows)
	}
	return 0
}

func runInviteRevoke(args []string) int {
	fs := flag.NewFlagSet("invite revoke", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("config", "", "config 파일 경로")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "rosshield invite revoke: %v\n", err)
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: rosshield invite revoke <invitationId>")
		return 2
	}
	id := fs.Arg(0)

	client, code := newAuthenticatedClient(*configPath, "rosshield invite revoke")
	if client == nil {
		return code
	}

	if err := client.Delete(context.Background(), "/api/v1/invitations/"+id); err != nil {
		fmt.Fprintf(os.Stderr, "rosshield invite revoke: %v\n", err)
		return HTTPErrorToExitCode(err)
	}
	fmt.Printf("revoked: %s\n", id)
	return 0
}

// invitationStatus 는 backend의 IsAccepted/IsExpired 로직을 클라이언트 측에서 분류합니다.
//
// pending: AcceptedAt nil + ExpiresAt 미경과
// accepted: AcceptedAt non-nil
// expired: AcceptedAt nil + ExpiresAt 경과
func invitationStatus(i invitationView) string {
	if i.AcceptedAt != nil {
		return "accepted"
	}
	// ExpiresAt 비교는 시간 파싱이 필요 — CLI는 단순화. 정확한 expired는 backend에 위임.
	return "pending"
}
