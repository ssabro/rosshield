# Customer Onboarding 보강 — Phase 6 후보 1 진입 Design

> **상태**: Phase 5 5 epic 100% 마감 + Phase 6 backlog(`phase6-backlog-design.md`) 1순위 채택 직후 진입 design doc. 본 문서는 코드 0줄 / 마이그레이션 0건 / pack 변경 0 — Phase 6 진입 합의 + 5 영역 합성 전략 + Stage 분해 + 결정 항목 권장 default까지만 마감합니다.
> **참조**:
> - `docs/design/notes/phase6-backlog-design.md`(404줄) — Phase 6 후보 5종 매트릭스 + 권장 1순위.
> - E38 결선 산출: `docs/onboarding/{README.md,quickstart.md,customer-info-template.md,demo-script.md}` (4 docs, 약 1,000줄).
> - License Enforcer: `internal/platform/license/{license.go,quota.go}` (E24 결선) + `cmd/rosshield-server/license_usage_adapter.go`.
> - Tenant create + admin seed orchestration: `cmd/rosshield-server/admin_seed.go` `executeSeedCreate()`.
> - 설계서: `docs/design/00-mission-and-positioning.md`(미션·customer 정의) · `docs/design/01-principles.md`(P1 결정성·P2 옵트인·P3 에어갭·P4 멀티테넌시·P9 불변성).
> - SESSION_HANDOFF "결정 로그" 2026-05-15 Phase 6 backlog 채택.
> **R 식별자**: R-CUSTONB-1 (본 doc 전체) — 결정 항목은 D-CUSTONB-1 ~ D-CUSTONB-7.
> **본 worktree**: `agent-a6db22cdff5875813`, main(head `fccd5cb`)에서 분기. 단독 sub-agent.

---

## 1. 상태 / 배경

### 1.1 진입 trigger

Phase 6 backlog design doc(`phase6-backlog-design.md`)의 권장 1순위 = **첫 paying customer onboarding 보강**입니다. D-PHASE6-1·D-PHASE6-2 사용자 합의 default 수용으로 본 design doc 진입.

근거(backlog §3.1·§4.1 요약):
- ROI 가장 큼(★★★★★ paying customer 직격) + 가장 작음(1~2주 보수 추정).
- 즉시 진입 가능(사용자 외부 트랙 D1·E36·E37 의존 0).
- 회귀 위험 낮음(신규 docs + thin wrapper API + 기존 endpoint 재사용).
- Phase 5 baseline(scanrun SSH·세분 RBAC·PWA·RBAC fleet·SSO) 즉시 가치 회수 — 이 baseline이 없는 상태였다면 customer 진입 자체가 불가능했음.
- 첫 paying customer 진입은 **D5 open-core 분리 시점·D6 GitHub public 전환·D8 patent 출원 후 enterprise 코드 채움 시점** 등 다른 결정의 trigger이므로 **시간 가치**가 가장 높음.

### 1.2 본 design doc 마감 목표

memory `feedback_design_doc_first.md` 일관 — 코드 진입 *전* design doc로 옵션 비교 + Stage 분해 + 결정 항목 권장 default 모두 명시. 다음 세션 즉시 Stage 1 진입 부담 0.

본 design doc 자체는:
- 코드 변경 **0**
- 마이그레이션 **0**
- pack/팩토리 변경 **0**
- API 변경 **0**

산출물: 본 markdown 1개 (~500줄) + commit 1건.

---

## 2. 현재 상태 진단 (E38 결선 + 한계 매트릭스)

### 2.1 E38 결선 산출 (head `58b5e81` 시점)

| 파일 | 줄 수 | 목적 | 마감 |
|---|---|---|---|
| `docs/onboarding/README.md` | 134 | T-7일 송부, 마일스톤·SLA 개요·연락처·intake 체크리스트 | 초판 + 시연 자료 추가 (2026-05-13) |
| `docs/onboarding/quickstart.md` | 352 | T+0 30분 첫 admin login → 첫 scan → 첫 PDF report 5단계 | 초판 (2026-05-11, v0.2.0 기준) |
| `docs/onboarding/customer-info-template.md` | 125 | T-7일 회수 yaml intake (15+ 필드) | 초판 v1 (2026-05-11) |
| `docs/onboarding/demo-script.md` | ~150~200 | T+14일 또는 sales pitch 30~45분 시연 (11 단계) | 신규 (2026-05-13) |

총 ~760~810줄 — customer "다운로드 → 첫 admin login → 첫 robot 등록 → 첫 scan → 첫 PDF report" 30분 가이드 cover.

### 2.2 한계 매트릭스

