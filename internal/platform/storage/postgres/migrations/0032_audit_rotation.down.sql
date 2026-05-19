-- E32 — audit_rotation_segments 회수.

DROP TRIGGER IF EXISTS audit_rotation_segments_no_delete ON audit_rotation_segments;
DROP TRIGGER IF EXISTS audit_rotation_segments_no_update ON audit_rotation_segments;
DROP FUNCTION IF EXISTS audit_rotation_segments_block_delete();
DROP FUNCTION IF EXISTS audit_rotation_segments_block_update();
DROP INDEX IF EXISTS idx_audit_rotation_tenant_segment;
DROP TABLE IF EXISTS audit_rotation_segments;
