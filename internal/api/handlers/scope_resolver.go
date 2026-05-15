package handlers

// scope_resolver.go — RBAC fleet 정밀화 Stage 2 산출:
//
//  1. ScopeResolver interface — cross-resource fleet lookup (robot/scan/insight/report → fleet_id).
//  2. RequirePermissionWithFleet(resource, action, opts ...FleetScopeOpt) factory —
//     기존 RequirePermission 보존 + body peek + ScopeResolver 호출 옵션 추가.
//  3. FleetScopeOpt 함수 옵션 패턴 — WithFleetFromBody / WithFleetFromResource.
//  4. body peek helper — 최대 10KB, fleetId 필드 추출, body 복원으로 핸들러 영향 0.
//
// design doc `docs/design/notes/rbac-fleet-scope-precision-design.md` §7 Stage 2 정확 일치.
// 결정 항목:
//   - D-RBACEX-1 권장 default A — middleware body peek + r.Body 복원.
//   - D-RBACEX-2 권장 default A — ScopeResolver interface + 도메인 repo wrap (구체 구현은 Stage 3).
//   - D-RBACEX-9 권장 default B — body peek 실패 시 빈 fleetID fallback (handler 별도 검증 위임).
//
// Stage 3에서 handlers.go 7 mutation endpoint를 본 factory로 교체합니다 — Stage 2는 인프라만.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/ssabro/rosshield/internal/platform/authz"
)

// bodyPeekLimit은 RequirePermissionWithFleet의 body peek 최대 바이트 수입니다.
//
// design doc §3.1.1 — POST /robots, /scans 모두 small JSON 요청체. 10KB는 fleetId·name·
// description·tags 등 합리적 상한 + DoS 회피. 한계 초과 시 빈 fleetID fallback (D-RBACEX-9).
const bodyPeekLimit = 10 * 1024

// errBodyOversize는 body peek이 10KB 한계를 넘었을 때 반환됩니다.
//
// 호출자(middleware)는 이 sentinel 또는 일반 error를 만나면 빈 fleetID로 fallback하여
// PDP 평가에 진입합니다.
var errBodyOversize = errors.New("rbac scope: body exceeds peek limit")

// ScopeResolver는 path param에서 추출한 도메인 리소스 ID로 fleet ID를 조회하는
// 평가용 thin lookup 인터페이스입니다.
//
// design doc §3.1.2 + D-RBACEX-2 권장 default A — middleware는 본 interface만 호출하고
// 실 lookup(robot/scan/insight/report repo)은 application bootstrap이 주입합니다.
// 도메인 경계 원칙 §5를 보존 — middleware가 도메인 repo를 직접 참조하지 않습니다.
//
// 본 stage 2는 interface 선언 + middleware 호출만. 구체 구현(sqlite repo wrap)은 Stage 3.
//
// 인자:
//   - ctx: request context (tenant·trace ID 등 propagation).
//   - resourceType: "robot" | "scan" | "insight" | "report" — opts에서 지정한 리소스 type.
//   - resourceID: path param 값 (예: robotId, sessionId).
//
// 반환:
//   - fleetID: 리소스가 속한 fleet ID. 빈 문자열이면 tenant 글로벌 / 미할당.
//   - err: lookup 실패 (not found, DB error 등). middleware는 D-RBACEX-9 정책에 따라
//     빈 fleetID로 PDP 평가에 진입 — fleet binding은 자동 거부.
type ScopeResolver interface {
	ResolveFleet(ctx context.Context, resourceType, resourceID string) (fleetID string, err error)
}

// fleetScopeConfig는 RequirePermissionWithFleet 내부 옵션 누적 상태입니다.
//
// opts 순서대로 시도 — 첫 non-empty fleetID 반환을 채택. 모두 실패하면 빈 fleetID로
// PDP 평가 진입 (D-RBACEX-9). chi URLParam("fleetID"|"fleetId") fallback은 opts 처리
// 이후 마지막 단계로 호출됩니다.
type fleetScopeConfig struct {
	extractors []fleetExtractor
}

// fleetExtractor는 request에서 fleetID를 추출하는 함수입니다.
//
// resolver는 cross-resource lookup에서 ScopeResolver를 호출하기 위한 의존성 전달용 —
// nil이면 resolver-기반 extractor는 빈 문자열 반환 (fail-closed).
type fleetExtractor func(r *http.Request, resolver ScopeResolver) (fleetID string)

// FleetScopeOpt는 RequirePermissionWithFleet의 함수 옵션 타입입니다.
//
// 사용 예:
//
//	h.RequirePermissionWithFleet(
//	    authz.ResourceRobot, authz.ActionWrite,
//	    WithFleetFromBody("fleetId"),
//	    WithFleetFromResource("robot", "robotId"),
//	)
//
// opts는 정의 순서대로 시도 — 첫 non-empty 결과 채택. 빈 결과면 다음 opt로 진행.
// opts 전부 빈 결과면 chi URLParam("fleetID"|"fleetId") fallback (기존 동작).
type FleetScopeOpt func(*fleetScopeConfig)

