# 09. UI 및 클라이언트 설계

## 9.1 UI 철학

1. **하나의 React 앱**이 세 배포 형태(데스크톱·온프렘·어플라이언스)를 모두 커버한다.
2. **사용자 유형별로 뷰가 달라진다** — 운영자·감사자·경영진이 다른 것을 본다.
3. **증거는 언제나 한 번의 클릭 거리 안에** — 체크 결과에서 원본 stdout까지 길을 끊지 않는다.
4. **UI 없이도 전부 CLI로 가능**해야 한다 (CI 사용 사례, 자동화).
5. **한국어 1급**. 영어 병행이되 한국어 번역이 주산물.

## 9.2 클라이언트 3종

### Web Console (주 UI)

- React 19 + TypeScript + shadcn/ui + Tailwind CSS v4 + TanStack Router/Query + Zustand (기존 자산 승계).
- Core가 `/`에 정적 자산 서빙. 절대 경로 없이 SPA.
- 접근성(AA 등급) 기본. 키보드 내비게이션 완전 지원.

### Desktop Shell

- **Tauri 2.x** 권장 (번들 크기·메모리·빌드 파이프라인 기준).
- 내부 Core 프로세스를 자식으로 띄우고 localhost 임의 포트에 바인딩.
- WebView로 같은 React 앱 로드.
- 네이티브 기능(파일 대화상자·알림·트레이 아이콘)은 Tauri 명령으로.
- **Electron fallback**: 기존 자산 재활용 긴급도에 따라 1.x는 Electron 유지도 허용. 상세는 `11-tech-stack-and-roadmap.md` §데스크톱 셸 결정.

### CLI

- 같은 HTTP API를 소비하는 얇은 클라이언트.
- 주요 명령: `fg login`, `fg scan`, `fg report`, `fg pack install`, `fg audit verify`.
- 출력은 표·JSON·YAML 선택 (`-o json` 등).
- CI 친화: 종료 코드 규칙(0=pass, 1=has_failures, 2=error).
- 단일 정적 바이너리 (Linux/macOS/Windows).

## 9.3 정보 구조 (IA)

```
Top-level 내비게이션
├─ Overview        (테넌트 홈, KPI 카드)
├─ Fleets          (플릿·로봇 관리)
├─ Scans           (세션 목록·실행·차분)
├─ Findings        (Insights, Drifts, Anomalies)
├─ Compliance      (프레임워크·통제·점수)
├─ Reports         (생성·다운로드·서명 검증)
├─ Benchmarks      (설치된 팩 관리)
├─ Advisor         (대화형 AI, 옵트인 시만 노출)
├─ Audit           (감사 로그·해시 체인·내보내기)
└─ Settings        (테넌트·사용자·통합·라이선스·업데이트)
```

## 9.4 주요 뷰

### Overview (대시보드)

- 오늘의 플릿 상태 (pass/fail 비율, 추세)
- 최근 24h의 drift·anomaly 수
- 컴플라이언스 프레임워크별 점수 카드
- 다가오는 스캔 스케줄
- 라이선스·팩 업데이트 알림

### Fleet → Robot List

- 필터: 태그, OS, ROS distro, criticality, 마지막 스캔 시간.
- 각 행: 기본 정보 + 마지막 스캔 요약 + 즉시 스캔 버튼.
- 벌크 액션: 태그 편집·스캔·태그·삭제.

### Robot Detail

- 연결 상태·자격증명 관리·피어 그룹.
- 최근 스캔 세션 타임라인.
- 체크별 추세 미니 차트 (pass/fail history).
- 온보딩 진단 결과(처음 등록 시 수집된 환경 정보).

### Scan Session Detail

- 요약: 대상 로봇·팩·레벨·시간.
- 결과 테이블: robot × check matrix.
- 실패 체크 필터 / 드리프트만 보기 / 재사용(차분) 표시.
- 결과 행 클릭 → **Evidence 드로어**: stdout 원문, redaction 배지, 재현 명령.
- 이 세션에서 발생한 Insight 목록 (하단 패널).

