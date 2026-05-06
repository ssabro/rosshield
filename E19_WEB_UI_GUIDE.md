# E19 Web UI 진행 가이드

> Phase 2 마지막 epic. Compliance·Findings·Advisor 페이지를 Web Console에 추가합니다. 백엔드 API는 W2(`85a0974`)에서 모두 준비됨 — frontend만 작업.

> **상태 (2026-05-06)**: 모든 코드 작업 완료. E19-1 Findings·E19-2 Compliance·E19-3-A Advisor 백엔드 HTTP 표면·E19-3-B Advisor 웹 페이지 모두 main에 적용됨(uncommitted). 본 가이드는 사용자가 dev 서버에서 검증하고 commit/push하기 위한 참고용입니다. 자세한 변경 요약은 `SESSION_HANDOFF.md` "현재 상태 한 줄" 참조.

**프로젝트 루트**: `D:\robot\dev\fleetguard\`
**작업 대상 디렉토리**: `D:\robot\dev\fleetguard\web\`
**예상 분량**: Findings 1~2시간, Compliance 3~4시간, Advisor 1일 + 백엔드 endpoint 추가 1~2시간

---

## 1. 사전 준비 (한 번만)

### 1-1. 도구 설치 확인

PowerShell 또는 git bash에서:

```bash
node --version       # v22.x 필요 (없으면 https://nodejs.org/)
pnpm --version       # 10.x 필요 (없으면 npm install -g pnpm@10)
go version           # 1.26 (이미 설치됨)
```

### 1-2. 의존성 설치

**중요**: 반드시 `web/` 폴더에서 실행 (루트 X).

```bash
# 작업 폴더로 이동
cd D:\robot\dev\fleetguard\web

# 의존성 설치 (package.json 변동 시만, 첫 1회 또는 dependencies 추가 후)
pnpm install
```

설치 후 `web/node_modules/` 폴더가 생성됩니다.

---

## 2. 폴더 구조 한눈에 보기

```
D:\robot\dev\fleetguard\
├─ cmd/rosshield-server/        ← Go 백엔드 (이미 동작)
├─ internal/                    ← Go 도메인 코드 (이미 동작)
├─ openapi/openapi.yaml         ← API 스펙 (수정 시 frontend types 재생성)
└─ web/                         ← ⭐ 이번 작업 영역
   ├─ package.json
   ├─ vite.config.ts
   ├─ src/
   │  ├─ main.tsx
   │  ├─ App.tsx
   │  ├─ api/                   ← API 호출 레이어
   │  │  ├─ types.ts            ← OpenAPI 자동 생성 (수정 금지)
   │  │  ├─ client.ts           ← fetch wrapper + Bearer 자동 부착
   │  │  ├─ errors.ts           ← ApiError 클래스
   │  │  └─ hooks.ts            ← ⭐ 여기에 useInsights 등 추가
   │  ├─ components/
   │  │  ├─ layout/
   │  │  │  ├─ Header.tsx       ← 우측 상단 user info
   │  │  │  └─ Sidebar.tsx      ← ⭐ 여기에 NavLink 추가
   │  │  └─ ui/                 ← shadcn 23개 (Card·Table·Button 등)
   │  ├─ routes/                ← TanStack Router (file-based)
   │  │  ├─ __root.tsx
   │  │  ├─ login.tsx
   │  │  └─ _authenticated/
   │  │     ├─ route.tsx        ← 인증 가드 + Sidebar/Header 셸
   │  │     ├─ robots.tsx       ← ⭐ 복사 출발점 (가장 단순한 패턴)
   │  │     ├─ scans.tsx
   │  │     ├─ reports.tsx
   │  │     ├─ findings.tsx     ← ⭐ 신규 (E19-1)
   │  │     ├─ compliance.tsx   ← ⭐ 신규 (E19-2)
   │  │     └─ advisor.tsx      ← ⭐ 신규 (E19-3)
   │  ├─ routeTree.gen.ts       ← 자동 생성 (수정 금지, dev server가 갱신)
   │  ├─ stores/
   │  │  └─ auth.ts             ← Zustand persist (accessToken·user)
   │  └─ test/
   │     └─ setup.ts            ← Vitest setup
   └─ dist/                     ← 빌드 출력 → 자동으로 internal/web/dist/로 출력됨
