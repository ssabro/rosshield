-- +goose Up
-- E32 — audit chain rotation 메타데이터 (segment) — SQLite 버전.
-- 참조: docs/design/notes/audit-chain-rotation-design.md (옵션 A).
--
-- 본 round (Stage 1): 메타데이터 테이블만 도입. rotation 실행·archive 생성은 별 layer.

CREATE TABLE audit_rotation_segments (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    tenant_id       TEXT NOT NULL,
    segment_number  INTEGER NOT NULL,
    started_at      TEXT NOT NULL,           -- RFC3339Nano UTC
    ended_at        TEXT NOT NULL,           -- RFC3339Nano UTC
    first_entry_id  INTEGER NOT NULL,
    last_entry_id   INTEGER NOT NULL,
    entry_count     INTEGER NOT NULL,
    segment_hash    BLOB NOT NULL,           -- sha256 32B (segment fold hash)
    archive_uri     TEXT,                    -- 'file://...' 또는 's3://...'
    archive_sha256  BLOB,                    -- archive 본문 sha256 32B
    cosign_bundle   BLOB,                    -- Sigstore keyless bundle (optional)
    created_at      TEXT NOT NULL,           -- RFC3339Nano UTC
    UNIQUE (tenant_id, segment_number),
    CHECK (last_entry_id >= first_entry_id),
    CHECK (entry_count > 0)
);

CREATE INDEX idx_audit_rotation_tenant_segment
    ON audit_rotation_segments (tenant_id, segment_number);

-- P9 불변성: UPDATE/DELETE 차단 (§10.8).
CREATE TRIGGER audit_rotation_segments_no_update
    BEFORE UPDATE ON audit_rotation_segments
    BEGIN SELECT RAISE(ABORT, 'audit rotation segments are immutable'); END;

CREATE TRIGGER audit_rotation_segments_no_delete
    BEFORE DELETE ON audit_rotation_segments
    BEGIN SELECT RAISE(ABORT, 'audit rotation segments are immutable'); END;

-- +goose Down
DROP TRIGGER IF EXISTS audit_rotation_segments_no_delete;
DROP TRIGGER IF EXISTS audit_rotation_segments_no_update;
DROP INDEX IF EXISTS idx_audit_rotation_tenant_segment;
DROP TABLE IF EXISTS audit_rotation_segments;
