-- E22-B — 0012 down. 컬럼 재추가 후 N:M 테이블 제거 (SQLite와 동일 정책).
ALTER TABLE scan_results ADD COLUMN evidence_ref TEXT NOT NULL DEFAULT '';
DROP INDEX IF EXISTS evidence_refs_evidence;
DROP TABLE IF EXISTS evidence_refs;
DROP INDEX IF EXISTS evidence_records_tenant_sha;
DROP TABLE IF EXISTS evidence_records;
