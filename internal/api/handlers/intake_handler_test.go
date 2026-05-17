package handlers

// intake_handler_test.go — Customer onboarding R1 Stage 2 단위 테스트.
//
// design doc `docs/design/notes/customer-onboarding-design.md` §7 R1 Stage 2 산출.
//
// 검증 범위:
//   - POST /api/v1/customers/intake 정상 (201 + pending status + 응답 형식)
//   - POST 검증 실패 — 422 (organization·email·plan·intendedUse·contactName)
//   - GET /api/v1/customers/intakes (list) — 200 + DESC 정렬 + status filter
//   - GET /api/v1/customers/intakes/{id} — 200 / 404 (not found)
//   - POST /api/v1/customers/intakes/{id}:accept — 200 + status=accepted + 멱등 거부
//   - POST /api/v1/customers/intakes/{id}:reject — 200 + status=rejected + reason 필수
//
// RBAC matrix는 별도 통합 test (rbac_integration_test.go)의 endpoint 매트릭스에 포함 —
// 본 파일은 handler 자체 단위 (storage·intake.Service in-memory + 직접 호출).
//
// white-box 패키지 (claimsCtxKey 등 internal symbol 활용).

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/ssabro/rosshield/internal/domain/intake"
	intakerepo "github.com/ssabro/rosshield/internal/domain/intake/sqliterepo"
	"github.com/ssabro/rosshield/internal/domain/tenant"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/idgen"
	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/sqlite"
)

// === harness ===

