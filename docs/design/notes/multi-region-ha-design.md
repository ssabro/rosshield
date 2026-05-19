# Multi-region HA 설계 — Cross-region replication + failover routing

> **상태**: Design draft (Phase 8 carryover, D-MR-1~5 결정 대기)
> **작성일**: 2026-05-19
> **범위**: 현재 single-region HA(E25)에서 **cross-region replication + failover routing**으로 확장. region 전체 장애(예: AWS ap-northeast-2 동시 다운) 시 다른 region(ap-northeast-1·us-west-2)으로 service 연속.
> **참조**: `notes/e25-ha-design.md` (single-region HA), `11-tech-stack-and-roadmap.md` Phase 5 carryover, `internal/platform/ha/` (Manager + PGLock + RoleProvider + ErrNotLeader), `internal/platform/storage/postgres/pg.go` (단일 region pgxpool).
> **비목표**: active-active multi-master 라우팅(옵션 C에 한정 평가만). PG cross-region streaming replication 자체 설정 절차(별도 ops doc). DR drill 자동화(별도 epic E-DR).
> **코드 변경**: 0건. 본 문서는 docs only — 구현은 D-MR-1~5 결정 후 별도 PR(Stage 1~7).

---

## 1. 상태 / 배경

### 1.1 현재 single-region HA

E25(`notes/e25-ha-design.md`)에서 결정된 single-region HA 구조는:

- 동일 region(예: ap-northeast-2 = Seoul) 안에서 leader/follower 2+ 인스턴스
- PG advisory lock(`pg_try_advisory_lock`)으로 단일 leader 보장
- fence token(leader epoch)으로 split-brain 방어
- failover 시간 = PG 세션 timeout + heartbeat 주기 = ~10~15s
- PG는 region 내부 streaming replication 가정(primary + standby 같은 region)

이는 **단일 region 내** 인스턴스 장애·하드웨어 장애·rolling update에는 충분합니다. 그러나 region 자체가 통째로 다운되면(power outage · cable cut · cloud provider regional incident) 모든 인스턴스 + PG가 동시에 사라집니다 → service 0.

### 1.2 region 장애 실제 사례

- AWS ap-northeast-1 (Tokyo) 2019-08-23 power loss → 10시간 partial outage
- AWS us-east-1 2021-12-07 control plane outage → 7시간 영향
- GCP asia-northeast1 (Tokyo) 2019-07-02 cooling failure → 부분 영향
- Cloudflare 2024-11-14 control plane outage → 글로벌 30분

enterprise customer는 SLA 99.9% 이상을 요구합니다. 99.9% = 연 8.76시간 downtime 허용 — region outage 한 번이면 SLA 위반. 99.99% = 연 52.6분이면 region 장애 자체가 즉시 위반.

### 1.3 enterprise customer 요구 시나리오

한국·일본·미국 multi-region 운영 enterprise customer 진입(Phase 7~8 가정):

- **RPO** (Recovery Point Objective) ≤ 1분 — region 장애 직전 1분 이내 데이터까지 보존
- **RTO** (Recovery Time Objective) ≤ 5분 — region 장애 감지 후 5분 안에 standby region에서 service 재개
- 한국 region 장애 시 일본·미국 region에서 read 계속(audit 검증 가능)
- 한국 region 복구 시 데이터 손실 없이 재진입

본 epic은 이 요구를 만족하는 **cross-region replication + failover routing** 메커니즘을 정의합니다.

---

## 2. 위협 모델 / 요구사항

### 2.1 위협

| 위협 | 발생 | 영향 | 본 epic 대응 |
|---|---|---|---|
| regional zonal 장애 (단일 AZ) | 분기/년 | region 내 일부 AZ만 영향 — E25로 흡수 | 대상 외 |
| **regional 장애** (region 전체) | 연 1~2회 | 모든 인스턴스 + PG 동시 다운 | **본 epic** |
| network partition (region 간 link) | 연 5~10회 | 잘못된 failover → split-brain | fence token + manual promote |
| DB replication lag (sync 미사용) | 상시 | RPO 보장 못함 (logical 한계) | RPO ≤ 1분 목표 + 모니터링 |
| DNS failover latency | TTL 의존 | 사용자 영향 최대 TTL(60s) | Route53 health check + low TTL |
| tenant context cross-region 일관 | 상시 | tenant_id 매핑 깨지면 데이터 노출 | tenant 메타 전체 replicate |
| audit chain cross-region 일관 | 상시 | hash chain head SHA mismatch | single-writer(원래 region) + replicate 후 검증 |

