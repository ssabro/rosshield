// Package intake는 첫 paying customer onboarding intake 도메인의 공개 표면을 정의합니다.
//
// 책임 (R1 Stage 1):
//
//   - CustomerIntake entity: 운영자가 회수한 customer-info-template.md를 영속화.
//   - Service interface: Create / Get / List / Accept / Reject 5종 메서드 (상태 머신).
//   - 도메인 에러 sentinel + validation helper.
//
// 본 stage는 도메인 + 마이그레이션 0030 + sqliterepo만 — handler·auto-provisioning은
// Stage 2~4에서 결선합니다.
//
// 상태 머신:
//
//	pending(default) ──Accept──▶ accepted (immutable)
//	                ──Reject──▶ rejected (immutable)
//
// 한 번 accepted/rejected가 된 row는 다시 상태 전환 불가 — Service 계층에서 강제
// (P9 불변성 일관 — accepted_at/rejected_at은 한 번 채워지면 변경 금지).
//
// 도메인 결합 규칙 (P5):
//
//	intake 도메인은 tenant·audit 패키지를 직접 import 하지 않습니다.
//	Stage 3 auto-provisioning 결선 시 cmd/* bootstrap이 tenant.Service.Create를
//	intake.Service.Accept와 같은 Tx로 묶어 호출 — 도메인 간 직접 의존 없음.
//
// 멀티테넌시 (P4):
//
//	본 도메인은 *tenant 생성 전* 단계 데이터입니다 — TenantID는 NULL 허용.
//	Accept 시점에 tenant.Create 동시 실행 후 채움. cross-tenant 격리는 application
//	layer에서 담당 (운영자 admin 전역 권한 가정 — Stage 2 handler에서 결정).
//
// 참조: docs/design/notes/customer-onboarding-design.md §6.1 R1 + §7 Stage 1.
package intake

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"time"

	"github.com/ssabro/rosshield/internal/platform/storage"
)

// IntakeStatus는 intake row의 상태 머신입니다.
//
//   - StatusPending  (default): 운영자 검토 대기. Accept/Reject 양 방향 전환 가능.
//   - StatusAccepted (terminal): tenant 생성 + admin invite + license 발급 완료. 변경 금지.
//   - StatusRejected (terminal): 운영자가 거부. 변경 금지.
type IntakeStatus string

const (
	StatusPending  IntakeStatus = "pending"
	StatusAccepted IntakeStatus = "accepted"
	StatusRejected IntakeStatus = "rejected"
)

// IsValidStatus는 s가 알려진 IntakeStatus enum인지 반환합니다.
func IsValidStatus(s IntakeStatus) bool {
	switch s {
	case StatusPending, StatusAccepted, StatusRejected:
		return true
	default:
		return false
	}
}

// PlanRequest는 customer가 신청한 SKU 분류입니다 (design doc §3.1 enum).
//
// 운영자가 검증·승인하는 hint 값이며, 실제 license edition은 Accept 시점에 운영자가
// 별 결정 가능 (Stage 3 auto-provisioning에서 license token 발급).
type PlanRequest string

const (
	PlanCommunity  PlanRequest = "community"
	PlanPro        PlanRequest = "pro"
	PlanEnterprise PlanRequest = "enterprise"
)

// IsValidPlanRequest는 p가 알려진 PlanRequest enum인지 반환합니다.
func IsValidPlanRequest(p PlanRequest) bool {
	switch p {
	case PlanCommunity, PlanPro, PlanEnterprise:
		return true
	default:
		return false
	}
}

