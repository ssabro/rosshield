// 세분 RBAC Stage 2 — RoleBinding (scope_type/scope_id) 단위 테스트.
//
// design doc `docs/design/notes/rbac-fine-grained-design.md` §7 Stage 2 명시 3건:
//   - TestAssignRoleScoped_FleetBinding
//   - TestExistingTenantBindingPreserved
//   - TestCrossTenantScopeIsolation
//
// RBAC fleet 정밀화 Stage 5 추가:
//   - TestRevokeUserRoleBindingsBySource_OnlyMatchingSourceDeleted
//     (D-RBACEX-7 권장 default — manual 보존, sso만 revoke)
package sqliterepo_test

import (
	"context"
	"testing"

	"github.com/ssabro/rosshield/internal/domain/tenant"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// TestAssignRoleScoped_FleetBinding은 fleet scope binding을 할당·복원하는 정상 경로를
// 검증합니다 (design doc §7 Stage 2).
//
// 시나리오: tenant 생성 후 admin user에 추가로 fleet-admin role을 fleet[X] scope로 할당.
// GetUserRoleBindings로 복원 시 (fleet-admin, fleet, "flt_X") binding이 정확히 1건 보임.
func TestAssignRoleScoped_FleetBinding(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)

	// 1. tenant + admin user + 3 시스템 role 시드.
	var result tenant.CreateResult
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		r, err := repo.Create(ctx, tx, sampleCreate())
		result = r
		return err
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	tenantCtx := storage.WithTenantID(context.Background(), result.Tenant.ID)

	// 2. fleet-admin 시드는 본 Stage 범위 밖이므로 기존 operator role을 fleet scope으로 할당하여
	//    fleet binding 동작 자체를 검증.
	const targetFleetID = "flt_warehouse_a"
	if err := store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		operatorRole, err := repo.GetRole(ctx, tx, result.Tenant.ID, tenant.RoleOperator)
		if err != nil {
			return err
		}
		// fleet scope binding 할당 (manual source — admin 수동 의미).
		if err := repo.AssignRoleScoped(ctx, tx, result.Admin.ID, operatorRole.ID,
			tenant.ScopeFleet, targetFleetID, tenant.BindingSourceManual); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatalf("AssignRoleScoped: %v", err)
	}

	// 3. GetUserRoleBindings로 복원 — admin binding(tenant) + operator binding(fleet) 두 건.
	if err := store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		bindings, err := repo.GetUserRoleBindings(ctx, tx, result.Admin.ID)
		if err != nil {
			return err
		}
		if len(bindings) != 2 {
			t.Fatalf("got %d bindings, want 2 (admin tenant + operator fleet)", len(bindings))
		}

		var (
			adminTenantFound   bool
			operatorFleetFound bool
		)
		for _, b := range bindings {
			switch b.Role.Name {
			case tenant.RoleAdmin:
				adminTenantFound = true
				if b.ScopeType != tenant.ScopeTenant {
					t.Errorf("admin binding ScopeType = %q, want %q", b.ScopeType, tenant.ScopeTenant)
				}
				if b.ScopeID != "" {
					t.Errorf("admin binding ScopeID = %q, want empty", b.ScopeID)
				}
			case tenant.RoleOperator:
				operatorFleetFound = true
				if b.ScopeType != tenant.ScopeFleet {
					t.Errorf("operator binding ScopeType = %q, want %q", b.ScopeType, tenant.ScopeFleet)
				}
				if b.ScopeID != targetFleetID {
					t.Errorf("operator binding ScopeID = %q, want %q", b.ScopeID, targetFleetID)
				}
			default:
				t.Errorf("unexpected role binding: %+v", b)
			}
		}
		if !adminTenantFound {
			t.Error("admin tenant binding not found")
		}
		if !operatorFleetFound {
			t.Error("operator fleet binding not found")
		}
		return nil
	}); err != nil {
		t.Fatalf("GetUserRoleBindings: %v", err)
	}
}

