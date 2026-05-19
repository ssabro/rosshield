# D-UI-1 Stage 5b — color-contrast 실측 자동화 보고

> **상태**: spec + 운영 doc 본 round 완료. 실 실행(10 케이스)은 후속 round 또는 CI에서 수행.
> **컨텍스트**: v0.6.4 직후 sub-agent 3 병렬 (Audit rotation 본체 · drill-down spacing · 본 트랙).
> **참조**:
> - `ui-stage5-polish-report.md` §1.3 / §4 carryover C5-1
> - WCAG 2.2 AA 1.4.3 Contrast (Minimum) — 일반 4.5:1, 대형 3:1
> - 기존 e2e 인프라: `web/playwright/` (Phase 3 C4 회수)

---

## 1. 배경 — 왜 jsdom으로는 부족한가

Stage 5에서 vitest-axe + jsdom 기반 5 페이지 a11y scan을 완료했으나, axe-core 공식
입장 "color contrast rule requires elements be rendered in the browser"에 따라
`color-contrast` rule을 **비활성**으로 유지했다 (`web/src/test/axe.ts` line 22).

→ Stage 1에서 정의한 severity 5-color HSL token, `--primary`, `--muted-foreground`,
`--destructive` 등의 실 contrast는 디자인 타임에 수동 검증되었을 뿐이고, 컴포넌트
조합(특히 `text-muted-foreground` on `bg-card` 등 token cross-application) 시점의
실측은 부재였다.

본 round는 **실 chromium** 브라우저에서 WCAG AA 4.5:1을 자동 측정하는 e2e spec을
도입하여 이 갭을 메운다.

---

## 2. 추가 (본 round)

### 2.1 의존성

| 패키지 | 버전 | 역할 |
|---|---|---|
| `@axe-core/playwright` | ^4.11.3 | Playwright fixture에 axe scan 주입 (devDep) |

`pnpm-lock.yaml` 갱신 1건. `axe-core` 자체는 기존 transitive로 이미 4.11.4 존재.

### 2.2 신규 파일

| 파일 | 줄 수 | 역할 |
|---|---|---|
| `web/playwright/tests/color-contrast.spec.ts` | 99 | 5 페이지 × light/dark = 10 케이스 spec |
| `docs/design/notes/ui-stage5b-color-contrast-report.md` | (본 문서) | 본 round 결과 |

### 2.3 수정 파일

| 파일 | 변경 | 근거 |
|---|---|---|
| `web/playwright/helpers.ts` | `applyThemeMode(page, mode)` helper 추가 (+24줄) | spec 안에서 reload 없이 light/dark 토글 |
| `web/playwright/README.md` | "color-contrast 실측" 절 + tests 목록 갱신 | 운영자 onboarding |
| `web/package.json` | `@axe-core/playwright` devDep 1건 | 위 2.1 |

---

## 3. 설계 결정

### 3.1 light/dark 토글 방식 — `colorScheme` project 분리 vs DOM 클래스

| 방안 | 장점 | 단점 | 채택 |
|---|---|---|---|
| Playwright `projects: [colorScheme: 'light'/'dark']` | 표준 패턴 | rosshield는 `prefers-color-scheme`이 아니라 zustand store 기반 → 클래스 토글이 따로 필요 | ❌ |
| spec 내부 helper로 `html.dark` 토글 + localStorage 세팅 | reload 없이 동일 페이지에서 양 모드 측정, 기존 1 project 유지 | helper 한 줄 추가 | ✅ |

→ **DOM 클래스 토글 방식 채택**. CI 비용도 더 적음 (project 2개로 분기하면 globalSetup
2번 수행, 단일 sqlite dataDir 충돌).

### 3.2 rule scope — `withRules(['color-contrast'])` 단독

color-contrast만 평가하고 다른 rule은 vitest-axe 쪽에서 이미 cover하므로 중복 제거.
spec 실패 시 violation의 원인이 명확 (다른 rule이 섞여있으면 디버깅 비용 증가).

### 3.3 페이지 ready 마커