// newIntakeHandlers는 in-memory SQLite + intake repo + Handlers를 결선합니다.
//
// AuthMiddleware·RBAC middleware는 mount하지 않음 — handler 자체 동작만 단위 검증.
// RBAC integration test (rbac_integration_test.go)가 별도 매트릭스로 endpoint 권한 회귀 차단.
func newIntakeHandlers(t *testing.T) (*Handlers, storage.Storage) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "intake.db")
	store, err := sqlite.Open(storage.Config{Driver: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	repo := intakerepo.New(intakerepo.Deps{
		Clock: clock.System(),
		IDGen: idgen.NewULID(),
	})
	h := &Handlers{deps: Deps{
		Storage: store,
		Clock:   clock.System(),
		Intake:  repo,
	}}
	return h, store
}

// withAdminClaims는 운영자 admin AccessClaims를 ctx에 주입합니다.
//
// CreateInvitation 등 다른 admin handler 패턴과 일관 — Subject는 audit emit 시 사용.
func withAdminClaims(req *http.Request) *http.Request {
	ctx := withClaims(req.Context(), tenant.AccessClaims{
		Subject:  "us_OPER1",
		TenantID: "system",
		Roles:    []string{"admin"},
	})
	return req.WithContext(ctx)
}

// sampleIntakeBody는 정상 intake JSON body를 반환합니다.
func sampleIntakeBody() string {
	return `{
		"organizationName": "Acme Robotics",
		"primaryContactEmail": "Admin@Acme.Example",
		"primaryContactName": "Acme Admin",
		"planRequest": "pro",
		"intendedUse": "ROS2 fleet 보안 감사 — warehouse-a (50대) PoC."
	}`
}

// === POST /api/v1/customers/intake ===

func TestCreateCustomerIntakeReturns201WithPendingStatus(t *testing.T) {
	t.Parallel()
	h, _ := newIntakeHandlers(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers/intake",
		bytes.NewBufferString(sampleIntakeBody()))
	req = withAdminClaims(req)
	rec := httptest.NewRecorder()

	h.CreateCustomerIntake(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("code = %d, body=%s, want 201", rec.Code, rec.Body.String())
	}
	var body struct {
		ID        string `json:"id"`
		Status    string `json:"status"`
		CreatedAt string `json:"createdAt"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.HasPrefix(body.ID, "ci_") {
		t.Errorf("ID = %q, want ci_ prefix", body.ID)
	}
	if body.Status != "pending" {
		t.Errorf("Status = %q, want pending", body.Status)
	}
	if body.CreatedAt == "" {
		t.Error("CreatedAt should be non-empty RFC3339")
	}
}

func TestCreateCustomerIntakeNormalizesEmail(t *testing.T) {
	t.Parallel()
	h, store := newIntakeHandlers(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers/intake",
		bytes.NewBufferString(sampleIntakeBody()))
	req = withAdminClaims(req)
	rec := httptest.NewRecorder()
	h.CreateCustomerIntake(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: code=%d body=%s", rec.Code, rec.Body.String())
	}

	// repo에서 email lowercase normalize 확인.
	var got intake.CustomerIntake
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		l, err := h.deps.Intake.ListIntakes(ctx, tx, intake.ListIntakesFilter{})
		if err != nil {
			return err
		}
		if len(l) != 1 {
			t.Fatalf("list len = %d, want 1", len(l))
		}
		got = l[0]
		return nil
	}); err != nil {
		t.Fatalf("list: %v", err)
	}
	if got.PrimaryContactEmail != "admin@acme.example" {
		t.Errorf("email = %q, want lowercase normalized", got.PrimaryContactEmail)
	}
}

func TestCreateCustomerIntakeValidationErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		body    string
		wantErr string // 응답 body의 error 부분 substring
	}{
		{
			name:    "empty organization",
			body:    `{"organizationName":"","primaryContactEmail":"a@b.c","primaryContactName":"X","planRequest":"pro","intendedUse":"x"}`,
			wantErr: "OrganizationName",
		},
		{
			name:    "invalid email",
			body:    `{"organizationName":"Acme","primaryContactEmail":"not-an-email","primaryContactName":"X","planRequest":"pro","intendedUse":"x"}`,
			wantErr: "PrimaryContactEmail",
		},
		{
			name:    "invalid plan",
			body:    `{"organizationName":"Acme","primaryContactEmail":"a@b.c","primaryContactName":"X","planRequest":"ultra","intendedUse":"x"}`,
			wantErr: "PlanRequest",
		},
		{
			name:    "empty intended use",
			body:    `{"organizationName":"Acme","primaryContactEmail":"a@b.c","primaryContactName":"X","planRequest":"pro","intendedUse":""}`,
			wantErr: "IntendedUse",
		},
		{
			name:    "empty contact name",
			body:    `{"organizationName":"Acme","primaryContactEmail":"a@b.c","primaryContactName":"","planRequest":"pro","intendedUse":"x"}`,
			wantErr: "PrimaryContactName",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h, _ := newIntakeHandlers(t)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/customers/intake",
				bytes.NewBufferString(tc.body))
			req = withAdminClaims(req)
			rec := httptest.NewRecorder()
			h.CreateCustomerIntake(rec, req)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("code = %d, body=%s, want 422", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.wantErr) {
				t.Errorf("body = %q, want substring %q", rec.Body.String(), tc.wantErr)
			}
		})
	}
}

func TestCreateCustomerIntakeMalformedJSONReturns400(t *testing.T) {
	t.Parallel()
	h, _ := newIntakeHandlers(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers/intake",
		bytes.NewBufferString(`{not json`))
	req = withAdminClaims(req)
	rec := httptest.NewRecorder()
	h.CreateCustomerIntake(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, body=%s, want 400", rec.Code, rec.Body.String())
	}
}

func TestCreateCustomerIntakeWithoutServiceReturns503(t *testing.T) {
	t.Parallel()
	h := &Handlers{deps: Deps{Intake: nil}}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers/intake",
		bytes.NewBufferString(sampleIntakeBody()))
	req = withAdminClaims(req)
	rec := httptest.NewRecorder()
	h.CreateCustomerIntake(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("code = %d, want 503", rec.Code)
	}
}

// === GET /api/v1/customers/intakes (list) ===

func TestListCustomerIntakesReturnsCreatedRows(t *testing.T) {
	t.Parallel()
	h, _ := newIntakeHandlers(t)

	// 3건 생성.
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/customers/intake",
			bytes.NewBufferString(sampleIntakeBody()))
		req = withAdminClaims(req)
		rec := httptest.NewRecorder()
		h.CreateCustomerIntake(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create %d: code=%d", i, rec.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/customers/intakes", nil)
	req = withAdminClaims(req)
	rec := httptest.NewRecorder()
	h.ListCustomerIntakes(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list code = %d, body=%s", rec.Code, rec.Body.String())
	}

	var body struct {
		Intakes []map[string]any `json:"intakes"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Intakes) != 3 {
		t.Errorf("len = %d, want 3", len(body.Intakes))
	}
}

func TestListCustomerIntakesFiltersByStatus(t *testing.T) {
	t.Parallel()
	h, _ := newIntakeHandlers(t)

	// 1건 생성 → pending only.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers/intake",
		bytes.NewBufferString(sampleIntakeBody()))
	req = withAdminClaims(req)
	rec := httptest.NewRecorder()
	h.CreateCustomerIntake(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/customers/intakes?status=pending", nil)
	listReq = withAdminClaims(listReq)
	listRec := httptest.NewRecorder()
	h.ListCustomerIntakes(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list pending: %d %s", listRec.Code, listRec.Body.String())
	}
	var body struct {
		Intakes []map[string]any `json:"intakes"`
	}
	_ = json.NewDecoder(listRec.Body).Decode(&body)
	if len(body.Intakes) != 1 {
		t.Errorf("pending len = %d, want 1", len(body.Intakes))
	}

	// status=accepted → 0건.
	listReq2 := httptest.NewRequest(http.MethodGet, "/api/v1/customers/intakes?status=accepted", nil)
	listReq2 = withAdminClaims(listReq2)
	listRec2 := httptest.NewRecorder()
	h.ListCustomerIntakes(listRec2, listReq2)
	if listRec2.Code != http.StatusOK {
		t.Fatalf("list accepted: %d %s", listRec2.Code, listRec2.Body.String())
	}
	var body2 struct {
		Intakes []map[string]any `json:"intakes"`
	}
	_ = json.NewDecoder(listRec2.Body).Decode(&body2)
	if len(body2.Intakes) != 0 {
		t.Errorf("accepted len = %d, want 0", len(body2.Intakes))
	}
}

func TestListCustomerIntakesInvalidStatusReturns400(t *testing.T) {
	t.Parallel()
	h, _ := newIntakeHandlers(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/customers/intakes?status=foo", nil)
	req = withAdminClaims(req)
	rec := httptest.NewRecorder()
	h.ListCustomerIntakes(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("code = %d, body=%s, want 400", rec.Code, rec.Body.String())
	}
}

// === GET /api/v1/customers/intakes/{id} ===

func TestGetCustomerIntakeReturnsRow(t *testing.T) {
	t.Parallel()
	h, _ := newIntakeHandlers(t)

	// 1건 생성.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers/intake",
		bytes.NewBufferString(sampleIntakeBody()))
	req = withAdminClaims(req)
	rec := httptest.NewRecorder()
	h.CreateCustomerIntake(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&created)

	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/customers/intakes/"+created.ID, nil)
	getReq = withAdminClaims(getReq)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("intakeId", created.ID)
	getReq = getReq.WithContext(context.WithValue(getReq.Context(), chi.RouteCtxKey, rctx))
	getRec := httptest.NewRecorder()
	h.GetCustomerIntake(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get: %d %s", getRec.Code, getRec.Body.String())
	}
	var got struct {
		ID               string `json:"id"`
		OrganizationName string `json:"organizationName"`
		Status           string `json:"status"`
	}
	_ = json.NewDecoder(getRec.Body).Decode(&got)
	if got.ID != created.ID {
		t.Errorf("ID mismatch: %q vs %q", got.ID, created.ID)
	}
	if got.OrganizationName != "Acme Robotics" {
		t.Errorf("OrganizationName = %q", got.OrganizationName)
	}
	if got.Status != "pending" {
		t.Errorf("Status = %q", got.Status)
	}
}

func TestGetCustomerIntakeNotFoundReturns404(t *testing.T) {
	t.Parallel()
	h, _ := newIntakeHandlers(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/customers/intakes/ci_NONEXISTENT", nil)
	req = withAdminClaims(req)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("intakeId", "ci_NONEXISTENT")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	h.GetCustomerIntake(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("code = %d, want 404", rec.Code)
	}
}

// === POST /api/v1/customers/intakes/{id}:accept ===

func TestAcceptCustomerIntakeTransitionsToAccepted(t *testing.T) {
	t.Parallel()
	h, _ := newIntakeHandlers(t)

	// create.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers/intake",
		bytes.NewBufferString(sampleIntakeBody()))
	req = withAdminClaims(req)
	rec := httptest.NewRecorder()
	h.CreateCustomerIntake(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d", rec.Code)
	}
	var created struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&created)

	// accept.
	acceptReq := httptest.NewRequest(http.MethodPost,
		"/api/v1/customers/intakes/"+created.ID+":accept", nil)
	acceptReq = withAdminClaims(acceptReq)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("intakeId", created.ID)
	acceptReq = acceptReq.WithContext(context.WithValue(acceptReq.Context(), chi.RouteCtxKey, rctx))
	acceptRec := httptest.NewRecorder()
	h.AcceptCustomerIntake(acceptRec, acceptReq)
	if acceptRec.Code != http.StatusOK {
		t.Fatalf("accept: %d %s", acceptRec.Code, acceptRec.Body.String())
	}
	var accepted struct {
		ID               string `json:"id"`
		Status           string `json:"status"`
		AcceptedAt       string `json:"acceptedAt,omitempty"`
		AcceptedByUserID string `json:"acceptedByUserId,omitempty"`
	}
	_ = json.NewDecoder(acceptRec.Body).Decode(&accepted)
	if accepted.Status != "accepted" {
		t.Errorf("status = %q, want accepted", accepted.Status)
	}
	if accepted.AcceptedAt == "" {
		t.Error("AcceptedAt should be set")
	}
	if accepted.AcceptedByUserID != "us_OPER1" {
		t.Errorf("AcceptedByUserID = %q, want us_OPER1", accepted.AcceptedByUserID)
	}
}

func TestAcceptCustomerIntakeAlreadyAcceptedReturns409(t *testing.T) {
	t.Parallel()
	h, _ := newIntakeHandlers(t)

	// create + accept once.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers/intake",
		bytes.NewBufferString(sampleIntakeBody()))
	req = withAdminClaims(req)
	rec := httptest.NewRecorder()
	h.CreateCustomerIntake(rec, req)
	var created struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&created)

	for i := 0; i < 2; i++ {
		acceptReq := httptest.NewRequest(http.MethodPost,
			"/api/v1/customers/intakes/"+created.ID+":accept", nil)
		acceptReq = withAdminClaims(acceptReq)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("intakeId", created.ID)
		acceptReq = acceptReq.WithContext(context.WithValue(acceptReq.Context(), chi.RouteCtxKey, rctx))
		acceptRec := httptest.NewRecorder()
		h.AcceptCustomerIntake(acceptRec, acceptReq)
		switch i {
		case 0:
			if acceptRec.Code != http.StatusOK {
				t.Fatalf("first accept: %d %s", acceptRec.Code, acceptRec.Body.String())
			}
		case 1:
			if acceptRec.Code != http.StatusConflict {
				t.Errorf("second accept: code=%d body=%s want 409", acceptRec.Code, acceptRec.Body.String())
			}
		}
	}
}

