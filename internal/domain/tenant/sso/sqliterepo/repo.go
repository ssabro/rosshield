// Package sqliterepo는 sso.Service의 SQLite 어댑터입니다 (E20-A).
//
// 본 stage 책임:
//
//	Provider CRUD          → sso_providers INSERT/UPDATE/DELETE/SELECT + audit emit.
//	StartLogin             → sso_login_attempts INSERT + audit emit (state·PKCE·nonce·RelayState 영속).
//	CompleteLogin          → state lookup + 만료/재사용 검증 + completed_at 마킹 + audit emit.
//	UpsertExternalIdentity → sso_external_identities INSERT 또는 last_seen_at·email 갱신.
//
// IdP HTTP 호출(OIDC discovery·token exchange / SAML AuthnRequest 생성·assertion 검증)은 본 패키지 범위 외 —
// E20-B(OIDC) / E20-C(SAML) 후속 stage에서 별도 application service가 본 Repo를 사용.
//
// 멀티테넌시 (P4):
//
//	모든 표면이 tx.TenantID()로 진입 — cross-tenant lookup은 ErrProviderNotFound로 마스킹.
package sqliterepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ssabro/rosshield/internal/domain/tenant/sso"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/idgen"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

const rfc3339Nano = time.RFC3339Nano

// Deps는 어댑터 의존성입니다.
//
// OIDC (E20-B):
//
//	OIDC == nil이면 본 어댑터는 OIDC provider에 대해 AuthURL을 빌드/검증하지 않고
//	이전 stage(E20-A)의 stub 동작을 유지합니다 — 테스트 호환성·옵트인(P10).
//	실제 IdP 통합은 OIDC=NewOIDCClient() 또는 mock client를 주입.
//
// IdentityResolver (E20-B):
//
//	CompleteLogin이 id_token에서 sub/email을 추출한 후, 이 sub를 내부 user.id로 매핑하는 책임은
//	tenant 도메인(또는 application service)에 위임합니다. nil이면 sso.UpsertExternalIdentity의
//	UserID는 빈 값 유지 — handler가 후속 처리 또는 200 응답으로 우회.
type Deps struct {
	Clock            clock.Clock
	IDGen            idgen.IDGen
	Audit            sso.AuditEmitter
	OIDC             *sso.OIDCClient  // E20-B — nil이면 OIDC 통합 비활성(stub 흐름 유지).
	IdentityResolver IdentityResolver // E20-B — nil이면 user 매핑 X(외부 identity 영속만).
}

// IdentityResolver는 id_token claims를 받아 내부 user.ID를 반환합니다 (E20-B).
//
// 구현 가이드(후속 stage·application layer):
//
//  1. (tenant, externalSubject) → users.external_subject 일치 user 조회.
//  2. 없으면 (tenant, email) → 기존 user 매칭(이메일 기준 link).
//  3. 그래도 없으면 자동 프로비저닝(R20 정책 결정 후) — 본 stage는 NOT FOUND 시 빈 ID 허용.
//
// nil 반환 또는 빈 ID는 "user 매핑 X — external identity만 영속" 의미.
type IdentityResolver interface {
	ResolveOIDCIdentity(ctx context.Context, tx storage.Tx, tenantID storage.TenantID, providerID string, claims sso.IDTokenClaims) (userID string, err error)
}

// Repo는 sso.Service의 SQLite 구현입니다.
type Repo struct {
	deps Deps
}

// New는 새 Repo를 반환합니다.
func New(deps Deps) *Repo {
	return &Repo{deps: deps}
}