```

**작업 시 cwd 규칙**:
- frontend 명령(pnpm·vite): 항상 `D:\robot\dev\fleetguard\web\`
- backend 명령(go·make): 항상 `D:\robot\dev\fleetguard\` (루트)
- git 명령: 어느 위치든 OK (자동으로 repo 루트 사용)

---

## 3. 개발 서버 실행 (작업 시작할 때마다)

**터미널 2개를 동시에 띄웁니다.**

### 터미널 A — Frontend dev server (hot reload)

```bash
cd D:\robot\dev\fleetguard\web
pnpm dev
# 출력: VITE v6.x  ready in ... ms
#       ➜  Local:   http://localhost:5173/
```

브라우저에서 `http://localhost:5173` 열기. 코드 수정 시 자동 reload.

### 터미널 B — Backend (API 서버)

```bash
cd D:\robot\dev\fleetguard

# 첫 실행만 — admin user 시드 (한 번만, data.db 생성됨)
go run ./cmd/rosshield-server seed admin --email me@local --password longpassword123

# 이후 매번 — 서버 시작 (반드시 -addr로 8080 명시!)
go run ./cmd/rosshield-server -addr 127.0.0.1:8080
# 출력: "addr":"127.0.0.1:8080"  ← 이게 8080이어야 함
#       platform bootstrap complete ...
```

**⚠️ `-addr` 생략하면 main.go 기본값 `127.0.0.1:0` (랜덤 포트)로 떠서 vite proxy(8080)와 안 맞아 ECONNREFUSED 발생합니다.** 반드시 `-addr 127.0.0.1:8080` 명시.

**Vite dev server가 `/api/*` 요청을 8080으로 자동 proxy합니다** (`web/vite.config.ts`의 `server.proxy` 설정). 즉 `localhost:5173/api/v1/auth/login` → `localhost:8080/api/v1/auth/login`로 forward.

따라서 **backend(8080)가 안 떠있으면 vite proxy가 ECONNREFUSED 에러를 표시하고 로그인 실패**합니다. 두 터미널 모두 살아있어야 함.

증상별 해결:
- `vite http proxy error: ECONNREFUSED` → 터미널 B의 `go run ./cmd/rosshield-server` 안 띄워짐. 먼저 띄우기.
- `Cannot destructure property 'accessToken' of 'undefined'` → 같은 원인. backend ECONNREFUSED → response가 undefined → setSession이 destructure 시도 시 폭발. (hooks.ts에 방어 가드 추가됨 — 이제 명확한 에러 메시지로 표시.)

브라우저에서 `me@local` / `longpassword123`로 로그인 → `/robots`로 자동 이동.

---

## 4. E19 작업 단계별 진행

### 단계 0 — API 타입 재생성 (5분, 모든 단계 시작 전 1회)

W2에서 OpenAPI spec에 7개 endpoint를 추가했으니 frontend types를 새로 만들어야 합니다.

```bash
# 루트에서 실행
cd D:\robot\dev\fleetguard
make web-types
# 또는 직접:
# cd web && pnpm openapi-typescript ../openapi/openapi.yaml -o src/api/types.ts
```

`web/src/api/types.ts` 파일이 갱신됩니다. git diff로 확인 가능.

---

### 단계 1 — Findings 페이지 (1~2시간, 가장 단순)

**목표**: `/findings`에서 active insights 목록을 표 형태로 보여주기.

**1-A. API 훅 추가** — `web/src/api/hooks.ts` 끝에 다음 추가:

```typescript
// hooks.ts 끝에 추가
export function useInsights(filter?: { kind?: string; severity?: string; robotId?: string }) {
  return useQuery({
    queryKey: ['insights', filter],
    queryFn: async () => {
      const params = new URLSearchParams();
      if (filter?.kind) params.set('kind', filter.kind);
      if (filter?.severity) params.set('severity', filter.severity);
      if (filter?.robotId) params.set('robotId', filter.robotId);
      const url = `/api/v1/insights${params.toString() ? '?' + params : ''}`;
      const res = await apiClient.GET(url as any);
      if (res.error) throw new ApiError(res.response.status, JSON.stringify(res.error));
      return res.data as { insights: Array<{
        id: string; kind: string; severity: string; summary: string;
        robotId?: string; checkId?: string; createdAt: string;
      }> };
    },
    enabled: !!useAuthStore.getState().accessToken,
  });
}
```

> 정확한 import는 hooks.ts 상단의 기존 import를 보고 재활용. 기존 `useRobots` 함수가 좋은 참고.

**1-B. 페이지 파일 신규** — `web/src/routes/_authenticated/findings.tsx`:

