# Scanrun 후속 — Pool size 동적 + per-tenant rate limit + per-robot circuit breaker (Phase 5)

> **상태**: Design draft (Phase 5 — scanrun 3 epic 마감 직후 후속, D-SCANEX 결정 대기)
> **작성일**: 2026-05-15
> **범위**: `internal/platform/sshpool/` Pool 동시성 모델 확장 + `internal/app/scanrun/` Orchestrator 통합. scanrun-ssh-integration-design.md Stage 1~5c 마감 (head `76ae2f0`) 직후 운영 부하·multi-tenant 격리 강화 후속 epic 정리.
> **참조**: `scanrun-ssh-integration-design.md` (직전 epic, Stage 1~5c 마감), `e6-ssh-scan-deepdive.md` R4-1·R4-7, `§01.4` 멀티테넌시 기본값, `§06` 보안·멀티테넌시, `§07.2`·`§07.7` 스캔 엔진, `phase5-backlog.md`.
> **비목표**: 새 transport(WebSocket·gRPC) 도입, 옵션 C 에이전트 모델, `scanrun.Orchestrator` fan-out 모델 자체 변경, ROS2 specific pack 추가(별 design doc), per-fleet 동시 ScanSession 허용(D-SCAN-6 (1) 유지).
> **코드 변경**: 0건 / 마이그레이션 0건. 본 문서는 docs only — 실제 구현은 D-SCANEX 결정 후 별도 PR(stage 분해 §7 참조).

---

## 1. 상태·배경

### 1.1 본 doc 위치

본 doc은 **Phase 5 scanrun 3 epic(scanrun-ssh-integration + RBAC + PWA) 마감 직후** 후속 가치 발굴 design doc입니다. SESSION_HANDOFF "현재 상태 한 줄"의 head `ee2aa34`(2026-05-15) 직후 — scanrun Stage 1~5c 5/5 모두 마감, RBAC 5/5 마감, PWA 4/4 마감으로 paying customer 진입 가능한 baseline이 완성된 직후의 후속 epic 정리.

memory feedback `feedback_design_doc_first.md` 일관 — 1일+ 잠재 작업은 코드 진입 전 design doc 작성. 본 doc 자체는 **코드 0줄 / 마이그레이션 0건**.

### 1.2 직전 scanrun epic이 cover한 것 — 한 페이지 요약

`scanrun-ssh-integration-design.md`의 Stage 1~5c가 다음을 production-quality로 결선:

| Stage | 결선 항목 | head | 위치 |
|---|---|---|---|
| 1 | `robot.RobotHostKey` 도메인 + 마이그레이션 0027 (TOFU host key, D-SCAN-2) | — | `internal/domain/robot/host_key.go`, `migrations/sqlite/0027_robot_host_keys.sql` |
| 2 | `KnownHostsManager` + sshpool host key callback | — | `internal/platform/sshpool/knownhosts.go` |
| 3 | bootstrap 결선 + `sudo -n` non-interactive (D-SCAN-3) | — | `cmd/rosshield-server/bootstrap.go`, `sshpool.SudoMode` |
| 4 | Pool idle 재사용 + IdleTimeout(5min) eviction + keepalive(30s) + metrics 5종 | — | `internal/platform/sshpool/pool.go` `popIdle`/`pushIdle`/`keepaliveLoop` |
| 5a | per-robot health window (HealthFailureThreshold=3, robot_offline 즉시 skip) | — | `internal/app/scanrun/scanrun.go` `healthState` (Run scope) |
| 5b | bootstrap Pool 결선 (idle 재사용 활성화 — IdleTimeout=5min · KeepaliveInterval=30s) | — | `cmd/rosshield-server/bootstrap.go` `sshpool.NewPool` |
| 5c | docker-compose.ssh.yml + sshd_e2e_test.go 5 Phase + Makefile `test-ssh-e2e` | `76ae2f0` | `test/integration/sshd_e2e_test.go` |

**결과**: 첫 paying customer 후보에 "당신 robot N대를 우리가 진짜로 SSH로 스캔합니다 + host key MITM 차단 + idle 재사용으로 latency 절감 + offline robot 즉시 skip"이라는 baseline 진술 가능.

### 1.3 본 doc이 다루는 후속 가치 4 epic

직전 baseline 위에 다음 시나리오가 trigger되면 본 doc의 후속 epic이 가치를 가집니다:

- **시나리오 1**: customer A의 fleet이 100 robot, customer B는 10 robot. 같은 rosshield-server 인스턴스가 두 tenant를 서비스. customer A 폭주 시 customer B 영향 차단 — 현재 PerTenantLimit(50 conn) semaphore만 존재 → conn 수만 제한, **exec 빈도(`req/s`) 제한 부재**.
- **시나리오 2**: customer 환경별 robot 부하 편차 — 어떤 host는 SSH session 5개 동시면 OK, 어떤 host는 2개로도 죽음. 현재 PerHostLimit=5 **고정** → 운영 중 customer마다 재시작·재배포 필요.
- **시나리오 3**: 영구 장애 robot이 fleet에 1대 — 현재 health window는 단일 Run scope. **다음 Run 시작 시 카운터 reset → 다시 dial 시도** → 매 Run마다 timeout × HealthFailureThreshold(3) 만큼 worker 점유.
- **시나리오 4**: 운영자가 customer 부하 스파이크·rate limit 발동·circuit state 변경을 Grafana dashboard에서 보고 싶음 — 현재 metric 5종(exec_total, exec_duration_seconds, dial_total, idle_conns_gauge)만, **rate state·circuit state·pool size 동적 변동 metric 부재**.

### 1.4 paying customer 0인 현 단계 ROI 평가

memory `feedback_design_doc_conservative.md` 일관 — 잠재 가치를 보수적으로 평가:

- **시나리오 1·3**: enterprise customer 진입 시 즉시 trigger. multi-tenant noisy neighbor + offline robot 비용은 첫 deployment에서 발생.
- **시나리오 2**: customer 환경 다양성에 대한 적응력. 첫 1~2 customer는 환경 균질, 3+ customer 시점에 trigger 가능성 ↑.
- **시나리오 4**: customer 진입 후 운영 디버깅 자료. 진입 *전*에는 dashboard 자체가 없을 수 있음 — 우선순위 가장 낮음.

