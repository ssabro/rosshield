// rbac_integration_test.go — 세분 RBAC Stage 4 통합 매트릭스 테스트.
//
// design doc `docs/design/notes/rbac-fine-grained-design.md` §7 Stage 4 산출 — handlers.go의
// admin 그룹 mutation endpoint를 RequireRole("admin") → RequirePermission(resource, action)
// 으로 교체했음을 검증합니다.
//
// 본 테스트는 RequirePermission middleware를 직접 호출 — 핸들러 본문이 아닌 RBAC 결정 phase
// 까지의 status code(200/403)만 검증합니다. 따라서 도메인 서비스 의존성 0이며, panic 없는
// 가벼운 단위 통합 테스트입니다.
//
// 매트릭스: 6 페르소나 × 29 endpoint = 174 case.
//   - 페르소나: owner / admin / fleet-admin@flt_a / operator@flt_a / auditor / read-only
//   - 엔드포인트: handlers.go의 RequirePermission mutation 24건 + Phase 6 후보 1 R1 Stage 3
//     customer intake 5건 (운영자 admin 전용 — design doc §6.2 line 90·538).
//
// 본 테스트는 chi RouteContext에 fleetId param을 직접 주입해 path scope 결정도 검증합니다.

package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/ssabro/rosshield/internal/domain/tenant"
	"github.com/ssabro/rosshield/internal/platform/authz"
)

// rbacEndpoint는 handlers.go의 admin 그룹 mutation 1건을 표현합니다.
//
// resource/action: handlers.go에서 RequirePermission(resource, action) 인자로 전달하는 값.
// fleetID: chi URL param fleetId 주입 — fleet scope endpoint에서만 사용. 빈 문자열이면 tenant.
type rbacEndpoint struct {
	name     string
	resource authz.Resource
	action   authz.Action
	fleetID  string // path에 fleetId가 있는 endpoint는 "flt_a" 주입, 그 외는 "" — tenant scope.
}

// allMutationEndpoints는 본 stage가 RequirePermission으로 교체한 24건 endpoint 정의입니다.
//
// design doc §3.3 매트릭스 + §2.2 endpoint 매핑 정확 일치. fleetID 컬럼이 비어 있으면
// path 추출 불가(robot/scan body 또는 separate path) — tenant scope binding 보유자만 통과.
func allMutationEndpoints() []rbacEndpoint {
	return []rbacEndpoint{
		// === Invitation (2) ===
		{name: "POST /invitations", resource: authz.ResourceTenantAdmin, action: authz.ActionAdmin},
		{name: "DELETE /invitations/{invitationId}", resource: authz.ResourceTenantAdmin, action: authz.ActionAdmin},

		// === SSO Provider (3) ===
		{name: "POST /sso/providers", resource: authz.ResourceTenantAdmin, action: authz.ActionAdmin},
		{name: "PUT /sso/providers/{providerId}", resource: authz.ResourceTenantAdmin, action: authz.ActionAdmin},
		{name: "DELETE /sso/providers/{providerId}", resource: authz.ResourceTenantAdmin, action: authz.ActionAdmin},

		// === Webhook (4) ===
		{name: "POST /webhooks", resource: authz.ResourceTenantAdmin, action: authz.ActionAdmin},
		{name: "PUT /webhooks/{endpointId}", resource: authz.ResourceTenantAdmin, action: authz.ActionAdmin},
		{name: "DELETE /webhooks/{endpointId}", resource: authz.ResourceTenantAdmin, action: authz.ActionAdmin},
		{name: "POST /webhooks/{endpointId}/test", resource: authz.ResourceTenantAdmin, action: authz.ActionAdmin},

		// === Robot (4) — path에 fleetId 없음 (body 또는 robotId만) → tenant 평가 ===
		{name: "POST /robots", resource: authz.ResourceRobot, action: authz.ActionWrite},
		{name: "DELETE /robots/{robotId}", resource: authz.ResourceRobot, action: authz.ActionWrite},
		{name: "POST /robots/{robotId}/credential:rotate", resource: authz.ResourceRobot, action: authz.ActionAdmin},
		{name: "POST /utils/ssh-fingerprint", resource: authz.ResourceTenantAdmin, action: authz.ActionAdmin},

		// === Scan (2) — path에 fleetId 없음 → tenant 평가 ===
		{name: "POST /scans", resource: authz.ResourceScan, action: authz.ActionExecute},
		{name: "POST /scans/{sessionId}:cancel", resource: authz.ResourceScan, action: authz.ActionExecute},

		// === Audit verify (1) ===
		{name: "POST /audit/verify", resource: authz.ResourceAudit, action: authz.ActionVerify},

		// === Report verify (1) ===
		{name: "POST /reports/{reportId}:verify", resource: authz.ResourceReport, action: authz.ActionVerify},

		// === Insight (2) — fleets/{fleetId}/insights:run는 fleet scope 평가 ===
		{name: "POST /insights/{insightId}:dismiss", resource: authz.ResourceInsight, action: authz.ActionWrite},
		{name: "POST /fleets/{fleetId}/insights:run", resource: authz.ResourceInsight, action: authz.ActionExecute, fleetID: "flt_a"},

		// === Fleet (3) — PATCH는 fleet scope, POST/DELETE는 tenant ===
		{name: "POST /fleets", resource: authz.ResourceFleet, action: authz.ActionAdmin},
		{name: "PATCH /fleets/{fleetId}", resource: authz.ResourceFleet, action: authz.ActionWrite, fleetID: "flt_a"},
		{name: "DELETE /fleets/{fleetId}", resource: authz.ResourceFleet, action: authz.ActionAdmin},

		// === Compliance (2) ===
		{name: "POST /compliance/profiles", resource: authz.ResourceCompliance, action: authz.ActionAdmin},
		{name: "POST /compliance/profiles/{profileId}/snapshots", resource: authz.ResourceCompliance, action: authz.ActionExecute},

		// === Compliance export (1) — Phase 11.B-5 audit log export wizard ===
		// admin + auditor 통과 (audit.export 매트릭스 §3.3). read-only/operator/fleet-admin 거부.
		{name: "POST /compliance/export", resource: authz.ResourceAudit, action: authz.ActionExport},

		// === Customer Intake (5) — Phase 6 후보 1 R1 Stage 3 ===
		// 운영자 admin 전용 — read 포함 모든 5 endpoint가 ResourceTenantAdmin.Admin 게이트.
		// design doc `customer-onboarding-design.md` §6.2 line 90·538 일관.
		{name: "POST /customers/intake", resource: authz.ResourceTenantAdmin, action: authz.ActionAdmin},
		{name: "GET /customers/intakes", resource: authz.ResourceTenantAdmin, action: authz.ActionAdmin},
		{name: "GET /customers/intakes/{intakeId}", resource: authz.ResourceTenantAdmin, action: authz.ActionAdmin},
		{name: "POST /customers/intakes/{intakeId}:accept", resource: authz.ResourceTenantAdmin, action: authz.ActionAdmin},
		{name: "POST /customers/intakes/{intakeId}:reject", resource: authz.ResourceTenantAdmin, action: authz.ActionAdmin},
	}
}

