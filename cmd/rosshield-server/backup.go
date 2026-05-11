package main

// backup.go — `rosshield-server backup --output <path>` 서브커맨드 (E28).
//
// 서버 실행 중에도 안전한 일관 스냅샷을 .tar.gz으로 만들어 운영자가 외부 스토리지
// (S3, NAS 등)로 옮길 수 있게 합니다. 핵심 디자인:
//
//   - SQLite consistent snapshot: VACUUM INTO '<temp>.db' — WAL 합류 + 단일 파일 떠냄.
//     서버 라이브 트랜잭션이 함께 흘러도 스냅샷은 호출 시점 commit 상태로 고정.
//   - keys/* + evidence/* 디렉터리는 그대로 tar에 포함 (옵션 --skip-evidence).
//   - 출력은 .tar.gz 파일 1건 + stdout JSON `{path, size, sha256, includesEvidence}`.
//
// exit code:
//
//	0 — 성공
//	1 — bootstrap·storage·tar·gzip 오류
//	2 — CLI args / validation 오류

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" // VACUUM INTO 직접 실행용 driver registration.
)

// backupOptions는 `backup` CLI 입력 묶음입니다.
type backupOptions struct {
	output       string
	dataDir      string
	skipEvidence bool
}

// backupOutput은 stdout JSON 출력 형식입니다.
type backupOutput struct {
	Path             string `json:"path"`
	Size             int64  `json:"size"`
	SHA256           string `json:"sha256"`
	IncludesEvidence bool   `json:"includesEvidence"`
	GeneratedAt      string `json:"generatedAt"`
}

// backupSubcommand는 `backup ...` 서브커맨드를 처리합니다.
func backupSubcommand(args []string) int {
	for _, a := range args {
		if a == "-h" || a == "--help" || a == "help" {
			fmt.Fprintln(os.Stderr, `backup 서브커맨드 — 데이터 디렉터리 일관 스냅샷 생성

사용법:
  rosshield-server backup --output <path.tar.gz> [--data-dir <path>] [--skip-evidence]

옵션:
  --output         tar.gz 파일 출력 경로 (필수)
  --data-dir       데이터 디렉터리 (기본 ~/.rosshield)
  --skip-evidence  evidence/ 디렉터리를 백업에서 제외 (metadata-only 백업)

출력:
  stdout — JSON {path, size, sha256, includesEvidence, generatedAt}

exit code:
  0  성공
  1  bootstrap·storage·tar 쓰기 오류
  2  CLI args / validation 오류`)
			return 0
		}
	}
	return runBackup(args)
}

// runBackup은 `backup` 본 흐름입니다.
//
// Platform Bootstrap을 거치지 않고 SQLite 파일을 직접 열어 VACUUM INTO 실행 — 운영 중인
// 서버 인스턴스와 동시 실행 가능 (modernc.org/sqlite는 WAL 동시 read 지원, VACUUM INTO는
// reader-only 작업으로 처리됨). 별도 connection 사용 → 운영 인스턴스의 lock·busy 영향
// 최소화. 출처 db가 없으면 "백업할 것이 없다"로 판단해 에러 반환.
func runBackup(args []string) int {
	opts, code := parseBackupFlags(args)
	if code != 0 {
		return code
	}

	out, err := executeBackup(context.Background(), opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "backup: %v\n", err)
		return 1
	}
	emitBackupJSON(os.Stdout, out)
	return 0
}

// executeBackup은 backupOptions 기반 일관 스냅샷을 tar.gz으로 생성합니다 (E28 + B7 후속).
//
// CLI 서브커맨드(runBackup) + scheduler 자동 backup(BackupRunner) 둘 다 본 함수를 사용 —
// 동작·에러 의미 일치 보장.
//
// 호출자 책임:
//   - opts.output 디렉터리 사전 생성
//   - opts.dataDir 존재 + data.db 가용성 확인은 본 함수가 수행 (없으면 에러)
//
// ctx는 VACUUM INTO + tar 작성 전체에 적용. 호출자 timeout으로 5분 권장.
func executeBackup(ctx context.Context, opts backupOptions) (backupOutput, error) {
	dbPath := filepath.Join(opts.dataDir, "data.db")
	if _, err := os.Stat(dbPath); err != nil {
		return backupOutput{}, fmt.Errorf("source db not found at %s: %w", dbPath, err)
	}

	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
	}

	tmpDB, err := snapshotDatabase(ctx, dbPath, opts.dataDir)
	if err != nil {
		return backupOutput{}, fmt.Errorf("snapshot db: %w", err)
	}
	defer func() { _ = os.Remove(tmpDB) }()

	size, sum, err := writeBackupArchive(opts, tmpDB)
	if err != nil {
		return backupOutput{}, fmt.Errorf("write archive: %w", err)
	}

	return backupOutput{
		Path:             opts.output,
		Size:             size,
		SHA256:           sum,
		IncludesEvidence: !opts.skipEvidence,
		GeneratedAt:      time.Now().UTC().Format(time.RFC3339Nano),
	}, nil
}

