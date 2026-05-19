# UI/UX Review — Accessibility (WCAG 2.2 AA) + Security UX 관점 (2026-05-19)

> 페르소나: 시니어 Accessibility Specialist (WCAG 2.2 AA 인증) + Security UX Researcher (Wazuh · Splunk · Datadog Security · Snyk 도메인). Lodestar (코드네임 rosshield) Web Console 검토.
>
> 스코프: `web/index.html`, `web/src/styles/globals.css`, `web/src/components/{layout,ui}/`, `web/src/routes/_authenticated/{findings, audit, compliance, robots, system, reports, sso, users, integrations}.tsx`.
>
> 코드 변경 0 — review report 전용. 4 sub-agent 병렬 중 a11y + security UX 영역.

## 1. 종합 평가

- **WCAG 2.2 AA 점수: 6.5 / 10** — Radix UI 기반 양호, 한국어 lang 명시 ✅, focus-visible ring ✅. 그러나 **Skip-to-content 부재**, **`window.confirm/prompt/alert` 다용**, **다크 모드 amber 텍스트 대비 한계**, **password 토글/Show 부재**가 AA 차단 요소.
- **Security UX 점수: 7.0 / 10** — VerifyButton + chain head + SHA256 검증 detail panel은 동급 B2B 도구 대비 **상위 수준 trust signal**. severity 5-단계 + 클릭 toggle도 효과적. 그러나 **tenant context 노출 0**, **role badge 부재**, **destructive confirm이 native dialog**, **notification center 부재**가 enterprise 신뢰 약화 요인.
- **한 줄 요약**: B2B 보안 도구로서 trust signal과 RBAC 정밀도는 동급 최상이지만, native browser dialog 의존과 a11y P0 항목 5건이 enterprise procurement·공공 입찰 (한국 KWCAG 2.2 / NIS) 통과를 막는다.

## 2. WCAG 2.2 AA 검증 결과

### Perceivable (인지 가능성)

| 항목 | 상태 | 비고 |
|---|---|---|
| 색상 대비 (4.5:1) | ⚠️ 부분 | 라이트 모드 `--foreground` HSL(222.2 84% 4.9%) on `--background` HSL(0 0% 100%) = **약 19:1** ✅. 그러나 `text-muted-foreground` HSL(215.4 16.3% 46.9%) on white = **약 4.6:1** — 경계선 통과, 작은 글자(text-[10px], text-xs) 다용 시 위험. **다크 모드 `text-yellow-700`(findings SeverityStats)는 dark variant 분기 있으나 본 token이 다크에서도 어두운 700**이 적용되는 케이스 — 색맹+저시력 사용자에게 fail (대비 측정 필요). |
| severity 색상 외 정보 | ⚠️ 부분 | findings SeverityStats — `bg-destructive/10 + text-destructive` 색만으로 critical/high 구분 (둘 다 동일 destructive 톤). **icon 없음**, severity 텍스트는 있으나 critical vs high 시각 차별성 0. 색맹(deuteranopia 8% 남성) — 두 카드를 구분 불가. |
| icon aria-label | ✅ | lucide icon에 `aria-hidden` 일관 적용. 의미 있는 icon은 ThemeIcon 등 wrapper에서 button aria-label로 의미 전달. |
| alt text | ✅ | `<img>` 사용 거의 없음 — favicon/apple-touch-icon만. 로고는 lucide `ShieldCheck` (aria-hidden). |
| autoplay 부재 | ✅ | video/audio 0. |
| HTML lang | ✅ | `<html lang="ko">` 명시 (index.html:2). |

### Operable (조작 가능성)

