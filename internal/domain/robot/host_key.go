package robot

import (
	"context"
	"errors"
	"time"

	"github.com/ssabro/rosshield/internal/platform/storage"
)

// HostKeyTrustState는 robot_host_keys.trust_state 값입니다.
//
// trusted: 정상 신뢰 상태. 다음 SSH 접속 시 fingerprint 일치 확인 통과.
// revoked: 운영자가 ResetTrust로 명시 폐기. 다음 first-touch가 새 키를 trusted로 갱신.
type HostKeyTrustState string

const (
	HostKeyTrustStateTrusted HostKeyTrustState = "trusted"
	HostKeyTrustStateRevoked HostKeyTrustState = "revoked"
)

// RobotHostKey는 robot의 TOFU(Trust On First Use) host key 레코드입니다.
//
// design doc `scanrun-ssh-integration-design.md` §5.5 + Stage 1 + D-SCAN-2 권장 default.
// 첫 SSH 접속 시 RecordFirstTouch로 즉시 trusted 상태로 영속, 두 번째 이후는 GetTrustedKey로
// fingerprint 비교. 변경(불일치) 시 즉시 차단(설계서 §06.8 정합).
//
// 같은 (tenant, robot, fingerprint)는 UNIQUE — robot당 다중 키는 history 보존 목적으로 허용.
type RobotHostKey struct {
	ID                string
	TenantID          storage.TenantID
	RobotID           string
	FingerprintSHA256 string // 'SHA256:<base64-no-pad>' (OpenSSH 표준)
	KeyType           string // 'ssh-rsa' | 'ssh-ed25519' | 'ecdsa-sha2-nistp256' 등
	KeyBlob           []byte // ssh.PublicKey.Marshal() 결과 — 재구성용
	FirstSeenAt       time.Time
	LastVerifiedAt    time.Time
	TrustState        HostKeyTrustState
}

// HostKeyRepo는 robot_host_keys 테이블 어댑터 표면입니다 (Service.host_key 부분).
//
// Stage 1에서는 도메인만 정의. Stage 2(KnownHostsManager)가 본 인터페이스를 통해 TOFU 결정.
type HostKeyService interface {
	// RecordFirstTouch는 (tenantID, robotID, fingerprint) 키를 처음 본 것으로 기록합니다.
	//
	// 멱등: 같은 (tenant, robot, fingerprint) 호출이면 LastVerifiedAt만 갱신, 같은 row 반환.
	// trust_state는 'trusted'로 INSERT (또는 'revoked'에서 다시 'trusted'로 복구).
	// audit 'robot.host_key.first_touched' emit.
	RecordFirstTouch(ctx context.Context, tx storage.Tx, req RecordFirstTouchRequest) (RobotHostKey, error)

	// GetTrustedKey는 (tenantID, robotID)의 현재 trusted host key를 반환합니다.
	//
	// 같은 robot의 trusted row가 0건이면 storage.ErrNotFound — 호출자(KnownHostsManager)가 first-touch로 진입.
	// 다중 trusted row(이론적으로 RecordFirstTouch가 같은 fingerprint만 trusted 보장)는 가장 최신 LastVerifiedAt 기준.
	GetTrustedKey(ctx context.Context, tx storage.Tx, robotID string) (RobotHostKey, error)

	// ResetTrust는 (tenantID, robotID)의 모든 trusted row를 revoked로 marking합니다.
	//
	// 운영자 명시 reset 흐름. 다음 first-touch가 새 키를 trusted로 등록.
	// audit 'robot.host_key.reset' emit. 영향 row 수 반환.
	ResetTrust(ctx context.Context, tx storage.Tx, robotID string) (int, error)
}

// RecordFirstTouchRequest는 RecordFirstTouch 입력입니다.
type RecordFirstTouchRequest struct {
	RobotID           string
	FingerprintSHA256 string
	KeyType           string
	KeyBlob           []byte
}

// HostKeyAuditEmitter는 host key 변경에 대한 audit 콜백입니다 (P5 격리 — robot이 audit 직접 import 안 함).
type HostKeyAuditEmitter interface {
	// EmitHostKeyFirstTouched는 'robot.host_key.first_touched' 엔트리를 audit에 append합니다.
	EmitHostKeyFirstTouched(ctx context.Context, tx storage.Tx, k RobotHostKey) error

	// EmitHostKeyChanged는 'robot.host_key.changed' 엔트리를 audit에 append합니다 (Stage 2 — 차단 발생 시 emit).
	EmitHostKeyChanged(ctx context.Context, tx storage.Tx, robotID string, tenantID storage.TenantID, oldFingerprint, newFingerprint string) error

	// EmitHostKeyReset는 'robot.host_key.reset' 엔트리를 audit에 append합니다.
	EmitHostKeyReset(ctx context.Context, tx storage.Tx, robotID string, tenantID storage.TenantID, revokedCount int) error
}

// host_key 도메인 에러.
var (
	ErrHostKeyEmptyRobotID     = errors.New("robot: host key RobotID is required")
	ErrHostKeyEmptyFingerprint = errors.New("robot: host key FingerprintSHA256 is required")
	ErrHostKeyEmptyKeyType     = errors.New("robot: host key KeyType is required")
	ErrHostKeyEmptyKeyBlob     = errors.New("robot: host key KeyBlob is required")
	ErrHostKeyMismatch         = errors.New("robot: host key fingerprint mismatch — TOFU 차단 (운영자 ResetTrust 필요)")
)
