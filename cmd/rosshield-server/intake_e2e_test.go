package main

// intake_e2e_test.go — Customer onboarding R1 Stage 5 실 e2e 통합 테스트.
//
// design doc `docs/design/notes/customer-onboarding-design.md` §7 R1 Stage 5 + §3.1 영역 1.
//
// 시나리오 (실 HTTP → chi mux → handler → wrap adapter → DB):
//
//   1. Bootstrap (자동 마이그레이션 0030 + intake.Service 결선 + wrap adapter)
//   2. tenant + admin 시드 (tenant.Service.Create — admin role 자동 할당)
//   3. admin login → JWT access token
//   4. e2e A: POST /api/v1/customers/intake — 201 + status=pending + DB row 검증
//   5. e2e B: GET  /api/v1/customers/intakes — 200 + 생성된 row 회수
//   6. e2e C: POST /api/v1/customers/intakes/{id}:accept — 200 +
//             - DB intake row: status=accepted + accepted_at 채움 + tenant_id 채움
//             - DB tenants 테이블: 새 tenant row INSERT (organization name 일치)
//             - DB users 테이블: 새 admin user INSERT (primaryContactEmail 일치)
//   7. e2e D: 또 다른 intake 생성 후 POST :reject — 200 + status=rejected +
//             rejection_reason 채움 + tenant_id NULL 유지 (tenant 미생성)
//   8. e2e E: GET /api/v1/customers/intakes?status=foo (admin) → 400 (잘못된 status)
//   9. e2e F: POST /api/v1/customers/intake (JWT 없이) → 401 (인증 누락)
//
// RBAC 매트릭스(admin·operator·auditor의 5 endpoint별 권한 게이트)는 별도
// `internal/api/handlers/rbac_integration_test.go`가 100% cover — 본 e2e는 wrap adapter +
// 실 DB 결선 + HTTP layer 통합 흐름 검증에 집중. 권한 미달 시나리오는 401 인증 누락으로
// AuthMiddleware → RBAC 게이트 결선 동작 확인 (chi 라우터 mount + RequirePermission 결선).
//
// e2e 자체 패턴은 cmd/rosshield-server/scanrun_e2e_test.go(Bootstrap → httptest.Server →
// JWT login → 실 endpoint 호출) 일관 — design doc §7 권장 default = cmd/rosshield-server
// 결선 패턴.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ssabro/rosshield/internal/domain/tenant"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// === harness ===

// intakeE2EFixture는 Bootstrap된 platform + httptest.Server + admin JWT를 묶은 fixture입니다.
//
// 모든 e2e 시나리오가 동일 fixture를 공유 — 단일 Bootstrap (~비용 큼) + 시나리오별 독립 intake row.
type intakeE2EFixture struct {
	p          *Platform
	server     *httptest.Server
	adminToken string
	adminEmail string
}

