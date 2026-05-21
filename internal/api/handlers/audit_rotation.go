package handlers

// audit_rotation.go — Phase 10.D-6 emergency override.
//
// POST /api/v1/audit/rotation/abort — 진행 중 audit chain signer key rotation 을 abort
// 또는 다음 rotation 1회 차단 + audit.chain.rotation_aborted event emit.
//
// 권한: ResourceTenantAdmin, ActionAdmin (다른 destructive ops 일관).
// 호출자: 운영자 (web admin 또는 rosshield audit rotation abort CLI).
//
// 응답 (200 OK):
//
//	{
//	  "aborted":       true,
//	  "auditEntryId":  <int64>,   # emit 된 audit_entries.seq (KeyRotator nil 가드 통과 시 항상 >0)
//	  "abortedAt":     "<iso8601 UTC>",
//	  "previousEpoch": <int64>,   # abort 시점 SwappableSigner.CurrentEpoch
//	  "reason":        "<echo>"   # 운영자 입력 echo (감사 trace UI 용)
//	}
//
// KeyRotator 미주입(deps.KeyRotator == nil) 이면 503 Service Unavailable.

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/ssabro/rosshield/internal/domain/audit/keyrotation"
)

// abortRotationRequest 는 POST /api/v1/audit/rotation/abort 요청 본문입니다.
type abortRotationRequest struct {
	Reason string `json:"reason"`
}

// abortRotationResponse 는 응답 본문입니다.
type abortRotationResponse struct {
	Aborted       bool   `json:"aborted"`
	AuditEntryID  int64  `json:"auditEntryId"`
	AbortedAt     string `json:"abortedAt"`
	PreviousEpoch int64  `json:"previousEpoch"`
	Reason        string `json:"reason"`
}

// AbortAuditRotation 는 POST /api/v1/audit/rotation/abort 핸들러입니다 (Phase 10.D-6).
//
// 호출자가 admin 권한 보유자 (RequirePermission middleware 통과) 인 상태에서만 진입.
// body parse → KeyRotator.Abort → response. follower 시 503 (NOT_LEADER).
func (h *Handlers) AbortAuditRotation(w http.ResponseWriter, r *http.Request) {
	if h.deps.KeyRotator == nil {
		writeError(w, http.StatusServiceUnavailable, "audit chain key rotation not configured")
		return
	}

	var req abortRotationRequest
	if r.ContentLength > 0 || r.Body != http.NoBody {
		// body 가 있을 때만 decode 시도 — empty body 도 허용 (reason 비어 있으면 default).
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, errEmptyBody) {
			// EOF 는 empty body — silent ignore.
			if err.Error() != "EOF" {
				writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
				return
			}
		}
	}

	// actor 식별 — AuthMiddleware 가 ctx 에 sub claim 을 넣는 패턴이 도메인마다 다름.
	// 본 endpoint 는 reason 자체에 운영자 메모 + audit Actor 는 system 으로 두고
	// payload 의 actor 필드에 admin user id 직렬화 (간단화). 향후 확장 가능.
	actor := extractUserSubject(r)

	result, err := h.deps.KeyRotator.Abort(r.Context(), req.Reason, actor)
	if err != nil {
		if errors.Is(err, keyrotation.ErrNotLeader) {
			writeError(w, http.StatusServiceUnavailable, "instance is not leader")
			return
		}
		writeError(w, http.StatusInternalServerError, "abort failed: "+err.Error())
		return
	}

	resp := abortRotationResponse{
		Aborted:       true,
		AuditEntryID:  result.AuditEntryID,
		AbortedAt:     result.AbortedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"),
		PreviousEpoch: result.PreviousEpoch,
		Reason:        req.Reason,
	}
	writeJSON(w, http.StatusOK, resp)
}

// errEmptyBody 는 sentinel — 본 패키지 내부 분기용.
var errEmptyBody = errors.New("handlers: empty body")

// extractUserSubject 는 ctx 의 access claim 에서 user subject 를 추출하는 helper 입니다.
//
// AuthMiddleware 가 claim 을 ctx 에 주입한 형태가 도메인마다 달라 일관 fallback "system" 으로
// 처리. 향후 별 stage 에서 claim 추출 헬퍼 통합 시 본 함수 갱신.
func extractUserSubject(r *http.Request) string {
	if v := r.Header.Get("X-Rosshield-Actor"); v != "" {
		return v
	}
	return "system"
}
