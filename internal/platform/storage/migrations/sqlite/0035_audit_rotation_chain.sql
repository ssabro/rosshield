-- +goose Up
-- E32 Stage 5 — audit rotation segment chain link (prev_segment_hash) — SQLite.
-- design: docs/design/notes/audit-chain-rotation-design.md Stage 5.
--
-- segment 간 chain link column. segment N의 segment_hash를 segment N+1의 prev_segment_hash로
-- 기록하여 cold 영역 chain 무결성을 entry-level chain (audit_entries.prev_hash)와 별도 layer로
-- 보장합니다. segment_number = 1은 NULL.

ALTER TABLE audit_rotation_segments
    ADD COLUMN prev_segment_hash BLOB;

-- +goose Down
-- SQLite 3.35+ (2021-03-12)는 DROP COLUMN 지원. 더 낮은 버전은 table rebuild 필요.
ALTER TABLE audit_rotation_segments
    DROP COLUMN prev_segment_hash;
