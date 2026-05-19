# UI/UX Review — UX Research / Information Architecture 관점 (2026-05-19)

> Reviewer: 시니어 UX Researcher / IA (B2B enterprise security · DevOps), NN/g · JTBD framework.
> Scope: `web/src/components/layout/{Sidebar,Header,Breadcrumbs,PageHeader,EmptyState}.tsx` + `web/src/routes/_authenticated/*.tsx` 20 페이지 + `web/src/i18n/dict.ts` (한·영).
> 본 문서는 **review 산출물**이며 코드 변경 0. 자매 노트: Visual / Interaction / Accessibility 별도 진행.

---

## 1. 종합 평가

**점수: 5.7 / 10** — "기능은 완비, IA·온보딩 미성숙. Phase 1 SaaS B2B 표준 대비 한 수 아래."

한 줄 요약: **20 페이지의 도메인 모델이 평탄(flat)하게 Sidebar에 노출되어 신규 사용자가 mental model을 만들 수 없으며, 핵심 일상 task (fleet health · evidence 다운로드)가 3~6 click + 페이지 간 점프로 분산되어 있습니다.** Linear · Datadog · GitHub 표준의 task grouping · global search · empty state CTA · 첫 진입 onboarding이 모두 부재합니다.

강점:
- i18n 인프라(ko/en 동기 강제 — `dict.test.ts`), permission-aware menu, Tauri-friendly route 구조, EmptyState 컴포넌트 존재(10 페이지 사용).
- Breadcrumbs가 drill-down 4 페이지(`robots/:id`, `fleets/:id`, `packs/:key`, `packs/:key/checks/:id`)에서 일관 사용.
- PageHeader + actions slot 패턴 — primary action 위치가 일관.

약점:
- Sidebar 15 항목 평탄 — 카테고리 0, scroll 가능성.
- Overview 카드 4개가 "robots · findings · compliance · score" — fleet health · 진행 중 scan · audit chain 상태 등 **운영 첫 화면의 actionable 요약 부재**.
- Global search · command palette 0건 (`command.tsx` 정의는 있으나 wiring 0).
- 도메인 용어(Pack · Check · Insight · Finding · Evidence · Snapshot · Profile) 학습 곡선에 대한 UI 내 가이드 없음.

---

## 2. Target user persona × Top tasks

| Persona | 일상 주기 | Top 3 task (frequency × value) | 현재 진입 시작점 |
|---|---|---|---|
| **P1. Security 운영자 (SecOps)** | 매일·근무 시간 내 다회 | (a) fleet 헬스·신규 alert 확인 (b) finding triage / dismiss (c) scan trigger | `/overview` → 정보 부족 → `/findings` + `/system` + `/scans` 점프 |
| **P2. 감사인 (Auditor)** | 분기·반기 | (a) compliance snapshot 생성·다운로드 (b) signed report 다운 (c) audit chain head 확인·verify | `/compliance` → snapshot → `/reports` → 다운로드 disabled |
| **P3. Robot fleet 운영자** | 주 1~3회 | (a) robot 등록·태그·credential rotate (b) 새 fleet 정의·policy 변경 (c) scan 결과 robot별 확인 | `/robots` 또는 `/fleets` |
| **P4. C-level / Compliance officer** | 주 1·월 1 | (a) score 트렌드 (b) 미해결 critical 수 (c) license · 사용자 수 | `/overview` → 단일 score 카드 → 트렌드 없음 |

**핵심 통찰**: P1(SecOps)이 가장 frequency 높은데 `/overview` 4 카드 중 3개가 단순 count(숫자만), 1개가 score(트렌드 없음). **운영 첫 5초 안에 "오늘 무엇을 해야 하는가"를 답해주지 못함.**

---

## 3. Job-to-be-Done flow 측정 (현재 click count vs 목표)

