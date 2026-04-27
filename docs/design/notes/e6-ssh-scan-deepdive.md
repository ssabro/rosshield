# E6 SSH + Scan 사전 리서치 노트

> **상태**: Draft v0.1 (2026-04-27) — Agent 백그라운드 리서치 결과
> **범위**: `phase1-backlog.md` E6 (1.5주) — `internal/platform/sshpool/` + `internal/domain/scan/`. 핸드오프의 "이전 세션 휘발 사전 리서치"를 노트로 보존(이전엔 결과물이 보존되지 않았음).
> **선행 의존**: E5 Robot/Fleet 완료 후 진입.
> **참조**: §07.2·§07.3·§07.7 + 백로그 E6.T1~T10 + `e1-storage-deepdive.md`·`e1-eventbus-deepdive.md` 형식 답습.

> **ID 컨벤션 주의**: E1 = R1(storage)·R2(eventbus), E5 = R3, **E6 = R4**. 본 노트의 미해결 질문은 R4-1~R4-7로 명명(이전 초안에서 R3로 표기된 곳을 통일).

---

## §1 목적과 범위

E6은 본 제품의 **핵심 가치 기능**. 플릿 로봇 N대를 병렬·결정론적으로 감사하고 결과를 증거·리포트로 기록.

### Exit 기준 (백로그 line 348)

- 로컬 docker `linuxserver/openssh-server` 3대 fleet × CIS 팩 2~3 check end-to-end 실행·pass/fail 분류 성공.
- 메모리·goroutine 누수 없음 (`go test -race` + pprof 스팟체크).

### 핵심 서브시스템

- **SSH Pool** (`internal/platform/sshpool/`): 호스트·테넌트별 동시 연결 제한.
- **SSH Exec**: 명령 실행·타임아웃·stderr/stdout 분리·exit code 채취.
- **ScanSession FSM**: pending → running → completed/cancelled/failed.
- **CheckExecutor**: robot × check 병렬 실행, 진행률 이벤트 발행.
- **Evaluator 결선**: E4 sealed AST evaluator(`8ce9cea`) + execution result context.

---

## §2 SSH 라이브러리

| 항목 | `golang.org/x/crypto/ssh` | 대안(libssh, Paramiko-Go) |
|---|---|---|
| 성숙도 | Go 공식 stdlib 확장, RFC 준거 | 미성숙 또는 C 바인딩 |
| knownhosts 검증 | `ssh/knownhosts` 제공 | 수동 |
| 크로스 컴파일 | OK | C 의존성 문제 |
| Phase 1 적격 | ✅ | ❌ |

**결정**: `golang.org/x/crypto/ssh` 채택.

근거:
1. Go 공식 crypto 패밀리 — 보안 업데이트 보장.
2. 이미 `go.mod`에 `golang.org/x/crypto v0.50.0` 존재(E3 argon2 의존성).
3. `knownhosts` 서브패키지 풀 생태계.
4. CGO 없는 단일 바이너리 유지.

---

## §3 SSH Pool 설계

### 풀 아키텍처 후보

| 후보 | 구현 | 특징 | 선택 |
|---|---|---|---|
| A. `sync.Cond` 기반 세마포어 | 뮤텍스 + 조건 변수 + 대기 큐 | Go 표준, 결정론적 | ✅ 권장 |
| B. 버퍼 채널 pool | `make(chan *ssh.Client, max)` | 비동기 pull 단순 | ⚠️ 다중 기준 limit 어려움 |
| C. `golang.org/x/sync/semaphore` | weighted | 복잡도 중 | 대안 |

**권장 A**: 다중 기준(host·key·tenant) 검증 후 대기/할당이 원자적, waiters 큐 누수 검출 용이, E1 EventBus M2(per-entity goroutine) 철학과 정합.

### 풀 상태 머신

```
┌─────────────────┐
│  Idle Conns     │  ← keepalive 30s ping
├─────────────────┤
│  Active Conns   │  ← session 실행 중
├─────────────────┤
│  Waiters (ctx)  │  ← Cond.Wait()
└─────────────────┘
   └─ Backoff: jittered exponential 3회
```

### 동시 연결 제한 (계층)

```go
type PoolLimits struct {
    PerKey    int  // robot 자격증명당: 3 (default)
    PerHost   int  // host당: 5 (default)
    PerTenant int  // tenant당: 50 (default)
}
// 할당: tenant → host → key 순 검증, 통과 시 connection 또는 대기
```

### 함정과 방어선