// TestExistingTenantBindingPreserved는 0028 마이그레이션 이전 INSERT된 기존 user_roles row이
// 마이그레이션 후 자동으로 scope_type='tenant' / scope_id=” 로 분류되는지 검증합니다
// (design doc §7 Stage 2 + D-RBAC-5 권장 default).
//
// 본 테스트는 newTestRepo가 모든 마이그레이션을 일괄 적용하는 흐름에서 Create가 호출하는
// assignRole 헬퍼가 명시적으로 scope_type='tenant' / scope_id=” 를 INSERT하는지 검증하는
// 형태로 등가 — 결과 row가 GetUserRoleBindings에서 tenant scope binding으로 복원되면 OK.
//
// 추가로 raw SQL로 scope 컬럼 부재한 row를 INSERT하면 DEFAULT가 적용되는지 검증
// (마이그레이션 0028의 DEFAULT 'tenant' / ” 동작 검증).
func TestExistingTenantBindingPreserved(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)

	var result tenant.CreateResult
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		r, err := repo.Create(ctx, tx, sampleCreate())
		result = r
		return err
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	tenantCtx := storage.WithTenantID(context.Background(), result.Tenant.ID)

	// 1. Create가 INSERT한 admin user_roles row는 자동으로 tenant scope여야 함.
	if err := store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		bindings, err := repo.GetUserRoleBindings(ctx, tx, result.Admin.ID)
		if err != nil {
			return err
		}
		if len(bindings) != 1 {
			t.Fatalf("got %d bindings, want 1 (admin tenant)", len(bindings))
		}
		b := bindings[0]
		if b.Role.Name != tenant.RoleAdmin {
			t.Errorf("binding role = %q, want admin", b.Role.Name)
		}
		if b.ScopeType != tenant.ScopeTenant {
			t.Errorf("binding ScopeType = %q, want %q", b.ScopeType, tenant.ScopeTenant)
		}
		if b.ScopeID != "" {
			t.Errorf("binding ScopeID = %q, want empty", b.ScopeID)
		}
		return nil
	}); err != nil {
		t.Fatalf("first GetUserRoleBindings: %v", err)
	}

	// 2. scope 컬럼 미지정 raw INSERT — DEFAULT 'tenant' / '' 가 적용되는지 검증.
	//    기존 row(0028 이전 INSERT) 가상 시뮬레이션.
	if err := store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		// 별 user + auditor role 할당 — 명시적 컬럼 list 없이 INSERT.
		auditorRole, err := repo.GetRole(ctx, tx, result.Tenant.ID, tenant.RoleAuditor)
		if err != nil {
			return err
		}
		// raw INSERT — 기존 row 형식 (scope 컬럼 명시 안 함, DEFAULT 적용 기대).
		if _, err := tx.Exec(ctx,
			`INSERT INTO user_roles (user_id, role_id) VALUES (?, ?)`,
			result.Admin.ID, auditorRole.ID); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatalf("raw INSERT: %v", err)
	}

	// 3. 마이그레이션 0028 DEFAULT 적용 결과 — auditor binding도 tenant scope.
	if err := store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		bindings, err := repo.GetUserRoleBindings(ctx, tx, result.Admin.ID)
		if err != nil {
			return err
		}
		if len(bindings) != 2 {
			t.Fatalf("got %d bindings, want 2 (admin + auditor)", len(bindings))
		}
		for _, b := range bindings {
			if b.ScopeType != tenant.ScopeTenant {
				t.Errorf("binding %q ScopeType = %q, want %q (DEFAULT 'tenant')",
					b.Role.Name, b.ScopeType, tenant.ScopeTenant)
			}
			if b.ScopeID != "" {
				t.Errorf("binding %q ScopeID = %q, want empty (DEFAULT '')",
					b.Role.Name, b.ScopeID)
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("post-default GetUserRoleBindings: %v", err)
	}
}

