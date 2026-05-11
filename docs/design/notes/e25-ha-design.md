# E25 HA 설계 — Leader/Follower (PG advisory lock 기반)

> **상태**: Design draft (Phase 5 carryover, R30-2 결정 대기)
> **작성일**: 2026-05-11
> **범위**: rosshield-server 단일 인스턴스 → 2+ 인스턴스 leader/follower 토폴로지로 확장. audit chain의 single-writer 보장 + 자동 failover.
> **참조**: `§03.3` 분리 모드, `§10.4` 해시 체인, `§10.9` 다중 노드 시 순번, `phase5-backlog.md` carryover E25.
> **비목표**: PG 자체 HA(별도 streaming replication 가정). geo-replication. Active-Active. SQLite 환경의 HA(설계상 불가).
> **코드 변경**: 0건. 본 문서는 docs only — 실제 구현은 R30-2 결정 후 별도 PR.

---

## 1. 목적·배경

### 왜 HA가 필요한가

현재 rosshield-server는 **단일 프로세스 + 단일 인스턴스**로만 운영됩니다. 데스크톱·소규모 온프렘에서는 충분하지만, 다음 시나리오가 발생하면 단일 인스턴스 모델이 깨집니다:

- 24/7 SOC 운영(감사 체인 INSERT가 끊기면 안 됨)
- OS 패치·업그레이드 중 무중단 요구 (rolling update)
- 하드웨어 장애 시 RTO가 분 단위(수동 복구 X)
- enterprise customer SLA — 99.9% uptime 약속 불가능

Phase 4까지는 단일 인스턴스로 진행했고(R30-2 deferred), Phase 5 carryover로 E25를 묶어 처리합니다.

### 핵심 제약: audit chain의 single-writer 보장

`§10.4` 해시 체인은 다음 순서로 INSERT 됩니다:

```
hash_i = sha256(hash_{i-1} || entry_i.payloadDigest || meta_i)
```

두 인스턴스가 **동시에** 같은 tenant의 chain head를 읽고 entry를 append하면, 둘 중 하나는 **stale prevHash**로 INSERT 합니다. UNIQUE `(tenantId, seq)` 제약으로 한쪽은 실패하지만, 그 사이에 발행된 도메인 이벤트(`AuditEntryAppended`)가 외부에 송출되면 외부 검증자는 chain mismatch를 보게 됩니다.

따라서 HA 설계의 가장 어려운 제약은:

**"여러 인스턴스가 떠 있어도, 임의 시점에 audit chain INSERT를 실행할 수 있는 인스턴스는 정확히 1개여야 한다."**

`§10.9`는 옵션 A(단일 writer)와 옵션 B(분산 시퀀스)를 제시했고 초기 출시는 옵션 B로 결정됐지만, 옵션 B는 `seq` 충돌만 방지할 뿐 **prevHash race**는 막지 못합니다. E25는 사실상 옵션 A를 강제하는 메커니즘입니다.

---

## 2. R30-2 결정 후보 비교

| 후보 | 장점 | 단점 | 추가 dep | 평가 |
|---|---|---|---|---|
| **PG advisory lock** | 이미 PG dep 있음, 운영 단순, audit single-writer 자연스러움 | sqlite 운영 시 single-instance 강제 | 0 | **권장** |
| **etcd lease** | DCS 표준, 장애 감지 빠름(저ms), TTL 정밀 | etcd 클러스터 운영 부담, customer 환경 강제 | etcd 3.x | 보류 |
| **Raft library (hashicorp/raft)** | self-contained, 외부 의존 0, 분산 commit log 통합 가능 | 복잡도 폭증, audit chain 자체를 raft state machine으로 강제 시 큰 변경 | hashicorp/raft | 보류 |

### 후보 1: PG advisory lock

