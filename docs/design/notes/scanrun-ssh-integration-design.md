# Scanrun SSH 통합 설계 — 실 ROS2 로봇 결선 (Phase 5 carryover)

> **상태**: Design draft (Phase 5 carryover, D-SCAN 결정 대기)
> **작성일**: 2026-05-15
> **범위**: `internal/app/scanrun/` Orchestrator + `internal/platform/sshpool/` + `cmd/rosshield-server/scanexec.go`. 현재 fakesshd 기반 in-proc 통합을 실 ROS2 로봇(또는 그 등가 환경)을 향한 production-quality SSH 결선으로 확장.
> **참조**: `e6-ssh-scan-deepdive.md`, `e6-stage-d-orchestrator-research.md`, `§07.2`·`§07.7` 스캔 엔진, `§06.8` 명령 실행 안전, `§00` 미션, `phase5-backlog.md`.
> **비목표**: 새 transport(WebSocket·gRPC) 도입, ROS2 DDS 직접 통신, 로봇 측 daemon 자체 제작·배포 인프라(D-SCAN-3 옵션 C 채택 시 별 doc), 자체 hardware 전제, agent framework화(CAI 영토).
> **코드 변경**: 0건 / 마이그레이션 0건. 본 문서는 docs only — 실제 구현은 D-SCAN 결정 후 별도 PR(stage 분해 §6 참조).

---

## 1. 상태·배경

### 1.1 본 doc 위치

본 doc은 Phase 5 carryover 입니다. SESSION_HANDOFF "현재 상태 한 줄"의 RBAC Stage 2-E 직후 다음 후보 항목 — **"scanrun deep dive / ROS2 실 SSH 통합 — Phase 2 carryover. 가장 큰 미해결 미지(MVP 가치 핵심). 며칠 ~ 1주"** 에 해당합니다. memory feedback `feedback_design_doc_first.md` 일관 — 1일+ 작업은 코드 진입 전 design doc 작성. 본 doc 자체는 **코드 0줄 / 마이그레이션 0건**.

### 1.2 왜 "deep dive"인가 — MVP 가치 핵심

`§00 mission-and-positioning`은 본 제품을 다음과 같이 정의합니다:

> "감사인이 받아들이는 결정론적 증거와 서명된 리포트를 생성하는 상용 B2B 제품."

이 미션을 충족하려면 결국 **실 ROS2 로봇에 SSH로 접속해 audit cmd를 실행하고, 그 stdout/stderr/exit code를 evidence로 보존하고, 평가 결과를 sealed AST evaluator로 결정해 audit chain에 hash로 묶어야** 합니다. 모든 결선이 통과해야 첫 paying customer에게 "당신 로봇 N대를 우리가 진짜로 스캔합니다"라고 말할 수 있습니다.

### 1.3 현재까지의 진행

Phase 1·2에서 다음을 cover 했습니다:

- **Stage A (E6)**: `sshpool.Executor` — `golang.org/x/crypto/ssh` 기반 dial→exec→close 표면 (`internal/platform/sshpool/sshpool.go`).
- **Stage B (E6)**: `sshpool.Pool` — per-host/per-tenant semaphore + dial backoff (`internal/platform/sshpool/pool.go`). 단, **dial-on-acquire / close-on-release** — idle 재사용 X.
- **Stage C (E6)**: `scan.Service` 도메인 격선 + sqliterepo (`internal/domain/scan/`).
- **Stage D (E6)**: `scanrun.Orchestrator` — robots × checks fan-out + worker pool + EventBus + RecordResult + finalize + Cancel (`internal/app/scanrun/scanrun.go`).
- **Stage D.2 (E6)**: bootstrap 결선 — `cmd/rosshield-server/scanexec.go`의 `sshExecutorAdapter` + `benchmarkEvaluatorAdapter`.

### 1.4 무엇이 시뮬레이션·placeholder로 남아 있는가

상기 결선은 모두 통합 테스트(`scanrun/integration_test.go`, `seed_e2e_integration_test.go`, `evidence_integration_test.go`)에서 **`sshpooltest.FakeSSHD` (in-proc fake SSH 서버)** 로만 검증되었습니다. 실 ROS2 로봇 또는 docker `linuxserver/openssh-server` 컨테이너를 향한 e2e는 부재합니다.

또한 다음 production gap이 남아 있습니다:

| Gap | 현재 상태 | 위치 |
|---|---|---|
| host key 검증 | `xssh.InsecureIgnoreHostKey()` placeholder + warning 로그 | `cmd/rosshield-server/bootstrap.go:1085` |
| Pool idle 재사용 | dial-on-acquire / close-on-release | `internal/platform/sshpool/pool.go` §2 doc |
| sudo / privilege escalation | 미지원 — `argv` 그대로 단일 user 권한 실행 | — |
| credential storage | KEK envelope으로 DB 저장 (E3) | OS keychain 미사용 |
| robot 측 ROS2 specific knowledge | 0 — pure POSIX SSH only | — |
| egress firewall 가정 | 없음 (운영자 책임) | — |
| concurrent ScanSession 격리 | sessionID 단위 mu만 — multi-tenant 동시 stress 미검증 | `scanrun.go:67` |
| 실 robot e2e | 없음 — fakesshd만 | `scanrun/*_test.go` |

본 doc은 위 gap의 **production-quality 결선 옵션**을 정리하고, 다음 세션이 즉시 코드에 진입할 수 있도록 D-SCAN 결정 항목과 stage 분해를 명시합니다.

---

## 2. 현재 상태 진단 — 코드 trace

### 2.1 Audit cmd가 evaluator에 도달하는 path

`StartScan` → `Orchestrator.Run` → `executeOne`의 한 cycle:

