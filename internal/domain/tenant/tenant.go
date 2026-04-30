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

// Permission은 RBAC 권한 토큰입니다 (§5.8). 단순 문자열.
//
// 형식: "<resource>.<action>" (예: "robot.read", "scan.execute"). 와일드카드 "*" = 전체.
// Phase 1은 와일드카드는 "*" 단일 토큰만 지원 (sub-domain wildcard는 후속).
type Permission string

const (
	PermissionAll Permission = "*"

	PermAuditRead      Permission = "audit.read"
	PermAuditExport    Permission = "audit.export"
	PermAuditVerify    Permission = "audit.verify"
	PermRobotRead      Permission = "robot.read"
	PermRobotWrite     Permission = "robot.write"
	PermScanRead       Permission = "scan.read"
	PermScanExecute    Permission = "scan.execute"
	PermReportRead     Permission = "report.read"
	PermReportSign     Permission = "report.sign"
	PermReportDownload Permission = "report.download"
)

// Role은 권한 묶음입니다 (§4.2). tenant 단위.
//
// IsSystem=true는 부팅 시 시드된 admin·auditor·operator 역할.
// 사용자 정의 역할은 IsSystem=false (Phase 2 기능).
type Role struct {
	ID          string // "rl_<ULID>"
	TenantID    storage.TenantID
	Name        string // "admin" | "auditor" | "operator" | custom
	Permissions []Permission
	IsSystem    bool
	CreatedAt   time.Time
}

// 시스템 역할 이름.
const (
	RoleAdmin    = "admin"
	RoleAuditor  = "auditor"
	RoleOperator = "operator"
)

// SystemRolePermissions는 부팅 시 시드되는 3개 시스템 역할의 기본 permission 셋입니다.
//
// admin은 와일드카드("*")로 모든 권한 — Phase 1 단순화.
var SystemRolePermissions = map[string][]Permission{
	RoleAdmin: {PermissionAll},
	RoleAuditor: {
		PermAuditRead, PermAuditExport, PermAuditVerify,
		PermScanRead, PermReportRead, PermReportDownload,
	},
	RoleOperator: {
		PermRobotRead, PermRobotWrite,
		PermScanRead, PermScanExecute,
		PermReportRead,
	},
}

// HasPermission은 단일 role이 perm을 갖는지 반환합니다.
// "*" 와일드카드는 모든 perm에 대해 true.
func (r Role) HasPermission(perm Permission) bool {
	for _, p := range r.Permissions {
		if p == PermissionAll || p == perm {
			return true
		}
	}
	return false
}

// AnyHasPermission은 roles 중 하나라도 perm을 갖는지 반환합니다 (RBAC 체크).
func AnyHasPermission(roles []Role, perm Permission) bool {
	for _, r := range roles {
		if r.HasPermission(perm) {
			return true
		}
	}
	return false
}

// ApiKey는 프로그래매틱 접근용 키입니다 (§5.9).
//
// 발급 시 raw token은 한 번만 호출자에게 반환되고, DB는 argon2id 해시(`Hashed`)만 저장합니다.
// `Prefix`(앞 12자, "fg_live_XXXX")는 사용자 식별·표시·DB lookup 용도.
type ApiKey struct {
	ID         string // "ak_<ULID>"
	TenantID   storage.TenantID
	Name       string
	Prefix     string // "fg_live_XXXX" 12자
	Hashed     string // argon2id encoded
	Scopes     []Permission
	ExpiresAt  *time.Time
	LastUsedAt *time.Time
	CreatedBy  string // user ID
	CreatedAt  time.Time
	RevokedAt  *time.Time // 설정되면 인증 거부 (soft delete)
}

// IssueApiKeyRequest는 Service.IssueApiKey 입력입니다.
type IssueApiKeyRequest struct {
	TenantID  storage.TenantID
	Name      string
	Scopes    []Permission // 빈 슬라이스 허용 — 발급 시 검증 안 함
	ExpiresAt *time.Time   // nil = 무기한
	CreatedBy string       // 발급한 user ID
}

// IssueApiKeyResult는 Service.IssueApiKey 출력입니다.
//
// RawToken은 발급 시점에만 반환됩니다 (§5.9). 호출자는 이 값을 안전하게 저장해야 하며,
// 이후 어떤 API로도 다시 노출되지 않습니다.
type IssueApiKeyResult struct {
	Key      ApiKey // Hashed만 채워짐, Hashed는 검증 외 노출 금지
	RawToken string // "fg_live_<32 random>", 사용자에게 한 번만 표시
}

// LoginRequest는 Service.Login 입력입니다.
type LoginRequest struct {
	TenantID storage.TenantID
	Email    string
	Password string
}