PostgreSQL은 `pg_try_advisory_lock(bigint)` / `pg_advisory_unlock(bigint)`로 **세션 수명 기반 mutex**를 제공합니다. 세션이 끊어지면(crash 포함) 락이 자동 해제됩니다. 이미 storage layer가 PG를 dep으로 가지므로 **새로운 인프라 0개**가 결정적 장점입니다. 단점은 sqlite 운영 환경에서는 동일 메커니즘이 없으므로 single-instance를 강제해야 한다는 점이지만, sqlite는 본래 데스크톱·소규모 온프렘 타깃이므로 현실적 제약 없음.

### 후보 2: etcd lease

Kubernetes leader-election의 표준 메커니즘입니다. lease TTL(예: 10s)을 두고 leader가 주기적으로 갱신, 갱신 실패 시 다른 노드가 가져갑니다. 장점은 장애 감지 latency가 ms 단위로 빠르고 watch API로 push 기반 알림이 가능합니다. 단점은 customer가 etcd 클러스터를 별도로 운영해야 하며, etcd 자체가 분산 시스템이라 운영 복잡도가 PG 대비 크게 증가합니다. PG가 이미 있는 환경에서 etcd를 추가로 요구하는 것은 P10(에어갭 1급)·운영 단순성 측면에서 마이너스.

### 후보 3: Raft library

`hashicorp/raft` 같은 in-process Raft 구현을 사용하면 외부 의존 없이 leader-election + replicated log를 구현할 수 있습니다. 장점은 audit chain 자체를 raft FSM(Finite State Machine)에 올리면 chain replication이 무료로 따라옵니다. 단점은 (a) 모든 audit INSERT가 quorum commit을 거쳐야 해서 latency가 늘고, (b) raft snapshot/log compaction 구현이 audit 영구 보관 정책과 충돌하며, (c) 코드베이스 변경이 매우 큽니다. Phase 5 4일 추정에 들어가지 않습니다.

---

## 3. 권고: PG advisory lock + leader/follower

### 결정 이유

1. **외부 의존 0** — 이미 PG가 storage 옵션이므로 새 인프라 추가 없음. 어플라이언스·온프렘 deployment guide가 단순.
2. **audit single-writer가 자연스러움** — leader만 chain INSERT 코드 경로 진입 가능. follower는 read-only API 서빙.
3. **PG 세션 수명 기반 자동 release** — leader crash 시 PG가 세션 종료를 감지해 락 자동 해제. zookeeper/etcd lease TTL을 직접 관리할 필요 없음.
4. **R30-2 후보 중 코드 변경량이 가장 작음** — 4일 추정에 들어맞음.

### sqlite 환경

sqlite는 advisory lock 동등 기능이 없습니다. 또한 sqlite + 다중 인스턴스는 file lock 측면에서도 권장되지 않습니다(`§E1` storage deepdive 참조). 따라서:

- **`storage=sqlite` + `--ha-enabled=true` 조합은 부팅 거부** (또는 warning만 — R30-2 결정 항목 3 참조)
- sqlite 사용자는 단일 인스턴스 운영 강제. HA가 필요하면 PG로 마이그레이션 권고.

---

## 4. 아키텍처 설계

### 4.1 leader-election 메커니즘

```
┌─────────────────┐           ┌─────────────────┐
│  Instance A     │           │  Instance B     │
│  ┌───────────┐  │           │  ┌───────────┐  │
│  │ HA Manager│──┼──heartbeat┼──│ HA Manager│  │
│  └─────┬─────┘  │   (5s)    │  └─────┬─────┘  │
└────────┼───────┘            └────────┼─────────┘
         │                              │
         │  pg_try_advisory_lock(12345) │
         ▼                              ▼
    ┌────────────────────────────────────────┐
    │  PostgreSQL (advisory lock space)      │
    │   lock 12345 → owned by Instance A     │
    └────────────────────────────────────────┘
```

