# Phase 9 — 자동 failover 자동화 옵션 research

> **상태**: Research only (docs only) — 코드 변경 0건. D-AF-1 ~ D-AF-4 결정 대기.
> **작성일**: 2026-05-20
> **범위**: Phase 8에서 도입한 cross-region replication + manual cutover(Route53 자동 + application manual)을 **end-to-end 자동 failover**로 진화시키는 옵션 비교 + 권장 진입 경로.
> **선행**: `notes/multi-region-ha-design.md` §5 Stage 6 (Phase 9+ 영역) + `notes/multi-region-ha-stage4-design.md` D-MR-3 = 수동 (split-brain 회피).
> **비목표**: 본 round에서 구현 진입 0. Patroni/Stolon 운영 가이드는 D-AF-1 결정 후 별 epic. Lodestar 자체 leader-election(E25 PG advisory lock)을 cross-region으로 확장하는 D-AF-4 옵션도 별 epic.
> **코드 변경**: 0건. 본 문서는 docs only — D-AF-1~4 결정 후 별 PR(Stage 6.1~6.x).

---

## 1. 상태 / 배경

### 1.1 v0.7.9 직후 현재 상태

Phase 8 Multi-region HA 사실상 마감 (v0.7.4 ~ v0.7.9 6 release):
- ✅ PG logical replication (Stage 1~3 + 후속)
- ✅ Route53 + Cloudflare DNS routing (Stage 4)
- ✅ ops runbook (Stage 5) — 운영자 manual 절차
- ✅ testcontainers e2e (Stage 7 6/8 + application integration)
- ✅ Prometheus monitoring + HA leader-only gate

**현재 RTO**: ~5분 = Route53 자동 90초 + 운영자 promote 2분 + restart 60초

**한계**: application promote은 **사람 손**이 필요. D-MR-3에서 manual로 결정한 이유:
- false positive 시 split-brain 위험 (network partition vs region 장애 구분 어려움)
- PG promote는 immediate + 되돌리기 비용 큼 (base backup 재시작)
- 운영자 판단이 자동 알고리즘보다 정확한 시나리오 다수

### 1.2 자동 failover 도입 의의

enterprise customer 일부가 RTO ≤ 1분 또는 ≤ 30초 요구 — manual 절차로는 도달 불가:
- 금융·의료 SLA 99.99% (연 52분 downtime)
- 24/7 무인 운영 (운영자 알람 응답 시간 큼)
- multi-cloud DR 자동화

**Phase 9 목표**:
- RTO ≤ 1분 (Patroni/Stolon 표준 동작 범위)
- split-brain 방어는 fence token + STONITH 또는 quorum lease로 유지
- Lodestar의 E25 leader-election(PG advisory lock)과 충돌 없는 통합

---

## 2. 위협 모델 / 요구사항

### 2.1 자동 failover 도입 시 신규 위협

| 위협 | 발생 | 영향 | 대응 |
|---|---|---|---|
| network partition false positive | 분기 1~3회 | quorum 부재 standby 잘못 promote → split-brain | quorum lease (≥3 노드 또는 etcd 분리) |
| auto failover loop | flaky region 시 | primary/standby 반복 swap, audit chain 손상 | back-off + minimum lease 시간 (예: 5분) |
| Patroni/etcd 자체 장애 | 분기 0~2회 | failover 자체 불가, primary 살아있는 한 OK | etcd 외부화 (Kubernetes etcd 별 deploy) |
| Lodestar E25 leader-election과 충돌 | 통합 시 | 두 leader-election layer가 다른 leader 선택 → application/DB split-brain | E25를 Patroni의 leader 감지로 위임 (D-AF-2) |
| Lodestar fence token과 STONITH 충돌 | 통합 시 | application이 epoch ↑인데 DB는 stale primary | STONITH가 stale primary를 강제 stop (PG process kill) |

### 2.2 요구사항

| ID | 요구 | 측정 |
|---|---|---|
| **RTO** | 자동 promote 완료 ≤ 60초 | Patroni metric 또는 e2e 실측 |
| **R1** | network partition 시 quorum 부재면 promote 안 함 | DCS quorum (≥2/3) 강제 |
| **R2** | E25 leader-election과 단일 source of truth 유지 | D-AF-2 결정 |
| **R3** | application fence token이 DB primary swap 즉시 반영 | callback hook 또는 polling |
| **R4** | air-gap customer는 자동 failover 비활성 옵션 유지 | feature flag |
| **R5** | 운영자가 자동 cutover 직후 1분 안에 알림 수신 + override 가능 | PagerDuty hook + manual reset |

---

## 3. 옵션 비교 (≥4)