```
scanrun.Orchestrator.Run(ctx, tenantID, sessionID, robots, checks)   // scanrun.go:97
  ↓
  TransitionSession(running) + audit emit "scan.started"              // :104
  ↓
  for r in robots: for c in checks:
    sem.Acquire → goroutine → executeOne(ctx, r, c)                   // :128–143
      ↓
      Executor.Exec(ctx, robot, check.AuditCommand, timeout)           // scanrun.go:214
        ↓ (sshExecutorAdapter)
        storage.Tx → robot.Service.GetCredentialMaterial               // scanexec.go:54
        materialToAuthMethod (password | privateKey + passphrase)      // scanexec.go:96
        sshpool.Executor.Exec(ctx, sshpool.Target, argv, timeout)      // scanexec.go:77
          ↓ (executor)
          net.Dialer.DialContext(ctx, "tcp", host:port)                 // sshpool.go:143
          ssh.NewClientConn → ssh.NewClient                            // :148–153
          client.NewSession (1회용)                                     // :156
          session.Run(JoinArgv(argv)) in goroutine                     // :173–175
          select ctx.Done | done                                        // :177
            cancel  → session.Close + client.Close + return ctx.Err
            done    → assemble(stdout, stderr, exit, dur)
        ↑ scan.ExecResult로 변환
      ↓
      Evaluator.Evaluate(check.EvalRuleJSON, exec)                     // scanrun.go:220
        ↓ (benchmarkEvaluatorAdapter)
        benchmark.ParseEvalRule → EvalNode.Eval                        // scanexec.go:141
        EvalStatus → scan.Outcome 매핑                                 // scanexec.go:159
      ↓
      별 Tx (background ctx) → Evidence.Store(stdout, stderr) +
                                   RecordResult + LinkToResult          // scanrun.go:236–286
      ↓
      EventBus.Publish "scan.progress"                                  // :291
  ↓
  wg.Wait → finalize(runCtx.Err()) → terminal transition + completed   // :147
```

### 2.2 누락 부분 (precise)

| ID | 누락 | 영향 | 위치 |
|---|---|---|---|
| G1 | host key TOFU(first-touch) 또는 known_hosts 검증 | MITM 위험 | `bootstrap.go:1085` |
| G2 | Pool idle conn 재사용 | 매 check마다 dial+handshake (~수십~수백ms 추가 latency) | `pool.go` Stage B doc |
| G3 | sudo/privilege escalation argv wrapping | 권한 필요한 audit cmd 실행 불가 (e.g. `cat /etc/shadow`) | scan.CheckDef·sshpool.Exec 모두 |
| G4 | credential rotation·passphrase prompting in CLI | 운영자 패스프레이즈 입력 흐름 부재 | — |
| G5 | egress firewall 가이드 / outbound only path | enterprise customer 채택 장애 | docs/runbook 부재 |
| G6 | 실 docker compose e2e | 회귀 안전망 부재 | `Makefile` `make ci`에 e2e target 없음 |
| G7 | ROS2 robot용 audit cmd library | 현재 CIS Linux pack만 — ROS2 specific check 0 | `internal/builtin/packs/` |
| G8 | Pool stress / 부하 테스트 + pprof | goroutine·mem 누수 미검증 | E6 exit 기준 line 348 미충족 |
| G9 | per-host backoff·circuit breaker (offline robot fast skip) | 죽은 robot이 다른 worker 막음 | scanrun.go executeOne |
| G10 | Observability — per-check OpenTelemetry span | 운영자 디버깅 어려움 | metrics 패키지에 SSH metric 부재 |

### 2.3 도메인 경계 측면 — 본 doc이 깨면 안 되는 것

`scan` 도메인은 `robot`·`benchmark`·`sshpool`을 직접 import 하지 않음(P5 + depguard). 본 deep dive에서 새 transport·wrapper를 도입해도 **scanexec.go bootstrap adapter layer**에만 결선해야 합니다. `scan.SSHExecutor` interface는 그대로 유지.

---

## 3. 합성 전략 옵션 ≥3

본 design doc의 핵심 분기는 **"로봇과 어떤 수단으로 통신하는가"** 입니다. 4개 옵션을 비교합니다.

### 옵션 A — `golang.org/x/crypto/ssh` 직접 통합 강화 (현 path 발전)

현 코드 path를 그대로 두고 누락 부분(G1~G10)을 점진적으로 채웁니다.

**구체 작업**:
- G1: `internal/platform/sshpool/knownhosts.go` 신규 — `ssh/knownhosts` 서브패키지 사용. `<dataDir>/keys/known_hosts` 파일 + DB(`robot_host_keys` 테이블) 이중 기록. first-touch trust + 변경 즉시 실패.
- G2: `pool.go`에 `idleConns map[PoolKey][]*pooledClient` + IdleTimeout(5min) eviction. health check는 `client.SendRequest("keepalive@openssh.com", ...)`.
- G3: `scan.CheckDef`에 `RequiresSudo bool` 필드 추가 + `sshExecutorAdapter`가 `["sudo", "-n", "--"]` prefix wrapping. `-n` (non-interactive)로 prompt 차단 — passwordless sudo 또는 askpass agent 가정. CredentialMaterial에 `SudoPassword *string` 옵션 추가는 별도 D-SCAN 결정.
- G6: docker compose harness — `test/integration/docker-compose.ssh.yml` (linuxserver/openssh-server 3 컨테이너). `make test-ssh-e2e` target.
- G9: Orchestrator에 per-robot health window — 연속 실패 N회 시 잔여 check를 skip(reason="robot_offline"). E6 deepdive §5.6 패턴.
- G10: `internal/platform/metrics/`에 ssh exec 카운터·히스토그램 추가. exec_total{outcome}·exec_duration_ms·dial_total{result}.

