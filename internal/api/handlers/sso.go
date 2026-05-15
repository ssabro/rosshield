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
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/ssabro/rosshield/internal/domain/audit"
	"github.com/ssabro/rosshield/internal/domain/tenant"
	"github.com/ssabro/rosshield/internal/domain/tenant/sso"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// startSSOLoginResponse는 GET /auth/sso/{providerId}/login 응답입니다.
//
// 본 E20-A scaffold는 IdP 호출 전이라 redirectUrl은 빈 값 — 후속 stage에서
// 302 redirect로 응답 형식이 바뀔 수 있음(클라이언트 호환성 검토 필요).
//
// E20-B 정책:
//
//	OIDC client가 주입돼 있고 result.AuthURL이 채워지면 302 redirect로 응답 — JSON 미사용.
//	OIDC client 미주입(stub) 또는 SAML이면 본 JSON으로 응답.
type startSSOLoginResponse struct {
	State       string `json:"state"`
	RedirectURL string `json:"redirectUrl,omitempty"`
	ProviderID  string `json:"providerId"`
	Stub        bool   `json:"stub,omitempty"` // 본 stage scaffold임을 명시
}

// ssoCallbackResponse는 OIDC callback 응답입니다.
//
// E20-B: 외부 identity의 sub/email을 노출 — 이후 token issue·user 매핑은 후속 stage(E20-D)에서
// 본 응답에 access_token/refresh_token 추가 예정.
type ssoCallbackResponse struct {
	State   string `json:"state"`
	Subject string `json:"subject,omitempty"`
	Email   string `json:"email,omitempty"`
	UserID  string `json:"userId,omitempty"`
	Stub    bool   `json:"stub,omitempty"`
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
	// E20-B — OIDC client가 주입되어 AuthURL이 채워지면 302 redirect.
	if result.AuthURL != "" {
		http.Redirect(w, r, result.AuthURL, http.StatusFound)
		return
	}
	// stub 흐름 (E20-A 호환 또는 SAML 미구현 단계).
	writeJSON(w, http.StatusOK, startSSOLoginResponse{
		State:       result.State,
		RedirectURL: result.AuthURL,
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
//  3. RBAC fleet 정밀화 Stage 5 — IdP groups claim → ResolvedBinding 셋 →
//     user_roles.source='sso' sync (revoke + INSERT) + audit 'user_role.synced' emit.
//  4. token 교환·user 매핑·access/refresh 발급은 본 stage 범위 외 → 200 stub.
//
// SSO group sync (Stage 5 — design doc §6.3 + §7 Stage 5):
//
//	Identity.UserID 채워졌고(ProvisionExternalUser 성공) deps.SSOGroupMapping 주입돼 있으면
//	동일 Tx에서 group sync 수행 — IdP 응답 source가 진실의 원천. source='manual' 보존
//	(D-RBACEX-7).
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

	_ = providerID // path scope는 sso.Service가 state로 attempt를 lookup하므로 직접 미사용 (result.ProviderID 사용).

	var result sso.CompleteLoginResult
	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		out, e := h.deps.SSO.CompleteLogin(ctx, tx, sso.CompleteLoginRequest{
			State: state,
			Code:  code,
		})
		if e != nil {
			return e
		}
		result = out
		// RBAC fleet 정밀화 Stage 5 — IdP groups → user_roles.source='sso' sync.
		// Identity.UserID 채워져야(ProvisionExternalUser 성공) sync 진입 — 빈 UserID는 stub 흐름.
		if h.deps.SSOGroupMapping != nil && result.Identity.UserID != "" && result.ProviderID != "" {
			if err := h.syncSSOGroupBindings(ctx, tx, result.Identity.UserID, result.ProviderID, result.Groups); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		writeError(w, ssoErrorStatus(err), err.Error())
		return
	}
	resp := ssoCallbackResponse{
		State:   state,
		Subject: result.Identity.ExternalSubject,
		Email:   result.Identity.Email,
		UserID:  result.Identity.UserID,
	}
	if result.Identity.ExternalSubject == "" {
		resp.Stub = true // OIDC client 미주입 — E20-A 호환 흐름.
	}
	writeJSON(w, http.StatusOK, resp)
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
	_ = providerID // result.ProviderID 사용 — RBAC fleet 정밀화 Stage 5.

	var result sso.CompleteLoginResult
	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		out, e := h.deps.SSO.CompleteLogin(ctx, tx, sso.CompleteLoginRequest{
			State:        relayState, // SAML은 state를 RelayState로 운반
			SAMLResponse: samlResp,
		})
		if e != nil {
			return e
		}
		result = out
		// RBAC fleet 정밀화 Stage 5 — SAML attribute groups → user_roles.source='sso' sync.
		if h.deps.SSOGroupMapping != nil && result.Identity.UserID != "" && result.ProviderID != "" {
			if err := h.syncSSOGroupBindings(ctx, tx, result.Identity.UserID, result.ProviderID, result.Groups); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		writeError(w, ssoErrorStatus(err), err.Error())
		return
	}
	resp := ssoCallbackResponse{
		State:   relayState,
		Subject: result.Identity.ExternalSubject,
		Email:   result.Identity.Email,
		UserID:  result.Identity.UserID,
	}
	if result.Identity.ExternalSubject == "" {
		resp.Stub = true
	}
	writeJSON(w, http.StatusOK, resp)
}

// === E20-D — Provider CRUD HTTP 표면 ===

// providerView는 Provider의 클라이언트 응답 형태입니다.
//
// Config는 raw JSON으로 그대로 노출 (UI가 Type별 스키마 파싱).
type providerView struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	Name      string          `json:"name"`
	Enabled   bool            `json:"enabled"`
	Config    json.RawMessage `json:"config"`
	CreatedAt string          `json:"createdAt"`
	UpdatedAt string          `json:"updatedAt"`
}

func toProviderView(p sso.Provider) providerView {
	return providerView{
		ID:        p.ID,
		Type:      string(p.Type),
		Name:      p.Name,
		Enabled:   p.Enabled,
		Config:    p.Config,
		CreatedAt: p.CreatedAt.Format("2006-01-02T15:04:05.999999999Z07:00"),
		UpdatedAt: p.UpdatedAt.Format("2006-01-02T15:04:05.999999999Z07:00"),
	}
}

// listProvidersResponse는 GET /sso/providers 응답입니다.
type listProvidersResponse struct {
	Providers []providerView `json:"providers"`
}

// CreateSSOProvider는 POST /api/v1/sso/providers 핸들러입니다.
//
// body: {"type":"oidc"|"saml","name":"...","enabled":bool,"config":{...}}
// 반환: 201 + Provider view, 400/401/409.
func (h *Handlers) CreateSSOProvider(w http.ResponseWriter, r *http.Request) {
	if storage.TenantIDFromContext(r.Context()) == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}
	if h.deps.SSO == nil {
		writeError(w, http.StatusServiceUnavailable, "sso: service not configured")
		return
	}
	var body struct {
		Type    string          `json:"type"`
		Name    string          `json:"name"`
		Enabled bool            `json:"enabled"`
		Config  json.RawMessage `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	tenantID := storage.TenantIDFromContext(r.Context())
	var created sso.Provider
	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		out, e := h.deps.SSO.CreateProvider(ctx, tx, sso.CreateProviderRequest{
			TenantID: tenantID,
			Type:     sso.Type(body.Type),
			Name:     body.Name,
			Enabled:  body.Enabled,
			Config:   body.Config,
		})
		if e != nil {
			return e
		}
		created = out
		return nil
	})
	if err != nil {
		writeError(w, ssoErrorStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, toProviderView(created))
}

// ListSSOProviders는 GET /api/v1/sso/providers 핸들러입니다.
func (h *Handlers) ListSSOProviders(w http.ResponseWriter, r *http.Request) {
	if storage.TenantIDFromContext(r.Context()) == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}
	if h.deps.SSO == nil {
		writeError(w, http.StatusServiceUnavailable, "sso: service not configured")
		return
	}
	var providers []sso.Provider
	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		out, e := h.deps.SSO.ListProviders(ctx, tx)
		if e != nil {
			return e
		}
		providers = out
		return nil
	})
	if err != nil {
		writeError(w, ssoErrorStatus(err), err.Error())
		return
	}
	views := make([]providerView, 0, len(providers))
	for _, p := range providers {
		views = append(views, toProviderView(p))
	}
	writeJSON(w, http.StatusOK, listProvidersResponse{Providers: views})
}

// GetSSOProvider는 GET /api/v1/sso/providers/{providerId} 핸들러입니다.
func (h *Handlers) GetSSOProvider(w http.ResponseWriter, r *http.Request, providerID string) {
	if storage.TenantIDFromContext(r.Context()) == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}
	if h.deps.SSO == nil {
		writeError(w, http.StatusServiceUnavailable, "sso: service not configured")
		return
	}
	var p sso.Provider
	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		out, e := h.deps.SSO.GetProvider(ctx, tx, providerID)
		if e != nil {
			return e
		}
		p = out
		return nil
	})
	if err != nil {
		writeError(w, ssoErrorStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toProviderView(p))
}

// UpdateSSOProvider는 PUT /api/v1/sso/providers/{providerId} 핸들러입니다.
//
// body: {"name":"...?","enabled":bool?,"config":{...}?} — 모두 옵션, nil이면 변경 없음.
func (h *Handlers) UpdateSSOProvider(w http.ResponseWriter, r *http.Request, providerID string) {
	if storage.TenantIDFromContext(r.Context()) == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}
	if h.deps.SSO == nil {
		writeError(w, http.StatusServiceUnavailable, "sso: service not configured")
		return
	}
	var body struct {
		Name    *string         `json:"name"`
		Enabled *bool           `json:"enabled"`
		Config  json.RawMessage `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	tenantID := storage.TenantIDFromContext(r.Context())
	var updated sso.Provider
	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		out, e := h.deps.SSO.UpdateProvider(ctx, tx, sso.UpdateProviderRequest{
			ID:       providerID,
			TenantID: tenantID,
			Name:     body.Name,
			Enabled:  body.Enabled,
			Config:   body.Config,
		})
		if e != nil {
			return e
		}
		updated = out
		return nil
	})
	if err != nil {
		writeError(w, ssoErrorStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toProviderView(updated))
}

