package sqliterepo_test

// provision_external_test.go — O5 SSO autoprov 통합 테스트.
//
// 시나리오:
//
//	T1 TestProvisionExternalUserCreatesNewUserWithDefaultRole — 신규 user INSERT + operator role 할당
//	T2 TestProvisionExternalUserLinksExistingByEmail — 같은 이메일은 link (기존 user.ID 반환)
//	T3 TestProvisionExternalUserRejectsInvalidProvider — local 등 거부
//	T4 TestProvisionExternalUserSAMLPath — AuthProvider=saml도 동작
//	T5 TestProvisionExternalUserRespectsCustomDefaultRole — DefaultRoleName 지정 시 그 role

import (
	"context"
	"strings"
	"testing"

	"github.com/ssabro/rosshield/internal/domain/tenant"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

func TestProvisionExternalUserCreatesNewUserWithDefaultRole(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)

	// admin tenant 시드.
	var admin tenant.CreateResult
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		r, e := repo.Create(ctx, tx, sampleCreate())
		admin = r
		return e
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// 신규 SSO 사용자 provision.
	tenantCtx := storage.WithTenantID(context.Background(), admin.Tenant.ID)
	var user tenant.User
	if err := store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		u, e := repo.ProvisionExternalUser(ctx, tx, tenant.ProvisionExternalUserRequest{
			TenantID:        admin.Tenant.ID,
			Email:           "ext@acme.test",
			DisplayName:     "External User",
			AuthProvider:    tenant.AuthProviderOIDC,
			ExternalSubject: "google-oid-sub-12345",
		})
		user = u
		return e
	}); err != nil {
		t.Fatalf("ProvisionExternalUser: %v", err)
	}

	if !strings.HasPrefix(user.ID, "us_") {
		t.Errorf("user ID = %q, want us_ prefix", user.ID)
	}
	if user.Email != "ext@acme.test" {
		t.Errorf("Email = %q", user.Email)
	}
	if user.AuthProvider != tenant.AuthProviderOIDC {
		t.Errorf("AuthProvider = %q, want oidc", user.AuthProvider)
	}
	if user.ExternalSubject != "google-oid-sub-12345" {
		t.Errorf("ExternalSubject = %q", user.ExternalSubject)
	}
	if user.PasswordHash != "" {
		t.Errorf("PasswordHash = %q, want empty", user.PasswordHash)
	}

	// 기본 role(operator) 할당 확인.
	var roles []tenant.Role
	if err := store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		rs, e := repo.GetUserRoles(ctx, tx, user.ID)
		roles = rs
		return e
	}); err != nil {
		t.Fatalf("GetUserRoles: %v", err)
	}
	if len(roles) != 1 || roles[0].Name != tenant.RoleOperator {
		t.Errorf("roles = %+v, want 1 operator", roles)
	}
}

func TestProvisionExternalUserLinksExistingByEmail(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)

	var admin tenant.CreateResult
	_ = store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		r, _ := repo.Create(ctx, tx, sampleCreate())
		admin = r
		return nil
	})

	// admin email로 SSO provision 시도 → 기존 admin.ID 반환 (link).
	tenantCtx := storage.WithTenantID(context.Background(), admin.Tenant.ID)
	var linked tenant.User
	if err := store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		u, e := repo.ProvisionExternalUser(ctx, tx, tenant.ProvisionExternalUserRequest{
			TenantID:        admin.Tenant.ID,
			Email:           admin.Admin.Email,
			AuthProvider:    tenant.AuthProviderOIDC,
			ExternalSubject: "different-sub",
		})
		linked = u
		return e
	}); err != nil {
		t.Fatalf("ProvisionExternalUser link: %v", err)
	}
	if linked.ID != admin.Admin.ID {
		t.Errorf("linked.ID = %q, want admin.ID %q", linked.ID, admin.Admin.ID)
	}
	// admin role 그대로 유지 (link 모드는 role 미변경).
	var roles []tenant.Role
	_ = store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		rs, _ := repo.GetUserRoles(ctx, tx, linked.ID)
		roles = rs
		return nil
	})
	if len(roles) != 1 || roles[0].Name != tenant.RoleAdmin {
		t.Errorf("link roles = %+v, want 1 admin (unchanged)", roles)
	}
}

func TestProvisionExternalUserRejectsInvalidProvider(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)

	var admin tenant.CreateResult
	_ = store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		r, _ := repo.Create(ctx, tx, sampleCreate())
		admin = r
		return nil
	})

	tenantCtx := storage.WithTenantID(context.Background(), admin.Tenant.ID)
	err := store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		_, e := repo.ProvisionExternalUser(ctx, tx, tenant.ProvisionExternalUserRequest{
			TenantID:        admin.Tenant.ID,
			Email:           "x@x.test",
			AuthProvider:    tenant.AuthProviderLocal, // 잘못
			ExternalSubject: "x",
		})
		return e
	})
	if err == nil || !strings.Contains(err.Error(), "AuthProvider must be oidc or saml") {
		t.Errorf("err = %v, want AuthProvider rejection", err)
	}
}

func TestProvisionExternalUserSAMLPath(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)

	var admin tenant.CreateResult
	_ = store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		r, _ := repo.Create(ctx, tx, sampleCreate())
		admin = r
		return nil
	})

	tenantCtx := storage.WithTenantID(context.Background(), admin.Tenant.ID)
	var user tenant.User
	if err := store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		u, e := repo.ProvisionExternalUser(ctx, tx, tenant.ProvisionExternalUserRequest{
			TenantID:        admin.Tenant.ID,
			Email:           "saml@acme.test",
			AuthProvider:    tenant.AuthProviderSAML,
			ExternalSubject: "okta-nameid-xyz",
		})
		user = u
		return e
	}); err != nil {
		t.Fatalf("ProvisionExternalUser SAML: %v", err)
	}
	if user.AuthProvider != tenant.AuthProviderSAML {
		t.Errorf("AuthProvider = %q, want saml", user.AuthProvider)
	}
}

func TestProvisionExternalUserRespectsCustomDefaultRole(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)

	var admin tenant.CreateResult
	_ = store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		r, _ := repo.Create(ctx, tx, sampleCreate())
		admin = r
		return nil
	})

	tenantCtx := storage.WithTenantID(context.Background(), admin.Tenant.ID)
	var user tenant.User
	_ = store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		u, _ := repo.ProvisionExternalUser(ctx, tx, tenant.ProvisionExternalUserRequest{
			TenantID:        admin.Tenant.ID,
			Email:           "auditor@acme.test",
			AuthProvider:    tenant.AuthProviderOIDC,
			ExternalSubject: "auditor-sub",
			DefaultRoleName: tenant.RoleAuditor,
		})
		user = u
		return nil
	})
	var roles []tenant.Role
	_ = store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		rs, _ := repo.GetUserRoles(ctx, tx, user.ID)
		roles = rs
		return nil
	})
	if len(roles) != 1 || roles[0].Name != tenant.RoleAuditor {
		t.Errorf("roles = %+v, want 1 auditor", roles)
	}
}
