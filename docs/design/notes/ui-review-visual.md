# UI/UX Review — Visual Design 관점 (2026-05-19)

> 페르소나: 시니어 Visual/Brand Designer (B2B SaaS, Linear · Vercel · Datadog 디자인 시스템 경험).
> 대상: `web/` (React 19 + Tailwind v4 + shadcn/ui + Radix), v0.5.2 baseline, 코어 build only.
> 범위: Visual Design 영역만 (UX flow · interaction · a11y는 별 sub-agent).

---

## 1. 종합 평가

**점수: 5.5 / 10**

한 줄 요약 — **shadcn/ui default 위에 토큰 체계는 잘 깔려 있으나(다크 모드까지 결선됨), Lodestar 브랜드 적용·severity/status 컬러 체계·typography·data viz 가 모두 "default 그대로"라서 commercial B2B SaaS의 trust signal이 부족합니다.** 보안 감사 도구의 정체성이 시각에 드러나지 않고, 한국어 default임에도 system font fallback만 사용해 한·영 혼용 시 visual rhythm이 흐트러집니다. 토큰 backbone이 견고해 P0 작업은 토큰·매핑 표준화로 대부분 수 시간 내 가능합니다.

---

## 2. 강점 (Strengths)

1. **Tailwind v4 `@theme` + CSS 변수 분리가 깔끔** — `web/src/styles/globals.css`가 HSL 변수와 토큰을 명확히 분리(`--color-primary: hsl(var(--primary))`). 다크 모드(`.dark` selector + `useThemeStore` system/light/dark 3-mode)도 이미 결선됨. shadcn/ui 디자인 시스템의 통상적 도입 베스트프랙티스를 따름.
2. **layout primitive 일관성** — `PageHeader`/`Breadcrumbs`/`EmptyState` 3개 컴포넌트가 모든 페이지에서 동일하게 쓰임. 시각적 리듬은 안정적.
3. **lucide-react icon 일관성** — Sidebar 메뉴 14개 + Header 액션 icon 모두 lucide. weight/size(`h-4 w-4`) 일관. shadcn/ui와 자연스럽게 조화.
4. **테마 토글 패턴이 modern** — `light → dark → system → light` 3-mode cycle. Linear · Vercel과 동일 패턴.
5. **PWA brand chrome 결선** — `favicon.svg` (ShieldCheck), `apple-touch-icon`, `theme-color #0a0a0a` 메타 모두 정의됨. installable PWA로 제출 가능한 minimum 갖춤.

---

## 3. 핵심 약점 (Critical Issues)

### P0 — 당장 fix (~수 시간 each)

**P0-1. Severity color 체계 일관성 0 — 보안 감사 도구의 정체성 손상**
- 현 상태: `findings.tsx`(5색)와 `system.tsx`(4색)가 ad-hoc 매핑. **critical과 high가 같은 `bg-destructive/10 text-destructive` 빨강**으로 합쳐짐 → 사용자가 critical과 high를 시각으로 구분 불가.
- `findings.tsx` line 270-276 + `system.tsx` line 400-405에서 중복 정의.
- `severityVariant()`(findings.tsx:304) Badge variant 매핑은 critical = high = `destructive`, medium = `default`(primary 검정), low = info = `secondary`(회색) — **3단계로 압축**돼 5-severity 모델 의미 손실.
- 영향: 보안 감사인이 화면 보고 즉시 critical을 식별해야 하는데, "둘 다 빨강"이라 즉시 인지 불가. 보안 도구 최대 약점.
- 권장: `globals.css`에 5-severity semantic 토큰 신규 정의 — `--severity-critical: 0 84% 47%` (deep red), `--severity-high: 24 95% 53%` (orange), `--severity-medium: 38 92% 50%` (amber), `--severity-low: 217 91% 60%` (blue), `--severity-info: 215 16% 47%` (gray). 별 utility 컴포넌트 `<SeverityBadge severity="critical">` 신규 → 두 페이지의 ad-hoc 매핑 교체.

