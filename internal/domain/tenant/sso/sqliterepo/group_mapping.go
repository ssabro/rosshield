package sqliterepo

// group_mapping.go — RBAC fleet 정밀화 Stage 4 SSO group 매핑 sqlite 어댑터.
//
// 책임:
//
//	CreateGroupMapping        → sso_group_role_mappings INSERT (UNIQUE 5-tuple).
//	ListGroupMappings         → provider scope 전체 SELECT (created_at ASC).
//	DeleteGroupMapping        → tenant scope DELETE (cross-tenant 마스킹).
//	ResolveBindingsForGroups  → IdP claim group → 매핑 lookup → 중복 제거된 ResolvedBinding 슬라이스.
//
// 본 stage는 SSO callback 자동 sync(Stage 5)와 분리 — 순수 CRUD + resolve helper.
// SyncRoleBindings(user_roles row insert/delete)는 Stage 5에서 tenant.Service에 추가.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ssabro/rosshield/internal/domain/tenant/sso"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// CreateGroupMapping은 sso.GroupMappingService.CreateGroupMapping 구현입니다.
//
// scope_type 빈 값 → 'tenant' default. scope_type='tenant'이면 scope_id는 빈 문자열로 정규화 —
// DB 일관성 + UNIQUE 제약 cover. role_id는 같은 tenant 소속이어야 하며 cross-tenant은
// ErrRoleNotFoundForTenant. provider_id는 tenant 소속이어야 — cross-tenant은 ErrProviderNotFound.
func (r *Repo) CreateGroupMapping(ctx context.Context, tx storage.Tx, req sso.CreateGroupMappingRequest) (sso.GroupRoleMapping, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return sso.GroupRoleMapping{}, storage.ErrTenantMissing
	}
	if strings.TrimSpace(req.GroupValue) == "" {
		return sso.GroupRoleMapping{}, sso.ErrEmptyGroupValue
	}
	if strings.TrimSpace(req.RoleID) == "" {
		return sso.GroupRoleMapping{}, sso.ErrEmptyRoleID
	}
	scopeType := req.ScopeType
	if scopeType == "" {
		scopeType = "tenant"
	}
	if scopeType != "tenant" && scopeType != "fleet" {
		return sso.GroupRoleMapping{}, sso.ErrInvalidScopeType
	}
	scopeID := req.ScopeID
	if scopeType == "tenant" {
		scopeID = "" // tenant scope는 scope_id 무의미 — 빈 값 강제.
	} else if strings.TrimSpace(scopeID) == "" {
		return sso.GroupRoleMapping{}, sso.ErrEmptyScopeIDForFleet
	}

	// provider tenant 검증 (GetProvider가 cross-tenant lookup을 ErrProviderNotFound로 마스킹).
	if _, err := r.GetProvider(ctx, tx, req.ProviderID); err != nil {
		return sso.GroupRoleMapping{}, err
	}

	// role tenant 검증 — 같은 tenant 소속이어야 자동 sync 시 의미 일관.
	var roleTenant string
	row := tx.QueryRow(ctx, `SELECT tenant_id FROM roles WHERE id = ?`, req.RoleID)
	if err := row.Scan(&roleTenant); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return sso.GroupRoleMapping{}, sso.ErrRoleNotFoundForTenant
		}
		return sso.GroupRoleMapping{}, fmt.Errorf("sso: lookup role tenant: %w", err)
	}
	if storage.TenantID(roleTenant) != tenantID {
		return sso.GroupRoleMapping{}, sso.ErrRoleNotFoundForTenant
	}

	now := r.deps.Clock.Now().UTC()
	m := sso.GroupRoleMapping{
		ID:         r.deps.IDGen.New("sgm"),
		TenantID:   tenantID,
		ProviderID: req.ProviderID,
		GroupValue: req.GroupValue,
		RoleID:     req.RoleID,
		ScopeType:  scopeType,
		ScopeID:    scopeID,
		CreatedAt:  now,
	}

	_, err := tx.Exec(ctx, `INSERT INTO sso_group_role_mappings
(id, tenant_id, provider_id, group_value, role_id, scope_type, scope_id, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, string(m.TenantID), m.ProviderID, m.GroupValue, m.RoleID,
		m.ScopeType, m.ScopeID, m.CreatedAt.Format(rfc3339Nano),
	)
	if err != nil {
		if isUniqueViolation(err) {
			return sso.GroupRoleMapping{}, sso.ErrGroupMappingExists
		}
		return sso.GroupRoleMapping{}, fmt.Errorf("sso: insert group mapping: %w", err)
	}
	return m, nil
}

// ListGroupMappings는 sso.GroupMappingService.ListGroupMappings 구현입니다.
//
// provider tenant 검증 → 같은 provider의 모든 매핑 반환 (created_at ASC).
func (r *Repo) ListGroupMappings(ctx context.Context, tx storage.Tx, providerID string) ([]sso.GroupRoleMapping, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return nil, storage.ErrTenantMissing
	}
	if _, err := r.GetProvider(ctx, tx, providerID); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT id, tenant_id, provider_id, group_value, role_id, scope_type, scope_id, created_at
FROM sso_group_role_mappings WHERE provider_id = ? ORDER BY created_at ASC, id ASC`, providerID)
	if err != nil {
		return nil, fmt.Errorf("sso: list group mappings: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []sso.GroupRoleMapping
	for rows.Next() {
		m, err := scanGroupMapping(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// DeleteGroupMapping은 sso.GroupMappingService.DeleteGroupMapping 구현입니다.
//
// (tenantID, mappingID)로 격리 — cross-tenant DELETE 시도는 ErrGroupMappingNotFound.
func (r *Repo) DeleteGroupMapping(ctx context.Context, tx storage.Tx, mappingID string) error {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return storage.ErrTenantMissing
	}
	res, err := tx.Exec(ctx, `DELETE FROM sso_group_role_mappings
WHERE id = ? AND tenant_id = ?`, mappingID, string(tenantID))
	if err != nil {
		return fmt.Errorf("sso: delete group mapping: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sso.ErrGroupMappingNotFound
	}
	return nil
}

// ResolveBindingsForGroups는 sso.GroupMappingService.ResolveBindingsForGroups 구현입니다.
//
// providerID + IdP claim group 슬라이스 → 매핑 테이블 lookup → ResolvedBinding 슬라이스.
// 중복 (role_id, scope_type, scope_id) 셋은 1건만 반환 (set-of 의미). groups 빈 슬라이스나
// nil → 빈 결과 (매 login에서 source='sso' binding 모두 회수 의미). provider tenant
// 검증은 caller(SSO callback handler)가 사전 수행 — 본 메서드는 row만 반환.
func (r *Repo) ResolveBindingsForGroups(ctx context.Context, tx storage.Tx, providerID string, groups []string) ([]sso.ResolvedBinding, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return nil, storage.ErrTenantMissing
	}
	if len(groups) == 0 {
		return nil, nil
	}

	// IN 절 — placeholder 동적 생성.
	placeholders := make([]string, len(groups))
	args := make([]any, 0, len(groups)+2)
	args = append(args, providerID, string(tenantID))
	for i, g := range groups {
		placeholders[i] = "?"
		args = append(args, g)
	}
	q := fmt.Sprintf(`SELECT role_id, scope_type, scope_id FROM sso_group_role_mappings
WHERE provider_id = ? AND tenant_id = ? AND group_value IN (%s)`,
		strings.Join(placeholders, ","))

	rows, err := tx.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("sso: resolve group mappings: %w", err)
	}
	defer func() { _ = rows.Close() }()

	seen := make(map[sso.ResolvedBinding]struct{})
	var out []sso.ResolvedBinding
	for rows.Next() {
		var rb sso.ResolvedBinding
		if err := rows.Scan(&rb.RoleID, &rb.ScopeType, &rb.ScopeID); err != nil {
			return nil, fmt.Errorf("sso: scan resolved binding: %w", err)
		}
		if _, dup := seen[rb]; dup {
			continue
		}
		seen[rb] = struct{}{}
		out = append(out, rb)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func scanGroupMapping(s rowScanner) (sso.GroupRoleMapping, error) {
	var (
		id, tid, pid, group, rid, st, sid, createdStr string
	)
	if err := s.Scan(&id, &tid, &pid, &group, &rid, &st, &sid, &createdStr); err != nil {
		return sso.GroupRoleMapping{}, err
	}
	createdAt, _ := time.Parse(rfc3339Nano, createdStr)
	return sso.GroupRoleMapping{
		ID:         id,
		TenantID:   storage.TenantID(tid),
		ProviderID: pid,
		GroupValue: group,
		RoleID:     rid,
		ScopeType:  st,
		ScopeID:    sid,
		CreatedAt:  createdAt,
	}, nil
}
