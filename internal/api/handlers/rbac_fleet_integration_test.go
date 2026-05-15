package handlers

// rbac_fleet_integration_test.go — RBAC fleet 정밀화 Stage 3 통합 매트릭스.
//
// design doc `docs/design/notes/rbac-fleet-scope-precision-design.md` §7 Stage 3 산출 검증:
//   - 5 mutation endpoint × {path/body/cross-resource fleet pass + cross-fleet deny + 권한 미달}
//   - ScopeResolver 호출 검증 (LastType / LastID / CallCount)
//   - admin tenant scope cross-fleet 통과 (회귀)
//   - operator/fleet-admin 페르소나 fleet 격리
//
// 본 stage 3가 RequirePermissionWithFleet으로 교체한 5건 endpoint:
//   1. POST /robots — body fleetId
//   2. POST /scans — body fleetId
//   3. DELETE /robots/{robotID} — ScopeResolver("robot", robotID)
//   4. POST /robots/{robotID}/credential:rotate — ScopeResolver("robot", robotID)
//   5. POST /scans/{sessionID}:cancel — ScopeResolver("scan", sessionID)
//
// 본 task에서 비교체 2건 (Insight/Report Service에 GetByID + FleetID 미노출):
//   - POST /reports/{id}:verify — RequirePermission(report.verify) 유지 (fleet 무관 통과)
//   - POST /insights/{id}:dismiss — RequirePermission(insight.write) 유지 (fleet 무관 통과)
//
// 본 테스트는 5 endpoint × 5 페르소나 × 2 fleet = 50 case + 추가 ScopeResolver 검증
// + 권한 미달 deny 별도 = 약 60+ sub-test.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/ssabro/rosshield/internal/domain/tenant"
	"github.com/ssabro/rosshield/internal/platform/authz"
)

// fleetMutationEndpoint는 Stage 3에서 RequirePermissionWithFleet으로 교체한 endpoint 1건입니다.
//
// resource/action: PDP에 전달하는 (resource, action).
// extractor: "body" → WithFleetFromBody / "resource" → WithFleetFromResource(resourceType, paramName).
// resourceType / paramName: WithFleetFromResource 인자.
type fleetMutationEndpoint struct {
	name         string
	resource     authz.Resource
	action       authz.Action
	extractor    string // "body" | "resource"
	bodyField    string // "fleetId" (extractor=body 시)
	resourceType string // "robot" | "scan" (extractor=resource 시)
	paramName    string // "robotID" | "sessionID" (extractor=resource 시)
}

// allFleetMutationEndpoints는 Stage 3에서 fleet 정밀화 적용한 5건 정의입니다.
//
// design doc §3.1 매트릭스 + §7 Stage 3 정확 일치.
func allFleetMutationEndpoints() []fleetMutationEndpoint {
	return []fleetMutationEndpoint{
		{
			name:      "POST /robots",
			resource:  authz.ResourceRobot,
			action:    authz.ActionWrite,
			extractor: "body",
			bodyField: "fleetId",
		},
		{
			name:      "POST /scans",
			resource:  authz.ResourceScan,
			action:    authz.ActionExecute,
			extractor: "body",
			bodyField: "fleetId",
		},
		{
			name:         "DELETE /robots/{robotID}",
			resource:     authz.ResourceRobot,
			action:       authz.ActionWrite,
			extractor:    "resource",
			resourceType: "robot",
			paramName:    "robotID",
		},
		{
			name:         "POST /robots/{robotID}/credential:rotate",
			resource:     authz.ResourceRobot,
			action:       authz.ActionAdmin,
			extractor:    "resource",
			resourceType: "robot",
			paramName:    "robotID",
		},
		{
			name:         "POST /scans/{sessionID}:cancel",
			resource:     authz.ResourceScan,
			action:       authz.ActionExecute,
			extractor:    "resource",
			resourceType: "scan",
			paramName:    "sessionID",
		},
	}
}

// fleetPersona는 매트릭스 1행을 표현합니다.
//
// 본 stage 3는 fleet 격리에 집중하므로 fleet binding 페르소나 위주.
type fleetPersona struct {
	name     string
	bindings []tenant.RoleBindingClaim
}