| # | Job | 현재 step | click 수 | 목표 | 병목 |
|---|---|---|---|---|---|
| J1 | **매일 fleet 헬스 확인** (P1) | `/overview` → 카드 정보 부족 → `/system`(health) + `/system`(scans-severity) + `/findings`(critical 필터) | **3~4 page** | 1 page (overview에 통합) | `/overview` actionable 요약 부재 |
| J2 | **신규 robot 등록 + 첫 scan** (P3) | `/robots` → 등록 폼 → fleetId 입력(`/fleets` 사전 생성 필요) → `/scans` → fleet+pack 선택 → 진행 모니터링 → `/robots/:id` 결과 | **6~7 step** | 3 step (wizard) | fleetId 수동 입력, scan→결과 cross-page 점프 |
| J3 | **분기 audit evidence 다운로드** (P2) | `/compliance` → snapshot 생성 → `/reports` → **다운로드 버튼 disabled (Phase 2 미구현)** → CLI fallback | **4 step + dead-end** | 2 step | reports 다운로드 endpoint 미정의, audit verify CLI 분리 |
| J4 | **Compliance gap 식별 → fix** (P2/P1) | `/compliance` → snapshot → control 상태 확인 → 위반 control이 어느 robot/check인지 → `/packs/:key` 또는 `/findings`로 수동 점프 | **5~6 step** | 3 step | compliance ↔ findings ↔ packs 간 cross-link 부재 |
| J5 | **Robot SSH credential rotate** (P3) | `/robots` → 행 클릭 → `/robots/:id` → RotateCredentialCard | **3 step** | 3 step ✅ | 만료 임박 알림 부재 (push 부재) |

**평균 4.2 click** — Linear · Datadog 동급 task가 2~3 click에 끝나는 것 대비 1.5~2배. **J3(audit evidence download)는 dead-end** — Phase 1 출시 직전 P0.

---

## 4. Information Architecture 평가

### 4.1 현재 구조: 15 항목 flat Sidebar
```
개요 · Fleet 관리 · 로봇 · 스캔 · Findings · Compliance · Advisor ·
리포트 · 감사 · 통합 · SSO · 사용자 · 라이선스 · 시스템 · 설정
```

문제:
- **카테고리 0** — operations(daily) vs compliance(quarterly) vs admin(rare) 시간축이 섞임.
- **알파벳도 frequency도 아닌 임의 순서** — 코드상 정의 순(`Sidebar.tsx` L70-89)이 노출 순서. "감사 다음 통합"은 한국어/영어 어느 mental model에도 없음.
- **`/packs` index 부재** (`packs.$packKey.tsx`만 존재) — pack list는 `/system` 안 카드. 신규 사용자가 "어떤 검사가 있나"를 묻기 위한 첫 발견 경로 없음.
- **`/sso`와 `/integrations` 분리** — 둘 다 enterprise admin 작업, 사용자는 "외부 연동" 한 곳으로 기대.
- **`/license`와 `/settings` 분리** — license는 settings 한 카드(LicenseCard 이미 존재 — `settings.tsx` L66)에 중복 노출.

### 4.2 권장 grouping (3 + 1)

```
운영 (Operations)            ← P1 SecOps, 매일
  · 개요 (Overview)          ← actionable summary
  · 스캔 (Scans)
  · Findings
  · 로봇 (Robots)
  · Fleet

컴플라이언스 (Compliance)    ← P2 Auditor, 분기·반기
  · Compliance
  · 리포트 (Reports)
  · 감사 (Audit chain)
  · 벤치마크 팩 (Packs)      ← NEW index 신규 권장

지능화 (Intelligence) — opt-in
  · Advisor

관리 (Admin)                 ← P3/P4, 가끔
  · 외부 연동 (Integrations + SSO 통합)
  · 사용자 (Users)
  · 시스템 (System + License 통합)
  · 설정 (Settings)
```

총 4 그룹 / 12 항목. 운영자가 첫 출근 시 "운영" 그룹만 보면 일상 끝.

---

## 5. 핵심 약점 (Critical Issues)

### P0 — Phase 1 출시 전 반드시

- **[P0-1] J3 audit evidence download dead-end** — `/reports` 다운로드 버튼 disabled (`reports.tsx` L35 주석 "Phase 2 미정의"). 감사인 첫 task가 실패. **다운로드 endpoint를 Phase 1 안으로 끌어오거나, UI에서 "CLI로 다운로드: rosshield report download <id>" CTA 카드 표시.**
- **[P0-2] `/overview` 정보 빈약** — count 4개 + score 1개. SecOps 일상에 fleet health(`/system` healthz) · 진행 중 scan 수 · 미해결 critical insight 수 · 최근 24h drift 수 등 actionable 데이터 0. **운영 첫 화면이 dashboard 역할을 못 함.**
- **[P0-3] EmptyState CTA 불일치** — `/robots`(L120) · `/findings` · `/compliance`는 EmptyState 사용. `/system` · `/audit` · `/integrations` · `/license` · `/advisor`는 `<p>(데이터 없음)</p>` 텍스트만. **신규 tenant onboarding 첫 진입에서 "다음 무엇" 안내 0.**

