# 11. 기술 스택 결정 및 로드맵

## 11.1 결정 접근 방식

각 스택 선택은 **"결정됨"**이 아니라 **"추천 + 트레이드오프 + 체크포인트"**로 기록됩니다. 최종 결정은 팀 상황(역량·자원·일정)에 따라 확정하고 이 문서를 업데이트하세요.

---

## 11.2 백엔드 언어 — 핵심 결정

### 후보

| 후보 | 장점 | 단점 |
|---|---|---|
| **Go** | 단일 바이너리, 메모리 효율, 크로스 컴파일, 훌륭한 SSH/crypto 라이브러리, 어플라이언스에 최적 | 팀이 이미 TS에 익숙한 경우 학습 곡선, 제네릭·메타프로그래밍 제약 |
| **TypeScript (Node.js)** | 기존 `nrobotcheck` 자산·팀 역량 승계, 프론트와 언어 공유 | 런타임 번들 크기·배포 복잡도, 네이티브 모듈(better-sqlite3) ABI 문제, 장기 실행 메모리 관리 |
| **Rust** | 성능·안전성 최상 | 개발 속도 느림, 생태계가 SSH/SQL에서 상대적으로 덜 성숙, 채용 어려움 |

### 권장: **Go 백엔드, TypeScript 프론트**

**근거**:

1. **단일 정적 바이너리**가 3종 배포 형태(데스크톱·온프렘·어플라이언스)에 결정적 이점. Tauri 자식 프로세스·Docker 슬림 이미지·어플라이언스 immutable 이미지 모두에 자연스러움.
2. **Go의 SSH/암호/체인 라이브러리**(`golang.org/x/crypto`, `crypto/tls`, `crypto/ed25519`)가 감사 도구의 핵심 요구와 맞음.
3. **TypeScript Node.js**는 `nrobotcheck`에서 이미 네이티브 모듈·Electron ABI 관리의 복잡도를 경험한 바 있음. 상용화 단계에서 운영 부담이 크다.
4. **팀 역량**: TS 자산·문법 친숙함은 **프론트**에 집중시키고, 백엔드는 Go로 명확히 분리. 언어 스위칭 비용보다 장기 운영 비용이 중요.

### 다만 — TypeScript 유지 옵션도 유효

팀이 Go 전환을 감당 어렵다면:

- 백엔드도 TypeScript (Node.js 22+, Fastify/Nest 등).
- `better-sqlite3` 대신 `libsql` 또는 `pg-native`.
- 데스크톱은 `pkg` 같은 도구로 단일 바이너리화.
- 배포 이미지 슬림화에 더 신경 써야 함.

이 선택지는 **"Stage 1을 6개월 단축"**하는 대신 **"Stage 3 어플라이언스에서 운영 부담 가중"**의 트레이드오프.

### 체크포인트 결정

Stage 1 시작 전 **2주 스파이크**:
- Go·TS 각각으로 최소 기능(`POST /scans` + SSH exec + SQLite 저장 + `/healthz`)을 만들어 보고,
- 빌드 크기·메모리·개발 체감 속도를 수치로 비교.

## 11.3 핵심 라이브러리 (Go 기준)

| 용도 | 라이브러리 |
|---|---|
| HTTP | `net/http` + `chi` 또는 `echo` |
| WebSocket | `nhooyr.io/websocket` 또는 `gorilla/websocket` |
| OpenAPI 생성 | `oapi-codegen` |
| SQLite | `modernc.org/sqlite` (CGO 없음) 또는 `mattn/go-sqlite3` |
| PostgreSQL | `jackc/pgx/v5` |
| SSH | `golang.org/x/crypto/ssh` |
| YAML | `go.yaml.in/yaml/v4` |
| JSON Schema | `santhosh-tekuri/jsonschema` |
| 서명 | `crypto/ed25519` |
| 로깅 | `log/slog` |
| 메트릭 | `prometheus/client_golang` + OTel SDK |
| 추적 | OpenTelemetry SDK |
| 테스트 | `testify`, `testcontainers` |

## 11.4 핵심 라이브러리 (TypeScript 프론트)

기존 `nrobotcheck` 선택 승계:

- React 19, Vite, Tailwind CSS v4, shadcn/ui, TanStack Router/Query, Zustand, react-hook-form, zod.
- i18n: `i18next` + 번들 분할.
- 폼 유효성: zod 스키마(OpenAPI에서 생성).

추가:

- **OpenAPI → 클라이언트 SDK**: `openapi-fetch` + 생성된 타입.

## 11.5 데이터베이스

| 용도 | 선택 |
|---|---|
| 데스크톱·소규모 온프렘 | **SQLite** (WAL 모드, 단일 파일) |
| 중·대형 온프렘·어플라이언스 | **PostgreSQL 16+** |
| 대규모 분리 모드 | PostgreSQL 클러스터 (primary + replica) |
| 읽기 전용 분석(선택) | Columnar (DuckDB 임베드) — Insight 집계용 v2 |

