-- E22-B — SQLite 0010_robots.sql → PostgreSQL 변환.
-- 참조: docs/design/04-domain-and-data-model.md §4.2·§4.3 (Robot)
--       docs/design/notes/e5-robot-fleet-deepdive.md §10 Stage C
--
-- 변환 메모:
--   * TEXT (JSON array) → TEXT (tags)
--   * TEXT (RFC3339Nano) → TEXT
--   * partial unique index → 동일 (PG 지원)

CREATE TABLE robots (
    id              TEXT        NOT NULL,
    tenant_id       TEXT        NOT NULL,
    fleet_id        TEXT        NOT NULL,
    credential_id   TEXT        NOT NULL,
    name            TEXT        NOT NULL,
    host            TEXT        NOT NULL,
    port            INTEGER     NOT NULL DEFAULT 22,
    auth_type       TEXT        NOT NULL,            -- 'password' | 'privateKey'
    os_distro       TEXT        NOT NULL DEFAULT '',
    ros_distro      TEXT        NOT NULL DEFAULT '',
    tags            TEXT       NOT NULL DEFAULT '[]',
    role            TEXT        NOT NULL DEFAULT '',
    criticality     TEXT        NOT NULL DEFAULT 'medium',
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    last_scan_at    TEXT,
    deleted_at      TEXT,                     -- soft delete (R3-5)
    PRIMARY KEY (id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id),
    FOREIGN KEY (fleet_id) REFERENCES fleets(id),
    FOREIGN KEY (credential_id) REFERENCES credentials(id)
);

CREATE INDEX robots_tenant ON robots(tenant_id);
CREATE INDEX robots_tenant_fleet ON robots(tenant_id, fleet_id);
CREATE INDEX robots_credential ON robots(credential_id);

-- partial unique 1: 같은 tenant·fleet 내 활성 Robot 이름 중복 금지 (R3-7).
CREATE UNIQUE INDEX robots_tenant_fleet_name_active
    ON robots(tenant_id, fleet_id, name) WHERE deleted_at IS NULL;

-- partial unique 2: 같은 tenant 내 활성 (host, port) 중복 금지 (R3-7).
CREATE UNIQUE INDEX robots_tenant_host_port_active
    ON robots(tenant_id, host, port) WHERE deleted_at IS NULL;