// WithFleetFromBody는 POST/PATCH JSON body에서 지정 필드(예: "fleetId")를 추출합니다.
//
// 동작 (D-RBACEX-1 권장 default A):
//   - r.Body를 io.LimitReader(10KB)로 읽어 buf 슬라이스에 보관.
//   - r.Body를 io.NopCloser(bytes.NewReader(buf))로 복원 — 핸들러가 동일 내용을 재파싱 가능.
//   - json.Unmarshal로 fleetID 필드만 추출 (struct{FleetID string `json:"<field>"`}).
//   - 파싱 실패 / 한계 초과 → 빈 fleetID 반환 (D-RBACEX-9 fallback).
//
// 본 opt는 GET·DELETE처럼 body 없는 메서드에도 안전 — http.NoBody면 빈 fleetID + body 무영향.
func WithFleetFromBody(field string) FleetScopeOpt {
	return func(cfg *fleetScopeConfig) {
		cfg.extractors = append(cfg.extractors, func(r *http.Request, _ ScopeResolver) string {
			id, err := peekFleetIDFromBody(r, field)
			if err != nil {
				// D-RBACEX-9 — 실패 시 빈 fleetID fallback. 핸들러에서 별도 body 검증.
				return ""
			}
			return id
		})
	}
}

// WithFleetFromResource는 chi URL param resourceID를 추출하여 ScopeResolver로 fleetID를
// 조회합니다.
//
// 동작 (D-RBACEX-2 권장 default A):
//   - chi.URLParam(r, paramName) → resourceID (예: "rbt_x").
//   - resourceID 비어 있음 → 빈 fleetID 반환 (lookup skip).
//   - ScopeResolver nil → 빈 fleetID 반환 (fail-closed — bootstrap이 주입하지 않으면 fleet binding 통과 X).
//   - resolver.ResolveFleet(ctx, resourceType, resourceID) 호출 — error면 빈 fleetID fallback (D-RBACEX-9).
//
// resourceType은 ScopeResolver 구현이 분기에 사용하는 문자열 (예: "robot", "scan",
// "insight", "report"). paramName은 chi URL param 이름 (예: "robotId", "sessionId").
func WithFleetFromResource(resourceType, paramName string) FleetScopeOpt {
	return func(cfg *fleetScopeConfig) {
		cfg.extractors = append(cfg.extractors, func(r *http.Request, resolver ScopeResolver) string {
			if resolver == nil {
				return ""
			}
			resourceID := chi.URLParam(r, paramName)
			if resourceID == "" {
				return ""
			}
			fleetID, err := resolver.ResolveFleet(r.Context(), resourceType, resourceID)
			if err != nil {
				// D-RBACEX-9 — lookup 실패 시 빈 fleetID fallback.
				return ""
			}
			return fleetID
		})
	}
}

// RequirePermissionWithFleet은 fleet scope 정밀 평가가 가능한 인가 미들웨어 factory입니다.
//
// design doc §7 Stage 2 산출 — 기존 RequirePermission(resource, action)을 보존하면서
// body peek / cross-resource lookup으로 Subject.FleetID를 추출하는 옵션을 추가합니다.
//
// fleetID 추출 우선순위:
//
//  1. chi.URLParam("fleetID"|"fleetId") — path에 fleetID가 직접 등장하면 항상 우선
//     (design doc §2.1 매트릭스 "path fleetId" 카테고리 2건 동작 보존).
//  2. opts 정의 순서대로 시도 — 첫 non-empty fleetID 채택 (body peek > resolver 등).
//  3. opts가 0개이거나 모두 빈 결과면 빈 문자열 — tenant 글로벌 요청으로 PDP 평가.
//     fleet binding만 가진 사용자는 자동 거부 (fleet 매칭 실패, D-RBACEX-9 정책 일관).
//
// PDP 결과:
//   - Allow → next.ServeHTTP.
//   - Deny → 403 + {"error":"forbidden","reason":<Decision.Reason>}.
//   - Decision.MatchedBindings는 ALLOW 시에만 채워지므로 본 응답은 Reason 문자열에 집중.
//     Reason에는 fleet=... 컨텍스트가 포함되어 cross-fleet 진단 가능 (D-RBACEX-4).
//
// 본 factory는 opts 0개로도 호출 가능 — extractor 없이 path-only fallback으로 동작.
// 이 경우 RequirePermission 과 동치 (호환 보존). Stage 3에서 7 mutation endpoint만
// 명시 opts와 함께 교체합니다.
func (h *Handlers) RequirePermissionWithFleet(resource authz.Resource, action authz.Action, opts ...FleetScopeOpt) func(http.Handler) http.Handler {
	cfg := &fleetScopeConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := claimsFromContext(r.Context())
			if !ok || claims.Subject == "" {
				writeError(w, http.StatusUnauthorized, "authentication required")
				return
			}

			fleetID := extractFleetID(r, h.deps.ScopeResolver, cfg.extractors)

			sub := authz.Subject{
				Bindings: bindingsForSubject(claims),
				FleetID:  fleetID,
			}

			d := authz.Decide(sub, resource, action)
			if d.Allow {
				next.ServeHTTP(w, r)
				return
			}

			// 403 응답 — Decision.Reason을 함께 반환해 디버깅·감사 로그 친화.
			// Reason 문자열에 fleet=... 컨텍스트 포함 (D-RBACEX-4) — explainability.
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":  "forbidden",
				"reason": d.Reason,
			})
		})
	}
}

