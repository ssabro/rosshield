// decision_test.go — design doc §7 Stage 1의 단위 테스트 3종.
//
//   - TestDecisionTable_AllRoleResourceActionMatrix: §3.3 결정 테이블 전체 매트릭스
//     (6 role × 9 resource × 6 action = 324 case) 검증.
//   - TestPermissionImpliesWildcard: owner의 단일 와일드카드 permission이 모든 칸 통과.
//   - TestFleetScopeBeatsTenantDeny: fleet[A] operator + tenant scope read-only 동시 보유 →
//     fleet[A] write 가능.

package authz

import (
	"fmt"
	"testing"
)

// expectedAllow는 design doc §3.3 매트릭스 cell을 코드로 표현한 expected 셋입니다.
//
// 각 entry의 string list는 §3.3 cell에 등장하는 role 약칭 그대로:
//
//	"own" = owner (모든 cell implicit — 매트릭스에는 표기 안 함)
//	"adm" = admin
//	"fadm" = fleet-admin
//	"op" = operator
//	"aud" = auditor
//	"ro" = read-only
//
// owner는 §3.3 "owner는 모든 칸 implicit 포함" 직역으로 모든 cell에 자동 추가.
// "—" cell은 owner만 가능하므로 빈 슬라이스로 표기.
func expectedRolesForCell(resource Resource, action Action) []string {
	// design doc §3.3 매트릭스 그대로. owner는 별도 처리(implicit).
	matrix := map[Resource]map[Action][]string{
		ResourceRobot: {
			ActionRead:    {"ro", "aud", "op", "fadm", "adm"},
			ActionWrite:   {"op", "fadm", "adm"},
			ActionExecute: {}, // —
			ActionAdmin:   {"fadm", "adm"},
			ActionVerify:  {}, // —
			ActionExport:  {"aud", "adm"},
		},
		ResourceScan: {
			ActionRead:    {"ro", "aud", "op", "fadm", "adm"},
			ActionWrite:   {}, // —
			ActionExecute: {"op", "fadm", "adm"},
			ActionAdmin:   {"fadm", "adm"},
			ActionVerify:  {}, // —
			ActionExport:  {"aud", "adm"},
		},
		ResourceReport: {
			ActionRead:    {"ro", "aud", "op", "fadm", "adm"},
			ActionWrite:   {}, // —
			ActionExecute: {}, // —
			ActionAdmin:   {"fadm", "adm"},
			ActionVerify:  {"aud", "adm"},
			ActionExport:  {"aud", "adm"},
		},
		ResourceInsight: {
			ActionRead:    {"ro", "aud", "op", "fadm", "adm"},
			ActionWrite:   {"fadm", "adm"},
			ActionExecute: {"fadm", "adm"},
			ActionAdmin:   {"adm"},
			ActionVerify:  {}, // —
			ActionExport:  {}, // —
		},
		ResourceAudit: {
			ActionRead:    {"aud", "adm"},
			ActionWrite:   {}, // —
			ActionExecute: {}, // —
			ActionAdmin:   {}, // —
			ActionVerify:  {"aud", "adm"},
			ActionExport:  {"aud", "adm"},
		},
		ResourceFleet: {
			ActionRead:    {"ro", "aud", "op", "fadm", "adm"},
			ActionWrite:   {"fadm", "adm"},
			ActionExecute: {}, // —
			ActionAdmin:   {"adm"},
			ActionVerify:  {}, // —
			ActionExport:  {}, // —
		},
		ResourceCompliance: {
			ActionRead:    {"ro", "aud", "op", "fadm", "adm"},
			ActionWrite:   {"adm"},
			ActionExecute: {"fadm", "adm"},
			ActionAdmin:   {"adm"},
			ActionVerify:  {}, // —
			ActionExport:  {"aud", "adm"},
		},
		ResourceTenantAdmin: {
			ActionRead:    {"adm"},
			ActionWrite:   {}, // —
			ActionExecute: {}, // —
			ActionAdmin:   {"adm"},
			ActionVerify:  {}, // —
			ActionExport:  {}, // —
		},
		ResourceSystem: {
			ActionRead:    {"aud", "adm"},
			ActionWrite:   {}, // —
			ActionExecute: {}, // —
			ActionAdmin:   {"adm"},
			ActionVerify:  {}, // —
			ActionExport:  {}, // —
		},
	}

	roles := append([]string{"own"}, matrix[resource][action]...) // owner는 항상 implicit.
	return roles
}

