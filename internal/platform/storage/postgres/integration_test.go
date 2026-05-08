//go:build integration

// integration_test.go — E22-E PG 통합 테스트 (testcontainers-go).
//
// 본 파일은 `-tags=integration` 빌드 태그가 붙어야 컴파일됩니다.
// docker daemon 부재 시 testcontainers-go가 immediate fail — t.Skip 가드.
//
// 실행:
//
//	go test -tags=integration -count=1 ./internal/platform/storage/postgres/
//
// 검증 항목:
//
//   - TestIntegrationMigrationsApplyToFreshDB: 빈 PG에 0001~0021 적용 후 핵심 테이블 존재 확인.
//   - TestIntegrationMigrationsIdempotent: Migrate 두 번 호출 → 두 번째는 ErrNoChange (감춰짐) → no error.
//   - TestIntegrationTenantCreate: tenant + admin user + 시스템 역할 3개 시드 (sqlite 어댑터와 동일 흐름).
//   - TestIntegrationInvitationFlow: tenant 시드 → invitation 발송 → accept → user 생성·role 할당.
//   - TestIntegrationCrossTenantIsolation: tenantA·tenantB 동시 시드 → invitation list가 tenant scope로 격리.

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcpg "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/ssabro/rosshield/internal/domain/tenant"
	tenantrepo "github.com/ssabro/rosshield/internal/domain/tenant/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/idgen"
	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/postgres"
)

// pgFixture는 단일 테스트당 격리된 PG 컨테이너 + Storage 인스턴스입니다.
//
// 같은 컨테이너를 재사용하면 schema 충돌이 위험하므로, 각 테스트가 자체 컨테이너 보유.
// 5초 startup × 5 테스트 = ~25초 전체. CI는 docker 사용 가능.
func newPGFixture(t *testing.T) (storage.Storage, *postgres.Postgres) {
	t.Helper()
	ctx := context.Background()

	pgC, err := tcpg.Run(ctx, "postgres:16-alpine",
		tcpg.WithDatabase("rosshield_test"),
		tcpg.WithUsername("test"),
		tcpg.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		t.Skipf("docker unavailable or PG container failed: %v", err)
	}
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = pgC.Terminate(shutdownCtx)
	})

	dsn, err := pgC.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("ConnectionString: %v", err)
	}

	store, err := postgres.Open(storage.Config{Driver: "postgres", DSN: dsn})
	if err != nil {
		t.Fatalf("postgres.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return store, store
}

// === TestIntegrationMigrationsApplyToFreshDB ===

func TestIntegrationMigrationsApplyToFreshDB(t *testing.T) {
	t.Parallel()
	store, _ := newPGFixture(t)

	// 핵심 테이블 존재 확인.
	expected := []string{
		"platform_info", "tenants", "users", "roles", "user_roles",
		"auth_refresh_tokens", "api_keys", "audit_entries", "audit_checkpoints",
		"packs", "fleets", "credentials", "robots",
		"scan_sessions", "scan_results", "evidence_records", "evidence_refs",
		"reports", "framework_reports", "insights",
		"compliance_profiles", "framework_snapshots",
		"mapping_suggestions", "advisor_conversations", "advisor_turns",
		"webhook_endpoints", "webhook_deliveries",
		"sso_providers", "sso_login_attempts", "sso_external_identities",
		"invitations",
	}
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		for _, table := range expected {
			var n int
			row := tx.QueryRow(ctx,
				`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_name = ?`,
				table)
			if err := row.Scan(&n); err != nil {
				return err
			}
			if n != 1 {
				t.Errorf("table %q not found in PG schema", table)
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("verify tables: %v", err)
	}
}

// === TestIntegrationMigrationsIdempotent ===

func TestIntegrationMigrationsIdempotent(t *testing.T) {
	t.Parallel()
	store, pg := newPGFixture(t)
	_ = store // 같은 인스턴스에서 재호출.

	if err := pg.Migrate(context.Background()); err != nil {
		t.Fatalf("second Migrate: %v (멱등 위반)", err)
	}
}

// === TestIntegrationTenantCreate ===

func TestIntegrationTenantCreate(t *testing.T) {
	t.Parallel()
	store, _ := newPGFixture(t)

	clk := clock.System()
	ids := idgen.NewULID()
	repo := tenantrepo.New(tenantrepo.Deps{
		Clock: clk, IDGen: ids, Audit: nullAuditEmitter{},
	})

	var result tenant.CreateResult
	err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		r, e := repo.Create(ctx, tx, tenant.CreateRequest{
			Name:             "PG Tenant",
			Plan:             tenant.PlanDesktopFree,
			AdminEmail:       "admin@pg.test",
			AdminPassword:    "verylongpassword123",
			AdminDisplayName: "PG Admin",
		})
		if e != nil {
			return e
		}
		result = r
		return nil
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if result.Tenant.ID == "" || result.Admin.ID == "" {
		t.Errorf("tenant.ID=%q admin.ID=%q empty", result.Tenant.ID, result.Admin.ID)
	}

	// 시스템 역할 3개 + admin role 할당 확인.
	tenantCtx := storage.WithTenantID(context.Background(), result.Tenant.ID)
	if err := store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		roles, e := repo.GetUserRoles(ctx, tx, result.Admin.ID)
		if e != nil {
			return e
		}
		if len(roles) != 1 || roles[0].Name != tenant.RoleAdmin {
			t.Errorf("admin roles = %+v, want 1 admin", roles)
		}
		return nil
	}); err != nil {
		t.Fatalf("GetUserRoles: %v", err)
	}
}

