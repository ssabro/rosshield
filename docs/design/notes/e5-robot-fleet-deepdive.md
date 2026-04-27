# E5 Robot/Fleet 사전 설계 노트

> **상태**: Draft v0.1 (2026-04-27)
> **범위**: `phase1-backlog.md` E5 (4일) — `internal/domain/robot/` (Fleet·Robot·Credential).
> **선행**: E1 Platform · E2 Audit · E3 Tenant/Auth · E4 Pack 모두 완료.
> **목적**: 백로그 E5.T1~T7 착수 전 (a) 전신 nrobotcheck 자산 활용 범위, (b) 결정 필요한 항목, (c) 함정·엣지케이스를 한 곳에 정리.

## §1 도메인 패키지 구조

`internal/domain/tenant/` 패턴(E3) 답습 — Fleet·Robot·Credential을 **단일 패키지**로 묶는다. 이유:
- 셋이 강하게 결합 (Robot은 Fleet 참조, Robot은 Credential 참조).
- 외부에서 본 표면(Service)이 단일.
- E3 tenant 패키지가 같은 패턴 — 테스트·sqliterepo·서비스가 한 폴더.

```
internal/domain/robot/
├─ doc.go
├─ model.go              # Fleet, Robot, Credential, EncryptionMeta 구조체
├─ errors.go             # ErrFleetNotFound, ErrRobotNotFound, ErrCredentialKEKMissing ...
├─ service.go            # Service 인터페이스 + 구현
├─ csv.go                # CSV import 파싱·검증
├─ kek.go                # KEK 로드/저장 (별도 파일 0600)
├─ dek.go                # DEK 생성·wrap·unwrap (AES-256-GCM)
├─ sqliterepo/           # 마이그레이션 0007 + Repository 구현
│  ├─ migrations.go
│  ├─ repo.go
│  └─ repo_test.go
├─ service_test.go
├─ kek_test.go
├─ dek_test.go
└─ csv_test.go
```

ID 접두사:
- Fleet: `fl_<ULID>` (백로그·설계서 미정 → 본 노트에서 제안)
- Robot: `ro_<ULID>` (§04.4 명시)
- Credential: `cr_<ULID>` (제안)

## §2 nrobotcheck 자산 승계 결과 (2026-04-27 Agent A 조사)

### Tier A — 그대로 차용 가능

- **Robot 엔터티 스키마**: 전신 `robots` 테이블(name, ip_address, ssh_port, username, auth_type, ros_distro, criticality)이 본 리포 `Robot` 모델과 95% 일치. nrobotcheck `src/main/services/database/robotRepository.ts:47-187`의 CRUD 로직 구조(파라미터화 쿼리·Tx·중복 검증 existsByName/existsByConnection) 그대로 Go로 포팅.
- **CSV import 패턴**: `nrobotcheck/src/main/ipc/routes/v1/robot.router.ts:155-276`에서 헤더 정규화(영/한 다중 변형: 'name'/'Name'/'이름') + 행 순회 + 검증 + 벌크 삽입 + 부분 성공(robots[] + errors[] 분리) 패턴. 단, 본 리포 도입 시 헤더 다양성 함정(주석 행·프리헤더) 대응 필요(§5).
- **감사 로깅 접점**: `writeAuditEntry({eventType, action, actor, targetType, targetId, details})` 호출 구조 — 본 리포 E2 audit 인터페이스(emit at WRITE)에 자연 매핑.

### Tier B — 개념 승계, 재구현

- **Credential 암호화 알고리즘**: AES-256-GCM(전신·후계 동일). 전신 구조 `salt:iv:authTag:encrypted` (hex 연쇄, `nrobotcheck/src/main/services/crypto/CryptoService.ts`)는 본 리포 `EncryptionMeta` 스키마와 호환. **차이**: 전신은 Electron `safeStorage`(OS Keychain)로 KEK 봉인 → 후계는 OS Keychain 의존 회피, 파일·환경변수·KMS·TPM 옵션(§3).
- **온보딩 진단 (Phase 2+)**: `nrobotcheck/src/domains/onboarding/services/OnboardingDiagnostics.ts:1-125` — OS·ROS·Service 3개 SSH 프로브로 baseline 추천. **Phase 1 E5 범위 외** — F19 별도 도메인으로 이동 권고. E5는 단순 `testConnection` mock interface만.
- **SSH 연결 테스트**: 전신 `SSHClient.ts:1-295`의 재시도(지수 백오프 1s-10s, 최대 3회) + 인증 분기(`authHandler=['password']`로 pam_faillock 회피 — §5 함정 참조) + 타임아웃 30s 기본. **E5에서는 mock interface만**, 실제 SSH 동작은 E6에서.

### Tier C — 버린다

