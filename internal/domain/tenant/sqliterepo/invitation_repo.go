package sqliterepo

// invitation_repo.go — E21 InvitationService 구현.
//
// 기존 Repo가 tenant.InvitationService도 만족하도록 메서드를 별 파일로 분리.
// (Repo 본체 비대화 회피.)

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ssabro/rosshield/internal/domain/tenant"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// generateInvitationToken은 32B random → base64url 인코딩된 토큰을 생성합니다.
//
// 길이: 43자 (URL-safe). 추측 어려움 보장 (256-bit entropy).
func generateInvitationToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("tenant: generate invitation token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// CreateInvitation은 새 초대를 INSERT하고 토큰을 반환합니다.
func (r *Repo) CreateInvitation(ctx context.Context, tx storage.Tx, req tenant.CreateInvitationRequest) (tenant.CreateInvitationResult, error) {
	if req.TenantID == "" {
		return tenant.CreateInvitationResult{}, storage.ErrTenantMissing
	}
	emailNormalized := strings.ToLower(strings.TrimSpace(req.Email))
	if emailNormalized == "" {
		return tenant.CreateInvitationResult{}, tenant.ErrEmptyEmail
	}
	if strings.TrimSpace(req.RoleName) == "" {
		return tenant.CreateInvitationResult{}, tenant.ErrInvalidRole
	}
	if req.InvitedBy == "" {
		return tenant.CreateInvitationResult{}, fmt.Errorf("tenant: InvitedBy is required")
	}

	// roleName 존재 검증 — (tenantID, name) 조합.
	_, err := r.GetRole(ctx, tx, req.TenantID, req.RoleName)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return tenant.CreateInvitationResult{}, tenant.ErrInvalidRole
		}
		return tenant.CreateInvitationResult{}, fmt.Errorf("tenant: lookup role: %w", err)
	}

	now := r.deps.Clock.Now().UTC()

	// 활성 초대 중복 체크 — 같은 (tenant, email)로 미사용·미만료 row가 있으면 거부.
	var activeCount int
	row := tx.QueryRow(ctx, `
SELECT COUNT(*) FROM invitations
 WHERE tenant_id = ?
   AND email = ?
   AND accepted_at IS NULL
   AND expires_at > ?`,
		string(req.TenantID), emailNormalized, now.Format(time.RFC3339Nano))
	if err := row.Scan(&activeCount); err != nil {
		return tenant.CreateInvitationResult{}, fmt.Errorf("tenant: check active invitation: %w", err)
	}
	if activeCount > 0 {
		return tenant.CreateInvitationResult{}, tenant.ErrInvitationActive
	}

	ttl := req.ExpiresIn
	if ttl <= 0 {
		ttl = tenant.DefaultInvitationTTL
	}

	token, err := generateInvitationToken()
	if err != nil {
		return tenant.CreateInvitationResult{}, err
	}

	inv := tenant.Invitation{
		ID:        r.deps.IDGen.New("inv"),
		TenantID:  req.TenantID,
		Email:     emailNormalized,
		RoleName:  req.RoleName,
		Token:     token,
		InvitedBy: req.InvitedBy,
		ExpiresAt: now.Add(ttl),
		CreatedAt: now,
	}

	if _, err := tx.Exec(ctx, `
INSERT INTO invitations
  (id, tenant_id, email, role_name, token, invited_by, expires_at, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		inv.ID, string(inv.TenantID), inv.Email, inv.RoleName,
		inv.Token, inv.InvitedBy,
		inv.ExpiresAt.Format(time.RFC3339Nano), inv.CreatedAt.Format(time.RFC3339Nano)); err != nil {
		return tenant.CreateInvitationResult{}, fmt.Errorf("tenant: insert invitation: %w", err)
	}

	if r.deps.InvitationAudit != nil {
		if err := r.deps.InvitationAudit.EmitInvitationSent(ctx, tx, inv); err != nil {
			return tenant.CreateInvitationResult{}, fmt.Errorf("tenant: emit invitation.sent: %w", err)
		}
	}

	// O6 — InvitationNotifier 호출 (옵트인). 알림 실패는 INSERT를 rollback하지 않음 —
	// best-effort delivery. 실패 시에도 caller(handler)는 token을 응답으로 받아 admin이
	// 수동 전달 가능. 운영 모니터링은 Notifier 구현의 자체 metric/log에 위임.
	if r.deps.InvitationNotifier != nil {
		acceptURL := ""
		if r.deps.InvitationAcceptURLBuilder != nil {
			acceptURL = r.deps.InvitationAcceptURLBuilder(token)
		}
		_ = r.deps.InvitationNotifier.NotifyInvitationSent(ctx, inv, acceptURL)
	}

	return tenant.CreateInvitationResult{Invitation: inv, Token: token}, nil
}

// AcceptInvitation은 토큰으로 초대를 검증하고 user를 생성·role 할당합니다.
func (r *Repo) AcceptInvitation(ctx context.Context, tx storage.Tx, req tenant.AcceptInvitationRequest) (tenant.AcceptInvitationResult, error) {
	if strings.TrimSpace(req.Token) == "" {
		return tenant.AcceptInvitationResult{}, tenant.ErrEmptyToken
	}
	emailNormalized := strings.ToLower(strings.TrimSpace(req.Email))
	if emailNormalized == "" {
		return tenant.AcceptInvitationResult{}, tenant.ErrEmptyEmail
	}
	if len(req.Password) < 12 {
		return tenant.AcceptInvitationResult{}, tenant.ErrPasswordTooShort
	}

	// 1. token으로 invitation 조회.
	inv, err := r.lookupInvitationByToken(ctx, tx, req.Token)
	if err != nil {
		return tenant.AcceptInvitationResult{}, err
	}

	now := r.deps.Clock.Now().UTC()

	// 2. 상태 검증.
	if inv.IsAccepted() {
		return tenant.AcceptInvitationResult{}, tenant.ErrInvitationAlreadyUsed
	}
	if inv.IsExpired(now) {
		return tenant.AcceptInvitationResult{}, tenant.ErrInvitationExpired
	}
	if !strings.EqualFold(inv.Email, emailNormalized) {
		return tenant.AcceptInvitationResult{}, tenant.ErrInvitationEmailMismatch
	}

	// 3. role 조회.
	role, err := r.GetRole(ctx, tx, inv.TenantID, inv.RoleName)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return tenant.AcceptInvitationResult{}, tenant.ErrInvalidRole
		}
		return tenant.AcceptInvitationResult{}, fmt.Errorf("tenant: lookup role: %w", err)
	}

	// 4. 기존 user 중복 체크.
	if _, err := r.GetUserByEmail(ctx, tx, inv.TenantID, emailNormalized); err == nil {
		return tenant.AcceptInvitationResult{}, tenant.ErrEmailAlreadyExists
	} else if !errors.Is(err, storage.ErrNotFound) {
		return tenant.AcceptInvitationResult{}, fmt.Errorf("tenant: check existing user: %w", err)
	}

	// 5. user 생성 + role 할당.
	hash, err := tenant.HashPassword(req.Password)
	if err != nil {
		return tenant.AcceptInvitationResult{}, err
	}
	user := tenant.User{
		ID:           r.deps.IDGen.New("us"),
		TenantID:     inv.TenantID,
		Email:        emailNormalized,
		DisplayName:  req.DisplayName,
		AuthProvider: tenant.AuthProviderLocal,
		PasswordHash: hash,
		Status:       tenant.UserStatusActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := insertUser(ctx, tx, user); err != nil {
		return tenant.AcceptInvitationResult{}, err
	}
	if err := assignRole(ctx, tx, user.ID, role.ID); err != nil {
		return tenant.AcceptInvitationResult{}, err
	}

	// 6. invitation accepted_at·accepted_by 갱신.
	acceptedAt := now
	acceptedBy := user.ID
	if _, err := tx.Exec(ctx, `
UPDATE invitations SET accepted_at = ?, accepted_by = ?
 WHERE id = ?`,
		acceptedAt.Format(time.RFC3339Nano), acceptedBy, inv.ID); err != nil {
		return tenant.AcceptInvitationResult{}, fmt.Errorf("tenant: mark invitation accepted: %w", err)
	}
	inv.AcceptedAt = &acceptedAt
	inv.AcceptedBy = &acceptedBy

	if r.deps.InvitationAudit != nil {
		if err := r.deps.InvitationAudit.EmitInvitationAccepted(ctx, tx, inv, user); err != nil {
			return tenant.AcceptInvitationResult{}, fmt.Errorf("tenant: emit invitation.accepted: %w", err)
		}
	}

	return tenant.AcceptInvitationResult{
		User:       user,
		Roles:      []tenant.Role{role},
		Invitation: inv,
	}, nil
}

// GetInvitationByToken은 token으로 invitation을 조회합니다 (Bootstrap Tx 권장).
func (r *Repo) GetInvitationByToken(ctx context.Context, tx storage.Tx, token string) (tenant.Invitation, error) {
	if strings.TrimSpace(token) == "" {
		return tenant.Invitation{}, tenant.ErrEmptyToken
	}
	return r.lookupInvitationByToken(ctx, tx, token)
}

// ListInvitations는 tenant 안 모든 초대를 created_at DESC로 반환합니다.
func (r *Repo) ListInvitations(ctx context.Context, tx storage.Tx) ([]tenant.Invitation, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return nil, storage.ErrTenantMissing
	}
	rows, err := tx.Query(ctx, `
SELECT id, tenant_id, email, role_name, token, invited_by,
       expires_at, accepted_at, accepted_by, created_at
  FROM invitations
 WHERE tenant_id = ?
 ORDER BY created_at DESC`,
		string(tenantID))
	if err != nil {
		return nil, fmt.Errorf("tenant: list invitations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []tenant.Invitation
	for rows.Next() {
		inv, err := scanInvitationRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("tenant: iterate invitations: %w", err)
	}
	return out, nil
}

// RevokeInvitation은 (tenantID, invitationID) 초대를 즉시 만료시킵니다 (멱등).
func (r *Repo) RevokeInvitation(ctx context.Context, tx storage.Tx, invitationID string) error {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return storage.ErrTenantMissing
	}
	now := r.deps.Clock.Now().UTC().Format(time.RFC3339Nano)
	res, err := tx.Exec(ctx, `
UPDATE invitations SET expires_at = ?
 WHERE tenant_id = ? AND id = ?`,
		now, string(tenantID), invitationID)
	if err != nil {
		return fmt.Errorf("tenant: revoke invitation: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("tenant: rows affected: %w", err)
	}
	if n == 0 {
		return tenant.ErrInvitationNotFound
	}
	return nil
}

// === internals ===

// rowScanner는 sql.Row와 sql.Rows 공통 메서드입니다.
type rowScanner interface {
	Scan(dest ...any) error
}

func (r *Repo) lookupInvitationByToken(ctx context.Context, tx storage.Tx, token string) (tenant.Invitation, error) {
	row := tx.QueryRow(ctx, `
SELECT id, tenant_id, email, role_name, token, invited_by,
       expires_at, accepted_at, accepted_by, created_at
  FROM invitations
 WHERE token = ?`,
		token)
	inv, err := scanInvitationRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return tenant.Invitation{}, tenant.ErrInvitationNotFound
		}
		return tenant.Invitation{}, err
	}
	return inv, nil
}

func scanInvitationRow(scanner rowScanner) (tenant.Invitation, error) {
	var (
		inv          tenant.Invitation
		expiresAt    string
		acceptedAtNS sql.NullString
		acceptedByNS sql.NullString
		createdAt    string
		tenantIDStr  string
	)
	if err := scanner.Scan(
		&inv.ID, &tenantIDStr, &inv.Email, &inv.RoleName,
		&inv.Token, &inv.InvitedBy,
		&expiresAt, &acceptedAtNS, &acceptedByNS,
		&createdAt,
	); err != nil {
		return tenant.Invitation{}, err
	}
	inv.TenantID = storage.TenantID(tenantIDStr)
	parsed, err := time.Parse(time.RFC3339Nano, expiresAt)
	if err != nil {
		return tenant.Invitation{}, fmt.Errorf("tenant: parse expires_at: %w", err)
	}
	inv.ExpiresAt = parsed
	if acceptedAtNS.Valid {
		t, err := time.Parse(time.RFC3339Nano, acceptedAtNS.String)
		if err != nil {
			return tenant.Invitation{}, fmt.Errorf("tenant: parse accepted_at: %w", err)
		}
		inv.AcceptedAt = &t
	}
	if acceptedByNS.Valid {
		s := acceptedByNS.String
		inv.AcceptedBy = &s
	}
	cAt, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return tenant.Invitation{}, fmt.Errorf("tenant: parse created_at: %w", err)
	}
	inv.CreatedAt = cAt
	return inv, nil
}