**P0-2. `alert.tsx`가 design token을 안 쓰고 hardcoded color 사용 → 다크 모드에서 깨짐**
- `web/src/components/ui/alert.tsx` line 13-15: `'border-red-200 bg-red-50 text-red-800'` / `'border-slate-200 bg-slate-50 text-slate-800'` — Tailwind palette 직접 사용.
- 영향: 다크 모드에서 alert 박스가 매우 밝은 빨강/회색 배경으로 남아 page background와 contrast 깨짐. 다른 모든 ui 컴포넌트는 토큰 사용 — alert만 예외.
- 권장: `border-destructive/30 bg-destructive/10 text-destructive` (destructive variant) + `border-border bg-muted text-foreground` (default variant)로 교체. variant `success` · `warning` · `info` 추가.

**P0-3. PWA manifest의 `name`이 'rosshield Console' — 브랜드 'Lodestar' 미반영**
- `web/vite.config.ts` line 38-41: `name: 'rosshield Console'`, `short_name: 'rosshield'`.
- `web/index.html` line 14: `<title>rosshield Console</title>`.
- 영향: 사용자가 PWA install 시 "rosshield"로 표시 → 제품 브랜드 Lodestar(D1 2026-05-18 확정)와 충돌. iOS/Android 홈 화면 + Windows taskbar 모두 잘못된 이름.
- 권장: `name: 'Lodestar Console'`, `short_name: 'Lodestar'`, html title 동일 교체. 단 `code namespace rosshield`는 유지(D1 분리 결정).

**P0-4. `app.version` dict가 `v0.1.0 · Phase 2`로 stale (실제 v0.5.2)**
- `web/src/i18n/dict.ts` line 14, 879. Sidebar 하단에 표시됨.
- 영향: 사용자가 첫 화면에서 보는 버전 정보가 4 minor 뒤. trust signal 손상.
- 권장: build time injection (`import.meta.env.VITE_APP_VERSION` 같은 패턴, package.json version source of truth).

**P0-5. Font family 미지정 — system font fallback만 사용 → 한국어 typography 부서짐**
- `globals.css`에 `font-family` 선언 없음. body는 browser default (Windows: Segoe UI + Malgun Gothic, macOS: -apple-system + Apple SD Gothic Neo).
- 영향: 한국어 default 제품인데, OS마다 한국어 font가 천차만별. 한·영 혼용 시(예: "scan completed · 스캔 완료") baseline · weight · x-height 불일치. modern enterprise SaaS는 모두 자체 font 지정.
- 권장: Pretendard Variable 셀프호스트(에어갭 1급 원칙 §3 준수) + Inter Variable fallback. `@theme`에 `--font-sans: 'Pretendard Variable', 'Inter', system-ui, -apple-system, ...` 추가. Pretendard는 한국어 + Latin 모두 최적, OFL 라이선스, ~200KB woff2 단일 파일.

### P1 — 다음 round (1~2일 each)

**P1-1. Status color 체계 부정확** — `scans.tsx:732` `statusVariant`가 `cancelled`도 `destructive`로 처리. cancelled는 user-initiated이지 실패가 아님 → 의미상 `secondary`(회색)나 별 `cancelled` variant 필요. 추가로 `pending`과 `running`이 둘 다 `secondary` → 시각적으로 구분 불가. Linear / GitHub Actions는 pending=회색, running=파랑 spinner, completed=초록, failed=빨강, cancelled=회색 strikethrough.

**P1-2. Badge `success`/`warning` variant가 hardcoded green-500/yellow-500** — `badge.tsx:16-17`. dark/light 둘 다 명도 동일 → 다크에서 saturation 너무 진함. semantic 토큰화 필요.

**P1-3. Typography scale 불명확** — `PageHeader` h1 = `text-2xl`(24px), Sidebar brand = `text-sm`(14px), table row = `text-sm`, table head = `text-sm` (기본 + h-12 row 높이), Card title = `text-2xl`(card.tsx:26) → **card title과 page h1이 같은 크기** → hierarchy ambiguous. Linear 패턴: page h1 = 28~32px / card title = 16~18px(semibold). Card title 줄여야.