- **5초 주기 heartbeat**로 `pg_try_advisory_lock(<lock_id>)` 호출.
- 락 획득 성공 = **leader**, 실패 = **follower**.
- 한 번 leader가 된 인스턴스는 같은 PG 세션을 유지. 세션이 살아있는 동안 락 보유.
- leader 인스턴스가 crash·network 단절 → PG가 세션 종료 감지 → 락 자동 해제 → 다음 heartbeat 주기에 follower 중 하나가 leader 승격.

### 4.2 leader/follower 책임 차이

| 기능 | leader | follower |
|---|---|---|
| audit chain INSERT | ✅ | ❌ (503) |
| 일반 write API (robot.create 등) | ✅ | ❌ (503 + Retry-After 또는 redirect) |
| read API (GET /robots) | ✅ | ✅ |
| WebSocket 진행 스트림 | ✅ (송신) | ❌ (연결은 받지만 redirect) |
| Scheduler (cron jobs) | ✅ | ❌ (대기) |
| Pack 설치 (서명 검증·DB INSERT) | ✅ | ❌ |
| /healthz | ✅ (`role: leader`) | ✅ (`role: follower`) |
| /metrics | ✅ | ✅ |

### 4.3 fence token (split-brain 방지)

PG advisory lock은 강력하지만, **leader가 GC pause·OS swap·network partition으로 잠깐 멈춘 사이** 다른 인스턴스가 leader 승격할 수 있습니다. 원래 leader가 깨어나면 자기가 여전히 leader라고 믿고 INSERT를 시도합니다.

방어 메커니즘:

```
LeaderEpoch {
  epoch: int64  // PG sequence로 발급, leader 승격 시마다 +1
  leaderId: string  // hostname:pid
  acquiredAt: timestamp
}
```

- leader 승격 시 PG `nextval('leader_epoch_seq')`로 epoch 발급.
- 모든 audit INSERT 트랜잭션 시작 시 `SELECT epoch FROM leader_epoch WHERE current = true` 확인 → 자기 epoch와 다르면 **즉시 abort + leader 자격 박탈**.
- audit chain meta에 `leaderEpoch` 컬럼 추가 (검증 시 단조 증가 확인).

이로써 GC pause 후 깨어난 stale leader의 write가 차단됩니다.

### 4.4 follower → leader 승격 시퀀스

```
T0:    Instance A = leader (epoch=42), Instance B = follower
T0+5s: A crash (process kill)
T0+5s: PG 세션 timeout (TCP keepalive 또는 idle timeout)
T0+10s: B의 heartbeat에서 pg_try_advisory_lock(12345) 성공
T0+10s: B = nextval(leader_epoch_seq) → epoch=43
T0+10s: B = leader, /healthz role 변경, scheduler 시작
```

- 최악 failover 시간 = **PG 세션 timeout + heartbeat 주기 = ~10~15s** (TCP idle timeout 5s + heartbeat 5s + buffer).
- TCP keepalive를 짧게 설정(`tcp_keepalives_idle=5`)하면 더 빨라지지만 PG 부하 증가. 5s/5s/5s = 약 15s에 합의.

---

## 5. API/도메인 영향

### 5.1 `/healthz` 응답 확장

```jsonc
// 현재
{ "ok": true, "value": { "status": "healthy" } }

// HA 활성 시
{
  "ok": true,
  "value": {
    "status": "healthy",
    "ha": {
      "enabled": true,
      "role": "leader",            // | "follower"
      "leaderEpoch": 43,
      "leaderId": "host-a:1234",
      "lastHeartbeatAt": "2026-05-11T03:14:15Z"
    }
  }
}
```

### 5.2 follower의 write 요청 처리

두 옵션:

- **옵션 1: 503 + Retry-After** — follower가 직접 `503 Service Unavailable` + `Retry-After: 5` 헤더 + body `{ "ok": false, "error": { "code": "NOT_LEADER", "leaderId": "host-a:1234" } }`. 클라이언트가 재시도 책임.
- **옵션 2: leader URL로 307 redirect** — follower가 `307 Temporary Redirect` + `Location: https://leader-host/...` 송출. 클라이언트가 자동 follow. 단, leader URL을 follower가 알아야 하므로 advertised address 설정 필요.