// CustomerIntake는 customer onboarding intake 1건입니다.
//
// 한 row의 lifecycle:
//
//  1. 운영자가 customer로부터 customer-info-template.md (yaml) 회수 → CreateIntake
//     호출 → status=pending 으로 INSERT.
//  2. 운영자가 admin UI에서 row를 검토 → AcceptIntake (tenant 생성 + ...) 또는
//     RejectIntake (rejection_reason 명시) 호출.
//  3. accepted/rejected 진입 후 row는 영구 보존 (audit 추적용 — P9 일관).
//
// TenantID는 status=accepted 시점에만 채워짐 (Stage 3 결선 — 본 stage는 NULL 허용).
// AcceptedByUserID는 운영자(rosshield admin) user ID — 누가 accept 했는지 감사 추적.
type CustomerIntake struct {
	ID                  string           // "ci_<ULID>"
	TenantID            storage.TenantID // 비어있으면 미생성 (status=pending 또는 rejected). accept 후 채움.
	OrganizationName    string
	PrimaryContactEmail string // lowercase normalized
	PrimaryContactName  string
	PlanRequest         PlanRequest
	IntendedUse         string // 자유 텍스트 (use case 설명)
	Status              IntakeStatus
	CreatedAt           time.Time
	AcceptedAt          *time.Time // status=accepted 시점에만 채움
	AcceptedByUserID    *string    // 운영자 user ID (감사 추적)
	RejectedAt          *time.Time // status=rejected 시점에만 채움
	RejectionReason     *string    // 운영자가 명시한 거부 사유
}

// CreateIntakeRequest는 Service.CreateIntake 입력입니다.
//
// 운영자가 customer-info-template.md (yaml) → JSON 변환 후 본 struct로 매핑.
// validation 규칙:
//
//   - OrganizationName: 비공백.
//   - PrimaryContactEmail: RFC5322 형식 검증 + lowercase normalize.
//   - PrimaryContactName: 비공백.
//   - PlanRequest: PlanCommunity / PlanPro / PlanEnterprise 중 하나.
//   - IntendedUse: 비공백 (자유 텍스트 — 길이 제한 없음, 운영자가 검토).
type CreateIntakeRequest struct {
	OrganizationName    string
	PrimaryContactEmail string
	PrimaryContactName  string
	PlanRequest         PlanRequest
	IntendedUse         string
}

// AcceptIntakeRequest는 Service.AcceptIntake 입력입니다.
//
// 운영자(rosshield admin) user ID + 옵션으로 생성된 tenant ID (Stage 3 auto-provisioning
// 결선 시 채움 — 본 stage는 TenantID 채움이 옵션, 미채움이면 tenant 미생성 정책).
type AcceptIntakeRequest struct {
	IntakeID         string
	AcceptedByUserID string           // 운영자 admin user ID
	TenantID         storage.TenantID // 옵션 — Stage 3 결선 시 tenant.Create 결과로 채움. 본 stage는 비어있어도 OK.
}

// RejectIntakeRequest는 Service.RejectIntake 입력입니다.
//
// RejectionReason은 비공백 — 운영자가 customer에 회신할 사유 (audit 추적용).
type RejectIntakeRequest struct {
	IntakeID         string
	RejectedByUserID string // 운영자 admin user ID (audit 추적용 — 본 stage는 강제 X, Stage 2~3 정책)
	RejectionReason  string
}

// ListIntakesFilter는 Service.ListIntakes 필터입니다.
//
// Status가 빈 값이면 모든 상태 반환. Limit <= 0이면 default 50.
type ListIntakesFilter struct {
	Status IntakeStatus // 빈 값이면 필터 X
	Limit  int          // <= 0 이면 default 50
}

