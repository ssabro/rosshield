package sqliterepo_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/domain/intake"
	"github.com/ssabro/rosshield/internal/domain/intake/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/idgen"
	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/sqlite"
)

// newTestRepo는 in-memory 테스트용 (Repo, Storage)를 반환합니다.
//
// intake 도메인은 tenant scope이 아니므로 Bootstrap Tx로 R/W (운영자 admin 전역 권한 가정).
func newTestRepo(t *testing.T) (*sqliterepo.Repo, storage.Storage) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "intake.db")
	store, err := sqlite.Open(storage.Config{Driver: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	repo := sqliterepo.New(sqliterepo.Deps{
		Clock: clock.System(),
		IDGen: idgen.NewULID(),
	})
	return repo, store
}

func sampleCreate() intake.CreateIntakeRequest {
	return intake.CreateIntakeRequest{
		OrganizationName:    "Acme Robotics",
		PrimaryContactEmail: "Admin@Acme.Example",
		PrimaryContactName:  "Acme Admin",
		PlanRequest:         intake.PlanPro,
		IntendedUse:         "ROS2 fleet 보안 감사 — warehouse-a (50대) 자체 배포 PoC.",
	}
}

// === CreateIntake ===

func TestCreateIntakePersistsRowWithDefaults(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)

	var got intake.CustomerIntake
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		row, err := repo.CreateIntake(ctx, tx, sampleCreate())
		got = row
		return err
	}); err != nil {
		t.Fatalf("CreateIntake: %v", err)
	}

	if !strings.HasPrefix(got.ID, "ci_") {
		t.Errorf("ID = %q, want ci_ prefix", got.ID)
	}
	if got.Status != intake.StatusPending {
		t.Errorf("Status = %q, want pending", got.Status)
	}
	if got.PrimaryContactEmail != "admin@acme.example" {
		t.Errorf("PrimaryContactEmail = %q, want lowercase normalized", got.PrimaryContactEmail)
	}
	if got.AcceptedAt != nil || got.RejectedAt != nil {
		t.Error("AcceptedAt/RejectedAt should be nil for pending row")
	}
	if got.TenantID != "" {
		t.Errorf("TenantID = %q, want empty for pending row", got.TenantID)
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set")
	}
}

func TestCreateIntakeValidationErrors(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)

	cases := []struct {
		name    string
		mutate  func(req *intake.CreateIntakeRequest)
		wantErr error
	}{
		{
			name:    "empty organization",
			mutate:  func(req *intake.CreateIntakeRequest) { req.OrganizationName = "  " },
			wantErr: intake.ErrEmptyOrganization,
		},
		{
			name:    "invalid email",
			mutate:  func(req *intake.CreateIntakeRequest) { req.PrimaryContactEmail = "not-an-email" },
			wantErr: intake.ErrInvalidEmail,
		},
		{
			name:    "empty email",
			mutate:  func(req *intake.CreateIntakeRequest) { req.PrimaryContactEmail = "" },
			wantErr: intake.ErrInvalidEmail,
		},
		{
			name:    "empty contact name",
			mutate:  func(req *intake.CreateIntakeRequest) { req.PrimaryContactName = "" },
			wantErr: intake.ErrEmptyContactName,
		},
		{
			name:    "invalid plan",
			mutate:  func(req *intake.CreateIntakeRequest) { req.PlanRequest = intake.PlanRequest("ultra") },
			wantErr: intake.ErrInvalidPlanRequest,
		},
		{
			name:    "empty intended use",
			mutate:  func(req *intake.CreateIntakeRequest) { req.IntendedUse = "" },
			wantErr: intake.ErrEmptyIntendedUse,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := sampleCreate()
			tc.mutate(&req)
			err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
				_, err := repo.CreateIntake(ctx, tx, req)
				return err
			})
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// === GetIntake ===

func TestGetIntakeReturnsCreated(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)

	var created, fetched intake.CustomerIntake
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		c, err := repo.CreateIntake(ctx, tx, sampleCreate())
		if err != nil {
			return err
		}
		created = c
		f, err := repo.GetIntake(ctx, tx, created.ID)
		fetched = f
		return err
	}); err != nil {
		t.Fatalf("flow: %v", err)
	}

	if fetched.ID != created.ID {
		t.Errorf("ID mismatch: got %q want %q", fetched.ID, created.ID)
	}
	if fetched.OrganizationName != created.OrganizationName {
		t.Errorf("OrganizationName mismatch")
	}
	if fetched.PlanRequest != intake.PlanPro {
		t.Errorf("PlanRequest = %q, want pro", fetched.PlanRequest)
	}
}