```typescript
import { createFileRoute } from '@tanstack/react-router';
import { useInsights } from '@/api/hooks';
import { Card } from '@/components/ui/card';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';

export const Route = createFileRoute('/_authenticated/findings')({
  component: FindingsPage,
});

function FindingsPage() {
  const { data, isLoading, error } = useInsights();

  if (isLoading) return <Card className="p-6">로딩 중...</Card>;
  if (error) return <Card className="p-6 text-red-600">오류: {error.message}</Card>;

  const insights = data?.insights ?? [];

  return (
    <div className="space-y-4">
      <h1 className="text-2xl font-bold">Findings</h1>
      <Card className="p-4">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Kind</TableHead>
              <TableHead>Severity</TableHead>
              <TableHead>Summary</TableHead>
              <TableHead>Robot</TableHead>
              <TableHead>Created</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {insights.length === 0 ? (
              <TableRow>
                <TableCell colSpan={5} className="text-center text-muted-foreground">
                  아직 insight가 없습니다.
                </TableCell>
              </TableRow>
            ) : (
              insights.map((i) => (
                <TableRow key={i.id}>
                  <TableCell>{i.kind}</TableCell>
                  <TableCell>{i.severity}</TableCell>
                  <TableCell>{i.summary}</TableCell>
                  <TableCell className="text-muted-foreground">{i.robotId ?? '-'}</TableCell>
                  <TableCell className="text-muted-foreground">{new Date(i.createdAt).toLocaleString('ko-KR')}</TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </Card>
    </div>
  );
}
```

> `robots.tsx`와 거의 동일 구조 — 그것을 그대로 복사 후 변형해도 됩니다.

**1-C. Sidebar에 NavLink 추가** — `web/src/components/layout/Sidebar.tsx` 열고 robots/scans/reports 옆에 1줄 추가:

기존 NavLink 패턴을 찾아서 다음과 비슷하게 추가:
```typescript
<NavLink to="/findings" /* 또는 기존 패턴에 맞춰서 */>Findings</NavLink>
```

**1-D. 검증**:
- 터미널 A의 dev server는 자동 reload — 브라우저 `http://localhost:5173/findings` 접속
- 빈 표 보이면 OK (실제 insight는 scan 실행 후 생성됨)
- 콘솔에 에러 없으면 통과

**1-E. Vitest 단위 테스트** (선택):
```bash
cd D:\robot\dev\fleetguard\web
pnpm test
```

**1-F. 빌드 + Go embed 검증**:
```bash
# 1. Frontend 빌드 (web/ 폴더)
cd D:\robot\dev\fleetguard\web
pnpm build
# 출력: vite v6 build successful, dist generated at ../internal/web/dist/

# 2. Go 서버 재실행 (루트 폴더, 새 터미널)
cd D:\robot\dev\fleetguard
go run ./cmd/rosshield-server

# 3. 브라우저로 확인 — 이번엔 8080 (embed된 Web Console)
# http://localhost:8080/findings
```

embed에서 보이면 단계 1 완료.

**1-G. Commit**:
```bash
cd D:\robot\dev\fleetguard
git add web/src/ internal/web/dist/
git commit -m "feat(web): E19-1 — Findings 페이지 (Insight 목록)"
```

---

### 단계 2 — Compliance 페이지 (3~4시간)

**목표**: `/compliance`에서 profile 선택 + 점수 카드 + 통제 트리 표시.

**2-A. API 훅 추가** (`hooks.ts`):
- `useComplianceProfiles()` → `GET /api/v1/compliance/profiles`
- `useComplianceSnapshots(profileId)` → `GET /api/v1/compliance/profiles/{profileId}/snapshots`
- `useCreateComplianceProfile()` (mutation) → `POST /api/v1/compliance/profiles`
- `useGenerateSnapshot(profileId)` (mutation) → `POST /api/v1/compliance/profiles/{profileId}/snapshots`

**2-B. 페이지** — `web/src/routes/_authenticated/compliance.tsx`:
- 상단: framework 선택 dropdown + "Activate" 버튼 (CreateProfile)
- 가운데: 활성 profile 점수 카드 (OverallScore, Pass/Fail/Partial 분포)
- 하단: snapshot 히스토리 표 + "Generate" 버튼

**2-C. shadcn 컴포넌트 활용**:
- `<Card>` — 점수 카드
- `<Select>` — framework 선택
- `<Progress>` — 점수 게이지 (이미 ui/ 폴더에 있음)
- `<Table>` — snapshot 목록

**2-D. 검증·빌드·commit**: 단계 1과 동일.

---

### 단계 3 — Advisor 페이지 (1일)

**⚠️ 백엔드 endpoint 먼저 필요**

`/api/v1/advisor/ask`·`/api/v1/advisor/conversations`·`GET /api/v1/advisor/conversations/{id}` 3개가 아직 없습니다.

