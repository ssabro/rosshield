# Phase 1 백로그 — Core MVP

> **상태**: Draft v0.1 (2026-04-23)
> **범위**: `11-tech-stack-and-roadmap.md` §Phase 1 체크리스트를 **TDD 단위 태스크**로 분해
> **목표(§11.13)**: Tenant 1개·Fleet 1개·로봇 N대를 CIS Ubuntu 팩으로 감사하고 **서명된 PDF 리포트** 생성
> **Exit 기준**: 내부 환경 로봇 3대 감사 → 서명 PDF → **감사 체인 외부 검증** 성공
> **예상 기간**: 10~12주 (§11.14)

## 읽는 방법

- 의존 그래프대로 **위에서 아래로** 구현합니다. 같은 레벨의 에픽은 병렬 가능.
- 각 에픽은 `Epic E<N>.<NAME>` 형식. 태스크는 `E<N>.T<k>` 식별자.
- 태스크는 **Red(테스트 먼저) → Green(최소 구현) → 필요 시 Refactor**. 공개 API 변경·도메인 로직은 테스트 없이 커밋 금지(CLAUDE.md §TDD).
- 각 에픽이 **commit-able 단위** — 테스트 녹색 + `gofmt` + `go vet` + `golangci-lint` 통과 후 커밋.
- 파일·함수 크기 규칙(§11.12): ≤400줄 권장 / ≤50줄 함수 / 순환 복잡도 ≤10.

## 의존 그래프

```
              ┌─────────────────┐
              │  E1 Platform L1 │  (Logger·Clock·IDGen·Storage·EventBus·Signer·Scheduler)
              └────────┬────────┘
                       │
         ┌─────────────┼───────────────┐
         ▼             ▼               ▼
   ┌──────────┐  ┌──────────┐    ┌──────────┐
   │ E2 Audit │  │ E3 Tenant│    │ E4 Pack  │
   │  (교차)  │  │  / Auth  │    │   시스템 │
   └────┬─────┘  └────┬─────┘    └────┬─────┘
        │             │               │
        │             ▼               │
        │       ┌──────────┐          │
        │       │ E5 Robot │◀─────────┘
        │       │  / Fleet │
        │       └────┬─────┘
        │            ▼
        │      ┌──────────────┐
        │      │ E6 SSH + Scan│
        │      └──────┬───────┘
        │             ▼
        │      ┌──────────────┐
        │      │  E7 Evidence │
        │      └──────┬───────┘
        │             ▼
        │      ┌──────────────┐
        └─────▶│ E8 Reporting │ (서명 PDF)
               └──────┬───────┘
                      │
           ┌──────────┴──────────┐
           ▼                     ▼
    ┌──────────┐          ┌──────────┐
    │  E9 CLI  │          │ E10 Web  │
    └────┬─────┘          │    UI    │
         │                └────┬─────┘
         └──────────┬──────────┘
                    ▼
            ┌────────────────┐
            │ E11 Compose 번들│
            └────────┬────────┘
                     ▼
            ┌─────────────────┐
            │ E12 pack-tools  │ (nrobotcheck 자산 변환)
            └─────────────────┘
```

## 에픽 요약

| # | 에픽 | 기간 | 의존 | 대표 산출물 |
|---|---|---|---|---|
| E1 | Platform L1 | 1주 | — | `internal/platform/{logger,clock,idgen,storage,eventbus,signer,scheduler}` |
| E2 | Audit 교차 | 3일 | E1 | `internal/domain/audit` — 해시 체인 append-only, 외부 검증 API |
| E3 | Tenant/Auth | 1주 | E1, E2 | `tenant/user/role/session/apikey`, JWT(Ed25519), argon2id |
| E4 | Pack 시스템 | 1주 | E1, E2 | Pack 로더·Ed25519 검증·Self-Test 런너·생명주기 FSM |
| E5 | Robot/Fleet | 4일 | E3 | `robot/fleet` CRUD, Credential KEK/DEK 암호화 |
| E6 | SSH + Scan | 1.5주 | E4, E5 | SSH Pool, ScanSession, CheckExecutor, evaluationRule 엔진 |
| E7 | Evidence | 3일 | E6 | Content-addressed Blob Store(파일어댑터), sha256 중복제거 |
| E8 | Reporting | 1주 | E6, E7 | PDF 생성(gofpdf), 리포트 서명, `verify` API |
| E9 | CLI | 4일 | E3, E5, E6, E8 | `rosshield login/robot/scan/report` |
| E10 | Web UI | 2주 | E3, E5, E6, E7, E8 | React 19 + Overview/Fleet/Robot/Scan/Report |
| E11 | Compose 번들 | 2일 | E1~E10 | `deploy/compose/docker-compose.yml` + 초기화 |
| E12 | pack-tools | 1주 | E4 | `cmd/pack-tools convert` (nrobotcheck CSV/JSON → 팩) |