### 2.2 요구사항

| ID | 요구 | 측정 |
|---|---|---|
| **RPO** | region 장애 시 손실 데이터 ≤ 1분 | replication lag 모니터링 |
| **RTO** | region 장애 감지 + standby promote 완료 ≤ 5분 | failover runbook 실측 |
| **R1** | standby region read API 항상 가능 (failover 전에도) | health check `/healthz?role=standby-read` |
| **R2** | audit chain head SHA cross-region 검증 가능 | `report verify --cross-region` |
| **R3** | failover 후 데이터 손실 0 (replication catch-up 완료까지 promote 대기) | promote 절차 강제 |
| **R4** | failover 후 원래 region 복구 시 split-brain 없이 재진입 | manual demote + rebuild 절차 |
| **R5** | tenant 격리 cross-region 유지 | tenant 메타 전체 replicate + 검증 |

---

## 3. 옵션 비교 (≥3)

### 3.1 옵션 매트릭스

| 옵션 | DB replication | failover routing | RPO | RTO | 비용 | 운영 복잡도 | 강점 | 약점 |
|---|---|---|---|---|---|---|---|---|
| **A** | PG logical replication (primary → replica) + 1 region active | DNS routing (Route53/Cloudflare TTL ≤ 60s) | ~1분 (lag) | ~5분 (수동 promote) | 낮음 (기존 PG + DNS) | 중 | 비용 효율 + 표준 + 기존 PG 호환 | RPO 로그 지연 의존 + DNS TTL latency |
| **B** | PG physical (streaming) replication + sync replication | App-level routing (LB + health check) | 0 (sync) | ~5분 (수동 promote) | 중 (sync overhead — write latency +20ms) | 중 | sync = RPO 0 | latency 증가 + replica down 시 primary write 차단 |
| **C** | Multi-master (CockroachDB / Spanner / YugabyteDB) | 자동 라우팅 (multi-master 내장) | 0 | ~1초 (자동) | 매우 높음 (vendor 또는 cluster ops) | 높음 | 진정한 active-active + 자동 | vendor lock-in + 마이그레이션 비용 + PG 호환 깨짐 |
| **D** | event sourcing (audit chain 자체 cross-region replicate) | App-level | ~수 초 (event 송신 지연) | ~분 (replay) | 중 | 높음 | audit chain 본 자산 활용 — replicate 자체가 검증 가능 | 본 영역 외 — Phase 8+ 별 epic 권장 |

### 3.2 옵션별 상세

**옵션 A (PG logical replication + DNS routing)** — 권장 default.
PostgreSQL `CREATE PUBLICATION` + `CREATE SUBSCRIPTION`으로 primary region의 변경을 standby region으로 비동기 복제. standby region은 read-only로 동작(혹은 standby-mode flag). region 장애 시: (1) standby region PG `SELECT pg_promote()` → primary로 승격, (2) Lodestar standby instance가 `--standby-mode=false`로 재시작 + leader-elect 재실행, (3) DNS health check가 primary region down 감지 → standby region IP로 record 업데이트. RPO는 logical replication lag(평소 < 1초, peak 트래픽 ~수 초)에 의존. RTO는 (2) + (3) = ~5분.

**옵션 B (PG physical sync replication + App-level routing)**.
`synchronous_commit = remote_apply` + `synchronous_standby_names`로 primary write가 standby commit까지 대기. RPO = 0(sync 강제). 단 standby region이 down·network partition이면 primary write도 차단(데이터 일관성 우선 vs 가용성 trade-off). cross-region latency가 sync 쓰기 latency에 가산됨(한일 ~30ms, 한미 ~150ms 추가). 일반 write API throughput 절반 이하. enterprise customer 중 audit-critical(금융·의료) 한정으로 권장. App-level routing은 LB(nginx/HAProxy)가 두 region 인스턴스 동시 health check + active region으로 라우팅.

