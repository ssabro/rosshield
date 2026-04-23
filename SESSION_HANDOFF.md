# 세션 핸드오프

> **목적**: Claude Code 새 세션(재설치·다른 머신·오래 만에 재개)이 이 리포에서 바로 작업을 이어갈 수 있게 하는 지속 문서. git에 커밋되므로 로컬 `~/.claude/` 상태와 무관하게 유지된다.
>
> **Claude에게**: 이 문서를 먼저 읽고, 사용자에게 "## 진행 중 선택지" 섹션을 제시해라.

_마지막 업데이트: 2026-04-23 (D1~D6 결정 확정, 구현 미착수)_

---

## 현재 상태 한 줄

**Phase 0 — 스택·라이선스 결정 완료, 리포 부트스트랩 직전.** 설계서 13개 + D1~D6 결정 로그. 다음 단계는 Go 모듈/CI 스켈레톤/OpenAPI 1.0 초안.

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

```
fleetguard/
├── CLAUDE.md                  # Claude 지침 (세션 온보딩)
├── SESSION_HANDOFF.md         # 이 문서
├── README.md                  # 프로젝트 랜딩
├── CONTRIBUTING.md            # 기여 가이드
├── CHANGELOG.md               # 버전별 변경
├── LICENSE                    # placeholder (결정 pending)
├── .gitignore
├── .editorconfig
└── docs/
    └── design/                # 13개 설계 문서
        ├── README.md
        ├── 00-mission-and-positioning.md
        ├── ...
        └── 12-migration-and-non-goals.md
```

## 결정 필요 항목 (Phase 0 Exit 조건)

| # | 항목 | 결정 | 참조 | 상태 |
|---|---|---|---|---|
| D1 | 제품명·도메인·상표 | placeholder `FleetGuard` 유지, Phase 1 후반 최종 확정 | `docs/design/00-*` | 🟡 연기 |
| D2 | 백엔드 언어 | **Go** (백엔드) + **TypeScript** (프론트) | `docs/design/11-*` §11.2 | ✅ |
| D3 | 데스크톱 셸 | **Tauri 2.x** (Electron fallback 보류) | `docs/design/11-*` §11.8 | ✅ |
| D4 | 어플라이언스 OS | 보류, 기본 가정 Ubuntu Core 24, Phase 3 exit 재확정 | `docs/design/11-*` §11.9 | 🟡 연기 |
| D5 | 라이선스 | **Open-core** (코어 Apache-2.0 + 엔터프라이즈 closed) | `docs/design/12-*` | ✅ |
| D6 | 리포 호스팅 | **GitHub private** → Phase 1 exit 후 public 전환 | — | ✅ |
| D7 | 초기 타깃 벤치마크 | CIS Ubuntu 24.04 + ROS2 Jazzy | `docs/design/07-*` | 🟢 (기본값으로) |

## 진행 중 선택지

D1~D6 결정 확정됨(2026-04-23). 다음 세션은 **리포 부트스트랩(Step 0.2)**으로 시작:

1. **리포 부트스트랩** — `go.mod`, `.gitignore` 보강, `LICENSE`(Apache-2.0 코어), `.github/workflows/ci.yml`, `Makefile`, `depguard` 도메인 경계 린트.
2. **OpenAPI 1.0 스켈레톤** — `05-*` §5.5 기반 `openapi/openapi.yaml`. 엔벨로프·에러 구조·버저닝 규약, `oapi-codegen` 파이프라인.
3. **Phase 1 백로그 분해** — `11-*` Phase 1 체크리스트를 TDD 단위로 나누어 `docs/design/phase1-backlog.md`에 기록.
4. **벤치마크 팩 변환 도구(`pack-tools`) 설계** — `07-*` §7.13, `12-*` §12.4 기반. 기존 `nrobotcheck` CSV/JSON을 새 팩 포맷으로.
5. **스택 스파이크 (옵션)** — D2 확정 전이라면 필요했을 과정. 이미 Go 확정했으니 생략 권장.

**권장 순서**: 1 → 2 → 3.

## 결정 로그

날짜 내림차순.

- **2026-04-23 · D6 결정됨**: 리포 호스팅 `GitHub private`. Phase 1 exit 시점에 public 전환(open-core 코어 공개 연동).
- **2026-04-23 · D5 결정됨**: 라이선스 `Open-core`. 코어(감사 엔진·CLI·팩 포맷)는 Apache-2.0 공개, 엔터프라이즈 계층(SSO·멀티테넌트 관리·클라우드 대시보드)은 closed. 근거: 감사 도구 신뢰성 확보 + 팩 포맷의 외부 검증 가능성(P1) 유지.
- **2026-04-23 · D4 연기됨**: 어플라이언스 OS는 Phase 3 exit 시점에 최종 확정. 그때까지 기본 가정은 **Ubuntu Core 24**.
- **2026-04-23 · D3 결정됨**: 데스크톱 셸 `Tauri 2.x`. Go 백엔드는 자식 프로세스로, Tauri는 얇은 WebView 껍질. Electron은 긴급 출시 fallback으로만 보류.
- **2026-04-23 · D2 결정됨**: 백엔드 `Go`, 프론트 `TypeScript`. 근거: 단일 정적 바이너리, `crypto/ssh`·`ed25519` 성숙, 3종 배포 natural fit, P3/P7 원칙 부합. `nrobotcheck`의 Electron·native 모듈 운영 부담 회피.
- **2026-04-23 · D1 연기됨**: 제품명 확정은 Phase 1 후반으로 연기. 코드 네임스페이스는 `fleetguard`/`fg` 사용. 법무·도메인·상표 조사는 유료 고객 접촉 직전 병행.
- **2026-04-23**: 리포를 `D:\robot\dev\fleetguard`로 신설. 전신 `nrobotcheck`에서 설계·개념 승계, 코드는 새로 작성.
- **2026-04-23**: 13개 설계서 초안 완성(Draft v0.1).
- **2026-04-23**: 상업화 방향 — 어플라이언스 단독 진화 X, 헤드리스 코어 + 배포 3종(데스크톱·온프렘·어플라이언스 이미지). 근거는 전신 리포 `docs/COMMERCIALIZATION_STRATEGY.md`.
- **2026-04-23**: CAI(aliasrobotics)와의 포지션 분리 — 자율 공격 에이전트 프레임워크는 비목표.

## 작업 재개 절차

1. 이 문서 읽기
2. `git log --oneline -5`로 최근 상태 확인
3. 사용자에게 "재개하시겠습니까? 선택지 제시" + 위 선택지 6개 나열
4. 사용자가 번호 선택
5. 관련 설계서 섹션 정독
6. 도메인 경계·테넌시·감사 영향을 1차 점검
7. 착수 (TDD)
8. Phase/Step 완료 시 이 문서 **"결정 로그"** + **"현재 상태 한 줄"** + (필요 시) **"진행 중 선택지"** 갱신

## 아직 없는 것 (새 세션이 당황할 만한 포인트)

- 코드 0줄 — `src/` 같은 폴더 자체가 없다.
- 빌드 시스템 없음 — `package.json`·`go.mod`·`Makefile` 없음.
- CI 없음 — `.github/workflows/` 없음.
- 테스트 없음 — 구현이 없으니 당연.
- LICENSE는 placeholder — 결정 필요.

이것들은 **Phase 0~1에서 순차적으로 등장**할 예정이다. 그 전까지는 **설계서와 결정 미팅**이 주된 산출물이다.

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