### P1 — Phase 2 초

- **[P1-1] Sidebar 15 항목 flat — 카테고리 0** (위 §4 참조).
- **[P1-2] Global search · command palette 0** — `command.tsx`(cmdk) 정의만 있고 wiring 없음. 20 페이지 + N 개 robot · N 개 check 환경에서 검색 없이는 점프 불가.
- **[P1-3] cross-page navigation 단절** — finding → 해당 robot · scan → 해당 finding · compliance control → 해당 check link 0. **Breadcrumbs는 위로만, drill-across는 부재.**
- **[P1-4] 용어 학습 곡선** — Pack/Check/Insight/Finding/Evidence/Snapshot/Profile 7 개념. UI 내 inline help · glossary 0. 한국어 vs 영어 혼용("Compliance" "Findings" "Advisor"는 영어 유지, "스캔" "리포트" "감사"는 한글 — i18n dict `nav.findings: 'Findings'`).

### P2 — Phase 3+

- **[P2-1] 진입 시 첫 사용자 onboarding tour 0** — `docs/onboarding/quickstart.md` 텍스트만, UI 내 hint 0. 첫 5분 안에 fleet 생성 → robot 등록 → scan trigger 한 사이클 가이드 부재.
- **[P2-2] keyboard shortcut 0** — `g r`(go robots) 등 power user 가속 0.
- **[P2-3] credential 만료 임박 push 알람 0** — J5에서 발견. proactive notification 부재.
- **[P2-4] Discoverability** — PWA install · Advisor opt-in · webhook 신기능 발견 안내 0.

---

## 6. 권장 개선안

### 6.1 Navigation
- Sidebar **4 그룹 + 12 항목**으로 재구성 (§4.2).
- 그룹 헤더는 `text-[10px] uppercase muted-foreground` (예: "운영 / Operations") — Linear · Notion 패턴.
- 활성 그룹은 expand, 비활성은 collapse 가능 (option).
- frequency 기반 ordering — 운영 그룹은 매일, 관리 그룹은 월 1회.

### 6.2 IA grouping
- `/packs` index route 신설 (P1) — `/system` 카드는 요약만, drill-down은 `/packs`.
- `/sso` → `/integrations/sso` (또는 `/integrations` 단일 페이지 안 tab).
- `/license` → `/system` 안 통합 (또는 `/settings` LicenseCard 한 군데로 통일).

### 6.3 Discoverability
- **Command palette** (Ctrl+K / Cmd+K) — `cmdk` 이미 설치. 라우트 점프 + recent robot/fleet 검색 + 다국어 항목 검색. Phase 2 1주 작업.
- **keyboard shortcut** — `g o`(overview), `g r`(robots), `g s`(scans), `?`(help) — mousetrap 또는 hotkeys-js.
- **Notification badge** — Sidebar Findings 항목 옆 critical 수 배지 (`<Badge variant="destructive">3</Badge>`).
- **first-run banner** — 첫 tenant tour 4 step (welcome → fleet → robot → scan).

### 6.4 Empty / Loading / Error state 통일
- 모든 페이지의 빈/에러 상태를 `EmptyState` 컴포넌트로 통일 (현재 10/20 페이지만 사용).
- `/system` 각 카드 0건 → "처음 사용하시나요? 빠른 시작 가이드 →" CTA.
- error state에 항상 retry button + 지원 채널 link (`docs/onboarding/support-channels.md`).

### 6.5 Mental model · terminology
- **용어 사전 페이지** — `/help/glossary` 또는 `?` shortcut → modal. Pack/Check/Insight/Finding/Evidence/Snapshot/Profile 7 항목 + 예시.
- **한국어 vs 영어 일관 정책** — 도메인 핵심어(Compliance · Findings · Advisor)는 영어 유지 + 첫 등장 시 `(컴플라이언스)` 병기. Sidebar에서 "Compliance / 컴플라이언스 검토" 형태로 부제 추가 옵션.
- **i18n dict**에 `glossary.<term>.short` + `glossary.<term>.long` key 도입 — Tooltip이 inline 학습 채널.