**옵션 C (Multi-master)**.
CockroachDB·Spanner·YugabyteDB는 진정한 active-active multi-master 분산 DB. 모든 region이 read/write 동시 처리. failover 자동, RPO 0, RTO 초 단위. 단점: (1) PG 호환성 깨짐(현재 pgxpool + migrations 재작성 필요), (2) vendor lock-in (Spanner는 GCP 전용, CockroachDB는 enterprise 라이선스), (3) 마이그레이션 비용 거대(Phase 1~7 모든 schema 재검증), (4) on-prem/air-gap 배포 어려움 — P3(에어갭 1급) 위반 가능.

**옵션 D (Event sourcing cross-region replicate)**.
audit chain 자체를 event log로 보고 cross-region replicate. audit가 본 제품 핵심 자산이므로 자연스러움. 단점: (1) audit chain 외 데이터(scan result, robot inventory, evidence blob)도 별도 replication 필요 — 결국 옵션 A/B 병행 필요, (2) event replay 기반 standby state 재구성은 cold start 시간 길다(Phase 8+ catch-up). **본 epic 대상 외 — 별도 epic E-EventSourcing으로 분리**.

### 3.3 권장 default

**옵션 A** (PG logical replication + DNS routing) — D-MR-1.

근거:
1. **비용 효율**: 기존 PG 호환 100%, vendor lock-in 0. 마이그레이션 0건.
2. **운영 단순**: PG DBA 표준 지식으로 운영 가능. air-gap·on-prem 배포 그대로 유지.
3. **요구 만족**: RPO 1분 + RTO 5분 충족(평소 lag < 1초, peak ~수 초).
4. **점진적 업그레이드**: 옵션 B(sync)로 PG 설정 변경만으로 업그레이드 가능 — audit-critical customer 진입 시 적용.
5. **P3·P7 정합성**: 에어갭 + 단일 바이너리 원칙 유지.

옵션 B는 D-MR-1 결정 시 "audit-critical customer 전용 옵션"으로 docs에 명시. 옵션 C는 enterprise customer 요구가 명확하게 RPO 0 + 자동 failover일 때 별 epic으로 평가. 옵션 D는 audit chain 본 자산 활용 가치가 크므로 Phase 9+에 재평가.

---

## 4. 아키텍처

### 4.1 토폴로지 (옵션 A 기준)

```
┌──────────────────────────── region-A (ap-northeast-2, primary) ────────────────────────────┐
│                                                                                            │
│  ┌──────────┐   ┌──────────┐         ┌────────────────────────────────────┐                │
│  │ Lodestar │   │ Lodestar │ ──────► │  PostgreSQL primary (write)        │                │
│  │ leader-A │   │ follower │         │   - audit chain INSERT 수신        │                │
│  └──────────┘   └──────────┘         │   - CREATE PUBLICATION rosshield   │                │
│        │             │               └─────────────────┬──────────────────┘                │
│        │             │                                 │                                   │
│        └──── E25 HA ─┘                                 │ logical replication (async)       │
│                                                        │ (~< 1초 lag)                      │
└────────────────────────────────────────────────────────┼───────────────────────────────────┘
                                                         │
                                                         ▼ WAN
┌──────────────────────────── region-B (ap-northeast-1, standby) ────────────────────────────┐
│                                                                                            │
│  ┌──────────┐   ┌──────────┐         ┌────────────────────────────────────┐                │
│  │ Lodestar │   │ Lodestar │ ──read──│  PostgreSQL replica (read-only)    │                │
│  │ standby  │   │ standby  │         │   - CREATE SUBSCRIPTION            │                │
│  └──────────┘   └──────────┘         │   - failover 시 pg_promote()       │                │
│        │             │               └────────────────────────────────────┘                │
│        │             │                                                                     │
│        └─ standby-mode (write 거부 → 503 STANDBY_MODE) ─                                   │
└────────────────────────────────────────────────────────────────────────────────────────────┘

                                ┌────────────────────────┐
                                │  DNS (Route53)         │
                                │   api.lodestar.io      │
                                │   health check 30s     │
                                │   TTL 60s              │
                                │   primary: region-A IP │
                                │   secondary: region-B  │
                                └────────────────────────┘
```

### 4.2 정상 상태 (region-A primary, region-B standby)

