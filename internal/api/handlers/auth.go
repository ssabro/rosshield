package handlers

// auth.go — POST /api/v1/auth/login + GET /api/v1/auth/me 핸들러 (E9 Stage B).
//
// Login 흐름:
//  1. JSON body 파싱 (email + password)
//  2. Bootstrap Tx로 email → tenantID 조회 (Phase 1: 단일 tenant 가정 — seed admin이 생성한 첫 tenant)
//  3. tenant.Service.Login 호출 (TenantID 주입한 Tx)
//  4. 200 + accessToken·refreshToken·user 반환
//
// Me 흐름:
//  1. AuthMiddleware가 ctx에 주입한 claims 추출
//  2. Bootstrap Tx로 user 조회 (TenantID + Subject)
//  3. 200 + user 반환

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/ssabro/rosshield/internal/domain/tenant"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// loginRequest는 POST /api/v1/auth/login 요청 본문입니다.
type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// userResponse는 응답에 포함되는 user 메타입니다.
//
// Roles는 RBAC Stage 2-B(Phase 5)에서 추가됨 — Web UI가 button conditional
// render에 사용. JWT의 AccessClaims.Roles와 동일 셋. 서버 측 admin/auditor gate
// 적용 endpoint는 RBAC Stage 1+2-A 참조.
type userResponse struct {
	ID          string   `json:"id"`
	Email       string   `json:"email"`
	DisplayName string   `json:"displayName"`
	TenantID    string   `json:"tenantId,omitempty"`
	Roles       []string `json:"roles,omitempty"`
}

// loginResponse는 POST /api/v1/auth/login 성공 응답 본문입니다.
//
// Cookie 모드(`X-Cookie-Auth: true` 헤더)일 때 RefreshToken은 빈 문자열 — Set-Cookie로 송출.
// 그 외(legacy CLI 모드)는 본문에 그대로 포함 (호환성 유지 — C6 dual mode).
type loginResponse struct {
	AccessToken  string       `json:"accessToken"`
	RefreshToken string       `json:"refreshToken,omitempty"`
	User         userResponse `json:"user"`
}

// refreshRequest는 POST /api/v1/auth/refresh 요청 본문입니다 (legacy 모드 — 본문에서 토큰).
//
// Cookie 모드는 본 필드 비워두면 됨 — 핸들러가 cookie에서 자동 추출.
type refreshRequest struct {
	RefreshToken string `json:"refreshToken,omitempty"`
}

// refreshResponse는 POST /api/v1/auth/refresh 성공 응답 본문입니다.
type refreshResponse struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken,omitempty"`
}

// logoutRequest는 POST /api/v1/auth/logout 요청 본문입니다 (legacy 모드).
type logoutRequest struct {
	RefreshToken string `json:"refreshToken,omitempty"`
}

// refreshCookieName은 HttpOnly cookie의 이름입니다 (C6 — Web Console 보안 강화).
const refreshCookieName = "rosshield_refresh"

// cookieHeader는 클라이언트가 cookie 모드를 요청할 때 송신하는 헤더입니다.
//
// `X-Cookie-Auth: true` 일 때 — refresh token은 본문에서 제거되고 Set-Cookie로 송출.
// CLI 같은 legacy 클라이언트는 헤더 없이 호출 → 본문에 둘 다 포함 (호환성 유지).
const cookieAuthHeader = "X-Cookie-Auth"

// isCookieAuth는 클라이언트가 cookie 모드를 요청했는지 검사합니다.
func isCookieAuth(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get(cookieAuthHeader), "true")
}

