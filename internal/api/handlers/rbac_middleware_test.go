package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/ssabro/rosshield/internal/domain/tenant"
	"github.com/ssabro/rosshield/internal/platform/authz"
)

// withClaims는 ctx에 AccessClaims를 직접 주입하는 테스트 헬퍼입니다.
// 실제 AuthMiddleware를 통과한 효과를 시뮬레이션.
func withClaims(ctx context.Context, claims tenant.AccessClaims) context.Context {
	return context.WithValue(ctx, claimsCtxKey, claims)
}

func newRoleTestRequest(ctx context.Context) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	return req.WithContext(ctx)
}

func TestRequireRoleNoAllowedRolesIs500(t *testing.T) {
	t.Parallel()
	h := &Handlers{}
	mw := h.RequireRole() // 빈 호출 — misconfiguration
	wrapped := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("downstream should not run")
	}))

	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, newRoleTestRequest(withClaims(context.Background(), tenant.AccessClaims{Subject: "us_x", Roles: []string{"admin"}})))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("code = %d, want 500 (misconfigured)", rec.Code)
	}
}

func TestRequireRoleNoClaimsIs401(t *testing.T) {
	t.Parallel()
	h := &Handlers{}
	mw := h.RequireRole("admin")
	wrapped := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("downstream should not run without claims")
	}))

	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, newRoleTestRequest(context.Background()))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("code = %d, want 401", rec.Code)
	}
}

func TestRequireRoleEmptySubjectIs401(t *testing.T) {
	t.Parallel()
	h := &Handlers{}
	mw := h.RequireRole("admin")
	wrapped := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("downstream should not run with empty subject")
	}))

	rec := httptest.NewRecorder()
	ctx := withClaims(context.Background(), tenant.AccessClaims{Subject: "", Roles: []string{"admin"}})
	wrapped.ServeHTTP(rec, newRoleTestRequest(ctx))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("code = %d, want 401", rec.Code)
	}
}

func TestRequireRoleAdminPasses(t *testing.T) {
	t.Parallel()
	h := &Handlers{}
	mw := h.RequireRole("admin")
	called := 0
	wrapped := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	ctx := withClaims(context.Background(), tenant.AccessClaims{Subject: "us_a", Roles: []string{"admin"}})
	wrapped.ServeHTTP(rec, newRoleTestRequest(ctx))
	if rec.Code != http.StatusOK {
		t.Errorf("code = %d, want 200", rec.Code)
	}
	if called != 1 {
		t.Errorf("downstream called = %d, want 1", called)
	}
}

func TestRequireRoleOperatorRejectedFor403(t *testing.T) {
	t.Parallel()
	h := &Handlers{}
	mw := h.RequireRole("admin")
	wrapped := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("downstream should not run for non-admin")
	}))

	rec := httptest.NewRecorder()
	ctx := withClaims(context.Background(), tenant.AccessClaims{Subject: "us_o", Roles: []string{"operator"}})
	wrapped.ServeHTTP(rec, newRoleTestRequest(ctx))
	if rec.Code != http.StatusForbidden {
		t.Errorf("code = %d, want 403", rec.Code)
	}
	// error body 검증
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Errorf("decode body: %v", err)
	}
	if body["error"] == "" {
		t.Errorf("error body missing 'error' field")
	}
}

func TestRequireRoleMultipleAllowedAuditorPasses(t *testing.T) {
	t.Parallel()
	h := &Handlers{}
	mw := h.RequireRole("admin", "auditor")
	called := 0
	wrapped := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	ctx := withClaims(context.Background(), tenant.AccessClaims{Subject: "us_aud", Roles: []string{"auditor"}})
	wrapped.ServeHTTP(rec, newRoleTestRequest(ctx))
	if rec.Code != http.StatusOK {
		t.Errorf("auditor code = %d, want 200", rec.Code)
	}
	if called != 1 {
		t.Errorf("downstream called = %d, want 1", called)
	}
}

func TestRequireRoleMultipleRolesUserAdminPasses(t *testing.T) {
	t.Parallel()
	h := &Handlers{}
	mw := h.RequireRole("admin")
	wrapped := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	// claims에 admin + operator 둘 다 — admin 매치로 통과해야.
	ctx := withClaims(context.Background(), tenant.AccessClaims{Subject: "us_x", Roles: []string{"operator", "admin"}})
	wrapped.ServeHTTP(rec, newRoleTestRequest(ctx))
	if rec.Code != http.StatusOK {
		t.Errorf("user with admin in role list: code = %d, want 200", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// 세분 RBAC Stage 3 — RequirePermission factory 테스트
//
// design doc §7 Stage 3 산출 — claims에서 Bindings를 추출하여 authz.Decide로 결정합니다.
// FleetID는 chi URL param "fleetID" 또는 (없으면) 빈 문자열 = tenant 글로벌 요청.
// ---------------------------------------------------------------------------

// newPermTestRequestWithFleet은 chi URL param fleetID를 가진 요청을 만듭니다.
//
// chi.URLParam이 path param을 추출하려면 RouteContext를 ctx에 미리 주입해야 합니다 —
// httptest는 chi router를 통과하지 않으므로 직접 chi.RouteContext에 키를 넣습니다.
func newPermTestRequestWithFleet(ctx context.Context, fleetID string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	if fleetID != "" {
		rc := chi.NewRouteContext()
		rc.URLParams.Add("fleetID", fleetID)
		ctx = context.WithValue(ctx, chi.RouteCtxKey, rc)
	}
	return req.WithContext(ctx)
}

// TestRequirePermission_NoClaimsIs401는 ctx에 claims가 없으면 401을 반환합니다.
func TestRequirePermission_NoClaimsIs401(t *testing.T) {
	t.Parallel()
	h := &Handlers{}
	mw := h.RequirePermission(authz.ResourceRobot, authz.ActionWrite)
	wrapped := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("downstream should not run without claims")
	}))

	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, newPermTestRequestWithFleet(context.Background(), ""))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("code = %d, want 401", rec.Code)
	}
}