// === TestIntegrationInvitationFlow ===

func TestIntegrationInvitationFlow(t *testing.T) {
	t.Parallel()
	store, _ := newPGFixture(t)

	clk := clock.System()
	ids := idgen.NewULID()
	repo := tenantrepo.New(tenantrepo.Deps{
		Clock: clk, IDGen: ids, Audit: nullAuditEmitter{}, InvitationAudit: nullInvAudit{},
	})

	// admin tenant 시드.
	var admin tenant.CreateResult
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		r, e := repo.Create(ctx, tx, tenant.CreateRequest{
			Name:             "Inv Tenant",
			Plan:             tenant.PlanDesktopFree,
			AdminEmail:       "admin@inv.test",
			AdminPassword:    "verylongpassword123",
			AdminDisplayName: "Inv Admin",
		})
		if e != nil {
			return e
		}
		admin = r
		return nil
	}); err != nil {
		t.Fatalf("seed admin: %v", err)
	}

	// 초대 발송 (operator role).
	tenantCtx := storage.WithTenantID(context.Background(), admin.Tenant.ID)
	var token string
	if err := store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		res, e := repo.CreateInvitation(ctx, tx, tenant.CreateInvitationRequest{
			TenantID:  admin.Tenant.ID,
			Email:     "newuser@inv.test",
			RoleName:  tenant.RoleOperator,
			InvitedBy: admin.Admin.ID,
		})
		if e != nil {
			return e
		}
		token = res.Token
		return nil
	}); err != nil {
		t.Fatalf("CreateInvitation: %v", err)
	}

	// accept (Bootstrap Tx — 비인증 흐름).
	var acceptResult tenant.AcceptInvitationResult
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		r, e := repo.AcceptInvitation(ctx, tx, tenant.AcceptInvitationRequest{
			Token:       token,
			Email:       "newuser@inv.test",
			Password:    "verylongpassword123",
			DisplayName: "New User",
		})
		if e != nil {
			return e
		}
		acceptResult = r
		return nil
	}); err != nil {
		t.Fatalf("AcceptInvitation: %v", err)
	}
	if acceptResult.User.ID == "" {
		t.Error("accept user.ID empty")
	}
	if len(acceptResult.Roles) != 1 || acceptResult.Roles[0].Name != tenant.RoleOperator {
		t.Errorf("accept roles = %+v, want 1 operator", acceptResult.Roles)
	}

	// 두 번째 accept 시도 → ErrInvitationAlreadyUsed.
	err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		_, e := repo.AcceptInvitation(ctx, tx, tenant.AcceptInvitationRequest{
			Token: token, Email: "newuser@inv.test",
			Password: "verylongpassword123", DisplayName: "Dup",
		})
		return e
	})
	if !errors.Is(err, tenant.ErrInvitationAlreadyUsed) {
		t.Errorf("second accept err = %v, want ErrInvitationAlreadyUsed", err)
	}
}