// parseBackupFlags는 flag 파싱 + 필수 옵션 누락 여부 체크입니다.
func parseBackupFlags(args []string) (backupOptions, int) {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	var opts backupOptions
	fs.StringVar(&opts.output, "output", "", "tar.gz 출력 경로 (필수)")
	fs.StringVar(&opts.dataDir, "data-dir", defaultDataDir(), "데이터 디렉터리")
	fs.BoolVar(&opts.skipEvidence, "skip-evidence", false, "evidence/ 디렉터리 제외")

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "backup: flag parse error: %v\n", err)
		return opts, 2
	}
	if strings.TrimSpace(opts.output) == "" {
		fmt.Fprintln(os.Stderr, "backup: --output is required.")
		return opts, 2
	}
	if strings.TrimSpace(opts.dataDir) == "" {
		fmt.Fprintln(os.Stderr, "backup: --data-dir is required.")
		return opts, 2
	}
	return opts, 0
}

// snapshotDatabase는 SQLite VACUUM INTO로 일관 스냅샷 db 파일을 만듭니다.
//
// VACUUM INTO 'path'는 호출 시점의 commit 상태를 단일 파일로 떠냅니다(WAL 합류 포함).
// 서버 라이브 트랜잭션과 동시에 실행되어도 스냅샷은 호출 시점 view를 보장.
//
// VACUUM은 트랜잭션 내부에서 실행할 수 없으므로 storage.Storage 인터페이스(Tx 래퍼) 대신
// modernc.org/sqlite 드라이버를 직접 열고 raw connection에서 Exec — Tx 진입 없이.
// MaxOpenConns=1로 제한해 동시 statement 충돌 방지. busy_timeout 5s를 PRAGMA로 설정.
//
// 반환값은 생성된 임시 db 파일 경로 — 호출자가 cleanup 책임.
func snapshotDatabase(ctx context.Context, srcDBPath, tmpDir string) (string, error) {
	tmp, err := os.CreateTemp(tmpDir, ".backup-snapshot-*.db")
	if err != nil {
		return "", fmt.Errorf("create temp snapshot file: %w", err)
	}
	tmpPath := tmp.Name()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("close temp file: %w", err)
	}
	// VACUUM INTO는 destination이 존재하지 않아야 함 — Create로 만든 빈 파일 삭제.
	if err := os.Remove(tmpPath); err != nil {
		return "", fmt.Errorf("remove placeholder: %w", err)
	}

	// 별도 *sql.DB로 source db를 read 모드 컨셉으로 open (실제 read-only 모드 강제는
	// modernc.org/sqlite가 ?mode=ro 지원 — 다만 VACUUM INTO는 내부 read 작업이라 일반 모드도 OK).
	db, err := sql.Open("sqlite", srcDBPath)
	if err != nil {
		return "", fmt.Errorf("open source db: %w", err)
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)

	if _, err := db.ExecContext(ctx, "PRAGMA busy_timeout = 5000"); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("set busy_timeout: %w", err)
	}

	// SQL injection 위험 차단: tmpPath는 우리가 만든 신뢰 경로 + 단일 quote escape.
	escaped := strings.ReplaceAll(tmpPath, "'", "''")
	query := fmt.Sprintf("VACUUM INTO '%s'", escaped)

	if _, err := db.ExecContext(ctx, query); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("VACUUM INTO failed: %w", err)
	}

	if _, err := os.Stat(tmpPath); err != nil {
		return "", fmt.Errorf("snapshot file missing after VACUUM INTO: %w", err)
	}
	return tmpPath, nil
}