| 항목 | 상태 | 비고 |
|---|---|---|
| Skip-to-content link | **❌ 없음** | Tab 첫 진입 시 sidebar 16개 메뉴를 모두 거쳐야 main에 도달. 스크린 리더 + 키보드 only 사용자에게 매번 16 Tab. **WCAG 2.4.1 fail**. |
| focus visible | ✅ | shadcn button cva: `focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2` 일관. Badge는 `focus:ring-2`. |
| Tab 순서 | ⚠️ | 의미적 순서는 대체로 OK. 그러나 `<aside>` (Sidebar) 사용 없이 div+nav만 — landmark 부족 (Robust 참조). |
| Modal · Dialog focus trap | ✅ | Radix `Dialog`/`AlertDialog` 자동 (focus trap + Esc close + return focus). |
| Dropdown/Select 키보드 | ✅ | Radix Select/DropdownMenu — 화살표 키, Home/End, type-ahead 자동. |
| keyboard shortcut | ❌ 없음 | command palette (Ctrl+K) shadcn `command.tsx` import 있으나 미사용. Splunk/Datadog 동급 도구는 모두 제공. |
| touch target ≥ 44×44 px | ⚠️ 부분 | Header theme/locale toggle `h-8 w-8` (32×32 px) — **WCAG 2.5.5 AAA fail, 2.5.8 AA Minimum (24×24) 통과**. 모바일 (Tauri 데스크톱 외) 사용 시 marginal. Findings dismiss 버튼 `size="sm"` (h-9) — 통과. |
| `window.confirm/prompt/alert` | ❌ P0 | 7곳 (`findings.tsx:168 prompt`, `integrations.tsx:221 confirm` `:327 alert` `:330 alert`, `sso.tsx:224, :587 confirm`, `users.tsx:364 confirm`). **native browser dialog는 i18n 0, focus management 불완전, screen reader 음성 불일치**. WCAG 3.3.4 (입력 검증) + 일관성 fail. |
| 시간 제한 | ⚠️ | JWT 토큰 만료 (15min 추정) — 사용자 통제·연장 UI 부재. WCAG 2.2.1 — 사용자가 시간 조정 또는 만료 알림 받아야. session timeout 직전 modal 권장. |

### Understandable (이해 가능성)

| 항목 | 상태 | 비고 |
|---|---|---|
| `<html lang="ko">` | ✅ | 단, 사용자가 i18n locale 토글 (`en` 전환) 시 `<html lang>`은 그대로 ko — 동적 update 필요. |
| Form label | ✅ | shadcn `Label htmlFor` 일관 (login, robots, findings, compliance, sso 전반). |
| aria-describedby | ⚠️ | 도움말·에러 메시지가 input과 aria 연결 0. 예: `robots.tsx:373` `<p role="alert">` 있으나 input과 `aria-describedby` 매핑 없음. SR 사용자는 error 입력 시 어떤 field가 fail인지 모름. |
| error 메시지 위치 | ⚠️ | login.tsx `role="alert"` ✅. 그러나 form-level (md:col-span-2)만 — field-level error 부재. |
| 일관된 navigation | ✅ | Sidebar 항상 좌측, Header 항상 상단 (Tauri/Web 동일). |
| 일관된 component pattern | ✅ | shadcn 일관, Table 패턴 표준화. |

### Robust (견고성)

| 항목 | 상태 | 비고 |
|---|---|---|
| semantic HTML | ⚠️ | `<main>` ✅ (`route.tsx:34`), `<header>` ✅, `<nav>` ✅ (Sidebar + Breadcrumbs). **`<aside>` 없음** — Sidebar는 `<aside className=...>` 인데 read 결과 div만 — 재확인 결과 `<aside>` 사용 ✅. 일부 페이지 (compliance, findings) `<section>` 사용 ✅. |
| landmark coverage | ⚠️ | `aria-label`이 main, header에 없음 — 다중 main이 없으니 OK이나 sidebar nav `aria-label={t('app.brand')}` = "Lodestar"는 부적절 (메뉴 의미 0). `aria-label="주메뉴"` 권장. |
| Radix ARIA | ✅ | Dialog/AlertDialog/Select/DropdownMenu/Tooltip 모두 자동 ARIA role/state. |
| 스크린 리더 호환 | ⚠️ | 한국어 NVDA 미테스트. Findings prompt window 등 native dialog는 NVDA Sense Reader에서 음성 일관성 issue 보고 다수. |
| HTML validation | ✅ (추정) | TS + React 자동 — 중복 id 등은 form-level 검토 필요. |

