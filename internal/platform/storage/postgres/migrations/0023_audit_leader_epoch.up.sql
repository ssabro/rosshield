-- E25 Stage 2 — PG 버전.
-- audit_entries에 leader_epoch BIGINT 컬럼 추가 (nullable, 기존 row 호환).
--
-- HA 비활성 시 NULL, 활성 시 leader_epoch 테이블의 current epoch과 일치.

ALTER TABLE audit_entries ADD COLUMN leader_epoch BIGINT;