// LoginResult는 Service.Login·Refresh 출력입니다.
type LoginResult struct {
	AccessToken      string
	RefreshToken     string
	AccessExpiresAt  time.Time
	RefreshExpiresAt time.Time
	User             User
	Roles            []Role
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
	// Create는 새 tenant + 첫 admin user + 시스템 역할 3개(admin·auditor·operator) + admin 역할 할당을
	// 한 Tx에 생성하고 audit를 emit합니다.
	// Bootstrap Tx로 진입(tenant 생성은 tenant 외 진입점이므로 Storage.Bootstrap 사용).
	Create(ctx context.Context, tx storage.Tx, req CreateRequest) (CreateResult, error)

	// GetTenant는 ID로 tenant를 조회합니다. 없으면 storage.ErrNotFound.
	GetTenant(ctx context.Context, tx storage.Tx, id storage.TenantID) (Tenant, error)

	// GetUserByEmail은 (tenantID, email)로 사용자를 조회합니다. 없으면 storage.ErrNotFound.
	GetUserByEmail(ctx context.Context, tx storage.Tx, tenantID storage.TenantID, email string) (User, error)

	// GetRole은 (tenantID, name)으로 role을 조회합니다. 없으면 storage.ErrNotFound.
	GetRole(ctx context.Context, tx storage.Tx, tenantID storage.TenantID, name string) (Role, error)

	// AssignRole은 user에게 role을 할당합니다 (멱등 — 이미 할당돼 있어도 에러 없음).
	AssignRole(ctx context.Context, tx storage.Tx, userID, roleID string) error

	// GetUserRoles는 user에게 할당된 모든 role을 반환합니다.
	GetUserRoles(ctx context.Context, tx storage.Tx, userID string) ([]Role, error)

	// IssueApiKey는 새 API key를 발급합니다.
	// raw token은 결과의 RawToken에 한 번만 반환됩니다 — 호출자가 사용자에게 표시 후 즉시 폐기.
	// DB에는 argon2id 해시만 저장 (§5.9).
	IssueApiKey(ctx context.Context, tx storage.Tx, req IssueApiKeyRequest) (IssueApiKeyResult, error)

	// AuthenticateApiKey는 raw token으로 ApiKey를 검증·반환합니다.
	// raw token에서 prefix 추출 → DB lookup → argon2id verify → revoked·expires 체크.
	// 호출자(인증 미들웨어)는 storage.Bootstrap Tx로 진입 — 토큰 검증 시점에 tenant 미상.
	// 실패: ErrInvalidApiKeyFormat / ErrApiKeyNotFound / ErrApiKeyRevoked / ErrApiKeyExpired.
	AuthenticateApiKey(ctx context.Context, tx storage.Tx, rawToken string) (ApiKey, error)

	// RevokeApiKey는 (tenantID, apiKeyID) row의 revoked_at을 현재 시각으로 설정합니다 (soft delete).
	// 이미 revoked면 no-op (멱등). row가 없으면 storage.ErrNotFound.
	RevokeApiKey(ctx context.Context, tx storage.Tx, tenantID storage.TenantID, apiKeyID string) error

	// ListApiKeys는 tenant의 모든 ApiKey를 반환합니다 (revoked 포함).
	// Hashed 필드는 빈 값으로 마스킹 — 외부 노출 방지.
	ListApiKeys(ctx context.Context, tx storage.Tx, tenantID storage.TenantID) ([]ApiKey, error)

	// Login은 (tenant, email, password)로 로그인하여 access·refresh 토큰을 발급합니다.
	// 호출자는 ctx에 req.TenantID를 주입한 storage.Tx로 진입.
	// 실패: ErrInvalidCredentials / ErrUserDisabled.
	Login(ctx context.Context, tx storage.Tx, req LoginRequest) (LoginResult, error)

	// Refresh는 refresh token을 검증하고 새 access·refresh를 발급합니다 (rotation).
	// 기존 refresh의 revoked_at을 설정하고 새 jti를 INSERT — 같은 refresh 재사용 시 ErrRefreshRevoked.
	// 호출자는 refresh token에서 추출한 tid를 ctx에 주입한 후 storage.Tx로 진입.
	Refresh(ctx context.Context, tx storage.Tx, refreshToken string) (LoginResult, error)

	// Logout은 refresh token을 revoke합니다 (멱등). 같은 jti로 다시 호출해도 OK.
	Logout(ctx context.Context, tx storage.Tx, refreshToken string) error

	// VerifyAccessToken은 access token을 stateless 검증하여 claims를 반환합니다.
	// DB 접근 없음 — 미들웨어가 매 요청마다 호출 가능.
	VerifyAccessToken(ctx context.Context, accessToken string) (AccessClaims, error)

	// RevokeAllRefreshForUser는 한 user의 모든 활성(revoked_at IS NULL) refresh token을
	// 일괄 revoke합니다 (C7 reuse detection cleanup용).
	//
	// 일반 운영에서도 사용 가능: admin이 user의 비밀번호 강제 변경·계정 정지 시 호출.
	// 멱등 — 이미 revoked인 token은 그대로 둠.
	// 반환 int은 새로 revoke된 count.
	RevokeAllRefreshForUser(ctx context.Context, tx storage.Tx, tenantID storage.TenantID, userID string) (int, error)
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
