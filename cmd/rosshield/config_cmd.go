package main

// config_cmd.go — `rosshield config init|show` 서브커맨드 핸들러 (E9 Stage A).
//
// config.go는 Config 모델 + Load/Save 순수 헬퍼; 본 파일은 CLI 분기·flag 파싱·exit code.
// 분리 이유: 같은 모델을 Stage C(login/whoami)에서도 사용 — 핸들러는 stage별로 늘어나지만
// 모델은 안정.

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

// runConfig는 `config ...` 서브커맨드를 분기합니다.
func runConfig(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: rosshield config init|show [...]")
		return 2
	}
	switch args[0] {
	case "init":
		return runConfigInit(args[1:])
	case "show":
		return runConfigShow(args[1:])
	case "-h", "--help", "help":
		fmt.Fprintln(os.Stderr, `config 서브커맨드 — ~/.rosshield/config.yaml 관리

사용법:
  rosshield config init [--server URL] [--config <path>] [--force]
  rosshield config show [--config <path>] [-o table|json]

옵션:
  --server   서버 URL (기본 http://127.0.0.1:8080)
  --config   config 파일 경로 (기본 ~/.rosshield/config.yaml)
  --force    config init: 기존 파일을 덮어쓴다
  -o         config show: 출력 포맷 (table | json, 기본 table)

exit code:
  0  성공
  1  기존 파일 충돌(--force 미지정) 또는 I/O 오류
  2  invalid CLI args`)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "config: unknown sub-command %q\n", args[0])
		return 2
	}
}

// runConfigInit은 신규 config 파일을 생성합니다.
func runConfigInit(args []string) int {
	fs := flag.NewFlagSet("config init", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	server := fs.String("server", DefaultServerURL, "server URL")
	configPath := fs.String("config", DefaultConfigPath(), "config 파일 경로")
	force := fs.Bool("force", false, "기존 파일이 있으면 덮어쓴다")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "config init: flag parse error: %v\n", err)
		return 2
	}

	if !*force {
		if _, err := os.Stat(*configPath); err == nil {
			fmt.Fprintf(os.Stderr,
				"config init: %q already exists (use --force to overwrite)\n", *configPath)
			return 1
		} else if !errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(os.Stderr, "config init: stat %q: %v\n", *configPath, err)
			return 1
		}
	}

	cfg := Config{ServerURL: *server}
	if err := SaveConfig(*configPath, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "config init: %v\n", err)
		return 1
	}
	fmt.Printf("wrote %s\n", *configPath)
	return 0
}

// runConfigShow는 현재 config를 출력합니다 (token 마스킹).
func runConfigShow(args []string) int {
	fs := flag.NewFlagSet("config show", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("config", DefaultConfigPath(), "config 파일 경로")
	outFmt := fs.String("o", "table", "output format: table | json")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "config show: flag parse error: %v\n", err)
		return 2
	}
	format, err := ParseOutputFormat(*outFmt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config show: %v\n", err)
		return 2
	}

	cfg, err := LoadConfig(*configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(os.Stderr,
				"config show: %q not found (run 'rosshield config init')\n", *configPath)
			return 1
		}
		fmt.Fprintf(os.Stderr, "config show: %v\n", err)
		return 1
	}

	masked := Config{
		ServerURL:    cfg.ServerURL,
		AccessToken:  MaskToken(cfg.AccessToken),
		RefreshToken: MaskToken(cfg.RefreshToken),
		Email:        cfg.Email,
	}
	if format == OutputJSON {
		_ = PrintJSON(map[string]string{
			"path":         *configPath,
			"serverUrl":    masked.ServerURL,
			"email":        masked.Email,
			"accessToken":  masked.AccessToken,
			"refreshToken": masked.RefreshToken,
		})
		return 0
	}
	PrintTable([]string{"KEY", "VALUE"}, [][]string{
		{"path", *configPath},
		{"serverUrl", masked.ServerURL},
		{"email", masked.Email},
		{"accessToken", masked.AccessToken},
		{"refreshToken", masked.RefreshToken},
	})
	return 0
}