// writeBackupArchive는 tar.gz 아카이브를 생성하고 (size, sha256-hex)를 반환합니다.
//
// 아카이브 구조:
//
//	data.db                              ← VACUUM INTO 스냅샷 (snapshotPath의 내용)
//	keys/<file>...                       ← <dataDir>/keys/* 그대로
//	evidence/<aa>/<bb>/<sha>.blob...     ← <dataDir>/evidence/** (옵션 --skip-evidence면 제외)
//
// .staging/, .quarantine/, .backup-snapshot-*.db 같은 transient 파일은 제외.
func writeBackupArchive(opts backupOptions, snapshotPath string) (int64, string, error) {
	if err := os.MkdirAll(filepath.Dir(opts.output), 0o755); err != nil {
		return 0, "", fmt.Errorf("mkdir output dir: %w", err)
	}
	out, err := os.Create(opts.output)
	if err != nil {
		return 0, "", fmt.Errorf("create output: %w", err)
	}
	defer func() { _ = out.Close() }()

	// 출력 sha256은 gzip된 외부 byte 기준 (파일 자체 무결성 — 외부 검증·attest 용).
	hasher := sha256.New()
	mw := io.MultiWriter(out, hasher)

	gz := gzip.NewWriter(mw)
	tw := tar.NewWriter(gz)

	if err := writeArchiveEntries(tw, opts, snapshotPath); err != nil {
		_ = tw.Close()
		_ = gz.Close()
		return 0, "", err
	}

	if err := tw.Close(); err != nil {
		_ = gz.Close()
		return 0, "", fmt.Errorf("close tar: %w", err)
	}
	if err := gz.Close(); err != nil {
		return 0, "", fmt.Errorf("close gzip: %w", err)
	}
	if err := out.Sync(); err != nil {
		return 0, "", fmt.Errorf("sync output: %w", err)
	}

	st, err := out.Stat()
	if err != nil {
		return 0, "", fmt.Errorf("stat output: %w", err)
	}
	return st.Size(), hex.EncodeToString(hasher.Sum(nil)), nil
}

// writeArchiveEntries는 data.db (스냅샷) + keys/ + evidence/ 항목을 tar에 씁니다.
func writeArchiveEntries(tw *tar.Writer, opts backupOptions, snapshotPath string) error {
	// 1) 스냅샷 db → 아카이브 루트에 "data.db"로.
	if err := writeFileToTar(tw, snapshotPath, "data.db"); err != nil {
		return fmt.Errorf("tar data.db: %w", err)
	}

	// 2) keys/ 전체 — Phase 1 단순 1-level (bootstrap이 ed25519·KEK 평면 파일 생성).
	keysDir := filepath.Join(opts.dataDir, "keys")
	if err := walkAndArchive(tw, keysDir, "keys"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("tar keys: %w", err)
	}

	// 3) evidence/ — sharded 2-level. .staging·.quarantine 제외.
	if !opts.skipEvidence {
		evidenceDir := filepath.Join(opts.dataDir, "evidence")
		if err := walkAndArchive(tw, evidenceDir, "evidence"); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("tar evidence: %w", err)
		}
	}
	return nil
}

// walkAndArchive는 root 아래 모든 파일을 archivePrefix/<rel> 이름으로 tar에 씁니다.
// .staging·.quarantine·.backup-snapshot-*는 transient로 제외.
func walkAndArchive(tw *tar.Writer, root, archivePrefix string) error {
	st, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return fmt.Errorf("expected dir at %s, got file", root)
	}

	return filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		// 디렉터리 자체는 항목으로 추가하지 않음 — 파일만 (필요 시 자동 디렉터리 생성).
		if info.IsDir() {
			base := filepath.Base(path)
			if base == ".staging" || base == ".quarantine" {
				return filepath.SkipDir
			}
			return nil
		}
		name := filepath.Base(path)
		if strings.HasPrefix(name, ".backup-snapshot-") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		// tar는 항상 forward slash.
		archiveName := archivePrefix + "/" + filepath.ToSlash(rel)
		return writeFileToTar(tw, path, archiveName)
	})
}

// writeFileToTar는 단일 파일을 tar에 추가합니다 (regular file).
func writeFileToTar(tw *tar.Writer, srcPath, archiveName string) error {
	st, err := os.Stat(srcPath)
	if err != nil {
		return err
	}
	hdr := &tar.Header{
		Name:     archiveName,
		Mode:     int64(st.Mode().Perm()),
		Size:     st.Size(),
		ModTime:  st.ModTime().UTC(),
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("tar header for %s: %w", archiveName, err)
	}
	f, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("open %s: %w", srcPath, err)
	}
	defer func() { _ = f.Close() }()
	if _, err := io.Copy(tw, f); err != nil {
		return fmt.Errorf("copy %s: %w", srcPath, err)
	}
	return nil
}

// emitBackupJSON은 결과를 indented JSON으로 stdout에 씁니다.
func emitBackupJSON(w io.Writer, out backupOutput) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}