### Compliance View

- 프레임워크 탭 (ISO27001·NIST·ISMS-P 등).
- 통제 트리 (카테고리 → 소분류 → 개별 통제).
- 각 통제: 상태 배지, 매핑된 체크 수, 클릭 시 매핑·결과 드릴다운.
- 점수 추세 그래프 (30/90일).
- "리포트 생성" 버튼.

### Report

- 세션·프레임워크·플릿 기반 생성 위저드.
- 형식 선택(Markdown/PDF/HTML/XLSX).
- 진행 상황 WebSocket 스트리밍.
- 완료 후: 서명 검증 상태 표시·다운로드·공유 링크(만료 시간).

### Advisor

- 채팅 UI. 스레드형 대화.
- 사용자 입력 옆에 "현재 컨텍스트(선택된 세션·로봇)" 배지.
- 모델 응답에 **Evidence 인용 하이라이트** — 해당 줄 클릭하면 Evidence 드로어 열림.
- 오프라인/fallback 시 "AI 응답을 사용할 수 없습니다" 배너.

### Audit View

- 이벤트 스트림(WebSocket 실시간).
- 필터: 시점·행위자·대상 타입·액션.
- 체인 무결성 뱃지 (ChainHead 검증 상태).
- 내보내기: 서명된 번들 다운로드.

### Settings

- Tenant: 이름·플랜·라이선스·업데이트 채널.
- Users & Roles: 초대·역할 부여·2FA 강제.
- Integrations: SSO·LDAP·Webhook·SIEM·LLM provider.
- Intelligence Features: 기능별 토글 (기본 off).
- Compliance Frameworks: 활성화·커스터마이징.
- Benchmark Packs: 설치·업데이트·Self-Test 결과.
- Retention: 데이터 보존 기간 조정.
- Appliance-only: OTA 업데이트·디스크 암호화 상태·TPM 헬스.

## 9.5 상태 관리

### Zustand (client store)

- UI 로컬 상태(열린 드로어, 필터 선택, 토스트).
- 세션 토큰은 메모리 + HttpOnly 쿠키 이중.

### TanStack Query

- 서버 데이터 caching·refetch.
- 낙관적 업데이트(optimistic)는 스캔 시작·리포트 생성 등 빠른 피드백 필요한 곳에만.

### WebSocket

- 이벤트를 TanStack Query cache invalidation으로 연결.
- `ScanProgress` → 해당 session 쿼리 refetch.
- Advisor 스트리밍 chunk → 로컬 state.

## 9.6 i18n

- 기본 언어: 한국어. Secondary: 영어.
- 리소스 파일: `locales/{ko,en}/*.json` + 도메인별 네임스페이스.
- 사용자 언어 설정: Tenant 기본값 + User 개인 override.
- 체크 title·description·remediation은 **팩 안에서** 양 언어 제공(`{ ko, en }`).

## 9.7 접근성

- WAI-ARIA landmarks, role, label.
- 키보드 탐색: Tab 순서, skip link.
- 대비 AAA 레벨 제공(설정에서 고대비 모드).
- 읽기 전용 스크린리더 테스트: NVDA, VoiceOver.

## 9.8 다크 모드

- 시스템 테마 자동 + 수동 토글.
- Tailwind `dark:` 변형 일관되게.

## 9.9 반응형

- 주 대상: 데스크톱 1280px 이상.
- 태블릿 768+: 간소화 뷰.
- 모바일: **뷰 전용** (경보 확인·간단한 상태 보기). 쓰기 기능은 제한.

## 9.10 알림·토스트·다이얼로그

- 토스트: 짧은 액션 피드백(4초).
- 다이얼로그: 파괴적 액션(삭제·revoke) 2단계 확인.
- 알림 센터: Overview 우상단 벨 — webhook이 아닌 내부 이벤트(시스템 팩 업데이트 가능 등).

