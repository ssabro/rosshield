-- E32 Stage 4 — audit hot GC GUC mode (session-local SET LOCAL bypass).
--
-- design: docs/design/notes/audit-chain-rotation-design.md Stage 4 (hot GC, 옵션 A — GUC).
--
-- 본 마이그레이션은 0002에서 정의된 audit_entries_block_delete 함수를 갱신하여
-- session-local GUC `rosshield.audit_gc_mode = 'on'` 일 때만 DELETE 를 허용합니다.
--
-- UPDATE는 항상 차단 (audit_entries_block_update 함수 무변경).
-- application code 의 DELETE는 차단 유지 — rotation.HotGC만 SET LOCAL로 우회.
--
-- 절차 (rotation.HotGC.Run 내부):
--   1. tx 시작 (BEGIN)
--   2. SET LOCAL rosshield.audit_gc_mode = 'on'  (tx 끝에서 자동 reset)
--   3. DELETE FROM audit_entries WHERE tenant_id=$1 AND id BETWEEN $2 AND $3
--   4. audit.gc.complete entry append (chain link, GUC는 INSERT에 영향 없음)
--   5. COMMIT
--
-- 본 GUC는 PG 만 — sqlite는 별 메커니즘 필요 (carryover).
--
-- 보안 가정: SET LOCAL은 application 코드만 호출 — DB superuser 신뢰 모델. P9 트리거가
-- "잘못된 SET LOCAL 우회"를 막지는 못하므로, 본 우회는 application audit 도메인 권한 경계
-- 내부 (HotGC 만)에서만 사용되는 것을 코드 리뷰로 보장합니다.

CREATE OR REPLACE FUNCTION audit_entries_block_delete()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    -- GC mode 'on' 일 때만 DELETE 허용 (rotation.HotGC 가 SET LOCAL 로 활성화).
    -- current_setting 의 두 번째 인자 true 는 GUC 미정의 시 NULL 반환 (예외 회피).
    IF current_setting('rosshield.audit_gc_mode', true) = 'on' THEN
        RETURN OLD;
    END IF;
    RAISE EXCEPTION 'audit log is immutable';
END;
$$;