- Electron IPC 계층(handlers.ts·robot.router.ts) — Go HTTP/gRPC로 재설계.
- better-sqlite3 직접 바인딩 — 본 리포는 modernc.org/sqlite + storage Tx 추상.
- safeStorage 직접 의존 — KEK는 별도 추상(§3).

## §3 Credential 암호화 (KEK/DEK)

설계서 §06.6 키 계층:
```
Master KEK (TPM/Keychain/파일에 봉인)
  └── Tenant Key (각 테넌트별, KEK로 wrap)
       └── DEK (레코드 단위, Tenant Key로 wrap)
            └── Ciphertext (Credential payload)
```

### Phase 1 단순화 (제안)

- **2계층만**: KEK → DEK(레코드별) → Ciphertext. **Tenant Key 생략** — 단일 사용자 데스크톱·소규모 온프렘 시나리오에서 한 단계는 over-engineering. Tenant Key 추가는 Phase 2+ 마이그레이션으로 가능(KEK 파일 형식에 버전 필드).
- **KEK 저장**: 파일 `<dataDir>/keys/credential.kek` (32 byte raw, perm 0600·dir 0700) — Signer Ed25519 키(`platform.ed25519`)와 같은 디렉터리·동일 패턴(commit `4b6a2aa` 참조).
- **KEK 부팅 시 LoadOrCreate**: 부재 시 `crypto/rand` 32B 생성 + 저장. 존재하면 길이 검증 후 로드. Signer `soft.LoadOrCreate`와 동일 시퀀스.
- **알고리즘**: AES-256-GCM (NIST SP 800-38D). nonce는 12 byte `crypto/rand`, 매 wrap마다 새로. AAD = tenantId(혹시 모를 키 재사용 방지 — Tenant Key 도입 전 임시 격리).

### EncryptionMeta 구조 (DB 컬럼)

```go
type EncryptionMeta struct {
    Version   int    // 1 (Phase 1)
    Algorithm string // "AES-256-GCM"
    Nonce     []byte // 12 byte
    KEKKeyID  string // "kek_<sha256(KEK)[:8] hex>" — 회전 시 식별
    AAD       string // "tenant:<tenantId>" — 회전 가이드
    CreatedAt time.Time
}
```

Credential row:
```sql
encrypted_payload BLOB NOT NULL,  -- Ciphertext
encryption_meta   TEXT NOT NULL,  -- JSON of EncryptionMeta
```

### KEK 회전 (Phase 1 범위 외, 결정만)

- **수동만**: `rosshield admin kek-rotate` CLI(Phase 1 후반 또는 E9). 모든 Credential을 새 KEK로 re-wrap, 감사 emit.
- 자동 회전·HSM·KMS는 Phase 3.

### 결정 R3-1 — KEK 저장 위치

**제안**: 파일 `<dataDir>/keys/credential.kek` (0600). OS Keychain·KMS·TPM은 Phase 3 어플라이언스 단계로 미룸. **이유**: Phase 1 타깃은 단일 바이너리·파일 기반(Signer 패턴 동일). 의존성 추가 0(stdlib `crypto/aes`·`crypto/cipher`).

### 결정 R3-2 — Tenant Key 도입 시점

**제안**: Phase 2+. KEK → DEK 직접 wrap. AAD에 `tenant:<id>` 표시로 cross-tenant 키 재사용 방지(약한 격리).

### 결정 R3-3 — Credential rotation 트리거

**제안**: Phase 1은 **수동 API만** (`Rotate(credentialID, newPayload)` Service 메서드). 만료 알림(`rotationDueAt`)은 audit emit + UI 표시 (E10). 로봇 `authorized_keys` 자동 교체는 Phase 2+.

## §4 Fleet Policy

설계서 §04.2 `FleetPolicy` 명시 안 됨 — 본 노트에서 제안.

### Phase 1 단순화

```go
type FleetPolicy struct {
    DefaultBaselineID string   // 기본 적용 팩 (E4)
    DefaultLevel      string   // "L1" or "L2"
    DefaultCriticality string  // "low|medium|high|critical"
    ScanSchedule      string   // cron spec or "" (수동만)
}
```

Robot이 fleet의 policy를 **상속**, 개별 robot이 override 가능 (Robot.criticality 컬럼이 이미 있음).

### 결정 R3-4 — Fleet default policy 형태

**제안**: 위 구조체. 4 필드만. ScanSchedule는 Scheduler(E1.T9 cronsched) 표면 그대로 — `@every 1h` 등.

## §5 함정 및 엣지케이스

### F1 — KEK 손실 시 복구 불가

전신 nrobotcheck에서도 동일 문제 — `safeStorage` 키가 OS profile 손상 시 모든 credential 복구 불가. 후계는 파일 KEK이므로 **백업 필수** — `~/.rosshield/keys/credential.kek` 분실 시 모든 robot credential 재발급.
**대응**: 부팅 로그에 KEK keyID 출력(이미 Signer 패턴), 운영 문서에 백업 절차 명시.

