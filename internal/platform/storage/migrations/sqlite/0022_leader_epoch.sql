-- +goose Up
-- 0022_leader_epoch.sql — E25 HA leader-election 메타데이터.
--
-- PG 환경(--storage=postgres + --ha-enabled)에서만 사용됩니다. sqlite 환경은
-- advisory lock 동등 기능이 없어 HA 비대상이며, sqlite + --ha-enabled 조합은
-- bootstrap이 부팅을 거부합니다 (R30-2 결정 — 부속2 = 부팅 실패).
--
-- 설계: docs/design/notes/e25-ha-design.md §4.3 fence token (split-brain 방지),
--       §10 R30-2 결정 항목.
--
-- 컬럼:
--   epoch        — leader 승격 시마다 단조 증가하는 fence token. PG는 sequence,
--                  sqlite는 AUTOINCREMENT. audit chain INSERT 시 자기 epoch와
--                  current_epoch가 다르면 즉시 abort + leader 자격 박탈.
--   leader_id    — "hostname:pid" — 운영자가 어느 인스턴스가 현재 leader인지 파악용.
--   acquired_at  — ISO 8601 UTC.
--   current      — 정확히 1 row만 current=1 (UNIQUE WHERE current=1 partial index).

CREATE TABLE leader_epoch (
    epoch       INTEGER PRIMARY KEY AUTOINCREMENT,
    leader_id   TEXT    NOT NULL,
    acquired_at TEXT    NOT NULL,
    current     INTEGER NOT NULL DEFAULT 0
);

CREATE UNIQUE INDEX leader_epoch_current ON leader_epoch (current) WHERE current = 1;

-- +goose Down
DROP INDEX IF EXISTS leader_epoch_current;
DROP TABLE IF EXISTS leader_epoch;
