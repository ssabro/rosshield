package handlers

// invitation.go — E21 초대·역할 관리 HTTP 표면.
//
// 엔드포인트:
//
//	POST   /api/v1/invitations                      (인증 필요)  → 초대 생성
//	GET    /api/v1/invitations                      (인증 필요)  → 테넌트 안 모든 초대
//	DELETE /api/v1/invitations/{invitationId}       (인증 필요)  → 초대 즉시 만료
//	GET    /api/v1/invitations/by-token/{token}     (비인증)     → 토큰으로 미리보기
//	POST   /api/v1/invitations/by-token/{token}/accept  (비인증) → user 생성 + role 할당
//
// 옵트인 (P10):
//
//	deps.Invitation == nil → 503. handlers.Mount는 결선 여부와 무관하게 라우트 등록.
//
// 보안 메모:
//
//	by-token/* 는 인증 없이 진입 — token이 사실상 capability. 토큰 lookup은 1회 전체 테이블
//	scan(token UNIQUE INDEX 활용)이라 cross-tenant leak 없음.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/ssabro/rosshield/internal/domain/tenant"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// invitationView는 단일 초대의 클라이언트 응답 형태입니다.
//
// Token은 list/get 응답에는 포함하지 않음 — 발송 시점(create response)에만 노출.
// 사후 lookup은 admin이 사용자에게 직접 전달하는 모델로 단순화.
type invitationView struct {
	ID         string  `json:"id"`
	Email      string  `json:"email"`
	RoleName   string  `json:"roleName"`
	InvitedBy  string  `json:"invitedBy"`
	ExpiresAt  string  `json:"expiresAt"`
	AcceptedAt *string `json:"acceptedAt,omitempty"`
	AcceptedBy *string `json:"acceptedBy,omitempty"`
	CreatedAt  string  `json:"createdAt"`
}

func toInvitationView(i tenant.Invitation) invitationView {
	v := invitationView{
		ID:        i.ID,
		Email:     i.Email,
		RoleName:  i.RoleName,
		InvitedBy: i.InvitedBy,
		ExpiresAt: i.ExpiresAt.Format(time.RFC3339Nano),
		CreatedAt: i.CreatedAt.Format(time.RFC3339Nano),
	}
	if i.AcceptedAt != nil {
		s := i.AcceptedAt.Format(time.RFC3339Nano)
		v.AcceptedAt = &s
	}
	if i.AcceptedBy != nil {
		v.AcceptedBy = i.AcceptedBy
	}
	return v
}

