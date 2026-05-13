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
	"github.com/ssabro/rosshield/internal/app/scanrun"
	"github.com/ssabro/rosshield/internal/app/webhookrun"
	"github.com/ssabro/rosshield/internal/domain/advisor"
	"github.com/ssabro/rosshield/internal/domain/audit"
	"github.com/ssabro/rosshield/internal/domain/benchmark"
	"github.com/ssabro/rosshield/internal/domain/compliance"
	"github.com/ssabro/rosshield/internal/domain/insight"
	"github.com/ssabro/rosshield/internal/domain/integration/webhook"
	"github.com/ssabro/rosshield/internal/domain/reporting"
	"github.com/ssabro/rosshield/internal/domain/robot"
	"github.com/ssabro/rosshield/internal/domain/scan"
	"github.com/ssabro/rosshield/internal/domain/tenant"
	"github.com/ssabro/rosshield/internal/domain/tenant/sso"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/eventbus"
	"github.com/ssabro/rosshield/internal/platform/license"
	"github.com/ssabro/rosshield/internal/platform/metrics"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// Deps는 핸들러 의존성 묶음입니다.
//
// bootstrap이 *Platform에서 필요한 도메인 서비스만 추출하여 주입.
// Phase 1 Stage B는 Storage·Tenant·Robot·Scan·Reporting만 직접 사용 — 나머지는 후속 Stage.
type Deps struct {
	Storage           storage.Storage
	Clock             clock.Clock
	Tenant            tenant.Service
	Robot             robot.Service
	FleetScanSched    FleetScanScheduler // dynamic cron re-registration on fleet mutation
	Scan              scan.Service
	ScanRun           *scanrun.Orchestrator // E12 Stage 8 — production scanrun 결선 (CreateScan async trigger)
	Benchmark         benchmark.Service     // E12 Stage 3 — GET /api/v1/packs (built-in + tenant pack 표시)
	Reporting         reporting.Service
	Insight           insight.Service          // E17 Phase 2
	Compliance        compliance.Service       // E17 Phase 2
	Advisor           advisor.Service          // E16 Phase 2 — LLM 옵트인
	Audit             audit.Service            // B1 — Web UI Audit 페이지 (GET /audit/head)
	EventBus          eventbus.Bus             // C1 carryover — WebSocket scan progress 구독
	License           *license.Enforcer        // E24-C — Open-core enterprise feature 게이트
	SSO               sso.Service              // E20-A Phase 3 — SSO scaffold (옵트인, nil이면 503)
	Webhook           webhook.Service          // E23-C Phase 3 — Webhook CRUD HTTP 표면
	WebhookDispatcher *webhookrun.Dispatcher   // E29 — POST /webhooks/{id}/test (옵트인, nil이면 503)
	Invitation        tenant.InvitationService // E21 — 초대·역할 (옵트인, nil이면 503)
	Metrics           *metrics.Registry        // GET /api/v1/usage/stats — usage 통계 카운트 read (nil이면 503)
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

	// E21 — invitation by-token (비인증 — token이 capability).
	r.Get("/api/v1/invitations/by-token/{token}", func(w http.ResponseWriter, req *http.Request) {
		h.GetInvitationByToken(w, req, chi.URLParam(req, "token"))
	})
	r.Post("/api/v1/invitations/by-token/{token}/accept", func(w http.ResponseWriter, req *http.Request) {
		h.AcceptInvitation(w, req, chi.URLParam(req, "token"))
	})

	// C1 — WebSocket scan progress (자체 인증, 헤더 또는 ?access_token= query).
	// 브라우저 WebSocket API는 Authorization 헤더 부착 불가 → query token 우회 fallback 필요.
	// AuthMiddleware 우회 + 핸들러 내부 검증.
	r.Get("/api/v1/scans/{sessionId}/progress", func(w http.ResponseWriter, req *http.Request) {
		h.ScanProgress(w, req, chi.URLParam(req, "sessionId"))
	})

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
		r.Get("/api/v1/reports", h.ListReports)
		r.Get("/api/v1/reports/{reportId}/download", func(w http.ResponseWriter, req *http.Request) {
			h.DownloadReport(w, req, chi.URLParam(req, "reportId"))
		})

		// 미구현 endpoint들 (gen.Unimplemented 위임 — 자동 501)
		r.Get("/api/v1/audit/head", h.GetAuditHead)
		r.Get("/api/v1/usage/stats", h.GetUsageStats)
		r.Get("/api/v1/tenants/current", h.GetCurrentTenant)
		r.Get("/api/v1/robots/{robotId}", func(w http.ResponseWriter, req *http.Request) {
			h.GetRobot(w, req, chi.URLParam(req, "robotId"))
		})
		r.Get("/api/v1/robots/{robotId}/results", func(w http.ResponseWriter, req *http.Request) {
			h.ListRobotResults(w, req, chi.URLParam(req, "robotId"))
		})
		r.Get("/api/v1/scans", func(w http.ResponseWriter, req *http.Request) {
			h.ListScans(w, req, gen.ListScansParams{})
		})
		// E12 — 단일 scan session 조회 (Web UI 페이지 reload·polling fallback).
		r.Get("/api/v1/scans/{sessionId}", func(w http.ResponseWriter, req *http.Request) {
			h.GetScan(w, req, chi.URLParam(req, "sessionId"))
		})

		// E17 Phase 2 — Insight read.
		r.Get("/api/v1/insights", func(w http.ResponseWriter, req *http.Request) {
			h.ListInsights(w, req, parseListInsightsParams(req))
		})

		// E17 Phase 2 — Compliance read.
		r.Get("/api/v1/compliance/profiles", h.ListComplianceProfiles)
		r.Get("/api/v1/compliance/profiles/{profileId}/snapshots", func(w http.ResponseWriter, req *http.Request) {
			h.ListComplianceSnapshots(w, req, chi.URLParam(req, "profileId"), parseListSnapshotsParams(req))
		})

		// E16 Phase 2 / E19-3 — Advisor 도메인 표면 (옵트인).
		// chi 직접 mount — openapi spec(advisor) 후속 정리 (SESSION_HANDOFF 메모).
		r.Post("/api/v1/advisor/conversations:ask", h.AskAdvisor)
		r.Get("/api/v1/advisor/conversations", h.ListAdvisorConversations)
		r.Get("/api/v1/advisor/conversations/{conversationId}", func(w http.ResponseWriter, req *http.Request) {
			h.GetAdvisorConversation(w, req, chi.URLParam(req, "conversationId"))
		})

		// E24 — License info (B5 Web Console 지원).
		r.Get("/api/v1/license", h.GetLicenseInfo)

		// Fleet list (tenant scope, name ASC). 모든 인증 사용자 read.
		// scans 페이지 fleet dropdown + 다른 페이지 fleet 조회 활용.
		r.Get("/api/v1/fleets", h.ListFleets)
		// 단일 fleet 조회 (deep-link /fleets/$id 진입 응답 대기 회피).
		r.Get("/api/v1/fleets/{fleetId}", func(w http.ResponseWriter, req *http.Request) {
			h.GetFleet(w, req, chi.URLParam(req, "fleetId"))
		})

		// E12 Stage 3 — Pack list (built-in + tenant pack). 모든 인증 사용자 read.
		// systemTenant pack(cross-tenant 공유, §4.2)과 호출자 tenant pack 합쳐 반환.
		r.Get("/api/v1/packs", h.ListPacks)
		// E12 Stage 5 — Pack detail (checks 포함). systemTenant 우선 → caller fallback.
		r.Get("/api/v1/packs/{packKey}", func(w http.ResponseWriter, req *http.Request) {
			h.GetPack(w, req, chi.URLParam(req, "packKey"))
		})
		// E12 Stage 6 — Check detail (audit cmd + eval rule + rationale + fix).
		r.Get("/api/v1/packs/{packKey}/checks/{checkId}", func(w http.ResponseWriter, req *http.Request) {
			h.GetCheck(w, req, chi.URLParam(req, "packKey"), chi.URLParam(req, "checkId"))
		})
		// E12 Stage 7 — Selftest fixture (builtin pack 한정).
		r.Get("/api/v1/packs/{packKey}/checks/{checkId}/selftest", func(w http.ResponseWriter, req *http.Request) {
			h.GetCheckSelftest(w, req, chi.URLParam(req, "packKey"), chi.URLParam(req, "checkId"))
		})

		// E20-A Phase 3 — SSO scaffold (OIDC + SAML, 옵트인).
		// 본 stage는 protected group에 mount — 후속 stage에서 비인증 진입(사용자가 패스워드 모름)
		// 위해 별 group으로 이동 + tenant 결정 path 정리(서브도메인·헤더·설정 file 중 택일).
		r.Get("/api/v1/auth/sso/{providerId}/login", func(w http.ResponseWriter, req *http.Request) {
			h.StartSSOLogin(w, req, chi.URLParam(req, "providerId"))
		})
		r.Get("/api/v1/auth/sso/{providerId}/callback", func(w http.ResponseWriter, req *http.Request) {
			h.CompleteSSOLoginOIDC(w, req, chi.URLParam(req, "providerId"))
		})
		r.Post("/api/v1/auth/sso/{providerId}/saml/acs", func(w http.ResponseWriter, req *http.Request) {
			h.CompleteSSOLoginSAML(w, req, chi.URLParam(req, "providerId"))
		})

		// E21 Phase 3 — Invitation read (모든 인증 사용자), mutation은 admin gate (RBAC Stage 1).
		r.Get("/api/v1/invitations", h.ListInvitations)

		// E20-D Phase 3 — SSO Provider read (모든 인증 사용자), mutation은 admin gate.
		r.Get("/api/v1/sso/providers", h.ListSSOProviders)
		r.Get("/api/v1/sso/providers/{providerId}", func(w http.ResponseWriter, req *http.Request) {
			h.GetSSOProvider(w, req, chi.URLParam(req, "providerId"))
		})

		// E23-C Phase 3 — Webhook read (모든 인증 사용자), mutation은 admin gate.
		r.Get("/api/v1/webhooks", h.ListWebhookEndpoints)
		r.Get("/api/v1/webhooks/{endpointId}", h.getWebhookEndpointFromChi)
		r.Get("/api/v1/webhooks/{endpointId}/deliveries", h.listWebhookDeliveriesFromChi)

		// RBAC Stage 1+2 — admin role 전용 mutation 그룹.
		// Phase 5 1차는 admin/operator 2-tier 단순화. auditor 별 read-only 그룹은 후속.
		r.Group(func(r chi.Router) {
			r.Use(h.RequireRole("admin"))

			// === Stage 1 (E21·E20-D·E23-C) ===

			// Invitation mutation
			r.Post("/api/v1/invitations", h.CreateInvitation)
			r.Delete("/api/v1/invitations/{invitationId}", func(w http.ResponseWriter, req *http.Request) {
				h.RevokeInvitation(w, req, chi.URLParam(req, "invitationId"))
			})

			// SSO Provider mutation
			r.Post("/api/v1/sso/providers", h.CreateSSOProvider)
			r.Put("/api/v1/sso/providers/{providerId}", func(w http.ResponseWriter, req *http.Request) {
				h.UpdateSSOProvider(w, req, chi.URLParam(req, "providerId"))
			})
			r.Delete("/api/v1/sso/providers/{providerId}", func(w http.ResponseWriter, req *http.Request) {
				h.DeleteSSOProvider(w, req, chi.URLParam(req, "providerId"))
			})

			// Webhook mutation + test (E29)
			r.Post("/api/v1/webhooks", h.CreateWebhookEndpoint)
			r.Put("/api/v1/webhooks/{endpointId}", h.updateWebhookEndpointFromChi)
			r.Delete("/api/v1/webhooks/{endpointId}", h.deleteWebhookEndpointFromChi)
			r.Post("/api/v1/webhooks/{endpointId}/test", h.testWebhookEndpointFromChi)

			// === Stage 2 (Robot·Scan·Audit·Report·Insight·Compliance mutation) ===

			// Robot 등록 (시스템 자산 추가)
			r.Post("/api/v1/robots", func(w http.ResponseWriter, req *http.Request) {
				h.CreateRobot(w, req, gen.CreateRobotParams{})
			})
			// Robot 삭제 (soft delete, R3-5).
			r.Delete("/api/v1/robots/{robotId}", func(w http.ResponseWriter, req *http.Request) {
				h.DeleteRobot(w, req, chi.URLParam(req, "robotId"))
			})
			// Robot credential 회전 (R3-3, audit emit).
			r.Post("/api/v1/robots/{robotId}/credential:rotate", func(w http.ResponseWriter, req *http.Request) {
				h.RotateCredential(w, req, chi.URLParam(req, "robotId"))
			})
			// SSH fingerprint 미리보기 (admin, ephemeral 계산만 — 영속 X).
			r.Post("/api/v1/utils/ssh-fingerprint", h.SSHFingerprint)

			// Scan 실행 (시스템 자원 소비)
			r.Post("/api/v1/scans", func(w http.ResponseWriter, req *http.Request) {
				h.CreateScan(w, req, gen.CreateScanParams{})
			})
			// Scan cancel (running/pending 세션 강제 종료)
			r.Post("/api/v1/scans/{sessionId}:cancel", func(w http.ResponseWriter, req *http.Request) {
				h.CancelScan(w, req, chi.URLParam(req, "sessionId"))
			})

			// Audit verify (감사 작업)
			r.Post("/api/v1/audit/verify", h.VerifyAuditChain)

			// Report verify (감사 작업)
			r.Post("/api/v1/reports/{reportId}:verify", func(w http.ResponseWriter, req *http.Request) {
				h.VerifyReport(w, req, chi.URLParam(req, "reportId"))
			})

			// Insight mutation
			r.Post("/api/v1/insights/{insightId}:dismiss", func(w http.ResponseWriter, req *http.Request) {
				h.DismissInsight(w, req, chi.URLParam(req, "insightId"))
			})
			r.Post("/api/v1/fleets/{fleetId}/insights:run", func(w http.ResponseWriter, req *http.Request) {
				h.RunFleetInsights(w, req, chi.URLParam(req, "fleetId"))
			})

			// Fleet mutation
			r.Post("/api/v1/fleets", h.CreateFleet)
			r.Patch("/api/v1/fleets/{fleetId}", func(w http.ResponseWriter, req *http.Request) {
				h.UpdateFleet(w, req, chi.URLParam(req, "fleetId"))
			})
			r.Delete("/api/v1/fleets/{fleetId}", func(w http.ResponseWriter, req *http.Request) {
				h.DeleteFleet(w, req, chi.URLParam(req, "fleetId"))
			})

			// Compliance mutation
			r.Post("/api/v1/compliance/profiles", h.CreateComplianceProfile)
			r.Post("/api/v1/compliance/profiles/{profileId}/snapshots", func(w http.ResponseWriter, req *http.Request) {
				h.GenerateComplianceSnapshot(w, req, chi.URLParam(req, "profileId"))
			})
		})
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
	case errors.Is(err, license.ErrQuotaExceeded),
		errors.Is(err, license.ErrFeatureGated):
		return http.StatusPaymentRequired
	default:
		return http.StatusInternalServerError
	}
}

// writeQuotaError는 license.QuotaCheckResult 거부 응답을 402 Payment Required로 작성합니다.
//
// 응답 본문: {"error":"quota exceeded","reason":"<reason>","field":"<field>"}.
// reason은 운영자 메시지 — "robots quota exceeded (current=99 add=1 max=100)" 등.
// field는 클라이언트가 분기 가능한 식별자 — "robots_max", "scans_per_day", "llm_tokens_per_day", "feature:<name>".
func writeQuotaError(w http.ResponseWriter, result license.QuotaCheckResult) {
	writeJSON(w, http.StatusPaymentRequired, map[string]string{
		"error":  "quota exceeded",
		"reason": result.Reason,
		"field":  result.Field,
	})
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
