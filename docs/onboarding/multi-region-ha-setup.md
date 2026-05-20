# Multi-region HA setup (E-MR Stage 3)

> **상태**: Beta — Stage 3 자동 setup 완료 (publication/subscription). Stage 4~7
> (DNS hook · 자동 failover · cross-region audit witness)는 carryover.
> **대상**: 운영자 (multi-region 배포 진행 중).
> **참조**: `docs/design/notes/multi-region-ha-design.md` (D-MR-1~5 결정 참조).

본 문서는 Lodestar를 **primary region + standby region(s)** 구성으로 배포할 때
PG logical replication 자동 setup을 활성하는 절차를 정리합니다.

---

## 1. 사전 요구사항

### 1.1 PG 서버 측 설정

primary·standby 양쪽 모두에서:

```
wal_level = logical                  # logical replication 활성 (기본은 replica)
max_wal_senders = 10                 # primary가 보내는 동시 stream 수
max_replication_slots = 10           # standby가 생성하는 slot 수
max_logical_replication_workers = 4  # standby의 worker
```

`postgresql.conf` 수정 후 PG 재시작 필요. AWS RDS/Aurora의 경우 parameter group
변경 후 reboot, GCP CloudSQL은 flag 변경.

### 1.2 DB 사용자 권한

`replica` 전용 DB user 권장 (admin과 분리):

```sql
-- primary 서버
CREATE ROLE replica WITH REPLICATION LOGIN PASSWORD 'strong-password';
GRANT USAGE ON SCHEMA public TO replica;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO replica;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO replica;
```

`REPLICATION` attribute 필수 — 없으면 standby가 logical slot 생성 실패.

### 1.3 네트워크

- standby region이 primary region PG의 5432 port에 직접 접근 가능해야 함.
- 권장: VPC peering / Transit Gateway / WireGuard tunnel.
- public Internet 노출 금지 (encrypted backbone 필수).
- conn string에 `sslmode=require` 또는 `sslmode=verify-full`.

### 1.4 PG 버전

- PG 12 이상 (logical replication 안정).
- PG 14+ 권장 (subscription 측 column 부분 일치 지원, row filter).

---

## 2. 자동 setup 동작

### 2.1 활성 조건

bootstrap 시점에 다음 4개가 모두 만족되어야 자동 setup 실행:

1. `--storage=postgres` (sqlite는 logical replication 미지원).
2. `ROSSHIELD_REPLICATION_ENABLED=true` (replication 자체 활성).
3. `ROSSHIELD_REPLICATION_ROLE` 설정 (`primary` 또는 `standby`).
4. `--replication-auto-setup=true` 또는 `ROSSHIELD_REPLICATION_AUTO_SETUP=true`.

기본은 `--replication-auto-setup=false` — 운영자가 수동으로 publication/subscription을
미리 만든 환경에 자동 setup이 충돌하는 것을 회피합니다. 첫 부팅 시 운영자가
명시적으로 켭니다.

### 2.2 primary role 동작

```text
1. pg_publication에서 publication 이름 존재 확인
2. 없으면 CREATE PUBLICATION <name> FOR ALL TABLES (또는 FOR TABLE <list>)
3. 있으면 skip (idempotent — 두 번째 부팅에 안전)
```

기본 이름: `rosshield_main`. 기본 동작: `FOR ALL TABLES` — 신규 application 테이블
추가 시 publication 갱신 누락 risk 회피 (`multi-region-ha-design.md` §4.5 권장).

### 2.3 standby role 동작

```text
1. pg_subscription에서 subscription 이름 존재 확인
2. 없으면 CREATE SUBSCRIPTION <name> CONNECTION '<primary conn>' PUBLICATION <pub>
       WITH (copy_data = false, create_slot = true, enabled = true)
3. 있으면 skip
```

기본 이름: `rosshield_main_sub`. `copy_data=false` 가정 — 초기 데이터 복사는
사전에 pg_dump/pg_restore로 끝낸 시나리오. 빈 standby에 처음 부팅하면
`--replication-copy-data=true` 옵션 (향후 추가 가능 — 본 round 미포함) 또는
수동 `ALTER SUBSCRIPTION ... REFRESH PUBLICATION WITH (copy_data = true)`.

---

## 3. 환경변수 reference

### 3.1 기본 replication 활성

| env | 설명 | 기본 |
|---|---|---|
| `ROSSHIELD_REPLICATION_ENABLED` | replication 자체 활성 | `false` |
| `ROSSHIELD_REPLICATION_REGION` | 본 인스턴스 region 식별자 | `default` |
| `ROSSHIELD_REPLICATION_ROLE` | `primary` 또는 `standby` | `primary` |
| `ROSSHIELD_REPLICATION_PRIMARY_ENDPOINT` | standby가 응답 body에 안내할 primary API base URL | "" |

### 3.2 Stage 3 자동 setup (신규)

| env / flag | 설명 | 기본 |
|---|---|---|
| `ROSSHIELD_REPLICATION_AUTO_SETUP` / `--replication-auto-setup` | 자동 setup 실행 여부 | `false` |
| `ROSSHIELD_REPLICATION_PUBLICATION_NAME` / `--replication-publication-name` | publication 이름 | `rosshield_main` |
| `ROSSHIELD_REPLICATION_PUBLICATION_ALL_TABLES` / `--replication-publication-all-tables` | `FOR ALL TABLES` 사용 | `true` |
| `ROSSHIELD_REPLICATION_SUBSCRIPTION_NAME` / `--replication-subscription-name` | subscription 이름 | `rosshield_main_sub` |
| `ROSSHIELD_REPLICATION_PRIMARY_CONN_STRING` / `--replication-primary-conn-string` | standby의 primary conn (password 포함) | "" — standby 자동 setup 시 필수 |

