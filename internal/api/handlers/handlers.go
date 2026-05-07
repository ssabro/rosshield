// Package handlers는 OpenAPI에 정의된 HTTP 엔드포인트의 도메인 결선 구현체입니다 (E9 Stage B).
//
// 책임 분담:
//   - oapi-codegen이 생성한 `gen.ServerInterface`(13개 메서드)를 `*Handlers`가 구현
//   - Phase 1 Stage B는 5개 endpoint만 실구현 (Login·Me·ListRobots·StartScan·ListReports)
//   - 나머지는 `gen.Unimplemented` embed로 자동 501 반환
//   - JWT auth middleware는 보호된 path에 자동 적용 (Bearer → tenant.AccessClaims → ctx 주입)
//
// R11 합의:
//   - R11-6: chi-server 스텁 활용 (재생성 없이 spec과 결선)
//   - R11-8: HTTP exit code 매핑은 CLI 책임 — 서버는 표준 status code (200/201/400/401/403/404/500)
//
// 도메인 경계 규칙(P5):
//
//	본 패키지는 domain.* Service interface만 호출하며, repo·storage 직접 접근 금지.
//	Storage.Tx는 미들웨어가 ctx에 TenantID를 주입한 후 호출자(handler)가 진입.
package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/ssabro/rosshield/internal/api/gen"
	"github.com/ssabro/rosshield/internal/domain/advisor"
	"github.com/ssabro/rosshield/internal/domain/audit"
	"github.com/ssabro/rosshield/internal/domain/compliance"
	"github.com/ssabro/rosshield/internal/domain/insight"
	"github.com/ssabro/rosshield/internal/domain/reporting"
	"github.com/ssabro/rosshield/internal/domain/robot"
	"github.com/ssabro/rosshield/internal/domain/scan"
	"github.com/ssabro/rosshield/internal/domain/tenant"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/eventbus"
	"github.com/ssabro/rosshield/internal/platform/license"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// Deps는 핸들러 의존성 묶음입니다.
//
// bootstrap이 *Platform에서 필요한 도메인 서비스만 추출하여 주입.
// Phase 1 Stage B는 Storage·Tenant·Robot·Scan·Reporting만 직접 사용 — 나머지는 후속 Stage.
type Deps struct {
	Storage    storage.Storage
	Clock      clock.Clock
	Tenant     tenant.Service
	Robot      robot.Service
	Scan       scan.Service
	Reporting  reporting.Service
	Insight    insight.Service    // E17 Phase 2
	Compliance compliance.Service // E17 Phase 2
	Advisor    advisor.Service    // E16 Phase 2 — LLM 옵트인
	Audit      audit.Service      // B1 — Web UI Audit 페이지 (GET /audit/head)
	EventBus   eventbus.Bus       // C1 carryover — WebSocket scan progress 구독
	License    *license.Enforcer  // E24-C — Open-core enterprise feature 게이트
}

// Handlers는 gen.ServerInterface 구현체입니다.
//
// gen.Unimplemented 임베딩으로 미구현 endpoint는 자동 501 반환 — 본 Stage가 override한
// 5개(Login·GetCurrentSession·ListRobots·CreateScan/없음 — ListReports 추가)만 동작.
//
// 주의: ListReports는 OpenAPI spec에 정의되지 않음 (현 spec은 reports/{id}:verify만 있음).
// 본 Stage는 spec 미변경 원칙(R11-6)에 따라 ListReports 대신 VerifyReport는 501로 두고,
// 핸들러 메서드 ListReports는 chi router에 별도 mount하여 노출 (`GET /api/v1/reports?sessionId=...`).
type Handlers struct {
	gen.Unimplemented // 미구현 endpoint는 자동 501

	deps Deps
}

// New는 새 Handlers를 반환합니다.
func New(deps Deps) *Handlers {
	return &Handlers{deps: deps}
}