**Pros**:
- 기존 코드 재사용 — 도메인 경계·결선 그대로.
- 단일 바이너리 유지(P7) — CGO 0, 추가 dep 0.
- agent forward·known_hosts 등 SSH 표준 기능 모두 활용 가능.
- SSH 자체가 사실상 모든 Linux 로봇에 기본 탑재 — customer 추가 설치 0.

**Cons**:
- robot 측 sudoers 설정·SSH key 배포는 운영자 책임 — onboarding cost가 customer side에 남음.
- ROS2 specific 정보(노드 목록·DDS 토픽 등)는 audit cmd argv로만 표현 — pack 의존.
- per-host 동시 5 conn × N robot 부하가 결국 서버 메모리·fd 한계.

**회귀 위험**: 낮음 (기존 path 발전).

**추정**: Stage 분해 §6 — **5~7일** (G1·G2·G3·G6·G9·G10).

### 옵션 B — 외부 ssh CLI exec 위임

본 프로세스가 `os/exec`로 시스템 `ssh` 바이너리를 호출. 운영자 `~/.ssh/config` + `~/.ssh/known_hosts` + ssh-agent를 그대로 활용.

**구체 작업**:
- `internal/platform/sshpool/`을 `internal/platform/sshcli/`로 대체 또는 추가 backend.
- argv: `ssh -o BatchMode=yes -o ConnectTimeout=10 -o StrictHostKeyChecking=accept-new <user>@<host> -- bash -c "<JoinArgv(argv)>"`.
- stdout/stderr/exit code는 `cmd.Output()` + `*exec.ExitError`.

**Pros**:
- 운영자 친숙성 100% — 기존 SSH config·jump host·multiplexing 설정이 그대로 작동.
- ControlMaster/ControlPersist로 connection multiplexing 무료.
- known_hosts·agent forward·IdentitiesOnly 등 모든 옵션 기본 지원.
- 코드 단순 — `golang.org/x/crypto/ssh` dep 제거 가능.

**Cons**:
- **단일 바이너리 원칙(P7) 위반** — system ssh client 의존. Tauri desktop·snap 어플라이언스에서 ssh 패키지 별도 보장 필요.
- Windows desktop에서 OpenSSH client 옵션 필요(Windows 10+ 기본 탑재이긴 함).
- credential 관리가 OS-level로 분산 — DB의 envelope-encrypted credential을 매 exec마다 임시 파일로 dump → 보안 표면 증가.
- 멀티테넌시: 한 호스트의 ssh-agent를 tenant마다 격리하기 어려움.
- 에어갭(P3)에서 dynamic system 의존성은 Pkg 생성 시 검증 필요.

**회귀 위험**: 매우 높음 — `sshpool` 패키지·sshExecutorAdapter·integration_test 모두 재작성. 단위 테스트 fakesshd → mock CLI로 전환 비용.

**추정**: 코드 폐기 비용 + 신규 = **8~12일**.

### 옵션 C — 에이전트 모델 (push vs pull 모두 분석)

로봇 측에 본 제품 전용 에이전트(`rosshield-agent`)를 배치하고 audit cmd를 그쪽에서 실행.

**C1 — SSH 기반 push**: 서버가 SSH로 에이전트 바이너리를 배포·호출(`ssh root@robot rosshield-agent run <pack-cid>`). 본질은 옵션 A와 같지만 로봇 측 logic이 풍부해짐.

**C2 — 로봇 측 daemon pull**: 에이전트가 systemd로 상시 동작 + 서버에 outbound HTTPS/WebSocket으로 접속 + 작업 polling.

**Pros (공통)**:
- audit cmd가 풍부한 로직(ROS2 introspection, DDS topic 검사, ros2 doctor 호출 등)을 가질 수 있음.
- 로봇 측 캐싱·증분 검사·offline-tolerant.
- C2: outbound only — egress firewall friendly. enterprise customer 환영.
- C2: 서버 IP 변경에 자동 적응(에이전트가 도메인 명으로 접속).

**Cons (공통)**:
- **새 도메인·새 인프라** — 에이전트 배포·서명·OTA·rollback·protocol versioning. Phase 5 일정에 못 들어감.
- D5 open-core 결정과 충돌 잠재 — 에이전트가 enterprise feature가 되어야 하는지 코어인지.
- C2: outbound channel = 새 보안 표면(token rotation·mTLS·rate limit·anti-replay).
- C2: scanrun fan-out 모델이 "서버가 통제" → "에이전트가 자율 polling"으로 inversion. Cancel·진행률·timeout 시멘틱 모두 재설계.
- 단일 바이너리(P7) 두 개로 분화 — 빌드·CI·release 파이프라인 복잡도 증가.

**회귀 위험**: 매우 높음 — `scanrun.Orchestrator` 모델 자체 재설계.

**추정**: **2주+** (별도 design doc 필수). 본 doc 범위 초과 → **별도 phase**.

### 옵션 D — 옵션 A + ROS2 specific cmd library 동시 (권장 하이브리드)

옵션 A를 채택하되, **builtin/packs**에 ROS2 specific check 팩(`ros2-baseline-1.0`)을 동시 추가. SSH transport는 옵션 A 그대로 유지하면서 audit cmd가 `ros2 node list`, `ros2 topic list -t`, `cat /opt/ros/*/setup.bash`, `systemctl status ros2-*` 등 ROS2 도메인 cmd로 채워짐. evaluator는 기존 sealed AST 그대로(stdout/stderr/exitCode만 입력).

