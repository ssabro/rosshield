// Package sqliterepo는 tenant.Service의 SQLite 어댑터입니다.
//
// Create는 단일 트랜잭션 안에서:
//  1. INSERT tenants
//  2. argon2id로 admin password 해시
//  3. INSERT users (admin)
//  4. AuditEmitter.EmitTenantCreated (audit_entries에 'tenant.created' append)
//
// 모두 같은 Tx에 묶이므로 원자적입니다 (P5·P9).
package sqliterepo

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/ssabro/rosshield/internal/domain/tenant"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/idgen"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// Deps는 어댑터 의존성입니다.
type Deps struct {
	Clock clock.Clock
	IDGen idgen.IDGen
	Audit tenant.AuditEmitter // bootstrap이 audit.Service를 어댑팅한 구현체 주입.

	// InvitationAudit은 E21 초대 관련 audit emit. nil이면 emit skip (테스트 호환).
	InvitationAudit tenant.InvitationAuditEmitter

	// InvitationNotifier는 O6 — invite 생성 시 외부 채널(email 등) 알림 hook.
	// nil이면 알림 skip (기본·기존 동작 유지). 알림 실패는 invitation INSERT를 rollback하지
	// 않음 — best-effort delivery.
	InvitationNotifier tenant.InvitationNotifier

	// InvitationAcceptURLBuilder는 InvitationNotifier 호출 시 전달할 acceptURL을
	// 빌드하는 함수입니다. nil이면 빈 문자열을 넘김 — Notifier 구현이 직접 빌드하거나
	// URL 없이 발송할 수 있다.
	//
	// bootstrap이 cfg.PublicBaseURL 기반 closure를 주입한다 (도메인은 PublicBaseURL 미지각).
	InvitationAcceptURLBuilder func(token string) string

	// JWTPrivateKey/JWTPublicKey는 access·refresh 토큰 서명·검증용 (Stage D).
	// 비어 있으면 Login/Refresh/VerifyAccessToken은 ErrInvalidToken 반환 (테스트 외 부팅 경로에선 필수).
	JWTPrivateKey ed25519.PrivateKey
	JWTPublicKey  ed25519.PublicKey

	// AccessTTL은 access 토큰 수명. 0이면 tenant.DefaultAccessTTL.
	AccessTTL time.Duration
	// RefreshTTL은 refresh 토큰 수명. 0이면 tenant.DefaultRefreshTTL.
	RefreshTTL time.Duration
}

// Repo는 tenant.Service의 SQLite 구현입니다.
type Repo struct {
	deps Deps
}

// New는 새 Repo를 반환합니다.
func New(deps Deps) *Repo {
	return &Repo{deps: deps}
}

