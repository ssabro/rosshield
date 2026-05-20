-- +goose Up
-- E32 Stage 4 후속 (v0.7.0 carryover) — sqlite hot GC marker 메커니즘.
--
-- design: docs/design/notes/audit-chain-rotation-design.md Stage 4.
--
-- 배경:
--   0034가 PG GUC(SET LOCAL) 방식으로 audit_entries_block_delete를 conditional 우회하지만
--   sqlite는 SET LOCAL/GUC 미지원. 본 마이그레이션은 sqlite 환경에서도 hot GC가
--   동작하도록 marker-table 방식을 도입합니다.
--
-- 메커니즘:
--   1. `audit_gc_mode` 테이블 — 단일 row marker (id PRIMARY KEY, active INT).
--   2. `audit_entries_no_delete` 트리거를 DROP/RECREATE — WHEN 절에 marker 부재 시
--      RAISE(ABORT) 조건으로 변경. marker가 active=1이면 trigger가 silent pass.
--
-- 안전 모델:
--   - rotation.HotGC가 같은 Tx 안에서 INSERT OR REPLACE → DELETE → DELETE marker
--     순서로 진행. Tx ROLLBACK 시 marker도 자동 사라짐.
--   - sqlite Tx의 row 변경은 같은 connection의 trigger WHEN 절에서 즉시 보임
--     (Read-Your-Writes 보장).
--   - application code가 audit_gc_mode를 직접 manipulate 가능하지만 P5 도메인 경계
--     + 코드 리뷰가 HotGC 한정 사용 강제. PG의 GUC 신뢰 모델과 같은 등급.
--
-- 트리거 명세 변경:
--   기존: BEFORE DELETE ON audit_entries
--         BEGIN SELECT RAISE(ABORT, 'audit log is immutable'); END;
--   변경: BEFORE DELETE ON audit_entries
--         WHEN NOT EXISTS (SELECT 1 FROM audit_gc_mode WHERE id = 1 AND active = 1)
--         BEGIN SELECT RAISE(ABORT, 'audit log is immutable'); END;

CREATE TABLE IF NOT EXISTS audit_gc_mode (
    id     INTEGER PRIMARY KEY CHECK (id = 1),
    active INTEGER NOT NULL DEFAULT 0 CHECK (active IN (0, 1))
);

DROP TRIGGER IF EXISTS audit_entries_no_delete;

CREATE TRIGGER audit_entries_no_delete
    BEFORE DELETE ON audit_entries
    WHEN NOT EXISTS (
        SELECT 1 FROM audit_gc_mode WHERE id = 1 AND active = 1
    )
    BEGIN SELECT RAISE(ABORT, 'audit log is immutable'); END;

-- +goose Down
DROP TRIGGER IF EXISTS audit_entries_no_delete;

CREATE TRIGGER audit_entries_no_delete
    BEFORE DELETE ON audit_entries
    BEGIN SELECT RAISE(ABORT, 'audit log is immutable'); END;

DROP TABLE IF EXISTS audit_gc_mode;