// Mount는 chi 라우터에 모든 endpoint를 mount합니다.
//
// 절차:
//  1. /healthz·/readyz·login은 인증 없이 노출
//  2. /api/v1/* 나머지는 AuthMiddleware로 보호
//  3. ListReports는 OpenAPI spec 미정의 — chi에 직접 등록
//
// chi router를 받아 modify — 호출자(main.go)가 NewRouter() 후 본 메서드로 결선.
func (h *Handlers) Mount(r chi.Router) {
	// 1. Public endpoints — auth 미적용
	r.Post("/api/v1/auth/login", h.Login)
	r.Post("/api/v1/auth/refresh", h.RefreshAuth) // C6 — refresh token rotation
	r.Post("/api/v1/auth/logout", h.LogoutAuth)   // C6 — refresh revoke + cookie clear

	// 2. Protected endpoints — AuthMiddleware 통과 후 진입
	r.Group(func(r chi.Router) {
		r.Use(h.AuthMiddleware)
		r.Get("/api/v1/auth/me", h.GetCurrentSession)
		r.Get("/api/v1/robots", func(w http.ResponseWriter, req *http.Request) {
			// chi 직접 등록 — gen 래퍼는 query parsing이 ListRobotsParams 객체로 들어가지만
			// 본 Stage는 fleetId 한 개만 사용하므로 query 직접 추출.
			h.ListRobots(w, req, gen.ListRobotsParams{
				FleetId: stringPtrOrNil(req.URL.Query().Get("fleetId")),
			})
		})
		r.Post("/api/v1/scans", func(w http.ResponseWriter, req *http.Request) {
			h.CreateScan(w, req, gen.CreateScanParams{})
		})
		r.Get("/api/v1/reports", h.ListReports)

		// 미구현 endpoint들 (gen.Unimplemented 위임 — 자동 501)
		r.Get("/api/v1/audit/head", h.GetAuditHead)
		r.Post("/api/v1/audit/verify", h.VerifyAuditChain)
		r.Get("/api/v1/tenants/current", h.GetCurrentTenant)
		r.Post("/api/v1/robots", func(w http.ResponseWriter, req *http.Request) {
			h.CreateRobot(w, req, gen.CreateRobotParams{})
		})
		r.Get("/api/v1/robots/{robotId}", func(w http.ResponseWriter, req *http.Request) {
			h.GetRobot(w, req, chi.URLParam(req, "robotId"))
		})
		r.Get("/api/v1/scans", func(w http.ResponseWriter, req *http.Request) {
			h.ListScans(w, req, gen.ListScansParams{})
		})
		r.Post("/api/v1/reports/{reportId}:verify", func(w http.ResponseWriter, req *http.Request) {
			h.VerifyReport(w, req, chi.URLParam(req, "reportId"))
		})

		// E17 Phase 2 — Insight 도메인 표면.
		r.Get("/api/v1/insights", func(w http.ResponseWriter, req *http.Request) {
			h.ListInsights(w, req, parseListInsightsParams(req))
		})
		r.Post("/api/v1/insights/{insightId}:dismiss", func(w http.ResponseWriter, req *http.Request) {
			h.DismissInsight(w, req, chi.URLParam(req, "insightId"))
		})
		r.Post("/api/v1/fleets/{fleetId}/insights:run", func(w http.ResponseWriter, req *http.Request) {
			h.RunFleetInsights(w, req, chi.URLParam(req, "fleetId"))
		})

		// E17 Phase 2 — Compliance 도메인 표면.
		r.Get("/api/v1/compliance/profiles", h.ListComplianceProfiles)
		r.Post("/api/v1/compliance/profiles", h.CreateComplianceProfile)
		r.Get("/api/v1/compliance/profiles/{profileId}/snapshots", func(w http.ResponseWriter, req *http.Request) {
			h.ListComplianceSnapshots(w, req, chi.URLParam(req, "profileId"), parseListSnapshotsParams(req))
		})
		r.Post("/api/v1/compliance/profiles/{profileId}/snapshots", func(w http.ResponseWriter, req *http.Request) {
			h.GenerateComplianceSnapshot(w, req, chi.URLParam(req, "profileId"))
		})

		// E16 Phase 2 / E19-3 — Advisor 도메인 표면 (옵트인).
		// chi 직접 mount — openapi spec(advisor) 후속 정리 (SESSION_HANDOFF 메모).
		r.Post("/api/v1/advisor/conversations:ask", h.AskAdvisor)
		r.Get("/api/v1/advisor/conversations", h.ListAdvisorConversations)
		r.Get("/api/v1/advisor/conversations/{conversationId}", func(w http.ResponseWriter, req *http.Request) {
			h.GetAdvisorConversation(w, req, chi.URLParam(req, "conversationId"))
		})

		// C1 carryover — WebSocket scan progress (Phase 1 deferred 회수).
		r.Get("/api/v1/scans/{sessionId}/progress", func(w http.ResponseWriter, req *http.Request) {
			h.ScanProgress(w, req, chi.URLParam(req, "sessionId"))
		})

		// E24 — License info (B5 Web Console 지원).
		r.Get("/api/v1/license", h.GetLicenseInfo)
	})
}