func TestAcceptCustomerIntakeNotFoundReturns404(t *testing.T) {
	t.Parallel()
	h, _ := newIntakeHandlers(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers/intakes/ci_NONE:accept", nil)
	req = withAdminClaims(req)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("intakeId", "ci_NONE")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	h.AcceptCustomerIntake(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("code = %d, want 404", rec.Code)
	}
}

// === POST /api/v1/customers/intakes/{id}:reject ===

func TestRejectCustomerIntakeTransitionsToRejectedWithReason(t *testing.T) {
	t.Parallel()
	h, _ := newIntakeHandlers(t)

	// create.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers/intake",
		bytes.NewBufferString(sampleIntakeBody()))
	req = withAdminClaims(req)
	rec := httptest.NewRecorder()
	h.CreateCustomerIntake(rec, req)
	var created struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&created)

	// reject.
	rejectBody := `{"reason":"out of scope (heavy industrial fleet)"}`
	rejectReq := httptest.NewRequest(http.MethodPost,
		"/api/v1/customers/intakes/"+created.ID+":reject",
		bytes.NewBufferString(rejectBody))
	rejectReq = withAdminClaims(rejectReq)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("intakeId", created.ID)
	rejectReq = rejectReq.WithContext(context.WithValue(rejectReq.Context(), chi.RouteCtxKey, rctx))
	rejectRec := httptest.NewRecorder()
	h.RejectCustomerIntake(rejectRec, rejectReq)
	if rejectRec.Code != http.StatusOK {
		t.Fatalf("reject: %d %s", rejectRec.Code, rejectRec.Body.String())
	}
	var rejected struct {
		Status          string `json:"status"`
		RejectedAt      string `json:"rejectedAt,omitempty"`
		RejectionReason string `json:"rejectionReason,omitempty"`
	}
	_ = json.NewDecoder(rejectRec.Body).Decode(&rejected)
	if rejected.Status != "rejected" {
		t.Errorf("status = %q, want rejected", rejected.Status)
	}
	if rejected.RejectedAt == "" {
		t.Error("RejectedAt should be set")
	}
	if !strings.Contains(rejected.RejectionReason, "out of scope") {
		t.Errorf("RejectionReason = %q", rejected.RejectionReason)
	}
}