**Pros**:
- **MVP 가치 = "ROS2 로봇 보안 감사" — 가장 빠른 path**. transport는 기본기, 진짜 가치는 check 정의에서 나옴.
- 기존 코드 재사용 100%. evaluator·orchestrator·storage·audit chain 변경 0.
- 코어/enterprise 분리 친화적 — pack은 D5 open-core에서 enterprise tier로 묶기 자연스러움.

**Cons**:
- ROS2 distribution 다양성(humble·iron·jazzy)에 대한 fixture/snapshot 부담.
- ROS2가 안 깔린 robot 환경(예: 로봇 OS만)에서 모든 check가 INDETERMINATE — UX 명확화 필요.

**회귀 위험**: 낮음 — pack 추가 + transport 누락 보강.

**추정**: 옵션 A (5~7일) + ROS2 pack 초기 30~50건 (3~5일) = **8~12일**.

---

## 4. 권장 옵션 + 근거

### 4.1 권장: **옵션 A + 옵션 D의 ROS2 pack은 별 phase로 분리**

본 doc(scanrun deep dive)은 **옵션 A 단독**을 마무리합니다. ROS2 specific pack(옵션 D의 후반부)은 본 doc 종료 후 별 design doc(`ros2-baseline-pack-design.md`)로 분리. 이유:

1. **MVP 가치는 transport 신뢰성에서 시작**. ROS2 pack 30건이 있어도 host key MITM·idle conn 누수가 있으면 감사인 수용 X.
2. **ROI 최대** — 본 doc 5~7일로 production-quality SSH path 완성. 그 위에 추후 pack을 누적.
3. **회귀 위험 최소** — 기존 코드 path 발전이라 도메인 경계·테스트 패턴 모두 유지.
4. **운영 단순도** — 추가 dep 0, 추가 인프라 0, customer 추가 설치 0(SSH는 모든 robot에 기본).
5. **D5 open-core 정합** — 옵션 A는 코어. 옵션 C(에이전트)는 enterprise tier 후보로 미루는 것이 자연스러움.
6. **메모리 `feedback_design_doc_conservative.md`** — 잠재 효과 보수 평가. 옵션 C는 "별 phase 2주+"로 분리해 본 doc 일정 압박 회피.

### 4.2 옵션 C(에이전트)를 *지금* 채택하지 않는 추가 이유

- `phase5-backlog.md`의 카드 추정 자체가 "며칠 ~ 1주". 옵션 C는 단독으로 그 한도를 초과.
- 첫 paying customer 후보 운영 환경 미확정 — egress firewall 강도, ROS2 distribution, sudo 정책 미지수. 그 정보 없이 에이전트 모델을 fix하는 것은 premature optimization.
- 옵션 A → 옵션 C 전환은 `scan.SSHExecutor` interface를 다른 backend로 swap만 하면 되므로 **rip-and-replace 비용이 낮음** — 지금 결정 deferred 가능.

### 4.3 옵션 B(외부 ssh CLI)를 채택하지 않는 추가 이유

- P7(단일 바이너리, 다중 껍질) 위반.
- 보안 표면 증가(temp credential file).
- 회귀 비용 가장 큼(기존 코드 폐기).
- multiplexing benefit은 G2(Pool idle 재사용)로 자체 구현해도 충분.

---

## 5. 변경 사항 outline (옵션 A 채택 시)

본 절은 다음 세션이 즉시 코드에 진입할 수 있는 정밀도로 기술합니다. memory `feedback_design_doc_first.md` 일관.

### 5.1 신규 파일

| 파일 | 책임 | 추정 LOC |
|---|---|---|
| `internal/platform/sshpool/knownhosts.go` | `KnownHostsManager` — 파일 backed + DB backed 이중 기록. `ssh.HostKeyCallback` 팩토리 함수 노출 | ~150 |
| `internal/platform/sshpool/knownhosts_test.go` | TOFU first-touch + 변경 시 실패 + 동시 multiple host 단위 테스트 | ~200 |
| `internal/domain/robot/host_key.go` | `RobotHostKey` 도메인 모델 + Service 메서드 (RecordFirstTouch, GetByRobotID, ResetTrust). `tenant_id` 컬럼 필수(원칙 4) | ~120 |
| `internal/domain/robot/sqliterepo/host_key_repo.go` | sqlite 어댑터 + UNIQUE(tenant_id, robot_id, fingerprint_sha256) | ~100 |
| `migrations/sqlite/00NN_robot_host_keys.up.sql` + `.down.sql` | `robot_host_keys` 테이블 + index + audit trigger 미부착(append-only는 도메인에서) | ~30 |
| `migrations/postgres/00NN_robot_host_keys.up.sql` + `.down.sql` | 동일 (PG 방언) | ~30 |
| `test/integration/docker-compose.ssh.yml` | linuxserver/openssh-server 3 컨테이너 + 사전 password/key 시드 | ~50 |
| `test/integration/sshd_e2e_test.go` (build tag `integration`) | 3 컨테이너 × CIS check 2~3건 e2e + race + pprof 스냅샷 | ~300 |
| `Makefile` 수정 | `test-ssh-e2e` target 추가 | +5 |
| `internal/platform/sshpool/idle_pool.go` (또는 pool.go 확장) | `idleConns map[PoolKey][]*pooledClient` + IdleTimeout eviction + keepalive | ~200 |
| `internal/platform/metrics/sshpool_metrics.go` | exec_total{outcome}·exec_duration_ms·dial_total{result}·idle_conns_gauge | ~80 |

### 5.2 수정 site

