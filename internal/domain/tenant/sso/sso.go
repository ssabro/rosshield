// Package sso는 외부 IdP(OIDC + SAML) 기반 single-sign-on 도메인 표면입니다.
//
// E20-A 스코프 (E20-A 본 stage):
//
//   - Provider CRUD + LoginAttempt(state·PKCE·nonce·RelayState) 영속.
//   - ExternalIdentity 매핑(provider sub → users.id)의 upsert 표면.
//   - 모든 sentinel·인터페이스 정의 — 실제 IdP HTTP 호출은 후속 stage(E20-B/C)에서.
//
// 도메인 결합 규칙 (P5):
//
//	본 패키지는 tenant 패키지의 일부지만 sub-package로 분리한 이유는 표면 비대화 방지.
//	audit 도메인은 직접 import 금지 — AuditEmitter interface로 주입.
//	users 테이블 갱신(External SSO 첫 로그인 시 user 자동 생성)은 본 stage 범위 외 —
//	tenant.Service.RegisterExternal(가칭) 같은 helper로 후속 위임 예정.
//
// 멀티테넌시 (P4):
//
//	모든 표면은 tx.TenantID() 기반으로 격리. cross-tenant lookup 금지.
//
// 옵트인 (P10):
//
//	SSO는 enterprise 기능 — 라이선스 게이트(E24)에서 검증되며, 활성 provider 0개면
//	StartLogin은 ErrProviderNotFound. 기본 활성화 아님.
package sso

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/ssabro/rosshield/internal/platform/storage"
)

// Type은 SSO IdP 프로토콜 분류입니다.
type Type string

const (
	TypeOIDC Type = "oidc" // Authorization Code + PKCE (Google / Okta / Auth0)
	TypeSAML Type = "saml" // SP-initiated, HTTP-POST binding (Okta / Azure AD)
)

// Provider는 tenant 단위 IdP 설정 1건입니다.
//
// Config의 스키마는 Type별로 다릅니다 — application layer가 unmarshal/validate.
//
//	OIDC: {"issuer":"https://accounts.google.com","clientId":"...","redirectUri":"https://app/...","scopes":["openid","email","profile"]}
//	SAML: {"metadataUrl":"https://idp/metadata.xml","acsUrl":"https://app/saml/acs"}  (또는 metadataXml inline)
//
// Enabled=false면 StartLogin이 ErrProviderDisabled.
type Provider struct {
	ID        string
	TenantID  storage.TenantID
	Type      Type
	Name      string // tenant 안 unique 라벨 ("Google Workspace", "Okta - Eng")
	Enabled   bool
	Config    json.RawMessage
	CreatedAt time.Time
	UpdatedAt time.Time
}

// LoginAttempt는 IdP 호출 전 영속되는 in-flight state입니다.
//
// CSRF 방어 핵심:
//
//	OIDC: state(랜덤) + PKCEVerifier(code_verifier) + Nonce(id_token nonce).
//	SAML: state(랜덤) + RelayState(post-login destination URL 같은 임의 데이터).
//
// 만료(ExpiresAt) 후 callback 도달 → ErrStateExpired. 한 번 CompletedAt 채워지면 재사용 거부 → ErrInvalidState.
type LoginAttempt struct {
	ID           string
	TenantID     storage.TenantID
	ProviderID   string
	State        string
	PKCEVerifier string // OIDC. SAML이면 빈 값.
	Nonce        string // OIDC. SAML이면 빈 값.
	RelayState   string // SAML. OIDC면 빈 값.
	CreatedAt    time.Time
	ExpiresAt    time.Time
	CompletedAt  *time.Time // nil=미사용. 채워지면 재사용 거부.
}

// IsExpired는 now 기준 attempt가 만료되었는지 반환합니다.
func (a LoginAttempt) IsExpired(now time.Time) bool {
	return !a.ExpiresAt.IsZero() && !now.Before(a.ExpiresAt)
}

// IsCompleted는 attempt가 이미 사용되었는지 반환합니다 (재사용 방지).
func (a LoginAttempt) IsCompleted() bool {
	return a.CompletedAt != nil
}

