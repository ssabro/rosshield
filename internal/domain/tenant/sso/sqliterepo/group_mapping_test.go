package sqliterepo_test

// group_mapping_test.go — RBAC fleet 정밀화 Stage 4 SSO group 매핑 단위 테스트.
//
// design doc `docs/design/notes/rbac-fleet-scope-precision-design.md` §7 Stage 4 명시:
//   - TestCreateGroupMapping_TenantScopeDefault — scope_type 빈 값 → 'tenant' default.
//   - TestCreateGroupMapping_FleetScopeRequiresScopeID — scope_type='fleet' + scope_id 빈 값 → ErrEmptyScopeIDForFleet.
//   - TestCreateGroupMapping_DuplicateRejected — 같은 5-tuple 중복 INSERT → ErrGroupMappingExists.
//   - TestCreateGroupMapping_CrossTenantRoleRejected — 다른 tenant의 role_id → ErrRoleNotFoundForTenant.
//   - TestCreateGroupMapping_CrossTenantProviderMasked — 다른 tenant의 provider_id → ErrProviderNotFound.
//   - TestListGroupMappings_OrdersByCreatedAt — provider scope ASC.
//   - TestDeleteGroupMapping_TenantScoped — cross-tenant DELETE 시도 → ErrGroupMappingNotFound.
//   - TestResolveBindingsForGroups_DeduplicatesAndIgnoresUnknown.
//   - TestResolveBindingsForGroups_EmptyGroupsReturnsNil.

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/domain/tenant/sso"
	"github.com/ssabro/rosshield/internal/domain/tenant/sso/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/idgen"
	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/sqlite"
)

// === harness — group mapping 전용 (roles 시드 추가) ===

const (
	gmTenantA = "tn_GMA"
	gmTenantB = "tn_GMB"
	gmUserA   = "us_GMA"
	gmUserB   = "us_GMB"
	gmRoleAID = "rl_GMA_admin"
	gmRoleBID = "rl_GMB_admin"
)

type gmHarness struct {
	repo  *sqliterepo.Repo
	store storage.Storage
	clock *stepClock
	em    *fakeAuditEmitter
}

func newGroupMappingHarness(t *testing.T) *gmHarness {
	t.Helper()
	dir := t.TempDir()
	store, err := sqlite.Open(storage.Config{Driver: "sqlite", DSN: filepath.Join(dir, "gm.db")})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		// 두 tenant + user + role 시드 — cross-tenant 격리 검증용.
		for _, tn := range []string{gmTenantA, gmTenantB} {
			if _, e := tx.Exec(ctx, `INSERT INTO tenants (id, name, plan, created_at) VALUES (?, ?, 'desktop_free', ?)`,
				tn, "tenant-"+tn, now); e != nil {
				return e
			}
		}
		users := []struct{ id, tid string }{{gmUserA, gmTenantA}, {gmUserB, gmTenantB}}
		for _, u := range users {
			if _, e := tx.Exec(ctx, `INSERT INTO users (id, tenant_id, email, display_name, auth_provider, status, created_at, updated_at)
VALUES (?, ?, ?, 'U', 'oidc', 'active', ?, ?)`, u.id, u.tid, u.id+"@x.test", now, now); e != nil {
				return e
			}
		}
		roles := []struct{ id, tid string }{{gmRoleAID, gmTenantA}, {gmRoleBID, gmTenantB}}
		for _, r := range roles {
			if _, e := tx.Exec(ctx, `INSERT INTO roles (id, tenant_id, name, permissions, is_system, created_at)
VALUES (?, ?, 'admin', '["*"]', 1, ?)`, r.id, r.tid, now); e != nil {
				return e
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	em := &fakeAuditEmitter{}
	clk := newStepClock(time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC))
	repo := sqliterepo.New(sqliterepo.Deps{
		Clock: clk,
		IDGen: idgen.NewULID(),
		Audit: em,
	})
	return &gmHarness{repo: repo, store: store, clock: clk, em: em}
}

// createTestProvider는 tenantID에 OIDC provider 1건을 INSERT하고 ID를 반환합니다.
func (h *gmHarness) createTestProvider(t *testing.T, tenantID string) string {
	t.Helper()
	var pid string
	if err := h.store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		p, e := h.repo.CreateProvider(ctx, tx, sso.CreateProviderRequest{
			TenantID: storage.TenantID(tenantID),
			Type:     sso.TypeOIDC,
			Name:     "Provider-" + tenantID,
			Enabled:  true,
			Config:   json.RawMessage(`{"issuer":"https://accounts.google.com","clientId":"abc","redirectUri":"https://app/callback"}`),
		})
		pid = p.ID
		return e
	}); err != nil {
		t.Fatalf("createTestProvider: %v", err)
	}
	return pid
}

