-- +goose Up
-- 플랫폼 부트스트랩. 도메인 테이블은 후속 에픽(E2 audit·E3 tenant)에서 추가됩니다.
-- platform_info는 현재 schema 버전·인스턴스 메타를 보관하는 단일 KV 테이블.
CREATE TABLE platform_info (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- +goose Down
DROP TABLE platform_info;