// parseListInsightsParams는 query string에서 ListInsightsParams를 추출합니다.
//
// gen 래퍼 대신 직접 파싱 — chi 미들웨어 단계에서 typed binding 없이 진입하므로 query 추출.
func parseListInsightsParams(req *http.Request) gen.ListInsightsParams {
	q := req.URL.Query()
	params := gen.ListInsightsParams{}
	if v := q.Get("kind"); v != "" {
		k := gen.ListInsightsParamsKind(v)
		params.Kind = &k
	}
	if v := q.Get("severity"); v != "" {
		s := gen.ListInsightsParamsSeverity(v)
		params.Severity = &s
	}
	if v := q.Get("robotId"); v != "" {
		params.RobotId = &v
	}
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			params.Limit = &n
		}
	}
	return params
}

// parseListSnapshotsParams는 query string에서 ListComplianceSnapshotsParams를 추출합니다.
func parseListSnapshotsParams(req *http.Request) gen.ListComplianceSnapshotsParams {
	q := req.URL.Query()
	params := gen.ListComplianceSnapshotsParams{}
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			params.Limit = &n
		}
	}
	return params
}

// stringPtrOrNil는 빈 문자열을 nil 포인터로 변환합니다 (query 옵션 표현).
func stringPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// writeJSON은 status + JSON body를 응답합니다.
//
// Content-Type을 application/json으로 설정하고 indent 없이 직렬화 — 응답 사이즈 최소화.
// encode 실패는 무시 (이미 헤더가 송신됐을 가능성).
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// writeError는 표준 에러 응답을 작성합니다.
//
// `{"error": "<message>"}` 형식 — Phase 1 단순화. OpenAPI ErrorEnvelope는 후속 Stage에서.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// errorStatusFor는 도메인 sentinel을 HTTP status로 매핑합니다.
//
// 알 수 없는 에러는 500 — 호출자가 message를 노출 여부 결정.
func errorStatusFor(err error) int {
	switch {
	case errors.Is(err, storage.ErrNotFound),
		errors.Is(err, insight.ErrInsightNotFound):
		return http.StatusNotFound
	case errors.Is(err, storage.ErrTenantMissing):
		return http.StatusUnauthorized
	case errors.Is(err, tenant.ErrInvalidCredentials),
		errors.Is(err, tenant.ErrUserDisabled),
		errors.Is(err, tenant.ErrInvalidToken),
		errors.Is(err, tenant.ErrTokenExpired),
		errors.Is(err, tenant.ErrTokenSignatureInvalid):
		return http.StatusUnauthorized
	default:
		return http.StatusInternalServerError
	}
}

// complianceErrorStatus는 compliance 도메인 sentinel을 HTTP status로 매핑합니다.
//
// 별도 함수로 두는 이유: ErrProfileExists → 409, ErrFrameworkVersionMismatch → 400 등
// 일반 errorStatusFor 매핑과 카테고리가 다름.
func complianceErrorStatus(err error) int {
	switch {
	case errors.Is(err, compliance.ErrProfileNotFound),
		errors.Is(err, compliance.ErrSnapshotNotFound):
		return http.StatusNotFound
	case errors.Is(err, compliance.ErrProfileExists):
		return http.StatusConflict
	case errors.Is(err, compliance.ErrFrameworkVersionMismatch),
		errors.Is(err, compliance.ErrUnknownFramework):
		return http.StatusBadRequest
	default:
		return errorStatusFor(err)
	}
}
