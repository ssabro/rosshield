-- E32 Stage 4 — GC GUC bypass 회수. 0002의 원본 함수 복원.

CREATE OR REPLACE FUNCTION audit_entries_block_delete()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'audit log is immutable';
END;
$$;