권장: **두 옵션 모두 지원**, 기본값은 503. enterprise customer가 LB 뒤에 두면 LB의 health check가 follower를 자동 제외하므로 redirect 불필요.

### 5.3 WebSocket (`/api/v1/scans/progress`)

진행 중 스캔 이벤트는 leader의 in-process scheduler가 발행합니다. follower는 scheduler가 비활성이므로 송신할 이벤트 없음. follower로 들어온 WS 연결은:

- 옵션 A: `1011 internal error` + close
- 옵션 B: leader로 redirect (HTTP upgrade 단계에서 가능)

권장: **옵션 B** — UX 마찰 최소화. LB가 sticky session을 안 잡아도 동작.

### 5.4 스케줄러 (`internal/platform/scheduler/`)

현재 `scheduler.go`의 cron-like job runner는 단일 프로세스에서만 안전합니다. HA 활성 시:

```go
// 의사코드
func (s *Scheduler) Start(ctx context.Context, ha *HAManager) {
    ha.OnLeaderAcquired(func() { s.startCronLoop(ctx) })
    ha.OnLeaderLost(func() { s.stopCronLoop() })
}
```

- leader 승격 시 cron loop 시작
- leader 자격 상실 시 즉시 cron loop 정지 (in-flight job은 best-effort 완료 또는 cancel)

### 5.5 audit chain INSERT 게이트

`audit` 도메인 서비스의 모든 append 코드 경로에 leader 체크 추가:

```go
func (s *AuditService) Append(ctx context.Context, entry AuditEntry) error {
    if !s.ha.IsLeader() {
        return ErrNotLeader  // 도메인 레벨 sentinel
    }
    epoch := s.ha.CurrentEpoch()
    return s.repo.AppendWithEpoch(ctx, entry, epoch)
}
```

repository는 `WHERE current_epoch = $1` 조건으로 INSERT, mismatch면 RETURNING 0 → ErrEpochStale 반환.

---

## 6. TDD 태스크 분해

| ID | 테스트 | 구현 |
|---|---|---|
| **E25.T1** | `TestLeaderElectionRoundTrip` — 두 PG 세션이 동시에 `pg_try_advisory_lock` 시도 → 정확히 1개 성공, 1개 실패 | `internal/platform/ha/pg_lock.go` — `Acquire()`, `Release()`, `IsHeld()` |
| **E25.T2** | `TestLeaderCrashTriggersFailover` — leader 세션 강제 종료 → 5~15s 내 follower가 leader 승격 (epoch +1 확인) | `HAManager` heartbeat loop + epoch 발급 (`nextval('leader_epoch_seq')`) |
| **E25.T3** | `TestFollowerWriteReturns503` — follower 인스턴스에 `POST /api/v1/robots` → 503 + `NOT_LEADER` 응답 | API middleware `requireLeader` + 모든 write route에 적용 |
| **E25.T4** | `TestAuditChainBlocksConcurrentInsert` — leader가 epoch=N으로 INSERT 진행 중, stale leader가 epoch=N-1로 INSERT 시도 → 차단 (`ErrEpochStale`) | audit repo `AppendWithEpoch` + `leader_epoch` 테이블 |
| **E25.T5** | `TestSqliteRefusesHaEnabled` — `--storage=sqlite --ha-enabled=true` → 부팅 실패 (또는 warning, R30-2 결정에 따라) | bootstrap 검증 로직 |

**테스트 인프라**:
- T1·T2·T4: testcontainers-go로 실제 PG 띄우기. mock으로는 advisory lock 동작 재현 어려움.
- T3·T5: 단위 테스트 + httptest.

---

## 7. 운영 모드

### 7.1 docker-compose 예시

