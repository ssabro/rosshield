-- E32 Stage 5 — audit rotation segment chain link (prev_segment_hash).
--
-- design: docs/design/notes/audit-chain-rotation-design.md Stage 5 (verify CLI 확장).
--
-- 본 마이그레이션은 segment N의 segment_hash를 segment N+1의 prev_segment_hash column에
-- 기록해 segment 간 chain link를 구성합니다 (entry 내 chain (`prev_hash`)와 별도 layer).
--
-- 외부 감사인은 segment archive 들을 순서대로 fetch한 뒤 각 archive의 manifest 내
-- prev_segment_hash가 직전 segment의 segment_hash와 일치하는지 확인하여 cold 영역까지
-- chain 무결성을 확장 검증합니다.
--
-- segment_number = 1 (첫 segment)는 prev_segment_hash = NULL.
-- 이후 segment는 prev_segment_hash = audit_rotation_segments(segment_number - 1).segment_hash.
--
-- 본 column 추가는 0032에서 정의된 segments 테이블의 UPDATE를 요구하지 않습니다 —
-- 신규 INSERT 시점에 채워지며 P9 불변성 트리거가 그 이후 변경을 차단합니다.

ALTER TABLE audit_rotation_segments
    ADD COLUMN prev_segment_hash BYTEA;

COMMENT ON COLUMN audit_rotation_segments.prev_segment_hash IS
    'previous segment hash (segment-level chain link). NULL for segment 1.';
