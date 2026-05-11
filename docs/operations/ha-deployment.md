# HA 배포 운영 가이드

> **대상**: Phase 5 enterprise customer 운영자
> **범위**: rosshield-server를 leader/follower 토폴로지로 배포·운영하기 위한 절차
> **참조 설계**: [`docs/design/notes/e25-ha-design.md`](../design/notes/e25-ha-design.md)
> **상태**: E25 Stage 1~3 구현 완료 기준 (head e03cc6c)

---

## 1. 개요

### HA 모델

rosshield-server의 HA는 **PG advisory lock 기반 leader/follower** 모델입니다 (R30-2 결정).

- 여러 인스턴스가 동시에 떠 있어도 **임의 시점에 leader는 정확히 1개**
- leader만 audit chain INSERT, write API, scheduler 실행
- follower는 read API(`GET /api/v1/*`)를 서빙. write 요청은 `503 NOT_LEADER + Retry-After` 응답
- leader crash → 5~15s 내 follower 중 하나가 자동 승격 (epoch +1)

### 핵심 제약

`§10.4` audit 해시 체인은 `hash_i = sha256(hash_{i-1} || ...)` 구조이므로 **두 인스턴스가 동시에 INSERT 하면 prevHash race**가 발생합니다. 이를 막기 위해:

1. PG advisory lock으로 leader-election (단일 writer 보장)
2. **fence token (leader epoch)** — leader 승격 시마다 PG sequence로 발급. audit row에 epoch 기록 → stale leader의 write 차단

### 비목표

| 항목 | 상태 | 대안 |
|---|---|---|
| PostgreSQL 자체 HA | ❌ | 별도 streaming replication(primary + standby) 가정. patroni / repmgr 등 검증된 솔루션 권장 |
| SQLite 환경 HA | ❌ | sqlite는 단일 인스턴스 운영. HA 필요 시 PG로 마이그레이션 |
| Active-Active (multi-leader) | ❌ | audit chain 구조상 불가 |
| Geo-replication | ❌ | 본 epic 범위 밖 |

### 권장 토폴로지

```
                     ┌───────────────────┐
                     │   nginx (LB)      │
                     │   :8443 → rr      │
                     └────────┬──────────┘
                              │
              ┌───────────────┼───────────────┐
              │               │               │
       ┌──────▼─────┐  ┌──────▼─────┐  ┌─────▼──────┐
       │ rosshield  │  │ rosshield  │  │ rosshield  │
       │ -a (leader)│  │ -b (follow)│  │ -c (follow)│
       └──────┬─────┘  └──────┬─────┘  └─────┬──────┘
              │               │               │
              └───────────────┼───────────────┘
                              │
                       ┌──────▼─────┐
                       │ PostgreSQL │  (별도 HA 가정)
                       │ primary    │
                       └────────────┘
```

- **인스턴스 수**: 2~3개 (3개가 rolling update + 장애 허용 측면 권장)
- **LB**: nginx 1.18+ (`proxy_next_upstream http_503` 지원)
- **PG**: 16+ (advisory lock, sequence) — 별도 HA 구성

---

## 2. 사전 준비

### 2.1 PostgreSQL 요구사항

- **버전**: PostgreSQL 16+ (advisory lock + sequence 의존)
- **사용자**: `rosshield` (CREATE/INSERT/UPDATE/DELETE on rosshield DB + sequence usage 권한)
- **연결 풀**: heartbeat 전용 connection (라이브 세션 유지) — `maxConns >= 2 * 인스턴스 수` 권장

### 2.2 마이그레이션 적용 확인

E25 Stage 1~2가 추가한 마이그레이션 파일:

| 파일 | 내용 |
|---|---|
| `0022_leader_epoch.up.sql` | `leader_epoch` 테이블 + `leader_epoch_seq` 시퀀스 |
| `0023_audit_leader_epoch.up.sql` | `audit_entries.leader_epoch` 컬럼 추가 |

적용 명령:

```bash
make pg-migrate-up PG_DSN='postgres://rosshield:PASSWORD@HOST:5432/rosshield?sslmode=disable'

# 적용 버전 확인
make pg-migrate-status PG_DSN='postgres://...'
# → 23 (clean)
```

### 2.3 nginx 요구사항

