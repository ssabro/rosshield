-- E22-F R30-1 = C 하이브리드 — PG 핫 path 3 컬럼을 native type으로 복원.
--
-- 변경 영향:
--   - sqliterepo는 변경 0. PG가 TIMESTAMPTZ에 RFC3339 string 인자를 자동 파싱·
--     SELECT 시 string SCAN target에 자동 직렬화하므로 driver-agnostic 코드 그대로 동작.
--   - JSONB 도 동일 (TEXT 인자 자동 캐스트, string/byte SCAN 지원).
--   - PG는 range query·GIN 인덱스 활용 가능 — 향후 audit_entries occurred_at BETWEEN
--     쿼리 plan이 sqlite-equivalent TEXT 비교 대비 빨라짐.
--
-- 비포함:
--   - BOOLEAN 회수는 driver type 강제로 인한 mismatch 위험 → 보류 (현 SMALLINT 유지).
--   - 다른 _at TEXT 컬럼들 (created_at·updated_at 등)은 1차에서 제외 — 핫 path 식별 후 점진 확장.
--
-- 설계: docs/design/notes/e22-f-pg-native-design.md (R30-1 결정 = 하이브리드).

ALTER TABLE audit_entries
    ALTER COLUMN occurred_at TYPE TIMESTAMPTZ
    USING occurred_at::timestamptz;

ALTER TABLE audit_chain_heads
    ALTER COLUMN updated_at TYPE TIMESTAMPTZ
    USING updated_at::timestamptz;

-- insights.evidence_json은 0014에서 TEXT DEFAULT '[]'로 정의됨.
-- TEXT default '[]'은 JSONB로 자동 cast 불가 (42804) — DROP DEFAULT → ALTER TYPE →
-- SET DEFAULT 패턴으로 분리.
ALTER TABLE insights
    ALTER COLUMN evidence_json DROP DEFAULT;

ALTER TABLE insights
    ALTER COLUMN evidence_json TYPE JSONB
    USING evidence_json::jsonb;

ALTER TABLE insights
    ALTER COLUMN evidence_json SET DEFAULT '[]'::jsonb;
