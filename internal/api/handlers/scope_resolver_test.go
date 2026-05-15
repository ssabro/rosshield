package handlers

// scope_resolver_test.go — RBAC fleet 정밀화 Stage 2 단위 테스트.
//
// design doc `docs/design/notes/rbac-fleet-scope-precision-design.md` §7 Stage 2 산출 검증:
//   - RequirePermissionWithFleet factory + FleetScopeOpt 패턴
//   - body peek (10KB 한계) + body 복원 (handler 동일 body 수신)
//   - body peek 실패 시 빈 fleetID fallback (D-RBACEX-9)
//   - ScopeResolver mock — cross-resource lookup
//   - chi.URLParam fleetID 우선 (path > body > resolver)
//   - 권한 미달 403 + reason + MatchedBindings 일부 노출
//
// 본 stage 2는 middleware factory + ScopeResolver interface만 — handlers.go 7 endpoint
// 교체와 ScopeResolver 구체 구현은 Stage 3.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/ssabro/rosshield/internal/domain/tenant"
	"github.com/ssabro/rosshield/internal/platform/authz"
)

// mockScopeResolver는 ScopeResolver interface mock입니다.
//
// ResolveFleetFn 함수 필드를 주입하여 호출 인자(resourceType, resourceID)를 검증할 수 있습니다.
// CallCount는 호출 횟수 검증용.
type mockScopeResolver struct {
	ResolveFleetFn func(ctx context.Context, resourceType, resourceID string) (string, error)
	CallCount      int
	LastType       string
	LastID         string
}

func (m *mockScopeResolver) ResolveFleet(ctx context.Context, resourceType, resourceID string) (string, error) {
	m.CallCount++
	m.LastType = resourceType
	m.LastID = resourceID
	if m.ResolveFleetFn != nil {
		return m.ResolveFleetFn(ctx, resourceType, resourceID)
	}
	return "", nil
}

// newWithFleetReq는 chi RouteContext + body + path param을 채운 테스트 request를 만듭니다.
//
// pathFleetID가 비어있지 않으면 chi.URLParam("fleetID")로 추출 가능. body는 nil이면 빈 body.
func newWithFleetReq(ctx context.Context, method, body string, pathParams map[string]string) *http.Request {
	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, "/x", bodyReader)
	if len(pathParams) > 0 {
		rc := chi.NewRouteContext()
		for k, v := range pathParams {
			rc.URLParams.Add(k, v)
		}
		ctx = context.WithValue(ctx, chi.RouteCtxKey, rc)
	}
	return req.WithContext(ctx)
}

// TestRequirePermissionWithFleet_NoClaimsIs401 — claims 부재 시 401.
func TestRequirePermissionWithFleet_NoClaimsIs401(t *testing.T) {
	t.Parallel()
	h := &Handlers{}
	mw := h.RequirePermissionWithFleet(authz.ResourceRobot, authz.ActionWrite)
	wrapped := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("downstream should not run without claims")
	}))

	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, newWithFleetReq(context.Background(), http.MethodPost, "", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("code = %d, want 401", rec.Code)
	}
}

// TestRequirePermissionWithFleet_PathParamPriority — chi.URLParam("fleetID") 또는
// "fleetId" 가 존재하면 body·resolver를 무시하고 path를 사용합니다.
func TestRequirePermissionWithFleet_PathParamPriority(t *testing.T) {
	t.Parallel()
	resolver := &mockScopeResolver{}
	h := &Handlers{deps: Deps{ScopeResolver: resolver}}
	mw := h.RequirePermissionWithFleet(
		authz.ResourceRobot,
		authz.ActionWrite,
		WithFleetFromBody("fleetId"),
		WithFleetFromResource("robot", "robotId"),
	)
	called := 0
	wrapped := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusOK)
	}))

	claims := tenant.AccessClaims{
		Subject: "us_op",
		Bindings: []tenant.RoleBindingClaim{
			{Role: authz.RoleOperator, ScopeType: string(authz.ScopeFleet), ScopeID: "flt_a"},
		},
	}
	rec := httptest.NewRecorder()
	// path fleetID="flt_a" + body fleetId="flt_b" → path 우선이므로 ALLOW.
	req := newWithFleetReq(
		withClaims(context.Background(), claims),
		http.MethodPost,
		`{"fleetId":"flt_b"}`,
		map[string]string{"fleetID": "flt_a"},
	)
	wrapped.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("path priority: code = %d, want 200", rec.Code)
	}
	if called != 1 {
		t.Errorf("downstream called = %d, want 1", called)
	}
	if resolver.CallCount != 0 {
		t.Errorf("resolver called %d times — path 우선이면 호출 안 함", resolver.CallCount)
	}
}

