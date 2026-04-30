-- +goose Up
-- E14 Phase 2 — Insight 도메인 핵심 테이블.
-- 참조: docs/design/04-domain-and-data-model.md §4.2 Insight
--       docs/design/08-intelligence-and-compliance.md §8.4
--       docs/design/phase2-backlog.md E14
-- 결정 R14-3·R14-4·R14-5 (사용자 합의 2026-04-29).

CREATE TABLE insights (
    id              TEXT NOT NULL,                -- "ins_<ULID>"
    tenant_id       TEXT NOT NULL,
    kind            TEXT NOT NULL
                        CHECK (kind IN ('drift','anomaly','peer','root_cause','prediction')),
    severity        TEXT NOT NULL DEFAULT 'medium'
                        CHECK (severity IN ('info','low','medium','high','critical')),
    scope_robot_id  TEXT,                         -- 옵션
    scope_fleet_id  TEXT,                         -- 옵션
    scope_check_id  TEXT,                         -- 옵션 (pack_check_id)
    summary         TEXT NOT NULL,
    reasoning       TEXT NOT NULL DEFAULT '',     -- §01-11 explainability
    evidence_json   TEXT NOT NULL DEFAULT '[]',   -- 보조 증거 메타 JSON 배열
    rules_applied   TEXT NOT NULL DEFAULT '[]',   -- 적용된 룰 ID 배열 (drift_window_5 등)
    confidence      REAL NOT NULL DEFAULT 1.0,    -- 0.0~1.0 (deterministic은 1.0)
    produced_by     TEXT NOT NULL DEFAULT 'rules' -- 'rules'|'llm'|'hybrid'
                        CHECK (produced_by IN ('rules','llm','hybrid')),
    created_at      TEXT NOT NULL,
    dismissed_at    TEXT,
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

-- +goose Down
DROP INDEX IF EXISTS insights_scope_robot;
DROP INDEX IF EXISTS insights_tenant_active_created;
DROP TABLE IF EXISTS insights;