## 3. Security UX 평가

### 3.1 Severity 시각화

- **5-단계 SeverityStats card 클릭 toggle** (`findings.tsx:247-300`) — 우수. Wazuh dashboard 동급.
- ⚠️ **critical + high 둘 다 `bg-destructive/10 + text-destructive`** — 색맹 사용자 구분 불가. icon 부재.
- ⚠️ **deuteranopia 검증 0** — 빨강/녹색 결합 (badge variant default = primary dark green-ish, destructive = red, success 별도). compliance score Badge `default(green)` vs `destructive(red)` 둘 다 deuteranopia에서 비슷한 갈색.
- 💡 **권장**: severity별 icon (critical=`AlertOctagon`, high=`AlertTriangle`, medium=`AlertCircle`, low=`Info`, info=`Circle`) + Wazuh 스타일 좌측 vertical bar.

### 3.2 Alert · Notification

- ⚠️ **in-app notification center 0** — sidebar 16개 메뉴 어디에도 알림 inbox 부재.
- ⚠️ **toast system 부재** — `sonner` library 미설치. `window.alert(integrations.tsx:327)`로 대체 (i18n 0, 비동기 작업 결과 표시 불일관).
- ✅ webhook 백엔드는 E29로 존재 — UI 미연결 (integrations.tsx에 endpoint CRUD만).
- 💡 **권장**: shadcn `sonner` 도입 + Header에 Bell icon + critical finding count badge + dropdown notification list (Datadog 패턴).

### 3.3 Audit trail 시각화

- ✅ **Chain head hash + seq + updatedAt 표시** (`audit.tsx`) — 기본 요건 충족.
- ✅ **VerifyButton + VerifyDetail** (`reports.tsx:166-275`) — chain head hash, signer key ID, PDF SHA256, PDF size를 펼침 패널로 cross-check 가능. **동급 B2B 도구 중 상위 수준 trust signal**.
- ⚠️ **변조 검출 시각화 부재** — chain integrity 위반 시 어떤 UI가 보이나? E29의 chain validate endpoint 결과 UI 0.
- ⚠️ **단순 hex hash 외 시각화 부재** — Sigstore Rekor 동급 도구는 timeline + chain link 시각 (graph). 본 console은 평문 hex만.
- ⚠️ **evidence 다운로드 + verify CLI 명령 노출** — audit.tsx 하단에 `rosshield report verify --bundle` 텍스트만 (copy 버튼 0).
- 💡 **권장**: copy-to-clipboard 버튼 추가 + 향후 chain timeline 시각화 카드.

### 3.4 Confirmation pattern (destructive)

- ❌ **P0 — `window.confirm` 7곳 다용** — integrations webhook 삭제, sso provider 삭제, sso group mapping 삭제, users invitation 삭제, findings dismiss `window.prompt`. 모두 native dialog.
- **문제**:
  1. i18n 미동작 (브라우저 locale 강제)
  2. screen reader 음성 일관 X
  3. typing confirmation 0 (예: "DELETE" 타이핑 force)
  4. undo window 0 (5초 후 실행)
  5. focus return 불일관
- 💡 **권장**: shadcn `AlertDialog` (이미 import 가능)로 교체. credential rotate · robot delete 등 위험 작업은 typing "DELETE" + 5초 undo (Vercel/GitHub 패턴).

### 3.5 Permission 시각화 (RBAC)