**P1-4. Sidebar 너비 60(`w-60` = 240px) — 14 메뉴 + brand + version이 정확히 들어가지만 여유 0** — Korean label 길어지면 줄바꿈 위험. Linear/Datadog은 224~272px + 햄버거 collapse 옵션. collapse 토글 미존재.

**P1-5. Card padding `p-6` (24px) 일률 — data density 낮음** — system 페이지는 8개 카드 stack인데 카드마다 24px padding으로 vertical space 낭비. Datadog/Grafana는 12~16px (compact). page 수직 길이 ~30% 단축 가능.

**P1-6. Empty state visual 단조** — `EmptyState`가 `border-dashed bg-muted/30 py-10` 한 가지. illustration 없음. Linear/Vercel은 SVG illustration + 친근한 톤. 보안 감사 도구는 너무 친근할 필요는 없으나 lucide-only는 cold.

**P1-7. Logo 부재 — Sidebar brand 영역이 ShieldCheck icon + text 조합** — 별도 logomark 없음. 보안 도구 브랜드는 보통 wordmark + symbol pair (예: Snyk · 1Password · Tailscale). favicon.svg도 lucide ShieldCheck 재활용 → unique 브랜드 식별성 0.

**P1-8. Border-radius 일률(`--radius: 0.5rem`)** — 카드·버튼·input·badge 모두 동일 8px → 시각적 차별화 없음. Linear/Vercel은 button=6px, card=12px, badge=full(pill), input=6px 식 차등.

### P2 — long-term (2~4주)

**P2-1. Data visualization 0** — Recharts/Visx/Tremor 같은 chart library 없음. system 페이지의 "최근 50 세션 severity 합계"는 4-cell number grid로 표시 → trend line · stacked bar 등 시각화 부재. 보안 감사 도구의 핵심 가치는 "시간축 trend" + "fleet/robot 분포" 시각화. Tremor (shadcn 호환, Recharts wrapping) 권장.

**P2-2. Spacing scale 4의 배수 임의 사용** — `gap-2.5`, `py-1.5`, `text-[10px]`, `text-[11px]` 등 reserved 외 값이 산재. shadcn token 외 magic value가 5~7개 페이지에서 발견. spacing token 표준화 (4·8·12·16·24·32·48 · 64 = 8-multiple) 필요.

**P2-3. Density mode 부재** — 보안 audit 작업은 long session(수십 분 ~ 시간). data density 높은 "compact" mode 토글 권장 (table padding · font 1단계 down).

**P2-4. Brand color palette 부재** — primary가 `222.2 47.4% 11.2%` 거의 검정. accent color 없음 → 모든 강조가 같은 검정 hue. trust + intelligence 컬러 (deep navy `#1e3a8a` 정도) + accent (signal cyan/teal) 정의가 향후 brand identity의 anchor.

---

## 4. 권장 개선안 (Recommendations)

### Brand identity
- **Logomark 신규** — ShieldCheck variant 또는 nautical 별(Lodestar = 북극성) 모티프 SVG. 사용처: Sidebar brand 영역, login 페이지, manifest icons, favicon. wordmark "Lodestar"는 Pretendard SemiBold + 약간의 letter-spacing(-0.02em).
- **manifest + html title `Lodestar` 통일** — P0-3 참조.
- **brand color palette**: primary navy `#0a1628` (서명 + trust), accent `#0ea5e9` (signal/링크), neutral 9-step gray scale (Tailwind slate 차용).
- **로고 lockup 가이드** — clear space · minimum size · dark/light variant. 별 README 또는 brand notes doc.

### Typography
- **Pretendard Variable 셀프호스트** — `web/public/fonts/PretendardVariable.woff2` 추가, `globals.css`에 `@font-face` + `font-display: swap`. fallback: Inter, ui-sans-serif.
- **scale**: text-xs(12) · text-sm(14) · text-base(15) · text-lg(17) · text-xl(20) · text-2xl(24) · text-3xl(30) · text-4xl(36). 한국어 본문은 14~15px이 가장 읽힘 (Inter 본문 14 vs Pretendard 본문 15 권장).
- **line-height**: body 1.6, headings 1.25, table cell 1.4.
- **letter-spacing**: heading -0.02em (tight), body 0, mono normal.
- **font feature**: `'rlig' 1, 'calt' 1` 이미 결선(globals.css:111) — `'cv11' 1` (Pretendard 한·영 stylistic set) 추가 권장.

