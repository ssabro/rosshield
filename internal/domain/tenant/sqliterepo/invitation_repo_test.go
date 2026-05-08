package sqliterepo_test

// invitation_repo_test.go — E21 초대·역할 관리 통합 테스트.
//
// 시나리오 (TDD T1~T3):
//   T1 TestInvitationTokenIsSingleUseAndExpires
//      - 토큰은 한 번만 accept 가능 (두 번째 accept → ErrInvitationAlreadyUsed).
//      - 만료된 토큰 accept → ErrInvitationExpired.
//   T2 TestAcceptInvitationCreatesUserWithRoles
//      - accept 시 user 생성 + 지정 role 자동 할당 (예: operator).
//   T3 TestInvitationAuditEmitted
//      - CreateInvitation → invitation.sent emit.
//      - AcceptInvitation → invitation.accepted emit.
//   추가:
//      - 활성 초대 중복 → ErrInvitationActive.
//      - email mismatch → ErrInvitationEmailMismatch.
//      - 잘못된 role_name → ErrInvalidRole.
//      - tenant 격리: 다른 tenant의 토큰은 ListInvitations에 안 나옴.

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/domain/tenant"
	"github.com/ssabro/rosshield/internal/domain/tenant/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/idgen"
	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/sqlite"
)

// === fixture: tenant + admin user 시드 + repo ===

type invFixture struct {
	repo     *sqliterepo.Repo
	store    storage.Storage
	tenantID storage.TenantID
	otherID  storage.TenantID
	adminID  string
	otherAdm string
	auditMu  *recordingInvAudit
}

func newInvFixture(t *testing.T) *invFixture {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "data.db")
	store, err := sqlite.Open(storage.Config{Driver: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	clk := clock.System()
	ids := idgen.NewULID()
	rec := &recordingInvAudit{}

	repo := sqliterepo.New(sqliterepo.Deps{
		Clock:           clk,
		IDGen:           ids,
		InvitationAudit: rec,
	})

	// 두 tenant 시드 — 격리 검증.
	var t1, t2 tenant.CreateResult
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		r1, e := repo.Create(ctx, tx, tenant.CreateRequest{
			Name:             "Acme",
			Plan:             tenant.PlanDesktopFree,
			AdminEmail:       "admin@acme.test",
			AdminPassword:    "verylongpassword123",
			AdminDisplayName: "Acme Admin",
		})
		if e != nil {
			return e
		}
		t1 = r1
		r2, e := repo.Create(ctx, tx, tenant.CreateRequest{
			Name:             "Globex",
			Plan:             tenant.PlanDesktopFree,
			AdminEmail:       "admin@globex.test",
			AdminPassword:    "verylongpassword123",
			AdminDisplayName: "Globex Admin",
		})
		if e != nil {
			return e
		}
		t2 = r2
		return nil
	}); err != nil {
		t.Fatalf("seed tenants: %v", err)
	}

	return &invFixture{
		repo:     repo,
		store:    store,
		tenantID: t1.Tenant.ID,
		otherID:  t2.Tenant.ID,
		adminID:  t1.Admin.ID,
		otherAdm: t2.Admin.ID,
		auditMu:  rec,
	}
}

// recordingInvAudit는 InvitationAuditEmitter 호출을 기록합니다.
type recordingInvAudit struct {
	sent     []tenant.Invitation
	accepted []struct {
		Inv  tenant.Invitation
		User tenant.User
	}
}

func (r *recordingInvAudit) EmitInvitationSent(ctx context.Context, tx storage.Tx, inv tenant.Invitation) error {
	r.sent = append(r.sent, inv)
	return nil
}
func (r *recordingInvAudit) EmitInvitationAccepted(ctx context.Context, tx storage.Tx, inv tenant.Invitation, user tenant.User) error {
	r.accepted = append(r.accepted, struct {
		Inv  tenant.Invitation
		User tenant.User
	}{inv, user})
	return nil
}

// === T1: 토큰 1회 사용 + 만료 ===

