-- E22-B — SQLite 0017_mapping_suggestions.sql → PostgreSQL 변환.
-- 참조: docs/design/phase2-backlog.md E17
--       docs/design/08-intelligence-and-compliance.md §8.5 LLM mapper
--
-- 변환 메모:
--   * REAL → DOUBLE PRECISION (confidence)
--   * TEXT (RFC3339Nano) → TEXT
--   * partial index → 동일 (PG 지원)

CREATE TABLE mapping_suggestions (
    id              TEXT             NOT NULL,                -- "ms_<ULID>"
    tenant_id       TEXT             NOT NULL,
    check_code      TEXT             NOT NULL,                -- pack 내 check.code
    framework       TEXT             NOT NULL,                -- 제안 대상 framework
    control_id      TEXT             NOT NULL,                -- 제안 control ID
    confidence      DOUBLE PRECISION NOT NULL DEFAULT 0.0,
    reasoning       TEXT             NOT NULL DEFAULT '',     -- LLM rationale (P11 explainability)
    produced_by     TEXT             NOT NULL DEFAULT 'llm'
                        CHECK (produced_by IN ('llm', 'rules', 'manual')),
    status          TEXT             NOT NULL DEFAULT 'pending'
                        CHECK (status IN ('pending', 'confirmed', 'rejected')),
    llm_provider    TEXT             NOT NULL DEFAULT '',
    llm_model       TEXT             NOT NULL DEFAULT '',
    created_at      TEXT      NOT NULL,
    decided_at      TEXT,
    decided_by      TEXT,
    PRIMARY KEY (id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id),
    UNIQUE (tenant_id, check_code, control_id)
);

-- 활성 pending 큐: 사용자 검토 화면이 가장 자주 조회.
CREATE INDEX mapping_suggestions_pending
    ON mapping_suggestions(tenant_id, created_at DESC) WHERE status = 'pending';

-- check_code별 history.
CREATE INDEX mapping_suggestions_check
    ON mapping_suggestions(tenant_id, check_code, created_at DESC);
