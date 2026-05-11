-- E25 HA leader-election 메타데이터 — PG 버전.
--
-- PG는 sequence로 fence token(epoch)을 발급합니다. sqlite와 컬럼 의미·index는
-- 동일하지만 epoch 발급 메커니즘이 다릅니다:
--   - PG  : nextval('leader_epoch_seq')
--   - sqlite: INSERT ... RETURNING rowid (AUTOINCREMENT)
--
-- 설계: docs/design/notes/e25-ha-design.md §4.3 fence token.

CREATE SEQUENCE IF NOT EXISTS leader_epoch_seq START 1;

CREATE TABLE leader_epoch (
    epoch       BIGINT   NOT NULL,
    leader_id   TEXT     NOT NULL,
    acquired_at TEXT     NOT NULL,
    current     SMALLINT NOT NULL DEFAULT 0,
    PRIMARY KEY (epoch)
);

CREATE UNIQUE INDEX leader_epoch_current ON leader_epoch (current) WHERE current = 1;
