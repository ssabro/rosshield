-- +goose Up
-- E18 Phase 2 — Framework 리포트 PDF 영속.
-- 참조: docs/design/phase2-backlog.md E18
--       docs/design/04-domain-and-data-model.md §4.2 Report
--       docs/design/08-intelligence-and-compliance.md §8.13
--
-- 별도 테이블로 두는 이유:
--   - reports.scope_type CHECK가 ('session','fleet','tenant')라 SQLite ALTER로 framework 추가 시 table 재생성 필요 (위험)
--   - profile_id·snapshot_id FK는 framework 전용 → 컬럼이 reports와 의미가 다름
--   - inline signature 메타는 reports와 동일 패턴 (reporting 도메인 일관성)

CREATE TABLE framework_reports (
    id                  TEXT NOT NULL,                -- "frep_<ULID>"
    tenant_id           TEXT NOT NULL,
    profile_id          TEXT NOT NULL,
    snapshot_id         TEXT NOT NULL,
    pdf_sha256          TEXT NOT NULL,                -- 64자 lowercase hex
    pdf_size_bytes      INTEGER NOT NULL,
    pdf_blob            BLOB NOT NULL,                -- PDF 본문 (Phase 2 단순; 후속 blobstore 검토)
    generated_at        TEXT NOT NULL,                -- RFC3339Nano UTC
    generated_by        TEXT NOT NULL,                -- userID 또는 'system'
    -- ReportSignature inline (reports와 동일 스키마 → reporting 도메인 일관).
    sig_algorithm       TEXT NOT NULL DEFAULT 'ed25519',
    sig_key_id          TEXT NOT NULL,
    sig_bytes           BLOB NOT NULL,                -- 64B Ed25519 signature (Generate 직후 zero placeholder)
    sig_signed_at       TEXT NOT NULL,
    sig_chain_head_seq  INTEGER NOT NULL,
    sig_chain_head_hash TEXT NOT NULL,
    PRIMARY KEY (id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id),
    FOREIGN KEY (profile_id) REFERENCES compliance_profiles(id),
    FOREIGN KEY (snapshot_id) REFERENCES framework_snapshots(id)
);

CREATE INDEX framework_reports_tenant_generated
    ON framework_reports(tenant_id, generated_at DESC);
CREATE INDEX framework_reports_profile
    ON framework_reports(profile_id, generated_at DESC);
CREATE INDEX framework_reports_snapshot
    ON framework_reports(snapshot_id);

-- +goose Down
DROP INDEX IF EXISTS framework_reports_snapshot;
DROP INDEX IF EXISTS framework_reports_profile;
DROP INDEX IF EXISTS framework_reports_tenant_generated;
DROP TABLE IF EXISTS framework_reports;