**결론**: 4 epic 모두 "있으면 좋음 / 없어도 baseline은 작동". 그러나 시나리오 1·3은 첫 enterprise customer 진입 시 즉시 trigger되므로 우선순위 높음. 본 doc은 4 epic을 카탈로그화하되 권장 옵션은 §5에서 정의.

---

## 2. 현재 상태 진단 — 코드 trace + cover 매트릭스

### 2.1 Pool 동시성 코드 trace (Stage 4·5b 결선 후)

`pool.Acquire`의 한 cycle (`internal/platform/sshpool/pool.go:171`):

```
pool.Acquire(ctx, key, target)
  ↓ closed 체크 (mu)
  ↓ semFor(hostSems, hostKey, PerHostLimit=5)            // 고정
  ↓ semFor(tenSems, tenantID, PerTenantLimit=50)         // 고정
  ↓ tenSem <- struct{}{}  | ctx.Done                      // per-tenant 슬롯
  ↓ hostSem <- struct{}{} | ctx.Done                      // per-host 슬롯
  ↓ if IdleTimeout > 0: popIdle(key)                      // Stage 4 idle 재사용
  ↓ if miss: dialWithBackoff(ctx, target)                 // backoff: base*2^n + jitter
  ↓ release: isAlive ? pushIdle : Close + sem 반환
  ↓ keepaliveLoop (30s tick): evictExpiredAndDead         // Stage 4
```

### 2.2 cover/no-cover 매트릭스

| 측면 | 현재 cover (Stage 1~5c) | 본 doc 후속 cover 후보 |
|---|---|---|
| per-host 동시 conn | PerHostLimit=5 **고정** (PoolConfig) | adaptive throttle (per-host 부하 측정 → 동적 조정) — **epic A** |
| per-tenant 동시 conn | PerTenantLimit=50 **고정** semaphore | conn 수 제한만 — **exec 빈도 제한 부재** — **epic B** |
| per-tenant exec rate | 없음 — exec 빈도 제한 0 | token bucket 또는 leaky bucket — **epic B** |
| dial backoff | per-call exponential + jitter (DialMaxRetries=3, DialBaseDelay=200ms) | 영구 장애 robot은 매 Run에서 재시도 — **epic C** |
| per-robot health | Run scope 카운터 (HealthFailureThreshold=3) — Run 종료 시 GC | Run 간 persistent 없음 — **epic C** |
| circuit state | 없음 — open/half-open/closed 모델 부재 | 시간 기반 회복 (open → half-open after timeout → closed) — **epic C** |
| pool size 변동 metric | idle_conns_gauge만 (총합) | per-host limit 변동·rate limit 발동·circuit state 변경 — **epic D** |
| dial 실패 metric | dial_total{result=fail} 카운터 | per-tenant·per-robot label dimension 부재 — **epic D** |
| keepalive | 30s 주기 idle conn ping | OK — 본 doc 변경 0 |
| host key TOFU | OK (Stage 2 결선) | OK — 본 doc 변경 0 |
| sudo `-n` | OK (Stage 3 결선) | OK — 본 doc 변경 0 |
| docker e2e 5 Phase | OK (Stage 5c 결선) | OK — 본 doc은 e2e 회귀 테스트 추가 |

### 2.3 도메인 경계 측면 — 본 doc이 깨면 안 되는 것

`scan` 도메인은 `robot`·`benchmark`·`sshpool`을 직접 import 안 함(P5 + depguard). 본 후속 epic도 `sshpool` 패키지 자체에서 변경(`pool.go` 확장) 또는 `cmd/rosshield-server/` bootstrap adapter layer에서만 결선. `scan.SSHExecutor` interface는 그대로 유지.

특히 epic B(rate limit)는 **sshpool 내부 결선**이 자연스러움 — Acquire 직전 token bucket 통과 게이트로. epic C(circuit breaker)도 동일 layer. epic D(metric)는 `internal/platform/metrics/` 확장만으로 처리 가능.

### 2.4 multi-tenant 격리 측면 — 본 doc이 강화하는 것

`§01.4 멀티테넌시 기본값` + `§06 security-and-tenancy` 정합 — 모든 후속 epic은 tenant scope를 강제:

- epic A: per-host limit 동적 조정도 **(tenant_id, host) tuple 단위** — cross-tenant pool 재사용 절대 금지(현재도 PoolKey에 TenantID 포함).
- epic B: rate limit 단위는 **per-tenant** 또는 **(tenant, robot)** — 다른 tenant exec 빈도에 영향 0.
- epic C: circuit state 저장도 **tenant_id NOT NULL** 필수 (DB persistent 시) — 원칙 4.
- epic D: metric label에 `tenant_id` 추가 — 단, cardinality 폭발 회피(§8 회귀 위험 참조).

---

## 3. 요구 사항 분류 — 4 후보 epic 시나리오·트리거

각 epic의 trigger 조건과 가치를 정밀화. 다음 세션이 우선순위 결정에 사용.

### 3.1 Epic A — Pool size 동적 조정

**가치**: customer robot host 부하 다양성에 적응. 운영 중 재시작 없이 per-host limit 조정.

**시나리오**:
- 부팅 시 PerHostLimit=5 default. 어떤 host에서 dial 실패율(>30%) 또는 exec timeout 비율(>20%) 관찰되면 → limit을 동적으로 3 → 2 → 1로 throttle.
- 부하 안정 시(연속 N분 dial 성공률 >95%) → 점진적 회복(1 → 2 → … → 5).
- per-host 부하 측정: 최근 K회(예: 100회) sliding window 통계.

**트리거 customer 패턴**:
- 임베디드 robot(저성능 CPU·메모리)에서 SSH session 5개 동시 시 sshd OOM·timeout 빈발.
- 무선 네트워크(불안정)에서 link flap이 자주 발생 → TCP 재전송 폭증.

**Cover하지 않는 것**: per-tenant 동적 조정은 별도(epic B에서 cover). 운영자가 admin API로 수동 limit override는 별 epic.

**비고**: 영감 = AIMD(Additive Increase Multiplicative Decrease) — TCP congestion control 표준 패턴. Stage 4 결선된 metric `exec_total{outcome}`·`dial_total{result}`로 측정.