// DeleteSSOProvider는 DELETE /api/v1/sso/providers/{providerId} 핸들러입니다.
//
// hard delete + audit emit. 404 시 ErrProviderNotFound.
func (h *Handlers) DeleteSSOProvider(w http.ResponseWriter, r *http.Request, providerID string) {
	if storage.TenantIDFromContext(r.Context()) == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}
	if h.deps.SSO == nil {
		writeError(w, http.StatusServiceUnavailable, "sso: service not configured")
		return
	}
	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		return h.deps.SSO.DeleteProvider(ctx, tx, providerID)
	})
	if err != nil {
		writeError(w, ssoErrorStatus(err), err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ssoErrorStatus는 sso 도메인 sentinel을 HTTP status로 매핑합니다.
//
// E20-A 매핑:
//
//	ErrProviderNotFound      → 404
//	ErrProviderDisabled      → 403 (정책상 사용 불가 — R20-2 ENT 게이트)
//	ErrProviderNameExists    → 409
//	ErrInvalidState          → 400 (CSRF/재사용 의심)
//	ErrStateExpired          → 400
//	ErrIdPMismatch           → 400
//	ErrUnsupportedType       → 400
//	ErrEmptyName/Config/...  → 400
//
// E20-B 추가:
//
//	ErrInvalidOIDCConfig     → 500 (서버 misconfig)
//	ErrInvalidOIDCArgs       → 400
//	ErrIdPHTTP               → 502 (외부 IdP HTTP 실패)
//	ErrIDTokenInvalid        → 400 (id_token 검증 실패)
//	ErrNonceMismatch         → 400
//	ErrUnsupportedAlg        → 400
//	ErrJWKNotFound           → 502 (IdP가 키 회전 미알림)
func ssoErrorStatus(err error) int {
	switch {
	case errors.Is(err, sso.ErrProviderNotFound):
		return http.StatusNotFound
	case errors.Is(err, sso.ErrProviderDisabled):
		return http.StatusForbidden
	case errors.Is(err, sso.ErrProviderNameExists):
		return http.StatusConflict
	case errors.Is(err, sso.ErrIdPHTTP),
		errors.Is(err, sso.ErrJWKNotFound):
		return http.StatusBadGateway
	case errors.Is(err, sso.ErrInvalidOIDCConfig),
		errors.Is(err, sso.ErrInvalidSAMLConfig):
		return http.StatusInternalServerError
	case errors.Is(err, sso.ErrSAMLInvalid):
		return http.StatusBadRequest
	case errors.Is(err, sso.ErrInvalidState),
		errors.Is(err, sso.ErrStateExpired),
		errors.Is(err, sso.ErrIdPMismatch),
		errors.Is(err, sso.ErrUnsupportedType),
		errors.Is(err, sso.ErrEmptyName),
		errors.Is(err, sso.ErrEmptyConfig),
		errors.Is(err, sso.ErrEmptyState),
		errors.Is(err, sso.ErrEmptySubject),
		errors.Is(err, sso.ErrInvalidOIDCArgs),
		errors.Is(err, sso.ErrIDTokenInvalid),
		errors.Is(err, sso.ErrNonceMismatch),
		errors.Is(err, sso.ErrUnsupportedAlg),
		errors.Is(err, sso.ErrEmptyGroupValue),
		errors.Is(err, sso.ErrEmptyRoleID),
		errors.Is(err, sso.ErrInvalidScopeType),
		errors.Is(err, sso.ErrEmptyScopeIDForFleet):
		return http.StatusBadRequest
	case errors.Is(err, sso.ErrGroupMappingNotFound):
		return http.StatusNotFound
	case errors.Is(err, sso.ErrGroupMappingExists):
		return http.StatusConflict
	case errors.Is(err, sso.ErrRoleNotFoundForTenant):
		return http.StatusBadRequest
	default:
		return errorStatusFor(err)
	}
}

// === RBAC fleet 정밀화 Stage 5 — SSO group → role 자동 매핑 결선 ===

// syncSSOGroupBindings는 SSO callback 흐름의 핵심 sync 알고리즘입니다 (D-RBACEX-7 권장 default).
//
// 절차:
//
//  1. GroupMappingService.ResolveBindingsForGroups(providerID, groups) → ResolvedBinding 셋.
//  2. tenant.Service.RevokeUserRoleBindingsBySource(userID, BindingSourceSSO) — 기존 자동
//     binding 모두 revoke (IdP가 진실의 원천).
//  3. for _, rb := range resolved: tenant.Service.AssignRoleScoped(..., BindingSourceSSO).
//  4. audit.Service.Append('user_role.synced') — provider/groups/before/after 명세.
//
// source='manual' admin 수동 binding은 영향 없음 (D-RBACEX-7 분리 정책).
// 빈 groups는 "이 사용자는 어떤 자동 binding도 받지 않음" — sso 모두 revoke 후 INSERT 0건.
//
// 본 sync는 SSO callback Tx 안에서 호출 — IdP 응답 검증 + user 매핑 + role sync 원자적.
func (h *Handlers) syncSSOGroupBindings(ctx context.Context, tx storage.Tx, userID, providerID string, groups []string) error {
	// 1. group → ResolvedBinding 셋 (도메인 layer에서 dedupe 보장).
	resolved, err := h.deps.SSOGroupMapping.ResolveBindingsForGroups(ctx, tx, providerID, groups)
	if err != nil {
		return fmt.Errorf("sso: resolve group bindings: %w", err)
	}

	// 2. 기존 source='sso' 모두 revoke.
	revokedCount, err := h.deps.Tenant.RevokeUserRoleBindingsBySource(ctx, tx, userID, tenant.BindingSourceSSO)
	if err != nil {
		return fmt.Errorf("sso: revoke prior sso bindings: %w", err)
	}

	// 3. 새 binding INSERT (source='sso').
	addedCount := 0
	for _, rb := range resolved {
		scopeType := tenant.ScopeType(rb.ScopeType)
		if scopeType == "" {
			scopeType = tenant.ScopeTenant
		}
		if err := h.deps.Tenant.AssignRoleScoped(ctx, tx, userID, rb.RoleID,
			scopeType, rb.ScopeID, tenant.BindingSourceSSO); err != nil {
			return fmt.Errorf("sso: assign sso binding role=%s: %w", rb.RoleID, err)
		}
		addedCount++
	}

	// 4. audit emit ('user_role.synced' — design doc §6 권장 단일 kind).
	if h.deps.Audit != nil {
		payload := buildSSOSyncAuditPayload(providerID, groups, resolved, revokedCount, addedCount)
		_, err := h.deps.Audit.Append(ctx, tx, audit.AppendRequest{
			TenantID: tx.TenantID(),
			Actor:    audit.Actor{Type: audit.ActorSystem, ID: "system"},
			Action:   "user_role.synced",
			Target:   audit.Target{Type: "user", ID: userID},
			Payload:  payload,
			Outcome:  audit.OutcomeSuccess,
		})
		if err != nil {
			return fmt.Errorf("sso: audit append user_role.synced: %w", err)
		}
	}
	return nil
}

// buildSSOSyncAuditPayload는 user_role.synced audit payload(canonical JSON)를 빌드합니다.
//
// payload 형식:
//
//	{
//	  "providerId":  "...",
//	  "groups":      ["g1","g2"],   // IdP claim 추출값 (sorted, dedup)
//	  "bindings":    [{"roleId":"...","scopeType":"...","scopeId":"..."}],
//	  "revokedCount": N,             // 매 sync에서 삭제된 source='sso' row 수
//	  "addedCount":   M              // 매 sync에서 INSERT된 source='sso' row 수 (멱등 PK 충돌 시 같은 수치)
//	}
//
// 감사인 explainability 친화 — 각 sync에서 어떤 group/role/scope가 적용됐는지 audit chain에 보존.
func buildSSOSyncAuditPayload(providerID string, groups []string, bindings []sso.ResolvedBinding, revokedCount, addedCount int) []byte {
	// groups dedup + sort — 결정론 audit payload.
	gset := make(map[string]struct{}, len(groups))
	for _, g := range groups {
		s := strings.TrimSpace(g)
		if s != "" {
			gset[s] = struct{}{}
		}
	}
	gsorted := make([]string, 0, len(gset))
	for g := range gset {
		gsorted = append(gsorted, g)
	}
	sort.Strings(gsorted)

	// bindings → 직렬화 (sort by roleId/scopeId for 결정론).
	type bv struct {
		RoleID    string `json:"roleId"`
		ScopeType string `json:"scopeType"`
		ScopeID   string `json:"scopeId"`
	}
	bvs := make([]bv, 0, len(bindings))
	for _, rb := range bindings {
		bvs = append(bvs, bv{RoleID: rb.RoleID, ScopeType: rb.ScopeType, ScopeID: rb.ScopeID})
	}
	sort.Slice(bvs, func(i, j int) bool {
		if bvs[i].RoleID != bvs[j].RoleID {
			return bvs[i].RoleID < bvs[j].RoleID
		}
		if bvs[i].ScopeType != bvs[j].ScopeType {
			return bvs[i].ScopeType < bvs[j].ScopeType
		}
		return bvs[i].ScopeID < bvs[j].ScopeID
	})

	out := struct {
		ProviderID   string   `json:"providerId"`
		Groups       []string `json:"groups"`
		Bindings     []bv     `json:"bindings"`
		RevokedCount int      `json:"revokedCount"`
		AddedCount   int      `json:"addedCount"`
	}{
		ProviderID:   providerID,
		Groups:       gsorted,
		Bindings:     bvs,
		RevokedCount: revokedCount,
		AddedCount:   addedCount,
	}
	b, _ := json.Marshal(out)
	return b
}

// === RBAC fleet 정밀화 Stage 5 — Group Mapping CRUD HTTP 핸들러 ===

// groupMappingView는 GroupRoleMapping의 클라이언트 응답 형태입니다.
type groupMappingView struct {
	ID         string `json:"id"`
	ProviderID string `json:"providerId"`
	GroupValue string `json:"groupValue"`
	RoleID     string `json:"roleId"`
	ScopeType  string `json:"scopeType"`
	ScopeID    string `json:"scopeId,omitempty"`
	CreatedAt  string `json:"createdAt"`
}

func toGroupMappingView(m sso.GroupRoleMapping) groupMappingView {
	return groupMappingView{
		ID:         m.ID,
		ProviderID: m.ProviderID,
		GroupValue: m.GroupValue,
		RoleID:     m.RoleID,
		ScopeType:  m.ScopeType,
		ScopeID:    m.ScopeID,
		CreatedAt:  m.CreatedAt.Format("2006-01-02T15:04:05.999999999Z07:00"),
	}
}

// listGroupMappingsResponse는 GET /sso/providers/{providerId}/group-mappings 응답입니다.
type listGroupMappingsResponse struct {
	Mappings []groupMappingView `json:"mappings"`
}

// ListSSOGroupMappings는 GET /api/v1/sso/providers/{providerId}/group-mappings 핸들러입니다.
//
// provider tenant 격리 — cross-tenant lookup은 404로 마스킹.
func (h *Handlers) ListSSOGroupMappings(w http.ResponseWriter, r *http.Request, providerID string) {
	if storage.TenantIDFromContext(r.Context()) == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}
	if h.deps.SSOGroupMapping == nil {
		writeError(w, http.StatusServiceUnavailable, "sso: group mapping service not configured")
		return
	}
	var mappings []sso.GroupRoleMapping
	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		out, e := h.deps.SSOGroupMapping.ListGroupMappings(ctx, tx, providerID)
		if e != nil {
			return e
		}
		mappings = out
		return nil
	})
	if err != nil {
		writeError(w, ssoErrorStatus(err), err.Error())
		return
	}
	views := make([]groupMappingView, 0, len(mappings))
	for _, m := range mappings {
		views = append(views, toGroupMappingView(m))
	}
	writeJSON(w, http.StatusOK, listGroupMappingsResponse{Mappings: views})
}