// newIntakeE2EFixture는 Bootstrap + admin 시드 + login → JWT 회수를 한 흐름에 묶습니다.
//
// 시드 admin user는 admin role 자동 할당 (tenant.Service.Create가 보장) — intake 5 endpoint의
// ResourceTenantAdmin.Admin 게이트 통과.
func newIntakeE2EFixture(t *testing.T) *intakeE2EFixture {
	t.Helper()

	dir := t.TempDir()
	cfg := Config{
		DataDir: dir,
		Logger:  slog.New(slog.NewJSONHandler(io.Discard, nil)),
	}
	p, err := Bootstrap(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	t.Cleanup(func() {
		_ = p.Shutdown(context.Background())
	})

	const (
		adminEmail = "ops-admin@rosshield.example"
		adminPw    = "ops-admin-password-123"
	)

	// tenant + admin 시드 (Bootstrap Tx — tenant 생성 진입점).
	if err := p.Storage.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		_, e := p.Tenant.Create(ctx, tx, tenant.CreateRequest{
			Name:             "RossHield Ops",
			Plan:             tenant.PlanDesktopFree,
			AdminEmail:       adminEmail,
			AdminPassword:    adminPw,
			AdminDisplayName: "Ops Admin",
		})
		return e
	}); err != nil {
		t.Fatalf("seed admin tenant: %v", err)
	}

	mux := newMux(p)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	// admin login → JWT.
	loginBody, _ := json.Marshal(map[string]string{
		"email":    adminEmail,
		"password": adminPw,
	})
	resp, err := http.Post(server.URL+"/api/v1/auth/login", "application/json", bytes.NewReader(loginBody))
	if err != nil {
		t.Fatalf("POST login: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("login status %d: %s", resp.StatusCode, string(body))
	}
	var loginOut struct {
		AccessToken string `json:"accessToken"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&loginOut); err != nil {
		t.Fatalf("decode login: %v", err)
	}
	if loginOut.AccessToken == "" {
		t.Fatal("login returned empty accessToken")
	}

	return &intakeE2EFixture{
		p:          p,
		server:     server,
		adminToken: loginOut.AccessToken,
		adminEmail: adminEmail,
	}
}

// doJSON은 method + path + (옵션) admin 헤더 + (옵션) body로 HTTP 요청을 실행합니다.
//
// withAuth=false면 Authorization 헤더 미부착 — 401 인증 누락 시나리오용.
func (f *intakeE2EFixture) doJSON(t *testing.T, method, path string, body any, withAuth bool) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(buf)
	}
	req, err := http.NewRequest(method, f.server.URL+path, reader)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if withAuth {
		req.Header.Set("Authorization", "Bearer "+f.adminToken)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do %s %s: %v", method, path, err)
	}
	return resp
}

// === e2e 시나리오 6건 ===

// TestIntakeE2E_CreateListGetAcceptFullFlow — 정상 흐름 cover:
//
//	POST intake (201) → GET list (200 + 1건) → GET single (200) →
//	POST :accept (200 + tenant + admin user 자동 생성 검증).
func TestIntakeE2E_CreateListGetAcceptFullFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e — skipped in -short mode")
	}
	t.Parallel()

	f := newIntakeE2EFixture(t)

	// e2e A — POST /api/v1/customers/intake.
	createBody := map[string]any{
		"organizationName":    "Acme Robotics Corp",
		"primaryContactEmail": "Customer-Admin@Acme.Example",
		"primaryContactName":  "Acme Admin",
		"planRequest":         "pro",
		"intendedUse":         "ROS2 fleet 보안 감사 PoC — warehouse-a (50대).",
	}
	resp := f.doJSON(t, http.MethodPost, "/api/v1/customers/intake", createBody, true)
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		t.Fatalf("POST intake status=%d body=%s", resp.StatusCode, string(body))
	}
	var created struct {
		ID                  string `json:"id"`
		Status              string `json:"status"`
		PrimaryContactEmail string `json:"primaryContactEmail"`
		TenantID            string `json:"tenantId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		_ = resp.Body.Close()
		t.Fatalf("decode created: %v", err)
	}
	_ = resp.Body.Close()
	if created.ID == "" {
		t.Fatal("created.ID empty")
	}
	if created.Status != "pending" {
		t.Errorf("Status=%q, want pending", created.Status)
	}
	// email lowercase normalize 검증 — handler가 normalize 후 응답 반환.
	if created.PrimaryContactEmail != "customer-admin@acme.example" {
		t.Errorf("PrimaryContactEmail=%q, want lowercase normalized", created.PrimaryContactEmail)
	}
	if created.TenantID != "" {
		t.Errorf("TenantID=%q, want empty (pending)", created.TenantID)
	}

	// e2e B — GET /api/v1/customers/intakes — 생성된 row 회수.
	listResp := f.doJSON(t, http.MethodGet, "/api/v1/customers/intakes", nil, true)
	if listResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(listResp.Body)
		_ = listResp.Body.Close()
		t.Fatalf("GET list status=%d body=%s", listResp.StatusCode, string(body))
	}
	var listOut struct {
		Intakes []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"intakes"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&listOut); err != nil {
		_ = listResp.Body.Close()
		t.Fatalf("decode list: %v", err)
	}
	_ = listResp.Body.Close()
	if len(listOut.Intakes) != 1 || listOut.Intakes[0].ID != created.ID {
		t.Errorf("list mismatch: got %+v, want [%s]", listOut.Intakes, created.ID)
	}

	// e2e C — POST /api/v1/customers/intakes/{id}:accept — wrap adapter가 tenant + admin 자동 생성.
	acceptResp := f.doJSON(t, http.MethodPost, "/api/v1/customers/intakes/"+created.ID+":accept", nil, true)
	if acceptResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(acceptResp.Body)
		_ = acceptResp.Body.Close()
		t.Fatalf("POST accept status=%d body=%s", acceptResp.StatusCode, string(body))
	}
	var accepted struct {
		ID               string `json:"id"`
		Status           string `json:"status"`
		TenantID         string `json:"tenantId"`
		AcceptedAt       string `json:"acceptedAt"`
		AcceptedByUserID string `json:"acceptedByUserId"`
	}
	if err := json.NewDecoder(acceptResp.Body).Decode(&accepted); err != nil {
		_ = acceptResp.Body.Close()
		t.Fatalf("decode accept: %v", err)
	}
	_ = acceptResp.Body.Close()
	if accepted.Status != "accepted" {
		t.Errorf("accepted.Status=%q, want accepted", accepted.Status)
	}
	if accepted.TenantID == "" {
		t.Error("accepted.TenantID empty (wrap adapter는 tenant.Create + intake.TenantID 채움 보장)")
	}
	if accepted.AcceptedAt == "" {
		t.Error("AcceptedAt empty")
	}
	if accepted.AcceptedByUserID == "" {
		t.Error("AcceptedByUserID empty (JWT claims.Subject 채워야 함)")
	}

	// DB 검증 — tenants 테이블에 새 row INSERT (organization name 일치).
	verifyTenantCreated(t, f.p, accepted.TenantID, "Acme Robotics Corp")

	// DB 검증 — users 테이블에 새 admin user INSERT (lowercase email 일치).
	verifyAdminUserCreated(t, f.p, accepted.TenantID, "customer-admin@acme.example")

	// DB 검증 — customer_intakes 테이블 row의 status·tenant_id 영속화.
	verifyIntakeRowAccepted(t, f.p, created.ID, accepted.TenantID)
}