// TestCrossTenantScopeIsolation은 fleet scope binding이 tenant 경계를 가로지르지 않는지
// 검증합니다 — fleet[X]@tenant_A 할당 시 tenant_B 사용자는 fleet[X] 권한 0 (design doc §7 Stage 2).
//
// 시나리오:
//  1. tenant_A + admin_A 생성, operator role을 fleet[X] scope로 admin_A에 할당.
//  2. tenant_B + admin_B 생성.
//  3. admin_B의 GetUserRoleBindings로는 fleet[X] binding이 보이지 않음 (admin_B는
//     자신의 tenant scope admin binding 1건만 보유).
//
// user_roles는 user_id로 격리되므로 cross-tenant 격리는 자명하지만, 명시 검증으로
// 회귀 방지 (handlers·middleware가 tenant filter를 깜빡 누락하는 시나리오 대비).
func TestCrossTenantScopeIsolation(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)

	// 1. tenant_A 생성 + operator를 fleet[X] scope로 admin_A에 할당.
	var tenantA tenant.CreateResult
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		r, err := repo.Create(ctx, tx, tenant.CreateRequest{
			Name:             "TenantA",
			Plan:             tenant.PlanDesktopFree,
			AdminEmail:       "admin@a.example",
			AdminPassword:    "long-enough-secret-pw",
			AdminDisplayName: "Tenant A Admin",
		})
		tenantA = r
		return err
	}); err != nil {
		t.Fatalf("Create tenant A: %v", err)
	}
	const sharedFleetID = "flt_X"
	tenantACtx := storage.WithTenantID(context.Background(), tenantA.Tenant.ID)
	if err := store.Tx(tenantACtx, func(ctx context.Context, tx storage.Tx) error {
		operatorA, err := repo.GetRole(ctx, tx, tenantA.Tenant.ID, tenant.RoleOperator)
		if err != nil {
			return err
		}
		return repo.AssignRoleScoped(ctx, tx, tenantA.Admin.ID, operatorA.ID,
			tenant.ScopeFleet, sharedFleetID, tenant.BindingSourceManual)
	}); err != nil {
		t.Fatalf("AssignRoleScoped tenant A: %v", err)
	}

	// 2. tenant_B 생성.
	var tenantB tenant.CreateResult
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		r, err := repo.Create(ctx, tx, tenant.CreateRequest{
			Name:             "TenantB",
			Plan:             tenant.PlanDesktopFree,
			AdminEmail:       "admin@b.example",
			AdminPassword:    "long-enough-secret-pw",
			AdminDisplayName: "Tenant B Admin",
		})
		tenantB = r
		return err
	}); err != nil {
		t.Fatalf("Create tenant B: %v", err)
	}

	// 3. admin_B는 자신의 tenant scope admin binding 1건만 보유 — fleet[X] binding 0.
	tenantBCtx := storage.WithTenantID(context.Background(), tenantB.Tenant.ID)
	if err := store.Tx(tenantBCtx, func(ctx context.Context, tx storage.Tx) error {
		bindings, err := repo.GetUserRoleBindings(ctx, tx, tenantB.Admin.ID)
		if err != nil {
			return err
		}
		if len(bindings) != 1 {
			t.Fatalf("tenant B admin: got %d bindings, want 1", len(bindings))
		}
		b := bindings[0]
		if b.Role.Name != tenant.RoleAdmin {
			t.Errorf("tenant B admin binding role = %q, want admin", b.Role.Name)
		}
		if b.ScopeType != tenant.ScopeTenant {
			t.Errorf("tenant B admin ScopeType = %q, want tenant", b.ScopeType)
		}
		// tenant B admin role은 tenant_B에 속해야 함.
		if b.Role.TenantID != tenantB.Tenant.ID {
			t.Errorf("tenant B admin role TenantID = %q, want %q", b.Role.TenantID, tenantB.Tenant.ID)
		}
		return nil
	}); err != nil {
		t.Fatalf("tenant B GetUserRoleBindings: %v", err)
	}

	// 4. 역으로 admin_A는 tenant_A 컨텍스트에서 admin(tenant) + operator(fleet[X]) 2건 보유.
	if err := store.Tx(tenantACtx, func(ctx context.Context, tx storage.Tx) error {
		bindings, err := repo.GetUserRoleBindings(ctx, tx, tenantA.Admin.ID)
		if err != nil {
			return err
		}
		if len(bindings) != 2 {
			t.Fatalf("tenant A admin: got %d bindings, want 2", len(bindings))
		}
		var fleetBindingFound bool
		for _, b := range bindings {
			if b.Role.Name == tenant.RoleOperator {
				fleetBindingFound = true
				if b.ScopeType != tenant.ScopeFleet || b.ScopeID != sharedFleetID {
					t.Errorf("tenant A operator binding = (%q, %q), want (fleet, %q)",
						b.ScopeType, b.ScopeID, sharedFleetID)
				}
			}
		}
		if !fleetBindingFound {
			t.Error("tenant A operator fleet binding not found")
		}
		return nil
	}); err != nil {
		t.Fatalf("tenant A GetUserRoleBindings: %v", err)
	}
}