// Create는 tenant.Service.Create 구현입니다.
func (r *Repo) Create(ctx context.Context, tx storage.Tx, req tenant.CreateRequest) (tenant.CreateResult, error) {
	if err := validateCreate(req); err != nil {
		return tenant.CreateResult{}, err
	}
	plan := req.Plan
	if plan == "" {
		plan = tenant.PlanDesktopFree
	}
	if !validPlan(plan) {
		return tenant.CreateResult{}, tenant.ErrUnknownPlan
	}

	hash, err := tenant.HashPassword(req.AdminPassword)
	if err != nil {
		return tenant.CreateResult{}, err
	}

	now := r.deps.Clock.Now().UTC()
	tn := tenant.Tenant{
		ID:        storage.TenantID(r.deps.IDGen.New("tn")),
		Name:      req.Name,
		Plan:      plan,
		CreatedAt: now,
		Settings:  json.RawMessage(`{}`),
		Features:  json.RawMessage(`{}`),
		Retention: json.RawMessage(`{}`),
	}

	if err := insertTenant(ctx, tx, tn); err != nil {
		return tenant.CreateResult{}, err
	}

	admin := tenant.User{
		ID:           r.deps.IDGen.New("us"),
		TenantID:     tn.ID,
		Email:        strings.ToLower(strings.TrimSpace(req.AdminEmail)),
		DisplayName:  req.AdminDisplayName,
		AuthProvider: tenant.AuthProviderLocal,
		PasswordHash: hash,
		Status:       tenant.UserStatusActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := insertUser(ctx, tx, admin); err != nil {
		return tenant.CreateResult{}, err
	}

	// 시스템 역할 3개 시드 (admin, auditor, operator) — tenant마다 자체 역할 row.
	adminRole, err := r.seedSystemRoles(ctx, tx, tn.ID, now)
	if err != nil {
		return tenant.CreateResult{}, err
	}
	if err := assignRole(ctx, tx, admin.ID, adminRole.ID); err != nil {
		return tenant.CreateResult{}, err
	}

	if r.deps.Audit != nil {
		if err := r.deps.Audit.EmitTenantCreated(ctx, tx, tn, admin); err != nil {
			return tenant.CreateResult{}, fmt.Errorf("tenant: emit audit: %w", err)
		}
	}

	return tenant.CreateResult{Tenant: tn, Admin: admin}, nil
}

// seedSystemRoles는 admin/auditor/operator 3개 역할을 생성하고 admin role을 반환합니다.
func (r *Repo) seedSystemRoles(ctx context.Context, tx storage.Tx, tenantID storage.TenantID, now time.Time) (tenant.Role, error) {
	var adminRole tenant.Role
	for _, name := range []string{tenant.RoleAdmin, tenant.RoleAuditor, tenant.RoleOperator} {
		role := tenant.Role{
			ID:          r.deps.IDGen.New("rl"),
			TenantID:    tenantID,
			Name:        name,
			Permissions: tenant.SystemRolePermissions[name],
			IsSystem:    true,
			CreatedAt:   now,
		}
		if err := insertRole(ctx, tx, role); err != nil {
			return tenant.Role{}, err
		}
		if name == tenant.RoleAdmin {
			adminRole = role
		}
	}
	return adminRole, nil
}

// GetRole은 tenant.Service.GetRole 구현입니다.
func (r *Repo) GetRole(ctx context.Context, tx storage.Tx, tenantID storage.TenantID, name string) (tenant.Role, error) {
	row := tx.QueryRow(ctx, `
SELECT id, tenant_id, name, permissions, is_system, created_at
  FROM roles
 WHERE tenant_id = ? AND name = ?`,
		string(tenantID), name)
	return scanRoleRow(row)
}

// AssignRole은 tenant.Service.AssignRole 구현입니다 (멱등 — tenant scope).
//
// 본 메서드는 tenant scope binding을 INSERT — scope_type='tenant' / scope_id=” 자동 채움.
// fleet scope binding은 AssignRoleScoped를 사용합니다.
func (r *Repo) AssignRole(ctx context.Context, tx storage.Tx, userID, roleID string) error {
	return assignRole(ctx, tx, userID, roleID)
}

// AssignRoleScoped는 tenant.Service.AssignRoleScoped 구현입니다 (멱등 — 세분 RBAC Stage 2).
//
// scopeType=ScopeFleet이면 scopeID(fleet ID) 필수. scopeType=ScopeTenant이면 scopeID는
// 보수적으로 빈 문자열로 정규화 — DB 일관성 보존.
// 같은 (user, role) PK는 ON CONFLICT DO NOTHING으로 멱등 — 같은 user에 같은 role을 다른
// scope으로 두 번 할당하는 패턴은 본 Stage 범위 밖(Phase 6+ role binding multi-scope 개선).
func (r *Repo) AssignRoleScoped(ctx context.Context, tx storage.Tx, userID, roleID string, scopeType tenant.ScopeType, scopeID string) error {
	return assignRoleScoped(ctx, tx, userID, roleID, scopeType, scopeID)
}

// IssueApiKey는 tenant.Service.IssueApiKey 구현입니다.
func (r *Repo) IssueApiKey(ctx context.Context, tx storage.Tx, req tenant.IssueApiKeyRequest) (tenant.IssueApiKeyResult, error) {
	if req.TenantID == "" {
		return tenant.IssueApiKeyResult{}, storage.ErrTenantMissing
	}
	if strings.TrimSpace(req.Name) == "" {
		return tenant.IssueApiKeyResult{}, fmt.Errorf("tenant: api key Name is required")
	}
	if req.CreatedBy == "" {
		return tenant.IssueApiKeyResult{}, fmt.Errorf("tenant: api key CreatedBy is required")
	}

	rawToken, prefix, err := tenant.GenerateApiKeyToken()
	if err != nil {
		return tenant.IssueApiKeyResult{}, err
	}
	hash, err := tenant.HashPassword(rawToken)
	if err != nil {
		return tenant.IssueApiKeyResult{}, fmt.Errorf("tenant: hash api key: %w", err)
	}

	scopes := req.Scopes
	if scopes == nil {
		scopes = []tenant.Permission{}
	}
	scopesJSON, err := json.Marshal(scopes)
	if err != nil {
		return tenant.IssueApiKeyResult{}, fmt.Errorf("tenant: marshal scopes: %w", err)
	}

	now := r.deps.Clock.Now().UTC()
	key := tenant.ApiKey{
		ID:        r.deps.IDGen.New("ak"),
		TenantID:  req.TenantID,
		Name:      req.Name,
		Prefix:    prefix,
		Hashed:    hash,
		Scopes:    scopes,
		ExpiresAt: req.ExpiresAt,
		CreatedBy: req.CreatedBy,
		CreatedAt: now,
	}

	var expiresAt *string
	if key.ExpiresAt != nil {
		s := key.ExpiresAt.UTC().Format(time.RFC3339Nano)
		expiresAt = &s
	}

	_, err = tx.Exec(ctx, `
INSERT INTO api_keys (
    id, tenant_id, name, prefix, hashed, scopes,
    expires_at, last_used_at, created_by, created_at, revoked_at
) VALUES (?, ?, ?, ?, ?, ?, ?, NULL, ?, ?, NULL)`,
		key.ID, string(key.TenantID), key.Name, key.Prefix, key.Hashed, string(scopesJSON),
		expiresAt, key.CreatedBy, key.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return tenant.IssueApiKeyResult{}, fmt.Errorf("tenant: insert api key: %w", err)
	}
	return tenant.IssueApiKeyResult{Key: key, RawToken: rawToken}, nil
}

// AuthenticateApiKey는 tenant.Service.AuthenticateApiKey 구현입니다.
func (r *Repo) AuthenticateApiKey(ctx context.Context, tx storage.Tx, rawToken string) (tenant.ApiKey, error) {
	prefix, err := tenant.ExtractApiKeyPrefix(rawToken)
	if err != nil {
		return tenant.ApiKey{}, err
	}

	row := tx.QueryRow(ctx, `
SELECT id, tenant_id, name, prefix, hashed, scopes,
       expires_at, last_used_at, created_by, created_at, revoked_at
  FROM api_keys
 WHERE prefix = ?`,
		prefix)

	key, err := scanApiKeyRow(row)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return tenant.ApiKey{}, tenant.ErrApiKeyNotFound
		}
		return tenant.ApiKey{}, err
	}

	// argon2id verify (constant time).
	if err := tenant.VerifyPassword(rawToken, key.Hashed); err != nil {
		// hash 불일치 — wrong key를 같은 prefix로 시도하는 거의 없는 충돌 가능성.
		return tenant.ApiKey{}, tenant.ErrApiKeyNotFound
	}

	if key.RevokedAt != nil {
		return tenant.ApiKey{}, tenant.ErrApiKeyRevoked
	}
	if key.ExpiresAt != nil && !key.ExpiresAt.After(r.deps.Clock.Now().UTC()) {
		return tenant.ApiKey{}, tenant.ErrApiKeyExpired
	}
	return key, nil
}