### 3.2 Epic B — per-tenant exec rate limit (token bucket)

**가치**: noisy neighbor 차단. 한 tenant의 exec 폭주가 다른 tenant SLA 영향 0.

**시나리오**:
- customer A(100 robot × 50 check = 5000 exec)가 ScanSession 시작. customer B(10 robot)도 동시 진행.
- 현재: customer A가 PerTenantLimit=50 conn 슬롯을 모두 점유, customer B는 별도 50 슬롯이지만 **CPU·DB·log I/O는 공유** → customer B latency 영향.
- 개선: per-tenant exec rate(예: 100 req/s) 제한 token bucket. customer A burst는 자체 큐로 흡수.

**알고리즘 후보**:
- **token bucket** (`golang.org/x/time/rate.Limiter`): refill rate + burst capacity. 표준 라이브러리, 검증.
- **leaky bucket**: token bucket과 거의 동등, queue depth가 명시적.
- **sliding window log**: 정확하지만 메모리 비용 큼 (req timestamp 보존).

**트리거 customer 패턴**:
- 첫 enterprise customer = multi-tenant deployment (예: 본 제품을 SI 업체가 호스팅하면서 여러 robotics customer를 같은 인스턴스로 서비스).
- 한 tenant가 DDoS 또는 misconfigured cron으로 폭주.

**Cover하지 않는 것**: per-(tenant, robot) rate limit은 별 cardinality. 본 doc은 per-tenant만 권장 default.

**비고**: token bucket이 표준 — `golang.org/x/time/rate`는 std-adjacent, dep 추가 0(이미 indirect 가능). per-tenant `*rate.Limiter` map + lazy init.

### 3.3 Epic C — per-robot circuit breaker

**가치**: 영구 장애 robot의 회복 시점을 시간 기반으로 추정. Run 간 persistent 카운터.

**시나리오**:
- robot R1이 영구 down(하드웨어 고장 또는 long-term maintenance). 현재 health window: Run 1에서 3회 실패 후 skip → Run 2 시작 시 카운터 reset → 다시 3회 dial 시도 → timeout × 3 worker 점유.
- 개선: circuit breaker open 후 cool-down(예: 5분). cool-down 동안 dial 자체 skip. cool-down 종료 시 half-open(1회만 dial 시도) → 성공 시 closed(정상), 실패 시 open 갱신.

**상태 머신**:
```
closed → (N consecutive failures) → open → (timeout 경과) → half-open
                                                                ↓
                                              success → closed | fail → open
```

**저장**:
- (a) **in-memory** (process scope) — Orchestrator 재시작 시 reset. 단순.
- (b) **DB persistent** — `robot_circuit_state` 테이블. multi-instance HA(E25)에서 leader 변경 시 state 보존.
- (c) **Redis 또는 외부 캐시** — 분산 환경 친화. 새 dep.

**트리거 customer 패턴**:
- fleet에 영구 장애 robot이 1~수 대 (운영 중 종종 발생). 매 일·시간 단위 cron Scan에서 timeout 누적.
- robot 점검 mode (예: 한 시간 down 후 복귀) — circuit breaker가 자연 회복.

**Cover하지 않는 것**: per-(host, port) circuit (multiple robot이 같은 host 공유 시 — 드문 케이스). per-tenant circuit (한 tenant 전체 폭주) — multi-tenant SLA 측면에서 다른 mechanism.

**비고**: 영감 = sony/gobreaker 또는 hashicorp/go-breaker — 표준 패턴. dep 0 자체 구현 가능 (state machine 단순).

### 3.4 Epic D — observability 확장

**가치**: 운영자가 본 후속 epic의 효과를 Grafana에서 가시화.

**추가 metric 후보** (현재 Stage 4 metric 5종 위에):
- `rosshield_ssh_pool_per_host_limit{tenant,host}` gauge — epic A 발동 시 동적 변경 추적.
- `rosshield_ssh_rate_limit_total{tenant,result=allow|deny}` counter — epic B token bucket 통과·차단 카운트.
- `rosshield_ssh_circuit_state{tenant,robot,state=closed|open|half_open}` gauge — epic C state 머신 상태.
- `rosshield_ssh_circuit_transition_total{tenant,robot,from,to}` counter — state 전이 카운트.

**Cover하지 않는 것**: OpenTelemetry tracing은 별 phase(D-SCAN-7과 일관). 본 doc은 Prometheus carrier만.

**트리거 customer 패턴**:
- 운영자 SOC dashboard에 Grafana 패널 추가 요청. customer onboarding 후 발생.

**비고**: cardinality 주의 — `tenant`+`robot` 두 label은 fleet 크기 × tenant 수 곱으로 폭증 가능. circuit state 같이 의미상 필요한 경우만 robot label 부착 (rate_limit는 tenant까지만).

---

## 4. 합성 전략 옵션 ≥3

### 옵션 A — 4 epic 일괄 적용 (단일 큰 PR)

본 doc 4 epic을 한 단위로 묶어 Stage 5~10 추가 commit으로 진행.

**Pros**:
- 일관된 설계 — 한 번에 rate limit + circuit + pool dynamic + metric을 통합 디자인.
- multi-tenant 격리 강화 효과를 한 번에 demo 가능.
- Phase 5 마감 전 production 강도 끌어올림.

**Cons**:
- 추정 5~7일 — 다른 Phase 5 카드 일정 압박.
- paying customer 0인 현 단계에서 ROI 미명. premature optimization 위험(memory `feedback_design_doc_conservative.md` 일관).
- 회귀 위험 — Stage 5b 결선된 Pool 동작에 4 epic 모두 결선되면 e2e 회귀 surface 큼.
- 단일 큰 PR — 부분 rollback 어려움.

**회귀 위험**: 중. `pool.go`·`scanrun.go` 두 핫 path에 동시 침투.

**추정**: 5~7일 (epic A 1.5d + B 1d + C 2d + D 0.5d + 통합 e2e 1d).

### 옵션 B — 우선순위별 순차 (가장 가치 높은 1개부터)

§3 trigger 시나리오 분석에 따라 다음 우선순위로 순차 진행:

