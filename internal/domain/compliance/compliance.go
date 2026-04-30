// Package compliance는 규제·보안 프레임워크(ISMS-P·ISO 27001·NIST 800-53) 매핑과
// 통제별 status·점수 산출의 도메인 표면을 정의합니다 (E15 Phase 2).
//
// Phase 2 스코프:
//
//   - 모델: ComplianceProfile · FrameworkSnapshot · ControlStatus + ControlDefinition
//   - Service: CreateProfile · GenerateSnapshot · ListProfiles · ListSnapshots
//   - sqliterepo 어댑터 + audit emit (compliance.profile.created · compliance.snapshot.generated)
//   - frameworks.go: embed YAML → ControlDefinition 메모리 캐시
//   - mapping.go: ScanResultView → ControlStatus 집계 + 점수 산정
//
// 도메인 결합 규칙 (P5 + depguard):
//
//	compliance 도메인은 audit·scan·benchmark 패키지를 직접 import하지 않습니다.
//	cmd/* bootstrap이 audit.Service · scan.Service를 어댑팅해 AuditEmitter · ScanReader · AuditReader로 주입.
//	통제 데이터는 git commit YAML(R14-2·R14-9) — runtime fetch 금지.
//
// 결정 R14-2·R14-9 (사용자 합의 2026-04-29).
package compliance

import (
	"context"
	"errors"
	"time"

	"github.com/ssabro/rosshield/internal/platform/storage"
)

// Framework는 지원 프레임워크 enum입니다.
//
// Phase 2는 3종 — 추가는 frameworks/<framework>.yaml 추가 + 본 enum에 const 추가.
type Framework string

const (
	FrameworkISMSP    Framework = "isms-p"
	FrameworkISO27001 Framework = "iso27001-2022"
	FrameworkNIST     Framework = "nist-800-53-rev5"
)

// Status는 ControlStatus의 상태 enum입니다 (§04.2).
//
// 결정 알고리즘 (mapping.go AggregateControlStatuses):
//
//	mappedCheckIDs == ∅                                → Unmapped
//	mapped result == 0                                 → Unmapped
//	모든 mapped result가 not_applicable | skipped       → NotApplicable
//	passCount > 0 && failCount == 0                    → Pass
//	passCount == 0 && failCount > 0                    → Fail
//	그 외 (pass·fail·indeterminate 혼합)                → Partial
//
// 여기서 failCount는 fail + error + indeterminate 합 — 보수적(감사 관점에서 미확정은 실패).
type Status string

const (
	StatusPass          Status = "pass"
	StatusFail          Status = "fail"
	StatusPartial       Status = "partial"
	StatusNotApplicable Status = "not_applicable"
	StatusUnmapped      Status = "unmapped"
)

// ControlDefinition은 framework YAML의 한 통제 정의입니다 (§08.10).
//
// R14-2: 코드·제목·요약(<=200자, 자체 작성) + ReferenceURL만. 표준 본문(원문) 복사 금지.
// MappedCheckIDs는 pack 내 check.code(예: "CIS-1.1.1.1") 목록 — pack_check_id가 아님.
type ControlDefinition struct {
	ID             string   // "ISMS-P:5.1.1" 또는 "ISO27001:A.5.1"
	Title          string   // 한국어 또는 영어 제목 (자체 작성)
	Summary        string   // 짧은 설명 (<=200자, 저작권 안전)
	ReferenceURL   string   // 본문은 공식 사이트 URL
	MappedCheckIDs []string // 매핑된 check.code 목록 (수동 큐레이션, Phase 3에서 LLM 보강 — E17)
}

// ControlStatus는 한 ControlDefinition의 평가 결과입니다 (§04.2).
//
// PassCount/FailCount는 매핑된 check 결과의 분류 카운트 — diagnostics 용도.
// FrameworkSnapshot.Statuses에 그대로 보존됩니다.
type ControlStatus struct {
	ControlID string
	Status    Status
	PassCount int
	FailCount int
	Notes     string
}