// RevokeApiKey는 tenant.Service.RevokeApiKey 구현입니다 (멱등 soft delete).
func (r *Repo) RevokeApiKey(ctx context.Context, tx storage.Tx, tenantID storage.TenantID, apiKeyID string) error {
	now := r.deps.Clock.Now().UTC().Format(time.RFC3339Nano)
	res, err := tx.Exec(ctx, `
UPDATE api_keys
   SET revoked_at = COALESCE(revoked_at, ?)
 WHERE tenant_id = ? AND id = ?`,
		now, string(tenantID), apiKeyID)
	if err != nil {
		return fmt.Errorf("tenant: revoke api key: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("tenant: revoke api key rows affected: %w", err)
	}
	if affected == 0 {
		return storage.ErrNotFound
	}
	return nil
}

// ListApiKeys는 tenant.Service.ListApiKeys 구현입니다 (Hashed 마스킹).
func (r *Repo) ListApiKeys(ctx context.Context, tx storage.Tx, tenantID storage.TenantID) ([]tenant.ApiKey, error) {
	rows, err := tx.Query(ctx, `
SELECT id, tenant_id, name, prefix, hashed, scopes,
       expires_at, last_used_at, created_by, created_at, revoked_at
  FROM api_keys
 WHERE tenant_id = ?
 ORDER BY created_at DESC`,
		string(tenantID))
	if err != nil {
		return nil, fmt.Errorf("tenant: query api keys: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []tenant.ApiKey
	for rows.Next() {
		k, err := scanApiKeyRows(rows)
		if err != nil {
			return nil, err
		}
		k.Hashed = "" // 외부 노출 방지
		out = append(out, k)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("tenant: iter api keys: %w", err)
	}
	return out, nil
}

// Login은 tenant.Service.Login 구현입니다 (Stage D).
func (r *Repo) Login(ctx context.Context, tx storage.Tx, req tenant.LoginRequest) (tenant.LoginResult, error) {
	if req.TenantID == "" {
		return tenant.LoginResult{}, storage.ErrTenantMissing
	}
	if tx.TenantID() != "" && tx.TenantID() != req.TenantID {
		return tenant.LoginResult{}, fmt.Errorf("tenant: tx.TenantID()=%q != req.TenantID=%q",
			tx.TenantID(), req.TenantID)
	}

	user, err := r.GetUserByEmail(ctx, tx, req.TenantID, req.Email)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return tenant.LoginResult{}, tenant.ErrInvalidCredentials
		}
		return tenant.LoginResult{}, err
	}
	if user.Status != tenant.UserStatusActive {
		return tenant.LoginResult{}, tenant.ErrUserDisabled
	}
	if err := tenant.VerifyPassword(req.Password, user.PasswordHash); err != nil {
		return tenant.LoginResult{}, tenant.ErrInvalidCredentials
	}

	roles, err := r.GetUserRoles(ctx, tx, user.ID)
	if err != nil {
		return tenant.LoginResult{}, err
	}

	return r.issueTokens(ctx, tx, user, roles)
}

// issueTokens는 access + refresh 토큰을 발급하고 refresh를 DB에 INSERT합니다.
func (r *Repo) issueTokens(ctx context.Context, tx storage.Tx, user tenant.User, roles []tenant.Role) (tenant.LoginResult, error) {
	now := r.deps.Clock.Now().UTC()
	accessTTL := r.deps.AccessTTL
	if accessTTL <= 0 {
		accessTTL = tenant.DefaultAccessTTL
	}
	refreshTTL := r.deps.RefreshTTL
	if refreshTTL <= 0 {
		refreshTTL = tenant.DefaultRefreshTTL
	}

	roleNames := make([]string, len(roles))
	for i, role := range roles {
		roleNames[i] = role.Name
	}

	accessJTI := r.deps.IDGen.New("at")
	accessExp := now.Add(accessTTL)
	accessTok, err := tenant.SignAccessToken(r.deps.JWTPrivateKey, tenant.AccessClaims{
		Subject:   user.ID,
		TenantID:  user.TenantID,
		Roles:     roleNames,
		IssuedAt:  now,
		ExpiresAt: accessExp,
		JTI:       accessJTI,
	})
	if err != nil {
		return tenant.LoginResult{}, err
	}

	refreshJTI := r.deps.IDGen.New("rt")
	refreshExp := now.Add(refreshTTL)
	refreshTok, err := tenant.SignRefreshToken(r.deps.JWTPrivateKey, tenant.RefreshClaims{
		Subject:   user.ID,
		TenantID:  user.TenantID,
		IssuedAt:  now,
		ExpiresAt: refreshExp,
		JTI:       refreshJTI,
	})
	if err != nil {
		return tenant.LoginResult{}, err
	}

	if err := insertRefreshToken(ctx, tx, refreshJTI, user.ID, user.TenantID, refreshExp, now); err != nil {
		return tenant.LoginResult{}, err
	}

	return tenant.LoginResult{
		AccessToken:      accessTok,
		RefreshToken:     refreshTok,
		AccessExpiresAt:  accessExp,
		RefreshExpiresAt: refreshExp,
		User:             user,
		Roles:            roles,
	}, nil
}

// Refresh는 tenant.Service.Refresh 구현입니다.
//
// rotation: 기존 refresh의 revoked_at을 설정하고 새 access·refresh를 발급합니다.
// 이미 revoked된 refresh가 다시 들어오면 ErrRefreshRevoked + 해당 user의 모든 refresh 일괄 revoke
// (reuse detection — 탈취 신호).
func (r *Repo) Refresh(ctx context.Context, tx storage.Tx, refreshToken string) (tenant.LoginResult, error) {
	claims, err := tenant.ParseRefreshToken(r.deps.JWTPublicKey, refreshToken)
	if err != nil {
		return tenant.LoginResult{}, err
	}
	if tx.TenantID() != "" && tx.TenantID() != claims.TenantID {
		return tenant.LoginResult{}, fmt.Errorf("tenant: tx.TenantID()=%q != refresh.tid=%q",
			tx.TenantID(), claims.TenantID)
	}

	row, err := readRefreshToken(ctx, tx, claims.JTI)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return tenant.LoginResult{}, tenant.ErrRefreshNotFound
		}
		return tenant.LoginResult{}, err
	}
	if row.RevokedAt != nil {
		// C7 reuse detection — 같은 Tx에서 user의 모든 활성 refresh token을 일괄 revoke.
		// caller가 fn에서 nil 반환 시 cleanup commit, propagate 시 rollback (별도 Tx로 재호출 권장).
		now := r.deps.Clock.Now().UTC()
		_, _ = revokeAllRefreshForUser(ctx, tx, claims.TenantID, row.UserID, now)
		return tenant.LoginResult{}, tenant.ErrRefreshReuseDetected
	}
	if !row.ExpiresAt.After(r.deps.Clock.Now().UTC()) {
		return tenant.LoginResult{}, tenant.ErrRefreshExpired
	}

	// 기존 refresh revoke (rotation) — 새 발급 전에 atomic.
	if err := revokeRefresh(ctx, tx, claims.JTI, r.deps.Clock.Now().UTC()); err != nil {
		return tenant.LoginResult{}, err
	}

	user, err := r.getUserByID(ctx, tx, row.UserID)
	if err != nil {
		return tenant.LoginResult{}, err
	}
	roles, err := r.GetUserRoles(ctx, tx, user.ID)
	if err != nil {
		return tenant.LoginResult{}, err
	}
	return r.issueTokens(ctx, tx, user, roles)
}