- **버전**: 1.18+ (`proxy_next_upstream` + WebSocket upgrade)
- **모듈**: 표준 빌드(별도 모듈 불필요)
- 설정 예시: [`deploy/compose/nginx-ha.conf`](../../deploy/compose/nginx-ha.conf)

---

## 3. 단일 인스턴스 → HA 전환 절차

### 3.1 사전 백업 (필수)

```bash
# Postgres dump (단일 인스턴스가 이미 PG라면 skip 가능 — 하지만 안전망 권장)
pg_dump -U rosshield -h <CURRENT_HOST> rosshield > backup-$(date +%Y%m%d-%H%M%S).sql

# audit chain 무결성 사전 검증 (Phase 4 E30 verify CLI)
./bin/rosshield-audit-verify --pg-dsn "postgres://rosshield:...@HOST/rosshield"
# → "chain OK: N entries verified"
```

### 3.2 storage 전환 (sqlite 사용 중이라면)

sqlite 사용자라면 PG로 사전 마이그레이션이 필요합니다 (`docs/operations/sqlite-to-postgres-migration.md` 참조 — TODO: Stage 5 별도 문서).

```bash
# 단일 인스턴스로 PG 모드 부팅 후 동작 검증
./bin/rosshield-server --storage=postgres \
  --pg-dsn "postgres://rosshield:...@HOST/rosshield"
```

### 3.3 HA 부팅 시퀀스

```bash
# 1) PG 먼저 기동 (compose 사용 시 healthcheck로 보장됨)
docker compose -f deploy/compose/compose-ha.yml up -d postgres

# 2) 마이그레이션 적용
make pg-migrate-up PG_DSN='postgres://rosshield:...@localhost/rosshield?sslmode=disable'

# 3) 양쪽 rosshield 인스턴스 동시 부팅
docker compose -f deploy/compose/compose-ha.yml up -d rosshield-a rosshield-b

# 4) leader 확인 (한쪽만 leader여야 함)
docker exec rosshield-a wget -qO- http://localhost:8080/healthz | jq .value.ha
# → { "enabled": true, "role": "leader",   "epoch": 1, "leaderId": "rosshield-a:1", ... }

docker exec rosshield-b wget -qO- http://localhost:8080/healthz | jq .value.ha
# → { "enabled": true, "role": "follower", "epoch": 1, "leaderId": "rosshield-a:1", ... }

# 5) nginx 추가 (양 인스턴스 healthy 확인 후)
docker compose -f deploy/compose/compose-ha.yml up -d nginx

# 6) 외부에서 LB 경유 접근 검증
curl -s http://localhost:8443/healthz
```

### 3.4 외부 DNS / LB 전환

기존 단일 인스턴스 hostname을 가리키던 DNS A 레코드를 nginx LB로 전환합니다.

- **TTL을 사전에 60s 이하로 낮춰두기** (전환 1~24시간 전)
- 전환 후 5~10분 모니터링: `/healthz` 응답률, 에러 로그
- 문제 발생 시 DNS 롤백

### 3.5 롤백 시나리오

```bash
# 1) DNS 원복
# 2) HA 인스턴스 정지
docker compose -f deploy/compose/compose-ha.yml down

# 3) (필요 시) PG dump 복원
psql -U rosshield rosshield < backup-YYYYMMDD-HHMMSS.sql

# 4) 단일 인스턴스 재기동
./bin/rosshield-server --storage=postgres --pg-dsn '...'
```

**주의**: HA 가동 중 audit chain에 새 entry가 추가됐다면 dump 복원은 데이터 손실을 의미합니다. 가능한 한 dump 복원 없이 HA 환경에서 문제 해결을 먼저 시도하세요.

---

## 4. 운영 절차

### 4.1 failover 시연 (검증)

```bash
# 1) 현재 leader 확인
curl -s http://rosshield-a:8080/healthz | jq -r .value.ha.role
# → leader

# 2) leader kill
docker kill rosshield-a

# 3) 5~15s 대기 후 follower 승격 확인
sleep 15
curl -s http://rosshield-b:8080/healthz | jq .value.ha
# → { "enabled": true, "role": "leader", "epoch": 2, ... }
#   epoch이 1 → 2로 증가했음을 확인

# 4) LB 경유 요청도 정상 응답
curl -s http://localhost:8443/api/v1/healthz

# 5) 원래 leader 복구
docker start rosshield-a
sleep 10
curl -s http://rosshield-a:8080/healthz | jq -r .value.ha.role
# → follower (B가 여전히 leader, A는 follower로 가담)
```