| 파일·함수 | 변경 |
|---|---|
| `internal/platform/sshpool/sshpool.go` `Executor.Exec` | sudo wrapping 옵션 추가 — `Target`에 `SudoMode SudoMode` 필드(`SudoNone`·`SudoNonInteractive`·`SudoPassword`) |
| `internal/platform/sshpool/pool.go` `pool.Acquire` | dial-on-acquire → idle 풀에서 재사용 시도 후 fallback dial. release 시 idle 큐로 반납 |
| `internal/domain/scan/scan.go` `CheckDef` | `RequiresSudo bool` + `SudoTimeout time.Duration` 필드 추가. 기존 zero-value 호환(false=no sudo) |
| `internal/app/scanrun/scanrun.go` `Orchestrator.executeOne` | per-robot health window 추가 — `sync.Map[robotID]healthState{ConsecutiveFailures, LastSuccess}`. N회 연속 실패 시 잔여 check skip(reason="robot_offline") |
| `cmd/rosshield-server/scanexec.go` `sshExecutorAdapter.Exec` | `Target.SudoMode` 매핑 + `KnownHostsManager.HostKeyCallback(ctx, robotID, tenantID)` 주입 |
| `cmd/rosshield-server/bootstrap.go` ~line 1075 | `xssh.InsecureIgnoreHostKey()` 제거 + `sshpool.NewKnownHostsManager(cfg.DataDir, robotSvc)` 주입 + warning 로그 삭제 |
| `internal/builtin/packs/checks/cis-*.yaml` 일부 | `requiresSudo: true` 메타 추가 (예: `/etc/shadow` 검사). pack converter도 그 필드 pass-through |

### 5.3 단위 테스트 추가

- `sshpool/knownhosts_test.go` — 첫 접속 trust → 2번째 동일 fingerprint pass → 3번째 다른 fingerprint reject → reset 후 재 trust. fixture: 두 개의 ed25519 키 쌍 사전 생성.
- `sshpool/pool_test.go` 확장 — idle 재사용 hit/miss carrier + IdleTimeout eviction + keepalive failure 시 즉시 close.
- `sshpool/sshpool_test.go` 확장 — sudo wrapping argv 검증(stdin·argv 모두 mock SSHD가 record).
- `scanrun/scanrun_test.go` 확장 — health window: 연속 N회 fail 후 잔여 check가 OutcomeSkipped(reason="robot_offline") 발생.
- `robot/host_key_test.go` — RecordFirstTouch idempotency + ResetTrust 후 재 trust + tenant 격리.

### 5.4 통합 테스트 추가

- `test/integration/sshd_e2e_test.go` (build tag `integration`):
  - **Phase 1 — single robot**: 1 컨테이너 × CIS check 3건 → 모두 PASS, evidence 저장 검증, audit chain 연속성 검증.
  - **Phase 2 — fleet of 3**: 3 컨테이너 × CIS check 5건 = 15 work item. `WorkerLimit=10`으로 진행률 검증.
  - **Phase 3 — degraded fleet**: 1 컨테이너를 중간에 stop → health window 발동 → 잔여 check skip → session.Status=completed (failed가 아닌 — partial OK는 reason 기록).
  - **Phase 4 — host key change**: 컨테이너 키 교체 후 재 scan → 즉시 OutcomeError(reason="host key mismatch"). reset 후 재시도 PASS.
  - **Phase 5 — pprof**: `runtime.NumGoroutine()` 시작·종료 시점 비교(누수 0). heap profile 스냅샷 — manual.

### 5.5 마이그레이션

- `00NN_robot_host_keys.up.sql` — sqlite + PG 양쪽. 컬럼: `id ULID PK, tenant_id NOT NULL, robot_id NOT NULL, fingerprint_sha256 NOT NULL, key_type, key_blob, first_seen_at, last_verified_at, trust_state ENUM(trusted|revoked)`. UNIQUE `(tenant_id, robot_id, fingerprint_sha256)`. INDEX `(tenant_id, robot_id)`.
- migration ordinal은 다음 빈 번호(현재 head 이후) — 실제 commit 시점에 결정.
- backward-compat: 기존 robot 행은 row 0으로 시작 — 첫 scan 시 first-touch trust로 자동 채워짐. 운영자 이주 비용 0.

---

## 6. TDD Stage 분해 (5 commit)

각 stage는 독립적으로 `make ci` (vet + test + build)가 통과해야 합니다. memory `feedback_go_commit_pipeline.md` 일관 — gofmt·import 그룹·errcheck 사전 통과.

### Stage 1 — `robot_host_keys` 도메인 + 마이그레이션 (1일)

- `robot.RobotHostKey` 모델 + Service 인터페이스(`RecordFirstTouch`·`GetTrustedKey`·`ResetTrust`).
- sqlite + PG 마이그레이션.
- sqliterepo 어댑터 + 단위 테스트 10건 내외(tenant 격리, idempotent first-touch, fingerprint UNIQUE 중복 거부).
- audit emit 추가: `robot.host_key.first_touched`·`robot.host_key.changed`·`robot.host_key.reset`.

**검증**: `make test` 통과. 본 stage는 외부 결선 0 — 도메인만.

### Stage 2 — `KnownHostsManager` + sshpool host key callback (1일)

- `internal/platform/sshpool/knownhosts.go` — `KnownHostsManager` 구조체. 생성자가 `robot.Service`(host_key 부분)와 `dataDir` 받음.
- `(m) HostKeyCallback(ctx, tenantID, robotID) ssh.HostKeyCallback` 메서드 — closure로 ctx + ID 캡처.
- 첫 호출 = TOFU(`RecordFirstTouch`) + 파일 append. 2번째 = trusted과 비교, 일치하면 pass / 불일치 즉시 error(설계서 §06.8 정합).
- 단위 테스트: in-proc fakesshd 두 개(서로 다른 key)로 first-touch → 일치 → 불일치 시나리오.

**검증**: `make test` 통과. 본 stage 종료 후 bootstrap은 아직 placeholder 사용.