## 9.11 Desktop Shell 고유 기능

- **OS 알림**: 스캔 완료, 드리프트 발견.
- **트레이 아이콘**: 백그라운드에서도 예약 스캔 실행.
- **파일 저장**: 리포트 다운로드 시 네이티브 대화상자.
- **단일 인스턴스**: 중복 실행 방지, 기존 창에 포커스.
- **OS 로그인 시 자동 시작** (옵션).

## 9.12 CLI 명령 체계 (예시)

```
fg --version
fg login [--server URL] [--api-key ...]
fg robot list
fg robot add --name amr-01 --host 10.0.0.23 --key-file ./id_rsa
fg scan run --fleet production --pack cis-ubuntu-24.04 --level L1
fg scan status ss_01H...
fg scan results ss_01H... --format json
fg report generate --session ss_01H... --format pdf --template default
fg pack list
fg pack install ./cis-ubuntu-24.04-v1.2.3.pack
fg pack selftest cis-ubuntu-24.04
fg audit verify --from 2026-01-01 --to 2026-03-31
fg audit export --format bundle --out audit-2026Q1.tar.gz
```

### 출력 모드

- Default: human-readable 표.
- `-o json` / `-o yaml` / `-o ndjson`: 기계판독.
- `-q`: 조용 모드(exit code만).
- `-v` / `-vv`: 상세.

### 종료 코드 규약

- `0`: 성공, 전원 pass.
- `1`: 성공했으나 fail 체크 존재.
- `2`: 실행 오류(네트워크·인증 등).
- `3`: 입력·설정 오류.

## 9.13 임베디드 문서·도움말

- 각 섹션 우상단 `?` 버튼 → 해당 기능 도움말 드로어.
- 인라인 팁 대신 명확한 링크(너무 많은 툴팁은 피로).
- 사용 설명서(HTML·PDF)는 **팩과 동일한 업데이트 주기**.

## 9.14 UI 테스트

- **컴포넌트**: Vitest + React Testing Library.
- **통합**: Playwright — 실제 백엔드 컨테이너와 E2E.
- **시각 회귀**: Chromatic 또는 Playwright snapshot.
- 접근성 체크: axe-core 자동화.

## 9.15 성능 목표

- 초기 로드(First Contentful Paint): 데스크톱 < 1.5s, 서버 < 2.5s.
- 라우트 전환: < 200ms.
- 대용량 결과 테이블(> 10,000 행): 가상 스크롤로 부드럽게.
- WebSocket latency: < 200ms 도달.

## 9.16 기존 nrobotcheck UI 재활용

| 자산 | 전략 |
|---|---|
| `src/renderer/src/components/*` shadcn/ui 래퍼 | 그대로 복사 |
| 페이지 컴포넌트 | **수정 후 이전**: 멀티테넌시·신규 네비게이션에 맞춤 |
| `SessionSummaryDialog`·Compliance 카드 | 세부 개선 후 재사용 |
| 온보딩 마법사(F19) | 전체 IA에 맞춰 "로봇 추가" 플로우로 이전 |
| Zustand 스토어 | API 클라이언트 어댑터에 맞춰 재조정 |

자세한 승계 계획은 `12-migration-and-non-goals.md`.

## 9.17 이 문서의 핵심 결정

1. **한 React 앱 · 세 껍질 · 하나의 CLI** — 중복 코드 금지.
2. **Tauri 권장**, Electron fallback 허용.
3. **한국어 1급 + 접근성 AA** — 국내 B2B 시장 필수.
4. **증거 드로어 항상 한 클릭 거리** — 감사 가능성 UX로 실현.
5. **CLI는 일등 시민** — CI 사용 사례를 UI와 동등하게.

다음 문서: [10-audit-and-observability.md](./10-audit-and-observability.md)