| 영역 | 현 상태 | 한계 | 임팩트 (첫 customer 시점) |
|---|---|---|---|
| **Customer intake** | yaml template (`customer-info-template.md` 125줄, 15+ 필드) | 운영자가 손으로 채워서 이메일로 보내는 모델. 자동 검증 없음(필수 필드 누락·잘못된 enum값·airgap=true인데 SMTP 설정 등 모순 검출 불가). 회수 → tenant 생성 → admin seed → license 적용은 운영자가 수동 절차로 분리 실행. | T-7일 ~ T+0일 사이 운영자 수작업 약 1~2시간. 누락 항목 발생 시 메일 핑퐁으로 T+0 지연. |
| **PoC walkthrough** | `quickstart.md` 5단계 step-by-step + `demo-script.md` 11 단계 시연 | docs only — customer가 명령·화면·예상 결과를 줄별로 따라할 일관 시퀀스 부재. 부분 실패(예: cosign 검증 실패, robot SSH unreachable, scan 진행 중단) 시 어디서 막혔는지 진단 가이드는 트러블슈팅 표(부록 B) 수준. e2e 시퀀스 자체는 자동 검증 0(매 release 시 운영자가 직접 실행해 회귀 확인). | 30분 quickstart 안에 1단계라도 막히면 T+0 kickoff 시간 초과 가능성. release 회귀로 quickstart가 깨져도 release 직후 검출 어려움. |
| **SLA 정의** | `README.md` §"Support 채널 SLA (Phase 5 best-effort)" — Slack/email/Discord/P0 best-effort | uptime % 약속 0 / MTTR 약속 0 / 보상 조항 0 / customer 응답 의무 0. P0/P1/P2 정의는 있으나 customer 합의 markdown template 부재. enterprise 계약 시점에 정식 SLA로 승격 예정으로 명시되어 있으나 template 없이 매번 from-scratch 작성 필요. | 첫 enterprise 계약 협상 시 SLA 표 from-scratch 작성 1~2일 소요. customer가 표준 SLA 패턴(uptime 99.5% / RPO 24h / MTTR 4h 등) 기대 시 답변 지연. |
| **지원 채널 docs** | `README.md` §"연락처 (TBD)" — 이메일·Slack·Discord·휴대폰·PGP 모두 TBD | TBD 5건. customer가 어디로 어떻게 보고할지(이메일 제목 형식·우선순위 라벨 사용·로그 첨부 가이드·재현 절차 양식) 표준 부재. 이메일 본문 template 0. | T+0 ~ T+7일 사이 customer 첫 사고 보고가 자유 형식 → 분류·재현 추가 핑퐁 1~2 round. |
| **License lifecycle docs** | License Enforcer 코드(`internal/platform/license/license.go` 178줄 + `quota.go` 182줄, E24 결선) + `web/src/routes/_authenticated/license.tsx` UI. Customer-facing 발급·갱신·만료·회수 절차 docs 0. | 운영자(rosshield team)는 license token 발급(Ed25519 서명) 절차를 알지만 customer-facing 가이드는 quickstart `1.2`의 `ROSSHIELD_LICENSE_TOKEN` env 한 줄만. 갱신 시점 알림·만료 임박 경고·회수 시 데이터 처리 방침 문서 0. | T-7일 license token 발급 + 1년 후 갱신 시점에 customer가 절차를 모름 → 만료 직전 쿼리 폭주. enterprise 감사 시 "라이선스 거버넌스" 항목 답변 부재. |
| **운영자 customer-specific dashboard** | `web/src/routes/_authenticated/system.tsx`(B6+B7 통합) — 시스템 health + audit chain head + storage. License usage는 별 페이지(`license.tsx`). | system 페이지는 *전 인스턴스* 시점. customer-specific(특정 tenant의 quota 사용률 추세 7/30일·license 만료 임박 카운트다운·customer health 색상 indicator) 부재 또는 부분만. T+30일 정착 점검 시 운영자가 SQL 직접 쿼리 또는 audit log 수동 검토 필요. | 운영자가 customer 진척 보고서(주간 status mail) 작성 시 매번 ~30분 SQL/log 추출. 다중 customer로 확대 시 cost 선형 증가. |
| **TBD 7장 스크린샷** | `quickstart.md`에 7개 placeholder (`screenshots/00-version-output.png` ~ `07-audit-verify-output.png`) | 실 이미지 0. customer가 docs를 읽을 때 "내 화면이 맞는지" 확인 불가. release 후 UI 변경되면 스크린샷 회귀 추적 도구도 부재. | T+0 첫 admin login 시 customer가 "정상 화면이 맞는지" 질문 → 운영자가 캡처 직접 송부. |

### 2.3 진단 종합

E38 결선은 **"customer가 따라할 수 있는 docs 1차 cover"** 수준입니다. 첫 paying customer가 **운영자 핑퐁 최소화**로 자체 가동·정착하려면 다음 5 영역 보강이 필요합니다:

1. **Customer intake API** (yaml template → 자동 검증 + provisioning)
2. **PoC walkthrough** (실행 가능한 단계별 시퀀스 + 회귀 검증)
3. **SLA template** (customer 합의 markdown + 명시 수치)
4. **지원 채널 docs** (TBD 해소 + 보고 양식)
5. **License lifecycle docs** (발급·갱신·만료·회수 customer-facing)

(스크린샷 7장은 영역 2에 부속 — walkthrough 보강 시 함께 처리.)

---

## 3. 요구 사항 분류 (5 영역)

### 3.1 영역 1 — Customer intake API + 자동 provisioning

**문제**: yaml 회수 + 운영자 수동 절차 분리.

**요구**:
- **신규 endpoint**: `POST /api/v1/customers/intake`
  - 인증: 운영자 admin 권한(`tenant:create` 권한 신규 또는 기존 admin role 사용 — D-CUSTONB-2 결정 항목).
  - 입력: `customer-info-template.md`와 동등 JSON schema (yaml→JSON 변환은 클라이언트 측). 필수 필드 검증(`organization`·`contact_admin.email`·`license.edition`·`network.airgap`).
  - 검증 규칙:
    - 필수 필드 누락 → 422 Unprocessable Entity + 누락 필드 array.
    - enum 위반(`sku` ∉ {desktop,onprem,appliance}) → 422.
    - **모순 검출** — `network.airgap=true` && `network.smtp.enabled=true` → 422 (에어갭에서 SMTP는 자체 호스팅만 허용 명시).
    - **모순 검출** — `license.edition=community` && `license.expected_quota.robots=0` → 422 (community 0=unlimited 미허용).
    - **모순 검출** — `sso.enabled=true` && (`provider`·`idp_metadata_url`·`email_domains` 중 하나라도 빈값) → 422.
- **자동 provisioning** (검증 통과 시 단일 트랜잭션):
  - tenant 생성(`tenant.Service.Create()` 재사용 — 현 `cmd/rosshield-server/admin_seed.go` `executeSeedCreate()`와 동일 경로).
  - admin user 생성(invite token 발급 — 평문 패스워드는 본 endpoint에서 받지 않음, customer가 첫 로그인 시 token으로 패스워드 설정).
  - license token 발급(server-side에서 Ed25519 서명, 결과 응답 body에 일회성 표출 — 또는 별 secure 채널 ID 반환).
  - **default admin role binding** (RBAC fleet 정밀화 후 fleet=*).
  - **audit emit**: `customer.intake_completed` event(P9 불변성 일관) + payload(tenant_id, intake_yaml_digest).