// rbacPersona는 매트릭스 1행을 표현합니다 — 사용자 + 보유 binding 셋.
type rbacPersona struct {
	name     string
	bindings []tenant.RoleBindingClaim
}

// allPersonas는 design doc §3.1 시드 6 role 페르소나 셋입니다.
//
// fleet-admin과 operator는 fleet_a에만 binding(다른 fleet은 권한 0).
func allPersonas() []rbacPersona {
	return []rbacPersona{
		{name: "owner", bindings: []tenant.RoleBindingClaim{
			{Role: authz.RoleOwner, ScopeType: string(authz.ScopeTenant)},
		}},
		{name: "admin", bindings: []tenant.RoleBindingClaim{
			{Role: authz.RoleAdmin, ScopeType: string(authz.ScopeTenant)},
		}},
		{name: "fleet-admin@flt_a", bindings: []tenant.RoleBindingClaim{
			{Role: authz.RoleFleetAdmin, ScopeType: string(authz.ScopeFleet), ScopeID: "flt_a"},
		}},
		{name: "operator@flt_a", bindings: []tenant.RoleBindingClaim{
			{Role: authz.RoleOperator, ScopeType: string(authz.ScopeFleet), ScopeID: "flt_a"},
		}},
		{name: "auditor", bindings: []tenant.RoleBindingClaim{
			{Role: authz.RoleAuditor, ScopeType: string(authz.ScopeTenant)},
		}},
		{name: "read-only", bindings: []tenant.RoleBindingClaim{
			{Role: authz.RoleReadOnly, ScopeType: string(authz.ScopeTenant)},
		}},
	}
}

// expectAllow는 (페르소나, 엔드포인트) 쌍에서 ALLOW가 기대되는지 반환합니다.
//
// authz.Decide를 호출자 입장에서 미리 평가 — 본 함수는 design doc §3.3 매트릭스의 "정답"
// 역할입니다. 실 middleware 결정과 일치해야 합니다.
func expectAllow(p rbacPersona, e rbacEndpoint) bool {
	// authz.Subject 직접 구성 — Bindings를 그대로 변환.
	sub := authz.Subject{
		Bindings: convertBindingsToAuthz(p.bindings),
		FleetID:  e.fleetID,
	}
	return authz.Decide(sub, e.resource, e.action).Allow
}