### 3.1 매트릭스

| 옵션 | DCS | leader-election | STONITH | RTO | 운영 부담 | 강점 | 약점 |
|---|---|---|---|---|---|---|---|
| **A) Patroni + etcd** | etcd 별 cluster | Patroni 자체 (Raft) | watchdog (옵션) | ~30초 | 높음 (etcd ops + Patroni config) | 가장 성숙 + Kubernetes 친화 | etcd cluster 운영 부담 |
| B) Stolon + etcd/Consul | etcd 또는 Consul | Stolon proxy + sentinel | 자체 hook | ~30초 | 중-높 (proxy 추가 layer) | proxy 기반 transparent failover | proxy 추가 hop latency |
| C) PG built-in synchronous + pgwatch | pgwatch 또는 자체 | 별 manual orchestrator | 없음 | ~수분 (수동 자동화) | 중 (PG 표준만 사용) | dep 추가 0 + customer 환경 자유 | 자체 orchestrator 구현 필요 |
| **D) Lodestar E25 확장** (PG advisory + cross-region heartbeat) | 자체 PG advisory lock | E25 Manager 확장 | 별 hook | ~60~120초 | 낮음 (이미 있음) | dep 추가 0 + Lodestar 통합 자연 | cross-region heartbeat latency 크고 advisory lock primary 의존 |

### 3.2 옵션별 상세

**옵션 A — Patroni + etcd** (권장 default).