- **응답 형식** (JSON):
  ```json
  {
    "tenant_id": "01HV...",
    "admin_invite_url": "https://rosshield.acme.com/invite?token=...",
    "license_token": "<base64.payload.signature>",
    "license_token_warning": "본 토큰은 일회성입니다. 안전 채널로 customer에 전달 후 본 응답을 즉시 폐기하세요.",
    "next_steps": ["quickstart.md §1.4 첫 로그인", "..."]
  }
  ```

**세부 결정**: D-CUSTONB-2 (provisioning 범위 — minimal vs full).

### 3.2 영역 2 — PoC walkthrough (실행 가능 시퀀스)

**문제**: docs only — 부분 실패 시 진단 가이드 한정 + release 회귀 자동 검출 부재.

**요구**:
- **walkthrough script** — `scripts/onboarding/walkthrough.sh` (또는 Go 단일 바이너리 `cmd/rosshield-walkthrough/`):
  - 5 단계(quickstart §0~§4 cover) — `download/verify` · `seed admin` · `register robot (fakesshd 옵션)` · `run scan` · `verify report bundle`.
  - 각 단계: 명령 실행 → exit code 검증 → 예상 stdout substring 검증 → fail 시 진단 메시지 출력.
  - flags: `--mock` (실 SSH 없이 fakesshd로 walkthrough), `--customer-yaml <path>` (intake yaml 그대로 입력 → endpoint 호출), `--skip <stage>`, `--verbose`.
  - exit code 0=success, 1=stage fail, 2=환경 누락(rosshield-server binary missing 등).
- **e2e 회귀 검증**: CI에 walkthrough script 추가(`make walkthrough` 신규) — release tag 직전 fakesshd 모드로 자동 실행.
- **스크린샷 7장 처리**: walkthrough script `--capture` 모드 — Playwright(또는 chromedp) headless로 quickstart 7장 자동 캡처 → `docs/onboarding/screenshots/` 갱신. 매 release 직전 1회 실행.
- **기존 docs 통합**: `quickstart.md`에 "자동 walkthrough 실행" 섹션 1개 추가 — `./rosshield-walkthrough --customer-yaml acme-intake.yaml` 한 줄 + 단계별 OK/FAIL 출력 예시.

**세부 결정**: D-CUSTONB-3 (walkthrough 형식 — script vs 동영상 vs interactive).

### 3.3 영역 3 — SLA template

**문제**: best-effort 표만 있고 customer 합의 가능 markdown template 부재.

**요구**:
- **신규 파일**: `docs/onboarding/sla-template.md` — customer 합의용 markdown.
- **포함 항목** (보수 default 명시, customer 협상 가능 표시):
  | 항목 | community default | pro default | enterprise default |
  |---|---|---|---|
  | **Uptime SLO** | 명시 X (best-effort) | 99.0% (월) | 99.5% (월) |
  | **MTTR P0** | 명시 X | 영업일 8시간 | 24/7 4시간 |
  | **MTTR P1** | 명시 X | 영업일 24시간 | 영업일 8시간 |
  | **응답 시간 P0** | best-effort | 4시간 | 1시간 |
  | **응답 시간 P1** | best-effort | 영업일 8시간 | 영업일 4시간 |
  | **Audit chain 검증 보장** | 모두 (외부 SDK) | 모두 | 모두 + 분기별 chain head 합의 |
  | **데이터 손실 RPO** | best-effort | 24h (daily backup 가정) | 4h (4시간 cron 가정) |
  | **유지보수 windows** | 미정 | 월 1회 4시간 | 분기 1회 2시간 + 사전 7일 통보 |
  | **보상 조항** | 없음 | uptime 99.0% 미달 시 다음 달 10% credit | uptime 99.5% 미달 시 다음 달 20% credit |
  | **계약 변경** | EULA만 | 30일 통보 | 90일 통보 |
- **P0/P1/P2/P3 정의 표준화** — 현 `README.md` §SLA에서 발췌 → SLA template로 이전 + 강화(예시 사고 매트릭스 추가).
- **customer 응답 의무**: rosshield 진단 요청 시 customer 응답 시한(예: P0 사고 시 4시간 내 재현 정보 제공) — SLA 양방향성.

**세부 결정**: D-CUSTONB-4 (SLA 항목 정확값 — uptime % · MTTR · 응답 시간).

### 3.4 영역 4 — 지원 채널 docs (TBD 해소 + 보고 양식)

**문제**: 연락처 5건 TBD + 보고 양식 부재.

**요구**:
- **신규 파일**: `docs/onboarding/support.md` — customer-facing 지원 채널 가이드.
- **TBD 해소** (D1·외부 트랙 의존 항목은 Phase 6 본 epic 마감 *후* 커밋에서 채움; 본 docs는 placeholder 유지 + "Phase 6 마감 시점 갱신" 명시):
  - 1차 contact 이메일 — `support@<rosshield-domain-TBD>` 또는 `ssabro_k@naver.com` 임시.
  - Slack 공유 채널 URL — enterprise 한정 (Phase 6 후반).
  - GitHub issues — `https://github.com/ssabro/rosshield/issues` (D6 후 public 시).
  - 보안 신고 — `security@<rosshield-domain-TBD>` + PGP key fingerprint placeholder.
- **우선순위 라벨**:
  - **P0** — `[P0]` prefix 이메일 제목 + 휴대폰 SMS (enterprise 계약 시 별도 전달). 정의: 데이터 손상·audit 체인 깨짐·전 사용자 로그인 불가.
  - **P1** — `[P1]` prefix. 정의: 일부 기능 불가 + workaround 부재.
  - **P2** — 일반 이메일/GitHub issues. 정의: 일부 기능 불가 + workaround 존재.
  - **P3** — GitHub issues. 정의: UI 폴리시·문구.