| 함정 | 방어 |
|---|---|
| 한 session이 pool 전체 블록 | per-conn timeout + ctx deadline → close |
| ctx cancel 시 conn 누수 | defer cleanup + atomic isValid 플래그 |
| panic 격리 | executor 최상단 recover, pool은 panic 무지 |
| idle zombie | 5분 유휴 자동 close, heartbeat 실패 즉시 |
| known_hosts 부재 | 정책 결정 R4-2 |

---

## §4 SSH Exec 표면

### Session 1회용 원칙

`ssh.Session`은 **단일 명령 실행 용도**. 재사용 안 함.

```go
session, err := client.NewSession()
if err != nil { return }
defer session.Close()  // 필수

session.Stdout = ...
session.Stderr = ...
err = session.Run(cmd)  // cmd는 단일 string
```

### Argv 안전성 (쉘 파싱 금지)

SSH `exec` 채널은 **단일 문자열**만 전달. 클라이언트 split 금지.

**원칙**: 팩 정의 시 argv는 배열로 선언 → 애플리케이션이 쉘 거치지 않고 직렬화. 설계서 §06.8 "쉘 메타문자 화이트리스트, 벤치마크 팩에 선언된 argv만"과 정합.

```yaml
spec:
  auditCommand:
    argv: ["bash", "-c", "ls -la /boot && grep -i 'cramfs'"]
```

quoting helper 자체 구현(strings.Builder + escape) 또는 `mvdan.cc/sh/v3` 의존. 결정 R4-3.

### Stderr·Stdout 분리 + Exit Code + Cancel

```go
func (e *Executor) Exec(ctx context.Context, host string, argv []string, timeout time.Duration) (ExecResult, error) {
    client, release, err := e.pool.Acquire(ctx, keyID, host, tenantID)
    if err != nil { return ExecResult{}, err }
    defer release()

    session, err := client.NewSession()
    if err != nil { return ExecResult{}, err }
    defer session.Close()

    var stdout, stderr bytes.Buffer
    session.Stdout = &stdout
    session.Stderr = &stderr

    ctx, cancel := context.WithTimeout(ctx, timeout)
    defer cancel()

    done := make(chan error, 1)
    start := time.Now()
    go func() { done <- session.Run(joined(argv)) }()

    select {
    case <-ctx.Done():
        session.Close()  // force → Run() 에러 반환
        return ExecResult{Stdout: stdout.String(), Stderr: stderr.String(), Duration: time.Since(start)}, ctx.Err()
    case err := <-done:
        exit := 0
        if ee, ok := err.(*ssh.ExitError); ok { exit = ee.ExitStatus() }
        return ExecResult{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: exit, Duration: time.Since(start)}, nil
    }
}
```

**함정**:
- `session.Run()` 중간 cancel은 **원격 작업을 강제 중단하지 않음** (SSH 채널 close만; 원격 프로세스는 SIGHUP). → **timeout이 더 중요**.
- stdout/stderr 최대 크기 제한(설계서 §06.8: 기본 10MB). 무한 스트림 방지.
- exit code는 `*ssh.ExitError`에서만. 그 외 error는 execution failure → outcome `ERROR`.

---

## §5 Scan Orchestrator

### ScanSession FSM

```
[Pending] → [Running] → [Completed | Failed | Cancelled]
              ↓ (EventBus 발행)
            ScanStarted / ScanProgress(M/N) / ScanCompleted
```

### Worker Pool 구조

```go
type Orchestrator struct {
    session       *ScanSession
    checks        []CheckDef
    robots        []Robot
    pool          SSHPool
    evaluator     EvaluatorService  // E4
    evidenceStore EvidenceStore     // E7
    bus           eventbus.Bus       // E1.T6
    semaphore     *semaphore.Weighted // 동시 executor (default 10)
    queue         chan WorkItem
    resultsCh     chan CheckResult
}

// Run(ctx):
//  1. session.Status = pending → running, audit emit
//  2. queue 적재 (robot × check × Level 카티전 곱)
//  3. spawn N workers → 각자 queue pull → executeCheck → resultsCh push
//  4. EventBus publish ScanStarted
//  5. results 집계 + 주기 ScanProgress
//  6. all workers join → status = completed
//  7. EventBus publish ScanCompleted
```

### 병렬도 (E6.T6 검증)

- Worker pool 기본 10 — `runtime.NumCPU()` 또는 고정값. 결정 R4-4.
- SSH pool limits (host:5, tenant:50)에 의해 큐잉 — 한 호스트에서 동시 5개 한정.
- 50대 × 180 체크 = 9000 work item, 단일 체크 1초 가정 → ~900초.

### Cancel 전파 (E6.T9)