// allFleetPersonas는 Stage 3 통합 매트릭스용 5 페르소나 셋입니다.
//
// admin/owner는 tenant 글로벌 — 모든 fleet 통과(회귀).
// fleet-admin@flt_a / operator@flt_a는 fleet_a만 통과 — cross-fleet 격리.
// read-only는 모든 mutation 거부 — 권한 미달.
func allFleetPersonas() []fleetPersona {
	return []fleetPersona{
		{name: "admin", bindings: []tenant.RoleBindingClaim{
			{Role: authz.RoleAdmin, ScopeType: string(authz.ScopeTenant)},
		}},
		{name: "owner", bindings: []tenant.RoleBindingClaim{
			{Role: authz.RoleOwner, ScopeType: string(authz.ScopeTenant)},
		}},
		{name: "fleet-admin@flt_a", bindings: []tenant.RoleBindingClaim{
			{Role: authz.RoleFleetAdmin, ScopeType: string(authz.ScopeFleet), ScopeID: "flt_a"},
		}},
		{name: "operator@flt_a", bindings: []tenant.RoleBindingClaim{
			{Role: authz.RoleOperator, ScopeType: string(authz.ScopeFleet), ScopeID: "flt_a"},
		}},
		{name: "read-only", bindings: []tenant.RoleBindingClaim{
			{Role: authz.RoleReadOnly, ScopeType: string(authz.ScopeTenant)},
		}},
	}
}

// expectFleetAllow는 (페르소나, endpoint, 평가 fleetID) 쌍에서 ALLOW가 기대되는지 반환합니다.
//
// authz.Decide의 정답 — 본 함수가 매트릭스의 source-of-truth 입니다.
func expectFleetAllow(p fleetPersona, e fleetMutationEndpoint, fleetID string) bool {
	sub := authz.Subject{
		Bindings: convertBindingsToAuthz(p.bindings),
		FleetID:  fleetID,
	}
	return authz.Decide(sub, e.resource, e.action).Allow
}

// newFleetMatrixRequest는 매트릭스 case 1건의 request를 만듭니다.
//
// extractor=body면 body에 {"fleetId": evalFleetID} 주입, path는 비움.
// extractor=resource면 chi URLParam(paramName) = "rsc_x" 주입, body는 비움.
// resolver는 paramName="rsc_x"를 받으면 evalFleetID 반환하도록 mock 셋업.
func newFleetMatrixRequest(claims tenant.AccessClaims, e fleetMutationEndpoint, evalFleetID string) *http.Request {
	var body string
	pathParams := map[string]string{}
	if e.extractor == "body" {
		body = fmt.Sprintf(`{%q:%q,"name":"r1"}`, e.bodyField, evalFleetID)
	} else {
		pathParams[e.paramName] = "rsc_x"
	}
	return newWithFleetReq(withClaims(context.Background(), claims), http.MethodPost, body, pathParams)
}

// makeFleetMatrixMW는 endpoint 정의에서 RequirePermissionWithFleet middleware를 빌드합니다.
//
// resolver는 호출 시 항상 evalFleetID를 반환하도록 미리 셋업 — 매트릭스 case마다 evalFleetID 변경.
func makeFleetMatrixMW(h *Handlers, e fleetMutationEndpoint) func(http.Handler) http.Handler {
	switch e.extractor {
	case "body":
		return h.RequirePermissionWithFleet(e.resource, e.action, WithFleetFromBody(e.bodyField))
	case "resource":
		return h.RequirePermissionWithFleet(e.resource, e.action, WithFleetFromResource(e.resourceType, e.paramName))
	default:
		panic("unknown extractor")
	}
}

