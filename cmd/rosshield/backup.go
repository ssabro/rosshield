package main

// backup.go — `rosshield backup list/download` 서브커맨드 (E29 후속, Phase 5).
//
// B7 Stage 1+2가 추가한 endpoint 활용:
//
//	GET /api/v1/backups                              → {ok, value: {backups: [BackupMeta...]}}
//	GET /api/v1/backups/{filename}/download          → application/gzip binary

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// backupMetaView는 GET /api/v1/backups 응답의 BackupMeta 형식입니다.
type backupMetaView struct {
	Filename         string `json:"filename"`
	Size             int64  `json:"size"`
	SHA256           string `json:"sha256"`
	GeneratedAt      string `json:"generatedAt"`
	IncludesEvidence bool   `json:"includesEvidence"`
}

func runBackup(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: rosshield backup <list|download> ...")
		return 2
	}
	switch args[0] {
	case "list":
		return runBackupList(args[1:])
	case "download":
		return runBackupDownload(args[1:])
	case "-h", "--help", "help":
		fmt.Fprintln(os.Stderr, `backup 서브커맨드 — 자동 백업 목록·다운로드 (B7 후속)

사용법:
  rosshield backup list [-o table|json]
  rosshield backup download <filename> [--output <path>] [--force]

옵션:
  --output <path>  download 저장 경로 (기본 ./<filename>)
  --force          --output 경로에 이미 파일 있으면 덮어씀
  -o table|json    list 출력 포맷 (기본 table)
  --config <path>  config 파일 경로

자동 백업은 server에서 --backup-schedule cron spec으로 옵트인 (snap에서는
'sudo snap set rosshield backup-schedule="@every 24h"' 후 systemctl restart).`)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "backup: unknown sub-command %q\n", args[0])
		return 2
	}
}

func runBackupList(args []string) int {
	fs := flag.NewFlagSet("backup list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var (
		configPath = fs.String("config", "", "config 파일 경로")
		output     = fs.String("o", "table", "출력 포맷")
	)
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "rosshield backup list: %v\n", err)
		return 2
	}
	format, err := ParseOutputFormat(*output)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rosshield backup list: %v\n", err)
		return 2
	}

	client, code := newAuthenticatedClient(*configPath, "rosshield backup list")
	if client == nil {
		return code
	}

	var resp struct {
		OK    bool `json:"ok"`
		Value struct {
			Backups []backupMetaView `json:"backups"`
		} `json:"value"`
	}
	if err := client.Get(context.Background(), "/api/v1/backups", nil, &resp); err != nil {
		fmt.Fprintf(os.Stderr, "rosshield backup list: %v\n", err)
		return HTTPErrorToExitCode(err)
	}

	switch format {
	case OutputJSON:
		_ = PrintJSON(resp.Value.Backups)
	default:
		rows := make([][]string, 0, len(resp.Value.Backups))
		for _, b := range resp.Value.Backups {
			rows = append(rows, []string{
				b.Filename,
				strconv.FormatInt(b.Size, 10),
				b.GeneratedAt,
				strconv.FormatBool(b.IncludesEvidence),
				b.SHA256[:min(16, len(b.SHA256))],
			})
		}
		PrintTable([]string{"FILENAME", "SIZE", "GENERATED_AT", "EVIDENCE", "SHA256_PREFIX"}, rows)
	}
	return 0
}

func runBackupDownload(args []string) int {
	fs := flag.NewFlagSet("backup download", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var (
		configPath = fs.String("config", "", "config 파일 경로")
		out        = fs.String("output", "", "저장 경로 (기본 ./<filename>)")
		force      = fs.Bool("force", false, "기존 파일 덮어씀")
	)
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "rosshield backup download: %v\n", err)
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: rosshield backup download <filename> [--output <path>] [--force]")
		return 2
	}
	filename := fs.Arg(0)

	// 안전성 검증 — server측에도 동일 가드(B7 Stage 2-A) 있지만 client도 사전 차단.
	if filepath.Base(filename) != filename {
		fmt.Fprintf(os.Stderr, "rosshield backup download: filename must be a simple base name (got: %s)\n", filename)
		return 2
	}
	if !strings.HasSuffix(filename, ".tar.gz") {
		fmt.Fprintf(os.Stderr, "rosshield backup download: filename must end with .tar.gz (got: %s)\n", filename)
		return 2
	}

	cfg, err := LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rosshield backup download: %v\n", err)
		return 2
	}
	if cfg.AccessToken == "" {
		fmt.Fprintln(os.Stderr, "rosshield backup download: not authenticated (run 'rosshield login' first)")
		return 2
	}

	target := *out
	if target == "" {
		target = filename
	}
	if !*force {
		if _, err := os.Stat(target); err == nil {
			fmt.Fprintf(os.Stderr, "rosshield backup download: %s already exists (use --force to overwrite)\n", target)
			return 1
		}
	}

	url := strings.TrimRight(cfg.ServerURL, "/") + "/api/v1/backups/" + filename + "/download"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rosshield backup download: build request: %v\n", err)
		return 1
	}
	req.Header.Set("Authorization", "Bearer "+cfg.AccessToken)

	resp, err := (&http.Client{Timeout: 5 * time.Minute}).Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rosshield backup download: transport: %v\n", err)
		return 1
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		fmt.Fprintf(os.Stderr, "rosshield backup download: HTTP %d: %s\n", resp.StatusCode, string(body))
		return 1
	}

	f, err := os.Create(target) // #nosec G304 — caller가 target 명시
	if err != nil {
		fmt.Fprintf(os.Stderr, "rosshield backup download: create %s: %v\n", target, err)
		return 1
	}
	defer func() { _ = f.Close() }()

	n, err := io.Copy(f, resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rosshield backup download: write: %v\n", err)
		return 1
	}
	fmt.Printf("downloaded: %s (%d bytes)\n", target, n)
	return 0
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