1. **Epic C** (circuit breaker) — 영구 장애 robot은 첫 deployment에서 즉시 발생. ROI 가장 높음.
2. **Epic B** (per-tenant rate limit) — 첫 multi-tenant deployment(SI 업체 hosting)에서 trigger.
3. **Epic A** (pool size dynamic) — customer 환경 다양성 누적 후. 3+ customer 시점.
4. **Epic D** (observability) — 위 3건의 부산물로 자연 따라옴.

각 epic을 별 design doc + 별 PR로. 본 doc은 카탈로그 + 우선순위 + Stage 분해만 정의.

**Pros**:
- ROI 가장 높은 epic부터 trigger되는 시점에 진입 — premature optimization 회피.
- 각 PR 작아 부분 rollback·회귀 surface 최소.
- customer 진입 전에는 본 doc만 두고 코드 0 — 다른 Phase 5 카드 일정 보호.
- memory `feedback_design_doc_conservative.md` 일관.

**Cons**:
- 4 epic 전체 마감까지 시간 분산 (Phase 6+ 까지 carryover 가능).
- 단일 통합 디자인의 일관성 결여 — 후행 epic이 선행 epic 결정을 우회·재작업 가능.
- 운영자 입장 — circuit breaker만 있고 rate limit 없는 중간 상태에서 이상 패턴 보일 수 있음.

**회귀 위험**: 낮음 — 각 epic 단독 결선이라 surface 좁음.

**추정**: 본 doc 자체 0.5d. 각 epic trigger 시점에 별 design doc 0.5d + 구현 1~2일.

### 옵션 C — 보류 (current ROI 미미, customer 진입 후 trigger)

본 doc만 작성하고 4 epic 모두 carryover. 코드 진입 0.

**Pros**:
- paying customer 0인 현 단계 ROI 평가 — premature optimization 회피 최강.
- Phase 5 마감 + Phase 6 진입 가속.
- 첫 enterprise customer 환경 정보 확보 후 정밀하게 디자인 가능 (예: rate limit threshold는 customer 실 부하로 결정).
- memory `feedback_design_doc_conservative.md` 일관.

**Cons**:
- 첫 enterprise customer 진입 시 trigger되면 baseline에 epic 1~2건 부족 → onboarding 도중 hot-fix 필요.
- 본 doc이 1~2 phase 동안 idle — 결정 항목 stale 가능.

**회귀 위험**: 0 (코드 변경 0).

**추정**: 본 doc 자체 0.5d.

### 옵션 D — 최소 cover (epic C만 즉시, 나머지 보류)

옵션 B + 옵션 C의 절충. epic C(circuit breaker)만 즉시 결선 — 영구 장애 robot이 첫 deployment에서 즉시 trigger되는 것이 분명하므로. 나머지 3 epic은 carryover.

**Pros**:
- 가장 큰 운영 부담(영구 장애 robot timeout)을 제거.
- 작업량 1.5~2일 — Phase 5 마감 일정 부담 최소.
- epic C는 in-memory 모델로 충분 (옵션 (a)) — 새 마이그레이션 0.

**Cons**:
- multi-tenant rate limit은 여전히 부재 — 첫 SI hosting 진입 시 trigger되면 hot-fix.
- pool dynamic·observability 확장은 후속 별 epic.

**회귀 위험**: 낮음. 단일 epic 침투.

**추정**: 1.5~2일 (epic C in-memory + metric 일부).

### 옵션 E — 옵션 D + epic D(metric) 동시 (권장 후보)

옵션 D에 추가로 epic D의 일부(circuit state 변동 metric)만 epic C와 함께 결선. 다른 metric은 후속.

**Pros**:
- circuit breaker 효과를 운영자가 즉시 Grafana에서 확인 가능.
- metric은 epic C 결선의 부산물 — 추가 비용 0.5일 미만.

**Cons**:
- rate limit·pool dynamic 부재는 여전.

**회귀 위험**: 낮음.

**추정**: 2~2.5일.

---

## 5. 권장 옵션 + 근거

### 5.1 권장: **옵션 B (우선순위별 순차)** — 본 doc은 카탈로그 + 우선순위 + Stage 분해

paying customer 0인 현 단계에서 옵션 A 일괄 적용은 premature optimization 위험. 옵션 C 완전 보류는 epic C(circuit breaker)의 즉시 trigger 가능성을 무시. 옵션 D·E는 epic C만 우선 결선 — **나쁜 옵션 아님, 사용자 승인 시 옵션 B 내 epic C부터 진입과 동등**.

**근거**:

1. **ROI 평가 (memory `feedback_design_doc_conservative.md`)** — 4 epic 모두 customer 진입 시 trigger. 그러나 trigger 확률·시점 분산:
   - epic C: 첫 deployment 즉시 (확률 100%).
   - epic B: 첫 multi-tenant hosting 시 (확률 ~50% — 모든 customer가 multi-tenant는 아님).
   - epic A: 3+ customer 누적 후 (확률 ~30%).
   - epic D: 운영자 dashboard 요청 시 (확률 ~70%, 후행).

2. **회귀 위험 최소** — 옵션 B는 각 epic 단독 PR. Stage 5b 결선된 Pool 동작에 한 번에 4 침투 회피. 본 doc Stage 분해 §7가 epic별 분해.

3. **paying customer 0 단계 정합** — 본 doc은 카탈로그·결정 항목·권장 default만. 실 결선은 customer trigger 시 별 design doc(예: `scanrun-circuit-design.md`)으로 진입. memory `feedback_design_doc_first.md` 일관 — 다음 세션 부담 0.

4. **D5 open-core 정합** — circuit breaker·rate limit은 enterprise tier 친화 기능. 코어/enterprise 분리 시점(첫 paying customer 직전)에 자연스럽게 enterprise로 분리 가능.

5. **옵션 C 완전 보류는 약간 위험** — 첫 enterprise customer가 영구 장애 robot 1대 가진 fleet으로 onboarding 시 첫 ScanSession에서 timeout 누적이 demo 인상에 부정. epic C는 우선순위 1로 두되 진입 시점은 customer onboarding 직전.

### 5.2 본 doc 종료 후 다음 진입 절차

1. SESSION_HANDOFF "현재 상태 한 줄" + "다음 후보" 갱신 — 본 doc 위치 + 4 epic 우선순위.
2. customer trigger 또는 사용자 명시 결정 시 epic C 별 design doc 진입 (`scanrun-circuit-design.md`).
3. 그 외 epic은 trigger 시 진입.