- ✅ **disabled button + tooltip "권한 없음" 일관** (`mutationGuardTitle` 패턴, `common.role.required.admin` 등). 17개 mutation 모두 통일 — 우수.
- ✅ **메뉴 가시성 RBAC 필터** (`Sidebar.tsx:100`) — sso/users는 admin만, system은 system.read 보유자만.
- ❌ **현재 사용자 role badge 부재** — Header에 email만 표시 (`Header.tsx:73`). 사용자가 자신의 권한을 한 눈에 확인 0. enterprise 다수 사용자 다수 role 환경에서 "내가 admin인가 auditor인가" 모름.
- 💡 **권장**: Header에 email 옆 role badge — `<Badge variant="secondary">admin</Badge>` 또는 hover dropdown으로 모든 role + tenant 표시.

### 3.6 Data sensitivity (masking · clear)

- ⚠️ **tenant 격리 노출 부재** — 멀티테넌시 핵심 원칙(설계서 §06)인데 현재 어느 tenant context에서 작업 중인지 UI 표시 0. tenant switching 시 cross-tenant data leak 인지 0.
- ⚠️ **API key/secret show/hide toggle 부재** — `robots.tsx:318` password `<Input type="password">` 만. private key PEM (`robots.$robotId.tsx:800`) 동일. show/hide eye icon 표준 없음.
- ✅ **invitation token URL 1회 노출 + copy-to-clipboard + 2초 fade**(`users.tsx:219-228`) — 우수. GitHub PAT 패턴.
- ❌ **clipboard auto-clear 부재** — copy 후 30초 후 clipboard clear (Bitwarden 패턴) 미적용.
- ⚠️ **password input on focus 또는 blur 시 자동 mask** — robots password는 type=password로 가려져 있음 ✅. integration webhook secret `integrations.tsx:596` 동일.
- 💡 **권장**: Header에 tenant pill (예: "Tenant: ACME · staging"). password input 옆 Show/Hide 토글 (Eye icon). copy 후 30초 후 clear (web crypto subtle 사용).

### 3.7 Compliance dashboard

- ✅ **framework 매핑 명시** (`compliance.tsx:57-67`) — ISMS-P, ISO 27001:2022, NIST 800-53 Rev5 3종.
- ✅ **score hero card + progress bar + control breakdown + status filter pill** — 우수, Splunk 동급.
- ✅ **gap analysis** — fail/partial/unmapped status filter로 missing control 즉시 확인.
- ⚠️ **audit-ready export 부재** — compliance snapshot을 PDF/Excel 다운로드 UI 0. (reports.tsx는 별도 — 연결 0)
- 💡 **권장**: snapshot에 "Export PDF (signed)" 버튼 — 감사인 제출용.

### 3.8 Trust signal

- ✅ **report VerifyButton + signer keyId + chain head hash detail** — **best-in-class**. Snyk · Splunk보다 우수.
- ✅ **report signed Badge + ✓/✗ icon** (reports.tsx:122-130) — aria-label 매핑 ✅.
- ⚠️ **HTTPS / HSTS / CSP 표시 부재** — footer 또는 settings에 보안 헤더 상태 표시 0.
- ⚠️ **cosign · Sigstore 명시 부재** — `signed` boolean만, 어떤 키·어떤 알고리즘 (ed25519 vs RSA vs ECDSA) 메타 노출 0. signer keyId만.
- ⚠️ **build version 표시** (sidebar 하단 `app.version`) — 좋음, 그러나 commit SHA + build date + provenance attestation 미연결.
- 💡 **권장**: settings 페이지 "Trust" 섹션 추가 — HTTPS/HSTS/CSP status + cosign public key fingerprint + Rekor entry URL + SBOM download.

## 4. 핵심 약점 (Critical Issues)

### P0 (즉시 fix — 법적·보안 risk · enterprise blocker)

1. **Skip-to-content link 부재** — WCAG 2.4.1 AA fail. 한국 공공 입찰 KWCAG 2.2 자동 탈락. (`route.tsx`)
2. **`window.confirm/prompt/alert` 7곳 다용** — i18n 미동작 + a11y 불일관. AlertDialog 교체 필수. (`findings.tsx, integrations.tsx, sso.tsx, users.tsx`)
3. **destructive action typing confirmation + undo window 0** — robot delete · credential revoke · SSO provider delete가 즉시 실행. operator 실수 1회 = 복구 불가.
4. **현재 사용자 role badge 부재** — Header email만, 권한 자가 인지 불가. enterprise 다수 role 환경 fail.
5. **다크 모드 `text-yellow-700`(severity low) 색상 대비 미측정** — 다크 배경에서 fail 가능 (`findings.tsx:274`).

