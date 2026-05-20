-- +goose Up
-- E32 Stage 4 — audit hot GC GUC mode — SQLite 미적용 (PG only).
--
-- design: docs/design/notes/audit-chain-rotation-design.md Stage 4.
--
-- SQLite 는 session-local GUC (SET LOCAL) 미지원 — RAISE(ABORT) 트리거를 우회할 수 없습니다.
-- sqlite hot GC 는 별 메커니즘 필요 (carryover) — 후보:
--   1. 트리거를 DROP/ADD around DELETE (race condition 위험)
--   2. 별 connection level pragma + 트리거에 SELECT 조건 (sqlite 트리거 conditional 지원)
--   3. hot GC 자체를 sqlite 배포(데스크톱·단일 노드)에서 지원 안 함 (rotation 만, GC 없이 hot 무한 유지)
--
-- 본 round 는 (3) 채택 — sqlite 환경에서는 hot row 무한 보존 + cold archive 만 생성.
-- PG 멀티 인스턴스 / 어플라이언스 환경에서만 hot GC 활성.
--
-- 본 파일은 sequence 일관성 유지용 noop (goose 가 version 등록).
SELECT 1;

-- +goose Down
SELECT 1;
