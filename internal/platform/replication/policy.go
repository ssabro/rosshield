// Package replication implements E-MR (Phase 8) — Multi-region HA Stage 1~2.
//
// 본 패키지는 cross-region replication 메타데이터(region·role·LSN 추적)와
// standby-mode middleware(role=standby 인스턴스의 write API 차단)를 제공합니다.
//
// 설계: docs/design/notes/multi-region-ha-design.md (D-MR-1 옵션 A = PG logical
// replication + Route53 DNS, D-MR-3 수동 failover, D-MR-4 standby read-only).
//
// 본 round (Stage 1·2) 범위:
//   - replication.Config + env override
//   - Replica/Failover 도메인 entity + Repository interface
//   - sqliterepo CRUD (sqlite·PG 공통 SQL)
//   - standby-mode HTTP middleware (POST/PUT/PATCH/DELETE 차단)
//   - manual failover handler scaffold (별 파일 replication.go API handler)
//
// 본 round 미진행 (Stage 3~7 carryover):
//   - PG CREATE PUBLICATION / CREATE SUBSCRIPTION 자동 setup
//   - Route53 DNS hook 실 SDK 호출
//   - 자동 failover (heartbeat timeout 기반)
//   - cross-region audit witness fold-in
//
// 도메인 import 가드: 본 패키지는 internal/domain/* 를 import하지 않습니다.
// audit emit은 별 layer(handler)가 audit.Service.Append로 결선.
package replication

import (
	"fmt"
	"os"
	"strings"
)

// Role은 본 인스턴스의 replication 역할입니다.
type Role string

const (
	// RolePrimary는 write 가능한 region입니다. audit chain INSERT + PG logical
	// publication 송신측. default (env 미설정).
	RolePrimary Role = "primary"

	// RoleStandby는 read-only region입니다. PG subscription 수신측. write API
	// 호출 시 standby-mode middleware가 409 Conflict로 차단.
	RoleStandby Role = "standby"
)

// IsValid는 role이 알려진 값인지 확인합니다.
func (r Role) IsValid() bool {
	return r == RolePrimary || r == RoleStandby
}

// Config는 region·role + primary endpoint를 정의하는 부팅 설정입니다.
//
// 모든 필드는 env override 가능 (LoadConfigFromEnv).
// default는 single-region (Region="default", Role=primary, PrimaryEndpoint="") —
// 기존 single-region 배포가 본 코드 도입으로 동작 변경 없음.
type Config struct {
	// Region은 본 인스턴스가 위치한 region 식별자입니다.
	// 예: "us-west-2", "ap-northeast-2", "default".
	// 빈 값은 "default" — single-region 배포 fallback.
	Region string

	// Role은 본 인스턴스의 replication role입니다 (primary | standby).
	// default RolePrimary — single-region single-instance 그대로 동작.
	Role Role

	// PrimaryEndpoint는 standby가 write 요청을 redirect할 primary region의
	// 외부 API base URL입니다 (예: "https://api.lodestar.io").
	// standby-mode middleware가 거부 응답 body에 포함 — 클라이언트가 자동 redirect.
	PrimaryEndpoint string

	// Enabled는 본 replication config가 활성인지 여부입니다.
	// false(default)면 standby-mode middleware가 모든 요청을 통과 — single-region
	// 호환. true면 Role 평가 → standby는 write 차단.
	Enabled bool
}

// DefaultConfig는 single-region 기본값을 반환합니다 (Enabled=false).
func DefaultConfig() Config {
	return Config{
		Region:  "default",
		Role:    RolePrimary,
		Enabled: false,
	}
}

// LoadConfigFromEnv는 환경변수에서 Config를 로드합니다.
//
// env 매핑:
//
//	ROSSHIELD_REPLICATION_ENABLED        — "true"/"1" 이면 Enabled=true
//	ROSSHIELD_REPLICATION_REGION         — Region (default "default")
//	ROSSHIELD_REPLICATION_ROLE           — Role ("primary"|"standby", default "primary")
//	ROSSHIELD_REPLICATION_PRIMARY_ENDPOINT — PrimaryEndpoint (standby 안내용)
//
// 유효성 검증: Role이 알려진 값이 아니면 에러. Enabled=true + Role="" → 에러.
func LoadConfigFromEnv() (Config, error) {
	cfg := DefaultConfig()

	if v := strings.TrimSpace(os.Getenv("ROSSHIELD_REPLICATION_ENABLED")); v != "" {
		switch strings.ToLower(v) {
		case "1", "true", "yes", "on":
			cfg.Enabled = true
		case "0", "false", "no", "off":
			cfg.Enabled = false
		default:
			return Config{}, fmt.Errorf("replication: invalid ROSSHIELD_REPLICATION_ENABLED %q", v)
		}
	}

	if v := strings.TrimSpace(os.Getenv("ROSSHIELD_REPLICATION_REGION")); v != "" {
		cfg.Region = v
	}

	if v := strings.TrimSpace(os.Getenv("ROSSHIELD_REPLICATION_ROLE")); v != "" {
		role := Role(strings.ToLower(v))
		if !role.IsValid() {
			return Config{}, fmt.Errorf("replication: invalid ROSSHIELD_REPLICATION_ROLE %q (allowed: primary|standby)", v)
		}
		cfg.Role = role
	}

	if v := strings.TrimSpace(os.Getenv("ROSSHIELD_REPLICATION_PRIMARY_ENDPOINT")); v != "" {
		cfg.PrimaryEndpoint = v
	}

	if cfg.Enabled && !cfg.Role.IsValid() {
		return Config{}, fmt.Errorf("replication: Enabled=true requires valid Role")
	}

	return cfg, nil
}

// IsStandby는 본 config가 standby 역할인지 반환합니다 (middleware 짧은 체크용).
// Enabled=false면 false (write 통과).
func (c Config) IsStandby() bool {
	return c.Enabled && c.Role == RoleStandby
}