- **region-A**: E25 HA 그대로. leader-A가 audit chain write, follower는 read-only. PG primary가 모든 write 수신.
- **region-B**: 모든 인스턴스가 `--standby-mode=true`. write API 호출 시 503 + `STANDBY_MODE` 코드. read API는 정상 응답(replica에서 SELECT). leader-election 비활성(advisory lock 시도 안 함).
- **PG replication**: logical replication slot이 WAL을 region-B로 송신. 평소 lag < 1초.
- **DNS**: `api.lodestar.io` A record가 region-A LB IP. Route53 health check 30초 주기로 region-A `/healthz` 호출.

### 4.3 failover (region-A 장애 감지 → region-B promote)

```
T0:    region-A 전체 장애 (모든 instance + PG 다운)
T0+30s: Route53 health check 3회 연속 실패 → region-A 비정상 판정
T0+30s: 운영자 알림 (PagerDuty/Slack)
T1:    운영자 failover 결정 (수동 — D-MR-3 Phase 8)
T1+30s: 운영자가 region-B PG에서 `SELECT pg_promote()` 실행
T1+1m: region-B Lodestar 인스턴스 재시작 (`--standby-mode=false`)
T1+1m: E25 leader-election 재실행 → region-B 안에서 leader-B 선출
T1+2m: 운영자가 Route53 record 업데이트 (region-B LB IP)
T1+3m: DNS TTL(60s) 캐시 만료 → 사용자 트래픽 region-B 도착
T1+3m: 서비스 복구 (audit chain write 재개)

총 RTO ≈ 3분 (운영자 즉시 반응 가정) ~ 5분 (반응 지연 고려)
```

자동 failover는 Phase 9+ (D-MR-3 옵션) — Patroni/Stolon 등 PG cluster 매니저 통합 평가.

### 4.4 audit chain cross-region 정합성

audit chain은 region-A에서만 write되므로 hash chain head SHA는 region-A PG가 권위. region-B는 replication을 통해 audit_entry 테이블 전체 받음 — head SHA가 자동으로 따라옴.

검증:
- region-B에서 `report verify --cross-region` 실행 → region-A의 head SHA와 region-B의 head SHA가 같은 시점 기준으로 일치하는지 확인 (lag만큼 차이 가능).
- failover 후 region-B 권위로 전환 → region-A 복구 시 region-A는 region-B subscription으로 catch-up. catch-up 완료 전까지 region-A write 금지(standby-mode 강제).

### 4.5 tenant 메타 cross-region

tenant 정의(tenant_id, name, settings)는 모든 도메인 테이블의 외래 키 부모. tenant 메타가 replicate되지 않으면 region-B에서 tenant 조회 실패 → 데이터 노출 위험.

해결: `CREATE PUBLICATION rosshield FOR ALL TABLES` (단순) 또는 `FOR TABLE tenant, audit_entry, scan, robot, evidence, ...` (선택). 권장: **ALL TABLES** — 새 테이블 추가 시 publication 갱신 누락 위험 회피.

---

## 5. TDD 진입

### 5.1 테스트 인프라

- **testcontainers-go 2개 PG container**: primary + replica. primary에 PUBLICATION 생성, replica에 SUBSCRIPTION 생성. 두 container 사이 logical replication 동작 확인.
- **Lodestar 2 instance 시뮬레이션**: 하나는 `--region=A --standby-mode=false`, 다른 하나는 `--region=B --standby-mode=true`. 각각 다른 PG container 연결.
- **failover 시뮬레이션**: testcontainers로 primary PG kill → replica에서 `pg_promote()` 실행 → standby instance restart with `--standby-mode=false` → leader-election 확인.

### 5.2 핵심 테스트 케이스

