-- E22-B — SQLite 0012_evidence.sql → PostgreSQL 변환.
-- 참조: docs/design/04-domain-and-data-model.md §4.2 EvidenceRecord
--       docs/design/07-scan-engine-and-benchmarks.md §7.8
--
-- 변환 메모:
--   * TEXT (RFC3339Nano) → TIMESTAMPTZ
--   * TEXT (JSON redactions) → JSONB
--   * ALTER TABLE ... DROP COLUMN: PG도 동일 syntax 지원
--   * FK ON DELETE CASCADE: 동일 (PG 표준)

-- evidence_records: 한 평문(redact 후 sha256)에 대한 메타.
CREATE TABLE evidence_records (
    id              TEXT        NOT NULL,                -- "ev_<ULID>"
    tenant_id       TEXT        NOT NULL,
    sha256          TEXT        NOT NULL,                -- 64자 lowercase hex
    content_type    TEXT        NOT NULL
                        CHECK (content_type IN ('stdout','stderr','file','config-snapshot','screenshot')),
    size_bytes      BIGINT      NOT NULL,                -- redact 후 평문 길이
    blob_locator    TEXT        NOT NULL,                -- "fs:<sha256>" — backend prefix + key
    redactions      JSONB       NOT NULL DEFAULT '[]'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id)
);

CREATE UNIQUE INDEX evidence_records_tenant_sha
    ON evidence_records(tenant_id, sha256);

-- evidence_refs: ScanResult ↔ EvidenceRecord N:M 매핑.
CREATE TABLE evidence_refs (
    scan_result_id  TEXT        NOT NULL,
    evidence_id     TEXT        NOT NULL,
    position        INTEGER     NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scan_result_id, evidence_id),
    FOREIGN KEY (scan_result_id) REFERENCES scan_results(id) ON DELETE CASCADE,
    FOREIGN KEY (evidence_id) REFERENCES evidence_records(id)
);

CREATE INDEX evidence_refs_evidence ON evidence_refs(evidence_id);

-- scan_results.evidence_ref 단일 placeholder 컬럼 정리 — N:M evidence_refs로 완전 대체(R9-7).
ALTER TABLE scan_results DROP COLUMN evidence_ref;
