package main

// backup_scheduler.go — B7 후속: 자동 백업 schedule + 백업 디렉터리 메타 list.
//
// 두 entry point:
//   - registerBackupJob: bootstrap에서 cron spec이 비지 않으면 sch.Schedule로 등록.
//     매 tick 시 BackupDir에 auto-YYYYMMDDHHMMSS.tar.gz 생성. HA 활성 시 leader-only
//     자동 적용 (cronsched.Scheduler.runJob의 RoleProvider gate, E25 Stage 4a).
//   - listBackups: 디렉터리 디스크 스캔 → 메타 슬라이스 (HTTP /api/v1/backups 핸들러용).
//
// 보안: BackupDir 내부 .tar.gz 파일만 노출, path traversal 방어, sha256 매번 재계산.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ssabro/rosshield/internal/platform/scheduler"
)

// backupSchedulerJobID는 cron 등록 시 사용되는 식별자입니다.
const backupSchedulerJobID = "system.backup.auto"

// autoBackupFilenameLayout은 자동 백업 파일명 시각 부분 포맷입니다 (UTC).
const autoBackupFilenameLayout = "20060102-150405"

// BackupMeta는 list endpoint 응답 + 자동 백업 stdout JSON에 사용됩니다.
type BackupMeta struct {
	Filename         string `json:"filename"`
	Size             int64  `json:"size"`
	SHA256           string `json:"sha256"`
	GeneratedAt      string `json:"generatedAt"`
	IncludesEvidence bool   `json:"includesEvidence"`
}

// registerBackupJob은 sch에 자동 백업 cron job을 등록합니다.
//
// spec=""이면 no-op (자동 백업 비활성). dataDir·backupDir이 모두 필요.
// HA 활성 환경에서 cronsched가 follower tick을 silent skip하므로 leader 단일 인스턴스만 백업 수행.
func registerBackupJob(sch scheduler.Scheduler, spec, dataDir, backupDir string, skipEvidence bool, logger *slog.Logger) error {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil
	}
	if strings.TrimSpace(dataDir) == "" {
		return fmt.Errorf("backup schedule: dataDir is required")
	}
	if strings.TrimSpace(backupDir) == "" {
		return fmt.Errorf("backup schedule: backupDir is required when BackupSchedule is set")
	}
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return fmt.Errorf("backup schedule: mkdir %q: %w", backupDir, err)
	}

	job := func(ctx context.Context) error {
		now := time.Now().UTC()
		filename := "auto-" + now.Format(autoBackupFilenameLayout) + ".tar.gz"
		out, err := executeBackup(ctx, backupOptions{
			output:       filepath.Join(backupDir, filename),
			dataDir:      dataDir,
			skipEvidence: skipEvidence,
		})
		if err != nil {
			logger.Error("backup auto: execute failed", "err", err.Error(), "filename", filename)
			return err
		}
		logger.Info("backup auto: completed",
			"filename", filename, "size", out.Size, "sha256", out.SHA256, "skipEvidence", skipEvidence)
		return nil
	}
	if err := sch.Schedule(backupSchedulerJobID, spec, job); err != nil {
		return fmt.Errorf("backup schedule: register cron %q: %w", spec, err)
	}
	logger.Info("backup auto schedule registered",
		"spec", spec, "dataDir", dataDir, "backupDir", backupDir,
		"skipEvidence", skipEvidence, "jobId", backupSchedulerJobID)
	return nil
}

// listBackups는 디렉터리에서 *.tar.gz 파일을 스캔해 메타 슬라이스를 반환합니다.
//
// 정렬: GeneratedAt 내림차순 (최신 백업 먼저).
// SHA256은 매 호출 시 재계산 — 디스크 무결성·운영자 신뢰 우선. 큰 백업이 많으면 향후
// 메타 캐시 도입 (Phase 5 1차는 단순 디스크 스캔).
//
// dir이 부재이거나 빈 디렉터리면 빈 슬라이스 + nil.
func listBackups(dir string) ([]BackupMeta, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("listBackups: read dir %q: %w", dir, err)
	}

	var out []BackupMeta
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		// path traversal 방어: 단순 파일명만 (Base가 자기 자신과 같아야 함).
		if filepath.Base(name) != name {
			continue
		}
		if !strings.HasSuffix(name, ".tar.gz") {
			continue
		}
		full := filepath.Join(dir, name)
		info, err := os.Stat(full)
		if err != nil {
			continue
		}
		sum, err := fileSHA256(full)
		if err != nil {
			continue
		}
		out = append(out, BackupMeta{
			Filename:         name,
			Size:             info.Size(),
			SHA256:           sum,
			GeneratedAt:      info.ModTime().UTC().Format(time.RFC3339Nano),
			IncludesEvidence: !strings.Contains(name, "skip-evidence"),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].GeneratedAt > out[j].GeneratedAt })
	return out, nil
}

// fileSHA256는 파일 sha256 hex digest를 반환합니다.
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path) // #nosec G304 — caller가 디렉터리 scope 검증
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
