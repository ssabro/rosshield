-- +goose Up
-- 0024_pg_native_hotpath.sql — E22-F R30-1 = C 하이브리드 (핫 path만 PG-native).
--
-- SQLite 마이그레이션은 cosmetic NO-OP. PG·SQLite 마이그레이션 시퀀스 일관성을
-- 위해 동일 번호 부여하지만, SQLite은 TEXT 컬럼을 그대로 유지합니다.
--
-- PG 측은 0024_pg_native_hotpath.up.sql에서 3 컬럼 ALTER TYPE:
--   audit_entries.occurred_at      TEXT → TIMESTAMPTZ (range query 빈번 — Verify/Export)
--   audit_chain_heads.updated_at   TEXT → TIMESTAMPTZ (checkpoint 시간 비교)
--   insights.evidence_json         TEXT → JSONB        (GIN 인덱스·JSONB query 잠재 활용)
--
-- 설계: docs/design/notes/e22-f-pg-native-design.md (R30-1 결정 = 하이브리드).

SELECT 1; -- NO-OP (goose는 빈 statement 거부, no-op SELECT로 마이그레이션 카운트만 증가)

-- +goose Down
SELECT 1; -- NO-OP