// shortToRoleName은 §3.3 약칭을 RoleName 상수로 변환합니다.
func shortToRoleName(short string) string {
	switch short {
	case "own":
		return RoleOwner
	case "adm":
		return RoleAdmin
	case "fadm":
		return RoleFleetAdmin
	case "op":
		return RoleOperator
	case "aud":
		return RoleAuditor
	case "ro":
		return RoleReadOnly
	default:
		panic("unknown role short: " + short)
	}
}

// makeBindingForRole은 role 단일 보유 Subject를 만듭니다 — fleet scope role은 fleet[A] binding.
func makeBindingForRole(roleName, fleetID string) []RoleBinding {
	if IsTenantScopedRole(roleName) {
		return []RoleBinding{{RoleName: roleName, ScopeType: ScopeTenant, ScopeID: ""}}
	}
	return []RoleBinding{{RoleName: roleName, ScopeType: ScopeFleet, ScopeID: fleetID}}
}

// TestDecisionTable_AllRoleResourceActionMatrix는 §3.3 매트릭스 6 role × 9 resource × 6 action
// = 324 case를 모두 검증합니다.
//
// 각 role 단일 보유 Subject로 모든 (resource, action) 쌍에 Decide 호출 — expected 매트릭스와 일치 검증.
// fleet scope role은 binding과 동일 fleet ID로 요청하여 매칭 통과 조건 형성.
func TestDecisionTable_AllRoleResourceActionMatrix(t *testing.T) {
	const fleetID = "flt_a"
	totalCases := 0
	for _, role := range AllSystemRoles() {
		for _, resource := range AllResources() {
			for _, action := range AllActions() {
				totalCases++
				expected := contains(expectedRolesForCell(resource, action), roleNameToShort(role))
				sub := Subject{
					Bindings: makeBindingForRole(role, fleetID),
					FleetID:  fleetID,
				}
				got := Decide(sub, resource, action)
				if got.Allow != expected {
					t.Errorf("role=%s resource=%s action=%s: expected Allow=%v, got Allow=%v reason=%q",
						role, resource, action, expected, got.Allow, got.Reason)
				}
			}
		}
	}
	if totalCases != 324 {
		t.Fatalf("expected 324 cases (6 role × 9 resource × 6 action), got %d", totalCases)
	}
}

