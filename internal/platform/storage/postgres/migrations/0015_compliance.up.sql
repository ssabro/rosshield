-- E22-B — SQLite 0015_compliance.sql → PostgreSQL 변환.
-- 참조: docs/design/04-domain-and-data-model.md §4.2 ComplianceProfile/FrameworkSnapshot/ControlStatus
--       docs/design/08-intelligence-and-compliance.md §8.10~§8.13
--
-- 변환 메모:
--   * INTEGER (boolean enabled) → SMALLINT
--   * TEXT (JSON customizations_json/statuses_json) → TEXT
--   * TEXT (RFC3339Nano) → TEXT
--   * REAL → DOUBLE PRECISION (overall_score)
--   * INTEGER chain_head_seq → BIGINT (audit chain seq 동일)

-- compliance_profiles: tenant 스코프로 활성화된 framework 1건.
CREATE TABLE compliance_profiles (
    id                  TEXT        NOT NULL,                -- "cp_<ULID>"
    tenant_id           TEXT        NOT NULL,
    framework           TEXT        NOT NULL,                -- "isms-p"|"iso27001-2022"|"nist-800-53-rev5"
    framework_version   TEXT        NOT NULL,
    enabled             SMALLINT  NOT NULL DEFAULT 1,
    customizations_json TEXT       NOT NULL DEFAULT '[]',
    created_at          TEXT NOT NULL,
    updated_at          TEXT NOT NULL,
    PRIMARY KEY (id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id),
    UNIQUE (tenant_id, framework)
);

-- framework_snapshots: 특정 시점의 통제 status 집계 + audit chain anchor.
CREATE TABLE framework_snapshots (
    id                   TEXT             NOT NULL,           -- "fs_<ULID>"
    tenant_id            TEXT             NOT NULL,
    profile_id           TEXT             NOT NULL,
    session_id           TEXT,                                 -- 옵션 (특정 ScanSession 기준)
    overall_score        DOUBLE PRECISION NOT NULL,
    pass_count           INTEGER          NOT NULL DEFAULT 0,
    fail_count           INTEGER          NOT NULL DEFAULT 0,
    partial_count        INTEGER          NOT NULL DEFAULT 0,
    not_applicable_count INTEGER          NOT NULL DEFAULT 0,
    unmapped_count       INTEGER          NOT NULL DEFAULT 0,
    chain_head_seq       BIGINT           NOT NULL,           -- audit anchor
    chain_head_hash      TEXT             NOT NULL,           -- 64자 lowercase hex
    statuses_json        TEXT            NOT NULL DEFAULT '[]',
    created_at           TEXT      NOT NULL,
    PRIMARY KEY (id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id),
    FOREIGN KEY (profile_id) REFERENCES compliance_profiles(id)
    -- session_id는 옵션 anchor (insights.scope_*와 동일 정책 — FK 미설정).
);

CREATE INDEX framework_snapshots_tenant_created
    ON framework_snapshots(tenant_id, created_at DESC);
CREATE INDEX framework_snapshots_profile_created
    ON framework_snapshots(profile_id, created_at DESC);