// ExternalIdentity는 IdP의 sub(또는 NameID)를 내부 user.id에 묶는 매핑입니다.
//
// last_seen_at은 매 로그인마다 갱신(P9 예외 — 'last seen'은 사실 갱신 허용).
// first_seen_at은 INSERT 시점 고정.
type ExternalIdentity struct {
	ProviderID      string
	ExternalSubject string // OIDC sub claim 또는 SAML NameID
	TenantID        storage.TenantID
	UserID          string
	Email           string
	FirstSeenAt     time.Time
	LastSeenAt      time.Time
}

// CreateProviderRequest는 Service.CreateProvider 입력입니다.
type CreateProviderRequest struct {
	TenantID storage.TenantID
	Type     Type
	Name     string
	Enabled  bool
	Config   json.RawMessage
}

// UpdateProviderRequest는 Service.UpdateProvider 입력입니다.
//
// nil 필드는 변경 없음(merge 의미). Enabled는 *bool로 명시(false 의미 보존).
type UpdateProviderRequest struct {
	ID       string
	TenantID storage.TenantID
	Name     *string
	Enabled  *bool
	Config   json.RawMessage // nil이면 변경 없음
}

// StartLoginRequest는 Service.StartLogin 입력입니다.
//
// RedirectAfter는 SAML RelayState 또는 OIDC custom state payload — 호출자가 callback에서 어디로
// 보낼지 결정 가능. 비워두면 default landing page로.
type StartLoginRequest struct {
	ProviderID    string
	RedirectAfter string
}

// StartLoginResult는 Service.StartLogin 출력입니다.
//
// AuthURL은 클라이언트가 302로 보낼 IdP 위치(OIDC authorization endpoint 또는 SAML AuthnRequest).
// 본 E20-A 단계는 stub — AuthURL은 빈 값, State만 영속 후 반환.
type StartLoginResult struct {
	AuthURL string // E20-B/C에서 IdP 호출 + redirect URL 생성 후 채움
	State   string // CSRF token — callback에서 매칭
	Attempt LoginAttempt
}

// CompleteLoginRequest는 Service.CompleteLogin 입력입니다.
//
// OIDC: Code 채움(IdP authorization code), SAMLResponse 빈 값.
// SAML: SAMLResponse 채움(base64 XML), Code 빈 값.
type CompleteLoginRequest struct {
	State        string
	Code         string // OIDC authorization code
	SAMLResponse string // SAML base64 XML
}

// CompleteLoginResult는 Service.CompleteLogin 출력입니다.
//
// 본 E20-A 단계는 stub — Identity·UserID는 빈 값. E20-B/C에서 IdP 토큰 교환 + claim 추출 후 채움.
// 실제로 access·refresh 토큰을 발급하는 책임은 후속 stage에서 tenant.Service에 위임.
//
// Groups (RBAC fleet 정밀화 Stage 5):
//
//	OIDC id_token 'groups' claim 또는 SAML attribute(groups/Groups/MemberOf/memberOf)에서
//	추출된 사용자 group 슬라이스. application layer(SSO callback handler)가 GroupMappingService
//	에 전달하여 user_roles.source='sso' 자동 sync 결정에 사용. 빈 슬라이스나 nil이면 매 login
//	에서 source='sso' 기존 binding 모두 revoke (IdP가 진실의 원천).
//
// ProviderID (RBAC fleet 정밀화 Stage 5):
//
//	SSO sync 호출자가 GroupMappingService.ResolveBindingsForGroups(providerID, groups) 호출
//	시 사용. CompleteLogin 흐름의 attempt에서 추출한 사실값.
type CompleteLoginResult struct {
	Identity   ExternalIdentity
	Groups     []string // RBAC fleet 정밀화 Stage 5 — IdP group claim/attribute 추출 결과.
	ProviderID string   // RBAC fleet 정밀화 Stage 5 — sync 호출자에 전달.
}

// AuditEmitter는 SSO 도메인 변경을 audit chain에 기록하는 콜백입니다 (P5).
//
// 호출 시점:
//
//	CreateProvider/UpdateProvider/DeleteProvider → EmitProviderChanged
//	StartLogin                                    → EmitLoginStarted
//	CompleteLogin (성공/실패)                     → EmitLoginCompleted
type AuditEmitter interface {
	EmitProviderChanged(ctx context.Context, tx storage.Tx, p Provider, action string) error // action: "created"|"updated"|"deleted"
	EmitLoginStarted(ctx context.Context, tx storage.Tx, a LoginAttempt) error
	EmitLoginCompleted(ctx context.Context, tx storage.Tx, a LoginAttempt, identity ExternalIdentity, ok bool) error
}