// === CreateGroupMapping ===

func TestCreateGroupMapping_TenantScopeDefault(t *testing.T) {
	t.Parallel()
	h := newGroupMappingHarness(t)
	pid := h.createTestProvider(t, gmTenantA)

	var m sso.GroupRoleMapping
	if err := h.store.Tx(tenantCtx(gmTenantA), func(ctx context.Context, tx storage.Tx) error {
		out, e := h.repo.CreateGroupMapping(ctx, tx, sso.CreateGroupMappingRequest{
			ProviderID: pid,
			GroupValue: "platform-admins",
			RoleID:     gmRoleAID,
			// ScopeType 빈 값 → 'tenant' default.
			// ScopeID 빈 값.
		})
		m = out
		return e
	}); err != nil {
		t.Fatalf("CreateGroupMapping: %v", err)
	}

	if !strings.HasPrefix(m.ID, "sgm_") {
		t.Errorf("ID = %q, want sgm_ prefix", m.ID)
	}
	if m.ScopeType != "tenant" {
		t.Errorf("ScopeType = %q, want tenant", m.ScopeType)
	}
	if m.ScopeID != "" {
		t.Errorf("ScopeID = %q, want empty (tenant scope)", m.ScopeID)
	}
	if m.GroupValue != "platform-admins" {
		t.Errorf("GroupValue = %q, want platform-admins", m.GroupValue)
	}
}

func TestCreateGroupMapping_FleetScopeRequiresScopeID(t *testing.T) {
	t.Parallel()
	h := newGroupMappingHarness(t)
	pid := h.createTestProvider(t, gmTenantA)

	if err := h.store.Tx(tenantCtx(gmTenantA), func(ctx context.Context, tx storage.Tx) error {
		_, e := h.repo.CreateGroupMapping(ctx, tx, sso.CreateGroupMappingRequest{
			ProviderID: pid,
			GroupValue: "fleet-admins-A",
			RoleID:     gmRoleAID,
			ScopeType:  "fleet",
			// ScopeID 빈 값 → 거부.
		})
		return e
	}); !errors.Is(err, sso.ErrEmptyScopeIDForFleet) {
		t.Errorf("err = %v, want ErrEmptyScopeIDForFleet", err)
	}

	// 정상 fleet scope.
	if err := h.store.Tx(tenantCtx(gmTenantA), func(ctx context.Context, tx storage.Tx) error {
		m, e := h.repo.CreateGroupMapping(ctx, tx, sso.CreateGroupMappingRequest{
			ProviderID: pid,
			GroupValue: "fleet-admins-A",
			RoleID:     gmRoleAID,
			ScopeType:  "fleet",
			ScopeID:    "flt_warehouse_a",
		})
		if e != nil {
			return e
		}
		if m.ScopeType != "fleet" || m.ScopeID != "flt_warehouse_a" {
			t.Errorf("got (%q, %q), want (fleet, flt_warehouse_a)", m.ScopeType, m.ScopeID)
		}
		return nil
	}); err != nil {
		t.Fatalf("CreateGroupMapping fleet: %v", err)
	}
}

func TestCreateGroupMapping_DuplicateRejected(t *testing.T) {
	t.Parallel()
	h := newGroupMappingHarness(t)
	pid := h.createTestProvider(t, gmTenantA)

	first := func() error {
		return h.store.Tx(tenantCtx(gmTenantA), func(ctx context.Context, tx storage.Tx) error {
			_, e := h.repo.CreateGroupMapping(ctx, tx, sso.CreateGroupMappingRequest{
				ProviderID: pid,
				GroupValue: "dup-group",
				RoleID:     gmRoleAID,
				ScopeType:  "tenant",
			})
			return e
		})
	}
	if err := first(); err != nil {
		t.Fatalf("first CreateGroupMapping: %v", err)
	}
	// 같은 5-tuple 두 번째 INSERT → ErrGroupMappingExists.
	if err := first(); !errors.Is(err, sso.ErrGroupMappingExists) {
		t.Errorf("second err = %v, want ErrGroupMappingExists", err)
	}
}

