package tenant

// invitation.go — E21 초대·역할 관리 도메인 표면.
//
// 책임:
//
//	- Invitation entity: 1회용 토큰 + 만료 + role_name + 발송자.
//	- Service.CreateInvitation: tenant admin이 (email, role)로 초대 생성, 토큰 반환.
//	- Service.AcceptInvitation: 토큰 + 사용자 입력으로 user 생성 + role 할당 + accepted_at 마킹.
//	- Service.GetInvitationByToken: 외부에서 token만으로 조회 (lookup 시 tenant ctx 진입 후).
//	- Service.ListInvitations: tenant 안 모든 초대 (pending·accepted·expired 모두).
//
// audit 결합 (P5):
//
//	AuditEmitter에 EmitInvitationSent / EmitInvitationAccepted 추가 (별 인터페이스).
//
// 멀티테넌시:
//
//	모든 메서드는 tx.TenantID() 또는 명시적 tenantID 파라미터로 격리.

import (
	"context"
	"errors"
	"time"

	"github.com/ssabro/rosshield/internal/platform/storage"
)

// Invitation은 tenant admin이 발송한 1회용 초대입니다.
//
// Token은 cryptographic random 32B → base64url. application layer가 생성·영속.
// AcceptedAt이 nil이면 active(만료 전이라면), 채워지면 1회 사용 완료 상태.
type Invitation struct {
	ID         string // "inv_<ULID>"
	TenantID   storage.TenantID
	Email      string // lowercase normalized
	RoleName   string // "admin" | "auditor" | "operator" | custom
	Token      string // base64url ~43자 — 1회용
	InvitedBy  string // 발송자 user ID
	ExpiresAt  time.Time
	AcceptedAt *time.Time
	AcceptedBy *string // accept 시 매칭된 user ID
	CreatedAt  time.Time
}

// IsExpired는 now 기준 invitation이 만료됐는지 반환합니다.
func (i Invitation) IsExpired(now time.Time) bool {
	return !i.ExpiresAt.IsZero() && !now.Before(i.ExpiresAt)
}

// IsAccepted는 accepted_at이 채워져 1회 사용 완료됐는지 반환합니다.
func (i Invitation) IsAccepted() bool {
	return i.AcceptedAt != nil
}

// IsActive는 사용 가능한 상태(미사용 + 미만료)인지 반환합니다.
func (i Invitation) IsActive(now time.Time) bool {
	return !i.IsAccepted() && !i.IsExpired(now)
}

// CreateInvitationRequest는 Service.CreateInvitation 입력입니다.
//
// ExpiresIn=0이면 DefaultInvitationTTL (7일).
type CreateInvitationRequest struct {
	TenantID  storage.TenantID
	Email     string
	RoleName  string
	InvitedBy string
	ExpiresIn time.Duration // 0이면 7일.
}

// CreateInvitationResult는 Service.CreateInvitation 출력입니다.
//
// Token은 발급 시점에만 노출 — accept URL에 포함되어 사용자에게 전달.
type CreateInvitationResult struct {
	Invitation Invitation
	Token      string // 위 Invitation.Token과 동일 — 명시적으로 한 번만 노출 의미를 강조.
}

// AcceptInvitationRequest는 Service.AcceptInvitation 입력입니다.
//
// Email은 token 발급 시점 email과 일치해야 함 — 다르면 ErrInvitationEmailMismatch.
// (동일 이메일 보안 — 발송자가 의도한 사용자가 accept 보장.)
type AcceptInvitationRequest struct {
	Token       string
	Email       string // 클라이언트가 입력한 이메일 (검증용)
	Password    string // local auth 용. 외부 IdP는 후속 stage.
	DisplayName string
}

// AcceptInvitationResult는 Service.AcceptInvitation 출력입니다.
type AcceptInvitationResult struct {
	User       User
	Roles      []Role
	Invitation Invitation
}

// InvitationAuditEmitter는 invitation 관련 audit emit 인터페이스입니다 (P5).
//
// 별 인터페이스로 분리 — 기존 AuditEmitter는 tenant.created 단일 메서드라 grow 회피.
// bootstrap이 같은 audit.Service 어댑터에 메서드 추가.
type InvitationAuditEmitter interface {
	EmitInvitationSent(ctx context.Context, tx storage.Tx, inv Invitation) error
	EmitInvitationAccepted(ctx context.Context, tx storage.Tx, inv Invitation, user User) error
}