### 4.2 일상 점검 항목

| 항목 | 체크 방법 | 정상 기준 |
|---|---|---|
| 양 인스턴스 healthy | `curl /healthz` | 둘 다 200 |
| leader 정확히 1개 | 양쪽 `.value.ha.role` 비교 | leader 1 + follower N |
| epoch 단조 증가 | `SELECT max(epoch) FROM leader_epoch` | 시간순으로 증가만, 감소 없음 |
| heartbeat 살아있음 | `.value.ha.lastHeartbeatAt` | 현재 시각 - 10s 이내 |
| audit chain 무결성 | `rosshield-audit-verify` 주기 실행 | "chain OK" |

### 4.3 Prometheus 메트릭

현재 노출 중 (Stage 1~3 기준):

- `rosshield_scan_started_total{tenant}` — 스캔 시작 카운터 (기존)
- HTTP 표준 메트릭 (request count, latency)

향후(Stage 5 예정) 추가 예상:

- `rosshield_ha_role` (gauge, 0=follower 1=leader)
- `rosshield_ha_leader_epoch` (gauge)
- `rosshield_ha_failover_total` (counter)
- `rosshield_ha_heartbeat_failures_total` (counter)

알림 권장: **`rosshield_ha_failover_total` 1시간 내 3회 이상** → flapping 의심, 운영자 호출.

### 4.4 로그 키워드

운영 시 grep할 핵심 로그:

| 키워드 | 의미 | 조치 |
|---|---|---|
| `ha: promoted to leader` | follower → leader 승격 | 정상. epoch 함께 기록 |
| `ha: heartbeat failed, demoting` | heartbeat 실패로 leader 자격 상실 | PG 연결 상태 확인 |
| `ha: lock acquisition failed` | 부팅 시 또는 회복 시 follower 대기 | 정상 (다른 leader 존재) |
| `audit: ErrEpochStale` | stale leader의 INSERT 차단됨 | fence token 정상 동작. 빈번하면 GC pause 조사 |
| `bootstrap: ha-enabled requires --storage=postgres` | sqlite + HA 조합 거부 | 설정 오류 — §5 참조 |

### 4.5 rolling update 절차

무중단 업그레이드 (예: v0.3.0 → v0.3.1):

```bash
# 1) follower 먼저 업그레이드
docker compose -f deploy/compose/compose-ha.yml stop rosshield-b
# 이미지 태그 갱신 후
docker compose -f deploy/compose/compose-ha.yml up -d rosshield-b

# 2) follower healthy 확인
curl -s http://rosshield-b:8080/healthz | jq -r .value.status
# → healthy (role은 follower)

# 3) 의도적 failover — 현재 leader(rosshield-a) graceful shutdown
docker compose -f deploy/compose/compose-ha.yml stop rosshield-a
# → rosshield-b 가 약 5~15s 내 leader 승격

# 4) rosshield-a 업그레이드 후 재기동
docker compose -f deploy/compose/compose-ha.yml up -d rosshield-a
# → follower로 가담
```

다운타임 = failover window(5~15s)에 한정.

---

## 5. 트러블슈팅