선택지:
- **(A) 백엔드를 먼저 추가** — handlers + OpenAPI spec + Mount + 테스트. W2 패턴 그대로(`internal/api/handlers/insight.go` 참고). **이 부분은 제가 자동 진행 가능합니다 — 요청만 주세요.**
- **(B) Advisor 페이지를 일단 보류**하고 Findings·Compliance만 끝낸 후 백엔드 추가 시점에 진행.

**(A) 진행 후 frontend 작업**:
- `useAskAdvisor()` mutation
- `useConversations()`, `useConversation(id)` 쿼리
- 페이지 UI: 대화 히스토리(좌측) + 입력 form(하단) + 답변 영역
- LLM 옵트인 gate: 첫 호출이 `503 ErrAdvisorDisabled`면 "LLM이 비활성화됨" 안내

---

## 5. 자주 막히는 지점

| 증상 | 원인 / 해결 |
|---|---|
| `pnpm install` 멈춤 | 네트워크 또는 lockfile mismatch — `pnpm install --frozen-lockfile=false` |
| dev server에서 `/api/*` 401 | accessToken 없음 — `/login`에서 로그인 후 다시 |
| dev server에서 `/api/*` 403/500 | backend 안 떠있음 — 터미널 B 확인 (`localhost:8080/healthz`) |
| 타입이 `unknown`이거나 mismatch | `make web-types` 다시 실행 / `as any` 임시 캐스팅 (E10에서 자주 사용한 패턴) |
| 401 → `/login` 무한 loop | accessToken이 stale — DevTools → Application → Local Storage → `rosshield-auth` 삭제 |
| 빌드 후 Go 서버에 새 페이지 안 보임 | `pnpm build` 실패했거나 Go 서버 재시작 안 함 — Ctrl+C로 종료 후 `go run` 다시 |
| TanStack Router 404 | 파일명 typo (file-based routing) — 파일 저장 시 routeTree.gen.ts가 자동 갱신되는지 dev server 출력 확인 |
| Vitest 실패 | `web/src/test/setup.ts` 누락 또는 path alias `@/` 깨짐 — `vite.config.ts` 확인 |

---

## 6. 검증 체크리스트 (각 페이지마다)

```bash
# 1. Frontend 단위 테스트
cd D:\robot\dev\fleetguard\web
pnpm test

# 2. Frontend 빌드 (dist 출력)
pnpm build

# 3. Backend 빌드 (embed 검증)
cd D:\robot\dev\fleetguard
go build ./...

# 4. Go 회귀 테스트 (혹시 web embed 영향)
go test -count=1 ./internal/web/...

# 5. 브라우저 e2e 검증
go run ./cmd/rosshield-server
# → http://localhost:8080 → 로그인 → 새 페이지 클릭 → 정상 표시 확인
```

5단계 모두 통과하면 commit + push.

---

## 7. Commit 컨벤션 (CLAUDE.md 준수)

```bash
cd D:\robot\dev\fleetguard
git add web/src/ internal/web/dist/    # frontend 코드 + embed 산출물
git commit -m "feat(web): E19-1 — Findings 페이지 (한국어 본문)"
git push origin main                    # CI 자동 실행
```

**중요**:
- `internal/web/dist/`는 반드시 함께 commit해야 함 (Go `//go:embed`가 요구 — `.gitignore`에 예외 등록되어 있음)
- Co-Author 라인은 붙이지 않음
- 메시지는 한국어 ("E19-1 Findings 페이지" 등)

---

## 8. 막히면

1. **에러 메시지 + 어떤 단계인지** 알려주세요. 백엔드 보강이 필요한 경우(예: advisor endpoint) 자동 진행 가능합니다.
2. SESSION_HANDOFF.md의 E10 Web UI 항목을 참고하면 첫 toolchain 설계 결정(R12-1~R12-12)이 있습니다.
3. 기존 `robots.tsx`를 가장 정직한 reference로 활용 — fetch·로딩·에러·테이블 패턴이 모두 거기 있습니다.

---

## 9. 다음 자동 진행 가능 항목 (선택)

E19 진행 중 또는 후에 제가 자동 진행 가능한 백엔드 작업:

- **Advisor endpoint 추가** (`/api/v1/advisor/ask` 등) — Web UI 단계 3의 선결 조건
- **C1 WebSocket scan progress** 백엔드만 (frontend 부분은 사용자)
- **C5 i18next 번들 백엔드 통합** (필요 시)

요청 주시면 언제든 진행합니다.