```yaml
services:
  postgres:
    image: postgres:16
    environment:
      POSTGRES_DB: rosshield
      POSTGRES_USER: rosshield
      POSTGRES_PASSWORD: <secret>
    volumes:
      - pgdata:/var/lib/postgresql/data

  rosshield-a:
    image: rosshield-server:0.3.0
    command: >
      --storage=postgres
      --pg-dsn=postgres://rosshield:<secret>@postgres/rosshield
      --ha-enabled
      --ha-lock-id=12345
      --advertised-addr=https://rosshield-a:8443
    depends_on: [postgres]

  rosshield-b:
    image: rosshield-server:0.3.0
    command: >
      --storage=postgres
      --pg-dsn=postgres://rosshield:<secret>@postgres/rosshield
      --ha-enabled
      --ha-lock-id=12345
      --advertised-addr=https://rosshield-b:8443
    depends_on: [postgres]

  nginx:
    image: nginx:1.27
    volumes:
      - ./nginx-ha.conf:/etc/nginx/nginx.conf:ro
    ports: ["8443:8443"]
    depends_on: [rosshield-a, rosshield-b]
```

`nginx-ha.conf` 핵심:

```nginx
upstream rosshield_backend {
    server rosshield-a:8443 max_fails=2 fail_timeout=10s;
    server rosshield-b:8443 max_fails=2 fail_timeout=10s backup;
}

server {
    listen 8443 ssl;
    location /healthz {
        # HEAD only — full healthz는 role 노출
        proxy_pass http://rosshield_backend;
    }
    location / {
        proxy_pass http://rosshield_backend;
        proxy_next_upstream error timeout http_503;
    }
}
```

LB는 follower의 503 응답을 보고 다음 upstream(leader)으로 자동 retry. customer는 인스턴스 추가/제거를 nginx config 갱신으로 해결.

### 7.2 CLI 플래그

| 플래그 | 기본값 | 설명 |
|---|---|---|
| `--ha-enabled` | `false` | HA 모드 활성 |
| `--ha-lock-id` | `12345` | PG advisory lock ID (cluster당 고정값) |
| `--ha-heartbeat-interval` | `5s` | leader heartbeat 주기 |
| `--ha-pg-keepalive` | `5s` | PG TCP keepalive (failover 시간 영향) |
| `--advertised-addr` | (없음) | 다른 인스턴스가 redirect 시 사용할 URL |

### 7.3 모니터링

추가 Prometheus 메트릭:

| 이름 | 타입 | 설명 |
|---|---|---|
| `rosshield_ha_role` | gauge | 0=follower, 1=leader |
| `rosshield_ha_leader_epoch` | gauge | 현재 epoch |
| `rosshield_ha_failover_total` | counter | 누적 failover 횟수 |
| `rosshield_ha_heartbeat_failures_total` | counter | PG heartbeat 실패 |

알림: `rosshield_ha_failover_total` 1시간 내 3회 이상 → 운영자 호출 (flapping 의심).

---

## 8. 리스크와 완화

| 리스크 | 영향 | 완화 |
|---|---|---|
| PG 자체가 SPOF | 전체 cluster 다운 | PG는 별도 streaming replication(primary + standby) 가정. customer guide에 명시. PG HA는 본 epic 비목표 |
| leader GC pause 중 stale write | audit chain 손상 | fence token (leader epoch) — §4.3 |
| advisory lock의 lock_id 충돌 | 다른 앱이 같은 ID 쓰면 deadlock | lock_id를 `rosshield_` prefix 해시(예: `0x726f7373`)로 설정, 충돌 가능성 무시 가능 |
| sqlite 사용자가 HA 활성화 시도 | 데이터 손상 위험 | `--storage=sqlite + --ha-enabled` 조합 부팅 거부 (R30-2 결정 항목 3) |
| network partition으로 양쪽 leader | split-brain | PG advisory lock 자체가 single-source-of-truth → PG에 도달 못 하는 인스턴스는 자동으로 follower로 강등 (heartbeat 실패) |
| PG 연결 풀 고갈로 heartbeat 실패 | 잘못된 failover | 전용 connection pool (`maxConns=2`) 분리. write traffic과 격리 |
| failover 직후 in-flight WebSocket 연결 끊김 | UX 마찰 | 클라이언트 재연결 로직 (이미 §09 web UI 가이드에 있음). 추가 로직 불필요 |
| 인스턴스 시계 어긋남으로 epoch 검증 오류 | 거짓 음성 | epoch는 시간 기반 X, PG sequence 기반. 시계 무관 |

