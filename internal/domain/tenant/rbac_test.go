package tenant_test

import (
	"testing"

	"github.com/ssabro/rosshield/internal/domain/tenant"
)

// E3.T7 본체.
func TestRBACPermissionCheck(t *testing.T) {
	t.Parallel()

	admin := tenant.Role{
		Name:        tenant.RoleAdmin,
		Permissions: tenant.SystemRolePermissions[tenant.RoleAdmin],
	}
	auditor := tenant.Role{
		Name:        tenant.RoleAuditor,
		Permissions: tenant.SystemRolePermissions[tenant.RoleAuditor],
	}
	operator := tenant.Role{
		Name:        tenant.RoleOperator,
		Permissions: tenant.SystemRolePermissions[tenant.RoleOperator],
	}

	cases := []struct {
		name string
		role tenant.Role
		perm tenant.Permission
		want bool
	}{
		{"admin has wildcard for robot.write", admin, tenant.PermRobotWrite, true},
		{"admin has wildcard for any.unknown", admin, tenant.Permission("any.unknown"), true},
		{"auditor has audit.export", auditor, tenant.PermAuditExport, true},
		{"auditor lacks robot.write", auditor, tenant.PermRobotWrite, false},
		{"auditor lacks scan.execute", auditor, tenant.PermScanExecute, false},
		{"operator has scan.execute", operator, tenant.PermScanExecute, true},
		{"operator lacks audit.export", operator, tenant.PermAuditExport, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.role.HasPermission(tc.perm)
			if got != tc.want {
				t.Errorf("HasPermission(%q) = %v, want %v", tc.perm, got, tc.want)
			}
		})
	}
}

func TestRBACAnyHasPermissionAcrossRoles(t *testing.T) {
	t.Parallel()

	roles := []tenant.Role{
		{Name: "auditor", Permissions: tenant.SystemRolePermissions[tenant.RoleAuditor]},
		{Name: "operator", Permissions: tenant.SystemRolePermissions[tenant.RoleOperator]},
	}

	if !tenant.AnyHasPermission(roles, tenant.PermAuditExport) {
		t.Error("auditor role should grant audit.export")
	}
	if !tenant.AnyHasPermission(roles, tenant.PermScanExecute) {
		t.Error("operator role should grant scan.execute")
	}
	if tenant.AnyHasPermission(roles, tenant.PermReportSign) {
		t.Error("neither role grants report.sign — should return false")
	}
	if tenant.AnyHasPermission(nil, tenant.PermRobotRead) {
		t.Error("empty roles should never grant any permission")
	}
}

func TestSystemRoleSetsAreDefined(t *testing.T) {
	t.Parallel()

	for _, name := range []string{tenant.RoleAdmin, tenant.RoleAuditor, tenant.RoleOperator} {
		perms, ok := tenant.SystemRolePermissions[name]
		if !ok || len(perms) == 0 {
			t.Errorf("system role %q has no permissions defined", name)
		}
	}
}