// InvitationNotifier는 초대가 INSERT된 직후 외부 채널(email 등)로 알림을 보내는
// 옵트인 hook입니다 (O6 — invite email adapter).
//
// bootstrap이 platform/email 어댑터를 감싼 구현체를 sqliterepo Deps에 주입한다.
// nil이면 알림 skip — 기존 동작(admin이 token URL을 수동 전달) 유지. acceptURL은 caller가
// 빌드해서 전달 — 도메인은 PublicBaseURL을 모름.
//
// audit emit과 분리한 이유:
//   - audit는 결정론적 + 항상 성공 (실패 시 invitation 자체가 rollback).
//   - email은 외부 IO + 실패 가능 — sqliterepo는 본 hook err를 로깅만 하고 invitation
//     INSERT는 commit (best-effort 발송). 본 메서드의 error는 caller(repo)가 결정.
type InvitationNotifier interface {
	NotifyInvitationSent(ctx context.Context, inv Invitation, acceptURL string) error
}

// InvitationService는 invitation 도메인 진입점입니다.
//
// tenant.Service와 분리 — 한 도메인 안 sub-interface 분할로 grow 가능. bootstrap은
// 같은 sqliterepo Repo가 두 인터페이스 모두 만족하도록 설계.
type InvitationService interface {
	// CreateInvitation은 새 초대를 INSERT하고 토큰을 반환합니다.
	// (tenantID, email)로 active(미사용·미만료) 초대가 이미 있으면 ErrInvitationActive.
	// roleName이 (tenantID, name) 조합으로 존재하지 않으면 ErrInvalidRole.
	CreateInvitation(ctx context.Context, tx storage.Tx, req CreateInvitationRequest) (CreateInvitationResult, error)

	// AcceptInvitation은 토큰 + email + password + displayName으로 user를 생성하고 role을 할당합니다.
	// 이미 사용된(AcceptedAt non-nil) 토큰은 ErrInvitationAlreadyUsed.
	// 만료된 토큰은 ErrInvitationExpired.
	// 같은 (tenantID, email) user가 이미 있으면 ErrEmailAlreadyExists.
	// 호출자: 비인증 entry — Storage.Bootstrap Tx로 진입.
	AcceptInvitation(ctx context.Context, tx storage.Tx, req AcceptInvitationRequest) (AcceptInvitationResult, error)

	// GetInvitationByToken은 token으로 invitation 1건을 반환합니다.
	// 호출자: AcceptInvitation 사전 조회 또는 Web /invitations/{token} 미리보기 페이지.
	// Bootstrap Tx로 진입 (token만으로 lookup — tenant 미상).
	// 없으면 ErrInvitationNotFound.
	GetInvitationByToken(ctx context.Context, tx storage.Tx, token string) (Invitation, error)

	// ListInvitations는 tenant 안 모든 초대를 created_at DESC로 반환합니다.
	// 만료된 초대도 포함 — UI가 색상으로 구분.
	ListInvitations(ctx context.Context, tx storage.Tx) ([]Invitation, error)

	// RevokeInvitation은 (tenantID, invitationID) 초대를 즉시 만료시킵니다 (expires_at = now).
	// 이미 사용된 초대는 no-op (멱등). 미존재 시 ErrInvitationNotFound.
	RevokeInvitation(ctx context.Context, tx storage.Tx, invitationID string) error
}

// 공통 sentinel.
var (
	ErrInvitationNotFound      = errors.New("tenant: invitation not found")
	ErrInvitationExpired       = errors.New("tenant: invitation expired")
	ErrInvitationAlreadyUsed   = errors.New("tenant: invitation already used")
	ErrInvitationActive        = errors.New("tenant: active invitation already exists for this email")
	ErrInvitationEmailMismatch = errors.New("tenant: email does not match invitation")
	ErrInvalidRole             = errors.New("tenant: invalid role name")
	ErrEmptyToken              = errors.New("tenant: token is required")
)

// DefaultInvitationTTL은 초대 토큰의 기본 만료 시간입니다 (7일).
const DefaultInvitationTTL = 7 * 24 * time.Hour
