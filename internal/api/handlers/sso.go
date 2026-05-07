package handlers

// sso.go — SSO HTTP 표면 scaffold (E20-A).
//
// 본 stage 범위:
//
//	scaffold만 — 라우팅 + sso.Service interface 결선 자리만. 실 IdP 호출(OIDC token exchange,
//	SAML assertion 검증)은 후속 stage(E20-B/C)에서 본 핸들러 본문 채움.
//
// 엔드포인트 3종:
//
//	GET  /api/v1/auth/sso/{providerId}/login         → StartSSOLogin
//	GET  /api/v1/auth/sso/{providerId}/callback      → CompleteSSOLoginOIDC (OIDC code + state)
//	POST /api/v1/auth/sso/{providerId}/saml/acs      → CompleteSSOLoginSAML (SAML POST binding)
//
// 옵트인 (P10):
//
//	deps.SSO == nil → 503. R20-2 enterprise 게이트는 후속 stage(E24)에서 라이선스 검증.

import (
	"context"
	"errors"
	"net/http"

	"github.com/ssabro/rosshield/internal/domain/tenant/sso"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// startSSOLoginResponse는 GET /auth/sso/{providerId}/login 응답입니다.
//
// 본 E20-A scaffold는 IdP 호출 전이라 redirectUrl은 빈 값 — 후속 stage에서
// 302 redirect로 응답 형식이 바뀔 수 있음(클라이언트 호환성 검토 필요).
type startSSOLoginResponse struct {
	State       string `json:"state"`
	RedirectURL string `json:"redirectUrl,omitempty"`
	ProviderID  string `json:"providerId"`
	Stub        bool   `json:"stub,omitempty"` // 본 stage scaffold임을 명시
}

// ssoCallbackResponse는 OIDC callback의 stub 응답입니다.
type ssoCallbackResponse struct {
	State string `json:"state"`
	Stub  bool   `json:"stub"`
}

// StartSSOLogin은 GET /api/v1/auth/sso/{providerId}/login 핸들러입니다.
//
// 본 stage scaffold:
//
//  1. providerId 추출 + tenant ctx 확인.
//  2. sso.Service.StartLogin 호출 — state·PKCE·nonce·RelayState 영속.
//  3. redirectUrl은 빈 값(stub) + state 반환 — 클라이언트가 임의 처리.
//
// 후속 stage(E20-B/C):
//
//	StartLogin이 IdP authorization endpoint URL을 빌드 → 302 redirect로 변경.
//	audit hook은 sso.Service 안에서 emit.
func (h *Handlers) StartSSOLogin(w http.ResponseWriter, r *http.Request, providerID string) {
	if storage.TenantIDFromContext(r.Context()) == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}
	if h.deps.SSO == nil {
		writeError(w, http.StatusServiceUnavailable, "sso: service not configured (E20-A scaffold)")
		return
	}

	redirectAfter := r.URL.Query().Get("redirectAfter")

	var result sso.StartLoginResult
	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		out, e := h.deps.SSO.StartLogin(ctx, tx, sso.StartLoginRequest{
			ProviderID:    providerID,
			RedirectAfter: redirectAfter,
		})
		if e != nil {
			return e
		}
		result = out
		return nil
	})
	if err != nil {
		writeError(w, ssoErrorStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, startSSOLoginResponse{
		State:       result.State,
		RedirectURL: result.AuthURL, // 본 stage는 빈 값
		ProviderID:  providerID,
		Stub:        true,
	})
}

