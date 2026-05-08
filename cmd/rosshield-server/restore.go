package main

// restore.go — `rosshield-server restore --input <path>` 서브커맨드 (E28).
//
// backup이 만든 .tar.gz를 빈 DataDir에 풀어 데이터를 복구합니다. 핵심 가드:
//
//   - 기본은 빈 디렉터리만 허용 — 기존 데이터 덮어쓰기 사고 차단.
//   - --force 옵션으로 강제 진행 (운영자가 명시적으로 동의).
//   - tar 본문에서 ".." 경로 절대 거부 (path traversal 방어).
//
// 출력은 stdout JSON `{restoredFrom, dataDir, sizeBytes, entries}`.
//
// exit code:
//
//	0 — 성공
//	1 — input 미존재·tar 파싱 실패·쓰기 오류
//	2 — CLI args / validation 오류 (output 미지정·기존 파일 — non-empty without --force)
//	3 — path traversal 거부

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// restoreOptions는 `restore` CLI 입력 묶음입니다.
type restoreOptions struct {
	input   string
	dataDir string
	force   bool
}

// restoreOutput은 stdout JSON 출력 형식입니다.
type restoreOutput struct {
	RestoredFrom string `json:"restoredFrom"`
	DataDir      string `json:"dataDir"`
	SizeBytes    int64  `json:"sizeBytes"`
	Entries      int    `json:"entries"`
}

// restoreSubcommand는 `restore ...` 서브커맨드를 처리합니다.
func restoreSubcommand(args []string) int {
	for _, a := range args {
		if a == "-h" || a == "--help" || a == "help" {
			fmt.Fprintln(os.Stderr, `restore 서브커맨드 — backup tar.gz를 DataDir에 복구

사용법:
  rosshield-server restore --input <path.tar.gz> [--data-dir <path>] [--force]

옵션:
  --input     backup tar.gz 입력 경로 (필수)
  --data-dir  복원 대상 디렉터리 (기본 ~/.rosshield, 비어있어야 함)
  --force     기존 파일이 있어도 진행 (위험 — 기존 데이터 덮어쓰기)

출력:
  stdout — JSON {restoredFrom, dataDir, sizeBytes, entries}

exit code:
  0  성공
  1  input 미존재·tar 손상·쓰기 오류
  2  CLI args / validation 오류 또는 비어있지 않은 dir (--force 없이)
  3  tar entry path traversal 거부`)
			return 0
		}
	}
	return runRestore(args)
}

// runRestore는 `restore` 본 흐름입니다.
func runRestore(args []string) int {
	opts, code := parseRestoreFlags(args)
	if code != 0 {
		return code
	}

	if _, err := os.Stat(opts.input); err != nil {
		fmt.Fprintf(os.Stderr, "restore: input not found: %v\n", err)
		return 1
	}

	if err := os.MkdirAll(opts.dataDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "restore: mkdir data-dir: %v\n", err)
		return 1
	}

	if !opts.force {
		empty, err := isDirEmpty(opts.dataDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "restore: read data-dir: %v\n", err)
			return 1
		}
		if !empty {
			fmt.Fprintf(os.Stderr,
				"restore: --data-dir %q is not empty — refusing to overwrite. Use --force to proceed.\n",
				opts.dataDir)
			return 2
		}
	}

	totalBytes, entries, code := extractArchive(opts)
	if code != 0 {
		return code
	}

	out := restoreOutput{
		RestoredFrom: opts.input,
		DataDir:      opts.dataDir,
		SizeBytes:    totalBytes,
		Entries:      entries,
	}
	emitRestoreJSON(os.Stdout, out)
	return 0
}

// parseRestoreFlags는 flag 파싱 + 필수 옵션 누락 여부 체크입니다.
func parseRestoreFlags(args []string) (restoreOptions, int) {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	var opts restoreOptions
	fs.StringVar(&opts.input, "input", "", "backup tar.gz 입력 경로 (필수)")
	fs.StringVar(&opts.dataDir, "data-dir", defaultDataDir(), "복원 대상 디렉터리")
	fs.BoolVar(&opts.force, "force", false, "기존 파일이 있어도 진행")

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "restore: flag parse error: %v\n", err)
		return opts, 2
	}
	if strings.TrimSpace(opts.input) == "" {
		fmt.Fprintln(os.Stderr, "restore: --input is required.")
		return opts, 2
	}
	if strings.TrimSpace(opts.dataDir) == "" {
		fmt.Fprintln(os.Stderr, "restore: --data-dir is required.")
		return opts, 2
	}
	return opts, 0
}