- **보고 양식 (이메일/issue 모두 적용)**:
  ```
  ## 환경
  - rosshield 버전: vX.Y.Z
  - SKU: desktop / onprem / appliance
  - storage: sqlite / postgres
  - OS: Ubuntu 22.04 / ...

  ## 재현 절차
  1. ...
  2. ...

  ## 기대 결과
  ...

  ## 실제 결과
  ...

  ## 첨부
  - 서버 로그 마지막 200줄 (민감 정보 제거 후)
  - audit chain head verify 결과 (`rosshield-audit-verify --bundle ...`)
  - 스크린샷 (선택)
  ```
- **첫 응답 SLA** (영역 3 SLA template 발췌 cross-reference).

**세부 결정**: D-CUSTONB-5 (지원 채널 우선순위 — Slack vs email vs GitHub).

### 3.5 영역 5 — License lifecycle docs (customer-facing)

**문제**: 발급·갱신·만료·회수 customer-facing 절차 0.

**요구**:
- **신규 섹션 또는 별 파일** (D-CUSTONB-6 결정 항목): `docs/onboarding/license-lifecycle.md` 신규 또는 `README.md`에 §"License lifecycle" 통합.
- **포함**:
  - **발급** — intake API(영역 1) 통과 시점에 자동 발급. 운영자가 별도로 절차 X. 토큰은 일회성 표출.
  - **저장** — customer 측 secret manager 권장(HashiCorp Vault/1Password/AWS Secrets Manager) + 평문 파일 보관 금지 경고.
  - **적용** — `ROSSHIELD_LICENSE_TOKEN` env (quickstart §1.2 cross-reference) + `rosshield-server` 부팅 시 자동 검증.
  - **갱신** (제일 부재):
    - 만료 30일 전 — `/license` UI에 경고 배너 + 운영자에 이메일 알림(SMTP 설정 시).
    - 만료 7일 전 — `/license` UI에 critical 배너 + admin 로그인 시 모달.
    - 만료 시점 — license enforce는 grace period 7일(현 `license.go` 동작 확인 필요, 보수적 default).
    - 갱신 절차 — customer가 support@ 채널로 갱신 요청 → rosshield team이 새 token 발급 → 별 secure 채널 전달 → customer가 `ROSSHIELD_LICENSE_TOKEN` 교체 + 무중단 reload(SIGHUP 또는 재시작 — 코드 확인 필요).
  - **만료** — 하드 차단 X(원칙 §3 에어갭 1급). 단 `enterprise` 기능(SSO·MT·webhook)은 게이트 차단. 코어 기능(scan·report·audit)은 항상 동작.
  - **회수** — customer 계약 종료 시 absent 처리(community fallback) — 데이터 삭제 X(append-only P9). 운영자 개입 필요 시: license_id revoke list (Phase 6 후속 — 본 doc은 docs only).
- **License lifecycle 다이어그램** (markdown ascii 또는 mermaid):
  ```
  intake → 발급(서버 서명) → 적용(env) → 운영(만료 30/7일 알림) → 갱신(새 token) 또는 만료(community fallback) → (선택) 회수
  ```
- **License 관련 audit events 명시**:
  - `license.applied` (env에서 첫 로드)
  - `license.expired` (만료 시점)
  - `license.renewed` (새 token 적용)
  - `license.feature_gate_blocked` (enterprise feature 진입 시도 + 거부)

**세부 결정**: D-CUSTONB-6 (license docs 위치 — 별 페이지 vs E38 README 통합).

---

## 4. 합성 전략 옵션 (4종)

### 4.1 옵션 A — 5 영역 모두 일괄 (큰 PR)

**범위**: 영역 1~5 단일 epic.

**Pros**:
- customer 진입 시점 baseline 완전 cover.
- 영역 간 hand-off(intake API 응답에 license token 포함 → license-lifecycle docs cross-reference 등) 일관 보장.
- handoff/SESSION_HANDOFF 갱신 1회.

**Cons**:
- 1.5~2주 큰 PR — review 부담 + Stage 분해 시 5 commit 이상 필수.
- 영역 1(intake API) 코드 변경 표면이 가장 큼 — 회귀 위험 집중.
- 첫 customer 진입 *전*에 5 영역 모두 필요한지 가설 단계(영역 4·5 docs는 즉시 가치, 영역 1 API는 customer 진입 직전에 충분).

**회귀 위험**: **중**. intake API + provisioning이 기존 `tenant.Service.Create()` + admin seed 경로와 충돌하지 않도록 careful integration test 필요. license enforce 동작 변경(grace period 명시) 시 기존 enterprise feature gate 회귀 위험.

**추정**: 1.5~2주 (보수).

### 4.2 옵션 B — 우선순위별 순차 (1순위 intake API → 2순위 walkthrough → 3순위 docs)

**범위**:
- **Round 1 (1주)**: 영역 1 intake API + 영역 5 license lifecycle docs (가장 큰 ROI).
- **Round 2 (0.5주)**: 영역 2 walkthrough script + e2e 회귀 검증.
- **Round 3 (0.5주)**: 영역 3 SLA template + 영역 4 지원 채널 docs.

**Pros**:
- 큰 ROI 영역(intake API)을 가장 먼저 마감 → customer 진입 시점 즉시 회수.
- Round 별로 사용자 합의·커밋 분리 — 회귀 영향 격리.
- memory `feedback_design_doc_conservative.md` 일관 — 큰 가설 회피.

**Cons**:
- 사용자 round 3회 — 본 design doc 1회 + Round 별 진입 합의 3회 = 합계 4회 round 필요.
- 영역 간 cross-reference (intake API 응답 → license-lifecycle docs) 누락 위험.

**회귀 위험**: **낮음~중**. Round 분리로 영역별 회귀 격리. intake API Round에 집중.

**추정**: 2~2.5주 누적 (Round 사이 buffer 포함, 보수).

### 4.3 옵션 C — docs only 우선 (코드 0, intake API는 customer 진입 직전)