### 5.3 옵션 A를 *지금* 채택하지 않는 추가 이유

- 5~7일 작업은 Phase 5 마감 직후 다른 가치(예: ROS2 baseline pack, 첫 customer onboarding 자료, deployment runbook)와 경쟁.
- 4 epic 통합 e2e가 docker compose 시나리오 추가 필요 — 본 doc Stage 5c 결선된 sshd_e2e_test 구조 위에 누적.
- 옵션 B로 점진 진입해도 기술 부채 없음 — 각 epic의 결정 항목이 §8에 권장 default 명시되어 있어 설계 일관성 보존.

### 5.4 옵션 D·E와의 비교

옵션 D·E는 epic C만(또는 +D) 즉시 결선. 본 권장 옵션 B와 실질 차이는 **"epic C 진입 시점이 지금이냐 customer trigger 시점이냐"**. 사용자가 옵션 D·E를 선택해도 본 doc Stage 분해(§7 epic C 부분)가 그대로 적용. 본 doc이 옵션 B를 권장하지만 옵션 D·E도 healthy.

---

## 6. 변경 사항 outline (옵션 B 채택 시 — epic 별 분리)

본 절은 옵션 B 권장 default — 각 epic이 trigger되어 진입하는 시점에 다음 세션이 즉시 코드에 진입할 수 있는 정밀도로 기술. memory `feedback_design_doc_first.md` 일관.

### 6.1 epic A — Pool size 동적 조정

#### 신규 파일
| 파일 | 책임 | 추정 LOC |
|---|---|---|
| `internal/platform/sshpool/throttle.go` | `HostThrottle` 구조체 — sliding window(K=100) 통계 + AIMD 로직 + `CurrentLimit(host) int` | ~180 |
| `internal/platform/sshpool/throttle_test.go` | window 통계·AIMD 증감·동시 host 격리 단위 테스트 | ~250 |

#### 수정 site
| 파일·함수 | 변경 |
|---|---|
| `internal/platform/sshpool/pool.go` `pool` 구조체 | `throttle *HostThrottle` 추가 (PoolConfig nil 허용) |
| `internal/platform/sshpool/pool.go` `Acquire` | `hostSem` capacity를 `throttle.CurrentLimit(host)`로 동적 조회 — semaphore swap 메커니즘 또는 `golang.org/x/sync/semaphore.Weighted` 마이그레이션 |
| `internal/platform/sshpool/pool.go` `release` | dial 결과·exec timeout 결과를 `throttle.RecordResult(host, outcome)` 콜백 |
| `internal/platform/metrics/metrics.go` | `SSHPoolPerHostLimit` GaugeVec 추가 (label: tenant·host) |

#### 마이그레이션
- 0건. in-memory state.

### 6.2 epic B — per-tenant exec rate limit

#### 신규 파일
| 파일 | 책임 | 추정 LOC |
|---|---|---|
| `internal/platform/sshpool/ratelimit.go` | `TenantRateLimiter` — `map[tenantID]*rate.Limiter` + lazy init + Wait(ctx) 메서드 | ~120 |
| `internal/platform/sshpool/ratelimit_test.go` | token bucket allow/deny + 다른 tenant 격리 + ctx cancel 단위 테스트 | ~200 |

#### 수정 site
| 파일·함수 | 변경 |
|---|---|
| `internal/platform/sshpool/pool.go` `PoolConfig` | `RateLimit *RateLimitConfig` 필드 (per-tenant rate, burst) |
| `internal/platform/sshpool/pool.go` `Acquire` | semaphore acquire 직후 `rateLimiter.Wait(ctx, tenantID)` 호출 — 차단 시 ctx 만료 또는 bucket refill 대기 |
| `cmd/rosshield-server/bootstrap.go` | `sshpool.NewPool` 인자에 RateLimit 100 req/s + burst 50 default 추가 |
| `internal/platform/metrics/metrics.go` | `SSHRateLimitTotal` CounterVec (label: tenant·result) |

#### 마이그레이션
- 0건. in-memory state.

### 6.3 epic C — per-robot circuit breaker

#### 신규 파일
| 파일 | 책임 | 추정 LOC |
|---|---|---|
| `internal/platform/sshpool/circuit.go` | `CircuitBreaker` — state machine(closed/open/half-open) + per-robot map + Allow(robotID)/Record(robotID, success bool) | ~200 |
| `internal/platform/sshpool/circuit_test.go` | state 전이·timeout 회복·half-open 1회 시도·동시 robot 격리 단위 테스트 | ~300 |

#### 수정 site
| 파일·함수 | 변경 |
|---|---|
| `internal/app/scanrun/scanrun.go` `Deps` | `CircuitBreaker sshpool.CircuitBreakerLike` 인터페이스 추가 (P5 — sshpool 직접 import 회피, scanrun에서 인터페이스 정의) |
| `internal/app/scanrun/scanrun.go` `executeOne` | health window 체크 직후 `circuit.Allow(robotID)` 추가 — open이면 OutcomeSkipped(reason="circuit_open") |
| `cmd/rosshield-server/bootstrap.go` | `sshpool.NewCircuitBreaker(timeout=5min, failureThreshold=5)` + Deps 주입 |
| `internal/platform/metrics/metrics.go` | `SSHCircuitState` GaugeVec (label: tenant·robot·state) + `SSHCircuitTransition` CounterVec |

#### 마이그레이션
- 권장 default: in-memory(option (a)). DB persistent(option (b))는 별 epic.

### 6.4 epic D — observability 확장

#### 신규 파일
- 0건. epic A·B·C 결선 시 동시 metric 추가가 자연스러움.

#### 수정 site
| 파일·함수 | 변경 |
|---|---|
| `internal/platform/metrics/metrics.go` | epic A·B·C metric 4종 + 등록 |
| `internal/platform/metrics/metrics_test.go` | metric 등록·label cardinality 테스트 |

#### 마이그레이션
- 0건.

### 6.5 단위 테스트 추가

epic 별 테스트 파일에 명시. 공통:
- `pool_test.go` 확장 — throttle·rateLimit·circuit 모두 nil(zero-value) 시 기존 동작 유지 (회귀 0).