| 증상 | 가능한 원인 | 해결 |
|---|---|---|
| 두 인스턴스 모두 follower (no leader) | PG 다운 / 네트워크 단절 / 양쪽 모두 lock 획득 실패 | (1) `pg_isready -h <HOST>` 확인 (2) 컨테이너 간 네트워크 점검 (3) PG에서 `SELECT * FROM pg_locks WHERE locktype='advisory'` 로 외부 락 점유 여부 확인 |
| follower 부팅 실패 with `ha-enabled requires --storage=postgres` | `--storage=sqlite` + `--ha-enabled` 조합 | `--storage=postgres` + `--pg-dsn` 또는 `ROSSHIELD_DATABASE_URL` 환경변수 설정 |
| 503 NOT_LEADER 응답 비율이 높음 | LB가 leader 자동 라우팅 미설정 | `nginx-ha.conf`에 `proxy_next_upstream error timeout http_503` 지시문 확인. 응답 헤더 `X-Upstream-Status` 추적 |
| failover 후 audit chain 불일치 (이론상 발생 X) | fence token 검증이 우회된 경우 — 심각 | (1) `leader_epoch` 테이블 검사 (2) `rosshield-audit-verify` 즉시 실행 (3) 발견 시 incident 보고 + chain 분기점 식별 |
| leader가 자주 바뀜 (flapping) | heartbeat interval 너무 짧음 / PG 연결 풀 고갈 / 네트워크 jitter | (1) `--ha-heartbeat-interval=5s` 권장 (2) PG `max_connections` 확인 (3) `rosshield_ha_failover_total` 메트릭 추세 분석 |
| `/healthz`에 `ha` 필드 없음 | `--ha-enabled` 미설정 — 단일 인스턴스 모드 | 의도된 동작. HA 모드 활성화 필요 시 플래그 추가 |
| WebSocket 진행 스트림 끊김 | failover로 leader 교체됨 | UX 정상. 클라이언트 재연결 로직(`§09 web UI`)으로 자동 복구 |
| 부팅 직후 `pg_try_advisory_lock` 타임아웃 | PG migrations 미적용 | `make pg-migrate-status` 확인, `0022`/`0023` 적용 필요 |

---

## 6. 알려진 한계

### 6.1 PG가 SPOF

본 HA 모델은 **PostgreSQL이 살아있을 것**을 전제합니다. PG 자체의 HA(streaming replication, patroni, repmgr 등)는 별도 구성이며 본 epic 범위 밖입니다. PG 단일 노드 운영 시 PG 다운 = 전체 cluster 다운.

권장: enterprise customer는 PG primary + 1+ standby + 자동 failover 도구(patroni 등)와 함께 운영.

### 6.2 failover 동안의 다운타임

- **윈도우**: 5~15s (PG 세션 timeout + heartbeat 주기)
- **영향**: 이 시간 동안 모든 write API는 503 응답. read는 follower가 계속 서빙
- **WebSocket**: in-flight 연결은 끊김. 클라이언트 재연결 로직 필수

5초 미만의 failover는 본 메커니즘으로 달성 불가. 더 빠른 failover가 필요하면 etcd lease 기반(R30-2 후보 B)으로 재설계 필요.

### 6.3 PG primary failover 시 epoch 도약

PG streaming replication 구성에서 primary가 standby로 promote 되면, `leader_epoch_seq` 시퀀스 값은 WAL replication으로 동기화되지만 **promote 직후 sequence 캐시 차이로 epoch이 도약**할 수 있습니다.

- 영향: epoch 단조 증가 검증은 유지됨 (도약은 증가 방향)
- 위험: 작지만 audit chain 검증 시 epoch 차이가 통계 anomaly로 잡힐 수 있음
- 완화: PG primary failover 시 운영자가 incident note에 "PG promote @ <ts>" 기록 → audit verify 출력과 대조

### 6.4 Active-Active 요구는 본 모델로 불가

read traffic을 모든 인스턴스에 분산하는 것은 가능하지만 (현재 구조), write traffic을 다중 leader에 분산하는 것은 audit chain 구조상 불가능합니다. write 부하 분산이 필요하면 tenant별 sharding(R30-2 부속1 옵션 B) 재검토 필요.

---

## 참조

- [`docs/design/notes/e25-ha-design.md`](../design/notes/e25-ha-design.md) — E25 설계 원본
- [`docs/design/03-architecture.md`](../design/03-architecture.md) §3.3 분리 모드, §3.10 실패·복구
- [`docs/design/10-audit-and-observability.md`](../design/10-audit-and-observability.md) §10.4 해시 체인
- [`deploy/compose/compose-ha.yml`](../../deploy/compose/compose-ha.yml) — HA 데모 compose
- [`deploy/compose/nginx-ha.conf`](../../deploy/compose/nginx-ha.conf) — LB 설정
- PostgreSQL 공식: [Advisory Locks](https://www.postgresql.org/docs/current/explicit-locking.html#ADVISORY-LOCKS)
- Martin Kleppmann, "How To Do Distributed Locking" — fence token 개념 출처