**범위**:
- **본 epic**: 영역 2(walkthrough script 한정 — Go binary 0, bash + 운영자 docs 한정) + 영역 3 SLA + 영역 4 지원 채널 + 영역 5 license lifecycle. **모두 docs only**.
- **별 epic 보류**: 영역 1 intake API는 첫 paying customer 진입 *직전* 또는 PoC partner 첫 합의 후 진입.

**Pros**:
- 가장 작음(0.5주) + 회귀 위험 0(코드 변경 0).
- 운영자 부담 감소 즉시 회수 — SLA·지원 채널·license docs는 customer 합의 단계에서 즉시 활용.
- 영역 1은 customer 진입 시점에 정확한 요구(예: SAML metadata 회수 형식)에 맞춰 진입 → 가설 회피.

**Cons**:
- intake 자동화 부재 → customer 진입 시점 운영자 수작업 1~2시간 그대로.
- walkthrough script 부재 → release 회귀 자동 검출 불가.
- 영역 1 진입 시점에 또 다른 design doc 1회 필요(round 분산).

**회귀 위험**: **0~매우 낮음** (코드 변경 0).

**추정**: 0.5~0.7주 (보수).

### 4.4 옵션 D — customer 진입 직전 minimal

**범위**:
- 영역 1 intake API 대신 운영자용 ad-hoc CLI script (`scripts/onboarding/provision.sh`) — yaml 입력 → tenant.Create + admin seed + license token 발급 한 번에.
- 영역 2~5 모두 보류.

**Pros**:
- 가장 작음(0.3주) + 코드 변경 거의 0 (script 1개).
- customer 진입 *전* 가설 단계에서 운영자 부담만 즉시 감소.

**Cons**:
- customer-facing 자동화 0 — customer가 self-service 가능 가치 부재.
- SLA·지원 채널·license docs 미해소 → enterprise 협상 시점에 from-scratch.
- 옵션 C 대비 즉시 가치 더 작음.

**회귀 위험**: **0** (script 1개, 운영자 사용).

**추정**: 0.3주.

### 4.5 옵션 비교 매트릭스

| 옵션 | 영역 cover | 추정 | 회귀 위험 | round 수 | customer self-service | 권장 |
|---|---|---|---|---|---|---|
| A 일괄 | 1~5 | 1.5~2주 | 중 | 1 | ✅ | 우선순위 2 |
| B 순차 | 1~5 (3 round) | 2~2.5주 | 낮음~중 | 3 | ✅ | **권장** |
| C docs only | 2~5 (2 한정) | 0.5~0.7주 | 0 | 1 (+ 후속) | ❌ | 우선순위 3 |
| D minimal | 부분 1 only | 0.3주 | 0 | 1 (+ 후속) | ❌ | 우선순위 4 |

---

## 5. 권장 옵션 + 근거

### 5.1 권장 = 옵션 B (우선순위별 순차)

**근거**:

1. **ROI 최대화 + 회귀 위험 분리** — intake API(가장 큰 ROI + 가장 큰 회귀 표면)를 Round 1에 집중하고 Round 2·3은 docs only로 분리. 영역별 commit 5~7개로 review 부담 분산.
2. **memory `feedback_design_doc_conservative.md` 일관** — 1.5~2주 큰 epic을 3 round로 분해해 가설 회피. intake API Round 마감 후 customer 진입 시점(또는 PoC partner 1차 합의 시점) 평가 → walkthrough/SLA Round를 customer 요구에 맞게 조정 가능.
3. **paying customer 0인 현 단계에서 자동 provisioning은 가설** — 옵션 A는 5 영역 일괄로 모두 가설. 옵션 B는 Round 1만 가설(intake API), Round 2·3은 docs로 즉시 가치 회수 (release 회귀 검증 + SLA template + 지원 채널 표준화 모두 customer 0 단계에서도 운영자 부담 즉시 감소).
4. **운영자 부담 균형** — 옵션 D는 운영자 부담만 감소(customer 가치 0), 옵션 C는 customer 가치 부분 감소. 옵션 B는 양쪽 균형.
5. **사용자 round 3회는 acceptable** — Phase 5에서 design doc → Stage 1 → Stage 2~5 패턴이 사용자 round 평균 5~7회였음. Round 3회는 정합. 각 Round 시작 시 design doc로 이미 합의된 결정 항목 재확인만.

### 5.2 옵션 B 내 Round 분해

| Round | 영역 | Stage 분해 (commit 단위) | 추정 | 사용자 합의 시점 |
|---|---|---|---|---|
| **R1** | 1 + 5 | Stage 1: API skeleton + 검증 / Stage 2: provisioning 트랜잭션 + license 발급 / Stage 3: license-lifecycle docs + UI 만료 알림 / Stage 4: 통합 테스트 + handoff | 1주 | 본 design doc 채택 직후 |
| **R2** | 2 | Stage 1: walkthrough script bash + 5단계 검증 / Stage 2: CI 통합 + e2e fakesshd / Stage 3: 스크린샷 자동 캡처(선택, D-CUSTONB-3에 따라) | 0.5주 | R1 마감 후 |
| **R3** | 3 + 4 | Stage 1: SLA template + edition별 default / Stage 2: 지원 채널 docs + 보고 양식 + TBD 일부 채움 / Stage 3: handoff + README 갱신 | 0.5주 | R2 마감 후 또는 customer 진입 직전 |

### 5.3 timeline (보수)

- T+0 (본 design doc commit): Phase 6 진입.
- T+1주: R1 마감 (intake API + license-lifecycle).
- T+1.5주: R2 마감 (walkthrough).
- T+2~2.5주: R3 마감 (SLA + 지원).
- 누적 2~2.5주, customer 진입 baseline 완전 cover.

**memory `feedback_design_doc_conservative.md`**: 잠재 시간 단축은 옵션 B 내 영역 4·5(docs only)의 시간 단축 가능성으로 회수 가능 — 단축 시 0.3~0.5주 절감.

---

## 6. 변경 사항 outline (옵션 B 채택 시)

