-- E21 Phase 3 — Invitation·Role 관리 PG 버전.
-- SQLite 0021_invitations 변환: schema 동일 (TEXT/INDEX 그대로).

CREATE TABLE invitations (
    id            TEXT NOT NULL,
    tenant_id     TEXT NOT NULL,
    email         TEXT NOT NULL,
    role_name     TEXT NOT NULL,
    token         TEXT NOT NULL,
    invited_by    TEXT NOT NULL,
    expires_at    TEXT NOT NULL,
    accepted_at   TEXT,
    accepted_by   TEXT,
    created_at    TEXT NOT NULL,
    PRIMARY KEY (id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id),
    FOREIGN KEY (invited_by) REFERENCES users(id),
    FOREIGN KEY (accepted_by) REFERENCES users(id)
);

CREATE UNIQUE INDEX invitations_token ON invitations(token);
CREATE INDEX invitations_tenant_created ON invitations(tenant_id, created_at DESC);
CREATE INDEX invitations_tenant_email ON invitations(tenant_id, email);