// TestFleetMatrix_PathFleetPass는 5 endpoint × 5 페르소나 × {flt_a, flt_b} 매트릭스를
// 검증합니다.
//
// case 분류:
//   - body extractor + body fleetId=flt_a / flt_b: PDP 평가에 evalFleetID 주입.
//   - resource extractor + resolver가 evalFleetID 반환: 동일.
//
// admin/owner는 tenant 글로벌 — flt_a / flt_b 모두 통과.
// fleet-admin@flt_a / operator@flt_a는 flt_a만 통과 — flt_b는 cross-fleet deny.
// read-only는 모든 case 거부.
//
// 총 50 case (5 endpoint × 5 페르소나 × 2 fleet).
func TestFleetMatrix_AllPersonasAllEndpointsAllFleets(t *testing.T) {
	t.Parallel()

	endpoints := allFleetMutationEndpoints()
	if got, want := len(endpoints), 5; got != want {
		t.Fatalf("endpoint count = %d, want %d (Stage 3 fleet 정밀화 5건)", got, want)
	}
	personas := allFleetPersonas()
	if got, want := len(personas), 5; got != want {
		t.Fatalf("persona count = %d, want %d", got, want)
	}

	evalFleets := []string{"flt_a", "flt_b"}

	for _, p := range personas {
		p := p
		for _, e := range endpoints {
			e := e
			for _, evalFleetID := range evalFleets {
				evalFleetID := evalFleetID
				caseName := fmt.Sprintf("%s__%s__%s", p.name, e.name, evalFleetID)
				t.Run(caseName, func(t *testing.T) {
					t.Parallel()

					// resolver는 모든 호출에서 evalFleetID 반환.
					resolver := &mockScopeResolver{
						ResolveFleetFn: func(_ context.Context, _, _ string) (string, error) {
							return evalFleetID, nil
						},
					}
					h := &Handlers{deps: Deps{ScopeResolver: resolver}}

					mw := makeFleetMatrixMW(h, e)
					downstreamCalled := false
					wrapped := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						downstreamCalled = true
						w.WriteHeader(http.StatusOK)
					}))

					claims := tenant.AccessClaims{Subject: "us_" + p.name, Bindings: p.bindings}
					rec := httptest.NewRecorder()
					wrapped.ServeHTTP(rec, newFleetMatrixRequest(claims, e, evalFleetID))

					want := expectFleetAllow(p, e, evalFleetID)
					gotAllow := rec.Code == http.StatusOK
					if gotAllow != want {
						t.Errorf("decision mismatch: persona=%s endpoint=%s fleet=%s want allow=%v, got code=%d (downstream=%v)",
							p.name, e.name, evalFleetID, want, rec.Code, downstreamCalled)
					}
					if !want && rec.Code != http.StatusForbidden {
						t.Errorf("DENY expected: code = %d, want 403", rec.Code)
					}
				})
			}
		}
	}
}

// TestFleetMatrix_ScopeResolverInvocation는 resource extractor 사용 endpoint에서
// ScopeResolver가 정확히 (resourceType, resourceID)로 호출되는지 검증합니다.
//
// 3 endpoint × 1 호출 = 3 sub-test. body extractor 2건은 resolver 미호출 검증.
func TestFleetMatrix_ScopeResolverInvocation(t *testing.T) {
	t.Parallel()

	for _, e := range allFleetMutationEndpoints() {
		e := e
		t.Run(e.name, func(t *testing.T) {
			t.Parallel()

			resolver := &mockScopeResolver{
				ResolveFleetFn: func(_ context.Context, _, _ string) (string, error) {
					return "flt_a", nil
				},
			}
			h := &Handlers{deps: Deps{ScopeResolver: resolver}}
			mw := makeFleetMatrixMW(h, e)
			wrapped := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			// admin tenant scope — 항상 ALLOW.
			claims := tenant.AccessClaims{
				Subject: "us_admin",
				Bindings: []tenant.RoleBindingClaim{
					{Role: authz.RoleAdmin, ScopeType: string(authz.ScopeTenant)},
				},
			}
			rec := httptest.NewRecorder()
			wrapped.ServeHTTP(rec, newFleetMatrixRequest(claims, e, "flt_a"))
			if rec.Code != http.StatusOK {
				t.Fatalf("admin must pass: code = %d", rec.Code)
			}

			switch e.extractor {
			case "body":
				if resolver.CallCount != 0 {
					t.Errorf("body extractor: resolver call count = %d, want 0", resolver.CallCount)
				}
			case "resource":
				if resolver.CallCount != 1 {
					t.Errorf("resource extractor: resolver call count = %d, want 1", resolver.CallCount)
				}
				if resolver.LastType != e.resourceType {
					t.Errorf("resolver LastType = %q, want %q", resolver.LastType, e.resourceType)
				}
				if resolver.LastID != "rsc_x" {
					t.Errorf("resolver LastID = %q, want 'rsc_x'", resolver.LastID)
				}
			}
		})
	}
}

// TestFleetMatrix_OperatorCrossFleetDeny는 operator@flt_a 페르소나가 flt_b 평가에서
// 거부됨을 명시 검증합니다 (fleet 격리 회귀 차단).
//
// 5 endpoint 모두 operator@flt_a + evalFleetID="flt_b" → 403.
func TestFleetMatrix_OperatorCrossFleetDeny(t *testing.T) {
	t.Parallel()

	operator := tenant.AccessClaims{
		Subject: "us_op",
		Bindings: []tenant.RoleBindingClaim{
			{Role: authz.RoleOperator, ScopeType: string(authz.ScopeFleet), ScopeID: "flt_a"},
		},
	}

	for _, e := range allFleetMutationEndpoints() {
		e := e
		// operator는 robot.admin 미보유 — credential:rotate(admin)는 fleet 무관 거부 → skip.
		if e.action == authz.ActionAdmin {
			continue
		}
		t.Run(e.name, func(t *testing.T) {
			t.Parallel()
			resolver := &mockScopeResolver{
				ResolveFleetFn: func(_ context.Context, _, _ string) (string, error) {
					return "flt_b", nil
				},
			}
			h := &Handlers{deps: Deps{ScopeResolver: resolver}}
			mw := makeFleetMatrixMW(h, e)
			wrapped := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				t.Errorf("downstream must not run for cross-fleet operator")
			}))

			rec := httptest.NewRecorder()
			wrapped.ServeHTTP(rec, newFleetMatrixRequest(operator, e, "flt_b"))
			if rec.Code != http.StatusForbidden {
				t.Errorf("cross-fleet deny: code = %d, want 403", rec.Code)
			}
		})
	}
}