각 페이지 entry text(`/개요/`, `/스캔|Scans/` 등)를 `expect(...).toBeVisible()`로
대기. networkidle만 의존하면 SSE/poll 끝나지 않는 케이스에서 hang하거나, skeleton
상태로 scan하면 contrast가 token이 아닌 skeleton 색만 검출되어 false PASS 위험.

### 3.4 CI 통합은 별 epic

본 round는 spec + 운영 doc만. `.github/workflows/`에 별도 job 추가는 다음 이유로 분리:
- 기존 `ci.yml`에 `e2e` job이 이미 있고, `color-contrast.spec.ts`는 자동으로 포함됨.
- CI workflow 수정은 `gh workflow` admin 권한이 필요할 수 있어 사용자 확인 필요.
- 본 round 작업 범위(sub-agent 3 병렬) 보호.

→ carryover: 사용자가 CI 실행 결과 확인 후 임계치(violation > 0 시 fail 또는 warn)
정책 결정.

---

## 4. 테스트 (본 round 검증)

| 항목 | 결과 |
|---|---|
| `pnpm exec tsc --noEmit -p playwright/tsconfig.json` | PASS (0 errors) |
| `pnpm exec playwright test --list color-contrast.spec.ts` | 10 tests discovered |
| 실 실행 (`playwright test color-contrast.spec.ts`) | **본 round 미실행** — Go server + web build 필요, 다른 sub-agent와 환경 공유 risk |

본 작업 회귀 0 (기존 spec/소스 변경 없음, helper 1개 추가만).

---

## 5. 실측 결과 — 후속

본 round는 spec 작성 + 운영 doc까지 완료. 실 10 케이스 PASS/violation 매트릭스는
다음 중 한 시점에 확정한다:

1. 사용자가 로컬에서 `pnpm exec playwright test color-contrast.spec.ts` 실행.
2. 다음 CI run에서 e2e job이 자동 수행 (workflow가 spec 디렉터리 전체 실행 가정).
3. 본 sub-agent 그룹 통합 빌드 시 별 round로 분리.

**예상 시나리오**:
- **violation 0** → Stage 1 token + 디자인 일관성 입증 완료. carryover C5-1 closed.
- **violation 1~2건** → 본 round에서 token CSS variable 1줄 수정으로 fix 가능.
- **violation 3건 이상** → 별 round로 분리 (token refactor + dark mode 재검토).

---

## 6. carryover (다음 round)

| ID | 항목 | 사유 | 권장 round |
|---|---|---|---|
| C5b-1 | 10 케이스 실 실행 + 결과 매트릭스 확정 | 본 round는 spec까지만 (다른 sub-agent 환경 공유 risk) | 통합 빌드 직후 또는 다음 e2e 실행 |
| C5b-2 | CI workflow 임계치 정책 | violation > 0 시 hard fail vs warn-only — 운영 정책 결정 필요 | E40 perf/CI 트랙 |
| C5b-3 | drill-down 페이지 추가 (`fleets.$id`, `robots.$id`, `packs.$pack.checks.$check`) | 본 round는 top-level 5 페이지만. drill-down은 다른 sub-agent 영역 + URL param seed 필요 | drill-down spacing 후 |
| C5b-4 | Login/Invitation accept 페이지 | 인증 전 페이지는 별도 helper (no loginAsAdmin) | 별 round |
| C5b-5 | Settings/Users/System 페이지 | admin role 한정 + form-heavy contrast 추가 | 별 round |

---

## 7. 결론

- color-contrast 실측 자동화 인프라 구축 (Playwright + @axe-core/playwright).
- 5 페이지 × light/dark = 10 케이스 spec 정의 (`web/playwright/tests/color-contrast.spec.ts`).
- 다크 모드 토글 helper 추가 (`applyThemeMode`).
- 운영 doc 갱신 (`web/playwright/README.md` "color-contrast 실측" 절).
- 실 실행 + 결과 매트릭스는 carryover C5b-1로 분리 (sub-agent 그룹 통합 빌드 직후).
- 본 round 회귀 0.