---

## 9. 추정 분해

총 **4일** (1인 작업 기준):

| 일차 | 작업 | 산출 |
|---|---|---|
| **Day 1** | E25.T1 + E25.T2 — leader-election + failover | `internal/platform/ha/pg_lock.go`, `manager.go`, testcontainers 통합 테스트 |
| **Day 2** | E25.T3 + E25.T5 — API middleware + sqlite 거부 | `requireLeader` middleware, bootstrap 검증, 단위 테스트 |
| **Day 3** | E25.T4 — audit chain epoch 게이트 | `leader_epoch` 마이그레이션, `AppendWithEpoch` repo, 동시성 테스트 |
| **Day 4** | 통합 테스트 + docker-compose 데모 + 문서 갱신 | `compose-ha.yml`, `nginx-ha.conf`, 운영 가이드 1 페이지, `phase5-backlog.md` E25 체크박스 마감 |

병렬 작업 가능 항목 없음 (T2가 T1 의존, T4가 T2 의존). 1인 직렬 진행 가정.

---

## 10. 결정 요청 항목 (R30-2 + 부속)

사용자 합의 후 본 문서 §3을 "결정 완료"로 갱신하고 구현 PR 진행.

### R30-2: HA leader-election 메커니즘

- **A) PG advisory lock** (본 문서 권고)
- B) etcd lease
- C) Raft library

권고 근거: §3 참조. enterprise customer가 etcd를 이미 운영 중인 경우는 드물고, 추가 인프라 요구는 onprem 채택 마찰을 키움.

### R30-2-부속1: lock_id 정책

- **A) 고정값 (`12345` 또는 `0x726f7373`)** — 단순. 한 PG 인스턴스에 rosshield cluster 1개 가정.
- B) tenant별 lock_id — 테넌트별로 leader가 다를 수 있음. audit chain은 테넌트별로 독립이므로 이론적으로 가능. 단, 운영 복잡도 ↑↑.
- C) cluster name 해시 (`hash("prod-cluster-01")`) — 한 PG 인스턴스에 여러 rosshield cluster 운영 가능.

권고: **A** (Phase 5 단순성). C는 multi-cluster 요구가 실제로 발생하면 그때 추가.

### R30-2-부속2: HA 활성 시 sqlite 운영 거부 방식

- **A) 부팅 실패** — `--storage=sqlite --ha-enabled` 조합이면 즉시 exit + 명시적 에러 메시지.
- B) Warning만 출력 + single-instance 강제 동작 — 사용자 실수에 관대.

권고: **A** (조용한 fallback 금지 — `§01` P11 설명 가능성). 사용자가 의도하지 않은 환경에서 chain 손상이 발생하는 것보다 부팅 실패가 안전.

---

## 11. 참조

- `docs/design/03-architecture.md` §3.3 분리 모드, §3.10 실패·복구
- `docs/design/10-audit-and-observability.md` §10.4 해시 체인, §10.9 다중 노드 시 순번
- `docs/design/notes/e1-storage-deepdive.md` PG·sqlite 드라이버 결정 배경
- `docs/design/phase5-backlog.md` Phase 4 carryover 표 (E25 정의)
- PostgreSQL 공식 문서: [Advisory Locks](https://www.postgresql.org/docs/current/explicit-locking.html#ADVISORY-LOCKS)
- "How To Do Distributed Locking" — Martin Kleppmann (fence token 개념 출처)
