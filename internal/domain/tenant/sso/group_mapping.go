package sso

// group_mapping.go — RBAC fleet 정밀화 Stage 4 SSO group → role 자동 매핑 도메인 표면.
//
// 책임:
//
//   - GroupRoleMapping entity: (provider_id, group_value, role_id, scope_type, scope_id) row.
//   - GroupMappingService.CreateGroupMapping / ListGroupMappings / DeleteGroupMapping / ResolveBindingsForGroups.
//
// 매핑 정책 (design doc D-RBACEX-5 권장 default = A 명시 mapping):
//
//	IdP claim에서 추출된 group 값(예: "fleet-admin-warehouse-a")을 명시적 매핑 테이블로
//	(role_id, scope_type, scope_id) 셋으로 resolve합니다. naming convention 자동 파싱은
//	본 stage 범위 외 — onboarding helper로 별 작업.
//
// 도메인 결합 규칙 (P5):
//
//	본 패키지는 tenant 도메인의 sub-package — 같은 tenant 패키지 안에서 표면 비대화 회피.
//	audit 도메인은 직접 import 금지 — AuditEmitter interface로 주입 (sso.go 일관).
//	users_roles 자동 sync(insert/delete) 결선은 본 stage 비대상 — Stage 5에서 tenant.Service
//	에 SyncRoleBindings 메서드 추가 후 callback 흐름에서 호출.
//
// 멀티테넌시 (P4):
//
//	모든 표면이 tx.TenantID() 기반. cross-tenant lookup은 ErrGroupMappingNotFound로 마스킹.

import (
	"context"
	"errors"
	"time"

	"github.com/ssabro/rosshield/internal/platform/storage"
)

// BindingSource는 user_roles row의 origin 추적 값입니다 (Stage 4 — D-RBACEX-7 권장 B).
//
//   - BindingSourceManual: 운영자가 admin UI / API로 명시 할당. SSO group sync는 보존.
//   - BindingSourceSSO:    SSO group 매핑 흐름이 자동 생성. 매 login sync 대상.
//
// DB 컬럼 user_roles.source는 TEXT NOT NULL DEFAULT 'manual' — 기존 row는 모두 manual.
type BindingSource string

const (
	BindingSourceManual BindingSource = "manual"
	BindingSourceSSO    BindingSource = "sso"
)

// IsValidBindingSource는 s가 알려진 BindingSource enum인지 반환합니다.
func IsValidBindingSource(s BindingSource) bool {
	switch s {
	case BindingSourceManual, BindingSourceSSO:
		return true
	default:
		return false
	}
}

// GroupRoleMapping은 IdP claim group 값 → (role, scope) 매핑 1건입니다.
//
// 같은 (provider_id, group_value, role_id, scope_type, scope_id) 5-tuple은 UNIQUE —
// 동일 매핑 중복 INSERT는 ErrGroupMappingExists로 차단. 같은 group이 다른 (role, scope)
// 조합으로 매핑되는 multi-binding은 별 row으로 허용 (예: "fleet-admin-warehouse-a"
// group이 fleet-admin@flt_A + auditor@flt_A 동시 할당).
//
// ScopeType은 tenant 도메인의 ScopeType과 같은 값 의미 — 'tenant' | 'fleet'. 본 패키지에서는
// 의존성 회피를 위해 string 형으로 보존하고, 변환은 호출자(application service)가 수행합니다.
type GroupRoleMapping struct {
	ID         string // "sgm_<ULID>"
	TenantID   storage.TenantID
	ProviderID string // sso_providers(id)
	GroupValue string // IdP claim group ("fleet-admin-warehouse-a")
	RoleID     string // roles(id)
	ScopeType  string // 'tenant' | 'fleet' (tenant.ScopeType 호환)
	ScopeID    string // scope_type='fleet'이면 fleet ID, 'tenant'이면 ''
	CreatedAt  time.Time
}

// ResolvedBinding은 ResolveBindingsForGroups의 결과 1건입니다.
//
// 호출자가 tenant.RoleBinding 또는 user_roles row로 변환하여 user에게 적용합니다.
// 본 표면은 sso sub-package에 격리되어 tenant 패키지를 import하지 않음 (P5).
type ResolvedBinding struct {
	RoleID    string
	ScopeType string // 'tenant' | 'fleet'
	ScopeID   string
}

// CreateGroupMappingRequest는 GroupMappingService.CreateGroupMapping 입력입니다.
type CreateGroupMappingRequest struct {
	ProviderID string
	GroupValue string
	RoleID     string
	ScopeType  string // 빈 값이면 'tenant' default.
	ScopeID    string // scope_type='tenant'이면 빈 문자열로 정규화.
}

// GroupMappingService는 SSO group 매핑 도메인 진입점입니다.
//
// CRUD는 admin이 web UI로 호출 (Stage 5에서 HTTP 핸들러 추가).
// ResolveBindingsForGroups는 SSO callback(CompleteLogin)에서 호출하여 user_roles 자동 sync 결정.
type GroupMappingService interface {
	// CreateGroupMapping은 새 매핑을 INSERT합니다.
	// 같은 5-tuple(provider, group, role, scope_type, scope_id)이 이미 있으면 ErrGroupMappingExists.
	// provider/role이 cross-tenant이거나 tenant 미일치면 ErrProviderNotFound 또는 ErrRoleNotFoundForTenant.
	CreateGroupMapping(ctx context.Context, tx storage.Tx, req CreateGroupMappingRequest) (GroupRoleMapping, error)

	// ListGroupMappings는 provider의 모든 매핑을 created_at ASC로 반환합니다.
	// provider가 다른 tenant 소속이면 ErrProviderNotFound.
	ListGroupMappings(ctx context.Context, tx storage.Tx, providerID string) ([]GroupRoleMapping, error)

	// DeleteGroupMapping은 (tenantID, mappingID) 매핑 1건을 삭제합니다.
	// 없으면 ErrGroupMappingNotFound.
	DeleteGroupMapping(ctx context.Context, tx storage.Tx, mappingID string) error

	// ResolveBindingsForGroups는 IdP claim group 슬라이스를 받아 provider 매핑 테이블로
	// resolve된 ResolvedBinding 슬라이스를 반환합니다 (멱등 · 결정론).
	//
	// 빈 groups 또는 매핑 없는 group은 skip — 결과 빈 슬라이스는 "이 사용자는 어떤 자동
	// binding도 받지 않음" 의미. 호출자(Stage 5 SyncRoleBindings)가 source='sso' 기존 row를
	// 삭제하면 IdP에서 group 회수된 사용자는 자동 권한 회수.
	//
	// 중복 binding 제거: 같은 (role_id, scope_type, scope_id) 셋이 여러 매핑에서 나오면
	// 1건만 반환 (호출자 멱등 보장 위해 set-of 의미).
	ResolveBindingsForGroups(ctx context.Context, tx storage.Tx, providerID string, groups []string) ([]ResolvedBinding, error)
}

// 공통 sentinel.
var (
	ErrGroupMappingNotFound  = errors.New("sso: group mapping not found")
	ErrGroupMappingExists    = errors.New("sso: group mapping already exists")
	ErrEmptyGroupValue       = errors.New("sso: group value is required")
	ErrEmptyRoleID           = errors.New("sso: role ID is required")
	ErrInvalidScopeType      = errors.New("sso: scope type must be 'tenant' or 'fleet'")
	ErrEmptyScopeIDForFleet  = errors.New("sso: scope ID is required when scope type is 'fleet'")
	ErrRoleNotFoundForTenant = errors.New("sso: role not found in this tenant")
)
