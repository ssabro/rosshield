// Package robot은 Fleet·Robot·Credential 도메인의 공개 표면을 정의합니다.
//
// Phase 1 스코프(§E5): Fleet·Robot·Credential을 한 패키지에 묶음 — tenant 패키지(E3) 패턴 답습.
// 도메인 경계 P5는 다른 도메인 간 격리를 강제하지, 한 도메인 내부 분리는 강제하지 않습니다.
//
// audit 도메인과의 결합: robot 도메인은 `audit` 패키지를 직접 import하지 않습니다 (P5 + depguard).
// 대신 `AuditEmitter` 인터페이스를 받아 cmd/* bootstrap이 audit.Service 어댑터를 주입합니다.
//
// Stage 분할 (e5-robot-fleet-deepdive.md §10):
//
//	Stage A — Fleet 도메인 + 마이그레이션 0008 + ID 접두사 + T1 (현재).
//	Stage B — Credential KEK/DEK + 마이그레이션 0009 + T3.
//	Stage C — Robot CRUD + 마이그레이션 0010 + T2·T4·T7.
//	Stage D — CSV import + T6.
//	Stage E — TestConnection mock + cross-tenant fuzzer + T5.
package robot

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/ssabro/rosshield/internal/platform/storage"
)

// Level은 스캔 레벨입니다 (§07.3, CIS L1/L2).
type Level string

const (
	LevelL1 Level = "L1"
	LevelL2 Level = "L2"
)

// Criticality는 자산 중요도입니다 (§04.2).
type Criticality string

const (
	CriticalityLow      Criticality = "low"
	CriticalityMedium   Criticality = "medium"
	CriticalityHigh     Criticality = "high"
	CriticalityCritical Criticality = "critical"
)

// FleetPolicy는 Fleet에 적용되는 기본 스캔 정책입니다 (R3-4 — e5 deepdive §4).
//
// Robot은 이 정책을 상속하되 개별 필드를 override할 수 있습니다 (Robot.Criticality 등).
// Phase 1은 4 필드만:
//
//	DefaultBaselineID — 기본 적용 팩 ID (E4 benchmark)
//	DefaultLevel      — "L1" 또는 "L2"
//	DefaultCriticality — robot 미설정 시 기본값
//	ScanSchedule      — Scheduler(E1.T9 cronsched) spec, "" = 수동만
type FleetPolicy struct {
	DefaultBaselineID  string      `json:"defaultBaselineId,omitempty"`
	DefaultLevel       Level       `json:"defaultLevel,omitempty"`
	DefaultCriticality Criticality `json:"defaultCriticality,omitempty"`
	ScanSchedule       string      `json:"scanSchedule,omitempty"` // cron spec, "" = manual
}

// Fleet은 정책 그룹으로 묶인 로봇 집합입니다 (§04.2).
type Fleet struct {
	ID          string // "fl_<ULID>"
	TenantID    storage.TenantID
	Name        string
	Description string
	Policy      FleetPolicy
	CreatedAt   time.Time
	UpdatedAt   time.Time
	DeletedAt   *time.Time // soft delete (R3-5), nil = 활성
}

// CreateFleetRequest는 Service.CreateFleet 입력입니다.
//
// Policy의 모든 필드는 optional — 빈 값은 default 적용 정책 없음(추후 robot이 매번 명시).
// Phase 1은 default policy 강제 안 함 — 점진적 도입.
type CreateFleetRequest struct {
	Name        string
	Description string
	Policy      FleetPolicy
}

// AuditEmitter는 도메인 변경을 감사 로그에 기록하는 콜백입니다 (P5 — audit 도메인 직접 import 회피).
//
// Stage A는 EmitFleetCreated만. 후속 Stage에서 Robot·Credential 메서드 추가.
type AuditEmitter interface {
	// EmitFleetCreated는 fleet.created 엔트리를 audit에 append합니다.
	// tx는 fleet 생성과 같은 Tx — 같은 commit·rollback에 묶임.
	EmitFleetCreated(ctx context.Context, tx storage.Tx, f Fleet) error
}

// Service는 robot 도메인 진입점입니다.
//
// Stage A는 Fleet CRUD만. 후속 Stage에서 Robot·Credential 메서드가 추가됩니다.
type Service interface {
	// CreateFleet는 새 Fleet을 생성하고 audit를 emit합니다.
	// ctx의 TenantID로 격리. 이름 중복(같은 tenant 내, 살아있는 fleet) 시 ErrFleetNameDuplicate.
	CreateFleet(ctx context.Context, tx storage.Tx, req CreateFleetRequest) (Fleet, error)

	// GetFleet은 ID로 fleet을 조회합니다 (deleted_at IS NULL 만). 없으면 storage.ErrNotFound.
	GetFleet(ctx context.Context, tx storage.Tx, id string) (Fleet, error)

	// ListFleets는 tenant의 활성 fleet을 모두 반환합니다 (deleted_at IS NULL).
	ListFleets(ctx context.Context, tx storage.Tx) ([]Fleet, error)
}

