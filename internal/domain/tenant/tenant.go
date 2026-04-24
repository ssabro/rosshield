// Package tenant는 테넌시·인증·인가 도메인의 공개 표면을 정의합니다.
//
// Phase 1 스코프(§E3): Tenant·User·Role·Permission·Session·ApiKey를 한 패키지에 묶음
// — 도메인 경계 P5는 다른 도메인 간 격리를 강제하지, 한 도메인 내부 분리는 강제하지 않습니다.
//
// audit 도메인과의 결합: tenant 도메인은 `audit` 패키지를 직접 import하지 않습니다 (P5).
// 대신 `AuditEmitter` 인터페이스를 받아 cmd/* bootstrap이 audit.Service 어댑터를 주입합니다.
package tenant

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/ssabro/rosshield/internal/platform/storage"
)

// Plan은 테넌트 SKU 분류입니다 (§4.2).
type Plan string

const (
	PlanDesktopFree Plan = "desktop_free"
	PlanDesktopPro  Plan = "desktop_pro"
	PlanEnterprise  Plan = "enterprise"
	PlanAppliance   Plan = "appliance"
)

// AuthProvider는 사용자 인증 출처입니다 (§5.7).
type AuthProvider string

const (
	AuthProviderLocal AuthProvider = "local" // 이메일+비밀번호 (argon2id)
	AuthProviderOIDC  AuthProvider = "oidc"  // OIDC 외부 IdP
	AuthProviderSAML  AuthProvider = "saml"  // SAML 외부 IdP
	AuthProviderOS    AuthProvider = "os"    // OS 로그인 매핑(데스크톱)
)

// UserStatus는 사용자 상태입니다.
type UserStatus string

const (
	UserStatusActive   UserStatus = "active"
	UserStatusDisabled UserStatus = "disabled"
	UserStatusInvited  UserStatus = "invited"
)

// Tenant는 테넌트 엔티티입니다 (§4.2).
type Tenant struct {
	ID        storage.TenantID
	Name      string
	Plan      Plan
	CreatedAt time.Time
	Settings  json.RawMessage
	Features  json.RawMessage
	Retention json.RawMessage
}

// User는 사용자 엔티티입니다 (§4.2).
//
// PasswordHash는 AuthProviderLocal인 경우만 채워지며, argon2id encoded 형식입니다
// (`$argon2id$v=19$m=65536,t=3,p=1$<salt>$<hash>`). 외부 IdP 사용자는 빈 값.
type User struct {
	ID              string
	TenantID        storage.TenantID
	Email           string
	DisplayName     string
	AuthProvider    AuthProvider
	ExternalSubject string
	PasswordHash    string
	Status          UserStatus
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// CreateRequest는 Service.Create 입력입니다.
//
// 첫 admin 사용자는 tenant 생성과 같은 Tx에 묶입니다 (B8 결정 — 빈 tenant는 의미 없음).
// admin 비밀번호는 raw 텍스트 — Service 내부에서 argon2id로 해시 후 저장.
type CreateRequest struct {
	Name             string
	Plan             Plan // 빈 값이면 PlanDesktopFree
	AdminEmail       string
	AdminPassword    string
	AdminDisplayName string
}

// CreateResult는 Service.Create 출력입니다.
type CreateResult struct {
	Tenant Tenant
	Admin  User
}

// AuditEmitter는 도메인 변경을 감사 로그에 기록하는 콜백입니다.
//
// tenant 도메인은 audit 도메인을 직접 import하지 않습니다 (P5 격리).
// bootstrap이 audit.Service를 어댑팅한 구현체를 주입합니다.
type AuditEmitter interface {
	// EmitTenantCreated는 tenant.created 이벤트를 audit에 append합니다.
	// tx는 tenant 생성과 같은 Tx — 같은 commit·rollback에 묶임.
	EmitTenantCreated(ctx context.Context, tx storage.Tx, t Tenant, admin User) error
}

// Service는 tenant 도메인 진입점입니다.
type Service interface {
	// Create는 새 tenant + 첫 admin user를 한 Tx에 생성하고 audit를 emit합니다.
	// Bootstrap Tx로 진입(tenant 생성은 tenant 외 진입점이므로 Storage.Bootstrap 사용).
	Create(ctx context.Context, tx storage.Tx, req CreateRequest) (CreateResult, error)

	// GetTenant는 ID로 tenant를 조회합니다. 없으면 storage.ErrNotFound.
	GetTenant(ctx context.Context, tx storage.Tx, id storage.TenantID) (Tenant, error)

	// GetUserByEmail은 (tenantID, email)로 사용자를 조회합니다. 없으면 storage.ErrNotFound.
	GetUserByEmail(ctx context.Context, tx storage.Tx, tenantID storage.TenantID, email string) (User, error)
}

// 공통 에러.
var (
	ErrEmptyName             = errors.New("tenant: Name is required")
	ErrEmptyEmail            = errors.New("tenant: AdminEmail is required")
	ErrInvalidEmail          = errors.New("tenant: AdminEmail format invalid")
	ErrEmptyPassword         = errors.New("tenant: AdminPassword is required")
	ErrPasswordTooShort      = errors.New("tenant: AdminPassword must be at least 12 characters")
	ErrEmailAlreadyExists    = errors.New("tenant: email already exists in this tenant")
	ErrInvalidPasswordCheck  = errors.New("tenant: password does not match")
	ErrPasswordHashMalformed = errors.New("tenant: password hash format invalid")
	ErrUnknownPlan           = errors.New("tenant: Plan is not a known value")
)
