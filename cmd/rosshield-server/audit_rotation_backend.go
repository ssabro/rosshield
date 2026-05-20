package main

// audit_rotation_backend.go — Audit chain rotation cold backend selector.
//
// Config.AuditColdBackend 값을 기반으로 rotation.Backend를 빌드합니다.
//
//   - "" / "file" → DataDir/audit-archives 로컬 디렉토리 (Apache-2.0 코어).
//   - "s3"        → S3Backend (BSL 1.1 enterprise, build tag `rosshield_enterprise`).
//                   코어 빌드에서는 ErrS3BackendNotAvailable → file backend로 graceful fallback.
//
// 설계 참조:
//   - docs/design/notes/audit-chain-rotation-design.md §D-AR-9
//   - docs/onboarding/audit-rotation-s3.md
//
// HA 활성 환경은 cronsched RoleProvider gate(E25 Stage 4a)가 follower tick을 skip하므로
// backend 자체는 leader 단일 인스턴스에서만 PUT 호출 — 동시 PUT race 없음.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/ssabro/rosshield/internal/domain/audit/rotation"
)

// buildRotationBackend는 cfg 값을 기반으로 rotation.Backend를 빌드합니다.
//
// 2번째 반환값은 운영자 식별용 description 문자열 (로그용).
//
// 에러 케이스:
//   - "s3" 모드 + Bucket·Region 누락 → error (필수 필드).
//   - "s3" 모드 + 코어 빌드 → warning log + file backend fallback (error 아님).
//   - file mkdir 실패 → error.
func buildRotationBackend(ctx context.Context, cfg Config, logger *slog.Logger) (rotation.Backend, string, error) {
	kind := strings.ToLower(strings.TrimSpace(cfg.AuditColdBackend))
	switch kind {
	case "", "file":
		return newFileRotationBackend(cfg)
	case "s3":
		return newS3RotationBackend(ctx, cfg, logger)
	default:
		return nil, "", fmt.Errorf("unknown AuditColdBackend %q (valid: \"file\", \"s3\")", cfg.AuditColdBackend)
	}
}

// newFileRotationBackend는 DataDir/audit-archives 하위 file backend를 생성합니다.
func newFileRotationBackend(cfg Config) (rotation.Backend, string, error) {
	root := filepath.Join(cfg.DataDir, "audit-archives")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, "", fmt.Errorf("mkdir rotation archive dir: %w", err)
	}
	be, err := rotation.NewFileBackend(root)
	if err != nil {
		return nil, "", err
	}
	return be, "file://" + root, nil
}

// newS3RotationBackend는 cfg.AuditS3* 필드를 기반으로 S3 backend를 생성합니다.
//
// 코어 빌드(enterprise tag 없음)에서 ErrS3BackendNotAvailable이 반환되면
// warning log + file backend로 graceful fallback. enterprise 빌드에서 필수 필드 누락 시 error.
func newS3RotationBackend(ctx context.Context, cfg Config, logger *slog.Logger) (rotation.Backend, string, error) {
	if cfg.AuditS3Bucket == "" {
		return nil, "", errors.New("AuditColdBackend=s3 requires AuditS3Bucket (env ROSSHIELD_AUDIT_S3_BUCKET)")
	}
	if cfg.AuditS3Region == "" {
		return nil, "", errors.New("AuditColdBackend=s3 requires AuditS3Region (env ROSSHIELD_AUDIT_S3_REGION)")
	}

	s3cfg := rotation.S3Config{
		Region:               cfg.AuditS3Region,
		Bucket:               cfg.AuditS3Bucket,
		Prefix:               cfg.AuditS3Prefix,
		EndpointURL:          cfg.AuditS3Endpoint,
		ForcePathStyle:       cfg.AuditS3ForcePathStyle,
		ServerSideEncryption: cfg.AuditS3SSE,
		KMSKeyID:             cfg.AuditS3KMSKeyID,
	}
	be, err := rotation.NewS3Backend(ctx, s3cfg)
	if err != nil {
		if errors.Is(err, rotation.ErrS3BackendNotAvailable) {
			logger.Warn("audit rotation backend s3 requested but core build (no `rosshield_enterprise` tag) — falling back to file backend",
				"bucket", cfg.AuditS3Bucket, "region", cfg.AuditS3Region,
				"hint", "build enterprise binary with `make build-enterprise` or set AuditColdBackend=file")
			return newFileRotationBackend(cfg)
		}
		return nil, "", fmt.Errorf("s3 backend init: %w", err)
	}
	desc := fmt.Sprintf("s3://%s/%s (region=%s, sse=%q)",
		cfg.AuditS3Bucket, cfg.AuditS3Prefix, cfg.AuditS3Region, cfg.AuditS3SSE)
	return be, desc, nil
}