- SQLite ↔ PostgreSQL **스키마 호환** 유지. DDL은 `USE_PG = true/false` 빌드 플래그로 작은 차이만.
- ORM은 **피함**: 쿼리 명시적으로. `sqlc`(Go) 또는 `kysely`(TS)로 타입 안전.

## 11.6 Blob 스토리지

| 배포 | 선택 |
|---|---|
| 데스크톱 | 로컬 파일시스템 (`~/.fleetguard/blobs`) |
| 온프렘 | **MinIO 번들** 또는 고객 S3 호환 |
| 어플라이언스 | 로컬 디스크 + LVM 암호화 |

S3 API만 코드에 있고, 로컬 파일시스템은 **MinIO gateway**가 아니라 **파일 어댑터 구현체**.

## 11.7 이벤트 버스

| 배포 | 선택 |
|---|---|
| 모노리스 | in-process pub/sub (채널·Go channel/Node EventEmitter) |
| 분리 모드 | **NATS JetStream** 또는 Redis Streams |

외부 큐를 요구하지 않는 단일 프로세스를 기본으로.

## 11.8 Desktop Shell 결정

| 후보 | 평가 |
|---|---|
| **Tauri 2.x** | 번들 작음(수십 MB), Rust 백엔드 지만 Go 서브프로세스 방식으로 독립 가능, WebView2/WKWebView/WebKitGTK 활용. **권장**. |
| **Electron** | `nrobotcheck` 자산 재활용 최대. 번들 큼(150MB+), 네이티브 모듈 ABI 고민. 긴급 출시 시 1.x에서 허용. |
| Wails | Go 네이티브 친화. Tauri 대비 생태계 작음. |
| .NET MAUI | Windows 주력이면 가능. 크로스 플랫폼 성숙도 중간. |

**권장**: Tauri 2.x. Electron은 1.0 출시 초기에 시간 제약이 있을 때만 fallback.

## 11.9 어플라이언스 OS

| 후보 | 평가 |
|---|---|
| **Ubuntu Core 24** | snap 기반 immutable. 검증된 생태계. **권장**. |
| Fedora IoT / OSTree | 원자적 업데이트 강력. 유지 주체 이슈 모니터링 필요. |
| Debian + custom | 유연하지만 직접 관리 부담. |
| Talos Linux | K8s 어플라이언스에 최적. 과잉. |

**권장**: Ubuntu Core. Core 컴포넌트는 snap으로, 추가 서비스는 OCI 컨테이너(snap내 podman)로 감싸 A/B 슬롯 업데이트.

## 11.10 빌드·CI/CD

- **Monorepo**: `pnpm workspaces` + `turbo` (프론트·CLI·도구) + Go 모듈 (백엔드).
- **CI**: GitHub Actions (공개 OSS 성격 파트) + 자체 러너(상용 빌드).
- **아티팩트**:
  - `fg-server` (Linux/amd64·arm64)
  - `fg-cli` (Linux/macOS/Windows × amd64·arm64)
  - `fleetguard-desktop` (Tauri, 3 OS)
  - OCI 이미지 (`ghcr.io/org/fleetguard:1.3.2`)
  - 어플라이언스 스냅·이미지
- **서명**: 모든 아티팩트는 sigstore cosign 서명.
- **SBOM**: 릴리스마다 `syft` 생성.

## 11.11 테스트 스택

- Go: `testing` + `testify` + `testcontainers`.
- TS: Vitest + React Testing Library.
- E2E: Playwright + docker-compose 하네스.
- 팩 Self-Test: 자체 러너(CLI `fg pack selftest`).
- Fuzzing: Go `fuzz` (API 파서·팩 로더·표현식 엔진).

## 11.12 코딩 규칙·품질 게이트

- 파일 ≤ 400줄 권장, ≤ 800줄 최대.
- 함수 ≤ 50줄 권장.
- 순환 복잡도 ≤ 10.
- PR 병합 조건: 테스트·린트·typecheck 녹색 + 2인 리뷰(보안 민감 영역).
- Go: `gofmt`, `staticcheck`, `golangci-lint`.
- TS: `eslint` (엄격 프리셋), `prettier`.
- 도메인 의존 방향 린트: 자체 툴 또는 `depguard`.

---

## 11.13 로드맵

### Phase 0 — 준비 (2~4주)

- [ ] 스택 결정 스파이크 (Go vs TS)
- [ ] 제품명·도메인·브랜드 확보
- [ ] OpenAPI 1.0 초안 스켈레톤
- [ ] CI 파이프라인 설정
- [ ] 라이선스·기여 가이드

### Phase 1 — Core MVP (10~12주)

**목표**: Tenant 1개, Fleet 1개, 로봇 N대를 CIS Ubuntu 팩으로 감사하고 PDF 리포트 생성.