// Service는 sso 도메인 진입점입니다.
type Service interface {
	// CreateProvider는 새 provider를 INSERT하고 audit emit합니다.
	// 같은 (tenantID, name) 조합은 ErrProviderNameExists.
	CreateProvider(ctx context.Context, tx storage.Tx, req CreateProviderRequest) (Provider, error)

	// UpdateProvider는 부분 갱신 + updated_at + audit emit. 없으면 ErrProviderNotFound.
	UpdateProvider(ctx context.Context, tx storage.Tx, req UpdateProviderRequest) (Provider, error)

	// DeleteProvider는 hard delete + audit emit. 없으면 ErrProviderNotFound.
	// 주의: 연결된 external_identities·login_attempts FK 정책은 application 책임 (cascade는 schema 미정의).
	DeleteProvider(ctx context.Context, tx storage.Tx, providerID string) error

	// GetProvider는 tenant scope로 단건 조회. 없으면 ErrProviderNotFound.
	GetProvider(ctx context.Context, tx storage.Tx, providerID string) (Provider, error)

	// ListProviders는 tenant의 모든 provider를 created_at ASC로 반환합니다.
	ListProviders(ctx context.Context, tx storage.Tx) ([]Provider, error)

	// StartLogin은 새 LoginAttempt를 영속하고 (AuthURL, state)를 반환합니다.
	// E20-A 단계는 AuthURL 빈 값(stub) — state·PKCE·nonce는 영속.
	// 비활성 provider면 ErrProviderDisabled, 미존재면 ErrProviderNotFound.
	StartLogin(ctx context.Context, tx storage.Tx, req StartLoginRequest) (StartLoginResult, error)

	// CompleteLogin은 state로 LoginAttempt를 lookup하여 검증 후 ExternalIdentity를 upsert합니다.
	// 본 E20-A 단계는 IdP 토큰 교환 X — state 검증 + completed_at 마킹만 수행.
	// 실패: ErrInvalidState / ErrStateExpired / ErrIdPMismatch.
	CompleteLogin(ctx context.Context, tx storage.Tx, req CompleteLoginRequest) (CompleteLoginResult, error)

	// UpsertExternalIdentity는 (providerID, externalSubject) 쌍을 INSERT 또는 last_seen_at 갱신합니다.
	// 후속 stage에서 CompleteLogin 흐름의 마지막 단계로 호출 — 본 stage는 단위 테스트 + 스캐폴드 표면.
	UpsertExternalIdentity(ctx context.Context, tx storage.Tx, identity ExternalIdentity) (ExternalIdentity, error)
}

// 공통 sentinel.
var (
	ErrProviderNotFound   = errors.New("sso: provider not found")
	ErrProviderDisabled   = errors.New("sso: provider is disabled")
	ErrProviderNameExists = errors.New("sso: provider name already exists in this tenant")
	ErrInvalidState       = errors.New("sso: state is invalid or already used")
	ErrStateExpired       = errors.New("sso: login attempt expired")
	ErrIdPMismatch        = errors.New("sso: identity provider mismatch")
	ErrUnsupportedType    = errors.New("sso: unsupported provider type")
	ErrEmptyName          = errors.New("sso: provider name is required")
	ErrEmptyConfig        = errors.New("sso: provider config is required")
	ErrEmptyState         = errors.New("sso: state is required")
	ErrEmptySubject       = errors.New("sso: external subject is required")
)

// DefaultAttemptTTL은 LoginAttempt의 기본 만료 시간입니다 (5분).
//
// IdP 왕복(인증 + redirect)에 충분하면서 짧게 — replay attack window 최소화.
const DefaultAttemptTTL = 5 * time.Minute

// IsValidType은 t가 알려진 IdP 프로토콜 enum인지 반환합니다.
func IsValidType(t Type) bool {
	switch t {
	case TypeOIDC, TypeSAML:
		return true
	default:
		return false
	}
}