// setRefreshCookie는 refresh 토큰을 HttpOnly cookie로 응답에 부착합니다.
//
// SameSite=Lax — POST /auth/refresh 같은 동일 origin 요청에 자동 전송, CSRF 위험 완화.
// Secure — TLS 환경에서만 true (개발 http://localhost는 false). r.TLS != nil 또는
// X-Forwarded-Proto: https 검사로 결정.
// Path=/api/v1/auth — refresh·logout 엔드포인트에만 동봉되도록 좁힘.
func setRefreshCookie(w http.ResponseWriter, r *http.Request, token string, expiresAt time.Time) {
	maxAge := int(time.Until(expiresAt).Seconds())
	if maxAge < 0 {
		maxAge = 0
	}
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    token,
		Path:     "/api/v1/auth",
		HttpOnly: true,
		Secure:   isHTTPS(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
	})
}

// clearRefreshCookie는 refresh cookie를 즉시 만료시킵니다 (logout).
func clearRefreshCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    "",
		Path:     "/api/v1/auth",
		HttpOnly: true,
		Secure:   isHTTPS(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// isHTTPS는 요청이 TLS인지 추정합니다 (직접 TLS 또는 reverse proxy X-Forwarded-Proto).
func isHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// readRefreshFromRequest는 cookie 우선 → 본문 fallback 순으로 refresh 토큰을 추출합니다.
//
// dual mode 지원 — Web Console은 cookie, CLI/legacy는 본문.
func readRefreshFromRequest(r *http.Request) string {
	if c, err := r.Cookie(refreshCookieName); err == nil && c.Value != "" {
		return c.Value
	}
	// 본문 fallback — JSON decode 실패는 무시 (빈 문자열 반환).
	var body refreshRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
		return body.RefreshToken
	}
	return ""
}

// readRefreshFromLogoutRequest는 logoutRequest 본문을 처리합니다 (refreshRequest와 키 동일이지만 분리 의도 명확).
func readRefreshFromLogoutRequest(r *http.Request) string {
	if c, err := r.Cookie(refreshCookieName); err == nil && c.Value != "" {
		return c.Value
	}
	var body logoutRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
		return body.RefreshToken
	}
	return ""
}

// Login은 POST /api/v1/auth/login 핸들러입니다 (gen.ServerInterface override).
//
// 401 매핑: invalid email/password (도메인 ErrInvalidCredentials) + ErrUserDisabled +
// 잘못된 JSON 본문 (보수적 — 사용자에게 더 적은 정보 노출).
func (h *Handlers) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" || req.Password == "" {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	// 1. email → tenantID 조회 (Bootstrap Tx, tenant 미상 시점).
	tenantID, err := lookupTenantByEmail(r.Context(), h.deps.Storage, email)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, http.StatusUnauthorized, "invalid credentials")
			return
		}
		writeError(w, http.StatusInternalServerError, "lookup failed")
		return
	}

	// 2. tenant scope Tx로 Login 호출.
	var result tenant.LoginResult
	err = h.deps.Storage.Tx(storage.WithTenantID(r.Context(), tenantID),
		func(ctx context.Context, tx storage.Tx) error {
			res, e := h.deps.Tenant.Login(ctx, tx, tenant.LoginRequest{
				TenantID: tenantID,
				Email:    email,
				Password: req.Password,
			})
			if e != nil {
				return e
			}
			result = res
			return nil
		})
	if err != nil {
		switch {
		case errors.Is(err, tenant.ErrInvalidCredentials),
			errors.Is(err, tenant.ErrUserDisabled):
			writeError(w, http.StatusUnauthorized, "invalid credentials")
		default:
			writeError(w, http.StatusInternalServerError, "login failed")
		}
		return
	}

	resp := loginResponse{
		AccessToken: result.AccessToken,
		User: userResponse{
			ID:          result.User.ID,
			Email:       result.User.Email,
			DisplayName: result.User.DisplayName,
			TenantID:    string(result.User.TenantID),
			Roles:       roleNames(result.Roles),
		},
	}
	if isCookieAuth(r) {
		// Cookie 모드: refresh를 HttpOnly cookie로만 송출, 본문에서 제외 (XSS 노출 차단).
		setRefreshCookie(w, r, result.RefreshToken, result.RefreshExpiresAt)
	} else {
		// Legacy 모드 (CLI 등): 본문에 refreshToken 포함 (호환성 유지).
		resp.RefreshToken = result.RefreshToken
	}
	writeJSON(w, http.StatusOK, resp)
}