### 6.1 R1 — intake API + license lifecycle

**마이그레이션**: 0~1건.
- 옵션: `customer_intakes` 테이블(intake yaml digest + tenant_id + applied_at) — audit 추적용, append-only(P9). 또는 audit_log 안에 `customer.intake_completed` event payload만 보존 — 마이그레이션 0. **권장 default**: audit_log 활용으로 마이그레이션 0.

**신규 파일**:
- `internal/app/intake/{service.go,service_test.go,types.go}` (~300~400줄) — intake validation + provisioning orchestration.
- `cmd/rosshield-server/intake_handler.go` (~150줄) — `POST /api/v1/customers/intake` handler.
- `cmd/rosshield-server/intake_handler_test.go` (~250줄).
- `docs/onboarding/license-lifecycle.md` (~150줄, D-CUSTONB-6 결정에 따라 별 파일 또는 README 통합).

**수정 site**:
- `cmd/rosshield-server/main.go` — 라우팅 등록 1줄.
- `web/src/routes/_authenticated/license.tsx` — 만료 30/7일 배너 추가.
- `cmd/rosshield-server/admin_seed.go` — `executeSeedCreate()`를 intake service에서 재사용 가능하게 export 또는 thin wrapper 추가.
- `internal/platform/license/quota.go` — 만료 알림 hook (옵션, 코드 변경 회피 가능).

**테스트**:
- 단위 — intake validation 검증 규칙 12+ test (모순 검출 4종 포함).
- 통합 — `POST /customers/intake` end-to-end (sqlite in-memory) 5 시나리오 (success · 필수 누락 422 · 모순 422 · 중복 tenant 409 · license token 검증).
- e2e — quickstart §1.4 첫 로그인을 intake API 응답 invite_url로 대체 시나리오.

### 6.2 R2 — walkthrough script

**마이그레이션**: 0.

**신규 파일**:
- `scripts/onboarding/walkthrough.sh` (~200줄) 또는 `cmd/rosshield-walkthrough/main.go` (~300줄) — D-CUSTONB-3 결정.
- `scripts/onboarding/walkthrough_test.sh` (~50줄, CI용).
- `docs/onboarding/walkthrough.md` (~100줄, 사용 가이드).

**수정 site**:
- `Makefile` — `walkthrough` 타겟 추가.
- `.github/workflows/release.yml` — release 직전 walkthrough 자동 실행 step 추가.
- `docs/onboarding/quickstart.md` — "자동 walkthrough" 섹션 추가.

**테스트**:
- CI — fakesshd 모드로 `make walkthrough` 통과 확인 (release branch에서만).

### 6.3 R3 — SLA + 지원 채널

**마이그레이션**: 0.

**신규 파일**:
- `docs/onboarding/sla-template.md` (~200줄).
- `docs/onboarding/support.md` (~150줄).

**수정 site**:
- `docs/onboarding/README.md` — §SLA 섹션 → SLA template로 이전 + cross-reference. §연락처 → support.md로 이전.

**테스트**: docs only — markdown lint만.

---

## 7. TDD Stage 분해 (R1 4 commit + R2 3 commit + R3 3 commit = 10 commit)

memory `feedback_parallel_agents.md` — 매 Stage 시작 시 병렬 가능성 재평가. R2·R3는 docs only라 sub-agent 병렬 가능(같은 round 안에서 worktree 2개).

### R1 Stage 분해 (4 commit)

**Stage 1**: intake validation + types
- `internal/app/intake/types.go` + `types_test.go` (validation 규칙 + 모순 검출 12+ test).
- TDD: 모순 검출 케이스 먼저 fail → validation 함수 구현.

**Stage 2**: provisioning service + handler
- `internal/app/intake/service.go` + `service_test.go` (tenant.Create + admin invite + license 발급 트랜잭션).
- `cmd/rosshield-server/intake_handler.go` + `_test.go` (HTTP layer).
- TDD: 통합 test 5 시나리오 먼저 → handler 구현.

**Stage 3**: license-lifecycle docs + UI 만료 알림
- `docs/onboarding/license-lifecycle.md` + UI 배너 (`license.tsx` 수정).
- 단위 test: 만료 임박 계산 로직 (web 또는 server side).

**Stage 4**: 통합 e2e + handoff
- 통합 시나리오: intake API → invite → 첫 로그인 → 첫 scan.
- `SESSION_HANDOFF.md` 갱신 + README 갱신.

### R2 Stage 분해 (3 commit)

**Stage 1**: walkthrough script + 5단계 검증
- `scripts/onboarding/walkthrough.sh` (또는 Go binary).
- 단위 fail/pass 로직.

**Stage 2**: CI 통합 + e2e fakesshd
- `Makefile` 타겟 + `release.yml` step.
- fakesshd 모드 검증.

**Stage 3**: 스크린샷 자동 캡처 (D-CUSTONB-3 옵션 채택 시) + handoff
- Playwright 또는 chromedp 모듈.

### R3 Stage 분해 (3 commit)

**Stage 1**: SLA template + edition별 default
- `docs/onboarding/sla-template.md`.

**Stage 2**: 지원 채널 docs + 보고 양식 + TBD 일부 채움
- `docs/onboarding/support.md`.

**Stage 3**: README 갱신 + handoff + Phase 6 마감
- `docs/onboarding/README.md` cross-reference 갱신.
- `SESSION_HANDOFF.md` Phase 6 1순위 마감.

---

## 8. 결정 항목 (D-CUSTONB-1 ~ D-CUSTONB-7)

memory `feedback_design_doc_first.md` 일관 — 모든 결정에 권장 default 명시. 다음 round 즉시 진입 부담 0.

### D-CUSTONB-1 — 본 design doc 채택 + 옵션 B 진입

- (1) **채택 + 옵션 B 진입** (권장 default).
- (2) 옵션 A 일괄 — 1.5~2주 큰 PR.
- (3) 옵션 C docs only — 0.5주, intake API 별 epic.
- (4) 옵션 D minimal — 0.3주, ad-hoc script만.
- (5) 보류 — Phase 6 후보 4(audit chain key rotation) 우선 진입.