// Logout은 tenant.Service.Logout 구현입니다 (멱등 revoke).
func (r *Repo) Logout(ctx context.Context, tx storage.Tx, refreshToken string) error {
	claims, err := tenant.ParseRefreshToken(r.deps.JWTPublicKey, refreshToken)
	if err != nil {
		return err
	}
	if tx.TenantID() != "" && tx.TenantID() != claims.TenantID {
		return fmt.Errorf("tenant: tx.TenantID()=%q != refresh.tid=%q",
			tx.TenantID(), claims.TenantID)
	}
	return revokeRefresh(ctx, tx, claims.JTI, r.deps.Clock.Now().UTC())
}

// VerifyAccessToken은 tenant.Service.VerifyAccessToken 구현입니다 (stateless).
func (r *Repo) VerifyAccessToken(ctx context.Context, accessToken string) (tenant.AccessClaims, error) {
	return tenant.ParseAccessToken(r.deps.JWTPublicKey, accessToken)
}

// getUserByID는 user ID로 user를 조회합니다 (Refresh 흐름에서 사용).
func (r *Repo) getUserByID(ctx context.Context, tx storage.Tx, userID string) (tenant.User, error) {
	row := tx.QueryRow(ctx, `
SELECT id, tenant_id, email, display_name, auth_provider, external_subject,
       password_hash, status, created_at, updated_at
  FROM users
 WHERE id = ?`, userID)

	var (
		id, tid, em                   string
		displayName                   sql.NullString
		provider                      string
		externalSubject, passwordHash sql.NullString
		status                        string
		createdStr, updatedStr        string
	)
	err := row.Scan(&id, &tid, &em, &displayName, &provider, &externalSubject,
		&passwordHash, &status, &createdStr, &updatedStr)
	if errors.Is(err, sql.ErrNoRows) {
		return tenant.User{}, storage.ErrNotFound
	}
	if err != nil {
		return tenant.User{}, fmt.Errorf("tenant: get user by id: %w", err)
	}
	createdAt, _ := time.Parse(time.RFC3339Nano, createdStr)
	updatedAt, _ := time.Parse(time.RFC3339Nano, updatedStr)
	return tenant.User{
		ID:              id,
		TenantID:        storage.TenantID(tid),
		Email:           em,
		DisplayName:     displayName.String,
		AuthProvider:    tenant.AuthProvider(provider),
		ExternalSubject: externalSubject.String,
		PasswordHash:    passwordHash.String,
		Status:          tenant.UserStatus(status),
		CreatedAt:       createdAt,
		UpdatedAt:       updatedAt,
	}, nil
}

// ----- refresh token DB 헬퍼 -----

type refreshRow struct {
	JTI       string
	UserID    string
	TenantID  storage.TenantID
	ExpiresAt time.Time
	RevokedAt *time.Time
}