// TestIntakeE2E_RejectFlowDoesNotProvisionTenant — Reject 흐름:
//
//	POST intake → POST :reject → status=rejected + reason 채움 + tenant 미생성 검증.
func TestIntakeE2E_RejectFlowDoesNotProvisionTenant(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e — skipped in -short mode")
	}
	t.Parallel()

	f := newIntakeE2EFixture(t)

	// 별도 intake 생성 — Accept 시나리오와 격리.
	createBody := map[string]any{
		"organizationName":    "Beta Robotics LLC",
		"primaryContactEmail": "ops@beta.example",
		"primaryContactName":  "Beta Ops",
		"planRequest":         "community",
		"intendedUse":         "단일 robot 시범 — SKU 검증 필요.",
	}
	resp := f.doJSON(t, http.MethodPost, "/api/v1/customers/intake", createBody, true)
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		t.Fatalf("POST intake status=%d body=%s", resp.StatusCode, string(body))
	}
	var created struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&created)
	_ = resp.Body.Close()

	// pre-count: 시스템 외 tenant 수 (admin seed가 만든 RossHield Ops 1건 = 1).
	tenantsBefore := countNonSystemTenants(t, f.p)

	// e2e D — POST :reject.
	rejectBody := map[string]string{"reason": "SKU desktop_free 외 quota 명시 부족 — re-submit 필요"}
	rejectResp := f.doJSON(t, http.MethodPost, "/api/v1/customers/intakes/"+created.ID+":reject", rejectBody, true)
	if rejectResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(rejectResp.Body)
		_ = rejectResp.Body.Close()
		t.Fatalf("POST reject status=%d body=%s", rejectResp.StatusCode, string(body))
	}
	var rejected struct {
		Status          string  `json:"status"`
		RejectionReason *string `json:"rejectionReason"`
		TenantID        string  `json:"tenantId"`
	}
	if err := json.NewDecoder(rejectResp.Body).Decode(&rejected); err != nil {
		_ = rejectResp.Body.Close()
		t.Fatalf("decode reject: %v", err)
	}
	_ = rejectResp.Body.Close()
	if rejected.Status != "rejected" {
		t.Errorf("Status=%q, want rejected", rejected.Status)
	}
	if rejected.RejectionReason == nil || *rejected.RejectionReason == "" {
		t.Errorf("RejectionReason nil or empty, want %q", rejectBody["reason"])
	}
	if rejected.TenantID != "" {
		t.Errorf("TenantID=%q, want empty (reject는 tenant 미생성)", rejected.TenantID)
	}

	// post-count: tenant 수 변동 없음 — reject는 tenant.Create 트리거 X.
	tenantsAfter := countNonSystemTenants(t, f.p)
	if tenantsAfter != tenantsBefore {
		t.Errorf("non-system tenant count: before=%d, after=%d (reject는 tenant 생성 X)",
			tenantsBefore, tenantsAfter)
	}
}

// TestIntakeE2E_InvalidStatusFilterReturns400 — 잘못된 status query 거부.
func TestIntakeE2E_InvalidStatusFilterReturns400(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e — skipped in -short mode")
	}
	t.Parallel()

	f := newIntakeE2EFixture(t)

	resp := f.doJSON(t, http.MethodGet, "/api/v1/customers/intakes?status=foo", nil, true)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("status=%d, want 400; body=%s", resp.StatusCode, string(body))
	}
}

