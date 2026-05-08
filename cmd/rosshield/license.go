package main

// license.go — `rosshield license info` 명령 (E24-C·B5 후속).
//
// GET /api/v1/license → 활성 라이선스 메타(edition·만료·features·quotas) 표시.
// 토큰 부재 시 stderr 안내 후 exit 2. enterprise 키 활성화는 운영 문서 참조 — 본 CLI는 read-only.

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

type licenseInfoBody struct {
	Edition   string   `json:"edition"`
	IssuedTo  string   `json:"issuedTo"`
	IssuedAt  string   `json:"issuedAt"`
	ExpiresAt string   `json:"expiresAt"`
	Expired   bool     `json:"expired"`
	Features  []string `json:"features"`
	Quotas    struct {
		RobotsMax       int `json:"robotsMax"`
		ScansPerDay     int `json:"scansPerDay"`
		LLMTokensPerDay int `json:"llmTokensPerDay"`
	} `json:"quotas"`
	Usage struct {
		CurrentRobots  int `json:"currentRobots"`
		ScansToday     int `json:"scansToday"`
		LLMTokensToday int `json:"llmTokensToday"`
	} `json:"usage"`
}

// runLicense는 `license ...` 서브커맨드를 분기합니다.
func runLicense(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: rosshield license info [-o table|json]")
		return 2
	}
	switch args[0] {
	case "info":
		return runLicenseInfo(args[1:])
	case "-h", "--help", "help":
		fmt.Fprintln(os.Stderr, `license 서브커맨드 — Open-core 라이선스 메타 조회 (read-only)

사용법:
  rosshield license info [-o table|json]

출력 필드:
  edition          community | enterprise
  issuedTo·At      라이선스 발급 대상·시점
  expiresAt        만료일 (community는 빈 값)
  expired          true면 enterprise feature 모두 비활성
  features         활성 enterprise 기능 (sso·mt·webhook·cloud·ha)
  quotas           robotsMax / scansPerDay / llmTokensPerDay (0 = unlimited)

exit code:
  0  OK
  1  config·HTTP 오류
  2  args·인증 누락`)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "license: unknown sub-command %q\n", args[0])
		return 2
	}
}

func runLicenseInfo(args []string) int {
	fs := flag.NewFlagSet("license info", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var (
		configPath = fs.String("config", "", "config 파일 경로")
		output     = fs.String("o", "table", "출력 포맷: table | json")
	)
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "rosshield license info: %v\n", err)
		return 2
	}

	format, err := ParseOutputFormat(*output)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rosshield license info: %v\n", err)
		return 2
	}

	cfgPath := *configPath
	if cfgPath == "" {
		cfgPath = DefaultConfigPath()
	}
	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rosshield license info: load config: %v\n", err)
		return 1
	}
	if cfg.AccessToken == "" {
		fmt.Fprintln(os.Stderr, "rosshield license info: no access token (run `rosshield login` first)")
		return 2
	}

	client := NewClient(cfg)
	var lic licenseInfoBody
	if err := client.Get(context.Background(), "/api/v1/license", nil, &lic); err != nil {
		fmt.Fprintf(os.Stderr, "rosshield license info: %v\n", err)
		return HTTPErrorToExitCode(err)
	}

	switch format {
	case OutputJSON:
		_ = PrintJSON(lic)
	default:
		rows := [][]string{
			{"edition", lic.Edition},
			{"issuedTo", lic.IssuedTo},
			{"issuedAt", lic.IssuedAt},
			{"expiresAt", lic.ExpiresAt},
			{"expired", strconv.FormatBool(lic.Expired)},
			{"features", strings.Join(lic.Features, ",")},
			{"quota.robotsMax", quotaStr(lic.Quotas.RobotsMax)},
			{"quota.scansPerDay", quotaStr(lic.Quotas.ScansPerDay)},
			{"quota.llmTokensPerDay", quotaStr(lic.Quotas.LLMTokensPerDay)},
			{"usage.currentRobots", strconv.Itoa(lic.Usage.CurrentRobots)},
			{"usage.scansToday", strconv.Itoa(lic.Usage.ScansToday)},
			{"usage.llmTokensToday", strconv.Itoa(lic.Usage.LLMTokensToday)},
		}
		PrintTable([]string{"KEY", "VALUE"}, rows)
	}
	return 0
}

// quotaStr는 quota 정수를 사람 가시 문자열로 변환합니다 (0 또는 음수 → "unlimited").
func quotaStr(n int) string {
	if n <= 0 {
		return "unlimited"
	}
	return strconv.Itoa(n)
}