// RefreshAuth는 POST /api/v1/auth/refresh 핸들러입니다 (C6 — 새 access·refresh 발급).
//
// dual mode: cookie 우선 → 본문 fallback. cookie 모드면 새 refresh를 cookie로,
// 본문에 access만 반환. legacy는 본문에 둘 다.
//
// 401 매핑: ErrInvalidToken / ErrTokenExpired / ErrRefreshNotFound / ErrRefreshRevoked /
// ErrRefreshReuseDetected / ErrRefreshExpired 모두 401 (탈취 의심 → 클라이언트는 재로그인).
func (h *Handlers) RefreshAuth(w http.ResponseWriter, r *http.Request) {
	token := readRefreshFromRequest(r)
	if token == "" {
		writeError(w, http.StatusUnauthorized, "refresh token missing")
		return
	}

	// Bootstrap Tx로 진입 — Refresh 구현은 tx.TenantID() == "" 일 때 토큰의 tid를 그대로 사용.
	// multi-tenant 강화 시 token claims에서 tid 추출 후 ctx.WithTenantID로 좁힘 (R12-12 후속).
	var result tenant.LoginResult
	err := h.deps.Storage.Bootstrap(r.Context(),
		func(ctx context.Context, tx storage.Tx) error {
			res, e := h.deps.Tenant.Refresh(ctx, tx, token)
			if e != nil {
				return e
			}
			result = res
			return nil
		})
	if err != nil {
		switch {
		case errors.Is(err, tenant.ErrInvalidToken),
			errors.Is(err, tenant.ErrTokenExpired),
			errors.Is(err, tenant.ErrTokenSignatureInvalid),
			errors.Is(err, tenant.ErrRefreshNotFound),
			errors.Is(err, tenant.ErrRefreshRevoked),
			errors.Is(err, tenant.ErrRefreshReuseDetected),
			errors.Is(err, tenant.ErrRefreshExpired):
			// 탈취 의심·만료 모두 클라이언트 재로그인 필요 — cookie도 비움.
			clearRefreshCookie(w, r)
			writeError(w, http.StatusUnauthorized, "refresh failed")
		default:
			writeError(w, http.StatusInternalServerError, "refresh failed")
		}
		return
	}

	resp := refreshResponse{AccessToken: result.AccessToken}
	if isCookieAuth(r) {
		setRefreshCookie(w, r, result.RefreshToken, result.RefreshExpiresAt)
	} else {
		resp.RefreshToken = result.RefreshToken
	}
	writeJSON(w, http.StatusOK, resp)
}

// LogoutAuth는 POST /api/v1/auth/logout 핸들러입니다 (C6 — refresh revoke + cookie clear).
//
// 멱등 — refresh가 없거나 이미 revoked여도 200. cookie는 항상 비움.
func (h *Handlers) LogoutAuth(w http.ResponseWriter, r *http.Request) {
	token := readRefreshFromLogoutRequest(r)
	// 토큰 부재여도 cookie clear는 진행 — 사용자 경험상 logout은 항상 성공으로 보이게.
	if token != "" {
		_ = h.deps.Storage.Bootstrap(r.Context(),
			func(ctx context.Context, tx storage.Tx) error {
				return h.deps.Tenant.Logout(ctx, tx, token)
			})
		// 도메인 에러는 무시 — 멱등 보장. 단, 향후 audit emit이 필요하면 분리 분기.
	}
	clearRefreshCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

// GetCurrentSession은 GET /api/v1/auth/me 핸들러입니다.
//
// AuthMiddleware가 이미 토큰을 검증하고 claims를 주입했으므로, 여기서는 user 메타만 조회.
func (h *Handlers) GetCurrentSession(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok || claims.Subject == "" {
		writeError(w, http.StatusUnauthorized, "no session in context")
		return
	}

	// Subject(userID)와 TenantID로 user 조회 — email 기반 GetUserByEmail은 부적절.
	// raw 쿼리로 userID lookup. (P5 위반 회피 위해 Service에 GetUserByID 추가 가능하지만
	// Phase 1 Stage B는 본 핸들러에서만 사용 — minimal change.)
	user, err := lookupUserByID(r.Context(), h.deps.Storage, claims.TenantID, claims.Subject)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, http.StatusUnauthorized, "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "lookup failed")
		return
	}

	// Roles는 AccessClaims(JWT)에서 직접 추출 — DB 재조회 회피 (회전 시점은 다음 refresh).
	// RBAC Stage 2-B: Web UI button conditional render에 사용.
	writeJSON(w, http.StatusOK, userResponse{
		ID:          user.ID,
		Email:       user.Email,
		DisplayName: user.DisplayName,
		TenantID:    string(user.TenantID),
		Roles:       append([]string{}, claims.Roles...),
	})
}

