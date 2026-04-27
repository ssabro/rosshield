# 세션 핸드오프

> **목적**: Claude Code 새 세션(재설치·다른 머신·오래 만에 재개)이 이 리포에서 바로 작업을 이어갈 수 있게 하는 지속 문서. git에 커밋되므로 로컬 `~/.claude/` 상태와 무관하게 유지된다.
>
> **Claude에게**: 이 문서를 먼저 읽고, 사용자에게 "## 진행 중 선택지" 섹션을 제시해라.

_마지막 업데이트: 2026-04-27 (E6 Stage D.2 완료 — bootstrap 결선 + Platform.ScanRun + 어댑터 단위 10)_

---

## 현재 상태 한 줄

**E6 Stage D.2 완료 — bootstrap 결선 + Platform.ScanRun + 어댑터 단위 10 tests.** scan 도메인은 외부 도메인을 import하지 않으므로 (P5) 결선은 cmd/* 책임 — `cmd/rosshield-server/scanexec.go` 신규: (a) `sshExecutorAdapter` (scan.SSHExecutor → sshpool.Executor + 매 Exec마다 별도 Tx로 robot.Service.GetCredentialMaterial unwrap → ssh.AuthMethod 변환 → sshpool.Target 구성) + (b) `benchmarkEvaluatorAdapter` (scan.CheckEvaluator → benchmark.ParseEvalRule + EvalNode.Eval, 3-값 EvalStatus → scan.Outcome 매핑). bootstrap에서 sshpool.New + 어댑터 + scanrun.New 결선 → `Platform.ScanRun *scanrun.Orchestrator`. host key callback은 임시 `ssh.InsecureIgnoreHostKey()` + warning 로그(R4-2 first-touch trust는 후속 stage). scanrun에서 worker ctx에 storage.WithTenantID 적용(어댑터가 Tx 시작 시 tenant 필요). 신규 dep 0(sshpool·benchmark는 이미 존재). **신규 10 tests** (`cmd/rosshield-server/scanexec_test.go`): mapEvalStatus 5-값 매핑·materialToAuthMethod password/empty/privateKey(ed25519 PEM)/empty/invalid/unknown type 7건·benchmarkEvaluatorAdapter pass/fail/empty rule/invalid JSON 4건. bootstrap_test에 ScanRun nil-check 추가. 누적 ~327+ tests, 전체 그린. **다음: E6 Stage D.3** — exit 검증 (3대 fleet × CIS 2~3 check end-to-end + race + memory/goroutine 누수 점검) + sshpool fakesshd 패키지 export(또는 sshpool 내 통합 테스트)로 T7·T8 통합. scan 도메인에 결합 표면 추가 — `RobotTarget`·`CheckDef`·`ExecResult`·`EvalResult` minimal struct + `SSHExecutor`·`CheckEvaluator` interface + `ProgressEventPayload`·`CompletedEventPayload` (P5 도메인 격리 유지). application layer 신규 패키지 `internal/app/scanrun/` — `Orchestrator.Run(ctx, tenantID, sessionID, robots, checks)` + `Cancel(ctx, tenantID, sessionID, reason)`. 흐름: pending→running 전이(audit `scan.started`) → semaphore(default 10·R4-4) + robots×checks fan-out → 각 worker `SSHExecutor.Exec`+`CheckEvaluator.Evaluate`+`scan.RecordResult`+EventBus `scan.progress` publish → wg.Wait → terminal 전이(`scan.completed`/`failed`/`cancelled`) + EventBus `scan.completed` publish. R4-5 시멘틱: Cancel 시 새 work item만 skip하고 진행 중 worker는 timeout까지 완료 대기(외부 `outer:` label로 acquire 실패 break). 결과 기록은 background ctx + 5s timeout으로 ctx cancel 영향 회피. 신규 dep 0(`golang.org/x/sync` indirect→direct만). **신규 7 tests**: T5(`TestRunFanOutProducesResultPerRobotCheck` 3×4=12 결과·progress 정확)·T6(`TestRunRespectsWorkerLimit` peak ≤ 3 atomic CAS)·T9(`TestRunCancelSkipsRemainingButWaitsInFlight` 50ms 후 Cancel·status=cancelled·totalCalls<20)·T10(`TestRunPublishesProgressAndCompleted` 6 progress + 1 completed 이벤트 inproc subscribe)·EmptyInput·MixedOutcomes(pass/fail/error 분포·Failed=2)·SSHError(`OutcomeError`+EvalReason 포함). 누적 ~317+ tests, 전체 그린. 백그라운드 agent 노트 `e6-stage-d-orchestrator-research.md` 작성됨(nrobotcheck ScanEngine·SSH·이벤트·테스트 패턴 + 함정 7건). **다음: E6 Stage D.2** — bootstrap 결선 (sshpool.Executor 어댑터 + E4 evaluator 어댑터 + `Platform.ScanRun`) + in-proc fakesshd 통합 테스트(T7). 새 도메인 `internal/domain/scan/`(tenant·robot 패키지 답습). 마이그레이션 0011 — `scan_sessions`(FSM CHECK + trigger CHECK + 진행률 3 컬럼 + (tenant,fleet,created) + (tenant,status,created) 인덱스), `scan_results`(별도 ID `scr_<ULID>` + composite UNIQUE(session,robot,check) + outcome 5-값 CHECK). 모델 `ScanSession.TransitionTo(target, now)` — FSM 검증·StartedAt/CompletedAt 자동 설정·원본 불변(P9). `pending → running·failed·cancelled` / `running → completed·failed·cancelled` / terminal 거부. `(*Repo).TransitionSession`이 audit 매핑(`scan.started`·`completed`·`failed`·`cancelled`). `RecordResult`는 running 강제 + UNIQUE dedupe + progress.Completed/Failed 원자 갱신. system pack(tenant_id='system') 접근 허용. **신규 scan 도메인 단위 6 tests** (FSM valid 6·invalid 10·preserve started·terminal·outcome·trigger) **+ sqliterepo 통합 16 tests** (StartScan·validate 4·default trigger·transition·invalid·cancel from pending+running·terminal cancel·RecordResult progress·duplicate·non-running·validate 4·list filter·status filter·notfound·system pack) **+ cross-tenant fuzzer 8 sub-cases** (Get·Transition·Cancel·Record·StartScan(fleetA·packA)·List 격리). bootstrap 결선 + auditEmitterAdapter 4 메서드 추가 + Platform.Scan + bootstrap_test nil-check. sshpool flake 1건 fix(`Duration < 0` 음수만 거부 — Windows 시계 분해능). 누적 ~310+ tests, 전체 그린. **다음: E6 Stage D** — Orchestrator(SSH 결선·worker pool 10·robot×check fan-out·E4 Evaluator 결선·EventBus 진행률·R4-5 cancel 시멘틱).

## 원격 저장소

- **URL**: `https://github.com/ssabro/rosshield` (PRIVATE, D6에 따라 Phase 1 exit 후 public 전환 예정)
- **계정**: `ssabro` (gh auth keyring에 병존; `nchecker-nsr`도 있으나 inactive)
- **브랜치**: `main` 단일, origin 추적
- **CI**: `.github/workflows/ci.yml` — `ubuntu-latest` × Go 1.26, `actions/checkout@v5` + `actions/setup-go@v6` + `golangci/golangci-lint-action@v8` (golangci-lint v2 설치). 약 1~2분 내 완료.
- **남은 경고**: `golangci-lint-action@v8`이 Node 20 내부 사용 — GitHub가 2026-06-02 Node 24 강제 전환 예정. 그때 upstream 대응이나 액션 추가 상향. 현재는 blocking 아님.

## 이 리포의 기원

2026-04-23, `D:\robot\dev\nrobotcheck`(Electron 데스크톱 앱, v2.0 DDD 리팩토링 중)에서 상업화 전략 검토 결과:

- 기존 리포를 점진 진화시키는 경로와
- **처음부터 새 코드베이스**로 재출발하는 경로 두 가지를 비교한 뒤,
- 상업화(온프렘·어플라이언스·멀티테넌시) 요구가 현 구조와 너무 많이 충돌한다는 결론으로
- 본 리포를 **후계 프로젝트**로 분리 개설.

상세 배경: `D:\robot\dev\nrobotcheck\docs\COMMERCIALIZATION_STRATEGY.md`

## 사용자 선호 (승계)

- **응답 언어: 한국어**
- **문체**: "-합니다" 체, 요점 우선
- **탐색적 질문**: 2~3문장 추천 + 트레이드오프, 즉시 실행 금지
- **선택지**: 숫자(1,2,3) 또는 A/B
- **커밋·푸시**: 로컬 커밋은 각 Phase 완료 시 OK. **remote push는 사용자 명시 요청 시에만**.

## 작업 컨벤션 (엄수)

1. **Trunk-based**: 피처 브랜치 없음. `main`에 직접 커밋·푸시.
2. **TDD**: 테스트 먼저 → 실패 → 구현 → 통과.
3. **커밋 전 파이프라인 녹색**: typecheck ✅ / 테스트 ✅ / 린트 0 errors ✅.
4. **커밋 메시지**: `<type>(<scope>): <한글 제목>` (상세는 `CLAUDE.md`).
5. **Co-Author 라인 붙이지 않음**.
6. **파일 ≤ 400/800줄, 함수 ≤ 50줄**.
7. **도메인 경계**: 다른 도메인 저장소 직접 호출 금지 (이벤트 또는 Application Service 경유).
8. **불변성**: append-only, 새 객체 리턴.

## 리포 구조

디스크 경로는 `D:\robot\dev\fleetguard`(리네이밍 안 함). Go 모듈·코드 네임스페이스는 `rosshield`.

```
fleetguard/                         # 디스크 폴더명 (Go 모듈과 무관)
├── CLAUDE.md                       # Claude 지침
├── SESSION_HANDOFF.md              # 이 문서
├── README.md                       # 프로젝트 랜딩
├── CONTRIBUTING.md                 # 기여 가이드
├── CHANGELOG.md                    # 변경 로그
├── LICENSE                         # Apache-2.0 (rosshield Contributors)
├── Makefile                        # build/test/vet/fmt/tidy/lint/ci/clean/openapi
├── go.mod                          # module github.com/ssabro/rosshield, go 1.26
├── .gitignore / .editorconfig / .golangci.yml (v2)
├── .github/workflows/ci.yml        # Actions CI (Go 1.26)
├── cmd/
│   └── rosshield-server/           # main.go + main_test.go (/healthz 스텁)
├── openapi/
│   └── openapi.yaml                # OpenAPI 3.1 v0.0.1 스켈레톤
├── contrib/
│   └── source-benchmarks/          # nrobotcheck 원본 자료 포인터 (파일 미복사)
├── bin/                            # 빌드 산출물 (gitignored)
└── docs/
    └── design/                     # 설계 문서 13종 + phase1-backlog.md
        ├── README.md
        ├── 00-mission-and-positioning.md
        ├── ...
        ├── 12-migration-and-non-goals.md
        └── phase1-backlog.md       # 실행 백로그 (Part VII)
```

## 결정 필요 항목 (Phase 0 Exit 조건)

| # | 항목 | 결정 | 참조 | 상태 |
|---|---|---|---|---|
| D1 | 제품명·도메인·상표 | 코드네임 `rosshield` 확정, 제품 브랜드는 `<ProductName>` placeholder → Phase 1 후반 확정 | `docs/design/00-*` | 🟡 연기 (코드네임은 ✅) |
| D2 | 백엔드 언어 | **Go** (백엔드) + **TypeScript** (프론트) | `docs/design/11-*` §11.2 | ✅ |
| D3 | 데스크톱 셸 | **Tauri 2.x** (Electron fallback 보류) | `docs/design/11-*` §11.8 | ✅ |
| D4 | 어플라이언스 OS | 보류, 기본 가정 Ubuntu Core 24, Phase 3 exit 재확정 | `docs/design/11-*` §11.9 | 🟡 연기 |
| D5 | 라이선스 | **Open-core** (코어 Apache-2.0 + 엔터프라이즈 closed) | `docs/design/12-*` | ✅ |
| D6 | 리포 호스팅 | **GitHub private** → Phase 1 exit 후 public 전환 | — | ✅ |
| D7 | 초기 타깃 벤치마크 | CIS Ubuntu 24.04 + ROS2 Jazzy | `docs/design/07-*` | 🟢 (기본값으로) |

## 진행 중 선택지

E6 SSH+Scan 진행 중. Stage A·B·C·D.1·D.2 완료, Stage D.3 대기.

1. **E6 Stage D.3 진입 (권장)** — Exit 검증. 3대 fleet × CIS 2~3 check end-to-end (in-proc fakesshd 확장 또는 testcontainers) + 메모리/goroutine 누수 점검. T7·T8 evaluation rule fixture 통합. sshpool fakesshd 패키지 export 또는 sshpool 내부 cross-package 테스트. 추정 2일.
2. **E12 pack-tools 진입** — `cmd/pack-tools convert` — nrobotcheck 312+329 baseline → rosshield pack 형식 변환. 백그라운드 agent가 4계층 evaluation 패턴 조사 완료. 추정 1주. E5와 독립적이라 병렬 가능.
3. **bootstrap CLI seed admin** — `--seed-admin email password` 플래그로 system tenant + admin user + 기본 system pack 시드. ~1~2시간.
4. **Step 0.3-β OpenAPI 코드 생성** — `oapi-codegen` for auth·pack·robot endpoints. ~2시간.
5. **EventBus WithCriticalFailure 옵션** — R2-4 핵심 구독자(audit) 실패 콜백.
6. **Scheduler Clock 확장 (노선 B)** — 결정론적 테스트 필요해질 때.
7. **Refresh reuse detection** — Phase 1 미구현, API 미들웨어 도입 시.
8. **Windows ACL 키 파일 보호** — 후순위.
9. **로컬 Windows Defender 우회** — 환경 위생.

**권장 순서**: 1(E6 Scan, 가장 큰 핵심 가치 + Phase 1 Exit 직결) → 2(pack-tools)로 자료 활용 + 4(OpenAPI)로 API 골격.
3. **E12 pack-tools 진입** — `cmd/pack-tools convert` — nrobotcheck 312+329 baseline → rosshield pack 형식 변환. 백그라운드 agent가 4계층 evaluation 패턴 조사 완료. 추정 1주.
4. **bootstrap CLI seed admin** — `--seed-admin email password` 플래그로 system tenant + admin user + 기본 system pack 시드. ~1~2시간.
5. **Step 0.3-β OpenAPI 코드 생성** — `oapi-codegen` for auth·pack endpoints. ~2시간.
6. **EventBus WithCriticalFailure 옵션** — R2-4 핵심 구독자(audit) 실패 콜백.
7. **Scheduler Clock 확장 (노선 B)** — 결정론적 테스트 필요해질 때.
8. **Refresh reuse detection** — Phase 1 미구현, API 미들웨어 도입 시.
9. **Windows ACL 키 파일 보호** — 후순위.
10. **로컬 Windows Defender 우회** — 환경 위생.

**권장 순서**: 1(E5) → 2(E6) → 3(pack-tools)로 자료 활용 + 5(OpenAPI)로 API 골격.

## 결정 로그

날짜 내림차순.

- **2026-04-27 · E6 Stage D.2 완료 — bootstrap 결선 + Platform.ScanRun + 어댑터 단위 10**: scan 도메인이 외부 도메인 import 안 하므로 (P5) 결선은 cmd/* 책임. `cmd/rosshield-server/scanexec.go` 신규 — (a) `sshExecutorAdapter`: scan.SSHExecutor 구현, 매 Exec마다 storage.Tx로 robot.Service.GetCredentialMaterial 호출 → 평문 CredentialMaterial unwrap → `materialToAuthMethod`로 ssh.AuthMethod 변환 (password=ssh.Password, privateKey=ssh.PublicKeys with ParsePrivateKey/WithPassphrase) → sshpool.Target 구성 + sshpool.Executor.Exec 호출 → ExecResult 변환. material은 함수 종료 시 GC(평문을 Orchestrator에 노출 X). (b) `benchmarkEvaluatorAdapter`: scan.CheckEvaluator 구현, ruleJSON → benchmark.ParseEvalRule → EvalNode.Eval(EvalInput{stdout/stderr 문자열화, exitCode}) → `mapEvalStatus`로 PASS/FAIL/INDETERMINATE → scan.OutcomePass/Fail/Indeterminate (unknown은 OutcomeError fallback). bootstrap.go: sshpool.New(Deps{Logger}) + 어댑터들 + scanrun.New(Deps{Scan, Storage, Executor, Evaluator, Bus, Clock}) → `Platform.ScanRun *scanrun.Orchestrator`. host key callback은 임시 `xssh.InsecureIgnoreHostKey()` + warning 로그(R4-2 first-touch trust + DB 기록은 후속 stage). scanrun.go: worker ctx에 `storage.WithTenantID(runCtx, tenantID)` 적용 — sshExecutorAdapter가 Tx 시작 시 tenant ctx 필요. bootstrap_test에 ScanRun nil-check 추가. 신규 dep 0. **신규 10 tests** (`scanexec_test.go`): mapEvalStatus 5건(PASS/FAIL/INDETERMINATE/unknown/empty → 매핑)·materialToAuthMethod 7건(password/empty/privateKey ed25519 PEM 생성·empty/invalid PEM/unknown type)·benchmarkEvaluatorAdapter 4건(equals pass/fail·empty rule/invalid JSON 거부). 누적 ~327+ tests, 전체 그린.
- **2026-04-27 · E6 Stage D.1 완료 — Orchestrator 골격 + mock-based 단위 (T5·T6·T9·T10, R6-1~R6-8)**: scan 도메인에 결합 표면 추가(`scan.go`) — `RobotTarget`·`CheckDef`(robot/benchmark의 부분 복제, P5+R6-2)·`ExecResult`·`EvalResult` minimal struct + `SSHExecutor`·`CheckEvaluator` interface(R6-3) + `ProgressEventPayload`·`CompletedEventPayload`+ `EventTypeProgress`·`EventTypeCompleted`·`AggregateTypeScanSession`(R6-6). application layer 신규 패키지 `internal/app/scanrun/` — `Orchestrator.Run(ctx, tenantID, sessionID, robots, checks)` + `Cancel(ctx, tenantID, sessionID, reason)`. 흐름: pending→running 전이(audit `scan.started`) → `golang.org/x/sync/semaphore` weighted(default 10·R4-4·R6-4) + robots×checks fan-out 외부 `outer:` label → 각 worker SSH exec+evaluate+RecordResult+`scan.progress` publish → wg.Wait → finalize: GetSession이 cancelled면 publish만, 아니면 completed로 전이+`scan.completed` publish. R4-5 시멘틱: Cancel 시 새 work item만 skip(`sem.Acquire(runCtx, 1)` 실패→break), 진행 중 worker는 timeout까지 완료 대기. 결과 기록은 background ctx + 5s timeout으로 ctx cancel 영향 회피. 신규 dep 0(`golang.org/x/sync` indirect→direct만). **신규 7 tests**: T5(3×4=12 결과·progress.Completed=12 정확)·T6(workerLimit=3·peak≤3 atomic CAS·16 work item)·T9(50ms 후 Cancel·status=cancelled·totalCalls<20·Run는 ctx.Canceled 또는 nil 반환)·T10(6 progress payload + 1 completed payload inproc subscribe·payload.Completed=6)·EmptyInput(robots/checks 길이 0이면 즉시 completed·executor.totalCalls=0)·MixedOutcomes(pass/fail/error 분포 1:1:1·Failed=2)·SSHError(executor 에러→`OutcomeError`+EvalReason 포함). 백그라운드 agent 노트 `docs/design/notes/e6-stage-d-orchestrator-research.md` 작성됨 (nrobotcheck ScanEngine.ts L146-744·SSHClient·CommandExecutor·DifferentialScanner·IPC events·in-memory test doubles 패턴 + 함정 7건: sudo 비밀번호 stdin 리셋·SSH timeout이 원격 프로세스 안 죽임·Cancel 후 stale connection·Evidence 수집 실패 silent·spec load 시 검증·circuit breaker 부재·Promise.all single fail). 누적 ~317+ tests, 전체 그린.
- **2026-04-27 · R6-1~R6-8 합의 (E6 Stage D)**: 권장값 채택 — Orchestrator 위치 `internal/app/scanrun/` (P5 application layer) / Robot·CheckDef는 scan 패키지 minimal struct(외부 도메인 import 0) / SSHExecutor·CheckEvaluator interface scan 패키지에 / Worker pool `golang.org/x/sync/semaphore` weighted / 통합 테스트 in-proc fakesshd 확장(testcontainers는 Phase 2 exit) / EventBus 페이로드 scan 패키지 정의 / Total은 호출자가 StartScan에 전달 / Cancel은 scan.Service.CancelSession + ctx 신호. Sub-stage 분할 D.1 (3일)·D.2 (2일)·D.3 (2일).
- **2026-04-27 · E6 Stage C 완료 — scan 도메인 격선 + 마이그레이션 0011 (R5-1~R5-7)**: 새 도메인 패키지 `internal/domain/scan/`(tenant·robot 답습 — 단일 패키지에 ScanSession+ScanResult+Service 묶음, P5는 다른 도메인 격리만). 마이그레이션 0011 — `scan_sessions`(FSM CHECK + trigger CHECK + progress 3 컬럼 + (tenant,fleet,created DESC)+(tenant,status,created DESC) 인덱스 — R5-6는 started_at→created_at 정정: NULL 정렬 안정성), `scan_results`(별도 ID `scr_<ULID>` + composite UNIQUE(session,robot,check) + outcome 5-값 CHECK + executed_at NOT NULL + created_at). 모델 `(ScanSession).TransitionTo(target, now)` 메서드 — FSM 검증·StartedAt(running 진입 시 + 기존값 보존)·CompletedAt(terminal 진입 시) 자동 설정·원본 불변(P9). FSM: pending→running·failed·cancelled (R5-5 pending에서도 cancel) / running→completed·failed·cancelled / terminal 거부. Service `StartScan·GetSession·ListSessions·TransitionSession·CancelSession·RecordResult·ListResults`. `(*Repo).TransitionSession`이 audit 매핑 — pending→running=`scan.started`·running→completed=`scan.completed`·*→failed=`scan.failed`(reason)·*→cancelled=`scan.cancelled`(reason). `RecordResult`는 status==running 강제(`ErrSessionNotRunning`) + UNIQUE 위반→`ErrResultDuplicate` + progress.Completed +1·{fail|error}이면 progress.Failed +1 atomic UPDATE. system pack(packs.tenant_id='system') 접근 허용 — `IN (?, 'system')`. AuditEmitter 4 메서드 추가 + bootstrap auditEmitterAdapter 결선(Platform.Scan + bootstrap_test nil-check). **신규 scan 도메인 단위 6 tests**(FSM valid 6 sub·invalid 10 sub·preserve started·terminal·outcome·trigger) **+ sqliterepo 통합 16 tests**(StartScan pending·validate 4 sub·default trigger·transition started·completed·invalid·cancel from pending+running 2 sub·terminal cancel·RecordResult progress 3 outcome·duplicate·non-running·validate 4 sub·list filter·status filter·notfound·system pack) **+ cross-tenant fuzzer 1 test + 8 sub-cases**(Get·Transition·Cancel·Record·StartScan(fleetA·packA) 모두 차단·List 격리). 부수 fix: sshpool `TestExecReturnsStdoutStderrExitCode`의 timing flake — `Duration <= 0` 거부 → `< 0`만 거부(Windows 시계 분해능에서 0 가능). 누적 ~310+ tests, 전체 그린.
- **2026-04-27 · R5-1~R5-7 합의 (E6 Stage C)**: 권장값 채택 — ScanSession ID `scan_<ULID>` / ScanResult 별도 ID `scr_<ULID>` + composite UNIQUE / 단일 `internal/domain/scan/` 패키지 / FSM은 모델 메서드(TransitionTo) + repo는 결과만 UPDATE / Cancel은 pending+running 둘 다 / 인덱스 (tenant,fleet,created DESC)+(tenant,status,created DESC) (started_at→created_at 정정) / trigger enum 컬럼 Stage C에 포함.
- **2026-04-27 · E6 Stage B 완료 — sshpool Pool + per-host/tenant limit + dial backoff (T1)**: `internal/platform/sshpool/pool.go` 신규. 채널 semaphore 기반 — `sync.Cond`보다 ctx 통합 단순(R4-1 권장 A를 채널로 교체, 기능 동일). `PoolKey{TenantID, KeyID, Host, Port}` 식별자, per-host(default 5) + per-tenant(default 50) 동시 conn limit, ctx cancel 시 슬롯 즉시 반환(누수 0). dial 재시도 — 1 + DialMaxRetries(default 3)회, base * 2^attempt + jitter[0, base/2). Phase 1 단순화: **idle 재사용 X** — dial-on-acquire / close-on-release. health check·conn 누수 부담 0, Phase 2+에서 부하 테스트 후 도입 검토. dialFunc은 swap 가능(테스트에서 fakesshd로 교체). **신규 7 sshpool tests**: T1(`TestSSHPoolRespectsHostLimit` — 12 동시 acquire가 limit 3 절대 초과 X, atomic CAS로 peak 추적) · RespectsTenantLimit(per-tenant 2 강제) · TenantsIsolated(두 tenant peak ≥3, ≤4) · CancelWaitingAcquire(첫 acquire가 슬롯 점유 후 두 번째는 ctx 200ms cancel → DeadlineExceeded < 1s) · ClosedRejectsAcquire(`Close()` 후 ErrPoolClosed) · ReleaseIdempotent(double release no-op + 슬롯 정상 회수) · DialBackoffRetries(unused port 1 → connection refused, retry 2회 + base 10ms 적용 검증). 누적 sshpool 16 tests, 전체 ~282+ tests, 그린.
- **2026-04-27 · E6 Stage A 완료 — sshpool Executor + in-proc fake sshd (T2·T3·T4)**: 새 패키지 `internal/platform/sshpool/`. `Executor.Exec(ctx, target, argv, timeout)` — TCP dial → SSH handshake → 1회용 session → argv POSIX single-quote escape 직렬화(`'\''` 패턴) → stdout/stderr/exit 수집. ctx cancel/timeout 시 session.Close + 부분 결과 + ctx.Err 반환(R4-5). MaxStdoutBytes/MaxStderrBytes 10 MiB 기본(§06.8). Target 검증 6건(host/port/user/auth/hostKeyCallback 필수). `golang.org/x/crypto/ssh` 의존(이미 E3에서 존재 — 추가 dep 0). **R4 결정 C** — 자체 in-proc fake sshd(`fakesshd_test.go` ~190줄): ed25519 host key·NoClientAuth·session 채널 accept·exec payload 디코드·delay/stdout/stderr/exit 응답. 받은 cmd 기록으로 argv 직렬화 검증 가능. **신규 sshpool 9 tests**(+6 sub-cases): T2 ReturnsStdoutStderrExitCode · T3 TimeoutCancels(timeout 200ms·fake delay 3s → context.DeadlineExceeded, < 2s 반환) · ContextCancelStops(ctx.Done → context.Canceled) · T4 ArgvNotShellParsed(`['echo','$HOME','&&',...]` → `'echo' '$HOME' '&&' ...` literal 직렬화 검증) · JoinArgvEscapesSingleQuote(POSIX `'\''` 4 cases) · RejectsEmptyArgv · ValidatesTarget(6 sub-cases) · TruncatesLargeStdout(MaxStdoutBytes 100 강제) · RejectsWrongHostKey(FixedHostKey 일치/불일치). 누적 ~275+ tests, 전체 그린.
- **2026-04-27 · R4-1~R4-7 합의 (E6)**: 권장값 채택 — 자격증명 session당 decrypt 1회 그 컨텍스트 캐시(Phase 2+ keychain 검토) / known_hosts first-touch trust + DB 기록 + 불일치 즉시 실패(설계서 §06.8 정합) / argv quoting은 팩 책임(`bash -c "..."`) + validation·길이 제한만 / Worker pool 10 고정(E6.T6 실측 후 config 항목 검토) / Cancel은 timeout 신뢰·진행 중 완료대기·다음 item 스킵 / Evidence redaction default on(E7) / Differential hash match reuse + 프리플라이트 불가 체크 제외 플래그(§07.9).
- **2026-04-27 · E5 Stage E 완료 — TestConnection mock + cross-tenant fuzzer (T5) → E5 epic 완전 종료 (5/5 Stage + 7/7 T)**: `robot.SSHTester` 인터페이스(`TestConnection(ctx, host, port, authType, material) error`) — E6 sshpool 구현체 결선 표면 미리 추상. `Service.TestConnection(robotID)` 추가 — GetRobot(활성 검증) → GetCredentialMaterial(unwrap) → SSHTester 위임. SSHTester nil이면 ErrSSHTesterNotConfigured. Deps에 SSHTester 추가, bootstrap에서 nil로 결선(E6 진입 시 sshpool 어댑터 주입). **신규 testconn_test.go 5 tests**: T5(`TestTestConnectionUsesSSHTester` — fakeSSHTester가 정확한 host/port/authType/username 받았는지 검증) + PropagatesSSHError + WithoutTesterReturnsConfigError + RobotSoftDeletedReturnsNotFound + CrossTenantReturnsNotFound. **신규 crosstenant_test.go**: `TestCrossTenantFuzzer` 1 test + 8 sub-cases(GetFleet·GetRobot·ListRobots(by fleetA)·DeleteRobot·GetCredentialMaterial·RotateCredential·TestConnection 모두 ErrNotFound · CreateRobot with fleetA → ErrFleetNotFound) + ListFleets/ListRobots(B만 노출) 2 sub-tests = 8 cross-tenant 회귀(E3 `031fa05` 패턴 답습). **E5 epic 완전 종료** — phase1-backlog.md E5.T1~T7 모두 ✅ + Exit 기준 모두 충족. 누적 robot 도메인 56 tests(A 13 + B 18 + C 12 + D 11 + E 5+8+2). 전체 ~260+ tests, 그린.
- **2026-04-27 · E5 Stage D 완료 — CSV import 파서 (T6)**: `internal/domain/robot/csv.go` 신규 — 패키지 레벨 함수 `ParseRobotsCSV(fleetID, reader)` → `([]CSVRow, []ImportRowError, error)`. **Service 인터페이스 건드리지 않음** — 호출자(API gateway·CLI)가 결과 row별로 `Service.CreateRobot` 반복 호출. `encoding/csv` stdlib만, 외부 의존 0. 표준 영문 소문자 헤더 13개(필수 4: name·host·username·authType + 옵션). 자격증명은 password XOR privateKeyPem 강제 — ambiguous(둘 다)/missing(둘 다 빈) 시 ImportRowError. UTF-8 BOM 자동 제거(파일 시작 BOM 보존, 첫 헤더 셀 strip), 빈/공백-only 행 skip, 부분 성공(nrobotcheck `robot.router.ts:155-276` 패턴 답습). 11 신규 tests: T6(`TestParseRobotsCSVRejectsInvalidRows` — 9 거부 케이스 + 1 정상 통과: 빈 host·port out-of-range·port 비숫자·invalid authType·자격증명 missing/ambiguous·authType↔자격증명 mismatch·빈 username·invalid criticality) + 보조 10(AcceptsValidRows·MissingHeader·UnknownHeader·Empty·HeaderOnly·StripsBOM·AppliesFleetID·SkipsBlankLines·LineNumber·ImportRowErrorString). 함정: Go는 string literal 안의 mid-file UTF-8 BOM을 거부 — escape sequence `﻿` 필수(테스트 데이터 작성 시 주의). 누적 ~245 tests, 전체 그린.
- **2026-04-27 · E5 Stage C 완료 — Robot CRUD + Credential 결선 + Rotate + soft delete (T2·T3·T4·T7)**: 마이그레이션 0010 — `robots` 테이블, FK fleet_id·credential_id, partial unique 2개(`(tenant_id, fleet_id, name)`·`(tenant_id, host, port)` 모두 `WHERE deleted_at IS NULL` — R3-7), tags TEXT JSON. `internal/domain/robot/sqliterepo/robot.go` 신규 — CreateRobot은 한 Tx에 Credential wrap·INSERT + Robot INSERT + audit emit, FleetID 활성 검증, AuthType↔Material.Type 일치 검증(불일치 시 ErrRobotInvalidAuthType). DeleteRobot은 soft + 연결 credential cascade revoke + audit emit, 두 번째 호출은 ErrNotFound(Phase 1 명시적 한 번). RotateCredential은 새 cred 생성·wrap·INSERT → Robot.credential_id·auth_type 갱신 → 이전 cred revoked_at 설정 → audit emit, 모두 한 Tx(R3-3). GetCredentialMaterial은 활성 Robot+활성 Credential 둘 다 검증 후 unwrap. AuditEmitter 인터페이스 3 메서드 추가(EmitRobotCreated·EmitRobotDeleted·EmitCredentialRotated) + bootstrap auditEmitterAdapter 결선. 마이그레이션 카운트 9→10 정정. **신규 12 tests**: T2(FleetID 빈 값/존재하지 않는 ID 거부) · T3(DB+WAL 파일 grep으로 평문 password marker 부재 검증 — encryption-at-rest acceptance) · T4(rotate audit head +1, OldCredID·NewCredID 검증, 새 material 일치) · T7(soft delete 후 GetRobot ErrNotFound·audit chain Verify·두 번째 delete ErrNotFound) + AppliesDefaults(port 22·medium·privateKey)·DuplicateNameInSameFleet·SameNameAcrossFleets·DuplicateHostPort·AuthTypeMaterialMismatch·CredentialMaterialRoundtrip·ListByFleetAndAll·CrossTenantBlocked(GetRobot·ListRobots·GetCredentialMaterial). 누적 ~234 tests, 전체 그린.
- **2026-04-27 · E5 Stage B 완료 — KEK/DEK envelope encryption 코어**: `internal/domain/robot/kek.go`(LoadOrCreateKEK — 32B AES-256, perm 0600 Unix 강제·Windows skip[ACL은 후순위], KeyID `kek_<sha256(KEK)[:8] hex>`, Signer LoadOrCreate 패턴 답습) + `dek.go`(WrapMaterial/UnwrapMaterial, KEK→DEK 2계층, per-record DEK 32B random + AAD `t=<tenantID>;c=<credentialID>;v=1`로 cross-credential 키 재사용 차단). 마이그레이션 0009 — `credentials` BLOB(`encrypted_payload`) + TEXT JSON(`encryption_meta`). `Credential`·`CredentialMaterial`·`EncryptionMeta` 모델 추가. bootstrap에 `LoadOrCreateKEK(<dataDir>/keys/credential.kek)` 결선 + `kekKeyId` 부팅 로그. 18 신규 단위 tests pass: KEK 6(generate·reload 동일 keyID·KeyID 형식·invalid length·loose perm·empty path) + DEK 12(roundtrip password·privateKey·plaintext 누출 부재·tampered ciphertext/wrappedDEK/AAD·다른 KEK·미지원 version·다른 두 wrap 다른 결과·empty tenant/credentialID·invalid type·empty username·AAD 형식). **외부 의존 추가 0** — stdlib `crypto/aes`·`crypto/cipher`·`crypto/rand`·`crypto/sha256`만. 마이그레이션 카운트 검증 8→9 정정. 누적 ~222 tests, 전체 그린. T3 acceptance(`TestRobotCredentialEncryptedAtRest` DB grep)는 **Stage C로 이동** — Service.CreateRobot이 Credential을 같은 Tx에 wrap하므로 Robot CRUD와 함께 검증.
- **2026-04-27 · E5 Stage A 완료 — Fleet 도메인 골격 + bootstrap 결선**: 합의된 R3-1~R3-7 권장값 7건 모두 채택. 새 도메인 패키지 `internal/domain/robot/` 신설(tenant 패키지 답습 — 단일 패키지에 Fleet/Robot/Credential 묶음, P5는 다른 도메인 격리만 강제). 마이그레이션 0008 (`fleets` 테이블, `(tenant_id, name)` partial unique index `WHERE deleted_at IS NULL` — R3-5/R3-7 적용). Fleet 모델 + FleetPolicy 4 필드(R3-4 — DefaultBaselineID·DefaultLevel·DefaultCriticality·ScanSchedule). Service.CreateFleet은 한 Tx에 fleets INSERT + audit emit(`auditEmitterAdapter.EmitFleetCreated` 추가, P5 격리). GetFleet/ListFleets는 deleted_at IS NULL 필터로 cross-tenant + soft-deleted 차단. 13 신규 tests pass(T1 + 보조 12: empty/long/invalid level/invalid criticality/duplicate/soft-deleted reusable/no-tenant-context/get returns/get ignores soft/cross-tenant get/list active only/cross-tenant list/policy roundtrip). bootstrap에 `Platform.Robot robot.Service` 결선. 마이그레이션 카운트 검증 7→8 정정. 누적 ~204 tests, 전체 그린.
- **2026-04-27 · R3-1~R3-7 합의 (E5)**: 권장값 채택 — KEK 파일(`<dataDir>/keys/credential.kek` 0600, OS Keychain·KMS·TPM은 Phase 3) / Tenant Key Phase 2+ (Phase 1은 KEK→DEK 2계층) / Credential rotation 수동 API만 / FleetPolicy 4 필드 / soft delete `deleted_at` only + 읽기 필터 / Fleet=`fl_<ULID>`·Credential=`cr_<ULID>` / Robot UNIQUE `(tenant_id, fleet_id, name)` + `(tenant_id, host, port)` 둘 다 partial.
- **2026-04-27 · E5 Robot/Fleet 진입 · 핸드오프/백로그 표기 정정 · 사전 리서치 노트 2건 신규**:
  - **표기 정정**: 백로그(`phase1-backlog.md`)는 E5=Robot/Fleet, E6=SSH+Scan으로 정의되어 있으나 핸드오프 line 13/101이 E5를 "Scan engine"으로 부르던 불일치를 백로그 의존 그래프(E5→E6) 기준으로 통일. 사용자 선택지 A(Robot/Fleet 먼저, SSH+Scan은 prerequisite로서 그 다음).
  - **사전 리서치 결과물 부재 보강**: 이전 세션이 "E5 SSH 사전 리서치 완료"로 표기했으나 결과물이 노트로 보존되지 않음(휘발). 백그라운드 agent 2개 병렬 분담 — (1) nrobotcheck robot/fleet/credential 자산 조사, (2) E6 SSH+Scan 사전 리서치 — 모두 노트로 보존.
  - **신규 노트**: `docs/design/notes/e5-robot-fleet-deepdive.md` (E5 도메인 구조·KEK/DEK·CSV import·함정 6건·결정 R3-1~R3-7), `docs/design/notes/e6-ssh-scan-deepdive.md` (E6 SSH 라이브러리·Pool·Executor·Orchestrator·Evaluator 결선·결정 R4-1~R4-7).
  - **결정 ID 컨벤션**: E1=R1(storage)·R2(eventbus), E5=R3, E6=R4. 후속 epic도 동일 패턴.
  - **R3-1~R3-7 (E5)** — KEK 저장 위치·Tenant Key 도입 시점·Credential rotation 트리거·Fleet policy 구조·Soft delete cascade·Fleet/Credential ID 접두사·UNIQUE 제약 범위. 사용자 합의 대기 중(E5 Stage A 착수 전).
- **2026-04-24 · E4 sqliterepo + bootstrap 결선 — 운영 준비 완료 (T8)**: `internal/domain/benchmark/sqliterepo/` 패키지 신설. Service 인터페이스를 5 메서드로 재정의 (InstallPack/GetPackByKey/ListPacks/CurrentState/TransitionPack). InstallPack은 한 Tx에서 packs/checks/lifecycle INSERT + audit emit. UNIQUE 위반 → ErrPackAlreadyInstalled 매핑. bootstrap의 `auditEmitterAdapter`에 EmitPackInstalled + EmitPackLifecycleChanged 추가 (P5 격리 — benchmark가 audit 직접 import 안 함). Platform.Benchmark 결선 완료. 4 신규 sqliterepo tests + bootstrap 회귀, ~190 tests, CI green 1m2s. 커밋 `f8acb30`. **다음 epic E5 Scan 사전 리서치 완료** (SSH connection 재사용·knownhosts·ctx 취소·세션 누수 방지 패턴 — 진입 즉시 활용).
- **2026-04-24 · E4 Pack 시스템 epic 완전 종료 (7/7 + Stage A~E)**: 새 도메인 `internal/domain/benchmark/` — 외부 자산(서명된 콘텐츠) 처리 첫 도메인. **5 stages, 합의된 C1~C8 모두 권장 채택**.
  - **Stage A** (`d2017eb`) — pack.yaml/checks/*.yaml YAML 파싱 + JSON Schema (draft 2020-12). `go.yaml.in/yaml/v3` 채택(v4는 RC). KnownFields strict + additionalProperties:false 이중 차단.
  - **Stage B** (`c50f872`) — tar.gz 안전 해체 (zip slip + zip bomb 차단) + MANIFEST canonical JSON + Ed25519 SIGNATURE. SIGNATURE를 가장 먼저 검증 (신뢰 경계 최소화). 파일당 16MiB / 총 256MiB 한도.
  - **Stage C** (`8ce9cea`) — Sealed interface AST evaluator. 9 op + 3 logical + 3-값 결과(PASS/FAIL/INDETERMINATE). 2-phase JSON 디코드(op 식별 → strict struct 재파싱) + DisallowUnknownFields + UseNumber. regex 256B 제한. **외부 평가 라이브러리(`expr-lang/expr`, `cel-go`, `goja`) 미사용** — attack surface 최소화.
  - **Stage D** (`fb1aa03`) — Self-Test fixture 러너 + Degraded 마커. expectedOutcome 3-값 강제. RunPackSelfTests 일괄 실행.
  - **Stage E** (`282c18b`) — Lifecycle FSM 5+1 상태(Installed→Staged→Active⇄Inactive→Archived→Removed). default deny 화이트리스트, self-transition 금지. Active 진입은 Staged 거쳐야만 (검증된 콘텐츠만 활성, P1·P8).
  - 신규 의존성: `go.yaml.in/yaml/v3 v3.0.4` + `github.com/santhosh-tekuri/jsonschema/v6 v6.0.2` (둘 다 Apache-2.0).
  - 누적 ~179 tests pass, CI green 5회. **3개 백그라운드 agent 활용**: nrobotcheck 자료(312+329 baseline 구조 + 4계층 evaluation) + YAML/JSONSchema/tar.gz 라이브러리 함정 + AST evaluator 권장 패턴 — 모두 사전 차단.
  - 후속 작업: sqliterepo INSERT 흐름 (Pack DB 영속 + lifecycle row + audit emit) — 별도 stage.
- **2026-04-24 · E3 Stage C·D·E 완료 → E3 epic 완전 종료 (8/8)**:
  - **Stage C** (`7d55ca9`) — ApiKey: 0005 마이그레이션, `fg_live_<32 base32>` 40자 토큰, prefix 12자(UNIQUE(tenant_id, prefix)), argon2id 해시, soft delete (`revoked_at = COALESCE(...)`로 멱등). T5·T6 + 5 보조 tests. AuthenticateApiKey는 cross-tenant Bootstrap Tx — prefix 통계 충돌 0(160bit random).
  - **Stage D** (`d8c6b8c`) — JWT login: 0006 마이그레이션, `golang-jwt/jwt/v5` EdDSA, `<dataDir>/keys/jwt.ed25519` 별도 키, access 15m·refresh 14d, rotation. 사전 리서치 agent로 함정 2개(alg=none·키 비대칭) 사전 차단. T3·T4 + 4 보조 tests. **reuse detection은 Phase 1 미구현** (같은 Tx에서 일괄 revoke하면 ErrRefreshRevoked 반환과 함께 rollback돼 의미 없음 — 호출자 별도 Tx 패턴은 후속).
  - **Stage E** (`031fa05`) — Cross-tenant fuzzer: 두 tenant 시나리오 + 7 cross-tenant 메서드 회귀 (GetUserByEmail·GetRole·RevokeApiKey·ListApiKeys·Login·GetUserRoles·AuthenticateApiKey).
  - 합의된 R-B3·B4·B5 권장 그대로 채택. `LoadOrCreatePrivateKey` 신설(jwt 라이브러리는 raw `ed25519.PrivateKey` 요구). 누적 ~135 tests, CI green.
  - **병렬 작업 활용**: 2026-04-24 사용자 지시 후 첫 적용 — Stage C 본 작업 중 Stage D JWT 사전 리서치를 백그라운드 agent로 분담. 결과 활용으로 Stage D 진입 즉시 함정 2개 사전 차단.
- **2026-04-24 · E3 Stage A·B (Tenant·User·RBAC) 완료**: depguard로 도메인 격리(audit 외부 production 차단, 테스트 예외) 강제. `internal/domain/tenant/` 단일 패키지에 Tenant·User·Role·Permission. 마이그레이션 0003(tenants+users)+0004(roles+user_roles). argon2id m=64MB·t=3·p=1·keyLen=32·saltLen=16 PHC 포맷. AuditEmitter 인터페이스로 audit 도메인 결합 분리(P5) — `cmd/rosshield-server/bootstrap.go`의 `auditEmitterAdapter`가 결선 글루. CreateTenant가 한 Tx에 tenant + admin user + 시스템 역할 3개(admin/auditor/operator) 시드 + admin role 자동 할당 + audit emit. permission는 string set + 와일드카드 `*`(admin). AssignRole `ON CONFLICT DO NOTHING`로 멱등. Service 시그니처: Create/GetTenant/GetUserByEmail/GetRole/AssignRole/GetUserRoles. 21 신규 tests pass(password 5·rbac 4·sqliterepo 8 + bootstrap 0 회귀). 합의된 R-B1·B2·B6·B7·B8 채택. 커밋 `eed4b35`(Stage A) + `bfc498e`(depguard test 예외 fix) + `d344c4b`(Stage B), CI green 3회. 남은 Stage C(ApiKey)·D(JWT)·E(cross-tenant fuzzer).
- **2026-04-24 · depguard 도메인 격리 룰 추가**: `.golangci.yml`에 `depguard` enable + `audit-domain-isolation` rule. `internal/domain/audit/**` + `cmd/**` + `internal/api/**` 외부에서 audit import 차단. `*_test.go`는 wiring 통합 검증 위해 예외. CI -race는 이미 적용돼 있던 것 확인(line 39). 커밋 `0c744a5`, CI green 53s.
- **2026-04-24 · Bootstrap 결선 + Signer 영속**: `cmd/rosshield-server/bootstrap.go`에 `Audit audit.Service` + `systemTenant` 추가. Signer는 `soft.LoadOrCreate(<dataDir>/keys/platform.ed25519)`로 변경(raw 64B Ed25519, 파일 0600·디렉토리 0700). Config에 `SystemTenantID`(default "system") + `CheckpointSpec`(default "@every 1h") 노출 — 테스트는 `@every 1s`로 단축. `audit.RegisterCheckpointJob`을 부팅 시 자동 등록(system tenant). healthz `auditHealth{HeadSeq, LastCheckpoint, Status}` 추가, storage liveness + audit 조회를 같은 Bootstrap Tx에서. 9 신규 tests pass(soft 4건·bootstrap 5건). 스모크: 같은 data-dir로 두 번 부팅 → `signerKeyId=key_ce7d13426af78184` 동일. 키 형식 raw bytes 채택(PEM 미사용 — 외부 도구 의존 최소). 커밋 `4b6a2aa`, CI green in 41s.
- **2026-04-24 · E2 Audit epic 완료 (8/8 + Stage A~D)**: 첫 도메인 패키지 `internal/domain/audit/` + sqliterepo 어댑터.
  - **Stage A** (`0508d4d`) — 스키마 3분할(`audit_entries`/`audit_chain_heads`/`audit_checkpoints`) + BEFORE UPDATE/DELETE trigger + `mapErr` "immutable" 매핑. Append는 외부 Tx에 head 읽기 → INSERT entry → UPSERT head를 묶음. canonical JSON meta(알파벳순 키, RFC3339Nano UTC)로 hash 입력 직렬화. 13 tests pass.
  - **Stage B** (`76d41f8` + `566afc5` lint fix) — Verify 재계산 루프. seq 연속성·prev_hash 연결·hash 재계산 3가지 검증, 첫 위반 시 BreakAt+Reason 반환. 6 tests + lint 후속 1.
  - **Stage C** (`c228937`) — Export NDJSON+gzip + Ed25519 signature line. SignedDigest=sha256(모든 entry 라인). 외부 도구 시뮬레이션 테스트로 검증 가능 확인. 3 tests.
  - **Stage D** (`d7639b4`) — `WriteCheckpoint`/`LatestCheckpoint` + `SerializeCheckpointPayload`(hash[32]‖seq BE 8B‖tenantId tail) + `RegisterCheckpointJob`. 빈 체인 ErrNoEntries / 중복 head ErrCheckpointExists는 cron debug 로그 후 noop. Scheduler 통합 테스트로 `@every 1s` 잡이 audit_checkpoints에 row 생성 검증. 6 tests.
  - 합의된 R-A1~A8 결정 8건 모두 권장값 그대로 채택. 누적 ~75 tests, CI green 4회.
- **2026-04-24 · E1 Exit — Platform bootstrap 완료 — E1 epic 완전 종료**: `cmd/rosshield-server/bootstrap.go`(`Config`/`Platform` + `Bootstrap(ctx, cfg)` + idempotent `Shutdown(ctx)`) + `main.go` 재작성(`--data-dir` 플래그, 기본 `~/.rosshield`, `os.UserHomeDir` fallback `os.TempDir/rosshield`). 시퀀스: Logger → Clock → IDGen → Storage(Open) → Migrate → EventBus → Signer → Scheduler. Shutdown 역순(Scheduler → EventBus → Storage), `sync.Once`로 멱등. SIGINT/SIGTERM에 http.Server.Shutdown(10s) + Platform.Shutdown(10s) 순. `/healthz`: storage 가벼운 트랜잭션(R1-2 Bootstrap 진입점) + 컴포넌트별 status + signer keyID(`key_<16hex>`) 노출, shutdown 후 503/`status:"shutting_down"`. main_test.go는 bootstrap_test.go가 cover하므로 삭제. 8 tests pass(InitsAllServices·CreatesDataFile·DataDirAutoCreated·HealthzAllOk·HealthzAfterShutdown503·POST405·ShutdownIdempotent·EmptyDataDirFails). 스모크: 부팅 로그 JSON·healthz 200 + 모든 컴포넌트 ok·POST 405·data.db + WAL(-shm/-wal) + flock(-migration.lock) 정상 생성 확인. 커밋 `f3feee9`, CI green in 38s. **E1 epic 완료** — 다음은 E2 Audit 도메인.
- **2026-04-23 · E1.T9 Scheduler 완료 — E1 9/9 태스크 완성**: `internal/platform/scheduler/`(인터페이스 + errors) + `cronsched/`(robfig/cron/v3 어댑터). New()는 즉시 cron.Start(), Schedule(id, spec, job)/Cancel(id)/Close(ctx) 표면. panic·error 모두 logger 기록 후 다음 발화 진행. 노선 A 채택 — robfig/cron 내부 `time.Now()` 그대로 사용, Clock 확장은 보류(필요 시 노선 B로 swap). robfig/cron의 ConstantDelaySchedule이 second-precision으로 truncate하므로 sub-second 스펙(@every 100ms)은 미지원, 모든 발화 테스트는 `@every 1s`. 7 tests pass(FiresAtSpec·CancelStopsJob·DuplicateID·InvalidSpec·HandlesJobError·CancelNonExistent·HandlesJobPanic). 신규 dep: `robfig/cron/v3 v3.0.1`. 커밋 `0ebe38f`, CI green in 2m40s. **E1 모든 platform 패키지 완성** — 다음은 cmd/rosshield-server bootstrap 시퀀스 (E1 Exit) 또는 E2 Audit 도메인.
- **2026-04-23 · E1.T8 Signer 완료**: `internal/platform/signer/`(인터페이스 + errors) + `soft/`(Ed25519 메모리 키). stdlib `crypto/ed25519`만 사용(외부 dep 0). `New()`는 매 호출 새 키 생성(영속은 E2 audit checkpoint에서 파일 로드 추가 예정). KeyID 형식 `key_<sha256(publicKey)[:8] hex>` (총 20자, 안정적). 7 tests pass(roundtrip·payload·sig·short·KeyID 형식·외부 검증 일치·인스턴스 분리). 표면: Sign(payload) → (sig, keyID, err) / Verify(payload, sig) / PublicKey() / KeyID(). 커밋 `950cd3a`, CI green in 37s.
- **2026-04-23 · E1.T6/T7 EventBus 완료**: `internal/platform/eventbus/`(공개 표면) + `inproc/`(어댑터). 채널 모델 B(per-subscriber fan-out) + 고루틴 모델 M2(subscriber당 worker) + bounded channel + 두 정책(Block·DropOldest) + panic 격리(defer recover) + Drain 헬퍼. envelope auto-fill: ID(`evt_<ULID>`)·OccurredAt(Clock.Now)·CorrelationID(ctx 또는 `cor_<ULID>` 자동 생성). handler ctx에 CorrelationID + CausationID 자동 주입(R2 §7 계보 전파). R2 §13 체크리스트 8건 + 추가 2건(auto-correlation·empty-Type) = 10 tests pass. 모든 R2 결정 7건 반영(R2-1 outbox 없음·R2-2 수용 보장·R2-3 미강제 lint 후속·R2-4 기본 로그·R2-5 영속은 audit·R2-6 wildcard 미지원·R2-7 자동 생성). 외부 의존 없음(stdlib만, 내부 platform 3개 의존). 커밋 `d97ff1f`, CI green in 30s. -race 검증은 Linux CI에서 별도 활성화 필요(후속).
- **2026-04-23 · E1.T5 Migrate 완료**: `internal/platform/storage/sqlite/migrate.go` + `internal/platform/storage/embed.go`(`//go:embed migrations`) + `migrations/sqlite/0001_platform_init.sql`. T4의 nil-stub Migrate를 실구현으로 교체. gofrs/flock OS file lock(`<dsn>.migration.lock`) 5초 timeout(R1-6) → goose v3 NewProvider(SQLite3) + Up. 첫 마이그레이션은 `platform_info` KV 테이블만 생성(도메인 테이블은 E2~E5에서 추가). 3 tests pass(스키마 적용·idempotent·외부 락 선점 시 ErrMigrationLocked). 신규 dep 2개(goose v3.27.0, flock v0.13.0) + transitive 3종. 커밋 `980d6f9`, CI green in 2m32s.
- **2026-04-23 · E1.T4 Storage 완료**: `internal/platform/storage/` Storage·Tx 인터페이스 + `sqlite/` modernc.org/sqlite 어댑터. 매 connection 확립 직후 PRAGMA 7개(`foreign_keys=ON`·`journal_mode=WAL`·`synchronous=NORMAL`·`busy_timeout=5000`·`temp_store=MEMORY`·`cache_size=20MB`·`wal_autocheckpoint=1000`)를 커스텀 `driver.Connector`로 적용. Tx는 ctx에서 TenantID 추출, 없으면 `ErrTenantMissing`. Bootstrap은 tenant-less 진입점. panic 시 rollback 후 re-panic. 7 tests pass(context roundtrip·commit/rollback·tenant 강제·Bootstrap·tenant 전파·PRAGMA 검증·panic rollback). modernc.org/sqlite v1.49.1 + transitive 4종 추가. Migrate()는 nil 반환 stub(T5에서 goose 통합). 첫 push 후 CI Tidy check 실패(transitive 누락) → `go mod tidy` 후 재push 성공. 커밋 `b1af50d` + `d8f3034`, CI green in 2m22s.
- **2026-04-23 · R1·R2 미해결 질문 14건 합의**: Storage 7건 + EventBus 7건 결정. 두 노트(`docs/design/notes/e1-storage-deepdive.md` §10, `e1-eventbus-deepdive.md` §12)에 결정 사항 마킹. R1 결정으로 본문 갱신: §5 Tx 인터페이스에서 `ReadOnly` 제거, `Bootstrap(ctx, fn)` 진입점 추가(tenant-less 마이그레이션·seed 전용), §6 PG audit는 `DO INSTEAD NOTHING` 폐기 후 `TRIGGER + RAISE EXCEPTION`로 교체, §9 Storage·Tx 인터페이스·`ErrMigrationLocked` 추가. R2는 본문이 이미 추천과 정합이라 §12 마킹만. E1.T4(Storage)·E1.T6(EventBus) 착수 차단 해제.
- **2026-04-23 · E1.T3 IDGen 완료**: `internal/platform/idgen/` `IDGen` 인터페이스(`New(prefix string) string`) + `NewULID()` (oklog/ulid v2.1.1 + `crypto/rand` + `sync.Mutex`, monotonic ms-내 ordering). 5 tests pass(prefix·길이·Crockford 알파벳·empty prefix·1000건 유일성·50×100 동시성 5000건). 신규 dep 1개(oklog/ulid v2, Apache-2.0, stdlib만 사용). Clock 주입은 보류 — T9 Scheduler에서 결정론적 ID 검증이 필요해지면 그때 생성자 확장. 커밋 `81ded88`, CI green in 1m36s.
- **2026-04-23 · E1.T2 Clock 완료**: `internal/platform/clock/` `Clock` 인터페이스(`Now() time.Time`) + `System()` + `*FakeClock`(`Set`/`Advance`, `sync.Mutex`로 동시성 안전, 음수 Advance는 panic). 6 tests pass(System now·FakeClock 주입·Set·Advance·negative panic·50 goroutine 동시성). 표면은 미니멀 시작(YAGNI), `Sleep`/`After`는 E1.T9 Scheduler 진입 시 필요하면 확장. 커밋 `d9ee1c1`, CI green in 25s.
- **2026-04-23 · E1 사전 설계 노트 2건**: 에이전트 병렬 리서치로 `docs/design/notes/e1-storage-deepdive.md` (502줄) + `docs/design/notes/e1-eventbus-deepdive.md` (444줄) 생성. 메인 단일 스레드로 E1.T1 Logger 구현하는 동안 백그라운드 확보. 파일 충돌 0, trunk 위배 0. 각 노트에 미해결 질문 7건 포함 — E1.T4/T6 착수 전 합의 필요. 커밋 `8517bcb`.
- **2026-04-23 · E1.T1 Logger 완료**: `internal/platform/logger/` context-aware slog 래퍼. TDD 5건 pass, CI green in 30s. 커밋 `b67b2c1`.
- **2026-04-23 · Phase 0 종료**: GitHub 원격 `ssabro/rosshield` (PRIVATE) 생성·연결·첫 push. CI workflow 2회 실행(첫 회 실패 → golangci-lint/Go 버전 충돌 수정 → 두 번째 회 green in 1m18s). 커밋 7개 공개.
- **2026-04-23 · Step 0.4**: Phase 1 백로그 `docs/design/phase1-backlog.md` (에픽 12 × TDD 태스크, 12주 추정).
- **2026-04-23 · Step 0.3**: OpenAPI 3.1 `openapi/openapi.yaml` v0.0.1 스켈레톤 — 엔벨로프·에러·공통 컴포넌트·대표 경로 11종.
- **2026-04-23 · Step 0.2**: Go 1.26 부트스트랩 완료 — `go.mod`·`Makefile`·`.golangci.yml` v2·`/healthz` TDD 스텁(통과).
- **2026-04-23 · D6 결정됨**: 리포 호스팅 `GitHub private`. Phase 1 exit 시점에 public 전환(open-core 코어 공개 연동).
- **2026-04-23 · D5 결정됨**: 라이선스 `Open-core`. 코어(감사 엔진·CLI·팩 포맷)는 Apache-2.0 공개, 엔터프라이즈 계층(SSO·멀티테넌트 관리·클라우드 대시보드)은 closed. 근거: 감사 도구 신뢰성 확보 + 팩 포맷의 외부 검증 가능성(P1) 유지.
- **2026-04-23 · D4 연기됨**: 어플라이언스 OS는 Phase 3 exit 시점에 최종 확정. 그때까지 기본 가정은 **Ubuntu Core 24**.
- **2026-04-23 · D3 결정됨**: 데스크톱 셸 `Tauri 2.x`. Go 백엔드는 자식 프로세스로, Tauri는 얇은 WebView 껍질. Electron은 긴급 출시 fallback으로만 보류.
- **2026-04-23 · D2 결정됨**: 백엔드 `Go`, 프론트 `TypeScript`. 근거: 단일 정적 바이너리, `crypto/ssh`·`ed25519` 성숙, 3종 배포 natural fit, P3/P7 원칙 부합. `nrobotcheck`의 Electron·native 모듈 운영 부담 회피.
- **2026-04-23 · D1 부분 확정**: 초기 가칭 "FleetGuard"는 Cummins(엔진 필터 1950~) 및 Attestor.ai·TrustArc 등 보안 감사 도메인과 상표 충돌로 폐기. Google 검색으로 후보 5개(robocheck·rosshield·scanroot·attestbot·attestor) 충돌 여부 검증 후 **코드 네임스페이스 `rosshield` 확정**(ROS2 도메인 연상 + 충돌 없음). 제품 브랜드는 `<ProductName>` placeholder로 유지 → Phase 1 후반 법무·도메인·상표 조사와 병행 확정.
- **2026-04-23**: 리포를 `D:\robot\dev\fleetguard`로 신설. 전신 `nrobotcheck`에서 설계·개념 승계, 코드는 새로 작성.
- **2026-04-23**: 13개 설계서 초안 완성(Draft v0.1).
- **2026-04-23**: 상업화 방향 — 어플라이언스 단독 진화 X, 헤드리스 코어 + 배포 3종(데스크톱·온프렘·어플라이언스 이미지). 근거는 전신 리포 `docs/COMMERCIALIZATION_STRATEGY.md`.
- **2026-04-23**: CAI(aliasrobotics)와의 포지션 분리 — 자율 공격 에이전트 프레임워크는 비목표.

## 작업 재개 절차

1. 이 문서 읽기
2. `git log --oneline -10` 및 `gh run list --limit 3 --repo ssabro/rosshield`로 최근 상태·CI 확인
3. 사용자에게 "## 진행 중 선택지"를 제시하고 번호 선택 받기
4. 관련 설계서 섹션 정독 (Phase 1이면 `docs/design/phase1-backlog.md`의 해당 에픽)
5. 도메인 경계·테넌시·감사 영향을 1차 점검 (아래 "긴급 체크리스트")
6. TDD 착수 (Red → Green → 필요 시 Refactor)
7. 커밋 전 로컬 파이프라인 녹색 확인: `make vet && make test && make lint` (또는 `make ci`)
8. 커밋·push 후 GitHub Actions 녹색 확인 (`gh run watch`)
9. 에픽/스텝 완료 시 이 문서 **"결정 로그"** + **"현재 상태 한 줄"** + (필요 시) **"진행 중 선택지"** 갱신 + `docs/design/phase1-backlog.md`의 태스크 체크박스 업데이트

## 아직 없는 것 (Phase 1 이후 생길 것)

- 도메인 레이어 (`internal/domain/*`) — Phase 1 E1~E12에서 점진 생성.
- 저장소 구현 (`internal/platform/storage/*`) — E1에서 SQLite 어댑터.
- 이벤트 버스 (`internal/platform/eventbus/*`) — E1.
- SSH 풀 (`internal/platform/sshpool/*`) — E6.
- 서명·감사 체인 (`internal/domain/audit/*`) — E2.
- Web UI (`web/` 또는 `ui/`) — E10 (별 모듈, 아마 pnpm workspace).
- OpenAPI 코드 생성 결과물 (`internal/api/gen/*`) — Step 0.3-β 또는 E9.
- pack-tools (`cmd/pack-tools/*`) — E12.
- Docker Compose 번들 (`deploy/compose/*`) — E11.
- 실제 벤치마크 팩 (`packs/*.pack.tar.gz`) — E12 산출물.

## 전신 리포와의 연결

- 승계 대상 자산 Tier 분류: `docs/design/12-migration-and-non-goals.md` §12.2
- 벤치마크 마이그레이션 도구: `docs/design/12-*` §12.4 — Phase 1 실행 항목
- **원본 벤치마크 자료 참조 포인터**: [`contrib/source-benchmarks/README.md`](./contrib/source-benchmarks/README.md) —
  `nrobotcheck/resources/baselines/` 아래의 CIS·ROS2 베이스라인 JSON·SCAP XML의 정확한
  경로·크기·SHA-256·라이선스·타깃 팩을 정리한 포인터 문서. **파일 자체는 복사하지 않았고**,
  Phase 1 `pack-tools` 착수 시 여기부터 확인.
- 전신 리포 위치: `D:\robot\dev\nrobotcheck`
- 전신의 DDD 도메인 설계 참조 경로:
  - `nrobotcheck/docs/design/` — v2.0 DDD 설계
  - `nrobotcheck/src/domains/` — 실제 도메인 분해 사례
  - `nrobotcheck/docs/SESSION_HANDOFF.md` — 전신의 현재 상태

## 긴급 체크리스트 (뭔가 꼬였다 싶을 때)

- [ ] 원칙 12개 중 어느 것을 위반했나? (`docs/design/01-principles.md`)
- [ ] 비목표를 건드리고 있지 않나? (`docs/design/12-*` §12.7)
- [ ] 도메인 경계를 넘었나? (`docs/design/03-*` §3.1)
- [ ] `tenant_id` 빠진 테이블·API를 만들었나?
- [ ] Audit append-only를 깼나?
- [ ] LLM을 필수 경로로 만들었나?
