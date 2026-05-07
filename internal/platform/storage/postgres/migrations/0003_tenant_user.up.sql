-- E22-B — SQLite 0003_tenant_user.sql 변환분.
--
-- 본 마이그레이션은 의도적 NO-OP입니다. 이유는 다음과 같습니다.
--   * SQLite 0003_tenant_user.sql은 `tenants`·`users` 테이블 + `users_tenant` 인덱스를 생성합니다.
--   * E22-A scaffold 단계에서 PG 0001_tenant_init은 SQLite 0001(platform_info) + 0003(tenants/users)을
--     하나로 합쳐 이미 적용했습니다 (참조: postgres/migrations/0001_tenant_init.up.sql §17~§54).
--   * 시퀀스 번호를 SQLite 와 1:1 정렬하기 위해 0003 파일 자체는 보존하되 본문은 비워둡니다.
--
-- 향후 0003 슬롯에 새 DDL을 추가해야 한다면 본 NO-OP 사실을 반드시 갱신하세요.
SELECT 1; -- migrate 라이브러리가 빈 파일을 거부하는 경우 대비한 placeholder 표현식.