// TestRequirePermission_InsufficientIs403는 binding에 권한이 없으면 403 + reason 응답.
//
// read-only role은 robot.write 권한이 없으므로 거부되어야 합니다.
func TestRequirePermission_InsufficientIs403(t *testing.T) {
	t.Parallel()
	h := &Handlers{}
	mw := h.RequirePermission(authz.ResourceRobot, authz.ActionWrite)
	wrapped := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("downstream should not run for read-only role on write")
	}))

	claims := tenant.AccessClaims{
		Subject: "us_ro",
		Bindings: []tenant.RoleBindingClaim{
			{Role: authz.RoleReadOnly, ScopeType: string(authz.ScopeTenant)},
		},
	}
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, newPermTestRequestWithFleet(withClaims(context.Background(), claims), ""))
	if rec.Code != http.StatusForbidden {
		t.Errorf("code = %d, want 403", rec.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "forbidden" {
		t.Errorf("error = %q, want 'forbidden'", body["error"])
	}
	if body["reason"] == "" {
		t.Errorf("reason missing — Decision.Reason 응답 누락")
	}
}

// TestRequirePermission_TenantScopeReadAllows는 tenant scope read-only binding으로
// robot.read 요청 → 200 통과를 검증합니다.
func TestRequirePermission_TenantScopeReadAllows(t *testing.T) {
	t.Parallel()
	h := &Handlers{}
	mw := h.RequirePermission(authz.ResourceRobot, authz.ActionRead)
	called := 0
	wrapped := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusOK)
	}))

	claims := tenant.AccessClaims{
		Subject: "us_ro",
		Bindings: []tenant.RoleBindingClaim{
			{Role: authz.RoleReadOnly, ScopeType: string(authz.ScopeTenant)},
		},
	}
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, newPermTestRequestWithFleet(withClaims(context.Background(), claims), ""))
	if rec.Code != http.StatusOK {
		t.Errorf("code = %d, want 200 (read-only.tenant should pass robot.read)", rec.Code)
	}
	if called != 1 {
		t.Errorf("downstream called = %d, want 1", called)
	}
}

// TestRequirePermission_FleetScopeWriteAllows는 fleet scope operator binding으로
// 같은 fleet의 robot.write 요청 → 200, 다른 fleet → 403을 검증합니다 (cross-fleet 격리).
func TestRequirePermission_FleetScopeWriteAllows(t *testing.T) {
	t.Parallel()
	h := &Handlers{}
	mw := h.RequirePermission(authz.ResourceRobot, authz.ActionWrite)
	called := 0
	wrapped := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusOK)
	}))

	// operator@fleet_A binding.
	claims := tenant.AccessClaims{
		Subject: "us_op",
		Bindings: []tenant.RoleBindingClaim{
			{Role: authz.RoleOperator, ScopeType: string(authz.ScopeFleet), ScopeID: "flt_a"},
		},
	}

	// 1) 같은 fleet — 200.
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, newPermTestRequestWithFleet(withClaims(context.Background(), claims), "flt_a"))
	if rec.Code != http.StatusOK {
		t.Errorf("same fleet: code = %d, want 200", rec.Code)
	}
	if called != 1 {
		t.Errorf("downstream called = %d, want 1", called)
	}

	// 2) 다른 fleet — 403 (cross-fleet 격리).
	rec2 := httptest.NewRecorder()
	wrapped.ServeHTTP(rec2, newPermTestRequestWithFleet(withClaims(context.Background(), claims), "flt_b"))
	if rec2.Code != http.StatusForbidden {
		t.Errorf("cross fleet: code = %d, want 403", rec2.Code)
	}
}

// TestRequirePermission_LegacyRolesFallback은 D-RBAC-7 호환 정책을 검증합니다.
//
// Bindings 없는 옛 토큰 (admin role만) → 모든 admin permission 통과.
// 본 fallback은 RequirePermission 내부에서 Roles → Bindings(tenant scope) 변환으로 구현.
func TestRequirePermission_LegacyRolesFallback(t *testing.T) {
	t.Parallel()
	h := &Handlers{}
	mw := h.RequirePermission(authz.ResourceTenantAdmin, authz.ActionAdmin)
	called := 0
	wrapped := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusOK)
	}))

	// 옛 토큰 시뮬레이션 — Bindings 없음, Roles만.
	claims := tenant.AccessClaims{
		Subject: "us_legacy",
		Roles:   []string{"admin"},
	}
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, newPermTestRequestWithFleet(withClaims(context.Background(), claims), ""))
	if rec.Code != http.StatusOK {
		t.Errorf("legacy admin token: code = %d, want 200 (D-RBAC-7 fallback)", rec.Code)
	}
	if called != 1 {
		t.Errorf("downstream called = %d, want 1", called)
	}
}