**근거**: 옵션 B는 ROI 최대화 + 회귀 위험 분리 + paying customer 0 단계에서 가설 격리. 사용자 round 3회는 정합.

### D-CUSTONB-2 — intake API 도입 vs yaml template 유지

- (1) **intake API 도입 + 자동 provisioning (full)** — tenant + admin invite + license 발급 단일 endpoint (권장 default).
- (2) intake API 도입 + minimal provisioning — tenant + admin user만, license는 별 endpoint.
- (3) yaml template 유지 + 운영자 ad-hoc script (옵션 D 동일).
- (4) yaml template 유지 그대로 — 운영자 수동 절차.

**근거**: full provisioning은 단일 트랜잭션으로 정합성 보장 + customer 진입 시점 운영자 핑퐁 0. license 발급은 동일 트랜잭션 안에서 Ed25519 서명 호출만 — 회귀 표면 작음. minimal은 hand-off 2회로 회귀 위험 더 큼.

### D-CUSTONB-3 — walkthrough 형식

- (1) **bash script + e2e CI 통합** — 가장 작음, fakesshd 호환 (권장 default).
- (2) Go single binary `cmd/rosshield-walkthrough/` — release asset 추가, customer 측 자체 실행 가능 + cross-platform.
- (3) 동영상 — 5~10분 제작, 시각적 학습 가치 + 운영자 제작 부담 0.5~1일.
- (4) interactive walkthrough (web wizard) — `/onboarding` 신규 라우트, 추정 1주+, 본 epic 범위 초과.

**근거**: bash script는 가장 작은 진입 + CI 통합 즉시. customer 측 self-execute는 후속 epic에서 Go binary 승급 가능. 동영상은 D-CUSTONB-3 별 round로 처리 가능. interactive wizard는 본 epic 범위 초과 — backlog로.

### D-CUSTONB-4 — SLA 항목 (uptime % · MTTR · 응답 시간 정확값)

- (1) **edition별 보수 default** — community 명시 X / pro 99.0%·MTTR P0 8h·응답 P0 4h / enterprise 99.5%·MTTR P0 4h·응답 P0 1h (권장 default).
- (2) 더 보수적 — pro 99.0%·MTTR P0 영업일만 / enterprise 99.5%·MTTR P0 영업일 4h.
- (3) 더 공격적 — pro 99.5% / enterprise 99.9%.
- (4) customer별 개별 협상만, template 0.

**근거**: 옵션 1은 표준 SaaS 패턴(99.0%/99.5% tier) + Phase 5 best-effort 표와 호환 + customer 협상 baseline 명확. 옵션 3은 founder-led 운영 단계에서 비현실. 옵션 2는 첫 customer 협상 시점에 약함.

### D-CUSTONB-5 — 지원 채널 우선순위

- (1) **이메일 1차 + GitHub issues P2/P3 + Slack enterprise만** — 현 README §SLA 일관 (권장 default).
- (2) Slack 1차(공유 채널) + 이메일 fallback — Slack 응답 더 빠름.
- (3) GitHub issues 1차(public) — D6 transition 후만 가능.
- (4) Discord community 1차 — community edition 친화.

**근거**: 옵션 1은 현 best-effort 표 일관 + enterprise 한정 Slack 옵션 명시. founder-led 운영에서 다채널 동시 모니터링은 비현실. GitHub issues는 D6(public 전환) 의존이라 P2/P3만 일단.

### D-CUSTONB-6 — license customer-facing docs 위치

- (1) **별 파일 `docs/onboarding/license-lifecycle.md` 신규** + README cross-reference (권장 default).
- (2) E38 `README.md`에 §License lifecycle 섹션 통합 — 한 곳 집중.
- (3) `docs/operations/license.md` 신규 — 운영자 docs 영역 (customer-facing이 아님 — 부적합).

**근거**: license lifecycle은 발급·갱신·만료·회수 4 phase로 ~150줄 cover 필요 — README 통합 시 README 길이 200+줄 추가로 가독성 저하. 별 파일 + cross-reference가 정합.

### D-CUSTONB-7 — 첫 customer 진입 timeline

- (1) **본 epic 옵션 B 마감(2~2.5주) 후 즉시 진입 가능 → PoC partner 모집 시작** (권장 default).
- (2) 본 epic + Phase 6 후보 4(audit chain key rotation) 마감 후 — compliance baseline 강화 후.
- (3) 즉시 진입 — 본 epic R1만 마감(1주) 후 customer 진입.
- (4) 보류 — Phase 6 마감(후보 1·4·5 모두) 후 진입.

**근거**: 옵션 1은 onboarding baseline cover 후 진입으로 운영자 부담 최소화. customer 진입 자체는 사용자 외부 트랙(영업·계약 협상) 의존이므로 본 doc은 enabling만 — 옵션 2는 compliance customer 한정 기다림이 길어짐. 옵션 3은 walkthrough/SLA/지원 channel 미비 진입으로 운영자 부담 큼. 옵션 4는 enterprise/LLM 트랙 customer 명시 요구 *전*까지 무한 대기 위험.

---

## 9. 회귀 위험 / 운영 고려

### 9.1 기존 E38 docs 영향

- `README.md` — §SLA 섹션 SLA template로 이전 시 cross-reference 갱신 필요. 기존 인입 링크(다른 docs에서 `#support-채널-sla-phase-5-best-effort` anchor 참조)가 깨지지 않도록 anchor 보존 + redirect 라인 추가.
- `quickstart.md` — `1.2` license token 적용 부분이 license-lifecycle docs와 cross-reference. 기존 `ROSSHIELD_LICENSE_TOKEN` env 한 줄은 유지.
- `customer-info-template.md` — yaml schema가 intake API JSON schema와 1:1 매핑. 변경 시 양쪽 sync 강제 (CI lint 권장 — 본 epic 범위 외, 후속 carryover 가능).