func TestCreateGroupMapping_CrossTenantRoleRejected(t *testing.T) {
	t.Parallel()
	h := newGroupMappingHarness(t)
	pid := h.createTestProvider(t, gmTenantA)

	// tenant_A의 provider + tenant_B의 role → 거부.
	if err := h.store.Tx(tenantCtx(gmTenantA), func(ctx context.Context, tx storage.Tx) error {
		_, e := h.repo.CreateGroupMapping(ctx, tx, sso.CreateGroupMappingRequest{
			ProviderID: pid,
			GroupValue: "cross-tenant-role",
			RoleID:     gmRoleBID, // tenant_B의 role!
			ScopeType:  "tenant",
		})
		return e
	}); !errors.Is(err, sso.ErrRoleNotFoundForTenant) {
		t.Errorf("err = %v, want ErrRoleNotFoundForTenant", err)
	}
}

func TestCreateGroupMapping_CrossTenantProviderMasked(t *testing.T) {
	t.Parallel()
	h := newGroupMappingHarness(t)
	pidA := h.createTestProvider(t, gmTenantA)

	// tenant_A의 provider를 tenant_B 컨텍스트에서 사용 → 마스킹 (ErrProviderNotFound).
	if err := h.store.Tx(tenantCtx(gmTenantB), func(ctx context.Context, tx storage.Tx) error {
		_, e := h.repo.CreateGroupMapping(ctx, tx, sso.CreateGroupMappingRequest{
			ProviderID: pidA,
			GroupValue: "x",
			RoleID:     gmRoleBID,
			ScopeType:  "tenant",
		})
		return e
	}); !errors.Is(err, sso.ErrProviderNotFound) {
		t.Errorf("err = %v, want ErrProviderNotFound", err)
	}
}

func TestCreateGroupMapping_RequiresGroupValueAndRoleID(t *testing.T) {
	t.Parallel()
	h := newGroupMappingHarness(t)
	pid := h.createTestProvider(t, gmTenantA)

	cases := []struct {
		name string
		req  sso.CreateGroupMappingRequest
		want error
	}{
		{"empty group value", sso.CreateGroupMappingRequest{ProviderID: pid, GroupValue: "  ", RoleID: gmRoleAID}, sso.ErrEmptyGroupValue},
		{"empty role id", sso.CreateGroupMappingRequest{ProviderID: pid, GroupValue: "g", RoleID: ""}, sso.ErrEmptyRoleID},
		{"invalid scope type", sso.CreateGroupMappingRequest{ProviderID: pid, GroupValue: "g", RoleID: gmRoleAID, ScopeType: "global"}, sso.ErrInvalidScopeType},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			err := h.store.Tx(tenantCtx(gmTenantA), func(ctx context.Context, tx storage.Tx) error {
				_, e := h.repo.CreateGroupMapping(ctx, tx, c.req)
				return e
			})
			if !errors.Is(err, c.want) {
				t.Errorf("err = %v, want %v", err, c.want)
			}
		})
	}
}

// === ListGroupMappings ===

