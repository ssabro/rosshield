-- E32 Stage 5 — prev_segment_hash 회수.

ALTER TABLE audit_rotation_segments
    DROP COLUMN IF EXISTS prev_segment_hash;
