-- E22-B — SQLite 0016_framework_reports.sql → PostgreSQL 변환.
-- 참조: docs/design/phase2-backlog.md E18
--       docs/design/08-intelligence-and-compliance.md §8.13
--
-- 변환 메모:
--   * BLOB → BYTEA (pdf_blob, sig_bytes)
--   * TEXT (RFC3339Nano) → TEXT
--   * pdf_size_bytes INTEGER → BIGINT
--   * sig_chain_head_seq INTEGER → BIGINT

CREATE TABLE framework_reports (
    id                  TEXT        NOT NULL,                -- "frep_<ULID>"
    tenant_id           TEXT        NOT NULL,
    profile_id          TEXT        NOT NULL,
    snapshot_id         TEXT        NOT NULL,
    pdf_sha256          TEXT        NOT NULL,                -- 64자 lowercase hex
    pdf_size_bytes      BIGINT      NOT NULL,
    pdf_blob            BYTEA       NOT NULL,                -- PDF 본문
    generated_at        TEXT NOT NULL,
    generated_by        TEXT        NOT NULL,
    -- ReportSignature inline (reports와 동일 스키마).
    sig_algorithm       TEXT        NOT NULL DEFAULT 'ed25519',
    sig_key_id          TEXT        NOT NULL,
    sig_bytes           BYTEA       NOT NULL,                -- 64B Ed25519 signature
    sig_signed_at       TEXT NOT NULL,
    sig_chain_head_seq  BIGINT      NOT NULL,
    sig_chain_head_hash TEXT        NOT NULL,
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