func TestInvitationTokenIsSingleUseAndExpires(t *testing.T) {
	f := newInvFixture(t)

	// 1. CreateInvitation.
	tenantCtx := storage.WithTenantID(context.Background(), f.tenantID)
	var token string
	if err := f.store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		res, e := f.repo.CreateInvitation(ctx, tx, tenant.CreateInvitationRequest{
			TenantID:  f.tenantID,
			Email:     "newuser@acme.test",
			RoleName:  tenant.RoleOperator,
			InvitedBy: f.adminID,
		})
		if e != nil {
			return e
		}
		token = res.Token
		return nil
	}); err != nil {
		t.Fatalf("CreateInvitation: %v", err)
	}
	if token == "" {
		t.Fatal("token is empty")
	}

	// 2. 첫 accept — 성공.
	if err := f.store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		_, e := f.repo.AcceptInvitation(ctx, tx, tenant.AcceptInvitationRequest{
			Token:       token,
			Email:       "newuser@acme.test",
			Password:    "verylongpassword123",
			DisplayName: "New User",
		})
		return e
	}); err != nil {
		t.Fatalf("first accept: %v", err)
	}

	// 3. 두 번째 accept — ErrInvitationAlreadyUsed.
	err := f.store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		_, e := f.repo.AcceptInvitation(ctx, tx, tenant.AcceptInvitationRequest{
			Token:       token,
			Email:       "newuser@acme.test",
			Password:    "verylongpassword123",
			DisplayName: "Should Fail",
		})
		return e
	})
	if err == nil || !strings.Contains(err.Error(), "already used") {
		t.Errorf("second accept err = %v, want ErrInvitationAlreadyUsed", err)
	}
}

func TestInvitationExpired(t *testing.T) {
	f := newInvFixture(t)

	// CreateInvitation with past expiry — 직접 SQL UPDATE로 expires_at을 어제로.
	var token string
	tenantCtx := storage.WithTenantID(context.Background(), f.tenantID)
	if err := f.store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		res, e := f.repo.CreateInvitation(ctx, tx, tenant.CreateInvitationRequest{
			TenantID:  f.tenantID,
			Email:     "expired@acme.test",
			RoleName:  tenant.RoleOperator,
			InvitedBy: f.adminID,
		})
		if e != nil {
			return e
		}
		token = res.Token
		// 강제 만료 — expires_at을 어제로.
		_, e = tx.Exec(ctx, `UPDATE invitations SET expires_at = ? WHERE token = ?`,
			time.Now().Add(-24*time.Hour).UTC().Format(time.RFC3339Nano), token)
		return e
	}); err != nil {
		t.Fatalf("setup: %v", err)
	}

	err := f.store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		_, e := f.repo.AcceptInvitation(ctx, tx, tenant.AcceptInvitationRequest{
			Token:       token,
			Email:       "expired@acme.test",
			Password:    "verylongpassword123",
			DisplayName: "Expired",
		})
		return e
	})
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Errorf("accept err = %v, want ErrInvitationExpired", err)
	}
}

// === T2: accept 시 user 생성 + role 할당 ===

func TestAcceptInvitationCreatesUserWithRoles(t *testing.T) {
	f := newInvFixture(t)

	tenantCtx := storage.WithTenantID(context.Background(), f.tenantID)
	var token string
	if err := f.store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		res, e := f.repo.CreateInvitation(ctx, tx, tenant.CreateInvitationRequest{
			TenantID:  f.tenantID,
			Email:     "auditor1@acme.test",
			RoleName:  tenant.RoleAuditor,
			InvitedBy: f.adminID,
		})
		if e != nil {
			return e
		}
		token = res.Token
		return nil
	}); err != nil {
		t.Fatalf("CreateInvitation: %v", err)
	}

	var result tenant.AcceptInvitationResult
	if err := f.store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		r, e := f.repo.AcceptInvitation(ctx, tx, tenant.AcceptInvitationRequest{
			Token:       token,
			Email:       "auditor1@acme.test",
			Password:    "verylongpassword123",
			DisplayName: "Auditor User",
		})
		if e != nil {
			return e
		}
		result = r
		return nil
	}); err != nil {
		t.Fatalf("AcceptInvitation: %v", err)
	}

	if result.User.Email != "auditor1@acme.test" {
		t.Errorf("user.Email = %q, want auditor1@acme.test", result.User.Email)
	}
	if result.User.AuthProvider != tenant.AuthProviderLocal {
		t.Errorf("user.AuthProvider = %q, want local", result.User.AuthProvider)
	}
	if result.User.Status != tenant.UserStatusActive {
		t.Errorf("user.Status = %q, want active", result.User.Status)
	}
	if len(result.Roles) != 1 {
		t.Fatalf("roles count = %d, want 1", len(result.Roles))
	}
	if result.Roles[0].Name != tenant.RoleAuditor {
		t.Errorf("role = %q, want auditor", result.Roles[0].Name)
	}

	// 사후 검증: GetUserRoles로도 같은 role 보임.
	if err := f.store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		roles, e := f.repo.GetUserRoles(ctx, tx, result.User.ID)
		if e != nil {
			return e
		}
		if len(roles) != 1 || roles[0].Name != tenant.RoleAuditor {
			t.Errorf("GetUserRoles = %+v, want 1 auditor", roles)
		}
		return nil
	}); err != nil {
		t.Fatalf("GetUserRoles: %v", err)
	}
}

