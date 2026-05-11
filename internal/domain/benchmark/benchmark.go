// Package benchmark는 벤치마크 팩 도메인의 공개 표면을 정의합니다.
//
// Phase 1 스코프(§E4): Pack 로더·서명 검증·Self-Test 러너·생명주기 FSM.
// 평가 엔진(scan)과는 분리됩니다 — benchmark는 "체크 정의를 어떻게 안전하게 로드/검증하느냐",
// 평가 실행은 scan 도메인(E5+).
//
// 외부 자산(pack.yaml, checks/*.yaml)은 P8 원칙대로 서명된 콘텐츠 — Ed25519 manifest로 무결성 검증.
//
// audit 결합은 P5 격리: `AuditEmitter` 인터페이스를 통해 cmd/* bootstrap이 audit.Service 어댑터 주입.
package benchmark

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/ssabro/rosshield/internal/platform/storage"
)

// Severity는 체크 심각도입니다.
type Severity string

const (
	SeverityLow      Severity = "low"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

// State는 팩 라이프사이클 상태입니다 (C7 결정).
//
// 전이 그래프:
//
//	Installed → Staged → Active ⇄ Inactive → Archived → Removed
//
// 직접 Active로 갈 수 없음 — 항상 Staged 거쳐야 함 (검증 강제).
type State string

const (
	StateInstalled State = "installed"
	StateStaged    State = "staged"
	StateActive    State = "active"
	StateInactive  State = "inactive"
	StateArchived  State = "archived"
	StateRemoved   State = "removed"
)

// Pack은 설치된 벤치마크 팩 메타입니다 (§4.2 BenchmarkPack).
//
// `TenantID = "system"`인 팩은 cross-tenant 공유 (§4.2 명시).
// `PackKey = "<vendor>-<name>-<version>"`는 사람 친화적 식별자, DB의 ID는 `pk_<ULID>`.
type Pack struct {
	ID            string
	TenantID      storage.TenantID
	PackKey       string
	Name          string
	Version       string
	Vendor        string
	Description   string
	SchemaVersion int
	ManifestHash  [32]byte
	SignerKeyID   string
	InstalledAt   time.Time
	Checks        []Check // Pack과 함께 로드된 모든 체크
}

// HashSize는 sha256 출력 크기입니다 (audit.HashSize와 동일하지만 도메인 격리 위해 별도 정의).
const HashSize = 32

// Check는 단일 체크 정의입니다.
//
// EvaluationRule은 화이트리스트 AST의 JSON 직렬화 (Stage C에서 검증·해석).
// AuditCommand는 단순 SSH 명령 (Phase 1) — 후속 multi-step 시퀀스로 확장 예정.
type Check struct {
	ID             string // ck_<ULID>
	PackID         string
	CheckID        string // 'CIS-1.1.1.1'
	Title          string
	Description    string
	Severity       Severity
	AuditCommand   string
	EvaluationRule json.RawMessage // AST JSON (Stage C에서 검증)
	Rationale      string
	FixGuidance    string
}

// AuditEmitter는 도메인 변경을 감사 로그에 기록하는 콜백입니다 (P5 격리 — tenant 패턴 동일).
type AuditEmitter interface {
	// EmitPackInstalled는 pack.installed 이벤트를 audit에 append합니다.
	EmitPackInstalled(ctx context.Context, tx storage.Tx, p Pack, actorID string) error

	// EmitPackLifecycleChanged는 pack.activated/deactivated/archived/removed 등을 emit합니다.
	EmitPackLifecycleChanged(ctx context.Context, tx storage.Tx, packID string, from, to State, actorID, reason string) error
}

// Service는 벤치마크 도메인 진입점입니다.
type Service interface {
	// InstallPack은 tar.gz 바이트를 LoadPackFromTar로 검증한 뒤 DB에 INSERT하고
	// lifecycle 첫 row(installed) + audit emit까지 한 Tx에 처리합니다.
	//
	// tenantID="system"이면 cross-tenant 공유 팩 (§4.2). 그 외에는 tenant scope.
	// 동일 (tenant_id, pack_key)가 이미 있으면 ErrPackAlreadyInstalled.
	InstallPack(ctx context.Context, tx storage.Tx, tenantID storage.TenantID,
		tarGzBytes []byte, publicKey []byte, signerKeyID, actorID string) (Pack, error)

	// GetPackByKey는 (tenant_id, pack_key)로 Pack 메타+체크를 조회합니다. 없으면 storage.ErrNotFound.
	GetPackByKey(ctx context.Context, tx storage.Tx, tenantID storage.TenantID, packKey string) (Pack, error)

	// GetPackByID는 packID(pk_<ULID>)로 Pack 메타+체크를 조회합니다. 없으면 storage.ErrNotFound.
	//
	// scanrun 결선에서 CreateScan handler가 packID로 pack의 checks를 fetch하기 위해 사용.
	// tenant 검증은 caller 책임 (시스템 tenant pack도 caller가 cross-tenant 조회 가능).
	GetPackByID(ctx context.Context, tx storage.Tx, packID string) (Pack, error)

	// ListPacks는 tenant의 모든 Pack 메타(체크 미포함)를 반환합니다.
	ListPacks(ctx context.Context, tx storage.Tx, tenantID storage.TenantID) ([]Pack, error)

	// CurrentState는 packID의 가장 최근 lifecycle 상태를 반환합니다.
	CurrentState(ctx context.Context, tx storage.Tx, packID string) (State, error)

	// TransitionPack은 from → to 검증 후 새 lifecycle row INSERT + audit emit.
	// 불법 전이는 ErrIllegalTransition.
	TransitionPack(ctx context.Context, tx storage.Tx, packID string, to State, actorID, reason string) error
}

// 추가 sentinel 에러.
var ErrPackAlreadyInstalled = errors.New("benchmark: pack already installed for this tenant")

// 공통 에러.
var (
	ErrInvalidYAML       = errors.New("benchmark: invalid YAML")
	ErrSchemaViolation   = errors.New("benchmark: pack does not match schema")
	ErrMissingPackYAML   = errors.New("benchmark: pack.yaml not found in archive")
	ErrUnknownAPIVersion = errors.New("benchmark: unknown apiVersion")
	ErrUnknownKind       = errors.New("benchmark: unknown kind")
	ErrEmptyPackKey      = errors.New("benchmark: pack key (vendor-name-version) is empty")
	ErrDuplicateCheckID  = errors.New("benchmark: duplicate check ID within pack")
	ErrInvalidSeverity   = errors.New("benchmark: invalid severity value")
)

// AllowedAPIVersion·Kind는 현재 지원하는 단일 값입니다.
const (
	APIVersion = "rosshield.io/v1"
	KindPack   = "Pack"
	KindCheck  = "Check"
)