합계 11.5주 + 0.5주 범퍼 = 12주 (§11.14 가정과 일치).

---

## E1. Platform L1 — 공통 인프라 (1주)

### 왜

모든 도메인이 공통으로 쓰는 L1 서비스. 여기가 흔들리면 상위 레이어가 모두 흔들립니다(§03.1). 인터페이스를 확정해 나중에 교체(SQLite↔PG, inproc↔NATS, soft↔TPM)가 가능하게.

### 인터페이스 (요지)

```go
package platform

type Logger interface { Info/Warn/Error(ctx, msg, fields ...) }
type Clock  interface { Now() time.Time }
type IDGen  interface { New(prefix string) string }         // ULID base32
type Storage interface {
    Tx(ctx, fn func(Tx) error) error
    Migrate(ctx, migrations []Migration) error
}
type EventBus interface {
    Publish(ctx, evt Event) error
    Subscribe(topic string, h Handler) Subscription
}
type Signer interface {
    Sign(payload []byte) (sig []byte, keyId string, err error)
    Verify(payload, sig []byte) error
    PublicKey() []byte
}
type Scheduler interface {
    Schedule(id, spec string, job func(ctx context.Context) error) error
    Cancel(id string)
}
```

### TDD 태스크

| ID | 테스트 (Red) | 최소 구현 (Green) |
|---|---|---|
| E1.T1 ✅ | `TestSlogJSONIncludesContextFields` — `tenantId`·`requestId`·`traceId`가 로그에 실림 | `internal/platform/logger`: `slog.JSONHandler` + `WithContext` wrapper (commit `b67b2c1`) |
| E1.T2 ✅ | `TestClockInjectableFake` — test에서 고정 시간 주입 | `Clock` 인터페이스 + `FakeClock` (commit `d9ee1c1`) |
| E1.T3 ✅ | `TestIDGenPrefixAndLength` — `ro_`·`ss_`·`au_` 접두사 + Crockford base32 26자 | `idgen.ULID` (commit `81ded88`) |
| E1.T4 ✅ | `TestStorageTxCommitAndRollback` — commit 후 조회·rollback 후 미존재 | `storage/sqlite`: `modernc.org/sqlite` + `database/sql` Tx wrapper (commit `b1af50d`+`d8f3034`) |
| E1.T5 ✅ | `TestStorageMigrateIdempotent` — 같은 마이그레이션 2번 적용해도 OK | goose v3 + embed.FS + flock OS lock (commit `980d6f9`) |
| E1.T6 ✅ | `TestEventBusInProcPublishAndSubscribe` — publish→subscriber 수신, causationId 보존 | `eventbus/inproc`: per-sub channel + M2 worker (commit `d97ff1f`) |
| E1.T7 ✅ | `TestEventBusHandlerErrorIsolated` — 한 구독자 패닉이 다른 구독자·publish에 영향 없음 | `defer recover()` + error log (commit `d97ff1f`) |
| E1.T8 ✅ | `TestSignerEd25519SignVerifyRoundtrip` + `TestSignerRejectsTamperedPayload` | `signer/soft`: `crypto/ed25519` + 메모리 키 (commit `950cd3a`) |
| E1.T9 | `TestSchedulerFiresAtSpec` — `@every 1s` spec 2회 발화 확인 (Clock 모의) | `scheduler/robfig`: `robfig/cron/v3` |