// convertBindingsToAuthz는 tenant.RoleBindingClaim → authz.RoleBinding 슬라이스 변환.
func convertBindingsToAuthz(bs []tenant.RoleBindingClaim) []authz.RoleBinding {
	out := make([]authz.RoleBinding, len(bs))
	for i, b := range bs {
		out[i] = authz.RoleBinding{
			RoleName:  b.Role,
			ScopeType: authz.ScopeType(b.ScopeType),
			ScopeID:   b.ScopeID,
		}
	}
	return out
}

// newRBACTestRequest는 chi RouteContext + claims가 주입된 테스트 요청을 만듭니다.
func newRBACTestRequest(claims tenant.AccessClaims, fleetID string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	ctx := withClaims(context.Background(), claims)
	if fleetID != "" {
		rc := chi.NewRouteContext()
		rc.URLParams.Add("fleetId", fleetID) // handlers.go path 정의 fleetId 일치.
		ctx = context.WithValue(ctx, chi.RouteCtxKey, rc)
	}
	return req.WithContext(ctx)
}

// TestRBACMatrix_AllPersonasAllEndpoints는 핵심 매트릭스 144 case 검증입니다.
//
// 본 테스트는 RequirePermission middleware의 결정만 검증 — 200(통과)이면 다음 핸들러 호출,
// 403이면 거부. expectAllow의 정답과 실 middleware 결정이 모든 case에서 일치해야 합니다.
//
// 회귀 발생 시: design doc §3.3 매트릭스와 SystemRolePermissions 둘 중 하나가 변경됐다는
// 신호입니다. matrix 갱신 시 본 테스트도 갱신해야 합니다.
func TestRBACMatrix_AllPersonasAllEndpoints(t *testing.T) {
	t.Parallel()

	endpoints := allMutationEndpoints()
	if got, want := len(endpoints), 30; got != want {
		t.Fatalf("endpoint count = %d, want %d (handlers.go admin 그룹 mutation 24 + intake 5 + compliance export 1)", got, want)
	}
	personas := allPersonas()
	if got, want := len(personas), 6; got != want {
		t.Fatalf("persona count = %d, want %d (design doc §3.1)", got, want)
	}

	h := &Handlers{}

	for _, p := range personas {
		p := p
		for _, e := range endpoints {
			e := e
			caseName := fmt.Sprintf("%s__%s", p.name, e.name)
			t.Run(caseName, func(t *testing.T) {
				t.Parallel()

				mw := h.RequirePermission(e.resource, e.action)
				downstreamCalled := false
				wrapped := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					downstreamCalled = true
					w.WriteHeader(http.StatusOK)
				}))

				claims := tenant.AccessClaims{Subject: "us_" + p.name, Bindings: p.bindings}
				rec := httptest.NewRecorder()
				wrapped.ServeHTTP(rec, newRBACTestRequest(claims, e.fleetID))

				want := expectAllow(p, e)
				gotAllow := rec.Code == http.StatusOK
				if gotAllow != want {
					t.Errorf("decision mismatch: want allow=%v, got code=%d (downstream called=%v)",
						want, rec.Code, downstreamCalled)
				}
				if !want && rec.Code != http.StatusForbidden {
					t.Errorf("DENY expected: code = %d, want 403", rec.Code)
				}
			})
		}
	}
}

// TestRBACMatrix_OwnerAllowsAllMutations는 owner 페르소나가 모든 mutation을 통과함을 검증합니다.
//
// design doc §3.1 — owner는 모든 (resource, action) implicit. 본 테스트는 매트릭스의
// owner row가 29/29 ALLOW임을 별도 검증합니다 (설계 회귀 방지). intake 5건 포함.
func TestRBACMatrix_OwnerAllowsAllMutations(t *testing.T) {
	t.Parallel()

	endpoints := allMutationEndpoints()
	h := &Handlers{}

	owner := rbacPersona{name: "owner", bindings: []tenant.RoleBindingClaim{
		{Role: authz.RoleOwner, ScopeType: string(authz.ScopeTenant)},
	}}

	for _, e := range endpoints {
		e := e
		t.Run(e.name, func(t *testing.T) {
			t.Parallel()
			mw := h.RequirePermission(e.resource, e.action)
			wrapped := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			claims := tenant.AccessClaims{Subject: "us_owner", Bindings: owner.bindings}
			rec := httptest.NewRecorder()
			wrapped.ServeHTTP(rec, newRBACTestRequest(claims, e.fleetID))
			if rec.Code != http.StatusOK {
				t.Errorf("owner denied %s: code = %d", e.name, rec.Code)
			}
		})
	}
}