### F2 — pam_faillock 계정 잠금

전신 SSHClient: `authHandler=['password']` 강제로 pubkey-then-password 시도 회피 — pam_faillock 활성 호스트에서 연속 실패 시 잠금. **E5는 mock만이라 직접 영향 없음**, **E6 SSH 도메인에서 재현 필요**(SSH+Scan 노트로 이관 — Agent B 결과에 반영 예정).

### F3 — CSV 헤더 다양성

전신은 영문/한글 다중 변형 매핑(`'name'|'Name'|'이름'` 등) — 사용자가 프리헤더 행·주석 행 포함 시 파싱 실패.
**대응**: Phase 1은 **표준 헤더만** 수용(영문 소문자·콤마 구분). 한글 헤더는 Phase 2+. 사용자 매핑 UI는 후순위. 대신 **명확한 에러**(`row 1: unknown header 'NAME'; expected one of [name, host, port, ...]`) 제공.

### F4 — Soft delete cascade

§04.7 보존 정책: Robot 무기한 + soft delete + tombstone. 그러나 **종속**:
- Credential: Robot 삭제 시 unwrap 불가능하게(`Credential.RevokedAt`) but row 유지(audit 추적용).
- ScanSession·ScanResult: Robot.deletedAt 이후 새 세션 거부(검증), 기존 결과는 보존.
- PeerGroup: Robot 삭제만 PeerGroup membership 제거(N:M).

### 결정 R3-5 — Soft delete cascade

**제안**: `deletedAt` 컬럼만(timestamps 추가). cascade는 **읽기 시 필터**(`WHERE deleted_at IS NULL`)와 **쓰기 시 거부**(deleted robot에 새 scan 거부). 실제 row 삭제는 retention job(Phase 1 범위 외).

### F5 — UNIQUE 제약과 soft delete의 충돌

`UNIQUE(tenant_id, name)` 또는 `UNIQUE(tenant_id, host, port)` 제약 시 — soft delete 후 같은 이름 재등록 차단. 전신은 hard delete였기에 문제 없었음.
**대응**: UNIQUE는 `WHERE deleted_at IS NULL` partial index — SQLite는 partial unique index 지원(`CREATE UNIQUE INDEX ... WHERE deleted_at IS NULL`). 마이그레이션 0007에 반영.

### F6 — 동일 host:port 중복 등록

전신은 `existsByConnection(host, port, username)`으로 사전 차단. 본 리포는 partial unique index로 DB 레벨 강제 + Service에서 사전 검증(친절한 에러).

## §6 인터페이스 시그니처 초안

```go
package robot

type Service interface {
    // Fleet
    CreateFleet(ctx context.Context, req CreateFleetRequest) (Fleet, error)
    GetFleet(ctx context.Context, id string) (Fleet, error)
    ListFleets(ctx context.Context) ([]Fleet, error)
    UpdateFleetPolicy(ctx context.Context, fleetID string, policy FleetPolicy) error

    // Robot
    CreateRobot(ctx context.Context, req CreateRobotRequest) (Robot, Credential, error) // atomic
    GetRobot(ctx context.Context, id string) (Robot, error)
    ListRobots(ctx context.Context, filter RobotFilter) ([]Robot, error)
    UpdateRobot(ctx context.Context, id string, patch RobotPatch) (Robot, error)
    DeleteRobot(ctx context.Context, id string) error // soft

    // Credential
    RotateCredential(ctx context.Context, robotID string, newPayload CredentialMaterial) error
    GetCredentialMaterial(ctx context.Context, credentialID string) (CredentialMaterial, error) // unwrap

    // Bulk import
    ImportRobotsCSV(ctx context.Context, fleetID string, csv io.Reader) (ImportReport, error)

    // Test connection (mocked in E5, real SSH in E6)
    TestConnection(ctx context.Context, robotID string) (TestResult, error)
}

type SSHClient interface { // E6에서 구현, E5는 mock
    Dial(ctx context.Context, host string, port int, cred CredentialMaterial) (SSHSession, error)
}
```

`CreateRobot`이 Robot + Credential을 **한 Tx**에서 생성 — Service 책임. CreateRobot 내부에서 `audit.Append`도 같은 Tx (E3 패턴 답습 — `eed4b35`).

## §7 테스트 전략

### TDD 태스크 (백로그 E5.T1~T7 그대로)

