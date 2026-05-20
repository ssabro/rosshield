# E35-refresh — snap refresh rollback 재설계

> **상태**: Research + 권장 — 코드 변경 0건. D-E35R-1 ~ D-E35R-3 결정 대기.
> **작성일**: 2026-05-21
> **범위**: 현재 `post-refresh` hook 기반 자동 rollback 설계의 **architectural mismatch** 진단 + 옵션 비교 + 권장 진입 경로.
> **선행**: `docs/operations/snap-deployment.md` §7 (현 설계), `.github/workflows/snap-smoke.yml` line 177~199 (CI carryover step), `snap/hooks/post-refresh` (현 hook).
> **비목표**: multipass nested VM 기반 실 multi-revision OTA broken simulation(사용자 hands-on 위임). snap layout/wrapper script 도입(별 epic).
> **코드 변경**: 0건. 본 문서는 docs only — D-E35R-1~3 결정 후 별 PR(Stage 1~4).

---

## 1. 상태 / 배경

### 1.1 현 설계 (v0.8.3 시점)

`snap/hooks/post-refresh` (45줄 shell script):
1. `snapctl get healthz-url` (default `http://127.0.0.1:8080/healthz`)
2. `snapctl get healthz-timeout` (default 60s, CI에서 120s로 확장)
3. deadline까지 2s 간격으로 `curl --max-time 5 $HEALTHZ_URL | grep "status":"ok"` 폴링
4. OK 받으면 exit 0, timeout 시 exit 1
5. `set -e` 활성

`snap/snapcraft.yaml`:
- hooks.post-refresh.plugs = [network] (curl localhost 가능)
- configure hook도 자동 인식 (healthz-url·healthz-timeout·backup-* 옵션 검증)

`.github/workflows/snap-smoke.yml` line 177~199:
- "snap refresh — same-snap re-install (round-trip simulation)" step
- `continue-on-error: true`로 carryover 트래킹 (job conclusion 영향 0)

설계 의도(`docs/operations/snap-deployment.md` §7):
> snap 표준 rollback 활용 — post-refresh hook exit 1 시 snapd가 자동으로 이전 revision으로 복원. broken refresh 시 운영자 수동 개입 불필요.

### 1.2 실제 CI 동작 (failure mode)

GH Actions ubuntu-22.04 환경 `Snap Smoke Test` workflow `26192573917`(2026-05-20 22:02 UTC):

```
22:06:35  rev before refresh: x1
22:06:35  sudo snap install "$SNAP_FILE" --dangerous   (refresh trigger)
22:08:39  error: cannot perform the following tasks:
22:08:39  - Run post-refresh hook of "rosshield" snap (run hook "post-refresh": exit status 1)
22:08:39  ##[error]Process completed with exit code 1.
22:08:41  rosshield removed (cleanup)
```

소요 시간: **2분 4초** — healthz-timeout=120s 정확 만료 후 hook exit 1 → snapd 자동 revert.

**문제 진단**:
- 첫 install + healthz wait (외부 CI step에서 60s polling)은 정상 통과
- **refresh round-trip만** hook timeout 발생
- 120s 안에 새 revision의 `/healthz` ok 응답 한 번도 못 받음

### 1.3 근본 원인 — snap hook lifecycle 가정 오류