func TestListGroupMappings_OrdersByCreatedAt(t *testing.T) {
	t.Parallel()
	h := newGroupMappingHarness(t)
	pid := h.createTestProvider(t, gmTenantA)

	// 3건 시간 진행하며 INSERT.
	groups := []string{"g-first", "g-second", "g-third"}
	for _, g := range groups {
		g := g
		if err := h.store.Tx(tenantCtx(gmTenantA), func(ctx context.Context, tx storage.Tx) error {
			_, e := h.repo.CreateGroupMapping(ctx, tx, sso.CreateGroupMappingRequest{
				ProviderID: pid,
				GroupValue: g,
				RoleID:     gmRoleAID,
				ScopeType:  "tenant",
			})
			return e
		}); err != nil {
			t.Fatalf("CreateGroupMapping %q: %v", g, err)
		}
		h.clock.Advance(time.Second)
	}

	if err := h.store.Tx(tenantCtx(gmTenantA), func(ctx context.Context, tx storage.Tx) error {
		out, e := h.repo.ListGroupMappings(ctx, tx, pid)
		if e != nil {
			return e
		}
		if len(out) != 3 {
			t.Fatalf("len = %d, want 3", len(out))
		}
		for i, expectGroup := range groups {
			if out[i].GroupValue != expectGroup {
				t.Errorf("out[%d].GroupValue = %q, want %q", i, out[i].GroupValue, expectGroup)
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("ListGroupMappings: %v", err)
	}
}

func TestListGroupMappings_CrossTenantProviderMasked(t *testing.T) {
	t.Parallel()
	h := newGroupMappingHarness(t)
	pidA := h.createTestProvider(t, gmTenantA)

	if err := h.store.Tx(tenantCtx(gmTenantB), func(ctx context.Context, tx storage.Tx) error {
		_, e := h.repo.ListGroupMappings(ctx, tx, pidA)
		return e
	}); !errors.Is(err, sso.ErrProviderNotFound) {
		t.Errorf("err = %v, want ErrProviderNotFound", err)
	}
}

// === DeleteGroupMapping ===

func TestDeleteGroupMapping_TenantScoped(t *testing.T) {
	t.Parallel()
	h := newGroupMappingHarness(t)
	pid := h.createTestProvider(t, gmTenantA)

	var mid string
	if err := h.store.Tx(tenantCtx(gmTenantA), func(ctx context.Context, tx storage.Tx) error {
		m, e := h.repo.CreateGroupMapping(ctx, tx, sso.CreateGroupMappingRequest{
			ProviderID: pid,
			GroupValue: "to-be-deleted",
			RoleID:     gmRoleAID,
			ScopeType:  "tenant",
		})
		mid = m.ID
		return e
	}); err != nil {
		t.Fatalf("CreateGroupMapping: %v", err)
	}

	// cross-tenant DELETE 시도 → ErrGroupMappingNotFound (마스킹).
	if err := h.store.Tx(tenantCtx(gmTenantB), func(ctx context.Context, tx storage.Tx) error {
		return h.repo.DeleteGroupMapping(ctx, tx, mid)
	}); !errors.Is(err, sso.ErrGroupMappingNotFound) {
		t.Errorf("cross-tenant DELETE err = %v, want ErrGroupMappingNotFound", err)
	}

	// 정상 DELETE.
	if err := h.store.Tx(tenantCtx(gmTenantA), func(ctx context.Context, tx storage.Tx) error {
		return h.repo.DeleteGroupMapping(ctx, tx, mid)
	}); err != nil {
		t.Fatalf("DeleteGroupMapping: %v", err)
	}

	// 두 번째 DELETE → not found.
	if err := h.store.Tx(tenantCtx(gmTenantA), func(ctx context.Context, tx storage.Tx) error {
		return h.repo.DeleteGroupMapping(ctx, tx, mid)
	}); !errors.Is(err, sso.ErrGroupMappingNotFound) {
		t.Errorf("second DELETE err = %v, want ErrGroupMappingNotFound", err)
	}
}

// === ResolveBindingsForGroups ===

func TestResolveBindingsForGroups_DeduplicatesAndIgnoresUnknown(t *testing.T) {
	t.Parallel()
	h := newGroupMappingHarness(t)
	pid := h.createTestProvider(t, gmTenantA)

	// 매핑 3건: 같은 (role, scope) 셋이 두 group에 중복 매핑 + fleet scope 1건.
	mappings := []sso.CreateGroupMappingRequest{
		{ProviderID: pid, GroupValue: "all-staff", RoleID: gmRoleAID, ScopeType: "tenant"},
		{ProviderID: pid, GroupValue: "platform-admins", RoleID: gmRoleAID, ScopeType: "tenant"}, // 중복 (role, scope)
		{ProviderID: pid, GroupValue: "fleet-admins-A", RoleID: gmRoleAID, ScopeType: "fleet", ScopeID: "flt_a"},
	}
	if err := h.store.Tx(tenantCtx(gmTenantA), func(ctx context.Context, tx storage.Tx) error {
		for _, req := range mappings {
			if _, e := h.repo.CreateGroupMapping(ctx, tx, req); e != nil {
				return e
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("seed mappings: %v", err)
	}

	// IdP claim에 3 group 모두 + 매핑 없는 unknown group.
	if err := h.store.Tx(tenantCtx(gmTenantA), func(ctx context.Context, tx storage.Tx) error {
		out, e := h.repo.ResolveBindingsForGroups(ctx, tx, pid,
			[]string{"all-staff", "platform-admins", "fleet-admins-A", "unknown-group"})
		if e != nil {
			return e
		}
		// 중복 (gmRoleAID, tenant, "")는 1건 + (gmRoleAID, fleet, flt_a) 1건 = 2건.
		if len(out) != 2 {
			t.Fatalf("len = %d, want 2 (deduplicated)", len(out))
		}
		var (
			tenantBindingFound bool
			fleetBindingFound  bool
		)
		for _, b := range out {
			if b.RoleID != gmRoleAID {
				t.Errorf("unexpected role %q", b.RoleID)
			}
			switch b.ScopeType {
			case "tenant":
				tenantBindingFound = true
				if b.ScopeID != "" {
					t.Errorf("tenant binding ScopeID = %q, want empty", b.ScopeID)
				}
			case "fleet":
				fleetBindingFound = true
				if b.ScopeID != "flt_a" {
					t.Errorf("fleet binding ScopeID = %q, want flt_a", b.ScopeID)
				}
			}
		}
		if !tenantBindingFound {
			t.Error("tenant binding not resolved")
		}
		if !fleetBindingFound {
			t.Error("fleet binding not resolved")
		}
		return nil
	}); err != nil {
		t.Fatalf("ResolveBindingsForGroups: %v", err)
	}
}

func TestResolveBindingsForGroups_EmptyGroupsReturnsNil(t *testing.T) {
	t.Parallel()
	h := newGroupMappingHarness(t)
	pid := h.createTestProvider(t, gmTenantA)

	if err := h.store.Tx(tenantCtx(gmTenantA), func(ctx context.Context, tx storage.Tx) error {
		out, e := h.repo.ResolveBindingsForGroups(ctx, tx, pid, nil)
		if e != nil {
			return e
		}
		if out != nil {
			t.Errorf("nil groups → out = %v, want nil", out)
		}
		out2, e := h.repo.ResolveBindingsForGroups(ctx, tx, pid, []string{})
		if e != nil {
			return e
		}
		if out2 != nil {
			t.Errorf("empty groups → out = %v, want nil", out2)
		}
		return nil
	}); err != nil {
		t.Fatalf("ResolveBindingsForGroups: %v", err)
	}
}

func TestResolveBindingsForGroups_TenantIsolated(t *testing.T) {
	t.Parallel()
	h := newGroupMappingHarness(t)
	pidA := h.createTestProvider(t, gmTenantA)

	if err := h.store.Tx(tenantCtx(gmTenantA), func(ctx context.Context, tx storage.Tx) error {
		_, e := h.repo.CreateGroupMapping(ctx, tx, sso.CreateGroupMappingRequest{
			ProviderID: pidA,
			GroupValue: "tenant-A-only",
			RoleID:     gmRoleAID,
			ScopeType:  "tenant",
		})
		return e
	}); err != nil {
		t.Fatalf("CreateGroupMapping: %v", err)
	}

	// tenant_B 컨텍스트에서 동일 group resolve 시도 → 빈 결과 (tenant 격리).
	if err := h.store.Tx(tenantCtx(gmTenantB), func(ctx context.Context, tx storage.Tx) error {
		out, e := h.repo.ResolveBindingsForGroups(ctx, tx, pidA, []string{"tenant-A-only"})
		if e != nil {
			return e
		}
		if len(out) != 0 {
			t.Errorf("cross-tenant resolve → out = %v, want empty", out)
		}
		return nil
	}); err != nil {
		t.Fatalf("ResolveBindingsForGroups: %v", err)
	}
}