### Exit 기준

- 9개 테스트 녹색, 패키지 커버리지 ≥ 80%.
- `cmd/rosshield-server/main.go`가 Platform bootstrap 시퀀스로 모든 서비스 초기화.
- SQLite 파일 DB(`~/.rosshield/data.db`) 생성 및 첫 마이그레이션 적용.
- 구조화 로그가 stdout에 JSON 한 줄씩 출력됨.

### 설계 참조

§03.1, §03.4 시작 시퀀스, §03.12 동시성, §11.3 Go 라이브러리 선택.

**사전 설계 노트** (E1.T4/T5, E1.T6/T7 착수 전 필독):
- [`notes/e1-storage-deepdive.md`](./notes/e1-storage-deepdive.md) — 드라이버 선택, PRAGMA, 마이그레이션, Tx 추상, 테넌시 격리, 테스트 전략
- [`notes/e1-eventbus-deepdive.md`](./notes/e1-eventbus-deepdive.md) — 채널 모델, backpressure, panic 격리, audit 통합, future NATS 호환

---

## E2. Audit 도메인 — 해시 체인 (3일)

### 왜

**모든 WRITE 경로가 감사 엔트리를 append**해야 하므로, 다른 도메인보다 먼저 스키마·append 경로가 존재해야 합니다. §10.1·§10.2·§10.4.

### 인터페이스

```go
package audit

type Entry struct {
    TenantID, Seq         string   // Seq는 tenant 내 단조
    OccurredAt            time.Time
    Actor                 Actor
    Action                string   // "robot.create" ...
    Target                Target
    PayloadDigest         [32]byte // sha256 canonical JSON
    Outcome               string   // success|failure|partial
    PrevHash, Hash        [32]byte
}

type Service interface {
    Append(ctx, req AppendRequest) (Entry, error)
    Head(ctx, tenantID string) (ChainHead, error)
    Verify(ctx, tenantID, from, to string) (VerifyResult, error)
    Export(ctx, tenantID string, format Format) (io.ReadCloser, error)
}
```

### TDD 태스크

| ID | 테스트 | 구현 |
|---|---|---|
| E2.T1 | `TestAppendInitializesGenesis` — 첫 엔트리의 `prevHash == 0x00..00` | genesis 처리 |
| E2.T2 | `TestAppendChainsHashes` — `hash_i = sha256(prevHash ‖ payloadDigest ‖ meta)` | SHA-256 계산 |
| E2.T3 | `TestAppendRejectsDuplicateSeq` — `(tenantId, seq)` UNIQUE | DB 제약 + 오류 mapping |
| E2.T4 | `TestAppendIsAppendOnly` — `UPDATE`·`DELETE` 시도 시 trigger가 막음 | SQLite trigger (또는 migration에 명시) |
| E2.T5 | `TestVerifyDetectsChainBreak` — 중간 엔트리 payloadDigest 조작 시 실패 지점 감지 | 재계산 루프 |
| E2.T6 | `TestVerifyAcceptsCleanRange` | 클린 경로 |
| E2.T7 | `TestExportNDJSONIncludesSignature` — 내보내기에 공개키·서명 포함 | gzip NDJSON + SIGNATURE |
| E2.T8 | `TestCheckpointSignedEveryHour` (Scheduler 연동) | Signer 호출 + CheckpointSignature 저장 |

### Exit 기준

- 체인 무결성·append-only·seq 유일성이 테스트로 보장.
- `POST /api/v1/audit/verify`가 구간 검증 결과 반환 (실제 라우트 등록은 E9 API Gateway에서).
- 백그라운드 checkpoint 서명 잡이 Scheduler에 등록됨.

### 설계 참조

§01 P1·P9, §10.2~§10.9.

---

## E3. Tenant / Auth (1주)

### 왜

멀티테넌시는 **기본값**(P4). 단일 사용자 데스크톱도 "기본 테넌트 1개"의 degenerate case. 인증 없이는 어떤 WRITE API도 못 받습니다.

