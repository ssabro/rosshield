package main

// intake_provisioning_test.go — Customer onboarding R1 Stage 4 단위 테스트.
//
// design doc `docs/design/notes/customer-onboarding-design.md` §7 R1 Stage 4 (auto-provisioning).
//
// 검증 매트릭스:
//
//	AcceptIntake:
//	  - tenant 생성 + admin user 시드 + intake.TenantID 채움 + license 발급 placeholder log.
//	  - PlanRequest → tenant.Plan 매핑 (community/pro/enterprise).
//	  - tenant.Create 실패 시 rollback (intake도 pending 유지).
//	  - tenant.Create 검증 실패(ErrInvalidEmail 등)도 surface.
//
//	delegate (Create/Get/List/Reject):
//	  - inner.Service 호출 후 결과 그대로 반환 — wrap 영향 없음.

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ssabro/rosshield/internal/domain/intake"
	intakerepo "github.com/ssabro/rosshield/internal/domain/intake/sqliterepo"
	"github.com/ssabro/rosshield/internal/domain/tenant"
	tenantrepo "github.com/ssabro/rosshield/internal/domain/tenant/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/idgen"
	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/sqlite"
)

// === harness ===

// newProvisioningTestRig는 in-memory SQLite + intake repo + tenant repo로
// intakeProvisioningAdapter를 결선합니다.
//
// license는 nil (paying customer 0 단계 placeholder). 본 stage 단위 테스트는 license
// 발급 동작 자체는 검증하지 않음 — adapter가 nil enforcer에서 panic하지 않는지만 검증.
func newProvisioningTestRig(t *testing.T) (*intakeProvisioningAdapter, storage.Storage, intake.Service) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "provisioning.db")
	store, err := sqlite.Open(storage.Config{Driver: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	intakeSvc := intakerepo.New(intakerepo.Deps{
		Clock: clock.System(),
		IDGen: idgen.NewULID(),
	})
	tenantSvc := tenantrepo.New(tenantrepo.Deps{
		Clock: clock.System(),
		IDGen: idgen.NewULID(),
		// Audit·Invitation* nil OK — Repo가 nil check.
	})

	adapter := newIntakeProvisioningAdapter(intakeSvc, tenantSvc, nil)
	return adapter, store, intakeSvc
}

// seedPendingIntake는 pending 상태의 intake row 1건을 시드합니다.
func seedPendingIntake(t *testing.T, store storage.Storage, inner intake.Service, planReq intake.PlanRequest) intake.CustomerIntake {
	t.Helper()
	var created intake.CustomerIntake
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		row, err := inner.CreateIntake(ctx, tx, intake.CreateIntakeRequest{
			OrganizationName:    "Acme Robotics",
			PrimaryContactEmail: "admin@acme.example",
			PrimaryContactName:  "Acme Admin",
			PlanRequest:         planReq,
			IntendedUse:         "ROS2 fleet 보안 감사 PoC.",
		})
		created = row
		return err
	}); err != nil {
		t.Fatalf("seed CreateIntake: %v", err)
	}
	return created
}

// countTenantsExcludingSystem은 tenants 테이블에서 system 외 row 수를 반환합니다.
//
// bootstrap의 alreadySeeded 가드와 일관 — system tenant row는 R1 Stage 4 단위에는
// 시드되지 않으므로 본 helper는 단순 SELECT COUNT 사용.
func countTenantsExcludingSystem(t *testing.T, store storage.Storage) int {
	t.Helper()
	var n int
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		row := tx.QueryRow(ctx, `SELECT COUNT(*) FROM tenants WHERE id != 'system'`)
		return row.Scan(&n)
	}); err != nil {
		t.Fatalf("countTenants: %v", err)
	}
	return n
}