// CredentialType은 SSH 자격증명 유형입니다 (§04.2).
type CredentialType string

const (
	CredentialTypePassword   CredentialType = "password"
	CredentialTypePrivateKey CredentialType = "privateKey"
)

// EncryptionAlgorithm은 현재 알고리즘 식별자입니다.
const EncryptionAlgorithm = "AES-256-GCM"

// EncryptionVersion은 EncryptionMeta 포맷 버전입니다.
// Phase 1 = 1 (KEK→DEK 2계층). Phase 2+에 Tenant Key 추가 시 2.
const EncryptionVersion = 1

// CredentialMaterial은 평문 자격증명입니다 (메모리 전용 — 절대 영속화 X).
//
// SSH client(E6)에서 사용 시점에만 unwrap → 사용 후 즉시 폐기 (defer로 zero-out 권장).
type CredentialMaterial struct {
	Type                 CredentialType `json:"type"`
	Username             string         `json:"username"`
	Password             string         `json:"password,omitempty"`             // type=password
	PrivateKeyPEM        string         `json:"privateKeyPem,omitempty"`        // type=privateKey
	PrivateKeyPassphrase string         `json:"privateKeyPassphrase,omitempty"` // 옵션
}

// EncryptionMeta는 Credential.encrypted_payload의 wrap 메타데이터입니다 (§06.6, R3-1).
//
// 모든 필드 필수(omitempty 안 함) — 변조 검증 단순화.
type EncryptionMeta struct {
	Version      int       `json:"version"`      // EncryptionVersion (1)
	Algorithm    string    `json:"algorithm"`    // EncryptionAlgorithm
	KEKKeyID     string    `json:"kekKeyId"`     // "kek_<sha256(KEK)[:8] hex>"
	AAD          string    `json:"aad"`          // "t=<tenantID>;c=<credentialID>;v=1"
	DEKNonce     []byte    `json:"dekNonce"`     // 12B (DEK wrap용)
	PayloadNonce []byte    `json:"payloadNonce"` // 12B (payload encrypt용)
	WrappedDEK   []byte    `json:"wrappedDek"`   // KEK로 wrap된 32B DEK + 16B GCM tag
	CreatedAt    time.Time `json:"createdAt"`
}

// Credential은 암호화된 자격증명 레코드입니다 (§04.2).
//
// EncryptedPayload는 DEK로 암호화된 CredentialMaterial JSON.
// EncryptionMeta는 KEK·DEK·nonce·AAD를 포함 — KEK 부재 시 unwrap 불가능.
type Credential struct {
	ID               string // "cr_<ULID>"
	TenantID         storage.TenantID
	Type             CredentialType
	EncryptedPayload []byte
	EncryptionMeta   EncryptionMeta
	RotationDueAt    *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
	RevokedAt        *time.Time // soft delete (R3-5)
}

// 공통 에러.
var (
	ErrFleetEmptyName       = errors.New("robot: fleet Name is required")
	ErrFleetNameTooLong     = errors.New("robot: fleet Name exceeds 200 characters")
	ErrFleetNameDuplicate   = errors.New("robot: fleet Name already exists in this tenant")
	ErrFleetInvalidLevel    = errors.New("robot: fleet Policy.DefaultLevel must be L1 or L2 if set")
	ErrFleetInvalidCritical = errors.New("robot: fleet Policy.DefaultCriticality must be one of low|medium|high|critical if set")

	// Credential errors (Stage B).
	ErrKEKInvalidLength      = errors.New("robot: KEK file must be exactly 32 bytes")
	ErrKEKFilePermissions    = errors.New("robot: KEK file permissions too permissive (require 0600)")
	ErrCredentialUnknownType = errors.New("robot: Credential Type must be password or privateKey")
	ErrCredentialEmptyUser   = errors.New("robot: Credential Username is required")
	ErrCredentialDecrypt     = errors.New("robot: failed to decrypt credential (key mismatch or tampered)")
	ErrCredentialMetaVersion = errors.New("robot: EncryptionMeta.Version unsupported")
)

// MarshalPolicy는 FleetPolicy를 DB 저장용 canonical JSON으로 직렬화합니다.
// 키는 알파벳순(json.Marshal 기본), 빈 필드는 omitempty 적용.
func MarshalPolicy(p FleetPolicy) ([]byte, error) {
	return json.Marshal(p)
}

// UnmarshalPolicy는 DB의 JSON을 FleetPolicy로 역직렬화합니다.
// 빈 문자열 또는 "{}"는 zero-value 반환.
func UnmarshalPolicy(raw []byte) (FleetPolicy, error) {
	if len(raw) == 0 || string(raw) == "{}" {
		return FleetPolicy{}, nil
	}
	var p FleetPolicy
	if err := json.Unmarshal(raw, &p); err != nil {
		return FleetPolicy{}, err
	}
	return p, nil
}
