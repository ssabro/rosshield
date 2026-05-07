-- E22-B — SQLite 0008_fleets.sql → PostgreSQL 변환.
-- 참조: docs/design/04-domain-and-data-model.md §4.2 (Fleet)
--       docs/design/notes/e5-robot-fleet-deepdive.md §4 (Fleet Policy)
--
-- 변환 메모:
--   * TEXT (JSON policy) → JSONB
--   * TEXT (RFC3339Nano) → TIMESTAMPTZ
--   * partial unique index (WHERE deleted_at IS NULL) → 동일 (PG 지원)

CREATE TABLE fleets (
    id            TEXT        NOT NULL,
    tenant_id     TEXT        NOT NULL,
    name          TEXT        NOT NULL,
    description   TEXT        NOT NULL DEFAULT '',
    -- policy: JSONB {DefaultBaselineID, DefaultLevel, DefaultCriticality, ScanSchedule}
    policy        JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at    TIMESTAMPTZ NOT NULL,
    updated_at    TIMESTAMPTZ NOT NULL,
    deleted_at    TIMESTAMPTZ,             -- soft delete (R3-5)
    PRIMARY KEY (id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id)
);

CREATE INDEX fleets_tenant ON fleets(tenant_id);

-- (tenant_id, name) UNIQUE — 같은 tenant 내 이름 중복 금지.
-- partial: deleted_at IS NULL 이면 살아있는 fleet만 — soft delete 후 같은 이름 재등록 허용.
CREATE UNIQUE INDEX fleets_tenant_name_active
    ON fleets(tenant_id, name) WHERE deleted_at IS NULL;
