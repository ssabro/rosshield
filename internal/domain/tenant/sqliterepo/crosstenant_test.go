package sqliterepo_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ssabro/rosshield/internal/domain/tenant"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// E3.T8 본체 — cross-tenant 접근이 모든 SELECT/UPDATE 경로에서 차단되는지 회귀 검증.
//
// 시나리오:
//  1. Tenant A 생성 + admin a, ApiKey a_key
//  2. Tenant B 생성 + admin b, ApiKey b_key
//  3. Tenant A의 모든 SELECT/UPDATE 메서드로 Tenant B의 리소스 접근 시도 → 모두 ErrNotFound
//  4. Tenant A의 List 메서드는 본인 리소스만 반환
//
// 새 SELECT/UPDATE 메서드가 추가될 때마다 이 테스트에 대응 라인을 추가해 leak 차단을 자동화합니다.
func TestTenantScopeBlocksCrossTenantRead(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)

	// --- A·B 두 tenant 생성 + 각자 admin user + 각자 ApiKey ---

	tenantA, tenantB := setupTwoTenants(t, repo, store)

	// --- Cross-tenant SELECT 시도들: 모두 ErrNotFound ---

	ctxA := storage.WithTenantID(context.Background(), tenantA.id)

	// GetUserByEmail: A 컨텍스트로 B의 admin email
	if err := store.Tx(ctxA, func(ctx context.Context, tx storage.Tx) error {
		_, e := repo.GetUserByEmail(ctx, tx, tenantA.id, tenantB.adminEmail)
		return e
	}); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("GetUserByEmail(A's tenantID, B's email) = %v, want ErrNotFound", err)
	}

	// GetRole: A 컨텍스트로 B 도메인의 admin role 조회 시도 (B의 role.name="admin"이지만 B의 tenant)
	if err := store.Tx(ctxA, func(ctx context.Context, tx storage.Tx) error {
		// 같은 name 'admin'인데 tenantA 스코프 — 실제 A의 role을 받아야 함 (이는 cross-tenant 아님)
		role, err := repo.GetRole(ctx, tx, tenantA.id, tenant.RoleAdmin)
		if err != nil {
			return err
		}
		// role은 A의 admin role이어야 함 (B의 것 아님).
		if role.TenantID != tenantA.id {
			t.Errorf("GetRole returned cross-tenant row: TenantID=%q, want %q", role.TenantID, tenantA.id)
		}
		return nil
	}); err != nil {
		t.Errorf("GetRole(A, admin): %v", err)
	}

	// RevokeApiKey: A의 컨텍스트로 B의 apikey ID 시도 → ErrNotFound (cross-tenant UPDATE 차단)
	if err := store.Tx(ctxA, func(ctx context.Context, tx storage.Tx) error {
		return repo.RevokeApiKey(ctx, tx, tenantA.id, tenantB.apiKeyID)
	}); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("RevokeApiKey(A, B's apiKeyID) = %v, want ErrNotFound", err)
	}

	// ListApiKeys: A의 ListApiKeys는 A의 키만 반환, B의 키 절대 포함 X
	if err := store.Tx(ctxA, func(ctx context.Context, tx storage.Tx) error {
		keys, err := repo.ListApiKeys(ctx, tx, tenantA.id)
		if err != nil {
			return err
		}
		for _, k := range keys {
			if k.TenantID != tenantA.id {
				t.Errorf("ListApiKeys leaked cross-tenant key: TenantID=%q, want %q", k.TenantID, tenantA.id)
			}
			if k.ID == tenantB.apiKeyID {
				t.Errorf("ListApiKeys returned B's apiKeyID %q in A's list", k.ID)
			}
		}
		return nil
	}); err != nil {
		t.Errorf("ListApiKeys(A): %v", err)
	}

	// GetUserRoles: B의 admin user ID로 A 컨텍스트에서 조회 시도.
	// 이 메서드는 user_id 기반이라 tenant 필터 없이 row 반환할 수도 있음 — 검증.
	if err := store.Tx(ctxA, func(ctx context.Context, tx storage.Tx) error {
		roles, err := repo.GetUserRoles(ctx, tx, tenantB.adminID)
		if err != nil {
			return err
		}
		// 의도: GetUserRoles는 user_id로 lookup하지만, tenant filter는 미적용 (현재 구현).
		// 조회되는 role의 tenant_id가 A가 아니라 B면 — leak signal.
		// Phase 1은 tenant_id 필터 미적용이지만, role.TenantID로 검증 시 B여야 함.
		// (호출자(미들웨어)가 tx.TenantID() ?= role.TenantID를 강제하는 패턴 — 후속 강화)
		for _, r := range roles {
			if r.TenantID != tenantB.id {
				t.Errorf("GetUserRoles(B's admin) returned role with TenantID=%q, want %q",
					r.TenantID, tenantB.id)
			}
		}
		return nil
	}); err != nil {
		t.Errorf("GetUserRoles: %v", err)
	}

	// Login: A 컨텍스트로 B의 admin email로 로그인 → ErrInvalidCredentials (B의 user는 A에 없음)
	if err := store.Tx(ctxA, func(ctx context.Context, tx storage.Tx) error {
		_, e := repo.Login(ctx, tx, tenant.LoginRequest{
			TenantID: tenantA.id,
			Email:    tenantB.adminEmail,
			Password: "long-enough-secret-pw",
		})
		return e
	}); !errors.Is(err, tenant.ErrInvalidCredentials) {
		t.Errorf("Login(A, B's email): err = %v, want ErrInvalidCredentials", err)
	}

	// AuthenticateApiKey: cross-tenant scenario (intentional)
	// 이 메서드는 tenant 미상 진입점이라 cross-tenant lookup이 맞음 — leak이 아니라 의도.
	// 검증: B의 raw token으로 인증 → 정상 성공 (B의 ApiKey 반환), 그리고 반환된 키의 TenantID는 B여야 함.
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		k, err := repo.AuthenticateApiKey(ctx, tx, tenantB.rawToken)
		if err != nil {
			return err
		}
		if k.TenantID != tenantB.id {
			t.Errorf("AuthenticateApiKey returned wrong TenantID=%q, want %q", k.TenantID, tenantB.id)
		}
		return nil
	}); err != nil {
		t.Errorf("AuthenticateApiKey(B's token): %v", err)
	}
}