// CreateInvitation: POST /api/v1/invitations.
//
// body: {email, roleName, expiresInHours?}
// 응답: 201 + invitationView + token (1회 노출).
func (h *Handlers) CreateInvitation(w http.ResponseWriter, r *http.Request) {
	tenantID := storage.TenantIDFromContext(r.Context())
	if tenantID == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}
	if h.deps.Invitation == nil {
		writeError(w, http.StatusServiceUnavailable, "invitation: service not configured")
		return
	}
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "no auth claims in context")
		return
	}
	var body struct {
		Email          string `json:"email"`
		RoleName       string `json:"roleName"`
		ExpiresInHours int    `json:"expiresInHours"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	var ttl time.Duration
	if body.ExpiresInHours > 0 {
		ttl = time.Duration(body.ExpiresInHours) * time.Hour
	}
	var (
		created tenant.Invitation
		token   string
	)
	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		res, e := h.deps.Invitation.CreateInvitation(ctx, tx, tenant.CreateInvitationRequest{
			TenantID:  tenantID,
			Email:     body.Email,
			RoleName:  body.RoleName,
			InvitedBy: claims.Subject,
			ExpiresIn: ttl,
		})
		if e != nil {
			return e
		}
		created = res.Invitation
		token = res.Token
		return nil
	})
	if err != nil {
		writeError(w, invitationErrorStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, struct {
		invitationView
		Token string `json:"token"`
	}{toInvitationView(created), token})
}

// ListInvitations: GET /api/v1/invitations.
func (h *Handlers) ListInvitations(w http.ResponseWriter, r *http.Request) {
	if storage.TenantIDFromContext(r.Context()) == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}
	if h.deps.Invitation == nil {
		writeError(w, http.StatusServiceUnavailable, "invitation: service not configured")
		return
	}
	var invitations []tenant.Invitation
	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		out, e := h.deps.Invitation.ListInvitations(ctx, tx)
		if e != nil {
			return e
		}
		invitations = out
		return nil
	})
	if err != nil {
		writeError(w, invitationErrorStatus(err), err.Error())
		return
	}
	views := make([]invitationView, 0, len(invitations))
	for _, i := range invitations {
		views = append(views, toInvitationView(i))
	}
	writeJSON(w, http.StatusOK, struct {
		Invitations []invitationView `json:"invitations"`
	}{views})
}

// RevokeInvitation: DELETE /api/v1/invitations/{invitationId}.
func (h *Handlers) RevokeInvitation(w http.ResponseWriter, r *http.Request, invitationID string) {
	if storage.TenantIDFromContext(r.Context()) == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}
	if h.deps.Invitation == nil {
		writeError(w, http.StatusServiceUnavailable, "invitation: service not configured")
		return
	}
	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		return h.deps.Invitation.RevokeInvitation(ctx, tx, invitationID)
	})
	if err != nil {
		writeError(w, invitationErrorStatus(err), err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetInvitationByToken: GET /api/v1/invitations/by-token/{token}.
//
// 비인증 — 토큰이 capability. tenant scope 미상 → Bootstrap Tx로 lookup.
// 응답 노출은 최소: email·roleName·expiresAt·tenant 이름(없음, ID만).
func (h *Handlers) GetInvitationByToken(w http.ResponseWriter, r *http.Request, token string) {
	if h.deps.Invitation == nil {
		writeError(w, http.StatusServiceUnavailable, "invitation: service not configured")
		return
	}
	var inv tenant.Invitation
	err := h.deps.Storage.Bootstrap(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		out, e := h.deps.Invitation.GetInvitationByToken(ctx, tx, token)
		if e != nil {
			return e
		}
		inv = out
		return nil
	})
	if err != nil {
		writeError(w, invitationErrorStatus(err), err.Error())
		return
	}
	// minimal preview — token 자체는 응답에 안 넣음 (이미 갖고 있음).
	writeJSON(w, http.StatusOK, struct {
		Email     string `json:"email"`
		RoleName  string `json:"roleName"`
		ExpiresAt string `json:"expiresAt"`
		Accepted  bool   `json:"accepted"`
	}{
		Email:     inv.Email,
		RoleName:  inv.RoleName,
		ExpiresAt: inv.ExpiresAt.Format(time.RFC3339Nano),
		Accepted:  inv.IsAccepted(),
	})
}

// AcceptInvitation: POST /api/v1/invitations/by-token/{token}/accept.
//
// body: {email, password, displayName}
// 응답: 200 + user 정보 (token 발급 안 함 — 사용자가 다시 /auth/login으로).
// Bootstrap Tx로 진입 (tenant 미상 — token이 결정).
func (h *Handlers) AcceptInvitation(w http.ResponseWriter, r *http.Request, token string) {
	if h.deps.Invitation == nil {
		writeError(w, http.StatusServiceUnavailable, "invitation: service not configured")
		return
	}
	var body struct {
		Email       string `json:"email"`
		Password    string `json:"password"`
		DisplayName string `json:"displayName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	var result tenant.AcceptInvitationResult
	err := h.deps.Storage.Bootstrap(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		res, e := h.deps.Invitation.AcceptInvitation(ctx, tx, tenant.AcceptInvitationRequest{
			Token:       token,
			Email:       body.Email,
			Password:    body.Password,
			DisplayName: body.DisplayName,
		})
		if e != nil {
			return e
		}
		result = res
		return nil
	})
	if err != nil {
		writeError(w, invitationErrorStatus(err), err.Error())
		return
	}
	roles := make([]string, 0, len(result.Roles))
	for _, role := range result.Roles {
		roles = append(roles, role.Name)
	}
	writeJSON(w, http.StatusOK, struct {
		UserID      string   `json:"userId"`
		Email       string   `json:"email"`
		DisplayName string   `json:"displayName"`
		Roles       []string `json:"roles"`
	}{
		UserID:      result.User.ID,
		Email:       result.User.Email,
		DisplayName: result.User.DisplayName,
		Roles:       roles,
	})
}

// invitationErrorStatus는 invitation 도메인 sentinel을 HTTP status로 매핑합니다.
func invitationErrorStatus(err error) int {
	switch {
	case errors.Is(err, tenant.ErrInvitationNotFound):
		return http.StatusNotFound
	case errors.Is(err, tenant.ErrInvitationExpired),
		errors.Is(err, tenant.ErrInvitationAlreadyUsed),
		errors.Is(err, tenant.ErrInvitationEmailMismatch),
		errors.Is(err, tenant.ErrInvalidRole),
		errors.Is(err, tenant.ErrEmptyEmail),
		errors.Is(err, tenant.ErrEmptyToken),
		errors.Is(err, tenant.ErrPasswordTooShort):
		return http.StatusBadRequest
	case errors.Is(err, tenant.ErrInvitationActive),
		errors.Is(err, tenant.ErrEmailAlreadyExists):
		return http.StatusConflict
	default:
		return errorStatusFor(err)
	}
}