### Color palette (semantic tokens 신규)
```css
/* globals.css @layer base — :root + .dark 양쪽 */
--severity-critical: 0 84% 47%;     /* deep red */
--severity-high:     24 95% 53%;    /* orange */
--severity-medium:   38 92% 50%;    /* amber */
--severity-low:      217 91% 60%;   /* blue */
--severity-info:     215 16% 47%;   /* gray */

--status-pending:    215 16% 47%;   /* gray */
--status-running:    217 91% 60%;   /* blue (with pulse animation) */
--status-completed:  142 71% 45%;   /* green */
--status-failed:     0 84% 47%;     /* red */
--status-cancelled:  215 14% 65%;   /* light gray */

--semantic-success:  142 71% 45%;
--semantic-warning:  38 92% 50%;
--semantic-info:     217 91% 60%;
--semantic-error:    0 84% 47%;
```
다크 모드 변형은 lightness 5~15 lift.

### Spacing + grid
- 4의 배수만 사용 (`gap-1` 4, `gap-2` 8, `gap-3` 12, `gap-4` 16, `gap-6` 24, `gap-8` 32). magic value(`gap-2.5`, `py-1.5`) 제거.
- Card 기본 padding `p-4`(16)로 down — `p-6` variant는 hero card에만.
- Page section spacing `space-y-4`(16) → `space-y-6`(24)로 lift (현재 너무 빡빡).

### Iconography
- lucide-react 유지 (이미 일관). weight 1.5 (lucide default 2 → 1.5로 가벼움 추가) 검토.
- severity icon 매핑 — `AlertOctagon`(critical), `AlertTriangle`(high), `AlertCircle`(medium), `Info`(low), `Circle`(info).
- status icon — `Loader2 animate-spin`(running), `CheckCircle2`(completed), `XCircle`(failed), `CircleSlash`(cancelled), `Clock`(pending).

### Data viz
- **Tremor 도입** (shadcn 호환, MIT) — line/bar/donut chart + sparkline + KPI card 모두 default style이 shadcn token과 자동 매핑. severity trend(7d/30d), fleet 분포, scan throughput 등 즉시 적용 가능.
- 별 round design doc 권장 (P2).

### Component hierarchy
- **Card title 줄이기**: `text-2xl` → `text-base font-semibold` (16px). page h1만 24~28px 유지.
- **Button hierarchy 강화**: primary는 brand color, secondary는 outline, tertiary는 ghost — 현재 3 단계가 모두 회색 monochrome이라 CTA 명확성 부족. primary 버튼 = accent color로 칠하기.
- **Badge `pill` (radius-full)**과 **`square` (radius-sm)** variant 분리 — status는 pill, count는 square가 modern convention.

---

## 5. 즉시 적용 가능 P0 fix (코드 변경 ~수 시간)

각 fix는 별 round commit으로 분리 권장 (review·rollback 용이).

**Fix 1. PWA manifest + html title 브랜드 통일** (~10분)
- `web/vite.config.ts` line 38-41:
  - `name: 'rosshield Console'` → `name: 'Lodestar Console'`
  - `short_name: 'rosshield'` → `short_name: 'Lodestar'`
- `web/index.html` line 14: `<title>rosshield Console</title>` → `<title>Lodestar Console</title>`

**Fix 2. `app.version` dict stale 제거** (~30분)
- `web/src/i18n/dict.ts` line 14, 879: `'app.version': 'v0.1.0 · Phase 2'` 제거
- `web/vite.config.ts` define injection: `__APP_VERSION__: JSON.stringify(pkg.version)` 추가
- `web/src/components/layout/Sidebar.tsx` line 147: `t('app.version')` → `__APP_VERSION__` 직접 참조 + `declare const __APP_VERSION__: string` 추가

**Fix 3. `alert.tsx` 토큰화 + dark mode 대응** (~30분)
- `web/src/components/ui/alert.tsx` line 13-15: variant 매핑을 토큰 기반으로
  - `destructive` → `'border-destructive/30 bg-destructive/10 text-destructive'`
  - `default` → `'border-border bg-muted text-foreground'`