// isDirEmpty는 dir 안에 entry가 하나도 없는지 확인합니다 (가드용).
func isDirEmpty(dir string) (bool, error) {
	f, err := os.Open(dir)
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()
	names, err := f.Readdirnames(1)
	if errors.Is(err, io.EOF) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return len(names) == 0, nil
}

// extractArchive는 input tar.gz를 파싱해 dataDir 아래에 풀어둡니다.
//
// 반환값: (totalBytes, entries, exitCode). exitCode != 0이면 stderr에 메시지 작성됨.
//   - 1: gzip/tar 파싱 또는 파일 쓰기 실패.
//   - 3: tar 엔트리 경로가 dataDir 밖을 가리킴 (path traversal).
func extractArchive(opts restoreOptions) (int64, int, int) {
	in, err := os.Open(opts.input)
	if err != nil {
		fmt.Fprintf(os.Stderr, "restore: open input: %v\n", err)
		return 0, 0, 1
	}
	defer func() { _ = in.Close() }()

	gz, err := gzip.NewReader(in)
	if err != nil {
		fmt.Fprintf(os.Stderr, "restore: gzip reader: %v\n", err)
		return 0, 0, 1
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	absDataDir, err := filepath.Abs(opts.dataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "restore: abs data-dir: %v\n", err)
		return 0, 0, 1
	}

	var total int64
	var count int
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "restore: tar Next: %v\n", err)
			return total, count, 1
		}

		// path traversal 가드 — Clean 후 dataDir prefix 강제.
		dst := filepath.Join(absDataDir, filepath.FromSlash(hdr.Name))
		dstClean := filepath.Clean(dst)
		if !strings.HasPrefix(dstClean, absDataDir+string(os.PathSeparator)) && dstClean != absDataDir {
			fmt.Fprintf(os.Stderr, "restore: refusing entry outside data-dir: %s\n", hdr.Name)
			return total, count, 3
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dstClean, 0o755); err != nil {
				fmt.Fprintf(os.Stderr, "restore: mkdir %s: %v\n", dstClean, err)
				return total, count, 1
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := writeTarFile(tr, dstClean, hdr); err != nil {
				fmt.Fprintf(os.Stderr, "restore: write %s: %v\n", dstClean, err)
				return total, count, 1
			}
			total += hdr.Size
			count++
		default:
			// symlink·hardlink·device 등은 스코프 밖 — backup이 안 만드는 type만 의도적으로 거부.
			fmt.Fprintf(os.Stderr, "restore: refusing unsupported tar entry type %d: %s\n",
				hdr.Typeflag, hdr.Name)
			return total, count, 1
		}
	}
	return total, count, 0
}

// writeTarFile은 단일 tar regular entry를 파일로 씁니다.
func writeTarFile(tr *tar.Reader, dst string, hdr *tar.Header) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("mkdir parent: %w", err)
	}
	mode := os.FileMode(hdr.Mode)
	if mode == 0 {
		mode = 0o644
	}
	f, err := os.OpenFile(dst, os.O_RDWR|os.O_CREATE|os.O_TRUNC, mode.Perm())
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer func() { _ = f.Close() }()

	// G110 (decompression bomb): tar 헤더 size가 양수일 때만 그만큼 복사. CopyN으로 상한 강제.
	if hdr.Size < 0 {
		return fmt.Errorf("invalid tar size %d", hdr.Size)
	}
	if _, err := io.CopyN(f, tr, hdr.Size); err != nil {
		return fmt.Errorf("copy body: %w", err)
	}
	return nil
}

// emitRestoreJSON은 결과를 indented JSON으로 stdout에 씁니다.
func emitRestoreJSON(w io.Writer, out restoreOutput) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}
