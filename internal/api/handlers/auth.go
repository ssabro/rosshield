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

	"github.com/ssabro/rosshield/internal/domain/tenant"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// loginRequest는 POST /api/v1/auth/login 요청 본문입니다.
type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// userResponse는 응답에 포함되는 user 메타입니다.
type userResponse struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
	TenantID    string `json:"tenantId,omitempty"`
}

// loginResponse는 POST /api/v1/auth/login 성공 응답 본문입니다.
type loginResponse struct {
	AccessToken  string       `json:"accessToken"`
	RefreshToken string       `json:"refreshToken"`
	User         userResponse `json:"user"`
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

	writeJSON(w, http.StatusOK, loginResponse{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		User: userResponse{
			ID:          result.User.ID,
			Email:       result.User.Email,
			DisplayName: result.User.DisplayName,
			TenantID:    string(result.User.TenantID),
		},
	})
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

	writeJSON(w, http.StatusOK, userResponse{
		ID:          user.ID,
		Email:       user.Email,
		DisplayName: user.DisplayName,
		TenantID:    string(user.TenantID),
	})
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