// countUsersByEmail은 (lowercase email) 사용자 수를 반환합니다.
func countUsersByEmail(t *testing.T, store storage.Storage, email string) int {
	t.Helper()
	var n int
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		row := tx.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE email = ?`, strings.ToLower(email))
		return row.Scan(&n)
	}); err != nil {
		t.Fatalf("countUsersByEmail: %v", err)
	}
	return n
}

// === AcceptIntake — auto-provisioning happy path ===

func TestProvisioningAcceptCreatesTenantAndAdminUser(t *testing.T) {
	t.Parallel()
	adapter, store, inner := newProvisioningTestRig(t)

	intakeRow := seedPendingIntake(t, store, inner, intake.PlanPro)

	var accepted intake.CustomerIntake
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		row, err := adapter.AcceptIntake(ctx, tx, intake.AcceptIntakeRequest{
			IntakeID:         intakeRow.ID,
			AcceptedByUserID: "us_OPER1",
		})
		accepted = row
		return err
	}); err != nil {
		t.Fatalf("AcceptIntake: %v", err)
	}

	if accepted.Status != intake.StatusAccepted {
		t.Errorf("Status = %q, want accepted", accepted.Status)
	}
	if accepted.TenantID == "" {
		t.Error("TenantID empty — auto-provisioning should fill it")
	}
	if accepted.AcceptedByUserID == nil || *accepted.AcceptedByUserID != "us_OPER1" {
		t.Errorf("AcceptedByUserID = %v, want us_OPER1", accepted.AcceptedByUserID)
	}

	if n := countTenantsExcludingSystem(t, store); n != 1 {
		t.Errorf("tenant count = %d, want 1", n)
	}
	if n := countUsersByEmail(t, store, intakeRow.PrimaryContactEmail); n != 1 {
		t.Errorf("admin user count = %d, want 1", n)
	}
}

func TestProvisioningAcceptMapsPlanRequest(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		planReq  intake.PlanRequest
		wantPlan tenant.Plan
	}{
		{"community→desktop_free", intake.PlanCommunity, tenant.PlanDesktopFree},
		{"pro→desktop_pro", intake.PlanPro, tenant.PlanDesktopPro},
		{"enterprise→enterprise", intake.PlanEnterprise, tenant.PlanEnterprise},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			adapter, store, inner := newProvisioningTestRig(t)
			intakeRow := seedPendingIntake(t, store, inner, tc.planReq)

			var accepted intake.CustomerIntake
			if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
				row, err := adapter.AcceptIntake(ctx, tx, intake.AcceptIntakeRequest{
					IntakeID:         intakeRow.ID,
					AcceptedByUserID: "us_OPER1",
				})
				accepted = row
				return err
			}); err != nil {
				t.Fatalf("AcceptIntake: %v", err)
			}

			// tenant_id 채워졌는지만 직접 검증 + plan 값은 DB 조회로 확인.
			var plan string
			if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
				row := tx.QueryRow(ctx, `SELECT plan FROM tenants WHERE id = ?`, string(accepted.TenantID))
				return row.Scan(&plan)
			}); err != nil {
				t.Fatalf("query plan: %v", err)
			}
			if plan != string(tc.wantPlan) {
				t.Errorf("plan = %q, want %q", plan, string(tc.wantPlan))
			}
		})
	}
}

// === AcceptIntake — rollback 검증 ===

func TestProvisioningAcceptRollsBackWhenTenantCreateFails(t *testing.T) {
	t.Parallel()
	adapter, store, inner := newProvisioningTestRig(t)

	// 이메일이 invalid → tenant.Create가 ErrInvalidEmail 반환.
	// intake 도메인의 CreateIntake는 이메일 validation을 통과한 상태로 시드 → 강제로
	// tenant.Create만 실패시키기 위해 별 방법: intake 시드 후 row의 email을 SQL로 corrupt.
	intakeRow := seedPendingIntake(t, store, inner, intake.PlanPro)
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE customer_intakes SET primary_contact_email = ? WHERE id = ?`,
			"not-an-email", intakeRow.ID)
		return e
	}); err != nil {
		t.Fatalf("corrupt email: %v", err)
	}

	err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		_, e := adapter.AcceptIntake(ctx, tx, intake.AcceptIntakeRequest{
			IntakeID:         intakeRow.ID,
			AcceptedByUserID: "us_OPER1",
		})
		return e
	})
	if err == nil {
		t.Fatal("AcceptIntake should fail when tenant.Create fails (invalid email)")
	}
	if !errors.Is(err, tenant.ErrInvalidEmail) {
		t.Errorf("err = %v, want tenant.ErrInvalidEmail", err)
	}

	// rollback 검증 — tenant 0건, intake는 pending 유지.
	if n := countTenantsExcludingSystem(t, store); n != 0 {
		t.Errorf("tenant count = %d, want 0 (rolled back)", n)
	}
	var status string
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		row := tx.QueryRow(ctx, `SELECT status FROM customer_intakes WHERE id = ?`, intakeRow.ID)
		return row.Scan(&status)
	}); err != nil {
		t.Fatalf("query status: %v", err)
	}
	if status != string(intake.StatusPending) {
		t.Errorf("intake status = %q, want pending (rolled back)", status)
	}
}