// TestRequirePermissionWithFleet_BodyPeekExtractsFleetID — body JSON에서 fleetId 추출 후
// PDP 평가에 사용.
func TestRequirePermissionWithFleet_BodyPeekExtractsFleetID(t *testing.T) {
	t.Parallel()
	h := &Handlers{}
	mw := h.RequirePermissionWithFleet(
		authz.ResourceRobot,
		authz.ActionWrite,
		WithFleetFromBody("fleetId"),
	)
	called := 0
	wrapped := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusOK)
	}))

	claims := tenant.AccessClaims{
		Subject: "us_op",
		Bindings: []tenant.RoleBindingClaim{
			{Role: authz.RoleOperator, ScopeType: string(authz.ScopeFleet), ScopeID: "flt_a"},
		},
	}
	rec := httptest.NewRecorder()
	req := newWithFleetReq(
		withClaims(context.Background(), claims),
		http.MethodPost,
		`{"fleetId":"flt_a","name":"r1"}`,
		nil,
	)
	wrapped.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("body peek allow: code = %d, want 200", rec.Code)
	}
	if called != 1 {
		t.Errorf("downstream called = %d, want 1", called)
	}
}

// TestRequirePermissionWithFleet_BodyReadableAfterPeek — middleware가 body를 peek한 후
// 핸들러가 동일 body를 다시 파싱 가능.
func TestRequirePermissionWithFleet_BodyReadableAfterPeek(t *testing.T) {
	t.Parallel()
	h := &Handlers{}
	mw := h.RequirePermissionWithFleet(
		authz.ResourceRobot,
		authz.ActionWrite,
		WithFleetFromBody("fleetId"),
	)

	original := `{"fleetId":"flt_a","name":"r1","extra":42}`
	var handlerBody []byte
	wrapped := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("handler read body: %v", err)
		}
		handlerBody = buf
		w.WriteHeader(http.StatusOK)
	}))

	claims := tenant.AccessClaims{
		Subject: "us_op",
		Bindings: []tenant.RoleBindingClaim{
			{Role: authz.RoleOperator, ScopeType: string(authz.ScopeFleet), ScopeID: "flt_a"},
		},
	}
	rec := httptest.NewRecorder()
	req := newWithFleetReq(
		withClaims(context.Background(), claims),
		http.MethodPost,
		original,
		nil,
	)
	wrapped.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	if string(handlerBody) != original {
		t.Errorf("handler body = %q, want %q (body 복원 실패)", string(handlerBody), original)
	}
}

// TestRequirePermissionWithFleet_BodyPeekFailureFallsBackToEmpty — body 파싱 실패 시
// 빈 fleetID로 PDP 평가 (D-RBACEX-9 권장 default B).
//
// fleet scope binding만 가진 사용자는 빈 fleetID 상태에서 fleet 매칭 실패 → 403.
func TestRequirePermissionWithFleet_BodyPeekFailureFallsBackToEmpty(t *testing.T) {
	t.Parallel()
	h := &Handlers{}
	mw := h.RequirePermissionWithFleet(
		authz.ResourceRobot,
		authz.ActionWrite,
		WithFleetFromBody("fleetId"),
	)
	wrapped := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("downstream should not run — fleet binding 미일치")
	}))

	claims := tenant.AccessClaims{
		Subject: "us_op",
		Bindings: []tenant.RoleBindingClaim{
			{Role: authz.RoleOperator, ScopeType: string(authz.ScopeFleet), ScopeID: "flt_a"},
		},
	}
	rec := httptest.NewRecorder()
	// 잘못된 JSON — body peek 실패 → fleetID="" → operator@flt_a binding 매칭 실패.
	req := newWithFleetReq(
		withClaims(context.Background(), claims),
		http.MethodPost,
		`not-json{`,
		nil,
	)
	wrapped.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("body peek failure fallback empty fleetID: code = %d, want 403", rec.Code)
	}
}

