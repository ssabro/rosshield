-- E32 Stage 4 후속 (v0.7.0 carryover) — sqlite hot GC marker 메커니즘.
--
-- PG는 이미 0034에서 GUC(SET LOCAL) 방식으로 audit_entries_block_delete를 conditional
-- 우회하므로 본 마이그레이션은 noop. sequence 일관성 유지용으로만 등록.
--
-- sqlite 환경은 별도 audit_gc_mode 테이블 + WHEN 절 트리거를 본 sequence 번호에 등록.

SELECT 1;
