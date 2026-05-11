ALTER TABLE audit_entries
    ALTER COLUMN occurred_at TYPE TEXT
    USING to_char(occurred_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"');

ALTER TABLE audit_chain_heads
    ALTER COLUMN updated_at TYPE TEXT
    USING to_char(updated_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"');

ALTER TABLE insights
    ALTER COLUMN evidence_json TYPE TEXT
    USING evidence_json::text;