- [ ] Platform services: Storage, EventBus, SSHPool, Signer, Logger
- [ ] Domains: tenant, fleet, robot, benchmark, scan, evidence, reporting, audit
- [ ] API Gateway: auth (local 계정 + API Key), envelope, v1 스키마
- [ ] Web UI: Overview, Fleet/Robot 페이지, Scan, Report
- [ ] CLI: login, robot, scan, report 기본 명령
- [ ] Pack 로더 + 서명 검증 + Self-Test 프레임워크
- [ ] Audit 해시 체인 + 외부 검증 API
- [ ] Docker Compose 기본 번들
- [ ] `nrobotcheck` CIS 팩·ROS2 Jazzy 팩 변환

**Exit 기준**: 내부 환경에서 로봇 3대 감사·서명 PDF·감사 체인 외부 검증 성공.

### Phase 2 — Intelligence & Compliance (8~10주)

- [ ] LLM Adapter + noop/ollama/anthropic
- [ ] Insight: drift, anomaly, peer comparison (규칙 기반)
- [ ] Root cause + LLM 설명 (옵트인)
- [ ] Advisor 대화 오케스트레이터 + 툴콜
- [ ] Compliance: ISO27001, NIST 800-53, ISMS-P 프로필
- [ ] Framework 리포트 PDF
- [ ] LLM 자동 매핑 제안

**Exit 기준**: ISMS-P 통제 기준 점수·리포트 생성, 감사 체인 포함 외부 검증.

### Phase 3 — Multi-tenant SKU (8~10주)

- [ ] SSO (OIDC + SAML), 초대·역할 관리
- [ ] Multi-tenant 스키마 + 격리 강화 테스트
- [ ] PostgreSQL 프로덕션 배포 경로 (Helm 차트)
- [ ] Webhook·SIEM 연동
- [ ] 라이선스·쿼터·과금 훅
- [ ] 고가용성 배포 모드 (옵션)

**Exit 기준**: 첫 유료 Enterprise 고객 PoC 배포.

### Phase 4 — Appliance (6~8주)

- [ ] Ubuntu Core 이미지 빌드 파이프라인
- [ ] TPM 키 봉인
- [ ] 디스크 암호화 + Secure Boot
- [ ] 오프라인 팩 번들 설치 도구
- [ ] A/B 슬롯 OTA 업데이트
- [ ] 레퍼런스 하드웨어 테스트 (NUC/OptiPlex 2종)

**Exit 기준**: 파트너 1곳과 어플라이언스 PoC 설치 성공.

### Phase 5 — 생태계 (지속)

- [ ] Pack Mirror 공개 서비스
- [ ] 파트너 OEM 라이선스
- [ ] 추가 프레임워크 매핑
- [ ] 플러그인 SDK
- [ ] 공개 문서·학술 논문·케이스스터디

---

## 11.14 리소스 가정

| 단계 | 팀 크기 | 기간 |
|---|---|---|
| Phase 0 | 2명 | 3주 |
| Phase 1 | 3~4명 | 12주 |
| Phase 2 | 4~5명 | 10주 |
| Phase 3 | 5명 | 10주 |
| Phase 4 | 3명 | 8주 |

팀 구성 권장:
- 백엔드 2~3명 (Go)
- 프론트 1~2명 (React/TS)
- 풀스택/DevOps 1명
- 보안·컴플라이언스 도메인 1명 (파트타임 가능)
- QA/릴리스 엔지니어 1명 (Phase 3부터)

## 11.15 주요 리스크 & 체크포인트

| 리스크 | 영향 | 완화 |
|---|---|---|
| 스택 선택 후회 | 일정 3~6개월 지연 | Phase 0 스파이크로 조기 검증 |
| 팩 콘텐츠 품질 부족 | 제품 가치 결여 | Self-Test 의무, 내부 QA VM |
| LLM 비용 폭주 | 수익성 악화 | 옵트인·쿼터·로컬 모델 우선 |
| 어플라이언스 지원 공수 | 대형 고객 대응 부담 | 파트너 채널, 이미지 표준화 |
| 규제 해석 오차 | 감사 실패 주장 | 법무 검토·면책 조항·인증 파트너 |

## 11.16 결정 로그 (초기)

- [ ] 2026-Qx: 제품명 확정
- [ ] 2026-Qx: 백엔드 언어 확정 (Go 또는 TS)
- [ ] 2026-Qx: 데스크톱 셸 확정 (Tauri 또는 Electron)
- [ ] 2026-Qx: 어플라이언스 OS 확정 (Ubuntu Core 또는 대안)
- [ ] 2026-Qx: 라이선스 모델 확정 (OSS vs closed vs open-core)

이 결정은 각 항목이 끝날 때 본 문서 §11.2~§11.9를 업데이트하는 방식으로 기록합니다.

다음 문서: [12-migration-and-non-goals.md](./12-migration-and-non-goals.md)