### 9.2 Customer 데이터 격리

- intake API는 운영자 admin 권한 필수 — 익명 endpoint 절대 금지. customer self-signup은 본 epic 범위 외(별 epic, Phase 6+).
- 신규 tenant 생성 시 RBAC fleet 정밀화(Phase 5 마감) 일관 — admin role binding은 fleet=* (전체 fleet, 본 tenant 한정).
- audit emit `customer.intake_completed` payload에 yaml digest만(평문 yaml은 audit log에 저장 X — SMTP 패스워드·license token 등 secret 누출 차단). 평문 yaml은 운영자 측 secret manager 보관.

### 9.3 License 발급 보안

- intake API 응답에 license token 평문 포함 — TLS 필수. HTTP cleartext 응답 차단(production 강제).
- token 일회성 표출 — 응답 받은 운영자가 secure 채널로 customer 전달 후 즉시 폐기 (응답 본문에 명시 경고).
- license token 재발급 요청은 별 endpoint(권장: `/api/v1/customers/{id}/license:renew`) — 본 epic R1 범위에 포함 가능, 또는 R3 docs로 절차만 명시.
- private key (Ed25519 서명용) — 현 운영자 측 별도 보관. 본 epic은 새 키 도입 0 — 기존 `internal/platform/license/license.go` `LicensePublicKey` 임베드 + 별 발급 도구 활용.

### 9.4 운영자 부담

- 본 epic 마감 후 운영자 onboarding 부담:
  - **Before** (E38): yaml 회수 → 수동 검증 → tenant.Create CLI → admin invite → license 발급 → 별 채널 전달 (~1~2시간 + 누락 시 핑퐁).
  - **After** (옵션 B): yaml→JSON 변환 → POST `/api/v1/customers/intake` 1회 → 응답에서 invite_url + license token 추출 → secure 채널 전달 (~10~20분, 핑퐁 0).
- 운영자 학습 곡선 — 새 endpoint 1개 + walkthrough script 1개 + docs 3개. 누적 0.5~1일 학습.
- 다중 customer 운영 시 cost 선형 → constant 가까이 감소.

### 9.5 TBD 7장 스크린샷 처리

- D-CUSTONB-3 옵션 (1) bash script 채택 시 — 스크린샷 자동 캡처는 R2 Stage 3 옵션. 미채택 시 수동 캡처(운영자 0.5일) 필요. 본 epic 외 carryover 가능.
- D-CUSTONB-3 옵션 (2) Go binary 채택 시 — chromedp 통합으로 자동 캡처 가능 (R2 Stage 3에 포함, 0.5일 추가).
- 권장: 본 epic 마감 시 스크린샷 7장은 placeholder 유지 + 운영자 수동 캡처 1회 별 PR — release v0.3.0 직전.

### 9.6 사용자 외부 트랙 영향

- D1 brand·도메인 — `support@<TBD>` placeholder는 본 epic 마감 *후* 사용자 외부 트랙에서 채움. 본 epic은 placeholder 그대로.
- D6 GitHub public 전환 — 지원 채널 옵션에서 GitHub issues는 P2/P3만 명시 (D6 후 P0/P1로 확장 가능).
- E36/E37 — 본 epic 의존 0.

---

## 10. 참조

### 10.1 Phase 5 5 epic baseline

- `internal/platform/license/{license.go,quota.go}` (E24) — 라이선스 발급·검증·쿼터 게이트.
- `cmd/rosshield-server/admin_seed.go` `executeSeedCreate()` — tenant + admin user 생성 베이스(intake API 재사용).
- `internal/domain/tenant/` — tenant 도메인 (CreateRequest·CreateResult).
- `internal/platform/authz/` (RBAC 세분화 + fleet 정밀화) — 신규 tenant admin role binding 베이스.
- `web/src/routes/_authenticated/license.tsx` — 만료 알림 배너 추가 site.
- `web/src/routes/_authenticated/system.tsx` — customer-specific dashboard 보강 후속 (본 epic 외).

### 10.2 E38 결선 docs (4 files, ~810줄)

- `docs/onboarding/README.md` (134줄, head `58b5e81`).
- `docs/onboarding/quickstart.md` (352줄).
- `docs/onboarding/customer-info-template.md` (125줄).
- `docs/onboarding/demo-script.md` (~150~200줄).

### 10.3 설계서

- `docs/design/00-mission-and-positioning.md` — 미션 + customer 정의.
- `docs/design/01-principles.md` — P1 결정성 / P2 옵트인 / P3 에어갭 1급 / P4 멀티테넌시 기본값 / P9 데이터 불변성.
- `docs/design/05-api-and-auth.md` — HTTP API 설계 + 인증 모델 (intake API endpoint 등록 site).
- `docs/design/notes/phase6-backlog-design.md` — Phase 6 후보 매트릭스 (1순위 trigger).
- `docs/design/notes/d1-brand-candidates.md` — D1 brand TBD (사용자 외부 트랙).

### 10.4 SESSION_HANDOFF 결정 로그

- 2026-05-15 Phase 5 5 epic 100% 마감.
- 2026-05-15 Phase 6 backlog 채택 (D-PHASE6-1=1, D-PHASE6-2=1) — 1순위 customer onboarding.

### 10.5 memory feedback

- `feedback_design_doc_first.md` — 1일+ 임계 design doc 우선.
- `feedback_design_doc_conservative.md` — 잠재 효과/시간 보수적.
- `feedback_parallel_agents.md` — 매 Stage 시작 시 병렬 가능성 재평가 (R2·R3 docs only는 sub-agent 병렬 가능).
- `feedback_no_rest_recommendation.md` — 휴식 옵션 자동 포함 X.
- `feedback_user_tracks.md` — D1·E36·E37 등 사용자 외부 트랙 제외.
- `feedback_naming_verification.md` — 새 endpoint 이름(`/customers/intake`)은 단순 동사형으로 상표 충돌 가능성 0.