| ID | 테스트 | 검증 |
|---|---|---|
| **MR.T1** | `TestLogicalReplicationLag` — primary에 100 row INSERT → replica에서 1초 안에 모두 보이는지 확인 | RPO 1분 가정 검증 |
| **MR.T2** | `TestStandbyModeRejectsWrite` — standby instance에 `POST /api/v1/robots` → 503 + `STANDBY_MODE` 응답 | R1 standby read-only 보장 |
| **MR.T3** | `TestStandbyModeAllowsRead` — standby instance에 `GET /api/v1/robots` → 200 + replica의 row 반환 | R1 standby read 가능 |
| **MR.T4** | `TestFailoverPromotesStandby` — primary PG kill → standby `pg_promote()` → standby instance restart → leader-election 성공 (epoch +1) | RTO 5분 가정 검증 |
| **MR.T5** | `TestAuditChainHeadSHACrossRegion` — primary에서 audit entry 5건 INSERT → replication 완료 후 replica에서 head SHA 조회 → primary와 일치 | R2 cross-region 검증 |
| **MR.T6** | `TestSplitBrainPrevented` — primary 살아있는 상태에서 standby에서 `pg_promote()` 강제 시도 → application-level fence(leader_epoch)로 standby write 차단 | split-brain 방어 |
| **MR.T7** | `TestTenantMetaReplicated` — primary에 tenant CREATE → replica에서 tenant 조회 성공 | R5 tenant 격리 cross-region |
| **MR.T8** | `TestReplicationLagMetric` — Prometheus metric `rosshield_replication_lag_seconds` 노출 + WAL position diff 정확 | RPO 모니터링 가능 |

### 5.3 실제 region 시뮬레이션 한계

testcontainers는 같은 host에서 PG container 2개 띄움 — 네트워크 latency·partition 시뮬레이션 한계. WAN 환경 검증은 별도 staging environment(실제 2 region AWS account)에서 수동 drill. 본 epic에서는 unit/integration 한정.

---

## 6. Stage 분해

총 7 stage, **3~4주** 추정(1인 작업 기준).

| Stage | 작업 | 산출 | 추정 |
|---|---|---|---|
| **Stage 1** | PG logical replication 설정 docs | `docs/operations/postgres-logical-replication.md` — PUBLICATION/SUBSCRIPTION 명령, slot 모니터링, lag 알림 | 2일 |
| **Stage 2** | replica 헬스 체크 코드 | `internal/platform/storage/postgres/replication.go` — `ReplicationLag()`, `IsReplica()`, `WALPosition()` 메서드 | 3일 |
| **Stage 3** | bootstrap config `--region`, `--standby-mode` flag | `cmd/rosshield-server/main.go` flag 추가 + standby middleware (`STANDBY_MODE` 503 응답) | 2일 |
| **Stage 4** | DNS 통합 docs | `docs/operations/multi-region-dns.md` — Route53 health check + failover record + Cloudflare 대안 | 2일 |
| **Stage 5** | failover runbook (수동 promote) | `docs/operations/multi-region-failover-runbook.md` — 절차 + 체크리스트 + roll-back | 3일 |
| **Stage 6** | 자동 failover (옵션 — Patroni/Stolon 통합 평가) | `notes/auto-failover-research.md` — research only, 구현은 Phase 9+ | 3일 |
| **Stage 7** | testcontainers e2e | `internal/platform/storage/postgres/replication_integration_test.go` — MR.T1~T8 구현 | 5일 |

Stage 1·2·3은 직렬(논리 의존). Stage 4·5·6은 docs로 병렬 가능. Stage 7은 Stage 2·3 의존.

---

## 7. 결정 항목 (D-MR-1 ~ D-MR-5)

### D-MR-1: replication 방식

- **A) PG logical replication** (권장 default) — async, RPO ~1분, 비용 효율, 표준
- B) PG physical streaming + sync replication — RPO 0, latency +cross-region RTT
- C) Multi-master (CockroachDB/Spanner) — RPO 0 + 자동 failover, vendor lock-in + 마이그레이션

권장: **A**. enterprise customer 진입 시 audit-critical 영역만 B 옵션으로 업그레이드.

### D-MR-2: region 수

- **A) 2 region** (primary + standby) — 권장 default
- B) 3+ region (1 primary + N standby) — 별 epic E-Multi-region-N
- C) active-active multi-region — 옵션 C 의존 (vendor 선택)

권장: **A**. 2 region이면 한일/한미 customer 요구 80% 충족. 3+ region은 Phase 9+ 평가.

### D-MR-3: failover trigger

- **A) 수동 (Phase 8)** — 운영자가 PagerDuty 알림 받고 runbook 절차 실행 — 권장 default
- B) 자동 (Phase 9) — Patroni/Stolon + DNS API 자동 호출 (Phase 9+ 별 epic)

권장: **A** (Phase 8). 자동 failover는 false positive 시 split-brain 위험 — 수동 절차로 안정성 우선. Phase 9에 자동화 평가.

### D-MR-4: DNS provider

- **A) Route53** (AWS-centric) — 권장 default
- B) Cloudflare — multi-cloud customer
- C) NS1 / DNSimple — enterprise 옵션

