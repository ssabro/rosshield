-- +goose Up
-- E15 Phase 2 — Compliance 도메인 핵심 테이블.
-- 참조: docs/design/04-domain-and-data-model.md §4.2 ComplianceProfile/FrameworkSnapshot/ControlStatus
--       docs/design/08-intelligence-and-compliance.md §8.10~§8.13
--       docs/design/phase2-backlog.md E15
-- 결정 R14-2·R14-9 (사용자 합의 2026-04-29).

-- compliance_profiles: tenant 스코프로 활성화된 framework 1건.
-- (tenant_id, framework) UNIQUE — 한 tenant가 같은 framework를 두 번 등록할 수 없음.
CREATE TABLE compliance_profiles (
    id                  TEXT NOT NULL,                -- "cp_<ULID>"
    tenant_id           TEXT NOT NULL,
    framework           TEXT NOT NULL,                -- "isms-p"|"iso27001-2022"|"nist-800-53-rev5"
    framework_version   TEXT NOT NULL,                -- "2024", "2022" 등
    enabled             INTEGER NOT NULL DEFAULT 1,
    customizations_json TEXT NOT NULL DEFAULT '[]',   -- ControlCustomization 배열 raw JSON
    created_at          TEXT NOT NULL,                -- RFC3339Nano UTC
    updated_at          TEXT NOT NULL,
    PRIMARY KEY (id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id),
    UNIQUE (tenant_id, framework)
);

-- framework_snapshots: 특정 시점의 통제 status 집계 + audit chain anchor.
-- statuses_json은 ControlStatus 배열 — 통제 정의가 갱신되어도 snapshot은 불변(R14-9 영속).
-- chain_head_seq/hash는 생성 시점의 audit chain head — 외부 검증 anchor (E10 §10).
CREATE TABLE framework_snapshots (
    id                   TEXT NOT NULL,                -- "fs_<ULID>"
    tenant_id            TEXT NOT NULL,
    profile_id           TEXT NOT NULL,
    session_id           TEXT,                         -- 옵션 (특정 ScanSession 기준)
    overall_score        REAL NOT NULL,                -- 0.0~1.0
    pass_count           INTEGER NOT NULL DEFAULT 0,
    fail_count           INTEGER NOT NULL DEFAULT 0,
    partial_count        INTEGER NOT NULL DEFAULT 0,
    not_applicable_count INTEGER NOT NULL DEFAULT 0,
    unmapped_count       INTEGER NOT NULL DEFAULT 0,
    chain_head_seq       INTEGER NOT NULL,             -- audit anchor
    chain_head_hash      TEXT NOT NULL,                -- 64자 lowercase hex
    statuses_json        TEXT NOT NULL DEFAULT '[]',   -- ControlStatus 배열 (snapshot 보존)
    created_at           TEXT NOT NULL,
    PRIMARY KEY (id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id),
    FOREIGN KEY (profile_id) REFERENCES compliance_profiles(id)
    -- session_id는 옵션 anchor이며 FK를 걸지 않음 (insights.scope_*와 동일 정책).
    -- 외부 검증은 chain_head_seq/hash anchor가 담당; session 라이프사이클과 분리.
);

CREATE INDEX framework_snapshots_tenant_created
    ON framework_snapshots(tenant_id, created_at DESC);
CREATE INDEX framework_snapshots_profile_created
    ON framework_snapshots(profile_id, created_at DESC);

-- +goose Down
DROP INDEX IF EXISTS framework_snapshots_profile_created;
DROP INDEX IF EXISTS framework_snapshots_tenant_created;
DROP TABLE IF EXISTS framework_snapshots;
DROP TABLE IF EXISTS compliance_profiles;