### Stage 3 — bootstrap 결선 + sudo 옵션 (1일)

- `cmd/rosshield-server/bootstrap.go`에서 `xssh.InsecureIgnoreHostKey()` → `khMgr.HostKeyCallback(...)` 교체.
- `scan.CheckDef`에 `RequiresSudo bool` + `SudoTimeout` 필드 추가. 기본값 false.
- `sshpool.Target`에 `SudoMode` 추가. `SudoNonInteractive` 시 `JoinArgv(append([]string{"sudo", "-n", "--"}, argv...))`.
- `sshExecutorAdapter`가 `CheckDef.RequiresSudo`를 `Target.SudoMode`로 매핑.
- 단위 테스트 — sudo argv 직렬화·non-interactive prompt 차단 검증.

**검증**: `make ci` 통과. 기존 fakesshd e2e가 그대로 PASS — 회귀 0(sudo 미설정 check는 zero-value).

### Stage 4 — Pool idle 재사용 + metrics (1.5일)

- `pool.go`에 `idleConns` map + IdleTimeout(5min) eviction + keepalive(30s).
- `Acquire`: idle 풀 try → miss 시 dial. `release`: 오류 없으면 idle 큐로 반환, 오류 시 close.
- `ssh exec_total`·`exec_duration_ms`·`dial_total`·`idle_conns_gauge` Prometheus carrier.
- 단위 테스트: idle hit/miss + eviction + keepalive 실패 시 close + metric count 검증.

**검증**: `make test-race` 통과 (Linux/WSL). Windows는 `make test`로 대체.

### Stage 5 — docker compose e2e + per-robot health window (2일)

- `test/integration/docker-compose.ssh.yml` + Makefile target.
- `sshd_e2e_test.go` 5 phase (5.4 기재).
- `scanrun.go executeOne` per-robot health window — 연속 N(default 3)회 실패 시 잔여 check `OutcomeSkipped(reason="robot_offline")` 즉시 emit.
- 단위 테스트 — health window 카운터·reset on success.
- Stage 5 commit이 본 doc cover의 마지막 — Phase 5 carryover 종료 인정.

**검증**: `make test` + `make test-ssh-e2e`(별 명령) 둘 다 통과. CI 분리 — `test-ssh-e2e`는 docker 가용 환경에서만.

### Stage 분해 합계

5 commit / 6.5일 — `phase5-backlog.md` "며칠 ~ 1주" 추정 부합. 보수적 잡기 — 7일 목표 + 1일 완충.

---

## 7. 결정 항목 (D-SCAN-1 ~ D-SCAN-7)

각 항목 권장 default 명시 — memory `feedback_design_doc_first.md` 일관. 다음 세션 즉시 진입 부담 0.

### D-SCAN-1: 합성 전략 (transport 모델)

- **선택지**: A(`x/crypto/ssh` 발전) / B(외부 ssh CLI) / C(에이전트) / D(A + ROS2 pack 동시).
- **권장 default**: **옵션 A** (본 doc 5~7일). 옵션 D의 ROS2 pack 부분은 별 design doc(`ros2-baseline-pack-design.md`)으로 분리. 옵션 C는 첫 enterprise customer egress 정책 확인 후 재논의.
- **결정 시점**: 본 doc 승인 직후.

### D-SCAN-2: host key 검증 정책

- **선택지**:
  - (1) TOFU(first-touch trust) + 변경 즉시 실패 + 운영자가 명시적 reset 필요.
  - (2) strict — 운영자가 사전에 known_hosts 파일·DB에 채워야 첫 scan 가능.
  - (3) accept-new (OpenSSH 7.6+ 옵션) — 처음 보는 호스트는 자동 추가, 변경은 거부.
- **권장 default**: **(1) TOFU**. 이유: (a) Phase 5 운영 단순도 우선, (b) e6 deepdive R4-2 이미 합의, (c) 변경 시 즉시 실패는 (3)과 동일 안전성.
- **결정 시점**: Stage 2 착수 전.

### D-SCAN-3: sudo / privilege escalation

- **선택지**:
  - (1) `sudo -n`(non-interactive)만 지원. passwordless sudo는 운영자 책임. password sudo는 OutcomeError.
  - (2) `sudo -S`로 stdin password 전달. CredentialMaterial에 `SudoPassword *string` 옵션 추가.
  - (3) sudo 미지원 — root SSH key 운영 권고.
  - (4) ssh-agent 기반 askpass — 복잡도 최상.
- **권장 default**: **(1) `sudo -n`**. 이유: (a) password 메모리 보존 회피(보안), (b) enterprise customer 대부분 ansible/ssh key 기반 passwordless sudo 정책 보유, (c) 향후 (2) 추가 cost 낮음.
- **결정 시점**: Stage 3 착수 전.

### D-SCAN-4: timeout 계층

- **선택지**:
  - (1) 현행 — `CheckDef.TimeoutSec`(check별) + `Deps.CheckTimeoutDefaultSec`(global default) + sshpool `DialTimeout`(10s 고정). 추가 layer 없음.
  - (2) `Pool.IdleTimeout` + per-host `KeepAliveInterval`도 config로 노출.
  - (3) Orchestrator 전체 wallclock budget — `ScanSession.MaxDurationSec` 추가.
- **권장 default**: **(1) + (2) 동시 채택**. (3)은 별 phase. 이유: (1)·(2)는 본 doc 자연 포함, (3)은 ScanSession 모델 변경 비용 큼.
- **결정 시점**: Stage 4 착수 전.

### D-SCAN-5: error handling — robot offline 시 정책