func TestRejectCustomerIntakeRequiresReason(t *testing.T) {
	t.Parallel()
	h, _ := newIntakeHandlers(t)

	// create.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers/intake",
		bytes.NewBufferString(sampleIntakeBody()))
	req = withAdminClaims(req)
	rec := httptest.NewRecorder()
	h.CreateCustomerIntake(rec, req)
	var created struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&created)

	// 빈 reason.
	rejectReq := httptest.NewRequest(http.MethodPost,
		"/api/v1/customers/intakes/"+created.ID+":reject",
		bytes.NewBufferString(`{"reason":""}`))
	rejectReq = withAdminClaims(rejectReq)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("intakeId", created.ID)
	rejectReq = rejectReq.WithContext(context.WithValue(rejectReq.Context(), chi.RouteCtxKey, rctx))
	rejectRec := httptest.NewRecorder()
	h.RejectCustomerIntake(rejectRec, rejectReq)
	if rejectRec.Code != http.StatusUnprocessableEntity {
		t.Errorf("code = %d, body=%s, want 422", rejectRec.Code, rejectRec.Body.String())
	}
}

func TestRejectCustomerIntakeAlreadyRejectedReturns409(t *testing.T) {
	t.Parallel()
	h, _ := newIntakeHandlers(t)

	// create + reject once.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers/intake",
		bytes.NewBufferString(sampleIntakeBody()))
	req = withAdminClaims(req)
	rec := httptest.NewRecorder()
	h.CreateCustomerIntake(rec, req)
	var created struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&created)

	body := `{"reason":"out of scope"}`
	for i := 0; i < 2; i++ {
		rejectReq := httptest.NewRequest(http.MethodPost,
			"/api/v1/customers/intakes/"+created.ID+":reject",
			bytes.NewBufferString(body))
		rejectReq = withAdminClaims(rejectReq)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("intakeId", created.ID)
		rejectReq = rejectReq.WithContext(context.WithValue(rejectReq.Context(), chi.RouteCtxKey, rctx))
		rejectRec := httptest.NewRecorder()
		h.RejectCustomerIntake(rejectRec, rejectReq)
		switch i {
		case 0:
			if rejectRec.Code != http.StatusOK {
				t.Fatalf("first reject: %d %s", rejectRec.Code, rejectRec.Body.String())
			}
		case 1:
			if rejectRec.Code != http.StatusConflict {
				t.Errorf("second reject: code=%d body=%s want 409", rejectRec.Code, rejectRec.Body.String())
			}
		}
	}
}
