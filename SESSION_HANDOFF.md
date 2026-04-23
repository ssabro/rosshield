# 세션 핸드오프

> **목적**: Claude Code 새 세션(재설치·다른 머신·오래 만에 재개)이 이 리포에서 바로 작업을 이어갈 수 있게 하는 지속 문서. git에 커밋되므로 로컬 `~/.claude/` 상태와 무관하게 유지된다.
>
> **Claude에게**: 이 문서를 먼저 읽고, 사용자에게 "## 진행 중 선택지" 섹션을 제시해라.

_마지막 업데이트: 2026-04-23 (E1.T5 Migrate 완료)_

---

## 현재 상태 한 줄

**Phase 1 E1 진행 중 — T1~T5 완료, T6~T9 대기.** `internal/platform/storage/` Migrate() 실구현: goose v3 + embed.FS(`migrations/sqlite/0001_platform_init.sql`) + gofrs/flock OS file lock 5초 timeout. 첫 마이그레이션은 `platform_info` KV 테이블만 생성(도메인 테이블은 후속 에픽). 10 tests pass(이전 7 + 신규 3). 원격 18개 커밋, CI green in 2m32s. 다음 세션 착수 후보: E1.T6/T7 EventBus 또는 E1.T8 Signer.

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

E1.T1~T5 완료 + R1·R2 14건 결정 합의 상태에서 재개 후보:

1. **E1.T6/T7 EventBus 착수** (권장). `internal/platform/eventbus/inproc/`. R2 결정 7건이 노트에 반영됨. M2 모델(subscriber당 goroutine), bounded channel + DropOldest 기본, panic 격리. 1~2일 사이클.
2. **E1.T8 Signer 착수** — `crypto/ed25519` 메모리 키 wrapper. 가장 작고 독립적, T6 사이클 중간 인터럽트로 처리 가능.
3. **E1.T9 Scheduler 착수** — `robfig/cron/v3`. T2 Clock 의존(완료). 결정론적 테스트가 필요하면 Clock 인터페이스 확장(현재 `Now()` 단일).
4. **depguard 도메인 경계 린트 설정** — `.golangci.yml`. R1-2 `Storage.Bootstrap` 강제(boot 경로에서만)에도 활용. E3 진입 전 권장.
5. **Step 0.3-β OpenAPI 코드 생성** — `oapi-codegen`.
6. **로컬 환경 정리** — Windows Defender `%TEMP%\go-build\*.test.exe` 격리 우회.

**권장 순서**: 1(E1.T6/T7) → 2(E1.T8) → 3(E1.T9) → E1 Exit (cmd/rosshield-server bootstrap 시퀀스로 모든 platform 서비스 초기화 + SQLite 파일 DB 생성·Migrate 적용·`platform_info` 검증).

## 결정 로그

날짜 내림차순.

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