// roleNames는 LoginResult.Roles ([]tenant.Role)에서 .Name만 뽑아 string slice로 변환합니다.
//
// nil 입력은 nil 반환(omitempty가 응답에서 필드를 누락). RBAC Stage 2-B에서 Login·Me 응답
// userResponse.Roles에 사용.
func roleNames(roles []tenant.Role) []string {
	if len(roles) == 0 {
		return nil
	}
	out := make([]string, 0, len(roles))
	for _, r := range roles {
		out = append(out, r.Name)
	}
	return out
}

// lookupTenantByEmail은 email로 tenantID를 조회합니다 (Bootstrap Tx — tenant 미상 시점).
//
// users 테이블에서 LOWER(email) 매칭 — 첫 매치 반환. multi-tenant 환경에서는 tenant
// hint(subdomain·header)가 필요하지만 Phase 1은 단일 tenant 가정.
//
// 못 찾으면 storage.ErrNotFound. raw SQL — Service에 메서드 추가는 후속 Stage.
func lookupTenantByEmail(ctx context.Context, store storage.Storage, email string) (storage.TenantID, error) {
	var tid string
	err := store.Bootstrap(ctx, func(ctx context.Context, tx storage.Tx) error {
		row := tx.QueryRow(ctx,
			`SELECT tenant_id FROM users WHERE LOWER(email) = LOWER(?) LIMIT 1`, email)
		if err := row.Scan(&tid); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		// sql.ErrNoRows → storage.ErrNotFound 매핑은 driver 책임이지만 raw QueryRow는 wrap 안 됨.
		// 빈 문자열이면 not found로 간주.
		if tid == "" {
			return "", storage.ErrNotFound
		}
		return "", err
	}
	if tid == "" {
		return "", storage.ErrNotFound
	}
	return storage.TenantID(tid), nil
}

// lookupUserByID는 (tenantID, userID)로 user를 조회합니다 (tenant scope Tx).
//
// Phase 1 Stage B 단순화 — Service.GetUserByID 메서드는 후속에서. raw SQL.
func lookupUserByID(ctx context.Context, store storage.Storage, tenantID storage.TenantID, userID string) (tenant.User, error) {
	var u tenant.User
	err := store.Tx(storage.WithTenantID(ctx, tenantID), func(ctx context.Context, tx storage.Tx) error {
		row := tx.QueryRow(ctx,
			`SELECT id, tenant_id, email, display_name, status FROM users WHERE id = ? AND tenant_id = ?`,
			userID, string(tenantID))
		var status string
		var tid string
		if err := row.Scan(&u.ID, &tid, &u.Email, &u.DisplayName, &status); err != nil {
			return err
		}
		u.TenantID = storage.TenantID(tid)
		u.Status = tenant.UserStatus(status)
		return nil
	})
	if err != nil {
		if u.ID == "" {
			return tenant.User{}, storage.ErrNotFound
		}
		return tenant.User{}, err
	}
	return u, nil
}