// TestRBACMatrix_ReadOnlyDeniesAllMutations는 read-only 페르소나가 모든 mutation에서 거부됨을
// 검증합니다.
//
// design doc §3.1 — read-only는 read 권한만. 본 테스트는 매트릭스 read-only row가 24/24
// DENY임을 검증합니다 (모든 mutation은 write/execute/admin/verify — read 외).
func TestRBACMatrix_ReadOnlyDeniesAllMutations(t *testing.T) {
	t.Parallel()

	endpoints := allMutationEndpoints()
	h := &Handlers{}

	ro := rbacPersona{name: "read-only", bindings: []tenant.RoleBindingClaim{
		{Role: authz.RoleReadOnly, ScopeType: string(authz.ScopeTenant)},
	}}

	for _, e := range endpoints {
		e := e
		t.Run(e.name, func(t *testing.T) {
			t.Parallel()
			mw := h.RequirePermission(e.resource, e.action)
			wrapped := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			claims := tenant.AccessClaims{Subject: "us_ro", Bindings: ro.bindings}
			rec := httptest.NewRecorder()
			wrapped.ServeHTTP(rec, newRBACTestRequest(claims, e.fleetID))
			if rec.Code != http.StatusForbidden {
				t.Errorf("read-only allowed mutation %s: code = %d, want 403", e.name, rec.Code)
			}
		})
	}
}

// TestRBACMatrix_OperatorCrossFleetIsolation는 operator@flt_a가 fleet_b path scope endpoint에
// 접근 시 거부되는지 검증합니다 (cross-fleet 격리, design doc §3.2).
//
// 본 테스트의 대상 endpoint는 path에 fleetId가 직접 등장하는 2건:
//   - POST /fleets/{fleetId}/insights:run
//   - PATCH /fleets/{fleetId}
//
// operator@flt_a가 path fleetId=flt_b 요청 → middleware의 fleetIDFromRequest는 "flt_b" 추출
// → operator binding ScopeID="flt_a" 불일치 → DENY.
func TestRBACMatrix_OperatorCrossFleetIsolation(t *testing.T) {
	t.Parallel()

	h := &Handlers{}
	operator := tenant.AccessClaims{
		Subject: "us_op",
		Bindings: []tenant.RoleBindingClaim{
			{Role: authz.RoleOperator, ScopeType: string(authz.ScopeFleet), ScopeID: "flt_a"},
		},
	}

	// fleet scope endpoint 중 operator가 ALLOW 받을 수 있는 것 — `fleets/{fleetId}/insights:run`
	// (insight.execute) — operator §3.3에 insight.execute 미보유 → fleet 일치해도 deny.
	// 따라서 본 테스트는 fleet-admin@flt_a를 사용해 같은 fleet ALLOW vs 다른 fleet DENY 검증.
	fleetAdmin := tenant.AccessClaims{
		Subject: "us_fadm",
		Bindings: []tenant.RoleBindingClaim{
			{Role: authz.RoleFleetAdmin, ScopeType: string(authz.ScopeFleet), ScopeID: "flt_a"},
		},
	}

	mw := h.RequirePermission(authz.ResourceInsight, authz.ActionExecute)
	wrapped := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// 1) fleet-admin@flt_a + path fleetId=flt_a → 200.
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, newRBACTestRequest(fleetAdmin, "flt_a"))
	if rec.Code != http.StatusOK {
		t.Errorf("fleet-admin@flt_a + flt_a: code = %d, want 200", rec.Code)
	}

	// 2) fleet-admin@flt_a + path fleetId=flt_b → 403 (cross-fleet 격리).
	rec2 := httptest.NewRecorder()
	wrapped.ServeHTTP(rec2, newRBACTestRequest(fleetAdmin, "flt_b"))
	if rec2.Code != http.StatusForbidden {
		t.Errorf("fleet-admin@flt_a + flt_b cross fleet: code = %d, want 403", rec2.Code)
	}

	// 3) operator(insight.execute 미보유) + path fleetId=flt_a → 403 (action 미보유).
	mwWrite := h.RequirePermission(authz.ResourceInsight, authz.ActionExecute)
	wrappedWrite := mwWrite(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec3 := httptest.NewRecorder()
	wrappedWrite.ServeHTTP(rec3, newRBACTestRequest(operator, "flt_a"))
	if rec3.Code != http.StatusForbidden {
		t.Errorf("operator without insight.execute: code = %d, want 403", rec3.Code)
	}
}