// TestRequirePermissionWithFleet_BodyPeekFailurePreservesBody — body peek 실패해도
// 핸들러는 원본 body를 받아야 함 (복원).
//
// 단, 이 case는 PDP가 통과해야 핸들러까지 진입 — admin tenant scope로 통과하도록 합니다.
func TestRequirePermissionWithFleet_BodyPeekFailurePreservesBody(t *testing.T) {
	t.Parallel()
	h := &Handlers{}
	mw := h.RequirePermissionWithFleet(
		authz.ResourceRobot,
		authz.ActionWrite,
		WithFleetFromBody("fleetId"),
	)

	original := `not-json{`
	var handlerBody []byte
	wrapped := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("handler read body: %v", err)
		}
		handlerBody = buf
		w.WriteHeader(http.StatusOK)
	}))

	// admin tenant scope — fleet 무관 통과.
	claims := tenant.AccessClaims{
		Subject: "us_admin",
		Bindings: []tenant.RoleBindingClaim{
			{Role: authz.RoleAdmin, ScopeType: string(authz.ScopeTenant)},
		},
	}
	rec := httptest.NewRecorder()
	req := newWithFleetReq(
		withClaims(context.Background(), claims),
		http.MethodPost,
		original,
		nil,
	)
	wrapped.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200 (admin tenant scope)", rec.Code)
	}
	if string(handlerBody) != original {
		t.Errorf("handler body = %q, want %q (피크 실패 시도 body 복원)", string(handlerBody), original)
	}
}

// TestRequirePermissionWithFleet_BodyPeekRespects10KBLimit — 10KB 초과 body는 peek 거부 +
// 빈 fleetID fallback (D-RBACEX-9 권장 default 일관).
//
// 핸들러는 원본 body를 그대로 수신해야 함 (peek은 실패하더라도 파괴 없음).
func TestRequirePermissionWithFleet_BodyPeekRespects10KBLimit(t *testing.T) {
	t.Parallel()
	h := &Handlers{}
	mw := h.RequirePermissionWithFleet(
		authz.ResourceRobot,
		authz.ActionWrite,
		WithFleetFromBody("fleetId"),
	)

	// 11KB body — 10KB 한계 초과. JSON 형식이지만 peek 거부 예정.
	largePayload := `{"fleetId":"flt_a","filler":"` + strings.Repeat("X", 11*1024) + `"}`

	var handlerBody []byte
	wrapped := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("handler read body: %v", err)
		}
		handlerBody = buf
		w.WriteHeader(http.StatusOK)
	}))

	// admin tenant scope — peek 실패해도 통과.
	claims := tenant.AccessClaims{
		Subject: "us_admin",
		Bindings: []tenant.RoleBindingClaim{
			{Role: authz.RoleAdmin, ScopeType: string(authz.ScopeTenant)},
		},
	}
	rec := httptest.NewRecorder()
	req := newWithFleetReq(
		withClaims(context.Background(), claims),
		http.MethodPost,
		largePayload,
		nil,
	)
	wrapped.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	if string(handlerBody) != largePayload {
		t.Errorf("handler body 길이 = %d, want %d (10KB 초과 body 복원)", len(handlerBody), len(largePayload))
	}
}

