# (가칭) `<ProductName>` — 차세대 ROS2 플릿 보안 감사 플랫폼 설계서

> **상태**: Draft v0.1
> **작성일**: 2026-04-23
> **관계**: `nrobotcheck`(현행 Electron 앱)의 개념과 자산을 차용하되 **완전히 새로운 코드베이스**로 재설계한 후계 제품
> **목적**: 상업화 가능한 멀티-배포 제품(데스크톱 / 온프렘 서버 / 어플라이언스 이미지)을 처음부터 염두에 둔 설계

---

## 왜 새 프로젝트인가

`docs/COMMERCIALIZATION_STRATEGY.md`에서 합의한 대로 기존 `nrobotcheck`는 이론상 헤드리스 전환이 가능합니다. 다만 다음 이유로 **새 코드베이스가 더 빠르고 안전**합니다.

1. **Electron-first 가정**이 파일·폴더·빌드 설정에 깊이 박혀 있음 (electron-vite, preload 브릿지, BrowserWindow 전제)
2. **단일 사용자 전제**로 SQLite 스키마·설정 저장·IPC 계약이 구성됨 — 멀티테넌시는 컬럼 증분이 아니라 **재설계 대상**
3. **벤치마크 포맷이 내부 전용** — 외부 플러그인·서명·배포 체계가 없음
4. **감사 추적의 외부 검증 가능성** — 현재 해시 체인은 내부 전용. 상용 제품은 **발급 주체·검증 API·제3자 검증 가능한 서명**이 필요
5. **제품·브랜드 분리**: `nrobotcheck`는 오픈소스/커뮤니티 축으로 유지하고, 상용 제품은 독립 정체성으로 출발하는 편이 영업·법무에 유리

동시에 **기존 자산을 최대한 차용**합니다 — 도메인 모델, 벤치마크 DB, 평가 로직, UI 컴포넌트 라이브러리, 평가 알고리즘, 컴플라이언스 매핑 데이터 등. 자세한 재활용 범위는 `11-migration-and-non-goals.md` 참조.

> 참고: 제품 브랜드는 미확정(D1 연기). 모든 문서는 `<ProductName>` placeholder를 사용합니다. 코드 네임스페이스는 `rosshield`로 확정(2026-04-23) — Go 모듈·내부 패키지·설정 경로에서 사용. 브랜드 확정은 Phase 1 후반으로 연기. 초기 가칭 "FleetGuard"는 Cummins의 Fleetguard(엔진 필터 브랜드) 및 Attestor.ai 등과 상표 충돌 가능성이 있어 폐기됨.

---

## 문서 구성 (13개)

### Part I · 전략 및 원칙

| # | 문서 | 내용 |
|---|---|---|
| 00 | [00-mission-and-positioning.md](./00-mission-and-positioning.md) | 미션, 시장 포지셔닝, CAI 대비 차별화, 고객 세그먼트 |
| 01 | [01-principles.md](./01-principles.md) | 설계 원칙 12개 (옵트인 지능화·에어갭 1급·결정론적 fallback 등 승계 + 상용화 원칙 추가) |

### Part II · 아키텍처

| # | 문서 | 내용 |
|---|---|---|
| 02 | [02-system-overview-and-deployment.md](./02-system-overview-and-deployment.md) | 시스템 한 눈에, 배포 타깃 3종(데스크톱·온프렘 서버·어플라이언스 이미지) |
| 03 | [03-architecture.md](./03-architecture.md) | 레이어, 컴포넌트, 도메인 경계, 프로세스 토폴로지 |
| 04 | [04-domain-and-data-model.md](./04-domain-and-data-model.md) | Tenant·Fleet·Robot·Benchmark·Scan·Evidence·Insight·Compliance·Audit 모델 + SQL 스키마 |

### Part III · 인터페이스·보안

| # | 문서 | 내용 |
|---|---|---|
| 05 | [05-api-and-auth.md](./05-api-and-auth.md) | HTTP/WebSocket API, 엔벨로프, 버저닝, 인증 토큰, SSO, API Key |
| 06 | [06-security-and-tenancy.md](./06-security-and-tenancy.md) | 테넌시 격리, RBAC, 비밀 관리, 암호화, 에어갭 시나리오 |

### Part IV · 핵심 엔진

| # | 문서 | 내용 |
|---|---|---|
| 07 | [07-scan-engine-and-benchmarks.md](./07-scan-engine-and-benchmarks.md) | 스캔 실행, SSH 풀, Evidence 저장, 벤치마크 포맷·서명·배포·Self-Test |
| 08 | [08-intelligence-and-compliance.md](./08-intelligence-and-compliance.md) | LLM 어댑터·옵트인·컴플라이언스 프레임워크·점수·서명된 리포트 |