- **선택지**:
  - (1) 현행 — 매 check마다 dial → 실패 시 OutcomeError, 다음 check도 dial 재시도.
  - (2) per-robot health window — 연속 N(default 3) 실패 시 잔여 check 자동 skip(reason="robot_offline"). 다음 ScanSession에서 reset.
  - (3) 별 health probe job — Orchestrator 시작 전 per-robot ssh handshake 1회로 사전 분류.
- **권장 default**: **(2)**. N=3 + 실패 정의=`dial 또는 handshake error`(exec error는 제외 — 실 cmd 결과). e6-stage-d-orchestrator-research.md §5.6 패턴 일관.
- **결정 시점**: Stage 5 착수 전.

### D-SCAN-6: 동시성 — 한 fleet 내 ScanSession 병렬

- **선택지**:
  - (1) 현행 — `ErrFleetActiveScanExists`로 같은 fleet에 동시 active 1개만 허용.
  - (2) 허용 + sshpool semaphore가 부하 자체 제한.
  - (3) 허용 but per-robot mutex로 같은 robot에 동시 두 cmd 차단.
- **권장 default**: **(1) 유지**. 이유: (a) audit chain 가독성·UX, (b) 동시 실행은 reporting·comparison 측 모델 변경 비용 큼, (c) Phase 5 범위 최소화.
- **결정 시점**: 본 doc 승인 시 (변경 없음 = no-op).

### D-SCAN-7: observability — metric·tracing 범위

- **선택지**:
  - (1) Prometheus carrier만 — exec_total·exec_duration_ms·dial_total·idle_conns·offline_robots 카운터.
  - (2) (1) + OpenTelemetry span — 각 check exec를 span으로, parent=ScanSession.
  - (3) (1) + structured slog만 — 외부 tracing 인프라 불필요.
- **권장 default**: **(1) + (3)**. OpenTelemetry는 별 phase. 이유: (a) 본 doc은 transport 자체 신뢰가 우선, (b) tracing은 cross-cutting 으로 별도 도메인 결정, (c) slog는 이미 코드베이스 표준.
- **결정 시점**: Stage 4 착수 전.

---

## 8. 회귀 위험 / 운영 고려

### 8.1 secrets storage

- **현재**: KEK envelope encryption(E3) — DB의 `robot_credentials.material_blob`이 KEK로 봉인. KEK는 HSM(또는 file)·passphrase로 보호.
- **본 doc 영향**: 추가 변경 0. material unwrap은 매 Exec마다 별 Tx로 발생(`scanexec.go:54`) — Orchestrator·Pool·Executor 어디에도 평문 자격증명이 영구 저장되지 않음.
- **운영 권고**: KEK rotation runbook 별도. `robot_host_keys` 테이블의 fingerprint는 평문 SHA-256 — encryption 불필요.
- **위험**: 새 `robot_host_keys` 테이블이 multi-tenant 격리 위반 시 cross-tenant host key leak. → tenant_id NOT NULL + 모든 query에서 tenant scope 강제(원칙 4) + sqliterepo unit test에서 cross-tenant 격리 검증 필수.

### 8.2 robot 측 sudo 권한

- **현재**: 0(미지원).
- **본 doc 영향**: D-SCAN-3 결정에 따라 옵션 (1) `sudo -n` 추가.
- **운영 권고**: customer onboarding 문서에 "rosshield 서비스 계정에 passwordless sudo 권한을 어떤 cmd에 한정해 부여할지" runbook 필요. sudoers fragment 예시 제공: `rosshield ALL=(ALL) NOPASSWD: /usr/bin/cat /etc/shadow, /usr/bin/systemctl status *`.
- **위험**: 너무 넓은 sudo 권한 부여 시 본 제품이 lateral movement 표면이 됨. CIS pack 자체에 "rosshield 서비스 계정 sudoers 검사" check 추가 권고(Phase 5+ 후속).

### 8.3 network egress

- **현재**: outbound 가정만(robot으로 SSH 22 또는 사용자 지정 port).
- **본 doc 영향**: 변화 없음 — outbound only model 유지.
- **운영 권고**: customer firewall 측 inbound rule 0 — 본 제품은 robot side에 inbound 22만 필요. OpenSSH bastion/jump host 시나리오는 옵션 A로 자연 지원(ssh.Client.Dial → ssh.NewClient 체인).
- **위험**: 일부 customer는 robot이 management network에서 outbound 0 정책 — 옵션 C(pull 모델)가 필요. 그 경우 본 doc 종료 후 옵션 C design doc 별도.

### 8.4 audit chain 영향

- **본 doc은 audit chain 모델 변경 0**. 단, 새 audit event type 4건 추가:
  - `robot.host_key.first_touched` (Stage 1)
  - `robot.host_key.changed` (Stage 2 — 차단 발생 시)
  - `robot.host_key.reset` (Stage 1 — 운영자 명시 reset)
  - `scan.robot_offline_skipped` (Stage 5 — health window 발동 시 robot 단위 1건)
- **위험**: 새 event type이 audit chain replay·verifier에서 unknown으로 처리될 가능성 — `cmd/rosshield-audit-verify`의 schema 검증 부분에 추가 필수. Stage 1 commit에 포함.

### 8.5 observability

- D-SCAN-7 (1)+(3) 채택 시 — Prometheus + slog. 신규 metric 5건은 `internal/platform/metrics/sshpool_metrics.go` 신규 파일에 격리. /metrics 엔드포인트는 자동 노출.
- structured log fields: `tenant_id`·`session_id`·`robot_id`·`check_id`·`exec_duration_ms`·`exit_code`·`outcome`·`reason`. PII는 stdout/stderr — log에 직접 출력 금지(요약만), evidence 테이블에서 redact 적용.

### 8.6 dev/prod 차이