### 6.6 통합 테스트 추가

`test/integration/sshd_e2e_test.go`에 epic별 phase 추가:
- **Phase 6 (epic C)**: 1 컨테이너 stop 후 circuit open → cool-down 후 half-open → 재시작 후 closed 회복 시나리오.
- **Phase 7 (epic B)**: 같은 tenant exec polling 1000 req/s 시 token bucket 차단 → 다른 tenant는 영향 0.
- **Phase 8 (epic A)**: 1 컨테이너 sshd 부하 의도적 한계 부여 → throttle이 limit 5 → 2로 감소 검증.

---

## 7. TDD Stage 분해 (옵션 B 채택 — epic 별 별도 PR)

각 epic은 독립적으로 `make ci`(vet + test + build) 통과. memory `feedback_go_commit_pipeline.md` 일관 — gofmt·import 그룹·errcheck 사전 통과.

### Stage C-1 (epic C — circuit breaker, 권장 우선순위 1, ~1.5일)

- `sshpool/circuit.go` — state machine + per-robot map. in-memory(option (a)) 채택.
- `sshpool/circuit_test.go` 단위 테스트 12건 내외 (state 전이 5건 + timeout 회복 + half-open 1회 시도 + 동시 robot 격리 + nil 호환).
- `scanrun.Deps`에 `CircuitBreaker` 인터페이스 추가 (P5 격리). `executeOne` 결선.
- bootstrap 결선 + default(timeout=5min, failureThreshold=5).
- audit emit `scan.circuit_open` / `scan.circuit_closed` 추가 (다음 세션이 audit verify schema에도 반영).
- `make ci` 통과.

### Stage C-2 (epic D 일부 — circuit state metric, 권장 우선순위 1과 동시, ~0.5일)

- `metrics.go`에 `SSHCircuitState` GaugeVec + `SSHCircuitTransition` CounterVec 추가.
- `circuit.go`가 state 변경 시 callback으로 metric 갱신.
- `metrics_test.go` 확장.
- `make ci` 통과.

### Stage B-1 (epic B — per-tenant rate limit, 권장 우선순위 2, ~1일)

- `golang.org/x/time/rate` import (이미 indirect dep 가능, `go mod tidy` 사전 확인).
- `sshpool/ratelimit.go` — `TenantRateLimiter` lazy map.
- 단위 테스트 8건 내외.
- `pool.go Acquire`에 결선 — RateLimit nil 허용 (회귀 0).
- bootstrap default(100 req/s, burst 50).
- `metrics.go`에 `SSHRateLimitTotal` 추가.
- `make ci` 통과.

### Stage A-1 (epic A — pool size 동적 조정, 권장 우선순위 3, ~1.5일)

- `sshpool/throttle.go` — sliding window(K=100) + AIMD.
- 단위 테스트 10건 내외.
- `pool.go` semaphore swap 메커니즘 — 가장 위험한 변경, 단위 테스트 + e2e 회귀 신중.
- bootstrap default(min=1, max=PerHostLimit=5).
- `metrics.go`에 `SSHPoolPerHostLimit` 추가.
- `make ci` + `make test-ssh-e2e` 모두 통과.

### Stage 분해 합계

4 commit / 4.5일 (옵션 B 권장, 우선순위 1 즉시 + 나머지 carryover). 본 doc은 Stage C-1·C-2만 즉시 진입 후보.

---

## 8. 결정 항목 (D-SCANEX-1 ~ D-SCANEX-6)

각 항목 권장 default 명시 — memory `feedback_design_doc_first.md` 일관. 다음 세션 즉시 진입 부담 0.

### D-SCANEX-1: 합성 전략 (epic 진입 시점·우선순위)

- **선택지**:
  - (1) 옵션 A — 4 epic 일괄 (5~7일).
  - (2) 옵션 B — 우선순위별 순차 (epic C → B → A → D 또는 customer trigger 시점에).
  - (3) 옵션 C — 전 보류 (코드 0).
  - (4) 옵션 D — epic C만 즉시 (~1.5일).
  - (5) 옵션 E — epic C + circuit metric(epic D 일부) 즉시 (~2일).
- **권장 default**: **(2) 옵션 B** — epic C(우선순위 1)는 첫 enterprise customer onboarding 직전 진입. 나머지는 trigger 시점.
- **결정 시점**: 본 doc 승인 시.

### D-SCANEX-2: rate limit 알고리즘 (epic B)

- **선택지**:
  - (1) `golang.org/x/time/rate.Limiter` (token bucket, 표준 std-adjacent).
  - (2) 자체 leaky bucket 구현 (queue depth 명시적).
  - (3) sliding window log (정확, 메모리 비용 큼).
- **권장 default**: **(1) `x/time/rate`** — 이유: (a) 검증된 표준, (b) dep 사실상 0(이미 indirect 가능), (c) Wait(ctx) API가 본 use case에 자연스러움, (d) per-tenant Limiter map만 추가하면 lazy init 가능.
- **결정 시점**: epic B Stage B-1 착수 전.

### D-SCANEX-3: rate limit threshold (epic B)

- **선택지**:
  - (1) per-tenant 100 req/s + burst 50 (default config).
  - (2) per-tenant 50 req/s + burst 25 (보수적).
  - (3) per-tenant 200 req/s + burst 100 (관대).
  - (4) customer 별 admin API로 override (Phase 6+ 별 epic).
- **권장 default**: **(1) 100 req/s + burst 50** — 이유: (a) PerTenantLimit=50 conn × 2 req/s/conn 가정, (b) 보수적이지만 일반 customer 충분, (c) admin override는 후속 epic으로 자연 확장. config flag로 노출(`--ssh-rate-limit-per-tenant=100`).
- **결정 시점**: epic B Stage B-1 착수 전.

### D-SCANEX-4: circuit breaker timeout·threshold (epic C)

- **선택지**:
  - (1) failureThreshold=5 + timeout=5min + half-open trial=1 (보수적).
  - (2) failureThreshold=3 + timeout=2min + half-open trial=1 (적극적 회복).
  - (3) failureThreshold=10 + timeout=15min + half-open trial=3 (관대).
  - (4) customer 별 admin API override (Phase 6+).