// TestIntakeE2E_UnauthenticatedReturns401 — JWT 없이 호출 시 AuthMiddleware 차단.
//
// design doc §6.1 + §9.2 — intake API는 운영자 admin 권한 필수 (anonymous 차단). 401은
// AuthMiddleware 응답 (RequirePermission 게이트 진입 전). 권한 미달 시나리오(403)는 별도
// rbac_integration_test.go 매트릭스가 RBAC 게이트별로 100% cover — 본 e2e는 인증 layer
// 결선 검증.
func TestIntakeE2E_UnauthenticatedReturns401(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e — skipped in -short mode")
	}
	t.Parallel()

	f := newIntakeE2EFixture(t)

	// JWT 없이 POST intake.
	createBody := map[string]any{
		"organizationName":    "Anon Co",
		"primaryContactEmail": "anon@anon.example",
		"primaryContactName":  "Anon",
		"planRequest":         "community",
		"intendedUse":         "should be rejected at auth layer",
	}
	resp := f.doJSON(t, http.MethodPost, "/api/v1/customers/intake", createBody, false)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("status=%d, want 401 (인증 누락); body=%s", resp.StatusCode, string(body))
	}
}

// === DB 검증 helper ===

// verifyTenantCreated는 tenants 테이블에 wrap adapter가 생성한 row를 직접 SELECT로 검증합니다.
//
// system tenant 제외. organization name과 일치하는 row가 정확히 1개여야 함.
func verifyTenantCreated(t *testing.T, p *Platform, tenantID, orgName string) {
	t.Helper()
	var name string
	if err := p.Storage.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		row := tx.QueryRow(ctx, `SELECT name FROM tenants WHERE id = ?`, tenantID)
		return row.Scan(&name)
	}); err != nil {
		t.Fatalf("query tenant %s: %v", tenantID, err)
	}
	if name != orgName {
		t.Errorf("tenant name=%q, want %q (intake.OrganizationName 매핑)", name, orgName)
	}
}

// verifyAdminUserCreated는 users 테이블에 admin user가 INSERT되었는지 검증합니다.
//
// tenant.Service.Create가 같은 Tx에 admin user 시드 + admin role 자동 할당하므로 user row
// 1개와 admin role binding 1개 이상 존재해야 함.
func verifyAdminUserCreated(t *testing.T, p *Platform, tenantID, email string) {
	t.Helper()
	tCtx := storage.WithTenantID(context.Background(), storage.TenantID(tenantID))
	var (
		userCount     int
		adminBindings int
	)
	if err := p.Storage.Tx(tCtx, func(ctx context.Context, tx storage.Tx) error {
		row := tx.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE tenant_id = ? AND email = ?`, tenantID, email)
		if e := row.Scan(&userCount); e != nil {
			return e
		}
		// admin role binding 1건 이상 — tenant.Service.Create가 admin role 자동 할당 보장.
		row2 := tx.QueryRow(ctx, `
SELECT COUNT(*)
  FROM user_roles ur
  JOIN roles r ON r.id = ur.role_id
  JOIN users u ON u.id = ur.user_id
 WHERE u.tenant_id = ? AND u.email = ? AND r.name = 'admin'`, tenantID, email)
		return row2.Scan(&adminBindings)
	}); err != nil {
		t.Fatalf("query users: %v", err)
	}
	if userCount != 1 {
		t.Errorf("user count(tenant=%s, email=%s)=%d, want 1", tenantID, email, userCount)
	}
	if adminBindings < 1 {
		t.Errorf("admin role binding count=%d, want >=1 (tenant.Create는 admin role 자동 할당 보장)", adminBindings)
	}
}

// verifyIntakeRowAccepted는 customer_intakes 테이블의 row가 accepted + tenant_id 채워졌는지
// 직접 SELECT로 검증합니다.
//
// intake row는 *tenant 생성 전* 단계 글로벌 데이터 — Bootstrap Tx 사용 (TenantID 없이).
func verifyIntakeRowAccepted(t *testing.T, p *Platform, intakeID, expectedTenantID string) {
	t.Helper()
	var (
		status   string
		tenantID *string
	)
	if err := p.Storage.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		row := tx.QueryRow(ctx, `SELECT status, tenant_id FROM customer_intakes WHERE id = ?`, intakeID)
		return row.Scan(&status, &tenantID)
	}); err != nil {
		t.Fatalf("query intake row: %v", err)
	}
	if status != "accepted" {
		t.Errorf("intake.status=%q, want accepted", status)
	}
	if tenantID == nil || *tenantID != expectedTenantID {
		got := "<nil>"
		if tenantID != nil {
			got = *tenantID
		}
		t.Errorf("intake.tenant_id=%q, want %q", got, expectedTenantID)
	}
}

// countNonSystemTenants는 tenants 테이블에서 system 외 row 수를 반환합니다.
//
// reject 전후 비교로 tenant 미생성 검증.
func countNonSystemTenants(t *testing.T, p *Platform) int {
	t.Helper()
	var n int
	if err := p.Storage.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		row := tx.QueryRow(ctx, `SELECT COUNT(*) FROM tenants WHERE id != 'system'`)
		return row.Scan(&n)
	}); err != nil {
		t.Fatalf("count tenants: %v", err)
	}
	return n
}