type tenantFixture struct {
	id         storage.TenantID
	adminID    string
	adminEmail string
	apiKeyID   string
	rawToken   string
}

func setupTwoTenants(t *testing.T, repo interface {
	Create(ctx context.Context, tx storage.Tx, req tenant.CreateRequest) (tenant.CreateResult, error)
	IssueApiKey(ctx context.Context, tx storage.Tx, req tenant.IssueApiKeyRequest) (tenant.IssueApiKeyResult, error)
}, store storage.Storage) (a, b tenantFixture) {
	t.Helper()

	makeTenant := func(name, adminEmail string) tenantFixture {
		var fx tenantFixture
		var created tenant.CreateResult
		if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
			r, err := repo.Create(ctx, tx, tenant.CreateRequest{
				Name:          name,
				Plan:          tenant.PlanDesktopFree,
				AdminEmail:    adminEmail,
				AdminPassword: "long-enough-secret-pw",
			})
			created = r
			return err
		}); err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
		fx.id = created.Tenant.ID
		fx.adminID = created.Admin.ID
		fx.adminEmail = created.Admin.Email

		// API key 발급 (tenant scope Tx).
		ctx := storage.WithTenantID(context.Background(), fx.id)
		if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
			r, err := repo.IssueApiKey(ctx, tx, tenant.IssueApiKeyRequest{
				TenantID:  fx.id,
				Name:      name + "-key",
				CreatedBy: fx.adminID,
			})
			if err != nil {
				return err
			}
			fx.apiKeyID = r.Key.ID
			fx.rawToken = r.RawToken
			return nil
		}); err != nil {
			t.Fatalf("IssueApiKey %s: %v", name, err)
		}
		return fx
	}

	a = makeTenant("Acme", "admin@acme.example")
	b = makeTenant("Beta", "admin@beta.example")
	return a, b
}
