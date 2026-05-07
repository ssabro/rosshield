-- E22-B — SQLite 0014_insights.sql → PostgreSQL 변환.
-- 참조: docs/design/04-domain-and-data-model.md §4.2 Insight
--       docs/design/08-intelligence-and-compliance.md §8.4
--
-- 변환 메모:
--   * TEXT (JSON evidence_json/rules_applied) → JSONB
--   * TEXT (RFC3339Nano) → TIMESTAMPTZ
--   * REAL → DOUBLE PRECISION (confidence)
--   * partial index → 동일 (PG 지원)

CREATE TABLE insights (
    id              TEXT             NOT NULL,                -- "ins_<ULID>"
    tenant_id       TEXT             NOT NULL,
    kind            TEXT             NOT NULL
                        CHECK (kind IN ('drift','anomaly','peer','root_cause','prediction')),
    severity        TEXT             NOT NULL DEFAULT 'medium'
                        CHECK (severity IN ('info','low','medium','high','critical')),
    scope_robot_id  TEXT,                                      -- 옵션
    scope_fleet_id  TEXT,                                      -- 옵션
    scope_check_id  TEXT,                                      -- 옵션 (pack_check_id)
    summary         TEXT             NOT NULL,
    reasoning       TEXT             NOT NULL DEFAULT '',     -- §01-11 explainability
    evidence_json   JSONB            NOT NULL DEFAULT '[]'::jsonb,
    rules_applied   JSONB            NOT NULL DEFAULT '[]'::jsonb,
    confidence      DOUBLE PRECISION NOT NULL DEFAULT 1.0,
    produced_by     TEXT             NOT NULL DEFAULT 'rules'
                        CHECK (produced_by IN ('rules','llm','hybrid')),
    created_at      TIMESTAMPTZ      NOT NULL,
    dismissed_at    TIMESTAMPTZ,
    dismissed_by    TEXT,
    PRIMARY KEY (id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id)
);

-- 활성 Insight 목록 (dismissed=NULL) — 가장 흔한 쿼리.
CREATE INDEX insights_tenant_active_created
    ON insights(tenant_id, created_at DESC) WHERE dismissed_at IS NULL;

-- 특정 robot의 Insight history.
CREATE INDEX insights_scope_robot
    ON insights(tenant_id, scope_robot_id, created_at DESC) WHERE scope_robot_id IS NOT NULL;