// TestRequirePermissionWithFleet_ScopeResolverInvoked — ScopeResolver를 통한
// cross-resource lookup. path에 robotId만 있고 ScopeResolver가 fleet_id 반환.
func TestRequirePermissionWithFleet_ScopeResolverInvoked(t *testing.T) {
	t.Parallel()
	resolver := &mockScopeResolver{
		ResolveFleetFn: func(ctx context.Context, resourceType, resourceID string) (string, error) {
			if resourceType != "robot" {
				t.Errorf("resourceType = %q, want 'robot'", resourceType)
			}
			if resourceID != "rbt_x" {
				t.Errorf("resourceID = %q, want 'rbt_x'", resourceID)
			}
			return "flt_a", nil
		},
	}
	h := &Handlers{deps: Deps{ScopeResolver: resolver}}
	mw := h.RequirePermissionWithFleet(
		authz.ResourceRobot,
		authz.ActionWrite,
		WithFleetFromResource("robot", "robotId"),
	)
	called := 0
	wrapped := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusOK)
	}))

	claims := tenant.AccessClaims{
		Subject: "us_op",
		Bindings: []tenant.RoleBindingClaim{
			{Role: authz.RoleOperator, ScopeType: string(authz.ScopeFleet), ScopeID: "flt_a"},
		},
	}
	rec := httptest.NewRecorder()
	// path에 robotId만 — resolver가 robotId → fleetID="flt_a" 반환.
	req := newWithFleetReq(
		withClaims(context.Background(), claims),
		http.MethodDelete,
		"",
		map[string]string{"robotId": "rbt_x"},
	)
	wrapped.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("resolver allow: code = %d, want 200", rec.Code)
	}
	if called != 1 {
		t.Errorf("downstream called = %d, want 1", called)
	}
	if resolver.CallCount != 1 {
		t.Errorf("resolver call count = %d, want 1", resolver.CallCount)
	}
}

// TestRequirePermissionWithFleet_ScopeResolverError — resolver 실패 시 빈 fleetID
// fallback (D-RBACEX-9 정책 일관). fleet binding만 가진 사용자는 403.
func TestRequirePermissionWithFleet_ScopeResolverError(t *testing.T) {
	t.Parallel()
	resolver := &mockScopeResolver{
		ResolveFleetFn: func(ctx context.Context, resourceType, resourceID string) (string, error) {
			return "", errors.New("not found")
		},
	}
	h := &Handlers{deps: Deps{ScopeResolver: resolver}}
	mw := h.RequirePermissionWithFleet(
		authz.ResourceRobot,
		authz.ActionWrite,
		WithFleetFromResource("robot", "robotId"),
	)
	wrapped := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("downstream should not run on resolver error fallback")
	}))

	claims := tenant.AccessClaims{
		Subject: "us_op",
		Bindings: []tenant.RoleBindingClaim{
			{Role: authz.RoleOperator, ScopeType: string(authz.ScopeFleet), ScopeID: "flt_a"},
		},
	}
	rec := httptest.NewRecorder()
	req := newWithFleetReq(
		withClaims(context.Background(), claims),
		http.MethodDelete,
		"",
		map[string]string{"robotId": "rbt_x"},
	)
	wrapped.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("resolver error fallback empty: code = %d, want 403", rec.Code)
	}
}

// TestRequirePermissionWithFleet_NilScopeResolverAllowed — ScopeResolver nil 허용.
// path/body만 사용하고 resolver opts 미설정이면 호출 0.
func TestRequirePermissionWithFleet_NilScopeResolverAllowed(t *testing.T) {
	t.Parallel()
	h := &Handlers{deps: Deps{ScopeResolver: nil}}
	mw := h.RequirePermissionWithFleet(
		authz.ResourceRobot,
		authz.ActionWrite,
		WithFleetFromBody("fleetId"),
	)
	called := 0
	wrapped := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusOK)
	}))

	claims := tenant.AccessClaims{
		Subject: "us_op",
		Bindings: []tenant.RoleBindingClaim{
			{Role: authz.RoleOperator, ScopeType: string(authz.ScopeFleet), ScopeID: "flt_a"},
		},
	}
	rec := httptest.NewRecorder()
	req := newWithFleetReq(
		withClaims(context.Background(), claims),
		http.MethodPost,
		`{"fleetId":"flt_a"}`,
		nil,
	)
	wrapped.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("nil resolver + body allow: code = %d, want 200", rec.Code)
	}
	if called != 1 {
		t.Errorf("downstream called = %d, want 1", called)
	}
}