권장: **A** Route53. AWS multi-region 가정. on-prem customer는 자체 DNS(BIND/PowerDNS) 가이드 별첨.

### D-MR-5: read replica 활용

- A) Phase 8 — read replica를 read API에 활용 (load balancing)
- **B) Phase 9+ deferral** — 본 epic 대상 외, standby 전용 — 권장 default

권장: **B**. read replica를 read API에 활용하면 stale read 가능(lag만큼) → 정합성 복잡도 ↑. Phase 9+ 별 epic에서 평가.

---

## 8. 변경 사항 outline

총 ~1500줄 추정(코드 + docs).

| 파일 | 신규/수정 | 추정 줄 |
|---|---|---|
| `internal/platform/storage/postgres/replication.go` | 신규 | ~200 (ReplicationLag, IsReplica, WALPosition, 헬스 체크) |
| `internal/platform/storage/postgres/replication_integration_test.go` | 신규 | ~400 (MR.T1~T8 testcontainers 2 PG) |
| `cmd/rosshield-server/main.go` | 수정 | +30 (`--region`, `--standby-mode` flag) |
| `internal/transport/http/middleware/standby.go` | 신규 | ~80 (STANDBY_MODE middleware) |
| `internal/transport/http/middleware/standby_test.go` | 신규 | ~120 |
| `internal/platform/storage/postgres/migrations/0027_region_label.up.sql` | 신규 | ~30 (region_label 컬럼) |
| `internal/platform/storage/postgres/migrations/0027_region_label.down.sql` | 신규 | ~10 |
| `docs/operations/postgres-logical-replication.md` | 신규 | ~250 |
| `docs/operations/multi-region-dns.md` | 신규 | ~150 |
| `docs/operations/multi-region-failover-runbook.md` | 신규 | ~200 |
| `docs/design/notes/auto-failover-research.md` | 신규 (Stage 6) | ~80 |
| 합계 | | **~1550줄** |

추정 분해: 코드 ~860 + docs ~690.

---

## 9. 검증

### 9.1 단위·integration

- testcontainers 2 PG container로 MR.T1~T8 자동화
- `make test` 통과 (3분 이내, 2 container 동시 띄우는 비용 고려)
- replication lag 측정 정확도 (WAL position diff 기반)

### 9.2 staging drill (실제 2 region)

- AWS staging account에서 ap-northeast-2(primary) + ap-northeast-1(standby) 구성
- 분기 1회 failover drill 실시 (운영자 절차 + RTO 측정)
- audit chain head SHA cross-region 일치 검증 (`report verify --cross-region`)

### 9.3 모니터링

추가 Prometheus 메트릭:

| 이름 | 타입 | 설명 |
|---|---|---|
| `rosshield_region_label` | gauge | 0=primary, 1=standby |
| `rosshield_replication_lag_seconds` | gauge | replica WAL apply lag |
| `rosshield_replication_slot_active` | gauge | logical slot 활성 여부 |
| `rosshield_standby_mode_rejections_total` | counter | standby region에서 거부한 write 수 |
| `rosshield_failover_total` | counter | 누적 region failover 횟수 |

알림:
- `rosshield_replication_lag_seconds > 60` 5분 지속 → 경고 (RPO 위반 가능)
- `rosshield_replication_slot_active == 0` → 즉시 알림 (replication 끊김)
- region-A `/healthz` 3회 연속 실패 → 즉시 운영자 호출 (failover 결정)

---

## 10. 비즈니스 / 라이선스 영향

### 10.1 라이선스 분류

- **코어 (Apache-2.0)**: 옵션 A 전체. PG logical replication 기반 + 수동 failover runbook + DNS 통합 docs. 표준 PG 기능 활용 — 특허 비대상.
- **enterprise (BSL/Commercial — D5 결정 후)**:
  - 자동 failover (D-MR-3 Phase 9, Patroni/Stolon 통합) — enterprise customer 차별 기능
  - sync replication 옵션 B + audit-critical 가이드 — enterprise customer SLA 99.99% 영역
  - read replica load balancing (D-MR-5 Phase 9+) — 성능 우위 옵션

### 10.2 가격 차별화

