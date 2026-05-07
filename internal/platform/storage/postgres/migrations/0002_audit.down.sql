-- E22-B — 0002 down. 트리거·함수·테이블 역순 제거.
DROP TRIGGER IF EXISTS audit_checkpoints_no_delete ON audit_checkpoints;
DROP TRIGGER IF EXISTS audit_checkpoints_no_update ON audit_checkpoints;
DROP TRIGGER IF EXISTS audit_entries_no_delete ON audit_entries;
DROP TRIGGER IF EXISTS audit_entries_no_update ON audit_entries;
DROP FUNCTION IF EXISTS audit_checkpoints_block_delete();
DROP FUNCTION IF EXISTS audit_checkpoints_block_update();
DROP FUNCTION IF EXISTS audit_entries_block_delete();
DROP FUNCTION IF EXISTS audit_entries_block_update();
DROP TABLE IF EXISTS audit_checkpoints;
DROP TABLE IF EXISTS audit_chain_heads;
DROP INDEX IF EXISTS audit_entries_tenant_occurred;
DROP TABLE IF EXISTS audit_entries;