// TestRevokeUserRoleBindingsBySource_OnlyMatchingSourceDeleted는 SSO callback sync 흐름의
// 핵심 D-RBACEX-7 권장 default (manual 보존 + sso만 revoke)를 검증합니다 — RBAC fleet
// 정밀화 Stage 5.
//
// 시나리오:
//
//  1. tenant + admin 시드 (admin은 manual binding 1건).
//  2. operator(manual tenant) + auditor(sso fleet) 추가 할당 — 3건 binding.
//  3. RevokeUserRoleBindingsBySource(userID, BindingSourceSSO) — sso 1건 revoke.
//  4. 사후 검증 — manual 2건 보존.
//  5. 두 번째 호출 — 멱등 (0건 삭제).
//  6. 빈 userID — 안전 noop.
func TestRevokeUserRoleBindingsBySource_OnlyMatchingSourceDeleted(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)

	var result tenant.CreateResult
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		r, err := repo.Create(ctx, tx, sampleCreate())
		result = r
		return err
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	tenantCtx := storage.WithTenantID(context.Background(), result.Tenant.ID)

	const ssoFleetID = "flt_sso_warehouse"
	if err := store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		operatorRole, err := repo.GetRole(ctx, tx, result.Tenant.ID, tenant.RoleOperator)
		if err != nil {
			return err
		}
		auditorRole, err := repo.GetRole(ctx, tx, result.Tenant.ID, tenant.RoleAuditor)
		if err != nil {
			return err
		}
		if err := repo.AssignRoleScoped(ctx, tx, result.Admin.ID, operatorRole.ID,
			tenant.ScopeTenant, "", tenant.BindingSourceManual); err != nil {
			return err
		}
		if err := repo.AssignRoleScoped(ctx, tx, result.Admin.ID, auditorRole.ID,
			tenant.ScopeFleet, ssoFleetID, tenant.BindingSourceSSO); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatalf("seed bindings: %v", err)
	}

	// 사전 — 3건 binding.
	if err := store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		bindings, err := repo.GetUserRoleBindings(ctx, tx, result.Admin.ID)
		if err != nil {
			return err
		}
		if len(bindings) != 3 {
			t.Fatalf("pre-revoke len=%d want 3", len(bindings))
		}
		return nil
	}); err != nil {
		t.Fatalf("pre-revoke GetUserRoleBindings: %v", err)
	}

	// SSO sync 시뮬레이션.
	var revoked int
	if err := store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		n, err := repo.RevokeUserRoleBindingsBySource(ctx, tx, result.Admin.ID, tenant.BindingSourceSSO)
		revoked = n
		return err
	}); err != nil {
		t.Fatalf("RevokeUserRoleBindingsBySource: %v", err)
	}
	if revoked != 1 {
		t.Errorf("revoked=%d want=1 (only auditor sso)", revoked)
	}

	// 사후 — manual 2건만 보존.
	if err := store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		bindings, err := repo.GetUserRoleBindings(ctx, tx, result.Admin.ID)
		if err != nil {
			return err
		}
		if len(bindings) != 2 {
			t.Fatalf("post-revoke len=%d want 2", len(bindings))
		}
		for _, b := range bindings {
			if b.Source != tenant.BindingSourceManual {
				t.Errorf("post-revoke binding %q Source=%q want manual", b.Role.Name, b.Source)
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("post-revoke GetUserRoleBindings: %v", err)
	}

	// 멱등 검증.
	if err := store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		n, err := repo.RevokeUserRoleBindingsBySource(ctx, tx, result.Admin.ID, tenant.BindingSourceSSO)
		if err != nil {
			return err
		}
		if n != 0 {
			t.Errorf("second revoke n=%d want=0 (idempotent)", n)
		}
		return nil
	}); err != nil {
		t.Fatalf("second revoke: %v", err)
	}

	// 빈 userID는 안전 noop.
	if err := store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		n, err := repo.RevokeUserRoleBindingsBySource(ctx, tx, "", tenant.BindingSourceSSO)
		if err != nil {
			return err
		}
		if n != 0 {
			t.Errorf("empty userID n=%d want=0", n)
		}
		return nil
	}); err != nil {
		t.Fatalf("empty userID: %v", err)
	}
}