| ID | Red 테스트 | Green 구현 |
|---|---|---|
| E5.T1 | `TestCreateFleetWithDefaultPolicy` | `CreateFleet` + 기본 policy 적용 |
| E5.T2 | `TestCreateRobotRequiresFleet` | FK 제약 + Service 검증 |
| E5.T3 | `TestRobotCredentialEncryptedAtRest` — DB 파일 grep으로 평문 부재 확인 | KEK/DEK + AES-256-GCM |
| E5.T4 | `TestRobotCredentialRotateIsAudited` | rotate API + audit emit |
| E5.T5 | `TestTestConnectionUsesSSHPool` (mocked SSH) | SSHClient 인터페이스 mock |
| E5.T6 | `TestRobotCSVImportValidates` — 잘못된 IP·포트 거부 + 부분 성공 | csv → validation |
| E5.T7 | `TestRobotDeleteKeepsAuditReferences` — soft + scan_results 참조 가능 | `deleted_at` |

추가 보조 테스트:
- `TestKEKLoadOrCreate` (kek_test.go) — 부재 시 생성, 두 번째 부팅 시 동일 keyID.
- `TestDEKWrapUnwrapRoundtrip` (dek_test.go) — 평문→ciphertext→평문 동일.
- `TestDEKDecryptRejectsTamperedCiphertext` — GCM tag 위변조 거부.
- `TestUniqueIndexAllowsRecreateAfterSoftDelete` (sqliterepo) — partial index 동작.
- `TestCrossTenantBlock` (E3 fuzzer 패턴) — tenant A의 robot을 B 컨텍스트로 조회 차단.

### 통합 검증 (Exit 기준)

- `cmd/rosshield-server/bootstrap.go` 결선 → Platform.Robot 노출.
- `data.db` 파일을 hex dump로 grep — 'password' 또는 평문 SSH 키 없음.
- 두 번 부팅 시 `kekKeyId` 동일.

## §8 의존 추가 후보

- `crypto/aes`·`crypto/cipher` — stdlib only.
- `encoding/csv` — stdlib only.
- **추가 의존성 0** — 새 외부 dep 없이 stdlib만 사용 가능. (Argon2id는 E3 tenant 패키지에서 이미 사용 중이라 신규 아님.)

## §9 미해결 질문 (사용자 합의 필요)

| ID | 질문 | 권장값 | 영향 범위 |
|---|---|---|---|
| **R3-1** | KEK 저장 위치 | 파일 `<dataDir>/keys/credential.kek` (0600), OS Keychain·KMS·TPM은 Phase 3 | E5.T3 착수 전 합의 |
| **R3-2** | Tenant Key 도입 시점 | Phase 2+ (Phase 1은 KEK→DEK 2계층) | EncryptionMeta version 필드만 예약 |
| **R3-3** | Credential rotation 트리거 | Phase 1은 수동 API만, 자동 회전 Phase 2+ | Service.RotateCredential 표면 |
| **R3-4** | Fleet policy 구조 | `{DefaultBaselineID, DefaultLevel, DefaultCriticality, ScanSchedule}` 4 필드 | model.go FleetPolicy |
| **R3-5** | Soft delete cascade 정책 | `deleted_at` only + 읽기 필터 + 쓰기 거부, partial unique index | sqliterepo 마이그레이션 0010 |
| **R3-6** | Fleet·Credential ID 접두사 | `fl_<ULID>` / `cr_<ULID>` (제안 — 설계서 미명시) | idgen 호출부 |
| **R3-7** | Robot UNIQUE 제약 범위 | `(tenant_id, fleet_id, name)` + `(tenant_id, host, port)` 둘 다 partial(`WHERE deleted_at IS NULL`) | 마이그레이션 0010 |

## §10 Stage 분할 제안

E3·E4 패턴 답습 (Stage A·B·C·D·E). 마이그레이션도 stage별 분할(0007은 E4에서 사용 중이므로 **0008부터**):

- **Stage A ✅** (`af599a2`) — `0008_fleets.sql` + Fleet 도메인 + ID 접두사 + T1.
- **Stage B ✅** (`4841381`) — `0009_credentials.sql` + KEK/DEK 코어 + EncryptionMeta + Credential 모델 + KEK/DEK 단위 테스트.
- **Stage C ✅** (`ff1f6c9`) — `0010_robots.sql`(FK fleet_id, credential_id) + Robot CRUD + Credential CRUD(같은 Tx에 CreateRobot 묶음) + soft delete + audit emit + T2·T3·T4·T7.
- **Stage D ✅** (`eeaf714`) — CSV import + T6.
- **Stage E ✅** — TestConnection mock interface(`SSHTester`) + cross-tenant fuzzer + T5 + E3 fuzzer 패턴.

**E5 epic 완전 종료** (5/5 Stage + 7/7 태스크).

## §11 다음 단계

1. **사용자 합의**: R3-1~R3-7 7개 결정 사항 (위 권장값 검토).
2. **Stage A 진입**: 모델·마이그레이션·ID 접두사 → TDD Red.
3. **Agent B 결과(SSH+Scan)** 도착 시 별도 노트 `e6-ssh-scan-deepdive.md`로 저장(E5 범위 외이지만 E6 prerequisite).
