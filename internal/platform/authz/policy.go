// Package authz는 세분 RBAC의 Policy Decision Point(PDP)를 제공합니다.
//
// design doc `docs/design/notes/rbac-fine-grained-design.md` §7 Stage 1 산출 — 결정 함수
// + 시스템 role permission matrix(§3.3)만. server middleware / DB / JWT 변경은
// Stage 2~5에서 진행합니다.
//
// 본 패키지는 pure utility — 도메인 import 0. 입력으로 받은 binding 슬라이스만 평가하고
// 결정(Decision)을 반환합니다. 도메인 경계 원칙 §5(DDD 경계) 준수.
package authz

// Resource는 권한 결정의 객체 차원입니다 (design doc §3.3).
//
// 9개 리소스 — robot/scan/report/insight/audit/fleet/compliance/tenantAdmin/system.
// tenantAdmin은 §3.3 매트릭스에서 "sso·webhook·users·invitation"이 하나의 행으로
// 묶여 있는 것을 단일 리소스로 정규화한 것입니다 — 모두 tenant 글로벌 관리 권한.
type Resource string

const (
	ResourceRobot       Resource = "robot"
	ResourceScan        Resource = "scan"
	ResourceReport      Resource = "report"
	ResourceInsight     Resource = "insight"
	ResourceAudit       Resource = "audit"
	ResourceFleet       Resource = "fleet"
	ResourceCompliance  Resource = "compliance"
	ResourceTenantAdmin Resource = "tenant_admin" // sso·webhook·users·invitation 통합
	ResourceSystem      Resource = "system"       // backup·integrity
)

// AllResources는 §3.3 매트릭스에 등장하는 9개 리소스를 순서대로 반환합니다 — 테스트 입력용.
func AllResources() []Resource {
	return []Resource{
		ResourceRobot, ResourceScan, ResourceReport, ResourceInsight,
		ResourceAudit, ResourceFleet, ResourceCompliance,
		ResourceTenantAdmin, ResourceSystem,
	}
}

// Action은 권한 결정의 동작 차원입니다 (design doc §3.3).
//
// 6개 — read/write/execute/admin/verify/export. 매트릭스 cell이 "—" 인 칸은
// 어떤 시스템 role도 해당 action을 갖지 않음을 의미합니다 (owner는 항상 implicit 통과).
type Action string

const (
	ActionRead    Action = "read"
	ActionWrite   Action = "write"
	ActionExecute Action = "execute"
	ActionAdmin   Action = "admin"
	ActionVerify  Action = "verify"
	ActionExport  Action = "export"
)

// AllActions는 §3.3 매트릭스 6개 action을 순서대로 반환합니다 — 테스트 입력용.
func AllActions() []Action {
	return []Action{
		ActionRead, ActionWrite, ActionExecute,
		ActionAdmin, ActionVerify, ActionExport,
	}
}

// ScopeType은 권한 binding 범위입니다 (design doc §3.2).
//
//   - ScopeTenant: tenant 전체 (모든 fleet에 implicit 적용).
//   - ScopeFleet: 특정 fleet ID 한정 — Subject.Fleet 가 ScopeID와 일치해야 함.
type ScopeType string

const (
	ScopeTenant ScopeType = "tenant"
	ScopeFleet  ScopeType = "fleet"
)

// 시스템 role 이름 (design doc §3.1 — 권장 시드 6개).
//
// owner는 모든 permission implicit. admin/auditor/read-only는 tenant scope.
// fleet-admin/operator는 fleet scope. design doc D-RBAC-3 권장 default = 6개.
const (
	RoleOwner      = "owner"
	RoleAdmin      = "admin"
	RoleFleetAdmin = "fleet-admin"
	RoleOperator   = "operator"
	RoleAuditor    = "auditor"
	RoleReadOnly   = "read-only"
)

// AllSystemRoles는 §3.1 시드 6개 role 이름을 반환합니다 — 테스트·전사 검증용.
func AllSystemRoles() []string {
	return []string{RoleOwner, RoleAdmin, RoleFleetAdmin, RoleOperator, RoleAuditor, RoleReadOnly}
}

// Permission은 단일 (resource, action) 결정 단위입니다.
//
// "*" 와일드카드는 Action.Wildcard 또는 Resource.Wildcard로 표현됩니다 — owner가 가진
// 단일 permission `{Resource:"*", Action:"*"}`은 모든 (resource, action) 쌍에 매치.
type Permission struct {
	Resource Resource
	Action   Action
}

// WildcardResource·WildcardAction은 와일드카드 매칭용 sentinel 값입니다.
//
// permission `{Resource:WildcardResource, Action:WildcardAction}` 는 모든 (r, a)에 매치.
// 부분 와일드카드(`{Resource:"robot", Action:WildcardAction}`) 는 robot.* 매치.
const (
	WildcardResource Resource = "*"
	WildcardAction   Action   = "*"
)

// Matches는 본 permission이 (resource, action) 요청을 충족하는지 반환합니다.
// 와일드카드는 양쪽 axis 독립적으로 동작합니다.
func (p Permission) Matches(resource Resource, action Action) bool {
	if p.Resource != WildcardResource && p.Resource != resource {
		return false
	}
	if p.Action != WildcardAction && p.Action != action {
		return false
	}
	return true
}

// RoleBinding은 한 사용자가 가진 role 한 건과 그 scope입니다.
//
// 예시:
//   - {RoleName:"admin", ScopeType:ScopeTenant, ScopeID:""} — 모든 fleet implicit.
//   - {RoleName:"operator", ScopeType:ScopeFleet, ScopeID:"flt_a"} — fleet_a만.
//
// 본 Stage 1은 RoleBinding 시리얼라이즈 / DB 영속화 / JWT claim 변경 0 — 결정 입력 타입만.
type RoleBinding struct {
	RoleName  string    // §3.1 시드 6개 또는 사용자 정의(Phase 6+).
	ScopeType ScopeType // ScopeTenant 또는 ScopeFleet.
	ScopeID   string    // ScopeType=ScopeFleet일 때만 fleet ID, ScopeTenant이면 빈 문자열.
}

// Subject는 권한 평가 입력 — 호출자(요청자) 정보입니다.
//
// Bindings는 사용자가 보유한 role binding 슬라이스. FleetID는 요청이 향한 fleet
// (없으면 빈 문자열 — tenant 글로벌 요청). Stage 4 server middleware에서 path 추출하여 주입합니다.
type Subject struct {
	Bindings []RoleBinding
	FleetID  string // 요청이 fleet scope 리소스를 다룰 때만 채움. 빈 문자열 = tenant 글로벌.
}