// CreateSSOGroupMapping은 POST /api/v1/sso/providers/{providerId}/group-mappings 핸들러입니다.
//
// body: {"groupValue":"...","roleId":"...","scopeType":"tenant"|"fleet","scopeId":"..."}
// scopeType 빈 값 → 'tenant' default. scopeType='fleet'이면 scopeId 필수.
func (h *Handlers) CreateSSOGroupMapping(w http.ResponseWriter, r *http.Request, providerID string) {
	if storage.TenantIDFromContext(r.Context()) == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}
	if h.deps.SSOGroupMapping == nil {
		writeError(w, http.StatusServiceUnavailable, "sso: group mapping service not configured")
		return
	}
	var body struct {
		GroupValue string `json:"groupValue"`
		RoleID     string `json:"roleId"`
		ScopeType  string `json:"scopeType"`
		ScopeID    string `json:"scopeId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	var created sso.GroupRoleMapping
	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		out, e := h.deps.SSOGroupMapping.CreateGroupMapping(ctx, tx, sso.CreateGroupMappingRequest{
			ProviderID: providerID,
			GroupValue: body.GroupValue,
			RoleID:     body.RoleID,
			ScopeType:  body.ScopeType,
			ScopeID:    body.ScopeID,
		})
		if e != nil {
			return e
		}
		created = out
		return nil
	})
	if err != nil {
		writeError(w, ssoErrorStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, toGroupMappingView(created))
}

// DeleteSSOGroupMapping은 DELETE /api/v1/sso/providers/{providerId}/group-mappings/{mappingId} 핸들러입니다.
//
// providerId path는 라우팅 일관성용 — 실 격리는 mappingId + tenant scope.
func (h *Handlers) DeleteSSOGroupMapping(w http.ResponseWriter, r *http.Request) {
	if storage.TenantIDFromContext(r.Context()) == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}
	if h.deps.SSOGroupMapping == nil {
		writeError(w, http.StatusServiceUnavailable, "sso: group mapping service not configured")
		return
	}
	mappingID := chi.URLParam(r, "mappingId")
	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		return h.deps.SSOGroupMapping.DeleteGroupMapping(ctx, tx, mappingID)
	})
	if err != nil {
		writeError(w, ssoErrorStatus(err), err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
