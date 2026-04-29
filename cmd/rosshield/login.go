package main

// login.go — `rosshield login` 명령 (E9 Stage C, R11-2 HTTP).
//
// POST /api/v1/auth/login → accessToken·refreshToken·user 회수 → ~/.rosshield/config.yaml
// 에 영속 (chmod 600). 기존 config 파일이 있으면 ServerURL만 보존하고 토큰 갱신.
//
// 흐름:
//
//	rosshield login --email admin@example.com [--password p | --password-stdin]
//	                [--server URL] [--config PATH]
//
// 결과 stdout (R11-5 -o table 기본 / -o json 옵션):
//
//	logged in as admin@example.com (us_..., tn_...)
//	access token saved to /home/u/.rosshield/config.yaml

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

// loginRequestBody는 POST /api/v1/auth/login 요청 본문입니다 (서버 spec 미러).
type loginRequestBody struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// loginResponseBody는 POST /api/v1/auth/login 응답 본문 (서버 spec 미러).
type loginResponseBody struct {
	AccessToken  string            `json:"accessToken"`
	RefreshToken string            `json:"refreshToken"`
	User         loginResponseUser `json:"user"`
}

type loginResponseUser struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
	TenantID    string `json:"tenantId"`
}

// runLogin은 `rosshield login` 진입점입니다.
func runLogin(args []string) int {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // 자체 usage 사용
	var (
		email         = fs.String("email", "", "관리자 이메일 (필수)")
		password      = fs.String("password", "", "패스워드 (CLI 노출 비권장 — --password-stdin 권장)")
		passwordStdin = fs.Bool("password-stdin", false, "stdin에서 패스워드 한 줄 읽기")
		server        = fs.String("server", "", "서버 URL (config 우선; 명시 시 override)")
		configPath    = fs.String("config", "", "config 파일 경로 (기본 ~/.rosshield/config.yaml)")
		output        = fs.String("o", "table", "출력 포맷: table | json")
	)
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "rosshield login: %v\n", err)
		return 2
	}
	if *email == "" {
		fmt.Fprintln(os.Stderr, "rosshield login: --email is required")
		return 2
	}

	pw := *password
	if *passwordStdin {
		s := bufio.NewScanner(os.Stdin)
		if s.Scan() {
			pw = s.Text()
		}
	}
	if pw == "" {
		fmt.Fprintln(os.Stderr, "rosshield login: password is required (use --password or --password-stdin)")
		return 2
	}

	format, err := ParseOutputFormat(*output)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rosshield login: %v\n", err)
		return 2
	}

	cfgPath := *configPath
	if cfgPath == "" {
		cfgPath = DefaultConfigPath()
	}

	// 기존 config 로드(있으면). ServerURL은 명시 옵션 > config > default 순.
	cfg, _ := LoadConfig(cfgPath)
	if *server != "" {
		cfg.ServerURL = *server
	}
	if cfg.ServerURL == "" {
		cfg.ServerURL = DefaultServerURL
	}

	client := NewClient(cfg)
	var resp loginResponseBody
	if err := client.Post(context.Background(), "/api/v1/auth/login",
		loginRequestBody{Email: strings.ToLower(strings.TrimSpace(*email)), Password: pw},
		&resp); err != nil {
		fmt.Fprintf(os.Stderr, "rosshield login: %v\n", err)
		return HTTPErrorToExitCode(err)
	}

	cfg.Email = resp.User.Email
	cfg.AccessToken = resp.AccessToken
	cfg.RefreshToken = resp.RefreshToken
	if err := SaveConfig(cfgPath, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "rosshield login: save config: %v\n", err)
		return 1
	}

	switch format {
	case OutputJSON:
		_ = PrintJSON(map[string]any{
			"email":      resp.User.Email,
			"userId":     resp.User.ID,
			"tenantId":   resp.User.TenantID,
			"configPath": cfgPath,
		})
	default:
		PrintTable(
			[]string{"KEY", "VALUE"},
			[][]string{
				{"email", resp.User.Email},
				{"userId", resp.User.ID},
				{"tenantId", resp.User.TenantID},
				{"configPath", cfgPath},
			},
		)
	}
	return 0
}

// 컴파일 가드 — errors.As가 본 패키지 build에 포함되도록.
var _ = errors.As
