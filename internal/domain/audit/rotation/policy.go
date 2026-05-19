// Package rotation은 audit chain rotation 도메인입니다.
//
// rotation은 hot chain의 오래된 segment를 cold archive로 이동시키고,
// 그 사실을 audit chain에 `audit.rotate.complete` entry로 link해 외부 검증 가능하게 유지합니다.
//
// 본 round (E32 Stage 1~3):
//   - policy: rotation 주기·hot retention·cold backend 선택 (env override 가능).
//   - builder: hot segment의 entry 들을 모아 segment_hash 계산 + 메타 반환.
//   - archiver: segment 본문을 tar.gz로 직렬화 + sha256 검증.
//   - backend: file:// 기본 (Apache-2.0), s3:// scaffold (BSL 1.1 enterprise — 별 epic).
//
// 참조: docs/design/notes/audit-chain-rotation-design.md (옵션 A).
package rotation

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// ColdBackend 식별자 (env 또는 config에서 단일 단어로 전달).
const (
	ColdBackendFile = "file"
	ColdBackendS3   = "s3"
)

// 환경 변수 이름.
const (
	EnvFrequency       = "ROSSHIELD_AUDIT_ROTATION_FREQUENCY"
	EnvHotRetentionDay = "ROSSHIELD_AUDIT_HOT_RETENTION_DAYS"
	EnvColdBackend     = "ROSSHIELD_AUDIT_COLD_BACKEND"
)

// 기본 값 (D-AR-1 / D-AR-3 / D-AR-5).
const (
	DefaultFrequency    = 30 * 24 * time.Hour // monthly (cron approximation)
	DefaultHotRetention = 365 * 24 * time.Hour
	DefaultColdBackend  = ColdBackendFile
)

// RotationPolicy는 rotation 동작 파라미터입니다.
//
// 모든 필드는 양수여야 합니다. Frequency 0이면 cron 미등록 (manual API only).
// HotRetention 0이면 GC 미수행 (메타데이터만 작성, hot row 보존).
type RotationPolicy struct {
	// Frequency는 cron tick 간격 — manual API 사용 시 무시.
	Frequency time.Duration
	// HotRetention은 rotation 후 hot DB row를 보존하는 기간.
	HotRetention time.Duration
	// ColdBackend는 archive 저장 대상: "file" | "s3".
	ColdBackend string
}

// DefaultPolicy는 권장 default (월 1회 + 1년 hot + file backend).
func DefaultPolicy() RotationPolicy {
	return RotationPolicy{
		Frequency:    DefaultFrequency,
		HotRetention: DefaultHotRetention,
		ColdBackend:  DefaultColdBackend,
	}
}

// LoadPolicyFromEnv는 env override를 적용한 RotationPolicy를 반환합니다.
//
// 누락된 env는 default 그대로 사용. 잘못된 값은 error.
//
//	ROSSHIELD_AUDIT_ROTATION_FREQUENCY: "monthly" | "weekly" | "daily" | "<N>h" (Go time.ParseDuration)
//	ROSSHIELD_AUDIT_HOT_RETENTION_DAYS: 양의 정수 (일 단위)
//	ROSSHIELD_AUDIT_COLD_BACKEND:       "file" | "s3"
func LoadPolicyFromEnv() (RotationPolicy, error) {
	p := DefaultPolicy()

	if v := strings.TrimSpace(os.Getenv(EnvFrequency)); v != "" {
		d, err := parseFrequency(v)
		if err != nil {
			return RotationPolicy{}, fmt.Errorf("rotation: %s: %w", EnvFrequency, err)
		}
		p.Frequency = d
	}

	if v := strings.TrimSpace(os.Getenv(EnvHotRetentionDay)); v != "" {
		days, err := strconv.Atoi(v)
		if err != nil || days <= 0 {
			return RotationPolicy{}, fmt.Errorf("rotation: %s must be positive integer (days), got %q",
				EnvHotRetentionDay, v)
		}
		p.HotRetention = time.Duration(days) * 24 * time.Hour
	}

	if v := strings.TrimSpace(os.Getenv(EnvColdBackend)); v != "" {
		switch v {
		case ColdBackendFile, ColdBackendS3:
			p.ColdBackend = v
		default:
			return RotationPolicy{}, fmt.Errorf("rotation: %s must be one of [file, s3], got %q",
				EnvColdBackend, v)
		}
	}

	if err := p.Validate(); err != nil {
		return RotationPolicy{}, err
	}
	return p, nil
}

// Validate는 정책 일관성 검사입니다.
func (p RotationPolicy) Validate() error {
	if p.Frequency < 0 {
		return fmt.Errorf("rotation: Frequency must be >= 0, got %s", p.Frequency)
	}
	if p.HotRetention < 0 {
		return fmt.Errorf("rotation: HotRetention must be >= 0, got %s", p.HotRetention)
	}
	switch p.ColdBackend {
	case ColdBackendFile, ColdBackendS3:
	default:
		return fmt.Errorf("rotation: unsupported ColdBackend %q (allowed: file, s3)", p.ColdBackend)
	}
	return nil
}

// parseFrequency는 keyword (monthly·weekly·daily) 또는 Go duration 문자열을 파싱합니다.
func parseFrequency(v string) (time.Duration, error) {
	switch strings.ToLower(v) {
	case "monthly":
		return 30 * 24 * time.Hour, nil
	case "weekly":
		return 7 * 24 * time.Hour, nil
	case "daily":
		return 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("invalid frequency %q (use monthly|weekly|daily or Go duration like 720h)", v)
	}
	if d <= 0 {
		return 0, fmt.Errorf("frequency must be positive, got %s", d)
	}
	return d, nil
}