// CompleteSSOLoginOIDC는 GET /api/v1/auth/sso/{providerId}/callback 핸들러입니다 (OIDC).
//
// 본 stage scaffold:
//
//  1. query string에서 state·code 추출.
//  2. sso.Service.CompleteLogin 호출 — state 검증 + 만료/재사용 체크 + completed_at 마킹.
//  3. token 교환·user 매핑·access/refresh 발급은 본 stage 범위 외 → 200 stub.
//
// 후속 stage(E20-B):
//
//	IdP token endpoint POST → id_token 검증 → external_subject·email 추출 →
//	UpsertExternalIdentity → tenant.Service.IssueTokensForExternal(가칭) → cookie set.
func (h *Handlers) CompleteSSOLoginOIDC(w http.ResponseWriter, r *http.Request, providerID string) {
	if storage.TenantIDFromContext(r.Context()) == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}
	if h.deps.SSO == nil {
		writeError(w, http.StatusServiceUnavailable, "sso: service not configured (E20-A scaffold)")
		return
	}
	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")
	_ = providerID // E20-B에서 provider type 분기 시 사용 (OIDC vs SAML)

	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		_, e := h.deps.SSO.CompleteLogin(ctx, tx, sso.CompleteLoginRequest{
			State: state,
			Code:  code,
		})
		return e
	})
	if err != nil {
		writeError(w, ssoErrorStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ssoCallbackResponse{State: state, Stub: true})
}

// CompleteSSOLoginSAML은 POST /api/v1/auth/sso/{providerId}/saml/acs 핸들러입니다 (SAML POST binding).
//
// 본 stage scaffold:
//
//  1. application/x-www-form-urlencoded 파싱 (SAMLResponse + RelayState).
//  2. sso.Service.CompleteLogin 호출 — state(=RelayState) 검증.
//  3. assertion XML 서명 검증·NameID 추출은 본 stage 범위 외 → 200 stub.
//
// 후속 stage(E20-C):
//
//	gosaml2 등 라이브러리로 assertion verify → NameID·attribute 추출 → 사용자 매핑.
func (h *Handlers) CompleteSSOLoginSAML(w http.ResponseWriter, r *http.Request, providerID string) {
	if storage.TenantIDFromContext(r.Context()) == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}
	if h.deps.SSO == nil {
		writeError(w, http.StatusServiceUnavailable, "sso: service not configured (E20-A scaffold)")
		return
	}
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form body")
		return
	}
	samlResp := r.PostForm.Get("SAMLResponse")
	relayState := r.PostForm.Get("RelayState")
	_ = providerID

	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		_, e := h.deps.SSO.CompleteLogin(ctx, tx, sso.CompleteLoginRequest{
			State:        relayState, // SAML은 state를 RelayState로 운반
			SAMLResponse: samlResp,
		})
		return e
	})
	if err != nil {
		writeError(w, ssoErrorStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ssoCallbackResponse{State: relayState, Stub: true})
}

// ssoErrorStatus는 sso 도메인 sentinel을 HTTP status로 매핑합니다.
//
//	ErrProviderNotFound      → 404
//	ErrProviderDisabled      → 409 (정책상 사용 불가)
//	ErrProviderNameExists    → 409
//	ErrInvalidState          → 400 (CSRF/재사용 의심)
//	ErrStateExpired          → 400
//	ErrIdPMismatch           → 400
//	ErrUnsupportedType       → 400
//	ErrEmptyName/Config/...  → 400
func ssoErrorStatus(err error) int {
	switch {
	case errors.Is(err, sso.ErrProviderNotFound):
		return http.StatusNotFound
	case errors.Is(err, sso.ErrProviderDisabled),
		errors.Is(err, sso.ErrProviderNameExists):
		return http.StatusConflict
	case errors.Is(err, sso.ErrInvalidState),
		errors.Is(err, sso.ErrStateExpired),
		errors.Is(err, sso.ErrIdPMismatch),
		errors.Is(err, sso.ErrUnsupportedType),
		errors.Is(err, sso.ErrEmptyName),
		errors.Is(err, sso.ErrEmptyConfig),
		errors.Is(err, sso.ErrEmptyState),
		errors.Is(err, sso.ErrEmptySubject):
		return http.StatusBadRequest
	default:
		return errorStatusFor(err)
	}
}