// Service는 intake 도메인 진입점입니다 (R1 Stage 1).
//
// 본 stage는 5종 메서드만. handler 결선·auto-provisioning·license 발급은 Stage 2~4.
type Service interface {
	// CreateIntake는 새 intake row를 status=pending으로 INSERT 합니다.
	//
	// validation 위반 시 ErrEmptyOrganization / ErrInvalidEmail / ErrEmptyContactName /
	// ErrInvalidPlanRequest / ErrEmptyIntendedUse.
	// PrimaryContactEmail은 lowercase normalize 후 저장.
	CreateIntake(ctx context.Context, tx storage.Tx, req CreateIntakeRequest) (CustomerIntake, error)

	// GetIntake는 intakeID로 row를 조회합니다. 없으면 ErrIntakeNotFound.
	GetIntake(ctx context.Context, tx storage.Tx, intakeID string) (CustomerIntake, error)

	// ListIntakes는 filter 조건에 맞는 row를 created_at DESC로 반환합니다.
	//
	// 본 stage는 cross-tenant 전역 list (운영자 admin이 모든 customer intake 검토).
	// Stage 2 handler에서 RBAC permission 'customer:intake:read' 강제 예정.
	ListIntakes(ctx context.Context, tx storage.Tx, filter ListIntakesFilter) ([]CustomerIntake, error)

	// AcceptIntake는 status=pending인 intake를 accepted로 전환합니다.
	//
	// 동작:
	//   - intakeID로 row 조회 → 없으면 ErrIntakeNotFound.
	//   - status != pending → ErrIntakeNotPending (이미 accepted/rejected).
	//   - accepted_at = now, accepted_by_user_id = req.AcceptedByUserID, status = 'accepted'.
	//   - req.TenantID가 비어있지 않으면 tenant_id 도 채움 (Stage 3 결선용).
	//
	// 멱등 X — 같은 intakeID로 두 번 호출 시 두 번째는 ErrIntakeNotPending.
	AcceptIntake(ctx context.Context, tx storage.Tx, req AcceptIntakeRequest) (CustomerIntake, error)

	// RejectIntake는 status=pending인 intake를 rejected로 전환합니다.
	//
	// 동작:
	//   - intakeID로 row 조회 → 없으면 ErrIntakeNotFound.
	//   - status != pending → ErrIntakeNotPending.
	//   - RejectionReason 비공백 검증 → 위반 시 ErrEmptyRejectionReason.
	//   - rejected_at = now, rejection_reason = req.RejectionReason, status = 'rejected'.
	//
	// 멱등 X.
	RejectIntake(ctx context.Context, tx storage.Tx, req RejectIntakeRequest) (CustomerIntake, error)
}

// 공통 에러 — 도메인 sentinel.
var (
	ErrEmptyOrganization    = errors.New("intake: OrganizationName is required")
	ErrInvalidEmail         = errors.New("intake: PrimaryContactEmail format invalid")
	ErrEmptyContactName     = errors.New("intake: PrimaryContactName is required")
	ErrInvalidPlanRequest   = errors.New("intake: PlanRequest is not a known value")
	ErrEmptyIntendedUse     = errors.New("intake: IntendedUse is required")
	ErrEmptyRejectionReason = errors.New("intake: RejectionReason is required")
	ErrIntakeNotFound       = errors.New("intake: intake not found")
	ErrIntakeNotPending     = errors.New("intake: intake is not pending (already accepted or rejected)")
)

// NormalizeEmail은 이메일을 lowercase + 공백 트림 normalize 합니다.
//
// validation은 별도 — ValidateCreateRequest 등에서 mail.ParseAddress 호출.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// ValidateCreateRequest는 CreateIntakeRequest의 validation 규칙을 검사합니다.
//
// PrimaryContactEmail은 lowercase normalize 후 mail.ParseAddress 검증.
// 위반 시 첫 번째 발견된 sentinel 에러 반환.
func ValidateCreateRequest(req CreateIntakeRequest) error {
	if strings.TrimSpace(req.OrganizationName) == "" {
		return ErrEmptyOrganization
	}
	normalized := NormalizeEmail(req.PrimaryContactEmail)
	if normalized == "" {
		return ErrInvalidEmail
	}
	if _, err := mail.ParseAddress(normalized); err != nil {
		return ErrInvalidEmail
	}
	if strings.TrimSpace(req.PrimaryContactName) == "" {
		return ErrEmptyContactName
	}
	if !IsValidPlanRequest(req.PlanRequest) {
		return ErrInvalidPlanRequest
	}
	if strings.TrimSpace(req.IntendedUse) == "" {
		return ErrEmptyIntendedUse
	}
	return nil
}