[Patroni](https://github.com/patroni/patroni)는 PG HA의 de-facto 표준. Python으로 작성, Postgres process를 wrap. DCS(Distributed Configuration Store)로 etcd / Consul / Kubernetes / ZooKeeper 지원.

동작:
1. 각 PG 인스턴스에 Patroni daemon 동거 (sidecar 또는 systemd unit)
2. Patroni가 PG를 시작/정지하고 etcd에 leader key 보유
3. Leader는 5초 간격 etcd lease 갱신
4. Standby Patroni는 leader lease 만료 감지 시 promote 시도 — quorum 확인 후 자동 promote
5. application은 `https://patroni:8008/master` REST endpoint로 현재 primary 식별

**강점**:
- 가장 성숙 + 활발한 커뮤니티 (10K+ stars, Cybertec/Zalando 운영)
- Kubernetes Helm chart 잘 정비
- watchdog STONITH로 false positive 방어
- `pg_rewind` 자동 사용 — primary 복구 시 base backup 없이 standby로 재진입

**약점**:
- etcd cluster (3 노드 권장) 별 운영 + 비용
- Python dep
- Lodestar의 E25 leader-election과 중복 — D-AF-2 통합 필요

**옵션 B — Stolon** (Zalando, [github.com/sorintlab/stolon](https://github.com/sorintlab/stolon)).

Stolon은 Go로 작성, sentinel + keeper + proxy 3 컴포넌트:
- **keeper**: PG instance wrap
- **sentinel**: cluster state 관리 (DCS)
- **proxy**: client traffic을 현재 primary로 routing

**강점**:
- Go dep (Patroni Python 대비 build 단순)
- proxy 기반 — application이 endpoint 변경 인지 불필요
- transparent failover

**약점**:
- proxy 추가 hop latency (~1ms)
- Patroni 대비 커뮤니티 작음 (4K stars, 2024년 commit 감소)
- application이 PG primary endpoint를 stable로 가정 — Lodestar의 application-level fence token과 결합 검증 필요

**옵션 C — PG built-in synchronous + 자체 orchestrator**.

PG 자체의 `synchronous_commit = remote_apply` + `synchronous_standby_names`로 sync replication. failover는 별 orchestrator(`repmgr`·`pg_auto_failover`·자체 script).

**강점**:
- dep 추가 0 (PG 표준만)
- customer가 자체 환경에 자유롭게 통합
- air-gap customer 친화

**약점**:
- 자동 failover 자체 구현 또는 별 도구 선택 필요
- standby down 시 primary write 차단 (sync 강제) — 가용성 trade-off
- cross-region latency가 sync 쓰기 latency에 가산

**옵션 D — Lodestar E25 확장** (PG advisory lock + cross-region heartbeat).

E25의 PG advisory lock 기반 leader-election을 cross-region으로 확장:
1. primary region의 PG에 advisory lock 보유한 인스턴스가 leader
2. standby region의 Lodestar 인스턴스가 PG cross-region connection으로 advisory lock heartbeat
3. heartbeat timeout (예: 30초) 후 standby region이 자체 PG에 advisory lock 시도
4. 성공 시 standby region promote + application restart

**강점**:
- dep 추가 0 (E25 + PG 표준만)
- Lodestar 자체 leader-election과 자연 통합
- 운영 단순 (별 etcd cluster 없음)

**약점**:
- cross-region heartbeat가 PG SQL 호출이라 latency 큰 환경에서 false positive risk
- PG primary down 시 advisory lock 자체가 접근 불가 — leader 식별 자체 불가
- Patroni/Stolon 대비 성숙도 낮음 (Lodestar에서 직접 구현 + 검증 필요)

### 3.3 권장

**옵션 A (Patroni + etcd)** — D-AF-1 default. 근거:
- Phase 9 RTO 목표 ≤ 60초 달성 가능
- Kubernetes customer는 Helm chart로 즉시 적용
- enterprise customer 대부분 이미 etcd 사용 중 (Kubernetes 자체 etcd)
- Lodestar는 Patroni가 expose하는 `/master` REST로 leader 식별 → E25를 Patroni 위임

옵션 D는 air-gap customer 또는 dep 추가 거부 customer를 위한 fallback option (별 epic, D-AF-4).

---

## 4. Patroni 통합 설계 (D-AF-1 = A)

### 4.1 아키텍처

```
Region Seoul (primary)                Region Tokyo (standby)
┌────────────────────────────┐        ┌────────────────────────────┐
│ rosshield-server (3 inst.) │        │ rosshield-server (3 inst.) │
│  └─ E25 RoleProvider gate  │        │  └─ E25 RoleProvider gate  │
│  └─ Patroni REST poller    │        │  └─ Patroni REST poller    │
└─────────┬──────────────────┘        └─────────┬──────────────────┘
          │ /master?                            │ /master?
          ▼                                     ▼
┌────────────────────────────┐        ┌────────────────────────────┐
│ Patroni daemon × 3          │       │ Patroni daemon × 3          │
│  └─ PG primary (1) + standby│       │  └─ PG standby × 3          │
└─────────┬──────────────────┘        └─────────┬──────────────────┘
          │                                     │
          └────────────┬────────────────────────┘
                       ▼
              ┌─────────────────┐
              │ etcd cluster ×5 │ (cross-region quorum)
              └─────────────────┘
```

### 4.2 Patroni → Lodestar leader gate 위임

E25의 `RoleProvider` interface가 PG advisory lock 대신 Patroni REST `/master?` polling으로 결과 회신:

```go
// internal/platform/ha/patroni/role_provider.go (신규 carryover)
type RoleProvider struct {
    patroniURL string  // http://patroni:8008
    pollMs     int     // 1000ms default
    leader     atomic.Bool
    epoch      atomic.Int64
}

func (rp *RoleProvider) IsLeader() bool       { return rp.leader.Load() }
func (rp *RoleProvider) CurrentEpoch() int64  { return rp.epoch.Load() }

func (rp *RoleProvider) run(ctx context.Context) {
    ticker := time.NewTicker(time.Duration(rp.pollMs) * time.Millisecond)
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            // GET /master 200 OK면 leader, 503이면 follower
            resp, _ := http.Get(rp.patroniURL + "/master")
            rp.leader.Store(resp.StatusCode == 200)
            // epoch는 Patroni의 promotion counter 또는 /cluster의 leader.elected_lsn 사용
        }
    }
}
```

E25 PG advisory lock + ha.Manager는 그대로 두고, RoleProvider 구현만 Patroni로 swap.

### 4.3 fence token (epoch) 매핑

Patroni의 `/cluster` API에서 leader 변경 시 `leader.elected_lsn` 또는 `timeline` 증가:

```json
{
  "members": [...],
  "leader": "patroni-seoul-1",
  "timeline": 42,
  "scheduled_switchover": null
}
```

Lodestar의 `leader_epoch`는 Patroni `timeline`을 그대로 사용:
- audit_entries.leader_epoch = Patroni timeline (PG에서 자동 변경)
- application의 RoleProvider.CurrentEpoch()도 timeline polling 결과 반환

### 4.4 운영자 override (R5)

Patroni는 manual control 지원:
- `patronictl failover` — 운영자 수동 promote
- `patronictl pause` — 자동 failover 일시 정지

운영자가 Patroni를 잠시 비활성하고 Lodestar 자체 RoleProvider(E25)로 fallback 가능 — `--ha-rp=patroni|e25` config flag.

---

## 5. Stage 분해 (Phase 9 진입 시)

| Stage | 작업 | 추정 |
|---|---|---|
| **9.1** | 본 design 결정 (D-AF-1~4) | 본 round |
| **9.2** | Patroni Helm chart sample + ops doc | 3일 |
| **9.3** | `internal/platform/ha/patroni/` RoleProvider 구현 + 단위 test | 5일 |
| **9.4** | bootstrap `--ha-rp=patroni` flag + E25 gate swap 결선 | 2일 |
| **9.5** | testcontainers e2e (Patroni 3-node + etcd 3-node) | 5일 |
| **9.6** | Stage 5b runbook 갱신 (자동 cutover 시나리오 추가) | 2일 |
| **9.7** | customer drill (staging env 실측) | (외부 트랙) |

Stage 9.2~9.6 = ~17일 (3주 단독). customer 외부 검증 (9.7)은 별 트랙.

---

## 6. 결정 항목 (D-AF-1 ~ D-AF-4)

### D-AF-1: 자동 failover 도구

- **A) Patroni** (권장 default) — Python + etcd, 가장 성숙
- B) Stolon — Go + proxy, transparent
- C) PG built-in + 자체 orchestrator — dep 추가 0
- D) Lodestar E25 확장 — Lodestar 자체 구현

