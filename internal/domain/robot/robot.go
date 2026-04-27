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

// SSHTester는 SSH 연결 테스트 표면입니다 (Stage E mock interface).
//
// 실제 구현은 E6(`internal/platform/sshpool` + `internal/domain/scan`)에서.
// Phase 1 E5는 Service.TestConnection이 이 인터페이스에 위임하는 결선만 제공 — 구현은 테스트의 mock.
//
// 호출자(미들웨어·CLI)가 SSH client를 만든 직후 즉시 폐기 책임. material은 결과 후 zero-out 권장.
type SSHTester interface {
	TestConnection(ctx context.Context, host string, port int, authType AuthType, material CredentialMaterial) error
}

// AuditEmitter는 도메인 변경을 감사 로그에 기록하는 콜백입니다 (P5 — audit 도메인 직접 import 회피).
//
// Stage A: EmitFleetCreated. Stage C: EmitRobotCreated/EmitRobotDeleted/EmitCredentialRotated.
type AuditEmitter interface {
	// EmitFleetCreated는 fleet.created 엔트리를 audit에 append합니다.
	// tx는 fleet 생성과 같은 Tx — 같은 commit·rollback에 묶임.
	EmitFleetCreated(ctx context.Context, tx storage.Tx, f Fleet) error

	// EmitRobotCreated는 robot.created 엔트리를 audit에 append합니다.
	// Robot+Credential은 같은 Tx에 생성되므로 단일 audit 엔트리로 묶음.
	EmitRobotCreated(ctx context.Context, tx storage.Tx, r Robot, credentialID string) error

	// EmitRobotDeleted는 robot.deleted 엔트리를 audit에 append합니다 (soft delete).
	EmitRobotDeleted(ctx context.Context, tx storage.Tx, robotID string, tenantID storage.TenantID) error

	// EmitCredentialRotated는 credential.rotated 엔트리를 audit에 append합니다 (R3-3).
	EmitCredentialRotated(ctx context.Context, tx storage.Tx, robotID, oldCredID, newCredID string, tenantID storage.TenantID) error
}

