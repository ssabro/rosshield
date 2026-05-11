package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ssabro/rosshield/internal/domain/tenant"
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
