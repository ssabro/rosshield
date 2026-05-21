-- Phase 10.D-2 회수 — audit_chain_keys 테이블 + 불변성 트리거 제거.

DROP TRIGGER IF EXISTS audit_chain_keys_no_delete ON audit_chain_keys;
DROP TRIGGER IF EXISTS audit_chain_keys_no_update ON audit_chain_keys;
DROP FUNCTION IF EXISTS audit_chain_keys_block_delete();
DROP FUNCTION IF EXISTS audit_chain_keys_block_update();
DROP INDEX IF EXISTS idx_audit_chain_keys_tenant_created;
DROP TABLE IF EXISTS audit_chain_keys;
