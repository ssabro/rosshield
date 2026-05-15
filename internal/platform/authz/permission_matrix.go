// permission_matrix.go — design doc §3.3 결정 테이블의 코드 표현입니다.
//
// 6 시스템 role × 9 resource × 6 action 매트릭스를 SystemRolePermissions map으로 표현합니다.
// owner는 단일 와일드카드 permission만 보유 (모든 칸 implicit 통과).
// admin은 거의 모든 admin/write/execute 권한 — 단 매트릭스에서 명시되지 않은 칸(예: scan.write)은 미보유.
//
// 본 매트릭스는 §3.3 cell 표기를 그대로 옮긴 것 — "ro/aud/op/fadm/adm" 표기를 각 role별
// permission 리스트로 분해. 시각적 검증 가능하도록 cell별 코멘트 첨부.

package authz

// SystemRolePermissions는 시스템 role 6개의 정적 permission 셋입니다.
//
// design doc §3.3 매트릭스 정확 일치. 매트릭스 cell이 "—" 인 (resource, action)은 어떤 role의
// permission 리스트에도 등장하지 않습니다 — 즉 owner를 제외한 어느 role도 해당 칸 미통과.
//
// owner는 단일 와일드카드 permission `{*, *}` 으로 모든 칸 implicit 통과 — design doc §3.3
// "owner는 모든 칸 implicit 포함" 직역.
var SystemRolePermissions = map[string][]Permission{

	// owner — 모든 (resource, action) implicit 통과 (§3.1).
	RoleOwner: {
		{Resource: WildcardResource, Action: WildcardAction},
	},

	// admin — tenant 글로벌 관리. §3.3 매트릭스 "adm" 등장 cell 모두 보유.
	//   robot:    read/write/admin/export
	//   scan:     read/execute/admin/export
	//   report:   read/admin/verify/export
	//   insight:  read/write/execute/admin
	//   audit:    read/verify/export
	//   fleet:    read/write/admin
	//   compliance: read/write/execute/admin/export
	//   tenant_admin (sso·webhook·users·invitation): read/admin
	//   system:   read/admin
	RoleAdmin: {
		{ResourceRobot, ActionRead}, {ResourceRobot, ActionWrite}, {ResourceRobot, ActionAdmin}, {ResourceRobot, ActionExport},
		{ResourceScan, ActionRead}, {ResourceScan, ActionExecute}, {ResourceScan, ActionAdmin}, {ResourceScan, ActionExport},
		{ResourceReport, ActionRead}, {ResourceReport, ActionAdmin}, {ResourceReport, ActionVerify}, {ResourceReport, ActionExport},
		{ResourceInsight, ActionRead}, {ResourceInsight, ActionWrite}, {ResourceInsight, ActionExecute}, {ResourceInsight, ActionAdmin},
		{ResourceAudit, ActionRead}, {ResourceAudit, ActionVerify}, {ResourceAudit, ActionExport},
		{ResourceFleet, ActionRead}, {ResourceFleet, ActionWrite}, {ResourceFleet, ActionAdmin},
		{ResourceCompliance, ActionRead}, {ResourceCompliance, ActionWrite}, {ResourceCompliance, ActionExecute}, {ResourceCompliance, ActionAdmin}, {ResourceCompliance, ActionExport},
		{ResourceTenantAdmin, ActionRead}, {ResourceTenantAdmin, ActionAdmin},
		{ResourceSystem, ActionRead}, {ResourceSystem, ActionAdmin},
	},

	// fleet-admin — 특정 fleet 한정 admin. §3.3 매트릭스 "fadm" 등장 cell.
	//   robot:    read/write/admin
	//   scan:     read/execute/admin
	//   report:   read/admin
	//   insight:  read/write/execute
	//   fleet:    read/write (settings)
	//   compliance: read/execute
	// audit·tenant_admin·system은 fadm 미등장 — fleet-admin은 tenant 관리 권한 0.
	RoleFleetAdmin: {
		{ResourceRobot, ActionRead}, {ResourceRobot, ActionWrite}, {ResourceRobot, ActionAdmin},
		{ResourceScan, ActionRead}, {ResourceScan, ActionExecute}, {ResourceScan, ActionAdmin},
		{ResourceReport, ActionRead}, {ResourceReport, ActionAdmin},
		{ResourceInsight, ActionRead}, {ResourceInsight, ActionWrite}, {ResourceInsight, ActionExecute},
		{ResourceFleet, ActionRead}, {ResourceFleet, ActionWrite},
		{ResourceCompliance, ActionRead}, {ResourceCompliance, ActionExecute},
	},

	// operator — fleet 한정 일상 운영. §3.3 매트릭스 "op" 등장 cell.
	//   robot:    read/write
	//   scan:     read/execute
	//   report:   read
	//   insight:  read
	//   fleet:    read
	//   compliance: read
	// audit·tenant_admin·system은 op 미등장.
	RoleOperator: {
		{ResourceRobot, ActionRead}, {ResourceRobot, ActionWrite},
		{ResourceScan, ActionRead}, {ResourceScan, ActionExecute},
		{ResourceReport, ActionRead},
		{ResourceInsight, ActionRead},
		{ResourceFleet, ActionRead},
		{ResourceCompliance, ActionRead},
	},

	// auditor — tenant 글로벌 read-only + verify/export. §3.3 매트릭스 "aud" 등장 cell.
	//   robot:    read/export
	//   scan:     read/export
	//   report:   read/verify/export
	//   insight:  read
	//   audit:    read/verify/export
	//   fleet:    read
	//   compliance: read/export
	//   system:   read
	// tenant_admin은 aud 미등장 — auditor는 sso/webhook/users 관리 0.
	RoleAuditor: {
		{ResourceRobot, ActionRead}, {ResourceRobot, ActionExport},
		{ResourceScan, ActionRead}, {ResourceScan, ActionExport},
		{ResourceReport, ActionRead}, {ResourceReport, ActionVerify}, {ResourceReport, ActionExport},
		{ResourceInsight, ActionRead},
		{ResourceAudit, ActionRead}, {ResourceAudit, ActionVerify}, {ResourceAudit, ActionExport},
		{ResourceFleet, ActionRead},
		{ResourceCompliance, ActionRead}, {ResourceCompliance, ActionExport},
		{ResourceSystem, ActionRead},
	},

	// read-only — tenant 글로벌 read-only. §3.3 매트릭스 "ro" 등장 cell.
	//   robot/scan/report/insight/fleet/compliance: read만.
	// audit·tenant_admin·system은 ro 미등장 (aud 묶음).
	RoleReadOnly: {
		{ResourceRobot, ActionRead},
		{ResourceScan, ActionRead},
		{ResourceReport, ActionRead},
		{ResourceInsight, ActionRead},
		{ResourceFleet, ActionRead},
		{ResourceCompliance, ActionRead},
	},
}

// IsTenantScopedRole는 role이 tenant 글로벌(모든 fleet implicit)인지 반환합니다 (§3.2).
//
// owner/admin/auditor/read-only — tenant scope. fleet-admin/operator — fleet scope.
// 사용자 정의 role은 호출자가 binding ScopeType으로 명시.
func IsTenantScopedRole(roleName string) bool {
	switch roleName {
	case RoleOwner, RoleAdmin, RoleAuditor, RoleReadOnly:
		return true
	default:
		return false
	}
}
