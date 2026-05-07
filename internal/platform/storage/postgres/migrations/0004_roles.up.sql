-- E22-B — SQLite 0004_roles.sql → PostgreSQL 변환.
-- 참조: docs/design/04-domain-and-data-model.md §4.2
--       docs/design/05-api-and-auth.md §5.8
--
-- 변환 메모:
--   * TEXT (JSON array) → JSONB (permissions)
--   * INTEGER NOT NULL DEFAULT 0 (boolean) → BOOLEAN NOT NULL DEFAULT FALSE (is_system)
--   * TEXT (RFC3339Nano) → TIMESTAMPTZ

CREATE TABLE roles (
    id           TEXT        PRIMARY KEY,
    tenant_id    TEXT        NOT NULL,
    name         TEXT        NOT NULL,
    permissions  JSONB       NOT NULL DEFAULT '[]'::jsonb,
    is_system    BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at   TIMESTAMPTZ NOT NULL,
    UNIQUE (tenant_id, name),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id)
);

CREATE INDEX roles_tenant ON roles(tenant_id);

CREATE TABLE user_roles (
    user_id   TEXT NOT NULL,
    role_id   TEXT NOT NULL,
    PRIMARY KEY (user_id, role_id),
    FOREIGN KEY (user_id) REFERENCES users(id),
    FOREIGN KEY (role_id) REFERENCES roles(id)
);

CREATE INDEX user_roles_user ON user_roles(user_id);
CREATE INDEX user_roles_role ON user_roles(role_id);