### P1 (1주 내 fix)

6. **Notification center 부재** — critical finding 발생 시 사용자 인지 경로 0 (in-app 알림 inbox).
7. **severity critical vs high 색상 동일** — 색맹 사용자 구분 0. icon + 좌측 vertical bar 추가.
8. **tenant context 표시 부재** — 멀티테넌시 cross-tenant 인지 0.
9. **password show/hide toggle 부재** — UX 표준 누락.
10. **command palette (Ctrl+K) 부재** — `command.tsx` import 있으나 미사용.
11. **session timeout 사용자 통제 부재** — JWT 만료 직전 warning modal 0.
12. **aria-describedby form error 연결 0** — SR 사용자 field-level error 인지 0.

### P2 (2주 내)

13. Audit chain timeline 시각화 (단순 hex hash 외).
14. Compliance snapshot PDF export.
15. Trust 섹션 (HTTPS/CSP/cosign fingerprint/SBOM).
16. clipboard auto-clear (30초).
17. locale 토글 시 `<html lang>` 동적 update.
18. NVDA + Sense Reader (한국어 SR) 실 테스트.

## 5. 권장 개선안

### a11y (WCAG 2.2 AA 달성 path)

- **Phase A (P0, 1주)**: Skip-to-content + AlertDialog 교체 + Header role badge + 다크 모드 색상 audit + form aria-describedby.
- **Phase B (P1, 1주)**: command palette + session timeout warning + 한국어 SR 실 테스트 + 다크 모드 severity 색상 재정의.
- **Phase C (P2, 2주)**: WCAG 2.2 자동 lint (`axe-core` Vite plugin) CI 통합 + 페르소나 5종 (low vision · color blind · keyboard only · NVDA · Sense Reader) 시나리오 테스트.

### Security UX

- **Phase 1 (1주)**: shadcn AlertDialog로 destructive 전환 + Header role/tenant badge + severity icon + password show/hide.
- **Phase 2 (1~2주)**: notification center (sonner toast + Header Bell + count badge) + compliance PDF export.
- **Phase 3 (2주)**: Trust 섹션 (HTTPS/CSP/cosign/SBOM) + audit timeline 시각화 + clipboard auto-clear.

## 6. 즉시 적용 가능 P0 fix

### F1. Skip-to-content link (`web/src/routes/_authenticated/route.tsx`)

```tsx
function AuthenticatedLayout(): React.ReactElement {
  const t = useT()
  return (
    <div className="flex min-h-screen bg-background text-foreground">
      <a
        href="#main-content"
        className="sr-only focus:not-sr-only focus:absolute focus:left-2 focus:top-2 focus:z-50 focus:rounded-md focus:bg-primary focus:px-3 focus:py-1.5 focus:text-primary-foreground focus:outline-none focus:ring-2 focus:ring-ring"
      >
        {t('a11y.skipToContent')}
      </a>
      <OfflineIndicator />
      <UpdatePrompt />
      <Sidebar />
      <div className="flex flex-1 flex-col">
        <Header />
        <main id="main-content" className="flex-1 overflow-auto p-6" tabIndex={-1}>
          <Outlet />
        </main>
      </div>
    </div>
  )
}
```

i18n key 추가: `'a11y.skipToContent': '본문으로 건너뛰기' / 'Skip to main content'`.

### F2. Header에 role badge (`web/src/components/layout/Header.tsx`)