// TestRequirePermissionWithFleet_NilResolverWithResolverOpt403 — ScopeResolver nil인데
// WithFleetFromResource 사용 시 빈 fleetID fallback (정책 일관) → fleet binding만 가진
// 사용자는 403. fail-closed.
func TestRequirePermissionWithFleet_NilResolverWithResolverOpt403(t *testing.T) {
	t.Parallel()
	h := &Handlers{deps: Deps{ScopeResolver: nil}}
	mw := h.RequirePermissionWithFleet(
		authz.ResourceRobot,
		authz.ActionWrite,
		WithFleetFromResource("robot", "robotId"),
	)
	wrapped := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("downstream should not run when resolver nil + fleet binding only")
	}))

	claims := tenant.AccessClaims{
		Subject: "us_op",
		Bindings: []tenant.RoleBindingClaim{
			{Role: authz.RoleOperator, ScopeType: string(authz.ScopeFleet), ScopeID: "flt_a"},
		},
	}
	rec := httptest.NewRecorder()
	req := newWithFleetReq(
		withClaims(context.Background(), claims),
		http.MethodDelete,
		"",
		map[string]string{"robotId": "rbt_x"},
	)
	wrapped.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("nil resolver fallback: code = %d, want 403", rec.Code)
	}
}

// TestRequirePermissionWithFleet_DeniesExposesMatchedBindings — 권한 미달 403 응답에
// MatchedBindings 정보 일부 노출 (explainability — D-RBACEX-4).
//
// MatchedBindings는 ALLOW 시에만 채워지므로 DENY 시에는 사용자가 가진 bindings 전체와
// 평가 reason을 노출. 본 stage 2는 reason 문자열에 fleet 매칭 정보 포함.
func TestRequirePermissionWithFleet_DeniesExposesMatchedBindings(t *testing.T) {
	t.Parallel()
	h := &Handlers{}
	mw := h.RequirePermissionWithFleet(
		authz.ResourceRobot,
		authz.ActionWrite,
		WithFleetFromBody("fleetId"),
	)
	wrapped := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("downstream should not run on cross-fleet deny")
	}))

	claims := tenant.AccessClaims{
		Subject: "us_op",
		Bindings: []tenant.RoleBindingClaim{
			{Role: authz.RoleOperator, ScopeType: string(authz.ScopeFleet), ScopeID: "flt_a"},
		},
	}
	rec := httptest.NewRecorder()
	// body fleetId=flt_b 인데 binding은 flt_a만 — DENY.
	req := newWithFleetReq(
		withClaims(context.Background(), claims),
		http.MethodPost,
		`{"fleetId":"flt_b"}`,
		nil,
	)
	wrapped.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("code = %d, want 403", rec.Code)
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "forbidden" {
		t.Errorf("error = %v, want 'forbidden'", body["error"])
	}
	reason, _ := body["reason"].(string)
	if reason == "" {
		t.Errorf("reason missing — Decision.Reason 응답 누락")
	}
	if !strings.Contains(reason, "fleet=") {
		t.Errorf("reason = %q, want fleet= 포함 (cross-fleet 진단)", reason)
	}
}

// TestRequirePermissionWithFleet_FallbackToFleetIDFromRequest — opts 없이 호출하면
// 기존 fleetIDFromRequest fallback (chi.URLParam fleetID|fleetId) 동작.
//
// 본 fallback으로 RequirePermission 호환 — Stage 3에서 7 endpoint만 opts 추가 교체.
func TestRequirePermissionWithFleet_FallbackToFleetIDFromRequest(t *testing.T) {
	t.Parallel()
	h := &Handlers{}
	mw := h.RequirePermissionWithFleet(authz.ResourceRobot, authz.ActionWrite)
	called := 0
	wrapped := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusOK)
	}))

	claims := tenant.AccessClaims{
		Subject: "us_op",
		Bindings: []tenant.RoleBindingClaim{
			{Role: authz.RoleOperator, ScopeType: string(authz.ScopeFleet), ScopeID: "flt_a"},
		},
	}
	rec := httptest.NewRecorder()
	// opts 없음 → 기존 fleetIDFromRequest로 path에서만 추출.
	req := newWithFleetReq(
		withClaims(context.Background(), claims),
		http.MethodPatch,
		"",
		map[string]string{"fleetId": "flt_a"},
	)
	wrapped.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("opts 없을 때 fallback: code = %d, want 200", rec.Code)
	}
	if called != 1 {
		t.Errorf("downstream called = %d, want 1", called)
	}
}

