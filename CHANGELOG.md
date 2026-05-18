# Changelog

이 프로젝트의 주요 변경 사항을 기록합니다. 포맷은 [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/)을 따르고, 버저닝은 [Semantic Versioning](https://semver.org/)을 따릅니다.

> **참고**: 본 changelog는 v0.3.0(2026-05-18 release candidate) 이후 항목이 최상단에 정리되어 있습니다. Phase 0~1 초기 부트스트랩 항목(2026-04-23 이하)은 역사 기록 보존을 위해 하단의 "Pre-v0.2.0 historical entries" 섹션으로 이동했습니다.

## [Unreleased]

### Added
- (placeholder) 차기 release 항목 — Phase 7 R-PUBLIC(사용자 GitHub Settings 권한 대기) / R-D8 청구권 후속 v2 / ROS2 pack Round 2 C4·C5 carryover

---

## [0.4.1] — 2026-05-18 (patch)

> **요약**: v0.4.0 직후 CI infrastructure fix cascade 14 round 마감 + snap binary 빌드 fix. 자체 코드 회귀 0. main CI 7/7 ALL PASS 완전 안정화 milestone 도달. 상세는 [docs/releases/v0.4.1.md](docs/releases/v0.4.1.md).
>
> **기준 commit**: `921a2cc` (main)

### Fixed

#### CI fix cascade 14 round

- `ci(go)` pack-archive pre-build step 추가 (`c7a630c`) — embed `_archives/*.tar.gz` fresh clone 부재 fix, Secret `DEV_PACK_SIGNER_KEY_B64` 사용
- `fix(snap)` architectures 중복 arm64 제거 (`cd29d62`) — snapcraft 8.x validation 호환
- `fix(packs)` cis-ubuntu-2404 duplicate placeholder 12건 제거 (`885700e`) — manual fixture 작성 후 obsolete
- `ci(go)` pack-archive 3 job 확장 (`b2f30b9`) — go-enterprise + pg-integration + e2e
- `ci(go)` test timeout 10m → 20m (`b21be1e`) — cmd/rosshield-server 일관 초과
- `fix(lint)` golangci-lint v8 cascade 2 round 14건 (`70851d1` + `9889fb5`) — gofmt + errcheck + staticcheck + unused
- `fix(postgres)` 마이그레이션 0024 evidence_json JSONB cast DEFAULT DROP/SET (`b19802a`)
- `fix(postgres)` pgnative_hotpath tenants schema drift (`2e1ba6a`) + insights schema drift (`c978964`)
- `ci(pg)` TESTCONTAINERS_RYUK_DISABLED=true (`282fb9a`) — Reaper hang fix
- `fix(ha)` pglock integration 다층 차단 + migrate conn leak 차단 + assertion 일관 (`a1d7e14` + `5bc5fdd` + `f17777d`)

#### main CI 결과
- **7/7 ALL PASS** — Go + Enterprise + Web + PG integration + CIS + TPM + Playwright E2E

### Notes
- 자체 코드 회귀 0 — 모든 fix는 CI infra · test infra · 마이그레이션 schema drift
- sub-agent stack trace 정독으로 migrate driver borrowed conn leak 진짜 root cause 발견 (golang-migrate/v4 postgres.WithInstance `instance.Conn(ctx)` 영구 borrow)

---

## [0.4.0] — 2026-05-18

> **요약**: Phase 7 코드 트랙 3/4 epic 마감(R-BRAND · R-LICENSE · R-D8 4/4 청구권 완전 마감) + ROS2 baseline pack Round 1 MVP 6/8 카테고리 cover. v0.3.0 이후 20 commit. 회귀 0건. 상세는 [docs/releases/v0.4.0.md](docs/releases/v0.4.0.md).
>
> **기준 commit**: `01e41fc` (main)

### Added

#### Phase 7 — R-BRAND (Lodestar 브랜드 확정)
- `feat(brand)` R-BRAND Stage 1 — Lodestar 채택 + `<ProductName>` placeholder 사용자 대면 6 파일 교체 (`3e3d892`)
- `feat(brand)` R-BRAND Stage 1 보완 — design/onboarding/web 잔여 9 파일 11 위치 교체 + d1-brand-candidates §5.6 확정 근거 (`20eddee`)
- 코드 네임스페이스 `rosshield` 보존 (Go 모듈 · CLI · YAML apiVersion · PWA manifest short_name 변경 0)

#### Phase 7 — R-LICENSE (Open-core 라이선스 양분)
- `docs(license)` R-LICENSE — LICENSE-ENTERPRISE (BSL 1.1, Change Date 2030-05-18) + NOTICE (third-party OSS attribution ~20 dep) (`ea8d5d7`)
- 기존 LICENSE(Apache 2.0) 보존 — 코어/enterprise 라이선스 양분 결선

#### Phase 7 — R-D8 (D8 청구권 코드 분리, enterprise build tag) — **4/4 본체 완전 마감**
- `feat(enterprise)` R-D8 A-1 — cross-witness fold-in 본체 (multi-fold hash chain, RFC 8785 canonical JSON, 17 단위 PASS) (`b4e77eb`)
- `feat(enterprise)` R-D8 B-1 — multi-hash evidence 본체 (sha256+blake3 cross-check + JSONPath/line sub-hash + VerifyMode enum, 48 단위 PASS, `lukechampine.com/blake3 v1.4.1` dep 추가) (`5292585`)
- `feat(enterprise)` R-D8 D-3 — robotid fingerprint 본체 (TPM EK + sorted MACs + CPU serial + tenant salt, 19+ 단위 PASS, `go-tpm-tools v0.4.8` indirect 활용) (`b8bbae7`)
- `feat(enterprise)` R-D8 C-1 — WASM sandboxed evaluator 본체 (wazero v1.11.0 + WASI 격리 + CPU timeout + hand-crafted WASM 4종, 45 단위 PASS, PolicyVerifier interface) (`012fe3f`)
- **enterprise 8 패키지 누적 129+ 단위 PASS** (crosswitness 17 + multihash 48 + wasmrt 45 + robotid 19+ = 4 청구권 본체 cover) + 코어 → enterprise import 0 (boundary_test 회귀 0)
- **1순위 결합 청구항 4 본체 모두 enterprise build tag 격리** — `docs/design/13-patent-strategy.md` §13.5 spec 정확 일치

#### ROS2 baseline pack Round 1 (솔루션 핵심 차별화 영역)
- `feat(packs)` Round 1 Stage 1 — `packs/ros2-jazzy/` 신규 pack + C1 SROS2 보안 활성화 + C6 distro(LTS/EOL/CLI) (`8eb3d7d`)
- `feat(packs)` Round 1 Stage 2 — C3 ROS_DOMAIN_ID 격리 + C7 RMW_IMPLEMENTATION (`edfba4f`)
- `feat(packs)` Round 1 Stage 3 — C8 governance.xml ENCRYPT topics (`f34f8b9`)
- `feat(packs)` Round 1 Stage 4 — C2 cmd_vel publisher count + ACL (`c6ea725`)
- **카테고리 cover 6/8** (C1·C2·C3·C6·C7·C8 ✅ / C4 binary 무결성·C5 launch 안전 carryover Round 2)
- 9 check 총 + 9 selftest fixture (mock 작성, D-ROS2-9 정확 준수) — ros2_jazzy_fixture_test.go 동적 round-trip cover

### Notes
- 메모리 정책 일관: 큰 작업 design doc 우선(`feedback_design_doc_first`) · 보수적 추정(`feedback_design_doc_conservative`) · 병렬 작업 사전 판단(`feedback_parallel_agents`) · backtick hash 보호(`feedback_commit_message_backticks`)
- sub-agent worktree 패턴 누적 31회 — 마라톤 retrospective(`c85838c`) 학습 반영
- Phase 7 코드 트랙 R-D8 본체 100% 마감 — 다음 자연 진입은 R-PUBLIC (사용자 GitHub Settings 권한 대기) / ROS2 pack Round 2 carryover (paying customer trigger 권장)

---

## [0.3.0] — 2026-05-18

> **요약**: Phase 5(Enterprise & Appliance) 5 epic 100% 마감 + Phase 6 후보 1(첫 paying customer onboarding 보강) 마감. v0.2.0 이후 90 commit. 회귀 0건. 상세는 [docs/releases/v0.3.0.md](docs/releases/v0.3.0.md).
>
> **기준 commit**: `c85838c` (main, marathon retrospective 후 handoff 갱신)

### Added

#### Phase 5 — scanrun SSH 통합 (epic 마감)
- `feat(robot)` scanrun SSH 통합 Stage 1 — `robot_host_keys` 도메인 + 마이그레이션 0027 (TOFU) (`e9b93c0`)
- `feat(sshpool)` Stage 2 — `KnownHostsManager` + TOFU host key callback (`951e924`)
- `feat(scanrun)` Stage 3 — bootstrap KnownHostsManager 결선 + sudo non-interactive (`894449e`)
- `feat(sshpool)` Stage 4 — Pool idle 재사용 + keepalive + metrics 5종 (`22f472d`)
- `feat(scanrun)` Stage 5a — per-robot health window (`robot_offline` 즉시 skip) (`cade719`)
- `feat(scanrun)` Stage 5b — Pool 결선 (idle 재사용 활성화) (`1d67cef`)
- `test(scanrun)` Stage 5c — docker compose + sshd e2e 5 phase (`ee2aa34`)

#### Phase 5 — 세분 RBAC (epic 마감)
- `feat(authz)` Stage 1 — authz 결정 테이블 + 6 시스템 role permission matrix (`4c4bfc9`)
- `feat(tenant)` Stage 2 — `RoleBinding` + 마이그레이션 0028 + repo 확장 (`scope_type`/`scope_id`) (`daacb57`)
- `feat(rbac)` Stage 3 — JWT bindings claim + `RequirePermission` middleware factory (`a9125aa`)
- `feat(rbac)` Stage 4 — handlers.go 24 mutation gate `RequireRole` → `RequirePermission` 교체 + 통합 매트릭스 (`0452941`)
- `feat(rbac)` Stage 5 — web `useHasPermission` + sidebar/router guard 확장 (`4ec5620`)

#### Phase 5 — PWA 오프라인 (epic 마감)
- `feat(web)` PWA Stage 1 — manifest + 아이콘 4종 + index.html link (installable, SW 없이) (`4079e66`)
- `feat(web)` PWA Stage 2 — vite-plugin-pwa generateSW + SW 등록 (오프라인 셸 캐싱) (`1bf2c21`)
- `feat(web)` PWA Stage 3 — `OfflineIndicator` + `UpdatePrompt` UX (`1732a40`)
- `feat(web)` PWA Stage 4 — mutation 가드 + 운영자 docs (`70ef3d6`)

#### Phase 5 — PWA persist (epic 마감)
- `feat(web)` PWA persist Stage 1 — idb-storage 모듈 (IndexedDB AsyncStorage 어댑터) (`2499722`)
- `feat(web)` PWA persist Stage 2 — `PersistQueryClientProvider` 결선 + dehydrate filter (보안 차단 list) (`7e855a8`)
- `feat(web)` PWA persist Stage 3 — logout flow clear (multi-tenant 격리) (`1f985c7`)
- `docs(operations)` PWA persist 운영자 가이드 (`350c38d`)

#### Phase 5 — RBAC fleet 정밀화 (epic 마감)
- `feat(authz)` Stage 1 — PDP `MatchedBindings` 확장 (explainability) (`d55cd71`)
- `feat(rbac)` Stage 2 — `RequirePermissionWithFleet` + body peek + `ScopeResolver` (`0deb4c8`)
- `feat(rbac)` Stage 3 — handlers 5 endpoint 교체 + ScopeResolver 구체 + 통합 매트릭스 (`e3a7958`)
- `feat(rbac)` Stage 4 — SSO group 매핑 도메인 + 마이그레이션 0029 + `user_roles.source` (`07fb0a8`)
- `feat(rbac)` Stage 5 — SSO callback sync + audit + web admin UI (`acde2b2`)
- `feat(rbac)` Stage 6 — reports/insights service 확장 + 2 endpoint 정밀화 (`77180db`)

#### Phase 6 후보 1 — Customer onboarding 보강 (R1·R2·R3 마감)
- `feat(intake)` R1 Stage 1 — intake 도메인 + 마이그레이션 0030 (`6d7f869`)
- `feat(intake)` R1 Stage 2 — intake handler + endpoint + RBAC mount (`09c20cf`)
- `feat(intake)` R1 Stage 3 — chi mount + RBAC + bootstrap intake 결선 (`6da6ffd`)
- `feat(intake)` R1 Stage 4 — auto-provisioning wrap (accept → tenant + admin invite) (`975109e`)
- `feat(intake)` R1 Stage 5 — 실 e2e 통합 + 운영자 docs 갱신 (`e13c9b0`)
- `docs(onboarding)` R2 — PoC walkthrough (단계별 명령 + 예상 결과 + 트러블슈팅 12개) (`f8446de`)
- `docs(onboarding)` R3 — SLA template + 지원 채널 정책 (`2b47546`)

#### Design docs (Phase 5 + Phase 6)
- `design(scanrun)` scanrun SSH 통합 design doc (`6f893de`)
- `design(web)` PWA 오프라인 지원 design doc (`eeebfdd`)
- `design(rbac)` 세분 RBAC (fleet scope + permission tier) design doc (`b975e94`)
- `design(rbac)` fleet-scope 정밀화 + SSO group 매핑 design doc (`37778ef`)
- `design(scanrun)` scanrun 후속 (Pool size 동적 + rate limit + circuit breaker) design doc (`7d26bfd`)
- `design(web)` PWA persist design doc — 옵션 C trigger (`af0b84d`)
- `design(phase6)` Phase 6 backlog — Phase 5 retrospective + 후보 5종 비교 + 권장 우선순위 (`ad5fcf6`)
- `design(onboarding)` 첫 customer onboarding 보강 design doc — intake API + walkthrough + SLA + 지원 채널 + license lifecycle (`c0f8586`)
- `design(meta)` 마라톤 세션 retrospective — 73 commit 패턴·결정·learnings 정리 (`ebc2b80`)

### Changed
- `AssignRoleScoped(..., source)` 매개변수 추가 — backward-compat (`source=""`/`"manual"` → 기존 동작, `"sso:<provider>"` → 자동 동기화 경로)
- 24 mutation gate `RequireRole` → `RequirePermission` 단계적 전환 (관리자 전용 gate는 `RequireRole` 잔존)

### Deprecated
- 없음

### Removed
- 없음

### Fixed
- 본 release 구간 내 회귀 0건 (separate fix commits 없음)

### Security
- TOFU host key 정책 도입 (마이그레이션 0027 `robot_host_keys`) — SSH MITM 방지
- fleet-scope 정밀 권한 검사로 cross-fleet 데이터 누출 차단 (RBAC fleet 정밀화 epic)
- PWA persist의 dehydrate filter에 보안 차단 list 적용 (token·자격 증명·민감 도메인 캐시 금지)

### Migrations
| ID | 내용 | down sql |
|---|---|---|
| `0027_robot_host_keys` | TOFU host key 저장 | ✅ |
| `0028_user_roles_scope` | `scope_type`/`scope_id` 컬럼 추가 (fleet-scope RBAC) | ✅ |
| `0029_sso_group_mappings` | SSO group → role 매핑 + `user_roles.source` | ✅ |
| `0030_customer_intakes` | customer intake 도메인 | ✅ |

---

## [0.2.0] — 2026-05-08

> **요약**: Phase 4(Production hardening) carryover 11/11 마감 + 첫 공식 release. 47 release assets + cosign keyless 서명 (Sigstore Fulcio).
>
> **기준 commit**: `14a3ccb` 이하 (tag `v0.2.0`).

### Added
- E12 — release CI signer + dual trust bundle (dev + release pack signer)
- E25 — HA leader-election scaffold (PG advisory lock + leader_epoch fence token, 마이그레이션 0022·0023)
- E22-F (1차) — PG-native 핫 path 3 컬럼 회수 (R30-1=C 하이브리드, JSONB + TIMESTAMPTZ, 마이그레이션 0024)
- E27 — Grafana dashboard + Prometheus scrape 가이드
- E29 — `rosshield ha status` + `backup list`/`download` CLI
- E31 — enterprise build tag scaffold (`//go:build rosshield_enterprise`)
- E33 — Ubuntu Core snap 빌드 파이프라인 + smoke test (R40-1=core22)
- E34 — TPM 2.0 PCR-sealed ed25519 (`go-tpm-tools`, PCR `[0,2,4,7]`)
- E35 — A/B OTA post-refresh hook + 자동 rollback + healthz polling
- E36 — 레퍼런스 HW 매트릭스 + 측정 절차 docs
- E38 — 첫 paying customer onboarding 사전 자료 (`docs/onboarding/`)
- O6 — invite email adapter (Noop + SMTP + `InvitationNotifier`)
- O7 — webhook UI 강화 (Test 버튼 + delivery 통계 + dead-letter)
- B6+B7 — `/system` 운영 정보 dashboard (헬스·HA·라이선스·백업) + 자동 백업 schedule + 다운로드 API
- OpenAPI spec — Webhook test + SSO 8 + Invitation 5 endpoint 추가

### Changed
- `apiClient` 100% 전환 (webhook·sso·invitation 4 wrapper 제거 + 16 hook 전환)
- 데스크톱 셸 Tauri 2.x 결선 (D3)

### Decisions (이 release 구간 확정)
- D5 — Open-core 채택 (코어 Apache-2.0 + enterprise BSL/Commercial 별 라이선스)
- D6 — GitHub private 유지 (release binary + report verify CLI로 P1 외부 검증 대체)
- R30-1=C 하이브리드 (E22-F 1차)
- R30-2 (E25 HA 권고안)
- R30-4 (Open-core + private repo 종결)
- R40-1~4 (snap 트랙)
- R41 (TPM 3종 결정 — B Keystore + go-tpm-tools + PCR `[0,2,4,7]`)

### Fixed
- `fix(bootstrap)` `WriteString(Sprintf)` → `Fprintf` (staticcheck QF1012) (`b700ff7`)

### Security
- cosign keyless 서명 (Sigstore Fulcio) — release artifact 무결성
- audit chain leader-gate + leader_epoch fence token (split-brain 방지)
- TPM 2.0 PCR-sealed key (E34)

---

## Pre-v0.2.0 historical entries

> Phase 0~1 초기 부트스트랩 기록 (2026-04-23). 본 entries는 v0.2.0 release 시점에 changelog 정식화가 진행되기 전 작성된 초기 항목으로, 역사 기록 보존을 위해 유지합니다. 향후 별도 chronological release tag로 정리될 가능성 있음.

### Added (Phase 0 — 설계)

- 2026-04-23 — 13개 설계 문서 초안 작성 (`docs/design/` Draft v0.1)
  - `00-mission-and-positioning.md` 미션·CAI 대비 포지셔닝
  - `01-principles.md` 12개 설계 원칙
  - `02-system-overview-and-deployment.md` 3종 배포 타깃
  - `03-architecture.md` 레이어·도메인·프로세스 토폴로지
  - `04-domain-and-data-model.md` 도메인 모델·SQL 스키마
  - `05-api-and-auth.md` HTTP/WS API·인증
  - `06-security-and-tenancy.md` 보안·멀티테넌시
  - `07-scan-engine-and-benchmarks.md` 스캔·벤치마크 팩
  - `08-intelligence-and-compliance.md` LLM·컴플라이언스
  - `09-ui-and-clients.md` Web/Desktop/CLI
  - `10-audit-and-observability.md` 해시 체인·관측성
  - `11-tech-stack-and-roadmap.md` 스택 선택·로드맵
  - `12-migration-and-non-goals.md` 자산 승계·비목표·리스크
- 2026-04-23 — `CLAUDE.md`, `SESSION_HANDOFF.md`, `README.md`, `CONTRIBUTING.md` 작성
- 2026-04-23 — 리포 부트스트랩(`.gitignore`, `.editorconfig`, `LICENSE` placeholder)

### Added (추가)

- 2026-04-23 — `contrib/source-benchmarks/README.md` 작성 — 전신 `nrobotcheck/resources/baselines/`의 원본 자료(CIS·ROS2 베이스라인 JSON·SCAP XML) 경로·크기·SHA-256·라이선스·타깃 팩 포인터. 파일 자체는 복사하지 않음.

### Added (Step 0.2 — Go 부트스트랩)

- 2026-04-23 — Apache License 2.0 본문 `LICENSE` 등록 (Copyright 2026 rosshield Contributors).
- 2026-04-23 — Go 모듈 초기화: `go.mod` (module `github.com/ssabro/rosshield`, go 1.26).
- 2026-04-23 — `Makefile` — `build`·`test`·`test-race`·`vet`·`fmt`·`tidy`·`lint`·`openapi`·`ci`·`clean` 타깃.
- 2026-04-23 — `.golangci.yml` v2 — `errcheck`·`govet`·`staticcheck`·`ineffassign`·`unused` + `gofmt`/`goimports` 포매터.
- 2026-04-23 — `.github/workflows/ci.yml` — Go 1.26 `ubuntu-latest` tidy → vet → build → test(-race) → golangci-lint 파이프라인.
- 2026-04-23 — `cmd/rosshield-server/main.go`/`main_test.go` — `/healthz` GET 200 JSON 스텁 + TDD 단위 테스트 2건(200/JSON body, POST 거부).

### Added (Step 0.3 — OpenAPI 스켈레톤)

- 2026-04-23 — `openapi/openapi.yaml` v0.0.1 (OpenAPI 3.1) — 엔벨로프(`Envelope`/`ErrorEnvelope`) + 8-카테고리 `ErrorCategory` + `Meta`/`PageMeta` + 공통 파라미터(`Limit`/`Cursor`/`Sort`/`IdempotencyKey`) + 보안 스키마(`bearerAuth`/`apiKeyAuth`). 대표 경로 11종(`/healthz`, `/readyz`, `/api/v1/auth/{login,me}`, `/api/v1/tenants/current`, `/api/v1/robots{,/{id}}`, `/api/v1/scans`, `/api/v1/reports/{id}:verify`, `/api/v1/audit/{head,verify}`) 스텁. 미구현 경로는 `x-status: todo`로 표기. 설계서 §5.12의 split 구조는 파일 크기 400줄 근처 진입 시 분할 예정.

### Added (Step 0.4 — Phase 1 백로그)

- 2026-04-23 — `docs/design/phase1-backlog.md` Draft v0.1 — Phase 1(Core MVP) 체크리스트를 에픽 12개(E1 Platform L1 → E2 Audit → E3 Tenant/Auth → E4 Pack 시스템 → E5 Robot/Fleet → E6 SSH+Scan → E7 Evidence → E8 Reporting → E9 CLI → E10 Web UI → E11 Compose 번들 → E12 pack-tools) × TDD 단위 태스크로 분해. 의존 그래프, 에픽별 인터페이스·대표 테스트·Exit 기준·설계 참조·기간 추정(총 11.5주 + 0.5주 범퍼 = 12주) 포함. 설계 문서 인덱스 README에 Part VII 섹션으로 등록.

### Added (Phase 1 — 구현 착수)

- 2026-04-23 — **E1.T1 Logger** (`internal/platform/logger/`) — `context.Context` 기반 구조화 로그. `slog.Handler` 래퍼가 `tenantId`/`requestId`/`traceId`를 자동 첨부. `WithTenantID`/`WithRequestID`/`WithTraceID` 주입 API + 동명 추출 API. TDD 5건(fields 실림, 미설정 필드 생략, 추출 헬퍼, 빈 ctx 추출, `With()` 후 ctx 필드 유지) 모두 pass. CI green.

### Added (Phase 1 — 사전 설계 노트)

- 2026-04-23 — `docs/design/notes/e1-storage-deepdive.md` (502줄) — E1.T4/T5 Storage 레이어 사전 설계. 드라이버 선택(`modernc.org/sqlite` 채택, 단일 정적 바이너리 원칙 정합), PRAGMA 고정값, SQLite↔PG 공존 전략(런타임 config + 분리 마이그레이션), 마이그레이션 툴(`pressly/goose`), Tx 함수형 API, Audit append-only 트리거, 테넌시 로우 레벨 격리, 테스트 전략, Go 인터페이스 스케치(`Storage`/`Tx`/`Repository[T,ID]`), E1.T4 착수 전 결정 필요 7건.
- 2026-04-23 — `docs/design/notes/e1-eventbus-deepdive.md` (444줄) — E1.T6/T7 EventBus 사전 설계. 아키텍처(channel-per-subscriber fan-out), 구독 lifecycle, goroutine 모델, backpressure(기본 DropOldest+256, audit Block+1024 override), panic 격리, 이벤트 envelope(§3.6 정합), correlation/causation ctx 전파, **audit 통합 후보 B 추천**(명시 `audit.Append()` + 커밋-후-퍼블리시 + outbox), 테스트 synchronous drain, NATS/Redis future compat interface 경계, Go 인터페이스 스케치, E1.T6 착수 전 결정 필요 7건.

### Decisions

- 2026-04-23 — 리포를 `D:\robot\dev\nrobotcheck` 전신과 분리해 `D:\robot\dev\fleetguard`로 신설
- 2026-04-23 — 상업화 방향: 헤드리스 코어 + 배포 3종(데스크톱·온프렘·어플라이언스 이미지)
- 2026-04-23 — 어플라이언스 자체 제조 포기, 이미지 + 파트너 채널 모델 채택
- 2026-04-23 — CAI와의 포지션 분리: 자율 공격 에이전트 프레임워크는 비목표
- **2026-04-23 — D2**: 백엔드 `Go`, 프론트 `TypeScript` 확정. 단일 정적 바이너리 + 에어갭 원칙 부합.
- **2026-04-23 — D3**: 데스크톱 셸 `Tauri 2.x` 확정 (Electron fallback 보류).
- **2026-04-23 — D5**: 라이선스 `Open-core` — 코어 Apache-2.0 + 엔터프라이즈 closed.
- **2026-04-23 — D6**: 리포 호스팅 `GitHub private` → Phase 1 exit 후 public 전환.
- **2026-04-23 — D1 부분 확정**: 코드네임 `rosshield` 채택(Google 검색으로 충돌 없음 확인). 제품 브랜드는 placeholder로 유지 → 2026-05-18 D-P7-1에서 **Lodestar**로 최종 확정. 초기 가칭 "FleetGuard"는 Cummins·Attestor.ai·TrustArc 등과 상표 충돌로 폐기.
- **2026-04-23 — D4 연기**: 어플라이언스 OS 기본 가정 `Ubuntu Core 24`, Phase 3 exit 재확정.