**보안**: `ROSSHIELD_REPLICATION_PRIMARY_CONN_STRING`은 password를 포함합니다.
- env에만 두고 disk file/secret manager로 주입 (`--replication-primary-conn-string` 사용 시 process 목록 노출 risk).
- AWS Secrets Manager / GCP Secret Manager / HashiCorp Vault 권장.
- audit log에는 conn string 본문이 남지 않습니다 — 부팅 시 logger.Info는 이름·publication만 기록.

---

## 4. 수동 setup (자동화 비활성 시)

`--replication-auto-setup=false` (default) 운영 환경에서는 다음 절차로 수동 setup.

### 4.1 primary

```sql
-- primary PG
CREATE PUBLICATION rosshield_main FOR ALL TABLES;
-- 확인
SELECT pubname, puballtables FROM pg_publication;
```

### 4.2 standby

```sql
-- standby PG
CREATE SUBSCRIPTION rosshield_main_sub
  CONNECTION 'host=primary.example.com port=5432 user=replica password=*** dbname=rosshield sslmode=require'
  PUBLICATION rosshield_main
  WITH (copy_data = false, create_slot = true, enabled = true);
-- 확인
SELECT subname, subenabled, subconninfo FROM pg_subscription;
```

`copy_data=true`는 standby가 비어있을 때만 (초기 부트스트랩).

### 4.3 적용 확인

primary에서 1 row insert → standby에서 같은 row가 보이는지 확인:

```sql
-- primary
INSERT INTO tenants (tenant_id, name) VALUES ('test-mr', 'multi-region-test');

-- standby (몇 초 후)
SELECT * FROM tenants WHERE tenant_id = 'test-mr';
```

---

## 5. Troubleshooting

### 5.1 `max_replication_slots` 초과

증상: `CREATE SUBSCRIPTION` 시 `could not create replication slot` 에러.

원인: primary의 `max_replication_slots` 값이 부족. 또는 기존 끊긴 standby의
slot이 남아있음.

해결:
```sql
-- primary에서 사용 중인 slot 확인
SELECT slot_name, active, restart_lsn FROM pg_replication_slots;
-- 끊긴 slot 제거 (active=false 확인 후)
SELECT pg_drop_replication_slot('inactive_slot_name');
```

`max_replication_slots`을 증가 (예: 10 → 20) 후 PG 재시작.

### 5.2 연결 실패 (subscription 측)

증상: standby log에 `connection to publisher refused` / `timed out`.

원인: 네트워크 / 방화벽 / pg_hba.conf 누락.

해결:
- primary `pg_hba.conf`에 standby IP 허용:
  ```
  host    rosshield    replica    10.0.0.0/16    md5
  ```
- VPC peering / Security Group 5432 ingress 확인.
- conn string의 host가 정확한지 (DNS 해석 — TTL 짧게 60s).

### 5.3 replication lag

증상: standby가 primary보다 데이터가 뒤처짐 (분 단위).

확인:
```sql
-- primary
SELECT client_addr, state, sent_lsn, write_lsn, flush_lsn, replay_lsn,
       pg_wal_lsn_diff(sent_lsn, replay_lsn) AS lag_bytes
FROM pg_stat_replication;
```

`lag_bytes`가 지속적으로 증가하면 네트워크 대역 또는 standby PG 부하 문제.

향후 Stage 6 (cross-region audit witness)에서 lag 메트릭이 Prometheus로
노출될 예정 (Phase 8 carryover).

### 5.4 publication 갱신 (테이블 추가 후)

`FOR ALL TABLES` 사용 시 신규 테이블은 자동 포함. `FOR TABLE <list>` 사용 시:

```sql
-- primary
ALTER PUBLICATION rosshield_main ADD TABLE new_table;
-- standby
ALTER SUBSCRIPTION rosshield_main_sub REFRESH PUBLICATION;
```

본 round 자동 setup은 첫 생성만 처리합니다 — 이후 변경은 수동 운영.

---

## 6. failover 절차 (Stage 1~2 manual API)

자동 setup이 완료된 후 region-A → region-B failover는 v0.6.6 manual failover
API를 사용:

```bash
# region-B (현재 standby) 인스턴스에 SSH
curl -X POST https://api-b.example.com/api/v1/replication/failover \
  -H 'Authorization: Bearer <admin-token>' \
  -H 'Content-Type: application/json' \
  -d '{"fromRegion":"us-west-2","toRegion":"ap-northeast-2","reason":"DR drill 2026-Q2"}'
```

이후 DNS hook (Stage 4 — carryover)이 Route53 record를 swap. 현재는 수동 DNS
변경 + standby PG에서 `pg_promote()` 호출 절차 필요. 상세는 별도 runbook
(`multi-region-failover-runbook.md` — 후속).

---

## 7. 한계 (Stage 4~7 carryover)

- DNS hook 실 SDK 호출 (Route53 / Cloudflare) — 현재 수동 swap.
- 자동 failover (heartbeat timeout 기반) — 현재 manual API only.
- cross-region audit witness fold-in — region 간 audit chain head SHA 일치 검증.
- replication slot 자동 cleanup — 끊긴 standby의 slot 누적 시 disk full risk.
- publication tables 변경 자동 동기화 — 첫 생성만 자동, 이후 수동.

본 한계들은 `docs/design/notes/multi-region-ha-design.md` §5 Stage 4~7에
명시되어 있으며 향후 별 epic으로 진행 예정.