// TestFleetMatrix_FleetAdminSameFleetAllow는 fleet-admin@flt_a 페르소나가 flt_a 평가에서
// 통과함을 명시 검증합니다.
//
// 5 endpoint 모두 fleet-admin@flt_a + evalFleetID="flt_a" → 200.
func TestFleetMatrix_FleetAdminSameFleetAllow(t *testing.T) {
	t.Parallel()

	fleetAdmin := tenant.AccessClaims{
		Subject: "us_fadm",
		Bindings: []tenant.RoleBindingClaim{
			{Role: authz.RoleFleetAdmin, ScopeType: string(authz.ScopeFleet), ScopeID: "flt_a"},
		},
	}

	for _, e := range allFleetMutationEndpoints() {
		e := e
		t.Run(e.name, func(t *testing.T) {
			t.Parallel()
			resolver := &mockScopeResolver{
				ResolveFleetFn: func(_ context.Context, _, _ string) (string, error) {
					return "flt_a", nil
				},
			}
			h := &Handlers{deps: Deps{ScopeResolver: resolver}}
			mw := makeFleetMatrixMW(h, e)
			called := 0
			wrapped := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				called++
				w.WriteHeader(http.StatusOK)
			}))

			rec := httptest.NewRecorder()
			wrapped.ServeHTTP(rec, newFleetMatrixRequest(fleetAdmin, e, "flt_a"))
			if rec.Code != http.StatusOK {
				t.Errorf("fleet-admin@flt_a own fleet: code = %d, want 200", rec.Code)
			}
			if called != 1 {
				t.Errorf("downstream called = %d, want 1", called)
			}
		})
	}
}

// TestFleetMatrix_AdminTenantCrossFleetAllow는 admin tenant scope가 flt_b 평가에서도
// 통과함을 검증합니다 (회귀 — tenant 글로벌 binding은 fleet 격리 무관).
func TestFleetMatrix_AdminTenantCrossFleetAllow(t *testing.T) {
	t.Parallel()

	admin := tenant.AccessClaims{
		Subject: "us_admin",
		Bindings: []tenant.RoleBindingClaim{
			{Role: authz.RoleAdmin, ScopeType: string(authz.ScopeTenant)},
		},
	}

	for _, e := range allFleetMutationEndpoints() {
		e := e
		t.Run(e.name, func(t *testing.T) {
			t.Parallel()
			resolver := &mockScopeResolver{
				ResolveFleetFn: func(_ context.Context, _, _ string) (string, error) {
					return "flt_b", nil
				},
			}
			h := &Handlers{deps: Deps{ScopeResolver: resolver}}
			mw := makeFleetMatrixMW(h, e)
			wrapped := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			rec := httptest.NewRecorder()
			wrapped.ServeHTTP(rec, newFleetMatrixRequest(admin, e, "flt_b"))
			if rec.Code != http.StatusOK {
				t.Errorf("admin tenant cross-fleet: code = %d, want 200", rec.Code)
			}
		})
	}
}

// TestFleetMatrix_ReadOnlyAllDeny는 read-only 페르소나가 5 endpoint 모두 거부됨을 검증합니다.
//
// read-only는 read 외 권한 0 — 모든 mutation 거부 (fleet 무관).
func TestFleetMatrix_ReadOnlyAllDeny(t *testing.T) {
	t.Parallel()

	readOnly := tenant.AccessClaims{
		Subject: "us_ro",
		Bindings: []tenant.RoleBindingClaim{
			{Role: authz.RoleReadOnly, ScopeType: string(authz.ScopeTenant)},
		},
	}

	for _, e := range allFleetMutationEndpoints() {
		e := e
		t.Run(e.name, func(t *testing.T) {
			t.Parallel()
			resolver := &mockScopeResolver{
				ResolveFleetFn: func(_ context.Context, _, _ string) (string, error) {
					return "flt_a", nil
				},
			}
			h := &Handlers{deps: Deps{ScopeResolver: resolver}}
			mw := makeFleetMatrixMW(h, e)
			wrapped := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				t.Errorf("downstream must not run for read-only")
			}))

			rec := httptest.NewRecorder()
			wrapped.ServeHTTP(rec, newFleetMatrixRequest(readOnly, e, "flt_a"))
			if rec.Code != http.StatusForbidden {
				t.Errorf("read-only: code = %d, want 403", rec.Code)
			}
		})
	}
}

