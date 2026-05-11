-- +goose Up
-- 0023_audit_leader_epoch.sql — E25 Stage 2: audit_entries에 leader_epoch 컬럼 추가.
--
-- 의미:
--   NULL  → HA 비활성 환경에서 INSERT됨 (단일 인스턴스, fence token 무관).
--   양수  → HA 활성 환경에서 INSERT됨. 값은 leader_epoch 테이블의 epoch과 일치.
--          향후 검증 도구가 audit chain의 leader_epoch가 단조 증가하는지 추가 검증 가능.
--
-- 컬럼은 nullable로 추가하여 기존 row 호환 유지 (DEFAULT NULL).
-- audit_chain_heads에는 컬럼 추가 X — head는 hash 체인만 추적.
--
-- 설계: docs/design/notes/e25-ha-design.md §4.3 fence token (split-brain 방지).

ALTER TABLE audit_entries ADD COLUMN leader_epoch INTEGER;

-- +goose Down
-- SQLite는 DROP COLUMN 지원 (3.35+). 본 프로젝트는 modernc.org/sqlite 최신 — OK.
ALTER TABLE audit_entries DROP COLUMN leader_epoch;
