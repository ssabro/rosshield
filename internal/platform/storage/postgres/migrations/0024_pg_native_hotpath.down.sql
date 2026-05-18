ALTER TABLE audit_entries
    ALTER COLUMN occurred_at TYPE TEXT
    USING to_char(occurred_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"');

ALTER TABLE audit_chain_heads
    ALTER COLUMN updated_at TYPE TEXT
    USING to_char(updated_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"');

-- evidence_json은 up에서 JSONB로 변경 + DEFAULT '[]'::jsonb. 회수 시 동일 패턴
-- (DROP DEFAULT → TYPE → SET DEFAULT '[]') — 자동 cast 가능 case이긴 하나 명시 처리.
ALTER TABLE insights
    ALTER COLUMN evidence_json DROP DEFAULT;

ALTER TABLE insights
    ALTER COLUMN evidence_json TYPE TEXT
    USING evidence_json::text;

ALTER TABLE insights
    ALTER COLUMN evidence_json SET DEFAULT '[]';