- **권장 default**: **(1) 5 / 5min / 1** — 이유: (a) Stage 5a HealthFailureThreshold=3보다 높여 Run-scope 외 영구 장애만 trigger, (b) 5min cool-down은 robot 점검·재부팅 일반 주기, (c) half-open 1회 시도가 OpenSSH session 저비용. config flag로 노출.
- **결정 시점**: epic C Stage C-1 착수 전.

### D-SCANEX-5: circuit state 저장 (epic C)

- **선택지**:
  - (a) **in-memory** (process scope) — Orchestrator 재시작 시 reset. 마이그레이션 0.
  - (b) **DB persistent** — `robot_circuit_state` 테이블 + tenant_id NOT NULL. multi-instance HA(E25 leader 변경)에서 state 보존.
  - (c) **Redis 또는 외부 캐시** — 분산 환경 친화. 새 dep.
- **권장 default**: **(a) in-memory** — 이유: (a) 마이그레이션 0, (b) Orchestrator 재시작은 드물고 재시작 시 어차피 robot 재 dial 시도가 자연스러움, (c) E25 multi-instance HA 결선 시점에 (b)로 마이그레이션 가능. (b)는 별 epic.
- **결정 시점**: epic C Stage C-1 착수 전.

### D-SCANEX-6: pool dynamics 측정 metric 단위 (epic A)

- **선택지**:
  - (1) sliding window K=100회 (per-host).
  - (2) sliding window K=50회 (반응 빠름, noise 위험).
  - (3) time-window 60s (시간 기반).
  - (4) EWMA(exponential weighted moving average) — smoothing.
- **권장 default**: **(1) K=100회 sliding window** — 이유: (a) 일반 fleet ScanSession 한 cycle = 수~수십 exec/host, K=100은 약 5~10 cycle 평균, (b) noise 흡수 + 반응 충분, (c) 구현 단순.
- **결정 시점**: epic A Stage A-1 착수 전.

### D-SCANEX-추가 (참고 — 본 doc 결정 후 미정)

본 doc 범위 외이지만 후속 epic에서 결정 필요:
- per-(tenant, robot) rate limit (epic B 확장) — cardinality 측정 후 결정.
- per-host circuit breaker (multiple robot이 같은 host 공유 — 드문 케이스).
- admin API override for limits·timeouts — Phase 6+ enterprise feature.
- OpenTelemetry tracing (D-SCAN-7과 일관, 별 phase).

---

## 9. 회귀 위험 / 운영 고려

### 9.1 기존 Pool 결선 영향

- **현재**: Stage 5b 결선된 `sshpool.NewPool(IdleTimeout=5min, KeepaliveInterval=30s, PerHostLimit=5, PerTenantLimit=50)`.
- **본 doc 영향**:
  - epic A: `pool.go Acquire`의 `hostSem` capacity가 동적 → semaphore swap 메커니즘 침투. 가장 위험. 단위 테스트 + e2e Phase 8 회귀 신중.
  - epic B: Acquire 직후 `rateLimiter.Wait(ctx, tenantID)` 추가 — RateLimit nil 시 no-op, 회귀 0.
  - epic C: `executeOne`에 `circuit.Allow(robotID)` 추가 — CircuitBreaker nil 시 no-op (인터페이스 default), 회귀 0.
  - epic D: 추가 metric 4종, 기존 5종 영향 0.
- **권고**: epic A는 e2e 회귀가 가장 큼 — 별 PR + 단위 테스트 충분 + sshd_e2e_test Phase 8 추가.

### 9.2 multi-tenant scaling

- **현재**: PerTenantLimit=50 conn semaphore + idle 풀(per-PoolKey LIFO). cross-tenant pool 재사용 0(idleKey에 TenantID 포함).
- **본 doc 영향**:
  - epic B: per-tenant Limiter map에 `tenant_id` key — 사용 후 GC 필요. tenant offboarding(Phase 6+) 시 entry cleanup 콜백 필요.
  - epic C: per-robot circuit map. tenant scope는 자연스럽게 robot_id가 cover (tenant 격리는 robot 도메인이 담당).
- **권고**: epic B Limiter map은 sync.Map + LRU eviction(예: 24h idle entry GC) 권장 — long-tail tenant 누적 메모리 회피.

### 9.3 customer 진입 시점 영향

- **시나리오**: 첫 enterprise customer가 영구 장애 robot 1대 가진 fleet으로 onboarding.
  - epic C 미결선: 첫 ScanSession에서 timeout × 3 worker 점유 → demo 인상 부정. **권장 epic C 진입 시점 = 첫 enterprise customer onboarding 직전**.
  - epic C 결선 후: 5회 fail 후 5min cool-down → 다음 Run에서도 skip → demo 깔끔.
- **시나리오**: 첫 SI 업체 hosting (multi-tenant).
  - epic B 미결선: customer A 폭주 → customer B latency 영향 → SLA 위반.
  - 권장 epic B 진입 시점 = 첫 SI hosting 진입 직전.
- **시나리오**: 3+ customer 누적 후 환경 다양성.
  - epic A 진입 시점 = customer 환경별 dial 실패율 차이 관찰 후.

### 9.4 audit chain 영향

- **본 doc은 audit chain 모델 변경 0**. 단, 새 audit event type 추가 권장:
  - epic C: `scan.circuit_open` / `scan.circuit_closed` / `scan.circuit_half_open_trial` (Stage C-1).
  - epic B: `scan.rate_limited` (per-tenant deny burst 시 1건 — 빈도 너무 높으면 bulk emit으로 throttle).
  - epic A: `scan.pool_throttle_decreased` / `scan.pool_throttle_increased`.
- **위험**: 새 event type이 audit chain replay·verifier에서 unknown 처리 가능 — `cmd/rosshield-audit-verify` schema 검증 사전 반영. Stage C-1·B-1·A-1 commit 각자 포함.

### 9.5 metric cardinality

- **위험**: `tenant`+`robot` 두 label은 fleet 크기 × tenant 수 곱으로 폭증.
- **권고**:
  - `SSHCircuitState{tenant, robot, state}` — 의미상 robot label 필수. fleet 크기 < 10000 robot/tenant 가정 시 OK.
  - `SSHRateLimitTotal{tenant, result}` — robot label 부재 (per-tenant만).
  - `SSHPoolPerHostLimit{tenant, host}` — host label은 dedup 필요(같은 host에 multiple robot이 있어도 한 entry).
