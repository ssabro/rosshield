-- E22-B — SQLite 0013_reports.sql → PostgreSQL 변환.
-- 참조: docs/design/04-domain-and-data-model.md §4.2 Report·ReportSignature
--       docs/design/notes/e8-pdf-signature-research.md
--
-- 변환 메모:
--   * BLOB → BYTEA (pdf_blob, sig_bytes)
--   * TEXT (RFC3339Nano) → TEXT
--   * sig_chain_head_seq INTEGER → BIGINT (audit chain seq와 동일 폭 유지)
--   * size_bytes INTEGER → BIGINT (PDF는 GB 단위 가능)

CREATE TABLE reports (
    id                  TEXT        NOT NULL,                -- "rep_<ULID>"
    tenant_id           TEXT        NOT NULL,
    template_id         TEXT        NOT NULL DEFAULT 'default',
    scope_type          TEXT        NOT NULL
                            CHECK (scope_type IN ('session','fleet','tenant')),
    scope_session_id    TEXT,                                 -- scope_type='session'일 때만 채움
    format              TEXT        NOT NULL DEFAULT 'pdf'
                            CHECK (format IN ('pdf')),        -- Phase 1 PDF only
    pdf_sha256          TEXT        NOT NULL,                 -- 64자 lowercase hex
    pdf_size_bytes      BIGINT      NOT NULL,
    pdf_blob            BYTEA       NOT NULL,                 -- PDF 본문
    generated_at        TEXT NOT NULL,
    generated_by        TEXT        NOT NULL,                 -- userID 또는 'system'
    -- ReportSignature inline.
    sig_algorithm       TEXT        NOT NULL DEFAULT 'ed25519',
    sig_key_id          TEXT        NOT NULL,                 -- "key_<8B hex>"
    sig_bytes           BYTEA       NOT NULL,                 -- 64B Ed25519 signature
    sig_signed_at       TEXT NOT NULL,
    sig_chain_head_seq  BIGINT      NOT NULL,                 -- 서명 시점 audit chain head seq
    sig_chain_head_hash TEXT        NOT NULL,                 -- 서명 시점 audit chain head hash (hex)
    PRIMARY KEY (id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id),
    FOREIGN KEY (scope_session_id) REFERENCES scan_sessions(id)
);

CREATE INDEX reports_tenant_generated ON reports(tenant_id, generated_at DESC);
CREATE INDEX reports_session ON reports(scope_session_id);