func insertRefreshToken(ctx context.Context, tx storage.Tx, jti, userID string, tenantID storage.TenantID, expiresAt, now time.Time) error {
	_, err := tx.Exec(ctx, `
INSERT INTO auth_refresh_tokens (jti, user_id, tenant_id, expires_at, revoked_at, created_at)
VALUES (?, ?, ?, ?, NULL, ?)`,
		jti, userID, string(tenantID),
		expiresAt.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("tenant: insert refresh token: %w", err)
	}
	return nil
}

func readRefreshToken(ctx context.Context, tx storage.Tx, jti string) (refreshRow, error) {
	row := tx.QueryRow(ctx, `
SELECT jti, user_id, tenant_id, expires_at, revoked_at
  FROM auth_refresh_tokens
 WHERE jti = ?`, jti)

	var (
		j, uid, tid, expStr string
		revoked             sql.NullString
	)
	err := row.Scan(&j, &uid, &tid, &expStr, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return refreshRow{}, storage.ErrNotFound
	}
	if err != nil {
		return refreshRow{}, fmt.Errorf("tenant: read refresh token: %w", err)
	}
	exp, err := time.Parse(time.RFC3339Nano, expStr)
	if err != nil {
		return refreshRow{}, fmt.Errorf("tenant: parse refresh expires_at: %w", err)
	}
	out := refreshRow{
		JTI:       j,
		UserID:    uid,
		TenantID:  storage.TenantID(tid),
		ExpiresAt: exp,
	}
	if revoked.Valid {
		t, err := time.Parse(time.RFC3339Nano, revoked.String)
		if err != nil {
			return refreshRow{}, fmt.Errorf("tenant: parse refresh revoked_at: %w", err)
		}
		out.RevokedAt = &t
	}
	return out, nil
}

func revokeRefresh(ctx context.Context, tx storage.Tx, jti string, now time.Time) error {
	_, err := tx.Exec(ctx, `
UPDATE auth_refresh_tokens
   SET revoked_at = COALESCE(revoked_at, ?)
 WHERE jti = ?`,
		now.Format(time.RFC3339Nano), jti)
	if err != nil {
		return fmt.Errorf("tenant: revoke refresh: %w", err)
	}
	return nil
}

// RevokeAllRefreshForUser는 tenant.Service.RevokeAllRefreshForUser 구현입니다 (C7).
//
// (tenant_id, user_id) scope의 active(revoked_at IS NULL) refresh token만 갱신 — cross-tenant 격리.
// 반환은 새로 revoke된 row 수 (이미 revoked는 제외).
func (r *Repo) RevokeAllRefreshForUser(ctx context.Context, tx storage.Tx, tenantID storage.TenantID, userID string) (int, error) {
	if tenantID == "" {
		return 0, storage.ErrTenantMissing
	}
	if tx.TenantID() != "" && tx.TenantID() != tenantID {
		return 0, fmt.Errorf("tenant: tx.TenantID()=%q != tenantID=%q", tx.TenantID(), tenantID)
	}
	now := r.deps.Clock.Now().UTC()
	return revokeAllRefreshForUser(ctx, tx, tenantID, userID, now)
}