// === T3: audit emit ===

func TestInvitationAuditEmitted(t *testing.T) {
	f := newInvFixture(t)

	tenantCtx := storage.WithTenantID(context.Background(), f.tenantID)
	var token string
	if err := f.store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		res, e := f.repo.CreateInvitation(ctx, tx, tenant.CreateInvitationRequest{
			TenantID:  f.tenantID,
			Email:     "auditcheck@acme.test",
			RoleName:  tenant.RoleOperator,
			InvitedBy: f.adminID,
		})
		if e != nil {
			return e
		}
		token = res.Token
		return nil
	}); err != nil {
		t.Fatalf("CreateInvitation: %v", err)
	}

	if len(f.auditMu.sent) != 1 {
		t.Errorf("invitation.sent count = %d, want 1", len(f.auditMu.sent))
	}

	if err := f.store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		_, e := f.repo.AcceptInvitation(ctx, tx, tenant.AcceptInvitationRequest{
			Token:       token,
			Email:       "auditcheck@acme.test",
			Password:    "verylongpassword123",
			DisplayName: "Audit Check",
		})
		return e
	}); err != nil {
		t.Fatalf("AcceptInvitation: %v", err)
	}

	if len(f.auditMu.accepted) != 1 {
		t.Errorf("invitation.accepted count = %d, want 1", len(f.auditMu.accepted))
	}
	if f.auditMu.accepted[0].User.Email != "auditcheck@acme.test" {
		t.Errorf("audit user.Email = %q", f.auditMu.accepted[0].User.Email)
	}
}

// === 활성 초대 중복 ===

func TestCreateInvitationDuplicateActiveRejected(t *testing.T) {
	f := newInvFixture(t)

	tenantCtx := storage.WithTenantID(context.Background(), f.tenantID)
	if err := f.store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		_, e := f.repo.CreateInvitation(ctx, tx, tenant.CreateInvitationRequest{
			TenantID: f.tenantID, Email: "dup@acme.test",
			RoleName: tenant.RoleOperator, InvitedBy: f.adminID,
		})
		return e
	}); err != nil {
		t.Fatalf("first invitation: %v", err)
	}

	err := f.store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		_, e := f.repo.CreateInvitation(ctx, tx, tenant.CreateInvitationRequest{
			TenantID: f.tenantID, Email: "dup@acme.test",
			RoleName: tenant.RoleOperator, InvitedBy: f.adminID,
		})
		return e
	})
	if err == nil || !strings.Contains(err.Error(), "active invitation") {
		t.Errorf("dup err = %v, want ErrInvitationActive", err)
	}
}

// === email mismatch ===

func TestAcceptInvitationEmailMismatch(t *testing.T) {
	f := newInvFixture(t)

	tenantCtx := storage.WithTenantID(context.Background(), f.tenantID)
	var token string
	if err := f.store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		res, e := f.repo.CreateInvitation(ctx, tx, tenant.CreateInvitationRequest{
			TenantID: f.tenantID, Email: "real@acme.test",
			RoleName: tenant.RoleOperator, InvitedBy: f.adminID,
		})
		if e != nil {
			return e
		}
		token = res.Token
		return nil
	}); err != nil {
		t.Fatalf("CreateInvitation: %v", err)
	}

	err := f.store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		_, e := f.repo.AcceptInvitation(ctx, tx, tenant.AcceptInvitationRequest{
			Token: token, Email: "wrong@acme.test",
			Password: "verylongpassword123", DisplayName: "Wrong",
		})
		return e
	})
	if err == nil || !strings.Contains(err.Error(), "email does not match") {
		t.Errorf("err = %v, want ErrInvitationEmailMismatch", err)
	}
}

// === 잘못된 role_name ===