### 6.6 Overview 재설계 (P0)
4 카드 → **6~8 카드 + 우선순위 섹션**:
1. 오늘의 alert (critical insight 수, 24h drift, 실패한 scan)
2. Fleet 헬스 요약 (healthy / degraded / down — `/system` healthz 데이터 재사용)
3. 진행 중 scan (live count + 최근 완료)
4. 최근 7일 score 트렌드 sparkline
5. 미해결 finding by severity (stacked bar)
6. 다음 audit 예정일 (manual schedule 또는 정책 기반)
+ "Quick actions" 카드: 새 robot 등록 · 새 scan · 리포트 다운로드 · compliance snapshot 생성 (4 CTA)

---

## 7. 즉시 적용 가능 P0 fix (3~5건, 코드 sketch)

1. **`reports.tsx` dead-end fix** — 다운로드 disabled 버튼 → CLI 안내 카드 (1 카드, `EmptyState` action slot 사용).
   - 파일: `web/src/routes/_authenticated/reports.tsx`
   - 변경: 다운로드 셀 tooltip → 별 카드("CLI 다운로드: `rosshield report download <id>`") 추가, link to `/audit` 검증 페이지.
2. **Sidebar 그룹 헤더 추가 (코드 최소)** — `Sidebar.tsx` items 배열에 `group` 필드 추가 + 그룹 변경 시 `<div className="px-3 pt-3 pb-1 text-[10px] uppercase">{group}</div>` 렌더. 항목 순서·라벨 보존, 시각 분류만.
   - 파일: `web/src/components/layout/Sidebar.tsx` L50-89.
3. **EmptyState 누락 페이지 보강** — `/system`(PacksCard·BackupsCard 빈 분기), `/audit`(seq=0 분기), `/integrations`(endpoint 0건), `/advisor`(503 분기), `/license`(미발급 분기) 5곳에 `EmptyState` 적용. action slot에 quickstart link.
4. **Overview Quick actions 카드 추가** — 4 카드 아래 grid에 "빠른 작업" 카드 1개 (`Button asChild` 4개로 robot 등록 · scan · compliance · reports 점프). 5분 작업.
5. **Sidebar Findings 항목 critical badge** — `useInsights({ severity: 'critical' })` count → `<Badge variant="destructive" className="ml-auto">{n}</Badge>`. `Sidebar.tsx` L126-141 Link 안 추가. SecOps 일상 첫 신호.

---

## 8. 별 round design doc 후보

| 후보 | 우선순위 | 추정 | 비고 |
|---|---|---|---|
| **IA 재구성 (Sidebar 4 그룹화 + `/packs` index + `/sso` 통합 + `/license` 통합)** | P1 | 1주 (design 2일 + impl 3일) | dict.ts 그룹 key 추가, route 이동, 회귀 테스트 |
| **Onboarding flow 신규 (first-run tour + Quick actions + glossary modal)** | P1 | 1.5~2주 | `react-joyride` 또는 자체 popover. quickstart.md 자산 재사용 |
| **Global search · command palette (Ctrl+K)** | P2 | 2~3주 | cmdk 이미 설치. 라우트 + 동적 robot/fleet/check 검색 + recent. backend search endpoint 필요 시 +1주 |
| **Notification system (toast + sidebar badge + push)** | P2 | 1주 | finding · scan complete · credential 만료 임박 |
| **Overview dashboard 재설계 (6~8 카드 + sparkline + Quick actions)** | P0 (위 §7-4와 분리 시 P1) | 1주 | recharts 또는 자체 SVG. healthz 재사용 |

권장 순서: **Overview 재설계 → IA 재구성 → Onboarding tour → Global search → Notification**. 첫 2개는 Phase 2 안으로 끌어오는 것을 강력 권고.

---

## 부록 — 측정 데이터 출처

- Sidebar item 수: `Sidebar.tsx` L50-89 — 15개 (permission filter 적용 전).
- 라우트 파일: `routes/_authenticated/*.tsx` 20개 (test 제외) — total 11,807줄.
- Breadcrumbs 사용: 4 페이지 (`robots.$robotId` · `fleets.$fleetId` · `packs.$packKey` · `packs.$packKey.checks.$checkId`).
- EmptyState 사용: 10 페이지 (위 §5 P0-3 누락 5 페이지와 대응).
- i18n dict 줄 수: 1,751 (ko + en + meta).
- `command.tsx`(cmdk) wiring: 0 (정의만).
- onboarding/walkthrough/firstRun UI: 0 (docs/onboarding/ 텍스트만).
