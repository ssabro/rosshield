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

	if r.deps.Audit != nil {
		if err := r.deps.Audit.EmitTenantCreated(ctx, tx, tn, admin); err != nil {
			return tenant.CreateResult{}, fmt.Errorf("tenant: emit audit: %w", err)
		}
	}

	return tenant.CreateResult{Tenant: tn, Admin: admin}, nil
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