func TestGetIntakeReturnsNotFoundForUnknownID(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)

	err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		_, err := repo.GetIntake(ctx, tx, "ci_NONEXISTENT")
		return err
	})
	if !errors.Is(err, intake.ErrIntakeNotFound) {
		t.Errorf("err = %v, want ErrIntakeNotFound", err)
	}
}

// === ListIntakes ===

func TestListIntakesOrdersByCreatedAtDesc(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)

	// 3건 생성 — sample 변형으로 plan 다르게.
	var ids []string
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		for i, plan := range []intake.PlanRequest{intake.PlanCommunity, intake.PlanPro, intake.PlanEnterprise} {
			req := sampleCreate()
			req.PlanRequest = plan
			req.OrganizationName = "Org-" + string(rune('A'+i))
			row, err := repo.CreateIntake(ctx, tx, req)
			if err != nil {
				return err
			}
			ids = append(ids, row.ID)
			// monotonic ULID 보장으로 created_at + ID 정렬 일관 — 아무 sleep 불요.
			_ = time.Now() // explicit no-op to keep import clean
		}
		return nil
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	var listed []intake.CustomerIntake
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		l, err := repo.ListIntakes(ctx, tx, intake.ListIntakesFilter{})
		listed = l
		return err
	}); err != nil {
		t.Fatalf("list: %v", err)
	}

	if len(listed) != 3 {
		t.Fatalf("len = %d, want 3", len(listed))
	}
	// DESC: 최신 (3번째 생성)이 listed[0].
	for i := range listed[:len(listed)-1] {
		if listed[i].CreatedAt.Before(listed[i+1].CreatedAt) {
			t.Errorf("not DESC: listed[%d].CreatedAt < listed[%d].CreatedAt", i, i+1)
		}
	}
}

func TestListIntakesFiltersByStatus(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)

	// 3건 생성, 1건 accept, 1건 reject — pending 1건 남음.
	var pendingID, acceptedID, rejectedID string
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		req := sampleCreate()
		p, err := repo.CreateIntake(ctx, tx, req)
		if err != nil {
			return err
		}
		pendingID = p.ID

		req2 := sampleCreate()
		req2.OrganizationName = "ToAccept"
		a, err := repo.CreateIntake(ctx, tx, req2)
		if err != nil {
			return err
		}
		acceptedID = a.ID
		if _, err := repo.AcceptIntake(ctx, tx, intake.AcceptIntakeRequest{
			IntakeID:         a.ID,
			AcceptedByUserID: "us_OPER1",
		}); err != nil {
			return err
		}

		req3 := sampleCreate()
		req3.OrganizationName = "ToReject"
		r, err := repo.CreateIntake(ctx, tx, req3)
		if err != nil {
			return err
		}
		rejectedID = r.ID
		_, err = repo.RejectIntake(ctx, tx, intake.RejectIntakeRequest{
			IntakeID:        r.ID,
			RejectionReason: "out of scope (heavy industrial)",
		})
		return err
	}); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		// pending only
		pending, err := repo.ListIntakes(ctx, tx, intake.ListIntakesFilter{Status: intake.StatusPending})
		if err != nil {
			return err
		}
		if len(pending) != 1 || pending[0].ID != pendingID {
			t.Errorf("pending list = %d items, ids=%v want 1 (id=%s)", len(pending), idsOf(pending), pendingID)
		}

		// accepted only
		accepted, err := repo.ListIntakes(ctx, tx, intake.ListIntakesFilter{Status: intake.StatusAccepted})
		if err != nil {
			return err
		}
		if len(accepted) != 1 || accepted[0].ID != acceptedID {
			t.Errorf("accepted list ids=%v want 1 (id=%s)", idsOf(accepted), acceptedID)
		}

		// rejected only
		rejected, err := repo.ListIntakes(ctx, tx, intake.ListIntakesFilter{Status: intake.StatusRejected})
		if err != nil {
			return err
		}
		if len(rejected) != 1 || rejected[0].ID != rejectedID {
			t.Errorf("rejected list ids=%v want 1 (id=%s)", idsOf(rejected), rejectedID)
		}
		return nil
	}); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

// === AcceptIntake ===