- **상한**: tenant 수 < 100 + robot 수 < 10000/tenant 가정 시 안전. 그 이상이면 per-tenant aggregate metric으로 대체.

### 9.6 dev/prod 차이

| 항목 | dev (fakesshd) | prod (실 SSHD) |
|---|---|---|
| epic A 부하 측정 | dial latency µs | LAN ms · WAN s |
| epic B rate limit | Wait(ctx) 즉시 통과 | 100 req/s burst 50 trigger |
| epic C circuit | 5회 fail → open → 5min cool-down | 동일 |
| epic D metric | promhttp /metrics 정상 노출 | 동일 |

dev/prod gap 보호: Stage C-1·B-1·A-1 각자 docker compose e2e Phase 6·7·8 추가 (§6.6).

### 9.7 첫 enterprise customer 시 재검토 항목

- D-SCANEX-3 rate limit threshold — customer 실 부하로 100 → 200 또는 50 조정.
- D-SCANEX-4 circuit timeout — customer robot 점검 주기에 맞춰 5min → 10min 또는 2min.
- D-SCANEX-5 circuit state 저장 — E25 multi-instance HA 결선 시점에 (a) → (b) DB persistent 마이그레이션.
- per-(tenant, robot) rate limit 필요 여부 — 한 tenant 내 특정 robot polling 폭주 시.
- admin API override (Phase 6+ enterprise feature).

---

## 10. 참조

### 10.1 관련 design doc

- `docs/design/notes/scanrun-ssh-integration-design.md` — 직전 epic. Stage 1~5c 마감 (head `76ae2f0`). 본 doc은 그 baseline 위에서 후속.
- `docs/design/notes/e6-ssh-scan-deepdive.md` — E6 사전 리서치 R4-1~R4-7. 본 doc Pool 결선 base.
- `docs/design/notes/e6-stage-d-orchestrator-research.md` — orchestrator pitfall(circuit breaker·timeout·sudo·batch wait). 본 doc epic C가 §5.6 패턴 확장.
- `docs/design/notes/e25-ha-design.md` — Phase 5 carryover design doc 표준 형식. 본 doc § 구조 본보기.
- `docs/design/notes/e22-f-pg-native-design.md` — 옵션 비교 + Stage 분해 형식 본보기.

### 10.2 관련 코드

- `internal/platform/sshpool/pool.go` — Pool (변경 site §6, epic A·B·C 모두 침투).
- `internal/platform/sshpool/sshpool.go` — Executor (변경 0 권장, 회귀 회피).
- `internal/app/scanrun/scanrun.go` — Orchestrator `executeOne` (변경 site §6, epic C 결선).
- `cmd/rosshield-server/bootstrap.go` — Pool 결선 site (Stage 5b head, 본 doc epic 별 결선 추가).
- `internal/platform/metrics/metrics.go` — Stage 4 SSH metric 5종 (epic D 4종 추가 site).
- `test/integration/sshd_e2e_test.go` — Stage 5c 5 Phase (epic A·B·C 별 Phase 6·7·8 추가).

### 10.3 설계서 섹션

- `§01.4 principles.md` — 멀티테넌시 기본값. 본 doc epic B·C 격리 강제 근거.
- `§01.7 principles.md` — 단일 바이너리, 다중 껍질. 본 doc 모든 epic dep 추가 0(또는 std-adjacent만).
- `§01.9 principles.md` — 데이터 불변성. 본 doc audit event 추가 시 append-only 강제.
- `§06 security-and-tenancy.md` — multi-tenant scaling. 본 doc epic A·B 격리 정합.
- `§07.2·§07.7 scan-engine-and-benchmarks.md` — 스캔 엔진 결정론. 본 doc circuit breaker가 OutcomeSkipped(reason) 결정론 보존.
- `§10 audit-and-observability.md` — audit chain·observability. 본 doc §9.4·9.5.

### 10.4 메모리 패턴

- `feedback_design_doc_first.md` — 본 doc 자체. 결정 항목 6건 모두 권장 default 명시. 다음 세션 즉시 진입 부담 0.
- `feedback_design_doc_conservative.md` — paying customer 0 단계 ROI 보수 평가. 옵션 B 우선순위별 순차 권장.
- `feedback_go_commit_pipeline.md` — Stage C-1·B-1·A-1 모두 gofmt·import group·errcheck 사전 통과.
- `feedback_parallel_agents.md` — epic 별 분리 PR — Stage C-1·B-1은 의존성 분리 가능 (sshpool 다른 파일). epic A는 pool.go 핫 path 침투 — 단독 진행.

### 10.5 phase5-backlog 카드 매핑

- 본 doc은 Phase 5 scanrun 3 epic 마감 직후 후속 가치 카탈로그.
- 본 doc 종료 후 다음 후보:
  - epic C 즉시 진입 (옵션 D·E 사용자 선택 시) — 별 design doc 또는 본 doc Stage C-1·C-2 직접 진입.
  - 첫 enterprise customer onboarding 직전 epic C 진입 (옵션 B 권장 default).
  - 첫 SI hosting 진입 직전 epic B 진입.
  - customer 환경 다양성 누적 후 epic A 진입.

---

## 11. 본 doc 결정 후 다음 세션 진입 절차

1. SESSION_HANDOFF "현재 상태 한 줄" 갱신 — "scanrun 3 epic 마감 직후 후속 design doc 작성 — D-SCANEX-1~6 결정 대기".
2. SESSION_HANDOFF "다음 후보" 갱신 — 본 doc 4 epic + 우선순위(C → B → A → D).
3. 사용자 결정: D-SCANEX-1 (합성 전략) 권장 default = 옵션 B(우선순위별 순차) 수용 또는 옵션 D·E(epic C 즉시) 선택.
4. 옵션 D·E 선택 시 — Stage C-1·C-2 즉시 진입. TDD red → green → commit.
5. 옵션 B 선택 시 — 본 doc만 두고 다른 Phase 5 카드(예: ROS2 baseline pack design, 첫 customer onboarding runbook)로 이동. epic C는 customer trigger 시점에 진입.