// revokeAllRefreshForUser는 한 (tenant, user)의 모든 active refresh token을 일괄 revoke합니다.
//
// SQLite는 RowsAffected를 지원하므로 새로 revoke된 count를 정확히 반환.
func revokeAllRefreshForUser(ctx context.Context, tx storage.Tx, tenantID storage.TenantID, userID string, now time.Time) (int, error) {
	res, err := tx.Exec(ctx, `
UPDATE auth_refresh_tokens
   SET revoked_at = ?
 WHERE tenant_id = ? AND user_id = ? AND revoked_at IS NULL`,
		now.Format(time.RFC3339Nano), string(tenantID), userID)
	if err != nil {
		return 0, fmt.Errorf("tenant: revoke all refresh for user: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		// SQLite는 항상 지원 — 오류는 무시 가능하지만 안전하게 0 반환.
		return 0, nil
	}
	return int(n), nil
}

// GetUserRoles는 tenant.Service.GetUserRoles 구현입니다 (scope 정보 없는 평탄 슬라이스).
//
// 호환 보존 — 새 코드는 GetUserRoleBindings 권장.
func (r *Repo) GetUserRoles(ctx context.Context, tx storage.Tx, userID string) ([]tenant.Role, error) {
	rows, err := tx.Query(ctx, `
SELECT r.id, r.tenant_id, r.name, r.permissions, r.is_system, r.created_at
  FROM roles r
  JOIN user_roles ur ON ur.role_id = r.id
 WHERE ur.user_id = ?`,
		userID)
	if err != nil {
		return nil, fmt.Errorf("tenant: query user roles: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []tenant.Role
	for rows.Next() {
		role, err := scanRoleRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, role)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("tenant: iter user roles: %w", err)
	}
	return out, nil
}

// GetUserRoleBindings는 tenant.Service.GetUserRoleBindings 구현입니다 — 세분 RBAC Stage 2.
//
// user_roles JOIN roles → 각 row에서 (Role, scope_type, scope_id)를 RoleBinding 으로 복원.
// 0028 이전 INSERT된 row는 DEFAULT 'tenant'/” — 자동으로 tenant scope binding으로 분류됩니다.
//
// scope_id의 빈 문자열은 ScopeID="" 그대로 — fleet scope binding은 scope_id가 fleet ID.
func (r *Repo) GetUserRoleBindings(ctx context.Context, tx storage.Tx, userID string) ([]tenant.RoleBinding, error) {
	rows, err := tx.Query(ctx, `
SELECT r.id, r.tenant_id, r.name, r.permissions, r.is_system, r.created_at,
       ur.scope_type, ur.scope_id
  FROM roles r
  JOIN user_roles ur ON ur.role_id = r.id
 WHERE ur.user_id = ?`,
		userID)
	if err != nil {
		return nil, fmt.Errorf("tenant: query user role bindings: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []tenant.RoleBinding
	for rows.Next() {
		var (
			id, tid, name, permsJSON, createdStr string
			isSystem                             int
			scopeType, scopeID                   string
		)
		if err := rows.Scan(&id, &tid, &name, &permsJSON, &isSystem, &createdStr,
			&scopeType, &scopeID); err != nil {
			return nil, fmt.Errorf("tenant: scan role binding row: %w", err)
		}
		role, err := assembleRole(id, tid, name, permsJSON, isSystem, createdStr)
		if err != nil {
			return nil, err
		}
		out = append(out, tenant.RoleBinding{
			Role:      role,
			ScopeType: tenant.ScopeType(scopeType),
			ScopeID:   scopeID,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("tenant: iter user role bindings: %w", err)
	}
	return out, nil
}

// GetTenant는 tenant.Service.GetTenant 구현입니다.
func (r *Repo) GetTenant(ctx context.Context, tx storage.Tx, id storage.TenantID) (tenant.Tenant, error) {
	row := tx.QueryRow(ctx, `
SELECT id, name, plan, created_at, settings, features, retention
  FROM tenants
 WHERE id = ?`, string(id))

	var (
		idStr, name, plan, createdStr    string
		settingsStr, featuresStr, retStr string
	)
	err := row.Scan(&idStr, &name, &plan, &createdStr, &settingsStr, &featuresStr, &retStr)
	if errors.Is(err, sql.ErrNoRows) {
		return tenant.Tenant{}, storage.ErrNotFound
	}
	if err != nil {
		return tenant.Tenant{}, fmt.Errorf("tenant: read tenant: %w", err)
	}
	createdAt, err := time.Parse(time.RFC3339Nano, createdStr)
	if err != nil {
		return tenant.Tenant{}, fmt.Errorf("tenant: parse created_at: %w", err)
	}
	return tenant.Tenant{
		ID:        storage.TenantID(idStr),
		Name:      name,
		Plan:      tenant.Plan(plan),
		CreatedAt: createdAt,
		Settings:  json.RawMessage(settingsStr),
		Features:  json.RawMessage(featuresStr),
		Retention: json.RawMessage(retStr),
	}, nil
}

// ProvisionExternalUser는 SSO IdP 첫 로그인 시 user를 자동 생성합니다 (O5 — Phase 4).
//
// 같은 (tenantID, email) user가 이미 있으면 link 모드 — 그 user.ID 반환 (role 변경 X).
// 없으면 새 user INSERT (auth_provider=oidc/saml, ExternalSubject) + DefaultRole 자동 할당.
//
// 본 메서드는 IdentityResolver 어댑터(bootstrap)가 호출 — sso.CompleteLogin 같은 Tx.
func (r *Repo) ProvisionExternalUser(ctx context.Context, tx storage.Tx, req tenant.ProvisionExternalUserRequest) (tenant.User, error) {
	if req.TenantID == "" {
		return tenant.User{}, storage.ErrTenantMissing
	}
	emailNormalized := strings.ToLower(strings.TrimSpace(req.Email))
	if emailNormalized == "" {
		return tenant.User{}, tenant.ErrEmptyEmail
	}
	if req.AuthProvider != tenant.AuthProviderOIDC && req.AuthProvider != tenant.AuthProviderSAML {
		return tenant.User{}, fmt.Errorf("tenant: ProvisionExternalUser: AuthProvider must be oidc or saml, got %q", req.AuthProvider)
	}

	// 1. 기존 user lookup (link 모드).
	existing, err := r.GetUserByEmail(ctx, tx, req.TenantID, emailNormalized)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, storage.ErrNotFound) {
		return tenant.User{}, fmt.Errorf("tenant: lookup external user: %w", err)
	}

	// 2. 신규 user INSERT.
	roleName := req.DefaultRoleName
	if roleName == "" {
		roleName = tenant.RoleOperator
	}
	role, err := r.GetRole(ctx, tx, req.TenantID, roleName)
	if err != nil {
		return tenant.User{}, fmt.Errorf("tenant: lookup default role %q: %w", roleName, err)
	}

	now := r.deps.Clock.Now().UTC()
	user := tenant.User{
		ID:              r.deps.IDGen.New("us"),
		TenantID:        req.TenantID,
		Email:           emailNormalized,
		DisplayName:     req.DisplayName,
		AuthProvider:    req.AuthProvider,
		ExternalSubject: req.ExternalSubject,
		PasswordHash:    "", // 외부 IdP 전용 — local password 없음.
		Status:          tenant.UserStatusActive,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := insertUser(ctx, tx, user); err != nil {
		return tenant.User{}, err
	}
	if err := assignRole(ctx, tx, user.ID, role.ID); err != nil {
		return tenant.User{}, err
	}
	return user, nil
}

// GetUserByEmail은 tenant.Service.GetUserByEmail 구현입니다.
func (r *Repo) GetUserByEmail(ctx context.Context, tx storage.Tx, tenantID storage.TenantID, email string) (tenant.User, error) {
	normalized := strings.ToLower(strings.TrimSpace(email))
	row := tx.QueryRow(ctx, `
SELECT id, tenant_id, email, display_name, auth_provider, external_subject,
       password_hash, status, created_at, updated_at
  FROM users
 WHERE tenant_id = ? AND email = ?`,
		string(tenantID), normalized)

	var (
		id, tid, em                   string
		displayName                   sql.NullString
		provider                      string
		externalSubject, passwordHash sql.NullString
		status                        string
		createdStr, updatedStr        string
	)
	err := row.Scan(&id, &tid, &em, &displayName, &provider, &externalSubject,
		&passwordHash, &status, &createdStr, &updatedStr)
	if errors.Is(err, sql.ErrNoRows) {
		return tenant.User{}, storage.ErrNotFound
	}
	if err != nil {
		return tenant.User{}, fmt.Errorf("tenant: read user: %w", err)
	}

	createdAt, _ := time.Parse(time.RFC3339Nano, createdStr)
	updatedAt, _ := time.Parse(time.RFC3339Nano, updatedStr)

	return tenant.User{
		ID:              id,
		TenantID:        storage.TenantID(tid),
		Email:           em,
		DisplayName:     displayName.String,
		AuthProvider:    tenant.AuthProvider(provider),
		ExternalSubject: externalSubject.String,
		PasswordHash:    passwordHash.String,
		Status:          tenant.UserStatus(status),
		CreatedAt:       createdAt,
		UpdatedAt:       updatedAt,
	}, nil
}

// ----- 내부 헬퍼 -----

func insertTenant(ctx context.Context, tx storage.Tx, t tenant.Tenant) error {
	_, err := tx.Exec(ctx, `
INSERT INTO tenants (id, name, plan, created_at, settings, features, retention)
VALUES (?, ?, ?, ?, ?, ?, ?)`,
		string(t.ID), t.Name, string(t.Plan), t.CreatedAt.Format(time.RFC3339Nano),
		string(t.Settings), string(t.Features), string(t.Retention))
	if err != nil {
		return fmt.Errorf("tenant: insert tenant: %w", err)
	}
	return nil
}

func insertUser(ctx context.Context, tx storage.Tx, u tenant.User) error {
	var (
		displayName     *string
		externalSubject *string
		passwordHash    *string
	)
	if u.DisplayName != "" {
		displayName = &u.DisplayName
	}
	if u.ExternalSubject != "" {
		externalSubject = &u.ExternalSubject
	}
	if u.PasswordHash != "" {
		passwordHash = &u.PasswordHash
	}

	_, err := tx.Exec(ctx, `
INSERT INTO users (
    id, tenant_id, email, display_name, auth_provider, external_subject,
    password_hash, status, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		u.ID, string(u.TenantID), u.Email, displayName,
		string(u.AuthProvider), externalSubject, passwordHash, string(u.Status),
		u.CreatedAt.Format(time.RFC3339Nano), u.UpdatedAt.Format(time.RFC3339Nano))
	if err != nil {
		// SQLite UNIQUE 위반 → ErrEmailAlreadyExists.
		if isUniqueViolation(err) {
			return tenant.ErrEmailAlreadyExists
		}
		return fmt.Errorf("tenant: insert user: %w", err)
	}
	return nil
}

func insertRole(ctx context.Context, tx storage.Tx, role tenant.Role) error {
	permsJSON, err := json.Marshal(role.Permissions)
	if err != nil {
		return fmt.Errorf("tenant: marshal permissions: %w", err)
	}
	isSystem := 0
	if role.IsSystem {
		isSystem = 1
	}
	_, err = tx.Exec(ctx, `
INSERT INTO roles (id, tenant_id, name, permissions, is_system, created_at)
VALUES (?, ?, ?, ?, ?, ?)`,
		role.ID, string(role.TenantID), role.Name, string(permsJSON), isSystem,
		role.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("tenant: insert role %q: %w", role.Name, err)
	}
	return nil
}

// assignRole은 멱등 INSERT (동일 (user, role)이 있으면 무시) — tenant scope.
//
// 0028 마이그레이션 이후 user_roles는 scope_type/scope_id 컬럼을 가집니다 — DEFAULT 'tenant'/”
// 로 자동 채움이 보장되지만, 명시적으로 'tenant'/” 를 INSERT하여 코드 의도를 분명히 합니다.
func assignRole(ctx context.Context, tx storage.Tx, userID, roleID string) error {
	return assignRoleScoped(ctx, tx, userID, roleID, tenant.ScopeTenant, "")
}

// assignRoleScoped는 멱등 INSERT (동일 (user, role) PK가 있으면 무시) — 세분 RBAC Stage 2.
//
// scopeType=ScopeTenant이면 scopeID는 빈 문자열로 정규화 — DB 일관성 + INDEX cover 보존.
func assignRoleScoped(ctx context.Context, tx storage.Tx, userID, roleID string, scopeType tenant.ScopeType, scopeID string) error {
	if scopeType == "" {
		scopeType = tenant.ScopeTenant
	}
	if scopeType == tenant.ScopeTenant {
		scopeID = "" // tenant scope는 scope_id 무의미 — 빈 값 강제.
	}
	_, err := tx.Exec(ctx,
		`INSERT INTO user_roles (user_id, role_id, scope_type, scope_id) VALUES (?, ?, ?, ?)
		 ON CONFLICT (user_id, role_id) DO NOTHING`,
		userID, roleID, string(scopeType), scopeID)
	if err != nil {
		return fmt.Errorf("tenant: assign role scoped: %w", err)
	}
	return nil
}

// scanRoleRow는 *sql.Row → Role.
func scanRoleRow(row *sql.Row) (tenant.Role, error) {
	var (
		id, tid, name, permsJSON, createdStr string
		isSystem                             int
	)
	err := row.Scan(&id, &tid, &name, &permsJSON, &isSystem, &createdStr)
	if errors.Is(err, sql.ErrNoRows) {
		return tenant.Role{}, storage.ErrNotFound
	}
	if err != nil {
		return tenant.Role{}, fmt.Errorf("tenant: scan role: %w", err)
	}
	return assembleRole(id, tid, name, permsJSON, isSystem, createdStr)
}

// scanRoleRows는 *sql.Rows → Role (반복 호출).
func scanRoleRows(rows *sql.Rows) (tenant.Role, error) {
	var (
		id, tid, name, permsJSON, createdStr string
		isSystem                             int
	)
	if err := rows.Scan(&id, &tid, &name, &permsJSON, &isSystem, &createdStr); err != nil {
		return tenant.Role{}, fmt.Errorf("tenant: scan role row: %w", err)
	}
	return assembleRole(id, tid, name, permsJSON, isSystem, createdStr)
}

func assembleRole(id, tid, name, permsJSON string, isSystem int, createdStr string) (tenant.Role, error) {
	var perms []tenant.Permission
	if err := json.Unmarshal([]byte(permsJSON), &perms); err != nil {
		return tenant.Role{}, fmt.Errorf("tenant: unmarshal permissions for role %q: %w", name, err)
	}
	createdAt, err := time.Parse(time.RFC3339Nano, createdStr)
	if err != nil {
		return tenant.Role{}, fmt.Errorf("tenant: parse role created_at: %w", err)
	}
	return tenant.Role{
		ID:          id,
		TenantID:    storage.TenantID(tid),
		Name:        name,
		Permissions: perms,
		IsSystem:    isSystem == 1,
		CreatedAt:   createdAt,
	}, nil
}

func scanApiKeyRow(row *sql.Row) (tenant.ApiKey, error) {
	var s apiKeyScan
	err := row.Scan(&s.id, &s.tenantID, &s.name, &s.prefix, &s.hashed, &s.scopesJSON,
		&s.expiresAt, &s.lastUsedAt, &s.createdBy, &s.createdAt, &s.revokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return tenant.ApiKey{}, storage.ErrNotFound
	}
	if err != nil {
		return tenant.ApiKey{}, fmt.Errorf("tenant: scan api key: %w", err)
	}
	return assembleApiKey(s)
}

func scanApiKeyRows(rows *sql.Rows) (tenant.ApiKey, error) {
	var s apiKeyScan
	if err := rows.Scan(&s.id, &s.tenantID, &s.name, &s.prefix, &s.hashed, &s.scopesJSON,
		&s.expiresAt, &s.lastUsedAt, &s.createdBy, &s.createdAt, &s.revokedAt); err != nil {
		return tenant.ApiKey{}, fmt.Errorf("tenant: scan api key row: %w", err)
	}
	return assembleApiKey(s)
}

type apiKeyScan struct {
	id, tenantID, name, prefix, hashed string
	scopesJSON                         string
	expiresAt, lastUsedAt              sql.NullString
	createdBy, createdAt               string
	revokedAt                          sql.NullString
}

func assembleApiKey(s apiKeyScan) (tenant.ApiKey, error) {
	var scopes []tenant.Permission
	if err := json.Unmarshal([]byte(s.scopesJSON), &scopes); err != nil {
		return tenant.ApiKey{}, fmt.Errorf("tenant: unmarshal api key scopes: %w", err)
	}
	createdAt, err := time.Parse(time.RFC3339Nano, s.createdAt)
	if err != nil {
		return tenant.ApiKey{}, fmt.Errorf("tenant: parse api key created_at: %w", err)
	}

	out := tenant.ApiKey{
		ID:        s.id,
		TenantID:  storage.TenantID(s.tenantID),
		Name:      s.name,
		Prefix:    s.prefix,
		Hashed:    s.hashed,
		Scopes:    scopes,
		CreatedBy: s.createdBy,
		CreatedAt: createdAt,
	}
	if s.expiresAt.Valid {
		t, err := time.Parse(time.RFC3339Nano, s.expiresAt.String)
		if err != nil {
			return tenant.ApiKey{}, fmt.Errorf("tenant: parse api key expires_at: %w", err)
		}
		out.ExpiresAt = &t
	}
	if s.lastUsedAt.Valid {
		t, err := time.Parse(time.RFC3339Nano, s.lastUsedAt.String)
		if err != nil {
			return tenant.ApiKey{}, fmt.Errorf("tenant: parse api key last_used_at: %w", err)
		}
		out.LastUsedAt = &t
	}
	if s.revokedAt.Valid {
		t, err := time.Parse(time.RFC3339Nano, s.revokedAt.String)
		if err != nil {
			return tenant.ApiKey{}, fmt.Errorf("tenant: parse api key revoked_at: %w", err)
		}
		out.RevokedAt = &t
	}
	return out, nil
}

func validateCreate(req tenant.CreateRequest) error {
	if strings.TrimSpace(req.Name) == "" {
		return tenant.ErrEmptyName
	}
	if strings.TrimSpace(req.AdminEmail) == "" {
		return tenant.ErrEmptyEmail
	}
	if _, err := mail.ParseAddress(req.AdminEmail); err != nil {
		return tenant.ErrInvalidEmail
	}
	if req.AdminPassword == "" {
		return tenant.ErrEmptyPassword
	}
	if len(req.AdminPassword) < 12 {
		return tenant.ErrPasswordTooShort
	}
	return nil
}

func validPlan(p tenant.Plan) bool {
	switch p {
	case tenant.PlanDesktopFree, tenant.PlanDesktopPro, tenant.PlanEnterprise, tenant.PlanAppliance:
		return true
	}
	return false
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "constraint failed: UNIQUE")
}
