-- +goose Up
-- E5 Stage A — Fleet 도메인 핵심 테이블.
-- 참조: docs/design/04-domain-and-data-model.md §4.2 (Fleet),
--       docs/design/notes/e5-robot-fleet-deepdive.md §4 (Fleet Policy)

CREATE TABLE fleets (
    id            TEXT NOT NULL,
    tenant_id     TEXT NOT NULL,
    name          TEXT NOT NULL,
    description   TEXT NOT NULL DEFAULT '',
    -- policy: JSON {DefaultBaselineID, DefaultLevel, DefaultCriticality, ScanSchedule}
    -- (R3-4 — e5 deepdive §4)
    policy        TEXT NOT NULL DEFAULT '{}',
    created_at    TEXT NOT NULL, -- RFC3339Nano UTC
    updated_at    TEXT NOT NULL,
    deleted_at    TEXT,          -- soft delete (R3-5)
    PRIMARY KEY (id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id)
);

CREATE INDEX fleets_tenant ON fleets(tenant_id);

-- (tenant_id, name) UNIQUE — 같은 tenant 내 이름 중복 금지.
-- partial: deleted_at IS NULL 이면 살아있는 fleet만 — soft delete 후 같은 이름 재등록 허용 (R3-5).
CREATE UNIQUE INDEX fleets_tenant_name_active
    ON fleets(tenant_id, name) WHERE deleted_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS fleets_tenant_name_active;
DROP INDEX IF EXISTS fleets_tenant;
DROP TABLE IF EXISTS fleets;
