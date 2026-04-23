# Changelog

이 프로젝트의 주요 변경 사항을 기록합니다. 포맷은 [Keep a Changelog](https://keepachangelog.com/)를 따르고, 버저닝은 [Semantic Versioning](https://semver.org/)을 따릅니다.

## [Unreleased]

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
- **2026-04-23 — D1 부분 확정**: 코드네임 `rosshield` 채택(Google 검색으로 충돌 없음 확인). 제품 브랜드는 `<ProductName>` placeholder로 유지, Phase 1 후반 최종 확정. 초기 가칭 "FleetGuard"는 Cummins·Attestor.ai·TrustArc 등과 상표 충돌로 폐기.
- **2026-04-23 — D4 연기**: 어플라이언스 OS 기본 가정 `Ubuntu Core 24`, Phase 3 exit 재확정.