// === TestIntegrationCrossTenantIsolation ===

func TestIntegrationCrossTenantIsolation(t *testing.T) {
	t.Parallel()
	store, _ := newPGFixture(t)

	clk := clock.System()
	ids := idgen.NewULID()
	repo := tenantrepo.New(tenantrepo.Deps{
		Clock: clk, IDGen: ids, Audit: nullAuditEmitter{}, InvitationAudit: nullInvAudit{},
	})

	// 두 tenant 시드.
	var t1, t2 tenant.CreateResult
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		r1, e := repo.Create(ctx, tx, tenant.CreateRequest{
			Name: "A", Plan: tenant.PlanDesktopFree,
			AdminEmail: "a@t.test", AdminPassword: "verylongpassword123", AdminDisplayName: "A",
		})
		if e != nil {
			return e
		}
		t1 = r1
		r2, e := repo.Create(ctx, tx, tenant.CreateRequest{
			Name: "B", Plan: tenant.PlanDesktopFree,
			AdminEmail: "b@t.test", AdminPassword: "verylongpassword123", AdminDisplayName: "B",
		})
		if e != nil {
			return e
		}
		t2 = r2
		return nil
	}); err != nil {
		t.Fatalf("seed tenants: %v", err)
	}

	// tenant A에 1개, tenant B에 2개 초대.
	mustInv := func(adm tenant.CreateResult, email string) {
		t.Helper()
		ctx := storage.WithTenantID(context.Background(), adm.Tenant.ID)
		if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
			_, e := repo.CreateInvitation(ctx, tx, tenant.CreateInvitationRequest{
				TenantID: adm.Tenant.ID, Email: email,
				RoleName: tenant.RoleOperator, InvitedBy: adm.Admin.ID,
			})
			return e
		}); err != nil {
			t.Fatalf("CreateInvitation %s: %v", email, err)
		}
	}
	mustInv(t1, "a1@t.test")
	mustInv(t2, "b1@t.test")
	mustInv(t2, "b2@t.test")

	// tenant A list = 1.
	tenantCtxA := storage.WithTenantID(context.Background(), t1.Tenant.ID)
	if err := store.Tx(tenantCtxA, func(ctx context.Context, tx storage.Tx) error {
		invs, e := repo.ListInvitations(ctx, tx)
		if e != nil {
			return e
		}
		if len(invs) != 1 {
			t.Errorf("tenantA list = %d, want 1", len(invs))
		}
		return nil
	}); err != nil {
		t.Fatalf("ListInvitations A: %v", err)
	}

	// tenant B list = 2.
	tenantCtxB := storage.WithTenantID(context.Background(), t2.Tenant.ID)
	if err := store.Tx(tenantCtxB, func(ctx context.Context, tx storage.Tx) error {
		invs, e := repo.ListInvitations(ctx, tx)
		if e != nil {
			return e
		}
		if len(invs) != 2 {
			t.Errorf("tenantB list = %d, want 2", len(invs))
		}
		return nil
	}); err != nil {
		t.Fatalf("ListInvitations B: %v", err)
	}
}

// === audit emitter fakes ===

type nullAuditEmitter struct{}

func (nullAuditEmitter) EmitTenantCreated(_ context.Context, _ storage.Tx, _ tenant.Tenant, _ tenant.User) error {
	return nil
}

type nullInvAudit struct{}

func (nullInvAudit) EmitInvitationSent(_ context.Context, _ storage.Tx, _ tenant.Invitation) error {
	return nil
}
func (nullInvAudit) EmitInvitationAccepted(_ context.Context, _ storage.Tx, _ tenant.Invitation, _ tenant.User) error {
	return nil
}