func TestCreateInvitationInvalidRole(t *testing.T) {
	f := newInvFixture(t)

	tenantCtx := storage.WithTenantID(context.Background(), f.tenantID)
	err := f.store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		_, e := f.repo.CreateInvitation(ctx, tx, tenant.CreateInvitationRequest{
			TenantID: f.tenantID, Email: "x@acme.test",
			RoleName: "nonexistent", InvitedBy: f.adminID,
		})
		return e
	})
	if err == nil || !strings.Contains(err.Error(), "invalid role") {
		t.Errorf("err = %v, want ErrInvalidRole", err)
	}
}

// === O6: InvitationNotifier 호출 ===

// recordingNotifier는 InvitationNotifier 호출을 기록합니다.
type recordingNotifier struct {
	calls []struct {
		Inv       tenant.Invitation
		AcceptURL string
	}
	returnErr error
}

func (n *recordingNotifier) NotifyInvitationSent(_ context.Context, inv tenant.Invitation, acceptURL string) error {
	n.calls = append(n.calls, struct {
		Inv       tenant.Invitation
		AcceptURL string
	}{inv, acceptURL})
	return n.returnErr
}

// newInvFixtureWithNotifier는 newInvFixture와 동일하되 InvitationNotifier hook을
// 추가 주입한 fixture를 반환합니다.
func newInvFixtureWithNotifier(t *testing.T, notifier tenant.InvitationNotifier, urlBuilder func(token string) string) *invFixture {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "data.db")
	store, err := sqlite.Open(storage.Config{Driver: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	clk := clock.System()
	ids := idgen.NewULID()
	rec := &recordingInvAudit{}

	repo := sqliterepo.New(sqliterepo.Deps{
		Clock:                      clk,
		IDGen:                      ids,
		InvitationAudit:            rec,
		InvitationNotifier:         notifier,
		InvitationAcceptURLBuilder: urlBuilder,
	})

	var t1, t2 tenant.CreateResult
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		r1, e := repo.Create(ctx, tx, tenant.CreateRequest{
			Name: "Acme", Plan: tenant.PlanDesktopFree,
			AdminEmail: "admin@acme.test", AdminPassword: "verylongpassword123",
			AdminDisplayName: "Acme Admin",
		})
		if e != nil {
			return e
		}
		t1 = r1
		r2, e := repo.Create(ctx, tx, tenant.CreateRequest{
			Name: "Globex", Plan: tenant.PlanDesktopFree,
			AdminEmail: "admin@globex.test", AdminPassword: "verylongpassword123",
			AdminDisplayName: "Globex Admin",
		})
		if e != nil {
			return e
		}
		t2 = r2
		return nil
	}); err != nil {
		t.Fatalf("seed tenants: %v", err)
	}

	return &invFixture{
		repo:     repo,
		store:    store,
		tenantID: t1.Tenant.ID,
		otherID:  t2.Tenant.ID,
		adminID:  t1.Admin.ID,
		otherAdm: t2.Admin.ID,
		auditMu:  rec,
	}
}

func TestCreateInvitationCallsNotifier(t *testing.T) {
	notifier := &recordingNotifier{}
	urlBuilder := func(tok string) string { return "https://app.example.com/invitations/accept/" + tok }
	f := newInvFixtureWithNotifier(t, notifier, urlBuilder)

	tenantCtx := storage.WithTenantID(context.Background(), f.tenantID)
	var token string
	if err := f.store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		res, e := f.repo.CreateInvitation(ctx, tx, tenant.CreateInvitationRequest{
			TenantID: f.tenantID, Email: "notify@acme.test",
			RoleName: tenant.RoleOperator, InvitedBy: f.adminID,
		})
		if e != nil {
			return e
		}
		token = res.Token
		return nil
	}); err != nil {
		t.Fatalf("CreateInvitation: %v", err)
	}

	if len(notifier.calls) != 1 {
		t.Fatalf("notifier calls = %d, want 1", len(notifier.calls))
	}
	if notifier.calls[0].Inv.Email != "notify@acme.test" {
		t.Errorf("notified email = %q", notifier.calls[0].Inv.Email)
	}
	wantURL := "https://app.example.com/invitations/accept/" + token
	if notifier.calls[0].AcceptURL != wantURL {
		t.Errorf("acceptURL = %q, want %q", notifier.calls[0].AcceptURL, wantURL)
	}
}