// ComplianceProfile는 tenant당 활성 framework 1건입니다 (§04.2).
//
// Customizations는 향후(E16+) 통제 가중치 조정 / 비활성 / 추가 — Phase 2는 skeleton.
type ComplianceProfile struct {
	ID                 string
	TenantID           storage.TenantID
	Framework          Framework
	FrameworkVersion   string
	Enabled            bool
	CustomizationsJSON []byte // ControlCustomization 배열 raw (Phase 2: 빈 "[]")
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// FrameworkSnapshot는 특정 시점의 통제 평가 + audit anchor 묶음입니다 (§08.13).
//
// chain_head_*은 생성 시점 audit chain head — 외부 검증 도구(`fg-verify`)가 본 snapshot의
// 점수가 어느 audit 상태에서 산출되었는지 검증하는 anchor.
// Statuses는 통제 정의가 향후 갱신되어도 snapshot은 불변(R14-9).
type FrameworkSnapshot struct {
	ID                 string
	TenantID           storage.TenantID
	ProfileID          string
	SessionID          string  // 옵션 (특정 ScanSession 기준일 때만 채움)
	OverallScore       float64 // 0.0~1.0
	PassCount          int
	FailCount          int
	PartialCount       int
	NotApplicableCount int
	UnmappedCount      int
	ChainHeadSeq       int64
	ChainHeadHash      string // 64자 lowercase hex
	Statuses           []ControlStatus
	CreatedAt          time.Time
}

// AuditEmitter는 compliance 도메인 변경을 audit chain에 기록하는 콜백입니다 (P5).
//
// 호출 시점:
//
//	CreateProfile      → EmitProfileCreated
//	GenerateSnapshot   → EmitSnapshotGenerated
//
// bootstrap이 audit.Service를 어댑팅해 주입.
type AuditEmitter interface {
	EmitProfileCreated(ctx context.Context, tx storage.Tx, p ComplianceProfile) error
	EmitSnapshotGenerated(ctx context.Context, tx storage.Tx, s FrameworkSnapshot) error
}

// ScanReader는 compliance가 필요한 scan 도메인 read-only 표면입니다 (P5 minimal DTO).
//
// bootstrap이 scan.Service를 어댑팅해 주입 — compliance는 scan 패키지를 직접 import하지 않음.
type ScanReader interface {
	ListResultsForSession(ctx context.Context, tx storage.Tx, sessionID string) ([]ScanResultView, error)
}

// ScanResultView는 ScanReader가 반환하는 minimal DTO입니다.
//
// CheckID는 pack 내 check.code(예: "CIS-1.1.1.1") — ControlDefinition.MappedCheckIDs와 매칭.
// Outcome은 scan 도메인의 5-값 enum string (pass·fail·indeterminate·error·skipped).
type ScanResultView struct {
	CheckID string
	Outcome string
}

// AuditReader는 audit chain head 조회 read-only 표면입니다 (P5 minimal DTO).
//
// bootstrap이 audit.Service를 어댑팅해 주입.
type AuditReader interface {
	Head(ctx context.Context, tx storage.Tx, tenantID storage.TenantID) (HeadView, error)
}

// HeadView는 audit chain head의 compliance 도메인 격리 사본입니다.
//
// Hash는 lowercase hex 64자 (audit.Hash → hex.EncodeToString).
type HeadView struct {
	Seq  int64
	Hash string
}

// CreateProfileRequest는 Service.CreateProfile 입력입니다.
type CreateProfileRequest struct {
	Framework        Framework
	FrameworkVersion string // YAML과 일치 강제 — 불일치 시 ErrFrameworkVersionMismatch.
	Enabled          bool
}

// Service는 compliance 도메인 진입점입니다.
type Service interface {
	// CreateProfile은 framework를 tenant에 활성화합니다.
	// 같은 (tenant, framework) 중복은 ErrProfileExists.
	// FrameworkVersion이 YAML과 다르면 ErrFrameworkVersionMismatch.
	CreateProfile(ctx context.Context, tx storage.Tx, req CreateProfileRequest) (ComplianceProfile, error)

	// GenerateSnapshot은 sessionID 기준 모든 매핑된 control에 대해 ControlStatus 집계 +
	// audit chain head 캡처 + framework_snapshots INSERT + audit emit.
	// profileID가 호출 tenant 소유가 아니면 ErrProfileNotFound.
	GenerateSnapshot(ctx context.Context, tx storage.Tx, profileID, sessionID string) (FrameworkSnapshot, error)

	// ListProfiles는 tenant의 모든 profile을 created_at ASC로 반환합니다.
	ListProfiles(ctx context.Context, tx storage.Tx) ([]ComplianceProfile, error)

	// ListSnapshots는 profile의 snapshot을 created_at DESC로 반환합니다.
	// limit <= 0이면 default 50.
	ListSnapshots(ctx context.Context, tx storage.Tx, profileID string, limit int) ([]FrameworkSnapshot, error)
}

// 공통 에러.
var (
	ErrProfileNotFound          = errors.New("compliance: profile not found")
	ErrProfileExists            = errors.New("compliance: profile already exists for this framework")
	ErrUnknownFramework         = errors.New("compliance: unknown framework")
	ErrFrameworkVersionMismatch = errors.New("compliance: requested framework version does not match embedded YAML")
	ErrSnapshotNotFound         = errors.New("compliance: snapshot not found")
)

// ValidateFramework는 Framework enum 값이 알려진 framework인지 검증합니다.
func ValidateFramework(f Framework) error {
	switch f {
	case FrameworkISMSP, FrameworkISO27001, FrameworkNIST:
		return nil
	default:
		return ErrUnknownFramework
	}
}