### 스코프

- 도메인: `tenant`, `user`, `role`, `apikey`, `session`.
- 인증: 로컬 계정(argon2id) + API Key(argon2id, 해시 저장) + JWT(Ed25519).

### TDD 태스크

| ID | 테스트 | 구현 |
|---|---|---|
| E3.T1 | `TestCreateTenantEmitsAudit` | `TenantService.Create` + audit.Append |
| E3.T2 | `TestUserArgon2PasswordVerify` | argon2id 파라미터(m=64MB, t=3, p=1) 고정 |
| E3.T3 | `TestLoginIssuesJWTWithTenantAndRoles` — access 15m, refresh 14d | JWT Ed25519 서명 |
| E3.T4 | `TestJWTRejectsExpiredAndInvalidSignature` | `jwt/v5` 검증 |
| E3.T5 | `TestApiKeyHashIsArgon2idAndPrefixVisible` — 해시 저장, `fg_live_xxxxx` prefix만 보임 | argon2id + prefix 추출 |
| E3.T6 | `TestApiKeyRevokeIsSoftDelete` — `revokedAt` 설정, 레코드 유지 | UPDATE revokedAt |
| E3.T7 | `TestRBACPermissionCheck` — `robot.write` 없으면 403 | permission set intersection |
| E3.T8 | `TestTenantScopeBlocksCrossTenantRead` — A 테넌트 사용자가 B의 리소스 조회 시 404(의도적, 존재 누설 방지) | Repository 레이어에서 tenant filter |

### Exit 기준

- `POST /api/v1/auth/login` 으로 토큰 발급.
- JWT에 `sub`·`tid`·`roles`·`exp` 포함, `EdDSA` 서명.
- cross-tenant fuzzer 테스트가 모든 repo 메서드에서 0 leak.

### 설계 참조

§04.2 Tenant/User/ApiKey, §05.7~§05.9, §06 전반.

---

## E4. Pack 시스템 (1주)

### 왜

벤치마크는 **서명된 외부 콘텐츠**(§01 P8). 설치→스테이지→활성화 생명주기와 **Self-Test 프레임워크**가 없으면 팩 품질 문제(§12.8 R3 — 치명 리스크)가 제품 가치를 훼손합니다.

### 스코프

- `internal/domain/benchmark`
- `pack.yaml`·`checks/*.yaml`·`SIGNATURE` 파싱
- Ed25519 manifest 서명 검증
- Self-Test fixture 러너 (in-memory 실행, SSH 없이)
- 생명주기 FSM: Install → Staged → Active → Archived → Removed

### TDD 태스크