snap 공식 docs + snapcraft.io forum(thread #37238 "Post-refresh hook needs services") 확정:

> The post-refresh hook is executed for the newly installed revision of the snap, **before starting new services** (if applicable).

snap refresh 시 hook 호출 sequence:
1. `pre-refresh` hook 실행 (이전 revision의 services 여전히 active)
2. 이전 revision services 정지
3. 새 revision unpack
4. **`post-refresh` hook 실행** (services 모두 stopped 상태)
5. 새 revision services 시작 (이 단계에서 daemon이 처음 bind/listen 시작)

**현재 `post-refresh` hook이 healthz polling을 하는 것은 architecturally 불가능**:
- hook 실행 시점에 새 daemon이 아직 시작 안 됨 (sequence 4 < 5)
- healthz가 응답할 수 있는 시나리오 = 이전 revision의 daemon이 8080을 잡고 있는 경우뿐인데, sequence 2에서 이미 stopped
- 따라서 120s polling은 항상 timeout 후 exit 1

자동 revert는 작동 중 (snapd가 hook fail을 정확히 감지하고 이전 revision으로 복원). 다만 **"healthz 검증 → 통과 → revert 안 함" 경로가 절대 닿을 수 없음** — 모든 refresh가 자동 revert로 귀결되는 broken 설계.

### 1.4 carryover 트래킹 (현재 commit `85b6bb1`)

`.github/workflows/snap-smoke.yml` line 172~178 코멘트:
> E35-refresh carryover (2026-05-21): GH Actions ubuntu-22.04 환경에서 snap refresh 시 새 revision의 서비스가 healthz-timeout=120s 안에 ok 못 받는 사례 누적 ... snap layout/wrapper script 도입 시 재검증 예정.

그러나 **본 문서에서 사실 확정** — snap layout/wrapper script로도 해결 불가 (post-refresh가 daemon start 전 호출되는 lifecycle 자체가 문제). 다른 hook 또는 외부 단계 분리가 필요.

---

## 2. snap hook lifecycle 정리 (fact-check 확정)

| Hook | 호출 시점 | exit 1 동작 | 자동 revert 트리거 |
|---|---|---|---|
| `install` | 첫 install 시, services start **전** | install 실패 → snap 미설치 | n/a (첫 install) |
| `pre-refresh` | refresh 시, 이전 services **active** 상태 | refresh 중단 → 이전 revision 유지 | ✅ 트리거 (refresh 자체가 실패) |
| `post-refresh` | refresh 시, services **모두 stopped** + 새 revision unpack 직후 | refresh 실패 → 이전 revision으로 자동 복원 | ✅ 트리거 |
| `configure` | `snap set` 호출 시 | snap set 거부 (값 미반영) | n/a |
| `check-health` | refresh/install/revert 완료 후 + 주기적 (~5분) | exit status 무시 — `snapctl set-health` 호출 결과만 활용 | ❌ unhealthy 표시만, revert 안 함 |
| `check-health` (hard timeout) | 위와 동일 | **snapd 30s hard timeout 강제** — 초과 시 install/refresh 자체 fail | (timeout 시 hook 종료 + install/refresh abort) |
| `install-device` | (Ubuntu Core gadget 전용) | 미사용 | n/a |

**핵심 발견**:
- 자동 revert 가능한 hook은 `pre-refresh` / `post-refresh` 둘 뿐 — 그러나 둘 다 **새 daemon이 active 되기 전**에 호출됨
- daemon health 검증 적합 hook은 `check-health` — 그러나 **자동 revert 안 함** (`snap list`에 "unhealthy" 표시만)
- snapd ≥ 2.41에서 `check-health` 지원 (Ubuntu 18.04 default 이상 모두 cover)

**snapd standard 정책 인용** (forum thread #10605):
> Snaps can provide a check-health hook that can be used by developers to signal to the system and the user that something is not well with the snap. ... The check-health hook is expected to use snapctl to inform snapd about the health of the snap.

→ **자동 revert + healthz polling을 동시에 만족하는 hook은 snap 표준에 존재 안 함**.

---

## 3. 옵션 비교

### 옵션 A — post-refresh 단순화 + check-health hook 도입 (권장)

**설계**:
- `post-refresh` hook: 새 revision 파일 무결성 + configure 값 sanity check만 (예: `bin/rosshield-server` 존재 + `snapctl get healthz-url`이 잘 정의된 URL). healthz polling 제거. exit 0 default.
- `check-health` hook 신규: snapd가 services start 후 호출. healthz polling + `snapctl set-health` 호출로 status 표시.
- `configure` hook: 변경 없음 (옵션 검증 유지).

**자동 revert**: 포기. customer 환경에서 운영자가 `snap revert rosshield` 수동 호출.

**보완 (auto-revert 대체)**:
- check-health unhealthy 상태가 N분 이상 지속 시 Prometheus alert → 운영자 호출 (Grafana dashboard `deploy/grafana/rosshield-dashboard.json` 갱신)
- `docs/operations/snap-deployment.md` §7 갱신: "broken refresh 발견 시 절차" 단계별 가이드 (1. `snap health rosshield` 확인 2. `snap revert rosshield` 3. 원인 추적)
- channel staged rollout 권장 (edge → candidate → stable, 옵션 D 부분 통합)

**장점**:
- snap 표준 hook 정상 활용 (architectural mismatch 해소)
- CI snap-smoke의 refresh round-trip step이 정상 통과 (continue-on-error 제거 가능)
- check-health unhealthy 상태가 운영자에게 명시적 signal → 자동 revert보다 안전 (false positive 방지)

**단점**:
- 자동 revert 메커니즘 상실 — customer 환경에서 broken refresh 시 운영자 수동 개입 필요 (RTO 증가)
- check-health hook은 snapd docs상 "work in progress" 표기 (forum thread #10605) — 향후 호출 정책 변경 가능

**노력**: 0.5~1일 (hook 2개 + CI workflow + docs)

---

### 옵션 B — post-refresh hook 완전 제거 + 외부 모니터링 위임

**설계**:
- `post-refresh` hook 삭제 (file 자체 제거).
- daemon health 검증은 외부 monitor 전담 (Prometheus + Grafana dashboard, `deploy/grafana/rosshield-dashboard.json`의 health panel)
- `configure` hook은 유지.

**자동 revert**: 완전 포기. broken refresh 시 외부 monitor가 unhealthy 감지 → 운영자 호출 → 수동 revert.

**장점**:
- 가장 단순 (hook 코드 감소)
- snap lifecycle 비표준 활용 0
- check-health "work in progress" 의존 0

**단점**:
- snap 자체의 health visibility 약화 (`snap list`/`snap info`에 status 표시 안 됨)
- 운영자 환경에서 Prometheus 미배포 site는 detection 자체가 없음
- design 의도 ("snap 표준 rollback 활용")에서 가장 큰 후퇴

**노력**: 0.25~0.5일 (hook 삭제 + docs)

---

### 옵션 C — channel staged rollout 운영 가이드 (자동 revert 대체 표면)

**설계**:
- snap channels 3-tier 도입: `edge` (직 build) → `candidate` (smoke test 통과) → `stable` (운영자 승급)
- 운영자는 `stable` channel만 추적
- `candidate` channel에서 N일 burn-in 후 issue 0건이면 stable로 manually 승급
- post-refresh hook은 옵션 A처럼 단순화

**자동 revert**: 운영자 환경에서 불필요 (stable에 broken revision 도달 안 함).

**장점**:
- snap store 공식 운영 패턴 일관 (Canonical 권장)
- broken revision이 customer 환경에 닿기 전 차단
- "snap revert" 운영 절차 필요 빈도 감소

**단점**:
- snap store 등록 필요 (현재 `snap install --dangerous`로 dev distribution) — 별 epic 트리거
- customer 환경에서 3 channel 관리 부담
- single-snap dev/PoC 환경에서는 oversized

**노력**: 2~3일 (snap store 등록 + CI release pipeline 분리 + docs)

---

### 옵션 D — systemd Watchdog + sd_notify 통합 (over-engineering 우려)

**설계**:
- `cmd/rosshield-server`에 `WatchdogSec`/`Type=notify` 추가
- snap services가 사용하는 systemd unit에 watchdog 옵션 wire
- daemon이 unhealthy 시 systemd가 자동 restart, N회 실패 시 service inactive → check-health unhealthy 표시

**자동 revert**: 시도 안 함. systemd 자체의 restart loop이 보완.

**장점**:
- daemon resilience 향상 (transient crash 자동 복구)
- check-health hook과 직교 (병용 가능)

**단점**:
- snap apps의 systemd unit override는 strict confined에서 제한적
- 본 문서 범위(refresh rollback) 초과 — 별 epic이 적절
- 자동 revert 본질 문제 해결 안 됨

**노력**: 1~2일 + 위험 큼 (snap strict confinement 환경에서 systemd Watchdog 동작 검증 필요)

---

### 옵션 비교 표

| 항목 | A: post-refresh + check-health | B: hook 제거 | C: channel rollout | D: Watchdog |
|---|---|---|---|---|
| 자동 revert | ❌ 포기 | ❌ 포기 | ✅ stable 도달 차단 | ❌ 포기 |
| snap 표준 정합 | ✅ 표준 hook 활용 | ⚠️ hook 미활용 | ✅ snap store 표준 | ⚠️ systemd override |
| CI smoke 통과 | ✅ continue-on-error 제거 가능 | ✅ refresh step 자체 단순화 | ✅ candidate channel smoke | ✅ 부분 |
| 운영 부담 | 보통 (검증 + alert) | 큼 (외부 monitor 의존) | 큼 (3 channel 관리) | 큼 (systemd 검증) |
| 노력 | 0.5~1일 | 0.25~0.5일 | 2~3일 | 1~2일 |
| 위험 | check-health WIP 의존 | snap visibility 약화 | snap store 등록 의존 | snap strict + systemd 충돌 가능 |

---

## 4. 권장 진입 경로

**권장 default = 옵션 A (post-refresh 단순화 + check-health 도입)** + **운영 docs에 옵션 C 부분 통합**.

근거:
- snap 표준 hook lifecycle 정합 (가장 큰 가치)
- 0.5~1일로 가장 작은 commit footprint
- 자동 revert 포기 단점은 옵션 C 운영 절차(channel staged rollout 권장)로 보완 — 단 channel 운영 자체는 customer 환경마다 선택 가능 (single channel customer는 외부 monitor 의존)
- check-health WIP 의존은 minor risk (snapd 표준 이미 5년+ 안정, deprecation 사례 없음)
- 옵션 B는 snap visibility 손실 큼 — `snap list rosshield`에 health status 안 나오는 것은 customer 운영 경험 후퇴
- 옵션 D는 separate concern (transient crash resilience) — 별 epic 적합

**대안**: 옵션 B는 운영자가 ROI 낮다고 판단 시 backup option.

---

## 5. Stage 분해 (옵션 A 채택 가정)

### Stage 1 — `post-refresh` hook 단순화 (0.25일)

**산출**:
- `snap/hooks/post-refresh` rewrite — 새 revision 파일 무결성 check만 (예: `[ -x "$SNAP/bin/rosshield-server" ]`) + configure 값 sanity (예: healthz-url이 http(s):// prefix). healthz polling 코드 완전 제거. exit 0 default.
- `set -e` 유지. 무결성/sanity 실패 시 exit 1 → snapd 자동 revert는 정상 동작 (binary corruption 등 catastrophic case만 cover).

**검증**:
- 현지 `snap install --dangerous` + `snap install --dangerous` 두 번 → revert 안 됨 + healthz는 별도 polling으로 정상 확인
- `bash -n snap/hooks/post-refresh` syntax OK

### Stage 2 — `check-health` hook 신규 (0.25일)

**산출**:
- `snap/hooks/check-health` 신규 작성 — snapctl get healthz-url + healthz-timeout 활용. 짧은 polling (max 30s, 2s 간격) 후 응답 OK면 `snapctl set-health okay`, 실패면 `snapctl set-health waiting "healthz timeout — daemon not ready"`.
- hook 호출은 idempotent + 빠름(< 30s) — snapd docs 권장 일관.
- `snap/snapcraft.yaml` hooks 섹션에 `check-health.plugs = [network]` 추가.

**검증**:
- `snap health rosshield` 명령으로 status 직접 확인 가능
- daemon 정지 상태에서 `snap run --hook=check-health rosshield` 호출 → "waiting" 출력

### Stage 3 — CI workflow 갱신 (0.25일)

**산출**:
- `.github/workflows/snap-smoke.yml` 갱신:
  - "snap refresh — same-snap re-install (round-trip simulation)" step에서 `continue-on-error: true` 제거
  - refresh 후 `sudo snap health rosshield` 호출 + 30s polling으로 "okay" 도달 확인 + `/healthz` 200 검증
  - post-refresh hook 직접 invocation 단계 단순화 (healthz polling 코드 제거)
- carryover 코멘트 (line 172~178) 갱신: redesign 완료 + 정상 round-trip 검증.

**검증**:
- workflow_dispatch로 snap-smoke 수동 trigger → 7/7 step 모두 PASS + refresh round-trip 정상 통과

### Stage 4 — 운영 docs 갱신 (0.25일)

**산출**:
- `docs/operations/snap-deployment.md` §7 rewrite:
  - 현 "snap 표준 rollback 활용" 가정 제거
  - 자동 revert는 binary 무결성 catastrophic case만 cover 명시
  - daemon health unhealthy 감지 → 운영자 절차 (snap health → snap revert) 단계별
  - **옵션 C 부분 통합**: channel staged rollout 권장 (운영자 선택, edge/candidate/stable 3-tier 사용 가이드)
- `docs/operations/snap-deployment.md` §9 한계 갱신: "post-refresh hook은 daemon health 검증 불가 (snap lifecycle 표준 제약)" 명시.
- `CHANGELOG.md` + 릴리스 노트 항목 추가.

**검증**: docs syntax PASS.

---

## 6. 결정 항목 (D-E35R-1 ~ D-E35R-3)

### D-E35R-1: hook 처리 방식

| 옵션 | 설명 | 권장 |
|---|---|---|
| A | post-refresh 단순화 (파일 무결성 only) + check-health 신규 (healthz polling) | ✅ |
| B | post-refresh 완전 제거 + 외부 모니터 위임 | (옵션 A 단점 받아들이기 어려운 경우) |

### D-E35R-2: 자동 revert 메커니즘

| 옵션 | 설명 | 권장 |
|---|---|---|
| 포기 | snap revert 수동 호출 + 모니터 alert로 위임 | ✅ |
| channel rollout | snap store edge→candidate→stable 3-tier 운영 (옵션 C) | (별 epic, snap store 등록 후) |

### D-E35R-3: check-health polling 동작

| 옵션 | 설명 | 권장 |
|---|---|---|
| 짧은 polling | 최대 30s polling 후 status 확정 | ✅ |
| 단일 shot | 1회 curl만 호출 후 status 확정 | (snapd 주기 호출이 5분이라 catch up 가능) |

---

## 7. 리스크

| 리스크 | 발생 빈도 | 영향 | 대응 |
|---|---|---|---|
| check-health hook이 snapd "work in progress" 표기 — 향후 API 변경 | 낮음 (5년+ 안정) | hook rewrite 필요 | snapd CHANGELOG 모니터링 + 별 epic 트리거 |
| 자동 revert 상실로 customer 환경 broken refresh 운영 부담 | 보통 (정상 release cadence 가정 1회/분기) | RTO 증가 | docs §7 절차 + Prometheus alert + channel rollout 권장 |
| `snapctl set-health waiting`이 `okay`로 transition 안 되는 race | 낮음 (daemon start 30s 이내 가정) | unhealthy 일시 표시 | check-health 주기 호출 (5분)이 자동 catch up |
| `snap health` 출력 형식 변경으로 CI workflow 회귀 | 낮음 | smoke test fail | CI에서 `--unicode=never` + format-agnostic grep 사용 |

---

## 8. 비목표

- multipass nested VM 기반 실 multi-revision OTA broken simulation — 사용자 hands-on 위임 (E36 carryover)
- snap layout / wrapper script 도입 — 본 문서 범위 외 (별 epic이 필요할 경우)
- snap store 등록 + channel rollout 완전 구현 — 옵션 C는 docs 권장만, 실제 채택은 별 epic (paying customer 진입 시점)
- systemd Watchdog 통합 — 별 concern (옵션 D)
- snap base core22 → core24 마이그레이션 — R40-1 결정으로 core22 유지

---

## 9. 참조

- `snap/hooks/post-refresh` (현 hook, 본 문서로 redesign 대상)
- `snap/snapcraft.yaml` hooks 섹션
- `.github/workflows/snap-smoke.yml` line 167~199 (carryover 트래킹 step)
- `docs/operations/snap-deployment.md` §7 (현 자동 rollback 가정), §9 한계
- snapcraft.io forum thread #37238 "Post-refresh hook needs services"
- snapcraft.io forum thread #10605 "Health Checks" (check-health hook 명세)
- snap 공식 docs `https://snapcraft.io/docs/reference/development/supported-snap-hooks/`
- Phase 5 backlog §E35 (원 설계)
- `deploy/grafana/rosshield-dashboard.json` (health visualization)
