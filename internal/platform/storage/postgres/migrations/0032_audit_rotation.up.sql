-- E32 — audit chain rotation 메타데이터 (segment).
--
-- design: docs/design/notes/audit-chain-rotation-design.md (옵션 A, 단순화된 PRIMARY 형식).
--
-- 본 round (Stage 1): 메타데이터 테이블만 도입. rotation 실행·archive 생성은 별 layer (Stage 2).
--
-- 의미:
--   - tenant 별 segment_number 단조 증가 (1부터 시작).
--   - first_entry_id / last_entry_id 는 audit_entries.seq 와 동일 의미 (tenant 내 단조 seq).
--   - segment_hash 는 segment 내 entry 들의 hash 를 fold (sha256 sequential 또는 Merkle root).
--   - archive_uri 는 'file://...' 또는 's3://...' — Backend interface 가 직접 해석.
--   - cosign_bundle 은 Sigstore keyless bundle (optional, 본 round 미주입; Stage 5 별 epic).
--
-- 불변성: audit table 정책 동등 — UPDATE/DELETE 차단 트리거 (P9 + §10.8).

CREATE TABLE audit_rotation_segments (
    id              BIGSERIAL PRIMARY KEY,
    tenant_id       TEXT NOT NULL,
    segment_number  BIGINT NOT NULL,
    started_at      TEXT NOT NULL,           -- RFC3339Nano UTC
    ended_at        TEXT NOT NULL,           -- RFC3339Nano UTC
    first_entry_id  BIGINT NOT NULL,
    last_entry_id   BIGINT NOT NULL,
    entry_count     BIGINT NOT NULL,
    segment_hash    BYTEA NOT NULL,          -- sha256 32B (segment fold hash)
    archive_uri     TEXT,                    -- 'file://...' 또는 's3://...'
    archive_sha256  BYTEA,                   -- archive 본문 sha256 32B
    cosign_bundle   BYTEA,                   -- Sigstore keyless bundle (optional)
    created_at      TEXT NOT NULL,           -- RFC3339Nano UTC
    UNIQUE (tenant_id, segment_number),
    CHECK (last_entry_id >= first_entry_id),
    CHECK (entry_count > 0)
);

CREATE INDEX idx_audit_rotation_tenant_segment
    ON audit_rotation_segments (tenant_id, segment_number);

-- P9 불변성: UPDATE/DELETE 차단.
CREATE OR REPLACE FUNCTION audit_rotation_segments_block_update()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'audit rotation segments are immutable';
END;
$$;

CREATE OR REPLACE FUNCTION audit_rotation_segments_block_delete()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'audit rotation segments are immutable';
END;
$$;

CREATE TRIGGER audit_rotation_segments_no_update
    BEFORE UPDATE ON audit_rotation_segments
    FOR EACH ROW EXECUTE FUNCTION audit_rotation_segments_block_update();

CREATE TRIGGER audit_rotation_segments_no_delete
    BEFORE DELETE ON audit_rotation_segments
    FOR EACH ROW EXECUTE FUNCTION audit_rotation_segments_block_delete();