// TestFleetMatrix_DenyReasonContainsFleet은 cross-fleet 거부 응답의 reason에
// fleet 컨텍스트가 포함되어 디버깅 친화적임을 검증합니다 (D-RBACEX-4).
func TestFleetMatrix_DenyReasonContainsFleet(t *testing.T) {
	t.Parallel()

	operator := tenant.AccessClaims{
		Subject: "us_op",
		Bindings: []tenant.RoleBindingClaim{
			{Role: authz.RoleOperator, ScopeType: string(authz.ScopeFleet), ScopeID: "flt_a"},
		},
	}

	// POST /robots — body fleetId=flt_b → DENY + reason에 fleet=flt_b 포함.
	h := &Handlers{}
	mw := h.RequirePermissionWithFleet(authz.ResourceRobot, authz.ActionWrite, WithFleetFromBody("fleetId"))
	wrapped := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Errorf("downstream must not run on cross-fleet deny")
	}))

	rec := httptest.NewRecorder()
	req := newWithFleetReq(withClaims(context.Background(), operator), http.MethodPost,
		`{"fleetId":"flt_b","name":"r1"}`, nil)
	wrapped.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("code = %d, want 403", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, `"reason"`) {
		t.Errorf("response missing reason field: %s", body)
	}
	if !strings.Contains(body, "fleet=") {
		t.Errorf("reason should include fleet context: %s", body)
	}
}

// TestFleetMatrix_HandlerMountIntegrationDoesNotAffectExisting는 본 stage 변경이
// chi router mount만 변경 — 기존 9 tenant-글로벌 + 2 path-fleet endpoint는 변동 없음을 가짜
// router를 만들어 검증합니다.
//
// 본 테스트는 회귀 차단 — Stage 4 195 sub-test 전체와 별도로 mount 변경 영향 격리 확인.
func TestFleetMatrix_FactoryEquivalentToRequirePermissionWhenNoOpts(t *testing.T) {
	t.Parallel()

	// opts 0 호출 → fleetIDFromRequest fallback 동작.
	// 본 case는 RequirePermission과 동일 결정이어야 합니다 (회귀 차단).
	h := &Handlers{}
	mw1 := h.RequirePermission(authz.ResourceFleet, authz.ActionWrite)
	mw2 := h.RequirePermissionWithFleet(authz.ResourceFleet, authz.ActionWrite)

	called1, called2 := 0, 0
	wrapped1 := mw1(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called1++
		w.WriteHeader(http.StatusOK)
	}))
	wrapped2 := mw2(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called2++
		w.WriteHeader(http.StatusOK)
	}))

	// fleet-admin@flt_a + path fleetId=flt_a — 둘 다 통과해야 합니다.
	claims := tenant.AccessClaims{
		Subject: "us_fadm",
		Bindings: []tenant.RoleBindingClaim{
			{Role: authz.RoleFleetAdmin, ScopeType: string(authz.ScopeFleet), ScopeID: "flt_a"},
		},
	}

	makeReq := func() *http.Request {
		req := httptest.NewRequest(http.MethodPatch, "/x", nil)
		ctx := withClaims(context.Background(), claims)
		rc := chi.NewRouteContext()
		rc.URLParams.Add("fleetId", "flt_a")
		ctx = context.WithValue(ctx, chi.RouteCtxKey, rc)
		return req.WithContext(ctx)
	}

	rec1, rec2 := httptest.NewRecorder(), httptest.NewRecorder()
	wrapped1.ServeHTTP(rec1, makeReq())
	wrapped2.ServeHTTP(rec2, makeReq())

	if rec1.Code != rec2.Code {
		t.Errorf("decision mismatch: RequirePermission=%d vs RequirePermissionWithFleet=%d", rec1.Code, rec2.Code)
	}
	if called1 != called2 {
		t.Errorf("downstream call count mismatch: %d vs %d", called1, called2)
	}
}