enterprise tier에 multi-region HA를 포함:
- Basic tier: single-region HA (E25)만
- Pro tier: multi-region HA (옵션 A) + 수동 failover runbook
- Enterprise tier: + 자동 failover + sync replication + SLA 99.99%

---

## 11. 리스크

| 리스크 | 영향 | 완화 |
|---|---|---|
| logical replication lag → RPO 보장 못함 | 1분 초과 lag 시 데이터 손실 | 모니터링 + 알림 (lag > 60s) + audit-critical은 옵션 B(sync)로 업그레이드 |
| DNS TTL 캐싱 → failover 사용자 영향 최대 60s | RTO 60s 가산 | TTL 30s까지 단축 + 클라이언트에 retry 권장 + 알림 후 DNS 갱신 |
| split-brain (network partition으로 양 region 모두 promote) | audit chain 분기 → 데이터 손상 | application-level fence(leader_epoch) cross-region 확장 + 수동 promote 강제(자동 X) |
| tenant 메타 replication 누락 | region-B에서 tenant 조회 실패 → 데이터 노출 | `FOR ALL TABLES` PUBLICATION + 새 테이블 추가 시 검증 테스트 |
| 운영자 failover 절차 실수 | RTO 초과 또는 데이터 손실 | 분기 1회 drill 의무화 + runbook 체크리스트 형식 |
| replication slot 누적 | primary disk full | slot 모니터링 + standby 끊긴 후 24시간 내 alert |
| region 간 latency로 sync replication 미적용 | 옵션 B 채택 어려움 | 옵션 A를 default로 + 옵션 B는 audit-critical customer 전용 |
| PG version mismatch (primary 14, standby 16) | logical replication 호환성 깨짐 | 운영 가이드에 동일 major version 강제 |
| WAN 비용 (logical replication 트래픽) | 월 비용 증가 (~$50~200) | docs에 추정 + customer 가이드 |
| failover 후 원래 region 복구 시 split-brain 위험 | 데이터 손상 | runbook에 demote + rebuild 절차 강제 (자동 재진입 금지) |

---

## 12. 결정 로그

### 작성 이력

- **2026-05-19**: 본 design draft 작성. Phase 8 carryover로 분류. 옵션 A 권장 default 제안. D-MR-1~5 결정 대기.

### 결정 요청 (사용자 합의 필요)

| ID | 항목 | 권장 | 상태 |
|---|---|---|---|
| D-MR-1 | replication 방식 | A (PG logical) | 대기 |
| D-MR-2 | region 수 | A (2 region) | 대기 |
| D-MR-3 | failover trigger | A (수동, Phase 8) | 대기 |
| D-MR-4 | DNS provider | A (Route53) | 대기 |
| D-MR-5 | read replica 활용 | B (Phase 9+ deferral) | 대기 |

### 의존 epic

- **선행**: E25 single-region HA (완료 가정)
- **후행 후보**: E-AutoFailover (Phase 9+, D-MR-3 B 옵션), E-MultiRegion-N (Phase 9+, D-MR-2 B 옵션), E-EventSourcing (Phase 9+, 옵션 D)

---

## 13. 참조

- `docs/design/notes/e25-ha-design.md` — single-region HA (PG advisory lock + leader/follower)
- `docs/design/03-architecture.md` §3.3 분리 모드, §3.10 실패·복구
- `docs/design/10-audit-and-observability.md` §10.4 해시 체인 — cross-region 검증 base
- `docs/design/11-tech-stack-and-roadmap.md` Phase 5 carryover (Pack Mirror 등과 동일 위계)
- `internal/platform/ha/` Manager + PGLock + RoleProvider + ErrNotLeader (E25 산출)
- `internal/platform/storage/postgres/pg.go` 단일 region pgxpool (본 epic 확장 대상)
- PostgreSQL 공식 문서:
  - [Logical Replication](https://www.postgresql.org/docs/current/logical-replication.html)
  - [Streaming Replication](https://www.postgresql.org/docs/current/warm-standby.html#STREAMING-REPLICATION)
  - [pg_promote()](https://www.postgresql.org/docs/current/functions-admin.html#FUNCTIONS-RECOVERY-CONTROL)
- AWS Route53 [Health Checks and DNS Failover](https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/dns-failover.html)
- "Patterns of Distributed Systems" — Unmesh Joshi (Replicated Log, Leader and Followers, Generation Clock)