func TestProvisioningAcceptReturnsErrIntakeNotPendingWhenAlreadyAccepted(t *testing.T) {
	t.Parallel()
	adapter, store, inner := newProvisioningTestRig(t)
	intakeRow := seedPendingIntake(t, store, inner, intake.PlanPro)

	// 첫 accept 성공.
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		_, e := adapter.AcceptIntake(ctx, tx, intake.AcceptIntakeRequest{
			IntakeID:         intakeRow.ID,
			AcceptedByUserID: "us_OPER1",
		})
		return e
	}); err != nil {
		t.Fatalf("first AcceptIntake: %v", err)
	}

	// 두 번째 accept는 ErrIntakeNotPending — tenant 추가 생성도 X (rollback).
	err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		_, e := adapter.AcceptIntake(ctx, tx, intake.AcceptIntakeRequest{
			IntakeID:         intakeRow.ID,
			AcceptedByUserID: "us_OPER1",
		})
		return e
	})
	if !errors.Is(err, intake.ErrIntakeNotPending) {
		t.Errorf("err = %v, want ErrIntakeNotPending", err)
	}
	if n := countTenantsExcludingSystem(t, store); n != 1 {
		t.Errorf("tenant count = %d, want 1 (second accept rolled back)", n)
	}
}

// === delegate 4 메서드 검증 ===

func TestProvisioningDelegatesCreateIntake(t *testing.T) {
	t.Parallel()
	adapter, store, _ := newProvisioningTestRig(t)

	var created intake.CustomerIntake
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		row, err := adapter.CreateIntake(ctx, tx, intake.CreateIntakeRequest{
			OrganizationName:    "Beta Robotics",
			PrimaryContactEmail: "ops@beta.example",
			PrimaryContactName:  "Beta Ops",
			PlanRequest:         intake.PlanCommunity,
			IntendedUse:         "delegate test.",
		})
		created = row
		return err
	}); err != nil {
		t.Fatalf("CreateIntake: %v", err)
	}
	if created.Status != intake.StatusPending {
		t.Errorf("Status = %q, want pending", created.Status)
	}
	// Create 단계에서는 tenant 생성 0 — wrap이 Accept만 가로채는지 검증.
	if n := countTenantsExcludingSystem(t, store); n != 0 {
		t.Errorf("tenant count = %d, want 0 (Create는 wrap 영향 없어야 함)", n)
	}
}

func TestProvisioningDelegatesGetIntake(t *testing.T) {
	t.Parallel()
	adapter, store, inner := newProvisioningTestRig(t)
	intakeRow := seedPendingIntake(t, store, inner, intake.PlanPro)

	var got intake.CustomerIntake
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		row, err := adapter.GetIntake(ctx, tx, intakeRow.ID)
		got = row
		return err
	}); err != nil {
		t.Fatalf("GetIntake: %v", err)
	}
	if got.ID != intakeRow.ID {
		t.Errorf("ID = %q, want %q", got.ID, intakeRow.ID)
	}
}

func TestProvisioningDelegatesListIntakes(t *testing.T) {
	t.Parallel()
	adapter, store, inner := newProvisioningTestRig(t)
	_ = seedPendingIntake(t, store, inner, intake.PlanPro)
	_ = seedPendingIntake(t, store, inner, intake.PlanCommunity)

	var rows []intake.CustomerIntake
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		out, err := adapter.ListIntakes(ctx, tx, intake.ListIntakesFilter{})
		rows = out
		return err
	}); err != nil {
		t.Fatalf("ListIntakes: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("len(rows) = %d, want 2", len(rows))
	}
}

func TestProvisioningDelegatesRejectIntake(t *testing.T) {
	t.Parallel()
	adapter, store, inner := newProvisioningTestRig(t)
	intakeRow := seedPendingIntake(t, store, inner, intake.PlanPro)

	var rejected intake.CustomerIntake
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		row, err := adapter.RejectIntake(ctx, tx, intake.RejectIntakeRequest{
			IntakeID:         intakeRow.ID,
			RejectedByUserID: "us_OPER1",
			RejectionReason:  "duplicate registration",
		})
		rejected = row
		return err
	}); err != nil {
		t.Fatalf("RejectIntake: %v", err)
	}
	if rejected.Status != intake.StatusRejected {
		t.Errorf("Status = %q, want rejected", rejected.Status)
	}
	// Reject은 tenant 생성 X — wrap이 Reject 경로에 개입하지 않는지 검증.
	if n := countTenantsExcludingSystem(t, store); n != 0 {
		t.Errorf("tenant count = %d, want 0 (Reject는 wrap 영향 없어야 함)", n)
	}
}