// 알림 실패는 INSERT를 rollback하지 않음 — best-effort.
func TestCreateInvitationNotifierFailureDoesNotRollback(t *testing.T) {
	notifier := &recordingNotifier{returnErr: errors.New("smtp down")}
	f := newInvFixtureWithNotifier(t, notifier, nil)

	tenantCtx := storage.WithTenantID(context.Background(), f.tenantID)
	var token string
	if err := f.store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		res, e := f.repo.CreateInvitation(ctx, tx, tenant.CreateInvitationRequest{
			TenantID: f.tenantID, Email: "noisy@acme.test",
			RoleName: tenant.RoleOperator, InvitedBy: f.adminID,
		})
		if e != nil {
			return e
		}
		token = res.Token
		return nil
	}); err != nil {
		t.Fatalf("CreateInvitation: %v", err)
	}
	if token == "" {
		t.Fatal("expected token despite notifier error")
	}

	tenantCtx2 := storage.WithTenantID(context.Background(), f.tenantID)
	if err := f.store.Tx(tenantCtx2, func(ctx context.Context, tx storage.Tx) error {
		list, e := f.repo.ListInvitations(ctx, tx)
		if e != nil {
			return e
		}
		if len(list) != 1 || list[0].Email != "noisy@acme.test" {
			t.Errorf("list = %+v, want 1 invitation for noisy@acme.test", list)
		}
		return nil
	}); err != nil {
		t.Fatalf("ListInvitations: %v", err)
	}
}

// nil InvitationAcceptURLBuilder는 빈 문자열을 넘김.
func TestCreateInvitationNilURLBuilderPassesEmptyAcceptURL(t *testing.T) {
	notifier := &recordingNotifier{}
	f := newInvFixtureWithNotifier(t, notifier, nil)

	tenantCtx := storage.WithTenantID(context.Background(), f.tenantID)
	if err := f.store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		_, e := f.repo.CreateInvitation(ctx, tx, tenant.CreateInvitationRequest{
			TenantID: f.tenantID, Email: "nourl@acme.test",
			RoleName: tenant.RoleOperator, InvitedBy: f.adminID,
		})
		return e
	}); err != nil {
		t.Fatalf("CreateInvitation: %v", err)
	}
	if len(notifier.calls) != 1 || notifier.calls[0].AcceptURL != "" {
		t.Errorf("acceptURL = %q, want empty (nil builder)", notifier.calls[0].AcceptURL)
	}
}

// === tenant 격리 ===

func TestListInvitationsTenantIsolated(t *testing.T) {
	f := newInvFixture(t)

	// tenantA에 1개, tenantB에 2개.
	mustCreate := func(tenantID storage.TenantID, invitedBy, email string) {
		t.Helper()
		tenantCtx := storage.WithTenantID(context.Background(), tenantID)
		if err := f.store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
			_, e := f.repo.CreateInvitation(ctx, tx, tenant.CreateInvitationRequest{
				TenantID: tenantID, Email: email,
				RoleName: tenant.RoleOperator, InvitedBy: invitedBy,
			})
			return e
		}); err != nil {
			t.Fatalf("create %s: %v", email, err)
		}
	}
	mustCreate(f.tenantID, f.adminID, "a1@acme.test")
	mustCreate(f.otherID, f.otherAdm, "b1@globex.test")
	mustCreate(f.otherID, f.otherAdm, "b2@globex.test")

	// tenantA 목록 → 1건.
	var listA []tenant.Invitation
	tenantCtxA := storage.WithTenantID(context.Background(), f.tenantID)
	if err := f.store.Tx(tenantCtxA, func(ctx context.Context, tx storage.Tx) error {
		l, e := f.repo.ListInvitations(ctx, tx)
		listA = l
		return e
	}); err != nil {
		t.Fatalf("ListInvitations A: %v", err)
	}
	if len(listA) != 1 {
		t.Errorf("tenantA list count = %d, want 1", len(listA))
	}

	// tenantB 목록 → 2건.
	var listB []tenant.Invitation
	tenantCtxB := storage.WithTenantID(context.Background(), f.otherID)
	if err := f.store.Tx(tenantCtxB, func(ctx context.Context, tx storage.Tx) error {
		l, e := f.repo.ListInvitations(ctx, tx)
		listB = l
		return e
	}); err != nil {
		t.Fatalf("ListInvitations B: %v", err)
	}
	if len(listB) != 2 {
		t.Errorf("tenantB list count = %d, want 2", len(listB))
	}
}