// extractFleetID는 path → opts → 빈 문자열 순으로 fleetID를 추출합니다.
//
// 우선순위 (design doc §7 Stage 2 + user task):
//
//  1. chi.URLParam("fleetID"|"fleetId") — path에 fleetID가 직접 등장하면 항상 우선.
//     RequirePermission 호환 + design doc §2.1 매트릭스 "path fleetId" 카테고리(2건)
//     동작 보존.
//  2. opts 정의 순서대로 시도 — 첫 non-empty 채택 (body peek > resolver 등).
//  3. 모두 빈 결과 → 빈 문자열 반환. tenant 글로벌 요청으로 PDP 평가 — fleet binding만
//     가진 사용자는 자동 거부 (D-RBACEX-9 정책 일관).
//
// 본 함수는 RequirePermissionWithFleet 내부 헬퍼 — 외부 노출 없음.
func extractFleetID(r *http.Request, resolver ScopeResolver, extractors []fleetExtractor) string {
	if id := fleetIDFromRequest(r); id != "" {
		return id
	}
	for _, ext := range extractors {
		if id := ext(r, resolver); id != "" {
			return id
		}
	}
	return ""
}

// peekFleetIDFromBody는 r.Body의 처음 10KB만 읽어 JSON에서 field 값을 추출합니다.
//
// 본 helper의 invariant:
//   - 항상 r.Body를 복원 — 호출자(핸들러)가 동일 내용을 다시 ReadAll 가능.
//   - body가 nil 또는 http.NoBody → 빈 string + nil error (no-op).
//   - body가 10KB 초과 → 빈 string + errBodyOversize. body는 그대로 보존 (chunk 단위가
//     아닌 전체 body 보존 — io.MultiReader로 peek buf + 남은 body 결합).
//   - JSON 파싱 실패 → 빈 string + json error (호출자가 빈 fleetID fallback에 사용).
//
// JSON 안의 field 값이 string이 아니면 (예: number, object) 빈 string + nil 또는 error.
// 필드 부재 시 빈 string + nil — middleware는 다음 extractor로 진행 가능.
//
// 본 함수는 r.Body가 nil이거나 일찍 닫혀 있어도 panic을 일으키지 않습니다.
func peekFleetIDFromBody(r *http.Request, field string) (string, error) {
	if r == nil || r.Body == nil || r.Body == http.NoBody {
		return "", nil
	}

	// 10KB + 1 byte를 시도 — 1 byte 더 읽혔으면 oversize.
	limited := io.LimitReader(r.Body, bodyPeekLimit+1)
	peeked, readErr := io.ReadAll(limited)
	if readErr != nil {
		// 읽기 실패 — body 상태가 불확실하나 r.Body는 복원 필요. peeked 자체를 NopCloser로 둠.
		r.Body = io.NopCloser(bytes.NewReader(peeked))
		return "", readErr
	}

	if len(peeked) > bodyPeekLimit {
		// oversize — peek 거부, body는 peeked + r.Body 남은 부분으로 결합 복원.
		// io.MultiReader(bytes.NewReader(peeked), r.Body) — Close는 r.Body의 책임.
		r.Body = struct {
			io.Reader
			io.Closer
		}{
			Reader: io.MultiReader(bytes.NewReader(peeked), r.Body),
			Closer: r.Body,
		}
		return "", errBodyOversize
	}

	// 10KB 이내 — body 전체를 읽었으므로 복원은 bytes.Reader로 충분.
	r.Body = io.NopCloser(bytes.NewReader(peeked))

	if len(peeked) == 0 {
		return "", nil
	}

	// json.NewDecoder + struct — 필드 한 개만 추출 (D-RBACEX-1).
	// json.Unmarshal로도 충분하지만 NewDecoder가 stream 패턴 일관.
	var holder map[string]json.RawMessage
	if err := json.Unmarshal(peeked, &holder); err != nil {
		return "", err
	}
	raw, ok := holder[field]
	if !ok {
		return "", nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		// 필드는 있지만 string이 아님 (예: number). 빈 string + nil — 호출자는 fallback.
		return "", nil
	}
	return s, nil
}