// CreateProvider는 sso.Service.CreateProvider 구현입니다.
func (r *Repo) CreateProvider(ctx context.Context, tx storage.Tx, req sso.CreateProviderRequest) (sso.Provider, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return sso.Provider{}, storage.ErrTenantMissing
	}
	if !sso.IsValidType(req.Type) {
		return sso.Provider{}, sso.ErrUnsupportedType
	}
	if strings.TrimSpace(req.Name) == "" {
		return sso.Provider{}, sso.ErrEmptyName
	}
	if len(req.Config) == 0 {
		return sso.Provider{}, sso.ErrEmptyConfig
	}
	if !json.Valid(req.Config) {
		return sso.Provider{}, sso.ErrEmptyConfig
	}

	now := r.deps.Clock.Now().UTC()
	p := sso.Provider{
		ID:        r.deps.IDGen.New("ssop"),
		TenantID:  tenantID,
		Type:      req.Type,
		Name:      strings.TrimSpace(req.Name),
		Enabled:   req.Enabled,
		Config:    append(json.RawMessage(nil), req.Config...),
		CreatedAt: now,
		UpdatedAt: now,
	}

	enabledInt := 0
	if p.Enabled {
		enabledInt = 1
	}
	_, err := tx.Exec(ctx, `INSERT INTO sso_providers
(id, tenant_id, type, name, enabled, config_json, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, string(p.TenantID), string(p.Type), p.Name, enabledInt, string(p.Config),
		p.CreatedAt.Format(rfc3339Nano), p.UpdatedAt.Format(rfc3339Nano),
	)
	if err != nil {
		if isUniqueViolation(err) {
			return sso.Provider{}, sso.ErrProviderNameExists
		}
		return sso.Provider{}, fmt.Errorf("sso: insert provider: %w", err)
	}
	if err := r.deps.Audit.EmitProviderChanged(ctx, tx, p, "created"); err != nil {
		return sso.Provider{}, fmt.Errorf("sso: emit provider.created: %w", err)
	}
	return p, nil
}

// UpdateProvider는 sso.Service.UpdateProvider 구현입니다.
func (r *Repo) UpdateProvider(ctx context.Context, tx storage.Tx, req sso.UpdateProviderRequest) (sso.Provider, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return sso.Provider{}, storage.ErrTenantMissing
	}
	p, err := r.GetProvider(ctx, tx, req.ID)
	if err != nil {
		return sso.Provider{}, err
	}
	if req.Name != nil {
		n := strings.TrimSpace(*req.Name)
		if n == "" {
			return sso.Provider{}, sso.ErrEmptyName
		}
		p.Name = n
	}
	if req.Enabled != nil {
		p.Enabled = *req.Enabled
	}
	if req.Config != nil {
		if len(req.Config) == 0 || !json.Valid(req.Config) {
			return sso.Provider{}, sso.ErrEmptyConfig
		}
		p.Config = append(json.RawMessage(nil), req.Config...)
	}
	p.UpdatedAt = r.deps.Clock.Now().UTC()

	enabledInt := 0
	if p.Enabled {
		enabledInt = 1
	}
	res, err := tx.Exec(ctx, `UPDATE sso_providers
SET name = ?, enabled = ?, config_json = ?, updated_at = ?
WHERE id = ? AND tenant_id = ?`,
		p.Name, enabledInt, string(p.Config), p.UpdatedAt.Format(rfc3339Nano),
		p.ID, string(p.TenantID),
	)
	if err != nil {
		if isUniqueViolation(err) {
			return sso.Provider{}, sso.ErrProviderNameExists
		}
		return sso.Provider{}, fmt.Errorf("sso: update provider: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sso.Provider{}, sso.ErrProviderNotFound
	}
	if err := r.deps.Audit.EmitProviderChanged(ctx, tx, p, "updated"); err != nil {
		return sso.Provider{}, fmt.Errorf("sso: emit provider.updated: %w", err)
	}
	return p, nil
}

// DeleteProvider는 sso.Service.DeleteProvider 구현입니다.
func (r *Repo) DeleteProvider(ctx context.Context, tx storage.Tx, providerID string) error {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return storage.ErrTenantMissing
	}
	p, err := r.GetProvider(ctx, tx, providerID)
	if err != nil {
		return err
	}
	res, err := tx.Exec(ctx, `DELETE FROM sso_providers WHERE id = ? AND tenant_id = ?`,
		providerID, string(tenantID),
	)
	if err != nil {
		return fmt.Errorf("sso: delete provider: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sso.ErrProviderNotFound
	}
	if err := r.deps.Audit.EmitProviderChanged(ctx, tx, p, "deleted"); err != nil {
		return fmt.Errorf("sso: emit provider.deleted: %w", err)
	}
	return nil
}

// GetProvider는 sso.Service.GetProvider 구현입니다.
func (r *Repo) GetProvider(ctx context.Context, tx storage.Tx, providerID string) (sso.Provider, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return sso.Provider{}, storage.ErrTenantMissing
	}
	row := tx.QueryRow(ctx, `SELECT id, tenant_id, type, name, enabled, config_json, created_at, updated_at
FROM sso_providers WHERE id = ?`, providerID)
	p, err := scanProvider(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return sso.Provider{}, sso.ErrProviderNotFound
		}
		return sso.Provider{}, err
	}
	if p.TenantID != tenantID {
		return sso.Provider{}, sso.ErrProviderNotFound // cross-tenant 마스킹
	}
	return p, nil
}

// ListProviders는 sso.Service.ListProviders 구현입니다.
func (r *Repo) ListProviders(ctx context.Context, tx storage.Tx) ([]sso.Provider, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return nil, storage.ErrTenantMissing
	}
	rows, err := tx.Query(ctx, `SELECT id, tenant_id, type, name, enabled, config_json, created_at, updated_at
FROM sso_providers WHERE tenant_id = ? ORDER BY created_at ASC`, string(tenantID))
	if err != nil {
		return nil, fmt.Errorf("sso: list providers: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []sso.Provider
	for rows.Next() {
		p, err := scanProvider(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// StartLogin은 sso.Service.StartLogin 구현입니다.
//
// E20-B 흐름 (OIDC):
//
//  1. provider 검증 + state·PKCE·nonce 생성.
//  2. login_attempt INSERT.
//  3. OIDC client(주입돼 있으면)로 BuildAuthURL → AuthURL 채워 반환.
//  4. OIDC client nil이거나 SAML이면 AuthURL은 빈 값(stub) — 후속 stage(E20-C)에서 SAML AuthnRequest.
//
// IdP HTTP 실패는 sso.ErrIdPHTTP로 감싸 전파 — handler에서 502 매핑.
func (r *Repo) StartLogin(ctx context.Context, tx storage.Tx, req sso.StartLoginRequest) (sso.StartLoginResult, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return sso.StartLoginResult{}, storage.ErrTenantMissing
	}
	p, err := r.GetProvider(ctx, tx, req.ProviderID)
	if err != nil {
		return sso.StartLoginResult{}, err
	}
	if !p.Enabled {
		return sso.StartLoginResult{}, sso.ErrProviderDisabled
	}

	state, err := newRandomToken(32)
	if err != nil {
		return sso.StartLoginResult{}, fmt.Errorf("sso: gen state: %w", err)
	}

	attempt := sso.LoginAttempt{
		ID:         r.deps.IDGen.New("ssoa"),
		TenantID:   tenantID,
		ProviderID: p.ID,
		State:      state,
		CreatedAt:  r.deps.Clock.Now().UTC(),
		ExpiresAt:  r.deps.Clock.Now().UTC().Add(sso.DefaultAttemptTTL),
	}
	switch p.Type {
	case sso.TypeOIDC:
		verifier, err := newRandomToken(64) // PKCE code_verifier (RFC 7636: 43~128 chars)
		if err != nil {
			return sso.StartLoginResult{}, fmt.Errorf("sso: gen pkce: %w", err)
		}
		nonce, err := newRandomToken(32)
		if err != nil {
			return sso.StartLoginResult{}, fmt.Errorf("sso: gen nonce: %w", err)
		}
		attempt.PKCEVerifier = verifier
		attempt.Nonce = nonce
	case sso.TypeSAML:
		attempt.RelayState = req.RedirectAfter
	}

	_, err = tx.Exec(ctx, `INSERT INTO sso_login_attempts
(id, tenant_id, provider_id, state, pkce_verifier, nonce, relay_state, created_at, expires_at, completed_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NULL)`,
		attempt.ID, string(attempt.TenantID), attempt.ProviderID, attempt.State,
		nullableString(attempt.PKCEVerifier), nullableString(attempt.Nonce), nullableString(attempt.RelayState),
		attempt.CreatedAt.Format(rfc3339Nano), attempt.ExpiresAt.Format(rfc3339Nano),
	)
	if err != nil {
		return sso.StartLoginResult{}, fmt.Errorf("sso: insert login_attempt: %w", err)
	}
	if err := r.deps.Audit.EmitLoginStarted(ctx, tx, attempt); err != nil {
		return sso.StartLoginResult{}, fmt.Errorf("sso: emit login.started: %w", err)
	}

	// E20-B — OIDC client가 주입되어 있고 provider가 OIDC면 IdP discovery + AuthURL 빌드.
	authURL := ""
	if p.Type == sso.TypeOIDC && r.deps.OIDC != nil {
		cfg, perr := sso.ParseOIDCConfig(p.Config)
		if perr != nil {
			return sso.StartLoginResult{}, perr
		}
		u, berr := r.deps.OIDC.BuildAuthURL(ctx, cfg, attempt.State, attempt.PKCEVerifier, attempt.Nonce)
		if berr != nil {
			return sso.StartLoginResult{}, berr
		}
		authURL = u
	}

	return sso.StartLoginResult{
		AuthURL: authURL,
		State:   state,
		Attempt: attempt,
	}, nil
}

// CompleteLogin은 sso.Service.CompleteLogin 구현입니다.
//
// E20-B 흐름 (OIDC):
//
//  1. state 검증 + 만료/재사용 체크 + completed_at 마킹.
//  2. provider 조회 → OIDC client 주입돼 있으면 ExchangeCode + VerifyIDToken.
//  3. id_token claims에서 sub/email 추출 → ExternalIdentity 빌드.
//  4. IdentityResolver 주입돼 있으면 user.ID 매핑 → UpsertExternalIdentity 호출.
//  5. audit emit + Identity 반환.
//
// OIDC client·IdentityResolver 미주입 시 (E20-A 호환):
//
//	state 검증 + completed_at 마킹만 수행, Identity 빈 채로 반환.
func (r *Repo) CompleteLogin(ctx context.Context, tx storage.Tx, req sso.CompleteLoginRequest) (sso.CompleteLoginResult, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return sso.CompleteLoginResult{}, storage.ErrTenantMissing
	}
	if strings.TrimSpace(req.State) == "" {
		return sso.CompleteLoginResult{}, sso.ErrEmptyState
	}

	attempt, err := r.findAttemptByState(ctx, tx, req.State, tenantID)
	if err != nil {
		return sso.CompleteLoginResult{}, err
	}
	if attempt.IsCompleted() {
		return sso.CompleteLoginResult{}, sso.ErrInvalidState
	}
	now := r.deps.Clock.Now().UTC()
	if attempt.IsExpired(now) {
		return sso.CompleteLoginResult{}, sso.ErrStateExpired
	}

	// completed_at 마킹 — 같은 state 두 번째 호출은 위 IsCompleted에서 거부.
	_, err = tx.Exec(ctx, `UPDATE sso_login_attempts SET completed_at = ?
WHERE id = ? AND tenant_id = ? AND completed_at IS NULL`,
		now.Format(rfc3339Nano), attempt.ID, string(tenantID),
	)
	if err != nil {
		return sso.CompleteLoginResult{}, fmt.Errorf("sso: mark completed: %w", err)
	}
	completed := now
	attempt.CompletedAt = &completed

	identity := sso.ExternalIdentity{}

	// E20-B — OIDC client + provider type OIDC + code 입력 → token 교환 + id_token 검증.
	if r.deps.OIDC != nil && req.Code != "" {
		p, perr := r.GetProvider(ctx, tx, attempt.ProviderID)
		if perr != nil {
			r.emitCompleted(ctx, tx, attempt, identity, false)
			return sso.CompleteLoginResult{}, perr
		}
		if p.Type != sso.TypeOIDC {
			r.emitCompleted(ctx, tx, attempt, identity, false)
			return sso.CompleteLoginResult{}, sso.ErrIdPMismatch
		}
		cfg, cerr := sso.ParseOIDCConfig(p.Config)
		if cerr != nil {
			r.emitCompleted(ctx, tx, attempt, identity, false)
			return sso.CompleteLoginResult{}, cerr
		}
		idToken, _, xerr := r.deps.OIDC.ExchangeCode(ctx, cfg, req.Code, attempt.PKCEVerifier)
		if xerr != nil {
			r.emitCompleted(ctx, tx, attempt, identity, false)
			return sso.CompleteLoginResult{}, xerr
		}
		claims, verr := r.deps.OIDC.VerifyIDToken(ctx, cfg, idToken, attempt.Nonce)
		if verr != nil {
			r.emitCompleted(ctx, tx, attempt, identity, false)
			return sso.CompleteLoginResult{}, verr
		}

		// IdentityResolver 주입 시 → user.ID 매핑.
		userID := ""
		if r.deps.IdentityResolver != nil {
			uid, rerr := r.deps.IdentityResolver.ResolveOIDCIdentity(ctx, tx, tenantID, p.ID, claims)
			if rerr != nil {
				r.emitCompleted(ctx, tx, attempt, identity, false)
				return sso.CompleteLoginResult{}, rerr
			}
			userID = uid
		}

		identity = sso.ExternalIdentity{
			ProviderID:      p.ID,
			ExternalSubject: claims.Subject,
			TenantID:        tenantID,
			UserID:          userID,
			Email:           claims.Email,
		}
		// userID가 비어 있으면 FK 제약(users.id) 때문에 UpsertExternalIdentity는 실패 가능 —
		// 본 stage는 안전하게 user 매핑이 결정된 경우만 영속.
		if userID != "" {
			persisted, uerr := r.UpsertExternalIdentity(ctx, tx, identity)
			if uerr != nil {
				r.emitCompleted(ctx, tx, attempt, identity, false)
				return sso.CompleteLoginResult{}, uerr
			}
			identity = persisted
		}
	}

	if err := r.deps.Audit.EmitLoginCompleted(ctx, tx, attempt, identity, true); err != nil {
		return sso.CompleteLoginResult{}, fmt.Errorf("sso: emit login.completed: %w", err)
	}
	return sso.CompleteLoginResult{Identity: identity}, nil
}

// emitCompleted는 실패 경로 audit emit helper입니다 (errors는 무시 — 실패 propagation 우선).
func (r *Repo) emitCompleted(ctx context.Context, tx storage.Tx, a sso.LoginAttempt, id sso.ExternalIdentity, ok bool) {
	_ = r.deps.Audit.EmitLoginCompleted(ctx, tx, a, id, ok)
}

// UpsertExternalIdentity는 sso.Service.UpsertExternalIdentity 구현입니다.
//
// (provider_id, external_subject) PK — INSERT 또는 last_seen_at·email 갱신.
func (r *Repo) UpsertExternalIdentity(ctx context.Context, tx storage.Tx, identity sso.ExternalIdentity) (sso.ExternalIdentity, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return sso.ExternalIdentity{}, storage.ErrTenantMissing
	}
	if strings.TrimSpace(identity.ExternalSubject) == "" {
		return sso.ExternalIdentity{}, sso.ErrEmptySubject
	}
	if strings.TrimSpace(identity.ProviderID) == "" {
		return sso.ExternalIdentity{}, sso.ErrProviderNotFound
	}
	// provider 존재 + tenant 일치 확인.
	p, err := r.GetProvider(ctx, tx, identity.ProviderID)
	if err != nil {
		return sso.ExternalIdentity{}, err
	}
	if p.TenantID != tenantID {
		return sso.ExternalIdentity{}, sso.ErrProviderNotFound
	}

	now := r.deps.Clock.Now().UTC()
	identity.TenantID = tenantID
	identity.LastSeenAt = now
	if identity.FirstSeenAt.IsZero() {
		identity.FirstSeenAt = now
	}

	_, err = tx.Exec(ctx, `INSERT INTO sso_external_identities
(provider_id, external_subject, tenant_id, user_id, email, first_seen_at, last_seen_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(provider_id, external_subject) DO UPDATE SET
  last_seen_at = excluded.last_seen_at,
  email = excluded.email,
  user_id = excluded.user_id`,
		identity.ProviderID, identity.ExternalSubject, string(identity.TenantID),
		identity.UserID, identity.Email,
		identity.FirstSeenAt.Format(rfc3339Nano), identity.LastSeenAt.Format(rfc3339Nano),
	)
	if err != nil {
		return sso.ExternalIdentity{}, fmt.Errorf("sso: upsert external_identity: %w", err)
	}

	// 갱신 후 first_seen_at은 변경되지 않음 — 다시 SELECT로 정확한 값 회수.
	row := tx.QueryRow(ctx, `SELECT provider_id, external_subject, tenant_id, user_id, email, first_seen_at, last_seen_at
FROM sso_external_identities WHERE provider_id = ? AND external_subject = ?`,
		identity.ProviderID, identity.ExternalSubject)
	out, err := scanExternalIdentity(row)
	if err != nil {
		return sso.ExternalIdentity{}, fmt.Errorf("sso: read external_identity: %w", err)
	}
	return out, nil
}

// === helpers ===

// rowScanner는 *sql.Row와 *sql.Rows의 공통 표면입니다.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanProvider(s rowScanner) (sso.Provider, error) {
	var (
		id, tid, ptype, name, configStr string
		enabledInt                      int
		createdStr, updatedStr          string
	)
	if err := s.Scan(&id, &tid, &ptype, &name, &enabledInt, &configStr, &createdStr, &updatedStr); err != nil {
		return sso.Provider{}, err
	}
	createdAt, _ := time.Parse(rfc3339Nano, createdStr)
	updatedAt, _ := time.Parse(rfc3339Nano, updatedStr)
	return sso.Provider{
		ID:        id,
		TenantID:  storage.TenantID(tid),
		Type:      sso.Type(ptype),
		Name:      name,
		Enabled:   enabledInt != 0,
		Config:    json.RawMessage(configStr),
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}, nil
}

func scanExternalIdentity(s rowScanner) (sso.ExternalIdentity, error) {
	var (
		pid, sub, tid, uid, email string
		firstStr, lastStr         string
	)
	if err := s.Scan(&pid, &sub, &tid, &uid, &email, &firstStr, &lastStr); err != nil {
		return sso.ExternalIdentity{}, err
	}
	first, _ := time.Parse(rfc3339Nano, firstStr)
	last, _ := time.Parse(rfc3339Nano, lastStr)
	return sso.ExternalIdentity{
		ProviderID:      pid,
		ExternalSubject: sub,
		TenantID:        storage.TenantID(tid),
		UserID:          uid,
		Email:           email,
		FirstSeenAt:     first,
		LastSeenAt:      last,
	}, nil
}

func (r *Repo) findAttemptByState(ctx context.Context, tx storage.Tx, state string, tenantID storage.TenantID) (sso.LoginAttempt, error) {
	row := tx.QueryRow(ctx, `SELECT id, tenant_id, provider_id, state, pkce_verifier, nonce, relay_state, created_at, expires_at, completed_at
FROM sso_login_attempts WHERE state = ?`, state)
	var (
		id, tid, pid, st       string
		verifier, nonce, relay sql.NullString
		createdStr, expiresStr string
		completedStr           sql.NullString
	)
	if err := row.Scan(&id, &tid, &pid, &st, &verifier, &nonce, &relay, &createdStr, &expiresStr, &completedStr); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return sso.LoginAttempt{}, sso.ErrInvalidState
		}
		return sso.LoginAttempt{}, fmt.Errorf("sso: find attempt: %w", err)
	}
	if storage.TenantID(tid) != tenantID {
		return sso.LoginAttempt{}, sso.ErrInvalidState // cross-tenant 마스킹
	}
	createdAt, _ := time.Parse(rfc3339Nano, createdStr)
	expiresAt, _ := time.Parse(rfc3339Nano, expiresStr)
	a := sso.LoginAttempt{
		ID:           id,
		TenantID:     storage.TenantID(tid),
		ProviderID:   pid,
		State:        st,
		PKCEVerifier: verifier.String,
		Nonce:        nonce.String,
		RelayState:   relay.String,
		CreatedAt:    createdAt,
		ExpiresAt:    expiresAt,
	}
	if completedStr.Valid {
		t, _ := time.Parse(rfc3339Nano, completedStr.String)
		a.CompletedAt = &t
	}
	return a, nil
}

// nullableString은 빈 문자열을 sql.NullString으로 변환합니다 (DB에 NULL 저장).
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// isUniqueViolation은 modernc.org/sqlite의 UNIQUE 제약 위반 에러를 식별합니다.
//
// modernc sqlite는 에러 메시지에 "UNIQUE constraint failed"를 포함 — 라이브러리 무지 회피 위해
// 메시지 매칭으로 단순화. 향후 PG 어댑터는 별도.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "constraint failed: UNIQUE")
}

// newRandomToken은 base64url(no padding) 인코딩된 nByte 랜덤 토큰을 반환합니다.
//
// crypto/rand 사용 — IdP 콜백의 CSRF 방어 핵심. 호출 실패는 시스템 에러로 전파.
func newRandomToken(nBytes int) (string, error) {
	return randomToken(nBytes)
}