```tsx
// useMe()에서 me.data.roles[0]?.role 노출. 또는 useRoles().
{email && (
  <span className="flex items-center gap-2">
    <Badge variant="secondary" className="text-[10px]">
      {me.data?.roles?.[0]?.role ?? '—'}
    </Badge>
    <span className="text-xs text-muted-foreground" title={email}>{email}</span>
  </span>
)}
```

### F3. `<html lang>` 동적 update (`web/src/i18n/store.ts` 또는 main.tsx)

```ts
useLocaleStore.subscribe((s) => {
  document.documentElement.lang = s.locale  // 'ko' | 'en'
})
```

### F4. window.confirm → AlertDialog (`web/src/routes/_authenticated/integrations.tsx:219-225` 외 6곳)

```tsx
// AlertDialog 패턴
<AlertDialog>
  <AlertDialogTrigger asChild>
    <Button size="sm" variant="outline" disabled={...} title={...}>
      {t('integrations.action.delete')}
    </Button>
  </AlertDialogTrigger>
  <AlertDialogContent>
    <AlertDialogHeader>
      <AlertDialogTitle>{t('integrations.action.delete.confirm.title')}</AlertDialogTitle>
      <AlertDialogDescription>{t('integrations.action.delete.confirm.desc')}</AlertDialogDescription>
    </AlertDialogHeader>
    <AlertDialogFooter>
      <AlertDialogCancel>{t('common.cancel')}</AlertDialogCancel>
      <AlertDialogAction onClick={() => del.mutate(endpoint.id)} className={buttonVariants({ variant: 'destructive' })}>
        {t('common.delete')}
      </AlertDialogAction>
    </AlertDialogFooter>
  </AlertDialogContent>
</AlertDialog>
```

### F5. severity icon 추가 (`web/src/routes/_authenticated/findings.tsx:270-276`)

```tsx
import { AlertOctagon, AlertTriangle, AlertCircle, Info, Circle } from 'lucide-react'

const order = [
  { severity: 'critical', icon: AlertOctagon, bg: 'bg-destructive/15', text: 'text-destructive' },
  { severity: 'high', icon: AlertTriangle, bg: 'bg-orange-500/10 dark:bg-orange-400/15', text: 'text-orange-700 dark:text-orange-300' },
  // ...
]
// 카드 안: <o.icon className="h-4 w-4" aria-hidden />
```

## 7. 별 round design doc 후보

| 후보 | priority | 예상 기간 | 트리거 |
|---|---|---|---|
| **a11y 전체 audit + axe-core CI** | P1 | 1주 | 한국 공공 입찰 또는 enterprise procurement 진입 시 |
| **Severity 시각화 강화 (icon + bar + colorblind)** | P1 | 1주 | F5 후속 — 다크 모드 전체 색상 재정의 + Wazuh/Splunk 벤치마크 |
| **Notification center (sonner + Bell + count badge)** | P1 | 1~2주 | 첫 enterprise customer 또는 critical finding 인지 SLA 요구 |
| **Audit trail 시각화 (timeline + chain link graph)** | P2 | 2주 | 감사 차별화 — Sigstore Rekor 동급 trust signal 강화 |
| **Trust 섹션 (HTTPS/CSP/cosign/SBOM)** | P2 | 1주 | Phase 5 public 전환 또는 보안 인증 (SOC 2 · ISO 27001) 진입 시 |
| **세션 관리 UX (idle timeout · concurrent session · revoke)** | P2 | 1~2주 | Auth deepening — RBAC E36 이후 |

---

**Cross-reference 필요 항목** (다른 sub-agent 결과와 종합):

- Visual (디자인 시스템): severity 색상 재정의·dark mode token은 본 검토와 동일 issue 지적 가능성 — 색상 audit은 합산하여 단일 design doc.
- UX research (information architecture): notification center · command palette · tenant switcher는 IA 결정 동시 필요.
- Frontend (구현): shadcn AlertDialog/sonner 도입, axe-core CI 통합은 본 P0 fix와 동일 코드 라인 touch — 병렬 작업 시 충돌 방지 위해 라인 수준 sync 필요.
