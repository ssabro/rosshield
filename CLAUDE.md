# CLAUDE.md

> Claude Code(claude.ai/code)가 이 리포에서 작업할 때 따라야 하는 지침.

## ⚠️ Session Resumption

**새 세션이거나 재설치·재개 상황이라면 [`SESSION_HANDOFF.md`](./SESSION_HANDOFF.md)를 먼저 읽어라.** 현재 Phase, 결정이 필요한 항목, 진행 중 선택지가 그 안에 있다.

## 프로젝트 개요

**이름**: 제품 브랜드는 **Lodestar**로 확정(2026-05-18, D-P7-1). 문서·UI의 사용자 대면 제품명은 `Lodestar`를 사용합니다. **코드 네임스페이스는 `rosshield`로 확정**(2026-04-23) — Go 모듈 `github.com/ssabro/rosshield`, 내부 패키지·설정 경로·YAML apiVersion 네임스페이스. 초기 가칭 "FleetGuard"는 Cummins·Attestor.ai 등과 상표 충돌로 폐기.

**요약**: ROS2 로봇 플릿 보안 감사 플랫폼. 감사인이 받아들이는 결정론적 증거와 서명된 리포트를 생성하는 상용 B2B 제품. 설계는 `nrobotcheck`(전신)의 개념·자산을 차용하되 **완전히 새로운 코드베이스**로 출발한다.

**현재 상태**: **Phase 0 (설계)**. 코드 0줄. 설계서 13개만 존재. 구현 스택 미확정.

## 설계 문서 (읽는 순서 중요)

```
docs/design/
├─ README.md                            # 인덱스·TL;DR·읽는 순서
├─ 00-mission-and-positioning.md        # 미션, CAI 대비 포지셔닝
├─ 01-principles.md                     # 설계 원칙 12개
├─ 02-system-overview-and-deployment.md # 3종 배포 타깃
├─ 03-architecture.md                   # 레이어·도메인·프로세스
├─ 04-domain-and-data-model.md          # 엔터티·SQL 스키마
├─ 05-api-and-auth.md                   # HTTP/WS API + 인증
├─ 06-security-and-tenancy.md           # 보안·멀티테넌시
├─ 07-scan-engine-and-benchmarks.md     # 스캔·벤치마크 팩
├─ 08-intelligence-and-compliance.md    # LLM·컴플라이언스
├─ 09-ui-and-clients.md                 # Web/Desktop/CLI
├─ 10-audit-and-observability.md        # 해시 체인·관측성
├─ 11-tech-stack-and-roadmap.md         # 스택 선택·로드맵
└─ 12-migration-and-non-goals.md        # 자산 승계·비목표·리스크
```

**처음 작업하는 Claude**: 최소 `README.md` + `01-principles.md` + `11-tech-stack-and-roadmap.md`는 읽어라. 작업 영역에 따라 추가로.

## 핵심 원칙 요약 (설계서 §01에서 발췌)

1. **감사인이 받아들이는 증거** — 결정론적 + 해시 체인 + 외부 검증
2. **옵트인 지능화** — AI 기능은 기본 비활성
3. **에어갭 1급** — 오프라인에서 완전 동작
4. **멀티테넌시 기본값** — 모든 테이블·API가 tenant 스코프
5. **DDD 경계** — 도메인 서비스가 다른 도메인 저장소를 직접 호출 금지
6. **결정론적 fallback** — AI는 규칙 기반의 보조
7. **단일 바이너리, 다중 껍질** — 데스크톱·온프렘·어플라이언스가 같은 코어
8. **컨텐츠/코드 분리** — 벤치마크·매핑은 서명된 팩
9. **데이터 불변성** — append-only
10. **프라이버시 기본값** — 로컬 우선
11. **설명 가능성** — 모든 AI 판단에 reasoning trace
12. **점진적 적용** — big-bang 금지

원칙 간 충돌 시 **번호가 작은 쪽이 이긴다**.

## 결정 현황 (Phase 0)

| # | 항목 | 상태 | 결정 | 참조 |
|---|---|---|---|---|
| D1 | 제품명·도메인·상표 | ✅ 2026-05-18 (D-P7-1) | 코드네임 `rosshield` 확정 + 제품 브랜드 **Lodestar** 확정 (Top 3 중 등록 가능성 최우선) | `00-*`, `notes/d1-brand-candidates.md`, `notes/phase7-public-transition-design.md` |
| D2 | 백엔드 언어 | ✅ 2026-04-23 | **Go** + TS 프론트 | `11-*` §11.2 |
| D3 | 데스크톱 셸 | ✅ 2026-04-23 | **Tauri 2.x** (Electron은 fallback 보류) | `11-*` §11.8 |
| D4 | 어플라이언스 OS | 🟡 연기 | 기본 가정 Ubuntu Core 24, Phase 3 exit 재확정 | `11-*` §11.9 |
| D5 | 라이선스 모델 | ✅ 2026-05-08 (R30-4) | **Open-core 채택** — 코어 Apache-2.0 + enterprise는 별 라이선스 (BSL/Commercial 구체 결정은 첫 enterprise customer 직전). 코드 분리는 단일 repo + build tag(R20-2). 실제 분리 시점은 첫 paying customer 직전. | `12-*`, `phase4-backlog.md` R30-4 |
| D6 | 리포지토리 호스팅 | ✅ 2026-05-08 (R30-4) | **GitHub private 유지** — release binary + report verify CLI(E30)로 P1 외부 검증 대체. 첫 enterprise customer 또는 Phase 5 진입 시 재논의 옵션. | `phase4-backlog.md` R30-4 |

