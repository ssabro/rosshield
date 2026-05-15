-- 0029_sso_group_mappings.up.sql — SSO group 매핑 + user_roles.source (PG).
--
-- SQLite 0029 미러. design doc:
-- docs/design/notes/rbac-fleet-scope-precision-design.md §6.1 + §7 Stage 4.
-- D-RBACEX-5 권장 default = A 명시 매핑, D-RBACEX-7 권장 default = B source 컬럼.
--
-- 변환 메모:
--   * SQLite TEXT → PG TEXT (변환 0).
--   * CHECK 제약은 PG도 동일 표현.

ALTER TABLE user_roles ADD COLUMN source TEXT NOT NULL DEFAULT 'manual';

CREATE TABLE sso_group_role_mappings (
    id           TEXT NOT NULL,
    tenant_id    TEXT NOT NULL,
    provider_id  TEXT NOT NULL,
    group_value  TEXT NOT NULL,
    role_id      TEXT NOT NULL,
    scope_type   TEXT NOT NULL DEFAULT 'tenant'
                     CHECK (scope_type IN ('tenant','fleet')),
    scope_id     TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL,
    PRIMARY KEY (id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id),
    FOREIGN KEY (provider_id) REFERENCES sso_providers(id) ON DELETE CASCADE,
    FOREIGN KEY (role_id) REFERENCES roles(id) ON DELETE CASCADE,
    UNIQUE (provider_id, group_value, role_id, scope_type, scope_id)
);

CREATE INDEX sso_group_role_mappings_provider ON sso_group_role_mappings(provider_id);
CREATE INDEX sso_group_role_mappings_tenant ON sso_group_role_mappings(tenant_id);