### Part V · 클라이언트·배포·운영

| # | 문서 | 내용 |
|---|---|---|
| 09 | [09-ui-and-clients.md](./09-ui-and-clients.md) | 웹 콘솔·데스크톱 셸·CLI, UX 패턴, i18n |
| 10 | [10-audit-and-observability.md](./10-audit-and-observability.md) | 감사 해시 체인(외부 검증 가능), 로그, 메트릭, 추적, 경보 |

### Part VI · 실행·결정

| # | 문서 | 내용 |
|---|---|---|
| 11 | [11-tech-stack-and-roadmap.md](./11-tech-stack-and-roadmap.md) | 언어·프레임워크 선택지, 권장안, Phase별 로드맵 |
| 12 | [12-migration-and-non-goals.md](./12-migration-and-non-goals.md) | nrobotcheck 자산 재활용 전략, 비목표, 주요 리스크 |

### Part VII · 실행 백로그 (살아있는 문서)

| 문서 | 내용 |
|---|---|
| [phase1-backlog.md](./phase1-backlog.md) | Phase 1(Core MVP) 에픽 12개 × TDD 태스크 분해, 의존 그래프, Exit 기준 |

---

## 한 눈에 보는 설계 요약 (TL;DR)

### 포지셔닝

> **"ROS2 로봇 플릿이 보안 기준을 지키고 있는지 매일 증명하는 도구."**
> CAI가 "공격 가능성 탐색"이라면, 이 제품은 **"방어 상태 지속 검증 + 감사 증거 생성"**.

### 아키텍처 3줄 요약

1. **헤드리스 코어(Go 또는 TS/Node) + HTTP/WS API**가 중심. UI와 CLI는 같은 API의 클라이언트.
2. **멀티테넌시 기본값** — 모든 테이블·API가 tenant 스코프를 처음부터 갖는다. 단일 사용자 데스크톱은 "기본 테넌트 1개"의 degenerate case.
3. **3가지 배포 형태를 같은 바이너리**로 — 데스크톱(로컬호스트 바인딩 + 내장 UI) / 온프렘 서버(k8s or compose) / 어플라이언스 이미지(immutable OS).

### 핵심 차별점

| 축 | 설계 |
|---|---|
| **감사 증거** | 해시 체인 + 외부 검증 API + 서명된 PDF. 감사인이 "받아들이는" 증거. |
| **벤치마크 생태계** | JSON/YAML 포맷 + 서명된 "팩" + OTA 업데이트 (에어갭 환경은 오프라인 번들) |
| **컴플라이언스 매핑** | CIS / NIST 800-53 / ISO27001 / IEC 62443 / ISMS-P / 주요정보통신기반시설 대응 |
| **LLM 의존성** | 완전 옵트인, 결정론적 fallback 필수. 오프라인 모델(Ollama/vLLM) 1급 지원. |
| **테넌시** | Row-level tenant scoping + 선택적 schema-per-tenant (대형 고객용) |
| **배포** | 같은 OCI 이미지. 어플라이언스는 OSTree/Ubuntu Core immutable root 위에 말림. |

### 비목표 (강조)

- 자율 공격 에이전트 프레임워크가 되지 않는다 (CAI 영토).
- 자체 하드웨어를 설계·제조하지 않는다.
- 범용 멀티 LLM 오케스트레이션 플랫폼이 되지 않는다.
- 인터넷 연결을 전제하는 SaaS-only 제품이 되지 않는다.
- 비-ROS2 / 비-Ubuntu 로봇 지원을 주력으로 삼지 않는다.

---

## 문서 읽기 순서

### 임원·PM

1. `00-mission-and-positioning.md`
2. `02-system-overview-and-deployment.md`
3. `11-tech-stack-and-roadmap.md` §로드맵

### 아키텍트

1. `01-principles.md`
2. `03-architecture.md`
3. `04-domain-and-data-model.md`
4. `05-api-and-auth.md`
5. `06-security-and-tenancy.md`

### 구현 엔지니어

1. `03-architecture.md`
2. `07-scan-engine-and-benchmarks.md`
3. `08-intelligence-and-compliance.md`
4. `11-tech-stack-and-roadmap.md`

### 보안·감사·컴플라이언스

1. `06-security-and-tenancy.md`
2. `08-intelligence-and-compliance.md`
3. `10-audit-and-observability.md`

---

## 변경 이력

| 날짜 | 버전 | 변경 |
|---|---|---|
| 2026-04-23 | 0.1 | 최초 초안. 13개 문서 세트 작성 |