권장: **A**. enterprise customer 대부분 Kubernetes 운영 + Patroni 표준.

### D-AF-2: E25 leader-election과의 통합

- **A) E25 RoleProvider를 Patroni REST로 swap** (권장 default) — 단일 source of truth
- B) E25 + Patroni 둘 다 운영 + 일치성 검증 — 복잡도 큼
- C) E25 deprecate — Patroni 전용 — 작은 customer (single PG)에서는 E25 필요

권장: **A**. 단일 RoleProvider interface가 PG advisory(E25) 또는 Patroni(P9) 둘 다 만족.

### D-AF-3: fence token (leader_epoch) source

- **A) Patroni timeline** (권장 default) — Patroni 자체 increment, customer 별 설정 0
- B) PG sequence (E25 leader_epoch_seq 그대로 사용) — Patroni timeline과 별 매핑
- C) etcd lease counter — Patroni 우회

권장: **A**. Patroni timeline은 PG promote 시 자동 증가 + replication에 자동 포함.

### D-AF-4: air-gap customer fallback

- **A) `--ha-rp=e25` flag로 E25 fallback** (권장 default) — Patroni 없이 single PG + E25만
- B) Lodestar 자체 cross-region heartbeat (옵션 D 확장) — 별 epic
- C) air-gap customer는 manual failover만 — Phase 8 그대로

권장: **A**. single-tenant air-gap은 single PG + E25로 충분 (cross-region 없음).

---

## 7. 위험 / open issues

### 7.1 etcd cluster 운영 부담

3~5 노드 etcd cluster 추가 운영 — Kubernetes etcd 재사용 권장. 별 etcd 운영은 Stage 9.2 ops doc에서 권고.

### 7.2 Patroni Python dep

binary 1개 추가 (Patroni 자체 Python virtualenv 또는 Docker image 사용). customer가 Python 거부하면 옵션 D fallback.

### 7.3 cross-region etcd latency

etcd quorum write는 majority 노드 동의 — cross-region latency 큼. 권장: region-local etcd cluster + Patroni가 region별 quorum 처리. cross-region은 watch만 (read-only).

### 7.4 split-brain edge case

network partition 시 minority quorum이 잘못된 promote 시도. watchdog STONITH로 자체 PG process kill — Linux kernel softdog 권장.

### 7.5 Lodestar e2e 회귀 risk

Patroni 통합 시 기존 E25 unit test가 Patroni mock 없으면 fail 가능 — `--ha-rp=e25` flag default로 기존 behavior 유지 + Patroni는 opt-in 후 점진 진입.

---

## 8. 결론

- **Patroni + etcd**가 Phase 9 자동 failover의 권장 진입 경로 (D-AF-1 = A).
- E25 RoleProvider를 Patroni REST polling으로 swap하여 단일 source of truth 유지 (D-AF-2 = A).
- `leader_epoch`는 Patroni `timeline` 그대로 사용 (D-AF-3 = A).
- air-gap customer는 `--ha-rp=e25` fallback으로 기존 E25 행동 유지 (D-AF-4 = A).
- Stage 9.2~9.6 = ~3주 추정 (구현 + Helm + e2e).
- 본 round는 docs only — D-AF-1~4 사용자 확정 후 Stage 9 진입.

---

## 9. 참조

- design [`multi-region-ha-design.md`](multi-region-ha-design.md) — Phase 8 epic 전체
- design [`multi-region-ha-stage4-design.md`](multi-region-ha-stage4-design.md) — DNS routing
- design [`e25-ha-design.md`](e25-ha-design.md) — single-region leader-election
- Patroni: https://github.com/patroni/patroni
- Stolon: https://github.com/sorintlab/stolon
- pg_auto_failover: https://github.com/citusdata/pg_auto_failover