// roleNameToShort는 RoleName 상수를 §3.3 약칭으로 변환합니다 (테스트 비교용).
func roleNameToShort(roleName string) string {
	switch roleName {
	case RoleOwner:
		return "own"
	case RoleAdmin:
		return "adm"
	case RoleFleetAdmin:
		return "fadm"
	case RoleOperator:
		return "op"
	case RoleAuditor:
		return "aud"
	case RoleReadOnly:
		return "ro"
	default:
		return ""
	}
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// TestPermissionImpliesWildcard는 owner가 보유한 단일 와일드카드 permission이 모든 (resource, action)
// 쌍에 대해 ALLOW 결정을 내는지 검증합니다 (§3.3 "owner는 모든 칸 implicit").
//
// 또한 부분 와일드카드(예: robot.*)도 검증하여 Permission.Matches 동작을 보장합니다.
func TestPermissionImpliesWildcard(t *testing.T) {
	t.Run("owner full wildcard allows all cells", func(t *testing.T) {
		sub := Subject{
			Bindings: []RoleBinding{{RoleName: RoleOwner, ScopeType: ScopeTenant}},
		}
		for _, resource := range AllResources() {
			for _, action := range AllActions() {
				got := Decide(sub, resource, action)
				if !got.Allow {
					t.Errorf("owner should allow %s.%s, got DENY reason=%q", resource, action, got.Reason)
				}
				if got.MatchedRole != RoleOwner {
					t.Errorf("expected MatchedRole=owner, got %q", got.MatchedRole)
				}
			}
		}
	})

	t.Run("partial wildcard resource only", func(t *testing.T) {
		// robot.* — 임의 action에 매치, 다른 resource 미매치.
		perm := Permission{Resource: ResourceRobot, Action: WildcardAction}
		for _, action := range AllActions() {
			if !perm.Matches(ResourceRobot, action) {
				t.Errorf("robot.* should match robot.%s", action)
			}
			if perm.Matches(ResourceScan, action) {
				t.Errorf("robot.* should NOT match scan.%s", action)
			}
		}
	})

	t.Run("partial wildcard action only", func(t *testing.T) {
		// *.read — 모든 resource의 read에 매치, 다른 action 미매치.
		perm := Permission{Resource: WildcardResource, Action: ActionRead}
		for _, resource := range AllResources() {
			if !perm.Matches(resource, ActionRead) {
				t.Errorf("*.read should match %s.read", resource)
			}
			if perm.Matches(resource, ActionWrite) {
				t.Errorf("*.read should NOT match %s.write", resource)
			}
		}
	})
}

// TestFleetScopeBeatsTenantDeny는 design doc §7 Stage 1의 핵심 결정 케이스를 검증합니다:
//
// 사용자가 다음 두 binding을 동시에 보유:
//   - fleet[A] operator (fleet scope)
//   - tenant scope read-only
//
// 일반적으로 read-only는 robot.write 미보유. operator는 fleet scope에서 robot.write 보유.
// → fleet[A] 컨텍스트에서 robot.write 요청 시 ALLOW (fleet scope binding이 더 강한 권한 부여).
//
// 추가로:
//   - fleet[B] 컨텍스트에서 robot.write 요청 시 DENY (operator binding은 fleet[A]에만 적용).
//   - tenant 글로벌 read 요청은 read-only가 처리 — fleet 무관 ALLOW.
func TestFleetScopeBeatsTenantDeny(t *testing.T) {
	bindings := []RoleBinding{
		{RoleName: RoleOperator, ScopeType: ScopeFleet, ScopeID: "flt_a"},
		{RoleName: RoleReadOnly, ScopeType: ScopeTenant, ScopeID: ""},
	}

	t.Run("fleet[A] write allowed by operator binding", func(t *testing.T) {
		sub := Subject{Bindings: bindings, FleetID: "flt_a"}
		got := Decide(sub, ResourceRobot, ActionWrite)
		if !got.Allow {
			t.Fatalf("expected ALLOW for fleet[A] robot.write, got DENY reason=%q", got.Reason)
		}
		if got.MatchedRole != RoleOperator {
			t.Errorf("expected MatchedRole=operator, got %q", got.MatchedRole)
		}
	})

	t.Run("fleet[B] write denied — operator binding only covers fleet[A]", func(t *testing.T) {
		sub := Subject{Bindings: bindings, FleetID: "flt_b"}
		got := Decide(sub, ResourceRobot, ActionWrite)
		if got.Allow {
			t.Fatalf("expected DENY for fleet[B] robot.write (operator binding is fleet[A] only), got ALLOW reason=%q", got.Reason)
		}
	})

	t.Run("read across all fleets — read-only tenant binding", func(t *testing.T) {
		// tenant scope read-only는 모든 fleet에서 read ALLOW.
		for _, fleetID := range []string{"flt_a", "flt_b", ""} {
			sub := Subject{Bindings: bindings, FleetID: fleetID}
			got := Decide(sub, ResourceRobot, ActionRead)
			if !got.Allow {
				t.Errorf("expected ALLOW for fleet=%q robot.read (tenant read-only), got DENY reason=%q",
					fleetID, got.Reason)
			}
		}
	})

	t.Run("no bindings → DENY", func(t *testing.T) {
		got := Decide(Subject{}, ResourceRobot, ActionRead)
		if got.Allow {
			t.Errorf("expected DENY for empty bindings, got ALLOW")
		}
	})

	t.Run("fleet binding without ScopeID is ignored", func(t *testing.T) {
		// 잘못된 binding(fleet scope인데 ScopeID 비어있음) — Decide는 skip해야 함.
		sub := Subject{
			Bindings: []RoleBinding{{RoleName: RoleOperator, ScopeType: ScopeFleet, ScopeID: ""}},
			FleetID:  "flt_a",
		}
		got := Decide(sub, ResourceRobot, ActionWrite)
		if got.Allow {
			t.Errorf("expected DENY for fleet binding with empty ScopeID, got ALLOW reason=%q", got.Reason)
		}
	})
}

// TestDecide_DiagnosticReason는 DENY reason 문자열에 요청 정보가 포함되는지 sanity check.
func TestDecide_DiagnosticReason(t *testing.T) {
	sub := Subject{
		Bindings: []RoleBinding{{RoleName: RoleReadOnly, ScopeType: ScopeTenant}},
		FleetID:  "flt_x",
	}
	got := Decide(sub, ResourceRobot, ActionWrite)
	if got.Allow {
		t.Fatalf("expected DENY")
	}
	expectFragment := fmt.Sprintf("%s.%s", ResourceRobot, ActionWrite)
	if !containsStr(got.Reason, expectFragment) {
		t.Errorf("expected reason to contain %q, got %q", expectFragment, got.Reason)
	}
}

func containsStr(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle || indexOf(haystack, needle) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
