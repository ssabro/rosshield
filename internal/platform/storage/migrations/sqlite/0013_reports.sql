-- +goose Up
-- E8 Stage A — Reporting 도메인 핵심 테이블.
-- 참조: docs/design/04-domain-and-data-model.md §4.2 Report·ReportSignature
--       docs/design/notes/e8-pdf-signature-research.md (R10-2·R10-3)
--       phase1-backlog.md E8

-- reports: 한 ScanSession(또는 fleet/tenant scope)에서 생성된 PDF 1건의 메타.
-- PDF blob 자체는 evidence와 별개로 reporting 전용 BLOB 컬럼에 저장(Phase 1 단순화).
-- ReportSignature는 본 테이블에 inline 컬럼으로 (sig_*) — 별도 테이블 분리는 Phase 2.
CREATE TABLE reports (
    id                  TEXT NOT NULL,                -- "rep_<ULID>"
    tenant_id           TEXT NOT NULL,
    template_id         TEXT NOT NULL DEFAULT 'default',
    scope_type          TEXT NOT NULL
                            CHECK (scope_type IN ('session','fleet','tenant')),
    scope_session_id    TEXT,                         -- scope_type='session'일 때만 채움
    format              TEXT NOT NULL DEFAULT 'pdf'
                            CHECK (format IN ('pdf')), -- Phase 1 PDF only
    pdf_sha256          TEXT NOT NULL,                -- 64자 lowercase hex (PDF body sha256)
    pdf_size_bytes      INTEGER NOT NULL,
    pdf_blob            BLOB NOT NULL,                -- PDF 본문 (Phase 1 단순; Phase 2 blobstore로 이전 검토)
    generated_at        TEXT NOT NULL,                -- RFC3339Nano UTC
    generated_by        TEXT NOT NULL,                -- userID 또는 'system'
    -- ReportSignature inline.
    sig_algorithm       TEXT NOT NULL DEFAULT 'ed25519',
    sig_key_id          TEXT NOT NULL,                -- "key_<8B hex>"
    sig_bytes           BLOB NOT NULL,                -- 64B Ed25519 signature
    sig_signed_at       TEXT NOT NULL,
    sig_chain_head_seq  INTEGER NOT NULL,             -- 서명 시점 audit chain head seq
    sig_chain_head_hash TEXT NOT NULL,                -- 서명 시점 audit chain head hash (hex)
    PRIMARY KEY (id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id),
    FOREIGN KEY (scope_session_id) REFERENCES scan_sessions(id)
);

CREATE INDEX reports_tenant_generated ON reports(tenant_id, generated_at DESC);
CREATE INDEX reports_session ON reports(scope_session_id);

-- +goose Down
DROP INDEX IF EXISTS reports_session;
DROP INDEX IF EXISTS reports_tenant_generated;
DROP TABLE IF EXISTS reports;