```go
func (o *Orchestrator) Cancel(ctx context.Context, sessionID string) error {
    // 1. session.Status = cancelled, audit emit
    // 2. context cancel → workers 다음 item skip
    // 3. 진행 중 SSH exec는 timeout까지 대기 (R4-5: 강제 중단 X)
    // 4. EventBus publish ScanCancelled
}
```

---

## §6 Evaluator 결선 (E4 재사용)

E4 sealed AST evaluator (`8ce9cea`) 그대로 재사용.

### 입력 컨텍스트 매핑

```go
// E4에서 제공:
type EvalInput struct {
    Stdout   string
    Stderr   string
    ExitCode int
}

type EvalResult struct {
    Status EvalStatus  // PASS | FAIL | INDETERMINATE
    Reason string
}

// E6 Executor에서 생성:
type CheckResult struct {
    SessionID  string
    RobotID    string
    CheckID    string
    Outcome    Outcome     // PASS | FAIL | INDETERMINATE | ERROR | SKIPPED
    EvalReason string
    Evidence   EvidenceRef // E7 sha256
    Duration   time.Duration
    ExecutedAt time.Time
}

// 5-값 매핑:
// PASS / FAIL / INDETERMINATE ← evaluator 결과 그대로
// ERROR ← Eval(in) error != nil 또는 SSH execution failure
// SKIPPED ← differential mode에서 hash match 시
```

### 호출 흐름

```go
func (e *Executor) executeCheck(ctx context.Context, robot Robot, def CheckDef) (CheckResult, error) {
    exec, execErr := e.sshExec(ctx, robot, def.Spec.AuditCommand)
    if execErr != nil {
        return CheckResult{Outcome: OutcomeError, EvalReason: execErr.Error()}, nil
    }
    evidence := e.evidenceStore.Store(exec.Stdout)  // E7 redaction + dedupe
    evalRes, evalErr := def.EvaluationRule.Eval(EvalInput{
        Stdout: exec.Stdout, Stderr: exec.Stderr, ExitCode: exec.ExitCode,
    })
    return CheckResult{
        Outcome:    toOutcome(evalRes.Status, evalErr),
        EvalReason: evalRes.Reason,
        Evidence:   evidence.Ref,
        Duration:   exec.Duration,
        ExecutedAt: e.clock.Now(),
    }, nil
}
```

---

## §7 테스트 전략

### 레이어별

| 테스트 | 구현 | 백로그 ID |
|---|---|---|
| 단위 (pool) | mock ssh.Client | E6.T1 (실제 sshd 통합) |
| 단위 (executor) | mock pool + fixture | E6.T2~T5 |
| 통합 (orchestrator) | docker-compose sshd + real executor | E6.T6~T10 |
| 부하 (pool stress) | `-race` enabled, pprof 스팟 | E6 exit |

### Docker testcontainer 선택

| 후보 | 장단 |
|---|---|
| A. `testcontainers-go` + `linuxserver/openssh-server` | Go 네이티브, compose 불필요 |
| B. docker-compose harness | E2E 환경 통합 |
| C. in-proc fake sshd (`gliderlabs/ssh`) | 빠르나 프로토콜 호환 위험 |

**권장 A + B 병행** — 단위/통합은 A, E2E는 B.

### Race Detector

CI line 39 `go test -race -count=1 ./...` — Linux/WSL만. Windows native `-race` 미지원. E6 goroutine·channel 많아 `-race` 필수.

---

## §8 의존 추가 후보

현재 `go.mod`:
- `golang.org/x/crypto v0.50.0` ✅ (E3에서 이미)

추가 가능성:

| 라이브러리 | 용도 | 결정 |
|---|---|---|
| `golang.org/x/term` | terminal (필요 시) | stdlib 확장이라 OK |
| `mvdan.cc/sh/v3` | shell quoting | 자체 구현으로 회피 — R4-3 결정 후 |
| `testcontainers-go` | E6.T1 통합 테스트 | E6 착수 시점에 추가 (R4-* 별개로 의존 추가 자체 합의 필요) |
| `gliderlabs/ssh` | in-proc fake sshd | testcontainers-go가 더 정확 → 회피 |

---

## §9 인터페이스 시그니처 초안

### `internal/platform/sshpool`

```go
package sshpool

type PoolConfig struct {
    PerKeyConnLimit    int           // default 3
    PerHostConnLimit   int           // default 5
    PerTenantConnLimit int           // default 50
    IdleTimeout        time.Duration // default 5m
    DialTimeout        time.Duration // default 10s
    KeepAliveInterval  time.Duration // default 30s
    KnownHostsPath     string        // <dataDir>/keys/known_hosts
}

type KeyID string  // robot 자격증명 hash 식별자

type Pool interface {
    // 제한 도달 시 ctx 만료까지 대기. release()는 idempotent.
    Acquire(ctx context.Context, keyID KeyID, host string, tenantID string) (*ssh.Client, func(), error)
    Close() error
}

type Executor interface {
    Exec(ctx context.Context, host string, argv []string, timeout time.Duration) (ExecResult, error)
}

type ExecResult struct {
    Stdout, Stderr string
    ExitCode       int
    Duration       time.Duration
}
```