| 항목 | dev (fakesshd) | prod (실 SSHD) |
|---|---|---|
| host key | 매 테스트마다 ed25519 새로 생성 | TOFU 후 영구 — operator reset 필요 |
| auth | NoClientAuth | password 또는 publicKey + (선택) sudo |
| latency | µs 단위 | 수십~수백 ms (LAN), 수초 (WAN) |
| timeout | 단위 테스트 1s 충분 | default 10s + per-check override 필수 |
| concurrency | in-proc — Goroutine 만이 제약 | per-host 5 + per-tenant 50 + customer firewall ratelimit |
| audit cmd 종류 | "echo X" 로만 | shell builtin·systemctl·ros2 doctor 등 |

dev/prod gap 보호: Stage 5 docker compose e2e가 prod-shaped(실 sshd + 실 password auth + 실 sudo)로 회귀 안전망.

### 8.7 첫 enterprise customer 시 재검토 항목

- D-SCAN-1 옵션 C 재논의 — egress firewall 정책 확정 후.
- D-SCAN-3 옵션 (2) 또는 (4) — sudoers 정책에 password 필요 시.
- D-SCAN-7 옵션 (2) — 운영자 OpenTelemetry collector 가용 시.
- robot OS 다양성 (Ubuntu Core·Debian·RHEL·SUSE) — pack 호환성 별 design doc.

---

## 9. 참조

### 9.1 관련 design doc

- `docs/design/notes/e6-ssh-scan-deepdive.md` — E6 사전 리서치(R4-1~R4-7). 본 doc은 그 결정 위에서 발전.
- `docs/design/notes/e6-stage-d-orchestrator-research.md` — nrobotcheck → fleetguard mapping. 본 doc §5.5 pitfall(circuit breaker·timeout·sudo·batch wait)을 D-SCAN-5·D-SCAN-3에 반영.
- `docs/design/notes/e25-ha-design.md` — Phase 5 carryover design doc 표준 형식 본보기.
- `docs/design/notes/e22-f-pg-native-design.md` — 옵션 비교 + Stage 분해 형식 본보기.

### 9.2 관련 코드

- `internal/app/scanrun/scanrun.go` — Orchestrator (변경 site 5.2, Stage 5).
- `internal/platform/sshpool/sshpool.go` — Executor (변경 site 5.2, Stage 3).
- `internal/platform/sshpool/pool.go` — Pool (변경 site 5.2, Stage 4).
- `internal/platform/sshpool/sshpooltest/fakesshd.go` — in-proc fake SSH 서버. Stage 2 단위 테스트 재사용.
- `cmd/rosshield-server/scanexec.go` — bootstrap adapter (변경 site 5.2, Stage 3).
- `cmd/rosshield-server/bootstrap.go:1075` — host key placeholder (Stage 3 교체 site).
- `internal/domain/scan/scan.go` — `CheckDef`·`SSHExecutor` interface (변경 site 5.2, Stage 3).
- `internal/domain/robot/` — `RobotHostKey` 도메인 신규 (Stage 1).

### 9.3 설계서 섹션

- `§00 mission-and-positioning.md` — MVP 가치 정의. 본 doc 1.2.
- `§01 principles.md` — P5(도메인 경계), P7(단일 바이너리), P9(불변성), P10(프라이버시 기본값). 본 doc 4.1·5.5·8.1.
- `§03.3 architecture.md` — 분리 모드·계층. 옵션 C 평가 시 재참조.
- `§06.8 security-and-tenancy.md` — 명령 실행 안전(쉘 메타·argv·timeout·max bytes). 본 doc Stage 3 sudo wrapping이 정합.
- `§07.2·§07.7 scan-engine-and-benchmarks.md` — 스캔 엔진·결정론. 본 doc 4 권장 옵션 근거.
- `§10 audit-and-observability.md` — audit chain·observability. 본 doc 8.4·8.5.

### 9.4 메모리 패턴

- `feedback_design_doc_first.md` — 본 doc 자체가 그 패턴. 결정 항목 7건 모두 권장 default 명시.
- `feedback_design_doc_conservative.md` — 추정 보수(7일 + 1일 완충). 옵션 C 별 phase 분리.
- `feedback_go_commit_pipeline.md` — Stage 5 commit 모두 gofmt·import group·errcheck 사전 통과.
- `feedback_parallel_agents.md` — Stage 1·Stage 2는 의존성 분리 가능, sub-agent 병렬 가능 (Stage 1 = robot 도메인, Stage 2 = sshpool — 서로 독립).

### 9.5 phase5-backlog 카드 매핑

- 본 doc은 SESSION_HANDOFF "다음 후보 — scanrun deep dive / ROS2 실 SSH 통합 — Phase 2 carryover. 가장 큰 미해결 미지(MVP 가치 핵심). 며칠 ~ 1주" 항목.
- 본 doc 종료 후 후속 phase: `ros2-baseline-pack-design.md` (옵션 D 후반부 — ROS2 specific check 30~50건).
- 본 doc 종료 후 별도 deferred: 옵션 C 에이전트 design (첫 enterprise customer egress 정책 확정 후).

---

## 10. 본 doc 결정 후 다음 세션 진입 절차

1. SESSION_HANDOFF 업데이트 — 본 doc 위치 + D-SCAN-1~7 권장 default 표기.
2. 사용자 승인: D-SCAN-1~7 권장 default 7건을 1번 "전체 수용" 또는 개별 변경.
3. Stage 1 진입 — `robot_host_keys` 도메인 + 마이그레이션. TDD red → green → commit.
4. Stage 2~5 순차 진행. 각 stage 종료 시 `make ci` + commit + SESSION_HANDOFF 갱신 한 줄.
5. Stage 5 완료 후 별 design doc `ros2-baseline-pack-design.md` 진입 또는 다른 Phase 5 카드로 이동.