func TestAcceptIntakeTransitionsToAcceptedWithMetadata(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)

	var accepted intake.CustomerIntake
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		created, err := repo.CreateIntake(ctx, tx, sampleCreate())
		if err != nil {
			return err
		}
		a, err := repo.AcceptIntake(ctx, tx, intake.AcceptIntakeRequest{
			IntakeID:         created.ID,
			AcceptedByUserID: "us_OPER1",
			TenantID:         storage.TenantID("tn_NEW"),
		})
		accepted = a
		return err
	}); err != nil {
		t.Fatalf("flow: %v", err)
	}

	if accepted.Status != intake.StatusAccepted {
		t.Errorf("Status = %q, want accepted", accepted.Status)
	}
	if accepted.AcceptedAt == nil {
		t.Error("AcceptedAt should be set")
	}
	if accepted.AcceptedByUserID == nil || *accepted.AcceptedByUserID != "us_OPER1" {
		t.Errorf("AcceptedByUserID = %v, want us_OPER1", accepted.AcceptedByUserID)
	}
	if accepted.TenantID != "tn_NEW" {
		t.Errorf("TenantID = %q, want tn_NEW", accepted.TenantID)
	}
	if accepted.RejectedAt != nil {
		t.Error("RejectedAt should be nil for accepted row")
	}
}

func TestAcceptIntakeRejectsNonPendingTransition(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)

	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		created, err := repo.CreateIntake(ctx, tx, sampleCreate())
		if err != nil {
			return err
		}
		// 1차 accept 성공.
		if _, err := repo.AcceptIntake(ctx, tx, intake.AcceptIntakeRequest{
			IntakeID:         created.ID,
			AcceptedByUserID: "us_OPER1",
		}); err != nil {
			return err
		}
		// 2차 accept → ErrIntakeNotPending.
		_, err = repo.AcceptIntake(ctx, tx, intake.AcceptIntakeRequest{
			IntakeID:         created.ID,
			AcceptedByUserID: "us_OPER2",
		})
		if !errors.Is(err, intake.ErrIntakeNotPending) {
			t.Errorf("second accept err = %v, want ErrIntakeNotPending", err)
		}
		return nil
	}); err != nil {
		t.Fatalf("flow: %v", err)
	}
}

func TestAcceptIntakeReturnsNotFoundForUnknownID(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)

	err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		_, err := repo.AcceptIntake(ctx, tx, intake.AcceptIntakeRequest{
			IntakeID:         "ci_NONEXISTENT",
			AcceptedByUserID: "us_OPER1",
		})
		return err
	})
	if !errors.Is(err, intake.ErrIntakeNotFound) {
		t.Errorf("err = %v, want ErrIntakeNotFound", err)
	}
}

// === RejectIntake ===

func TestRejectIntakeTransitionsToRejectedWithReason(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)

	const reason = "out of scope (heavy industrial robot fleet)"
	var rejected intake.CustomerIntake
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		created, err := repo.CreateIntake(ctx, tx, sampleCreate())
		if err != nil {
			return err
		}
		r, err := repo.RejectIntake(ctx, tx, intake.RejectIntakeRequest{
			IntakeID:        created.ID,
			RejectionReason: reason,
		})
		rejected = r
		return err
	}); err != nil {
		t.Fatalf("flow: %v", err)
	}

	if rejected.Status != intake.StatusRejected {
		t.Errorf("Status = %q, want rejected", rejected.Status)
	}
	if rejected.RejectedAt == nil {
		t.Error("RejectedAt should be set")
	}
	if rejected.RejectionReason == nil || *rejected.RejectionReason != reason {
		t.Errorf("RejectionReason = %v, want %q", rejected.RejectionReason, reason)
	}
	if rejected.AcceptedAt != nil {
		t.Error("AcceptedAt should be nil for rejected row")
	}
}

func TestRejectIntakeRequiresReason(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)

	err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		created, err := repo.CreateIntake(ctx, tx, sampleCreate())
		if err != nil {
			return err
		}
		_, err = repo.RejectIntake(ctx, tx, intake.RejectIntakeRequest{
			IntakeID:        created.ID,
			RejectionReason: "",
		})
		return err
	})
	if !errors.Is(err, intake.ErrEmptyRejectionReason) {
		t.Errorf("err = %v, want ErrEmptyRejectionReason", err)
	}
}

func TestRejectIntakeRejectsNonPendingTransition(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)

	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		created, err := repo.CreateIntake(ctx, tx, sampleCreate())
		if err != nil {
			return err
		}
		// reject 후 다시 reject 시도 → ErrIntakeNotPending.
		if _, err := repo.RejectIntake(ctx, tx, intake.RejectIntakeRequest{
			IntakeID:        created.ID,
			RejectionReason: "first reject",
		}); err != nil {
			return err
		}
		_, err = repo.RejectIntake(ctx, tx, intake.RejectIntakeRequest{
			IntakeID:        created.ID,
			RejectionReason: "second reject",
		})
		if !errors.Is(err, intake.ErrIntakeNotPending) {
			t.Errorf("second reject err = %v, want ErrIntakeNotPending", err)
		}
		return nil
	}); err != nil {
		t.Fatalf("flow: %v", err)
	}
}

// === helpers ===

func idsOf(rows []intake.CustomerIntake) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}