**기본 방침**: 중요한 설계 결정은 사용자와 합의 후 문서에 기록(`SESSION_HANDOFF.md` 결정 로그). 합의 없이 코드로 선결정하지 않는다.

## 사용자 선호 (승계)

- **응답 언어: 한국어**
- **문체**: "-합니다" 체, 요점 우선, 긴 설명 지양
- **탐색적 질문**("어떻게 할까"): 2~3문장 추천 + 트레이드오프, 즉시 실행 금지
- **승인 패턴**: 숫자(1,2,3) 또는 A/B 선택지
- **커밋·푸시**: 명시 요청 없어도 각 Phase 완료 시 local 커밋은 OK. **remote push는 사용자 명시 요청 시에만**.
- **Co-Author 라인 커밋에 붙이지 않음**

## 작업 컨벤션

### Trunk-based (승계)

- 피처 브랜치 없음. 작업은 `main`에 직접 커밋.
- 각 Phase/Step이 **통과 가능한 단위**로 커밋.

### TDD 강제

- Red → Green → (필요 시) Refactor.
- 테스트 먼저 작성 → 실패 확인 → 구현 → 통과 확인.
- 공개 API 변경·도메인 로직 추가는 **테스트 없으면 커밋 금지**.

### 커밋 전 파이프라인 녹색

- `typecheck` ✅ / 테스트 ✅ / 린트 (0 errors) ✅
- 파이프라인 없는 초기 단계는 **"이 커밋으로 빌드·테스트가 통과한다"를 스스로 검증** (로컬 실행).

### 커밋 메시지

```
<type>(<scope>): <한글 제목>

<본문 — 한국어, 구조적 섹션 권장>
```

- type: `feat`·`fix`·`refactor`·`docs`·`test`·`chore`·`design`·`build`·`ci`
- scope: 도메인 이름 또는 `meta`·`infra`·`ui`·`api`
- 본문 섹션 예: `## 추가/변경`, `## 테스트`, `## 결정·근거`
- Co-Author 라인 붙이지 않음.

### 파일·함수 크기

- 파일 **≤ 400줄**(권장), **≤ 800줄**(최대)
- 함수 **≤ 50줄**(권장)
- 순환 복잡도 **≤ 10**

### 도메인 경계 규칙 (필수)

- 도메인 서비스는 **다른 도메인의 저장소(Repository)를 직접 호출하지 않는다**.
- 도메인 간 통신은 **이벤트 버스** 또는 **L3 Application Service** 경유.
- 위반 시 린트로 차단(린트 설정 예정).

### 불변성

- 객체 mutation 금지, 새 객체 리턴.
- 스캔 결과·Evidence·Insight·Audit은 append-only (원칙 9).

## 작업 시작 체크리스트 (매 세션)

1. `SESSION_HANDOFF.md` 읽기
2. `git status` / `git log --oneline -5`로 최근 상태 확인
3. 사용자에게 "## 진행 중 선택지"에서 번호로 선택 요청
4. 작업 착수 전 관련 설계서 섹션 정독
5. 도메인 경계·테넌시·감사 로그 영향을 1차 점검
6. TDD로 착수
7. Phase 완료 시 `SESSION_HANDOFF.md` 업데이트 + 커밋

## 하지 말 것

- 설계 문서에 없는 기능을 임의로 추가
- 에이전트 프레임워크화·자율 공격 기능 추가 (비목표, CAI 영토)
- 자체 하드웨어 제조 전제의 설계 (비목표)
- LLM 필수 경로 생성 (옵트인 원칙 위반)
- `tenant_id` 없는 테이블 추가 (멀티테넌시 원칙 위반)
- `UPDATE/DELETE` 가능한 audit 테이블 (불변성 위반)
- Remote push (명시 요청 없이)
- 브랜치 생성 (trunk-based)
- Co-Author 라인 추가

## 명령어

Go가 PATH에 없을 수 있음 — 이 경우 PowerShell에서:
`$env:Path = "C:\Program Files\Go\bin;$env:Path"`

### Make 타깃

```bash
make build        # go build -o bin/rosshield-server ./cmd/rosshield-server
make test         # go test -count=1 ./...           (Windows에서 -race 없이)
make test-race    # go test -race -count=1 ./...     (Linux/CGO 필요)
make vet          # go vet ./...
make fmt          # gofmt -l -w .
make tidy         # go mod tidy
make lint         # golangci-lint run ./...           (설치 필요)
make openapi      # (TODO: Step 0.3-β 또는 E9에서 실구현)
make ci           # vet + test + build
make clean        # rm -rf bin/
```

### 자주 쓰는 gh 명령

```bash
gh run list --repo ssabro/rosshield --limit 5
gh run watch <id> --repo ssabro/rosshield --exit-status --compact
gh repo view ssabro/rosshield --json url,visibility
```

## 참조 — 전신 프로젝트

- `D:\robot\dev\nrobotcheck\` — 전신 Electron 앱. 벤치마크 CSV·UI 컴포넌트·도메인 설계의 출처.
- 승계 Tier 분류: `docs/design/12-migration-and-non-goals.md` §12.2
- `D:\robot\dev\nrobotcheck\docs\COMMERCIALIZATION_STRATEGY.md` — 이 리포가 탄생한 배경 전략 문서

## 문서 갱신 규칙

- 설계서를 수정할 때는 **이유를 커밋 메시지에 기록**. 추후 "왜 그렇게 결정했는지" 추적 가능하게.
- 중요한 트레이드오프 결정은 `SESSION_HANDOFF.md`의 "결정 로그"에 날짜와 함께 한 줄 기록.
- 본 CLAUDE.md는 규칙 자체가 바뀔 때만 갱신.