| ID | 테스트 | 구현 |
|---|---|---|
| E4.T1 | `TestPackLoadsValidManifest` — pack.yaml + checks YAML 역직렬화 | `go.yaml.in/yaml/v4` |
| E4.T2 | `TestPackRejectsInvalidSignature` | tar 해제 → manifest 재계산 → Ed25519 검증 |
| E4.T3 | `TestPackRejectsHashMismatch` — checks/*.yaml 해시가 manifest와 다르면 거부 | SHA-256 대조 |
| E4.T4 | `TestCheckDefinitionSchemaValidation` — 필수 필드 누락 시 loader 오류 | `santhosh-tekuri/jsonschema` |
| E4.T5 | `TestEvaluationRuleExpressionSafeSubset` — `eval()`·`require` 등 금지 토큰 거부 | 화이트리스트 AST walker |
| E4.T6 | `TestSelfTestFixturePassAndFail` — stdout/exit 코드 fixture로 pass/fail 판정 검증 | `selftest` 러너 |
| E4.T7 | `TestPackLifecycleFSM` — Install→Activate→Deactivate→Archive 상태 전이 + 불법 전이 거부 | FSM 구조체 |
| E4.T8 | `TestPackInstallIsAudited` | `benchmark.pack.install` audit event |

### Exit 기준

- 샘플 팩(picosize, 2 checks)이 설치·활성화·Self-Test 통과.
- 변조된 팩은 **로드 거부** + 감사 엔트리 기록.
- Pack 스토리지 디렉터리 규약 확정(§07.1 트리 구조).

### 설계 참조

§07.2 pack.yaml, §07.3 CheckDefinition, §07.5 서명, §07.6 Self-Test, §07.13 nrobotcheck 승계.

---

## E5. Robot / Fleet (4일)

### 왜

스캔의 대상. Fleet은 정책 그룹. Credential은 **암호화 필수**(§06).

### TDD 태스크

| ID | 테스트 | 구현 |
|---|---|---|
| E5.T1 | `TestCreateFleetWithDefaultPolicy` | `FleetService.Create` |
| E5.T2 | `TestCreateRobotRequiresFleet` — 존재하지 않는 fleet ID 거부 | FK 제약 + 도메인 검증 |
| E5.T3 | `TestRobotCredentialEncryptedAtRest` — DB 원본이 plaintext 아님 | KEK/DEK AES-256-GCM |
| E5.T4 | `TestRobotCredentialRotateIsAudited` | rotate API + audit |
| E5.T5 | `TestTestConnectionUsesSSHPool` (mocked SSH) — 실제 SSH 호출은 E6에서 | SSH 클라이언트 interface mock |
| E5.T6 | `TestRobotCSVImportValidates` — 잘못된 IP·포트 거부 | csv → validation |
| E5.T7 | `TestRobotDeleteKeepsAuditReferences` — 삭제 후에도 `scan_results.robot_id`는 참조 가능 | soft delete (`deletedAt`) |

### Exit 기준

- Robot CRUD 전부 audit-wrapped.
- Credential 평문이 DB 파일·로그에 노출되지 않는다(grep 검증).

### 설계 참조

§04.2 Fleet/Robot/Credential, §06 비밀 관리.

---

## E6. SSH + Scan 엔진 (1.5주)

### 왜

제품의 **핵심 가치 기능**. 체크 병렬 실행, SSH 풀 관리, 결정론적 평가.

### 스코프

- `internal/platform/sshpool` — 풀 관리, 키·호스트·테넌트별 limit
- `internal/domain/scan` — Session·Result·Executor
- `evaluationRule` 엔진 (E4에서 파서 생성; 여기서 실행)

### TDD 태스크

| ID | 테스트 | 구현 |
|---|---|---|
| E6.T1 | `TestSSHPoolRespectsHostLimit` (docker-based testcontainer로 실제 sshd) | `golang.org/x/crypto/ssh` + sync.Cond |
| E6.T2 | `TestSSHExecReturnsStdoutStderrExitCode` | `session.CombinedOutput` 래핑 |
| E6.T3 | `TestSSHExecTimeoutCancels` — 10초 timeout 후 channel close | context + deadline |
| E6.T4 | `TestSSHExecArgvNotShellParsed` — argv 슬라이스 그대로 전달 | quote 없음, shell 개입 없음 |
| E6.T5 | `TestScanSessionCreatesPendingResultPerRobotCheck` | fan-out |
| E6.T6 | `TestScanExecutorParallelWithinLimits` | semaphore |
| E6.T7 | `TestEvaluationRuleStdoutMatch` (fixture) | 정규식 기반 |
| E6.T8 | `TestEvaluationRuleExpressionComposite` | AND/OR/NOT |
| E6.T9 | `TestScanCancelStopsInFlight` | context cancellation |
| E6.T10 | `TestScanEmitsEventsAtMilestones` — `ScanStarted`·`ScanProgress`·`ScanCompleted` | EventBus publish |

### Exit 기준

- 로컬 docker `linuxserver/openssh-server` 3대 fleet에 대해 CIS 팩 2~3 check가 end-to-end 실행·pass/fail 분류 성공.
- 메모리·goroutine 누수 없음(`go test -race` + `pprof` 스팟체크).

### 설계 참조

§07.7 SSH, §07.3 evaluationRule, §03.12 동시성.

---

## E7. Evidence Store (3일)

### 왜

**원본 증거**(stdout·파일 스냅샷)는 결정론·감사의 근거(§01 P1). 중복제거·불변성 필요.

### TDD 태스크

| ID | 테스트 | 구현 |
|---|---|---|
| E7.T1 | `TestStoreAssignsSHA256AndDeduplicates` | content-addressed |
| E7.T2 | `TestStoreRefcountIncrementsOnRepeat` | `evidence_refs` 테이블 |
| E7.T3 | `TestStoreReadReturnsByHash` | fs 어댑터: `<hash[0:2]>/<hash[2:4]>/<hash>.blob` |
| E7.T4 | `TestStoreRejectsCorruption` — 읽기 시 해시 재계산 후 불일치면 오류 | 읽기 검증 |
| E7.T5 | `TestStoreReductionRemovesKnownSecrets` — `password=`·`Authorization: Bearer ...` 마스킹 | pattern engine (로거와 공유) |

### Exit 기준

- 파일시스템 어댑터 구현. S3 인터페이스는 정의만, Phase 3.
- Evidence → ScanResult N:M 매핑이 감사 체인에 해시로 고정됨.

### 설계 참조

§04.2 EvidenceRecord, §07.8 Evidence Store, §10.12 Redaction.

---

## E8. Reporting — 서명 PDF (1주)

### 왜

**감사인이 받아들이는 증거** 자체(§01 P1). Phase 1 Exit 조건이 "서명 PDF + 외부 검증 성공"인 이유.

### 스코프

- PDF 생성: `go-pdf/fpdf` 또는 대안 평가
- 리포트 템플릿 (팩에서 제공 가능, §07.1)
- Ed25519 detached signature → `report.pdf` + `report.pdf.sig`
- `POST /reports/{id}:verify` — 서명 검증 + audit 체인 교차 확인

### TDD 태스크

| ID | 테스트 | 구현 |
|---|---|---|
| E8.T1 | `TestReportBuildIncludesSessionSummary` — 스캔 세션의 pass/fail/error counts | builder |
| E8.T2 | `TestReportPDFContainsSignatureBlock` — PDF 말미에 `X-Signature-KeyId`·`X-Signature-Base64` | custom PDF trailer |
| E8.T3 | `TestReportVerifyAcceptsClean` | Signer.Verify |
| E8.T4 | `TestReportVerifyRejectsTamperedPage` | PDF 해시 변경 후 검증 실패 |
| E8.T5 | `TestReportIncludesAuditChainAnchor` — 리포트가 참조한 audit seq의 hash 포함 | audit.Head 참조 |
| E8.T6 | `TestReportGenerationIsAudited` | `reporting.generate`·`reporting.sign` |

### Exit 기준

- 샘플 3대 로봇 × CIS 팩 세션 → 리포트 PDF 생성·서명.
- `rosshield report verify <path>` CLI 명령으로 오프라인 검증 성공.
- 리포트 안에 `verificationUrl`(audit verification token 포함) 포함.

### 설계 참조

§04.2 Report/ReportSignature, §07.1 templates, §10.5 Checkpoint.

---

## E9. CLI (4일)

### 스코프

바이너리 이름: `rosshield`. HTTP API 호출만 하는 얇은 클라이언트(§01 P7, §09 Clients).

### 명령어

```
rosshield login [--server URL] [--email] [--password]
rosshield whoami
rosshield robot list [--fleet ID]
rosshield robot add --host --user --key-file
rosshield scan run --fleet|--robots --pack --level L1|L2
rosshield scan status <scanId>
rosshield report list|get|verify <path|id>
rosshield audit verify [--from --to]
```

### TDD 태스크

| ID | 테스트 | 구현 |
|---|---|---|
| E9.T1 | `TestLoginStoresTokenInConfig` | `~/.rosshield/config.yaml` (chmod 600) |
| E9.T2 | `TestScanRunStreamsProgressViaWebSocket` | `nhooyr.io/websocket` 클라이언트 |
| E9.T3 | `TestReportVerifyOfflineNeedsNoServer` | PDF + 서명 공개키 bundled |
| E9.T4 | `TestCLIOutputFormatJSONOrTable` — `-o json` 플래그 | `text/tabwriter` vs JSON |
| E9.T5 | `TestCLIRespectsHTTPError` — API 4xx/5xx 시 적절한 exit code | exit codes 0/1/2/3 |

### Exit 기준

- `rosshield scan run` 으로 전체 파이프라인 트리거 가능.
- 서명 PDF 오프라인 검증 명령 존재.

### 설계 참조

§05 API 전체, §09 Clients.

---

## E10. Web UI (2주)

### 스코프

- React 19 + Vite + Tailwind v4 + shadcn/ui
- 페이지: Overview / Fleets / Fleet 상세 / Robots / Robot 상세 / Scan 새로 / Scan 상세 / Reports
- 인증: JWT 저장 (in-memory + refresh 쿠키 HttpOnly)
- 타입: OpenAPI에서 생성 (`openapi-fetch` + 생성 타입)

### TDD 태스크 (프론트엔드는 컴포넌트 단위 테스트 + Playwright E2E)

| ID | 테스트 | 구현 |
|---|---|---|
| E10.T1 | `LoginPage.test.tsx` — 잘못된 자격증명 시 에러 메시지 | Vitest + RTL |
| E10.T2 | `OverviewPage` — 스캔 상태 카드 렌더 | mock API |
| E10.T3 | `ScanDetailPage` — WS 이벤트로 진행률 업데이트 | MSW + websocket mock |
| E10.T4 | Playwright E2E — login → new scan → watch progress → open report | docker-compose harness |
| E10.T5 | i18n — `i18next` ko/en 번들 분할 동작 | Vitest |

### Exit 기준

- 모든 핵심 흐름 수동 클릭으로 동작.
- Playwright E2E 녹색 (헤드리스).
- 번들 크기 게이트 (초기 ≤ 500KB gzipped).

### 설계 참조

§09 Clients, §11.4 TS 라이브러리.

---

## E11. Compose 번들 (2일)

### 스코프

```
deploy/compose/
  ├─ docker-compose.yml
  ├─ .env.example
  ├─ README.md
  └─ init/
      ├─ seed-tenant.sql         # 기본 테넌트·admin 사용자 생성
      └─ install-system-pack.sh
```

### TDD 태스크

| ID | 테스트 | 구현 |
|---|---|---|
| E11.T1 | `TestComposeUpSmoke` (scripts/compose-smoke.sh) — up → healthz 200 → down | shell 스크립트 + CI job |
| E11.T2 | `TestFirstBootSeedsAdmin` — 초회 기동 시 admin 계정 발급 토큰 출력 | init 스크립트 |
| E11.T3 | `TestPersistenceSurvivesRestart` — volume 마운트 검증 | named volume |

### Exit 기준

- `docker compose up`으로 5분 안에 완전 기동.
- 기본 admin 계정 생성 + 1회성 토큰 stdout 출력.
- `docker compose down && up` 후 데이터 유지.

### 설계 참조

§02.2 배포 타깃 2(온프렘).

---

## E12. pack-tools — nrobotcheck 자산 변환 (1주)

### 왜

Phase 1 Exit는 "CIS Ubuntu 팩으로 감사"를 요구. 팩을 바닥부터 작성하는 건 비현실. 전신 자산(`nrobotcheck/resources/baselines/`)이 이미 검증된 매핑을 담고 있음 — §12.4.

### 스코프

```
cmd/pack-tools/
  ├─ main.go
  └─ convert/        # nrobotcheck-baseline-v1 → rosshield pack
```

### TDD 태스크

| ID | 테스트 | 구현 |
|---|---|---|
| E12.T1 | `TestConvertCISUbuntu2404Baseline` — 1,138KB JSON → N개 CheckDefinition YAML | 역직렬화 + 재직렬화 |
| E12.T2 | `TestConvertROS2JazzyFrameworkV1_1` — 1,348KB JSON → YAML | 동일 패턴 |
| E12.T3 | `TestPassLogicTreeToExpression` — `{and:[{equals:"ok"},{contains:"foo"}]}` → `stdout.equals("ok") && stdout.contains("foo")` | AST 변환기 |
| E12.T4 | `TestConvertedPackLoadsInBenchmarkLoader` — E4 로더가 결과 팩을 수용 | 통합 테스트 |
| E12.T5 | `TestSelfTestSkeletonGenerated` — fixture 없는 degraded check 목록 산출 | `selftest/cases.yaml` 자동 생성 |
| E12.T6 | `TestConvertedPackSignedAndVerifies` | Signer 호출 |

### Exit 기준

- CIS Ubuntu 24.04 + ROS2 Jazzy 변환 팩이 로더 통과.
- 변환 품질 리포트: N checks 변환, M fixture 미비(degraded).

### 설계 참조

§07.13 nrobotcheck 승계, §12.4 변환 도구 스펙, `contrib/source-benchmarks/README.md`.

---

## 공통 규약

### 도메인 폴더 구조 (§03.8)

```
internal/domain/<name>/
  ├─ model/          # 불변 값·엔터티
  ├─ repository/     # 저장소 인터페이스 + SQLite 구현
  ├─ service/        # 유스케이스
  ├─ policy/         # 도메인 규칙·검증
  ├─ event/          # 이벤트 타입
  ├─ api/            # HTTP/WS 라우트 (OpenAPI 생성물 연결)
  └─ (테스트는 같은 폴더 내 `*_test.go`)
```

### 도메인 간 호출 규약 (§03.1, §01 P5)

- 도메인 서비스는 **다른 도메인의 저장소를 직접 호출하지 않는다**.
- 경유지: (a) EventBus 발행·구독, (b) L3 Application Service(`internal/app/...`).
- 위반 시 `depguard` 린트가 빌드 실패로 차단 (Phase 1 초기에 규칙 추가).

### 감사 엔트리가 필요한 모든 WRITE 작업 (체크리스트)

tenant/user/apikey/robot/fleet/credential/pack(설치·활성화)/scan(시작·취소)/evidence(저장은 로그만)/report(생성·서명)/llm/retention 설정 변경.

### 커밋 규약 (CLAUDE.md 재확인)

- `<type>(<scope>): <한글 제목>` — type ∈ {feat, fix, refactor, test, docs, build, ci, chore, design}
- 본문 `## 추가/변경`, `## 테스트`, `## 결정·근거`
- Co-Author 라인 없음, Remote push는 명시 요청 시에만.

---

## 리스크 및 완화 (Phase 1 한정)

| 리스크 | 완화 |
|---|---|
| SSH 풀 동시성 버그 | testcontainer 기반 실제 sshd 부하 테스트 (`-race` CI) |
| 해시 체인 구현 오류 → 감사 실패 | 속성 기반 테스트(property-based, `testing/quick` 또는 `rapid`) + 외부 `fg-verify` 단위 테스트 |
| 팩 Self-Test 미비로 오탐 | E12에서 degraded 마커 + 수동 fixture 확충 |
| PDF 라이브러리 선택 오류 | E8 초기 1일은 gofpdf vs unidoc vs pdfcpu 비교 스파이크 |
| OpenAPI spec drift (`docs/design/phase1-backlog.md` ↔ `openapi/openapi.yaml`) | 설계 변경 시 같은 커밋에서 스펙 갱신 + spectral CI (Phase 1 초기 도입) |

## Phase 1 Exit 체크리스트 (`12-*` §12.9 재확인)

- [ ] 내부 환경 3대 로봇 감사 전체 성공
- [ ] 서명 PDF 리포트 외부 검증 성공
- [ ] Self-Test 커버리지 60%+
- [ ] 팀 스택 결정 만족도(본 리포는 1인 운영이므로 본인 회고)

---

## 문서 생명주기

- 본 백로그는 **살아있는 문서**. 태스크 완료 시 체크 표시(`[x]`)와 커밋 해시를 본 문서에 반영.
- 새 태스크 발견 시 해당 에픽 섹션에 추가하고 `Added (Phase 1)` CHANGELOG 엔트리 기록.
- Phase 1 완료 시 본 문서는 아카이브(`docs/design/archive/phase1-backlog.md`)로 이동, Phase 2 백로그를 동일 경로에 신규 작성.