// TestRequirePermissionWithFleet_BodyPeekEmptyBodyOK — body가 nil/빈 GET·DELETE에서
// body peek opt가 있어도 panic 없이 빈 fleetID로 진행.
func TestRequirePermissionWithFleet_BodyPeekEmptyBodyOK(t *testing.T) {
	t.Parallel()
	h := &Handlers{}
	mw := h.RequirePermissionWithFleet(
		authz.ResourceRobot,
		authz.ActionWrite,
		WithFleetFromBody("fleetId"),
	)
	wrapped := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("downstream should not run — fleet binding 미일치")
	}))

	claims := tenant.AccessClaims{
		Subject: "us_op",
		Bindings: []tenant.RoleBindingClaim{
			{Role: authz.RoleOperator, ScopeType: string(authz.ScopeFleet), ScopeID: "flt_a"},
		},
	}
	rec := httptest.NewRecorder()
	// body 없음 — peek은 즉시 빈 fleetID 반환.
	req := newWithFleetReq(
		withClaims(context.Background(), claims),
		http.MethodPost,
		"",
		nil,
	)
	wrapped.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("empty body: code = %d, want 403", rec.Code)
	}
}

// TestRequirePermissionWithFleet_AdminTenantPassesAcrossFleets — admin tenant scope는
// path/body/resolver에서 어떤 fleetID가 추출되든 통과.
func TestRequirePermissionWithFleet_AdminTenantPassesAcrossFleets(t *testing.T) {
	t.Parallel()
	resolver := &mockScopeResolver{
		ResolveFleetFn: func(ctx context.Context, resourceType, resourceID string) (string, error) {
			return "flt_xyz", nil
		},
	}
	h := &Handlers{deps: Deps{ScopeResolver: resolver}}
	mw := h.RequirePermissionWithFleet(
		authz.ResourceRobot,
		authz.ActionWrite,
		WithFleetFromResource("robot", "robotId"),
	)
	called := 0
	wrapped := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusOK)
	}))

	claims := tenant.AccessClaims{
		Subject: "us_admin",
		Bindings: []tenant.RoleBindingClaim{
			{Role: authz.RoleAdmin, ScopeType: string(authz.ScopeTenant)},
		},
	}
	rec := httptest.NewRecorder()
	req := newWithFleetReq(
		withClaims(context.Background(), claims),
		http.MethodDelete,
		"",
		map[string]string{"robotId": "rbt_x"},
	)
	wrapped.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("admin tenant scope: code = %d, want 200", rec.Code)
	}
	if called != 1 {
		t.Errorf("downstream called = %d, want 1", called)
	}
}

// TestRequirePermissionWithFleet_BodyResolverFallback — body opt가 있지만 body에
// fleetId 키 부재 시 resolver fallback이 호출되어야 함 (정책: 첫 non-empty 우선).
func TestRequirePermissionWithFleet_BodyResolverFallback(t *testing.T) {
	t.Parallel()
	resolver := &mockScopeResolver{
		ResolveFleetFn: func(ctx context.Context, resourceType, resourceID string) (string, error) {
			return "flt_a", nil
		},
	}
	h := &Handlers{deps: Deps{ScopeResolver: resolver}}
	mw := h.RequirePermissionWithFleet(
		authz.ResourceRobot,
		authz.ActionWrite,
		WithFleetFromBody("fleetId"),
		WithFleetFromResource("robot", "robotId"),
	)
	called := 0
	wrapped := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusOK)
	}))

	claims := tenant.AccessClaims{
		Subject: "us_op",
		Bindings: []tenant.RoleBindingClaim{
			{Role: authz.RoleOperator, ScopeType: string(authz.ScopeFleet), ScopeID: "flt_a"},
		},
	}
	rec := httptest.NewRecorder()
	// body에 fleetId 없고 path에 robotId만 — body opt skip → resolver opt 호출.
	req := newWithFleetReq(
		withClaims(context.Background(), claims),
		http.MethodDelete,
		`{"name":"r1"}`,
		map[string]string{"robotId": "rbt_x"},
	)
	wrapped.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("body→resolver fallback: code = %d, want 200", rec.Code)
	}
	if called != 1 {
		t.Errorf("downstream called = %d, want 1", called)
	}
	if resolver.CallCount != 1 {
		t.Errorf("resolver call count = %d, want 1", resolver.CallCount)
	}
}

// peekFleetIDFromBody 단위 검증은 scope_resolver_peek_test.go로 분리됨 —
// invariant(body 복원·10KB 한계·nil body·재사용)는 거기서.
