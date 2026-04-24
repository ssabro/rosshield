-- +goose Up
-- E3 Stage B — Role·Permission RBAC.
-- 참조: docs/design/04-domain-and-data-model.md §4.2
--       docs/design/05-api-and-auth.md §5.8

CREATE TABLE roles (
    id           TEXT    PRIMARY KEY,
    tenant_id    TEXT    NOT NULL,
    name         TEXT    NOT NULL,
    permissions  TEXT    NOT NULL, -- JSON array of permission strings
    is_system    INTEGER NOT NULL DEFAULT 0, -- 1이면 시스템 시드 역할 (admin/auditor/operator)
    created_at   TEXT    NOT NULL,
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

-- +goose Down
DROP INDEX IF EXISTS user_roles_role;
DROP INDEX IF EXISTS user_roles_user;
DROP TABLE IF EXISTS user_roles;
DROP INDEX IF EXISTS roles_tenant;
DROP TABLE IF EXISTS roles;