- variant 추가: `success`(border-green-500/30 bg-green-500/10 text-green-700 dark:text-green-400), `warning`(border-yellow-500/30 bg-yellow-500/10 text-yellow-800 dark:text-yellow-400), `info`(border-blue-500/30 bg-blue-500/10 text-blue-700 dark:text-blue-400). 본 fix는 P0-2 임시 해결 — P1 round에서 semantic token으로 정식 마이그레이션.

**Fix 4. Severity 5-color 토큰 + `<SeverityBadge>` 신규** (~2~3시간)
- `web/src/styles/globals.css` `@layer base`에 5-severity HSL 토큰 추가 (P0-1 참조).
- `web/src/components/ui/severity-badge.tsx` 신규 — props: `severity: 'critical'|'high'|'medium'|'low'|'info'`, 토큰 자동 매핑 + icon prefix.
- `findings.tsx:270-300` `SeverityStats` 매핑 + `severityVariant()` 삭제 → `<SeverityBadge>` 교체.
- `system.tsx:400-428` ad-hoc 매핑 동일 교체.
- 테스트: `findings.test.tsx`의 severity badge assertion 갱신.

**Fix 5. Pretendard Variable 셀프호스트 + font token** (~1~2시간)
- `web/public/fonts/PretendardVariable.woff2` 추가 (Pretendard GitHub release, OFL).
- `web/src/styles/globals.css`:
  ```css
  @font-face {
    font-family: 'Pretendard Variable';
    font-weight: 45 920;
    font-style: normal;
    font-display: swap;
    src: url('/fonts/PretendardVariable.woff2') format('woff2-variations');
  }
  @theme {
    --font-sans: 'Pretendard Variable', 'Inter', ui-sans-serif, system-ui, ...;
  }
  ```
- 한국어 사용자 대상 product에서 가장 큰 visual perception lift.

---

## 6. 별 round design doc 후보

| 후보 | 우선순위 | 추정 | 비고 |
|------|---------|------|------|
| **Design system token 표준화** (severity · status · semantic · brand color + spacing scale 정리) | P1 | 1~2일 | P0-1/P0-2 임시 fix를 정식 토큰화. shadcn/ui custom palette config + theme storybook. |
| **Brand identity round 2** (logomark · wordmark · color palette · lockup guide) | P1 | 3~5일 | Lodestar 시각 정체성 확립. SVG logomark 디자인 + Sidebar/login/PDF report 적용. 외부 designer 협업 옵션. |
| **Density mode + table compact variant** | P2 | 2~3일 | 사용자 설정에 "compact" 모드 추가. table padding · font size 1단계 down. long-session 사용자 (감사인) 만족도. |
| **Data visualization 강화 (Tremor 도입)** | P2 | 2~4주 | system dashboard + overview + findings에 chart 추가. severity trend (line), fleet distribution (stacked bar), scan throughput (sparkline). 별 design doc + Tremor vs Recharts vs Visx 비교 필요. |
| **Empty state illustration set** | P2 | 1주 | findings/scans/audit/reports 등 빈 상태 SVG illustration 6~8개. 별 design system 자산. |

---

## Cross-reference 권장 (다른 페르소나 sub-agent)

- **UX research (`ui-review-ux.md`)** — Sidebar collapse 토글 부재(P1-4)는 navigation 패턴 영역, UX와 겹침. Density mode(P2-3)도 UX 사용자 시나리오 검증 필요.
- **Frontend interaction (`ui-review-frontend.md`)** — Pretendard 셀프호스트(Fix 5)는 build/bundle 영향, frontend perf 영역과 cross. Tremor 도입(P2-1)도 frontend lib 평가 필요.
- **Accessibility (`ui-review-a11y.md`)** — Severity 5-color 토큰(P0-1)은 color contrast WCAG AA 검증 필요(특히 amber medium · light dark 두 모드 모두). Status color 5종(P1-1)도 동일. dark mode contrast (`alert.tsx` Fix 3)도 a11y 영역.