### `internal/domain/scan`

```go
package scan

type ScanSession struct {
    ID, TenantID, FleetID, PackID string
    Status                        SessionStatus
    CreatedAt, StartedAt, CompletedAt time.Time
    Progress                      SessionProgress
}

type SessionStatus string
const (
    StatusPending   SessionStatus = "pending"
    StatusRunning   SessionStatus = "running"
    StatusCompleted SessionStatus = "completed"
    StatusFailed    SessionStatus = "failed"
    StatusCancelled SessionStatus = "cancelled"
)

type SessionProgress struct{ Total, Completed, Failed int }

type Orchestrator interface {
    Run(ctx context.Context, session *ScanSession, robots []Robot, checks []CheckDef) error
    Cancel(ctx context.Context, sessionID string) error
}

type CheckResult struct {
    SessionID, RobotID, CheckID string
    Outcome                     Outcome
    EvalReason                  string
    EvidenceRef                 string  // E7 sha256
    Duration                    time.Duration
    ExecutedAt                  time.Time
}

type Outcome string
const (
    OutcomePass          Outcome = "pass"
    OutcomeFail          Outcome = "fail"
    OutcomeIndeterminate Outcome = "indeterminate"
    OutcomeError         Outcome = "error"
    OutcomeSkipped       Outcome = "skipped"
)
```

---

## §10 미해결 질문 (R4-1 ~ R4-7)

| ID | 질문 | 권장값 | 결정 시점 |
|---|---|---|---|
| **R4-1** | Pool에서 SSH 자격증명 메모리 관리 | 옵션 1: session당 decrypt 1회, 그 context 내 캐시·재사용. Phase 2에서 OS keychain 검토 | E6 착수 전 |
| **R4-2** | Known_hosts 정책 (호스트 키 검증) | 옵션 A: first-touch trust + DB 기록 + 불일치 시 즉시 실패(설계서 §06.8과 정합). Phase 2 tenant별 정책 | E6 착수 전 |
| **R4-3** | Argv quoting helper | 옵션 C: 팩이 책임(`bash -c "..."`), 본 리포는 validation·길이 제한만. 자체 quote helper 최소 | E6.T4 착수 전 |
| **R4-4** | Worker pool size 기본값 | 옵션 B: 고정 10. E6.T6 실측 후 config 항목 도입 검토 | E6 exit 후 |
| **R4-5** | ScanSession cancel 의미론 | 옵션 A: timeout만 믿고 진행 중 체크는 완료대기, 다음 item 스킵. 강제 중단 미지원 | E6.T9 착수 전 |
| **R4-6** | Evidence redaction 기본 활성화 | 옵션 A: default on (P10 프라이버시 기본값). E7에서 구현 | E7 착수 전 |
| **R4-7** | Differential scan 정확성 | 옵션 A: hash match = reuse(성능 우선), §07.9 "프리플라이트 불가능 체크 제외" 플래그로 부분 완화 | Phase 1 후반 |

---

## §11 Phase 1 로드맵 요약

| 항목 | 권장 | 회피 | 의존 결정 |
|---|---|---|---|
| SSH 라이브러리 | `golang.org/x/crypto/ssh` | CGO/libssh | — |
| Pool 구현 | sync.Cond 세마포어 | channel pool | R4-1 |
| Session lifecycle | 1회용 close 후 재생성 | 재사용 | — |
| Argv 처리 | 팩 책임 + validation | mvdan.cc/sh | R4-3 |
| Worker pool | 10 고정 | 동적 scaling | R4-4 |
| Cancel | 진행 중 완료대기 | 강제 중단 | R4-5 |
| Evaluator | E4 EvalInput 재사용 | 새 정의 | — |
| Testcontainer | linuxserver/openssh-server | gliderlabs/ssh | — |
| Evidence redact | default on | default off | R4-6 |
| Differential | hash match reuse | 재검증 샘플 | R4-7 |

---

## §12 다음 단계

1. **E5 진행 중** — Robot/Fleet 도메인 (4일).
2. E5 종료 후 본 노트의 R4-1~R4-7 사용자 합의.
3. E6.T1~T2 (pool + exec 단위) → ~1주.
4. E6.T6~T10 (orchestrator + E4/E7 결선) → ~1주.

총 1.5주 (백로그 일정 부합).