// Service는 robot 도메인 진입점입니다.
//
// Stage A는 Fleet CRUD. Stage C는 Robot·Credential CRUD가 추가됩니다.
type Service interface {
	// CreateFleet는 새 Fleet을 생성하고 audit를 emit합니다.
	// ctx의 TenantID로 격리. 이름 중복(같은 tenant 내, 살아있는 fleet) 시 ErrFleetNameDuplicate.
	CreateFleet(ctx context.Context, tx storage.Tx, req CreateFleetRequest) (Fleet, error)

	// GetFleet은 ID로 fleet을 조회합니다 (deleted_at IS NULL 만). 없으면 storage.ErrNotFound.
	GetFleet(ctx context.Context, tx storage.Tx, id string) (Fleet, error)

	// ListFleets는 tenant의 활성 fleet을 모두 반환합니다 (deleted_at IS NULL).
	ListFleets(ctx context.Context, tx storage.Tx) ([]Fleet, error)

	// CreateRobot는 새 Robot + Credential을 한 Tx에 생성하고 audit를 emit합니다.
	// req.Material은 KEK로 wrap된 후 폐기됩니다.
	// FleetID 부재·deleted 시 ErrFleetNotFound. 이름 또는 (host,port) 중복 시 각각 Err...Duplicate.
	CreateRobot(ctx context.Context, tx storage.Tx, req CreateRobotRequest) (CreateRobotResult, error)

	// GetRobot은 ID로 robot을 조회합니다 (deleted_at IS NULL 만). 없으면 storage.ErrNotFound.
	GetRobot(ctx context.Context, tx storage.Tx, id string) (Robot, error)

	// ListRobots는 tenant의 활성 robot을 반환합니다.
	// fleetID="" 면 tenant 전체, 그 외엔 해당 fleet으로 필터.
	ListRobots(ctx context.Context, tx storage.Tx, fleetID string) ([]Robot, error)

	// DeleteRobot은 robot을 soft delete하고 연결된 credential을 revoke합니다 + audit emit.
	// 이미 삭제된 robot은 storage.ErrNotFound (멱등 아님 — Phase 1은 명시적 한 번만).
	DeleteRobot(ctx context.Context, tx storage.Tx, id string) error

	// GetCredentialMaterial은 robot의 credential을 unwrap하여 평문 자격증명을 반환합니다.
	// SSH client(E6)가 사용 시점에만 호출. 호출자는 결과를 사용 후 즉시 폐기 권장.
	GetCredentialMaterial(ctx context.Context, tx storage.Tx, robotID string) (CredentialMaterial, error)

	// RotateCredential은 새 credential을 생성하고 robot의 credential_id를 갱신합니다 + audit emit.
	// 이전 credential은 revoked_at으로 soft delete (감사 추적 보존).
	RotateCredential(ctx context.Context, tx storage.Tx, req RotateCredentialRequest) (RotateCredentialResult, error)

	// TestConnection은 robot의 host:port에 credential로 SSH 연결을 시도합니다 (Stage E).
	// 내부적으로 GetCredentialMaterial로 unwrap → SSHTester에 위임 → 결과 반환.
	// SSHTester가 nil이면 ErrSSHTesterNotConfigured.
	TestConnection(ctx context.Context, tx storage.Tx, robotID string) error
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

// AuthType은 SSH 인증 방식입니다 (§04.2).
//
// Phase 1은 password·privateKey만 지원 — `agent` 옵션은 §06.5 "ssh agent forward 없이 직접 사용" 정책 위반 위험으로 보류.
type AuthType string

const (
	AuthTypePassword   AuthType = "password"
	AuthTypePrivateKey AuthType = "privateKey"
)

// Robot은 스캔 대상 로봇입니다 (§04.2).
type Robot struct {
	ID           string // "ro_<ULID>"
	TenantID     storage.TenantID
	FleetID      string
	CredentialID string
	Name         string
	Host         string
	Port         int
	AuthType     AuthType
	OSDistro     string // "ubuntu-24.04" 등
	ROSDistro    string // "jazzy" 등
	Tags         []string
	Role         string // "mobile" | "manipulator" | custom
	Criticality  Criticality
	CreatedAt    time.Time
	UpdatedAt    time.Time
	LastScanAt   *time.Time
	DeletedAt    *time.Time // soft delete (R3-5)
}

// CreateRobotRequest는 Service.CreateRobot 입력입니다.
//
// Material은 평문 자격증명 — wrap 후 즉시 폐기됩니다 (메모리 외 영속화 X).
// Port=0이면 default 22, AuthType="" 이면 PrivateKey, Criticality="" 이면 Medium.
type CreateRobotRequest struct {
	FleetID     string
	Name        string
	Host        string
	Port        int
	AuthType    AuthType
	Material    CredentialMaterial
	OSDistro    string
	ROSDistro   string
	Tags        []string
	Role        string
	Criticality Criticality
}

// CreateRobotResult는 Service.CreateRobot 출력입니다.
//
// Credential은 메타데이터만 포함 (EncryptedPayload는 DB 저장본, 평문 자격증명 노출 X).
type CreateRobotResult struct {
	Robot      Robot
	Credential Credential
}

// RotateCredentialRequest는 Service.RotateCredential 입력입니다 (R3-3 — 수동 API만).
type RotateCredentialRequest struct {
	RobotID  string
	Material CredentialMaterial
}

// RotateCredentialResult는 새 credentialID와 이전 credentialID(감사용)를 반환합니다.
type RotateCredentialResult struct {
	NewCredentialID string
	OldCredentialID string
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

	// Robot errors (Stage C).
	ErrRobotEmptyName        = errors.New("robot: Name is required")
	ErrRobotNameTooLong      = errors.New("robot: Name exceeds 200 characters")
	ErrRobotEmptyHost        = errors.New("robot: Host is required")
	ErrRobotInvalidPort      = errors.New("robot: Port must be 1..65535")
	ErrRobotEmptyFleet       = errors.New("robot: FleetID is required")
	ErrRobotInvalidAuthType  = errors.New("robot: AuthType must be password or privateKey")
	ErrRobotInvalidCritical  = errors.New("robot: Criticality must be one of low|medium|high|critical")
	ErrFleetNotFound         = errors.New("robot: Fleet not found")
	ErrRobotNameDuplicate    = errors.New("robot: Name already exists in this fleet")
	ErrRobotHostPortConflict = errors.New("robot: Host:Port already exists in this tenant")

	// CSV import errors (Stage D).
	ErrCSVEmpty               = errors.New("robot: CSV input is empty")
	ErrCSVMissingHeader       = errors.New("robot: CSV header missing required columns (name, host, username, authType)")
	ErrCSVUnknownHeader       = errors.New("robot: CSV header contains unknown column")
	ErrCSVCredentialAmbiguous = errors.New("robot: CSV row has both password and privateKeyPem")
	ErrCSVCredentialMissing   = errors.New("robot: CSV row has neither password nor privateKeyPem")

	// TestConnection errors (Stage E).
	ErrSSHTesterNotConfigured = errors.New("robot: SSHTester not configured (E6 결선 전에는 사용 불가)")
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
