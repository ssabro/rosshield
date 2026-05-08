-- +goose Up
-- 0021_invitations.sql — E21 초대·역할 관리.
--
-- invitations: tenant admin이 새 사용자에게 발송하는 초대 토큰 1건.
--
-- 토큰 정책:
--   token: cryptographic random 32B → base64url 인코딩 (~43자).
--   1회 사용: accepted_at 채워지면 재사용 거부.
--   기본 만료 7일 — application layer가 expires_at에 기록.
--
-- role 부여:
--   role_name은 admin/auditor/operator + custom 허용 — accept 시점에 (tenant, role_name) → roles 조회.
--   role_name 미존재 시 application sentinel ErrInvalidRole.
--
-- 멀티테넌시:
--   모든 query는 (tenant_id, ...) 기반. token 단독 lookup도 application이 tenant scope tx로 진입.

CREATE TABLE invitations (
    id            TEXT NOT NULL,                -- "inv_<ULID>"
    tenant_id     TEXT NOT NULL,
    email         TEXT NOT NULL,                -- 초대받는 사용자 이메일 (lowercase normalize)
    role_name     TEXT NOT NULL,                -- "admin" | "auditor" | "operator" | custom
    token         TEXT NOT NULL,                -- 1회용 토큰 (base64url ~43자)
    invited_by    TEXT NOT NULL,                -- 발송자 user_id
    expires_at    TEXT NOT NULL,                -- ISO 8601 UTC
    accepted_at   TEXT,                         -- 채워지면 재사용 거부
    accepted_by   TEXT,                         -- accept 시 생성·매칭된 user_id
    created_at    TEXT NOT NULL,
    PRIMARY KEY (id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id),
    FOREIGN KEY (invited_by) REFERENCES users(id),
    FOREIGN KEY (accepted_by) REFERENCES users(id)
);

-- 토큰은 globally unique (cross-tenant lookup 차단은 application의 tenant scope tx에 위임).
CREATE UNIQUE INDEX invitations_token ON invitations(token);

-- tenant 안 list + filter용.
CREATE INDEX invitations_tenant_created ON invitations(tenant_id, created_at DESC);

-- (tenant, email) 활성(accepted_at IS NULL AND now <= expires_at) 초대 중복 체크용.
CREATE INDEX invitations_tenant_email ON invitations(tenant_id, email);

-- +goose Down
DROP INDEX IF EXISTS invitations_tenant_email;
DROP INDEX IF EXISTS invitations_tenant_created;
DROP INDEX IF EXISTS invitations_token;
DROP TABLE IF EXISTS invitations;
