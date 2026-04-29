# rosshield Web Console

Phase 1 Web Console — `rosshield-server` Go 백엔드의 주 UI.
설계 근거: `docs/design/09-ui-and-clients.md` §9.2.

## 스택

- React 19 + TypeScript
- Vite 6 + pnpm
- TanStack Router v1 (file-based) + TanStack Query v5 + Zustand
- Tailwind CSS v4 + shadcn/ui (전신 nrobotcheck에서 23개 컴포넌트 복사)
- Vitest + RTL은 Stage D에서 도입

## 사전 조건

- Node.js 20+ (권장 22.x)
- pnpm 9+ (권장 10.x)

## 명령어

```bash
pnpm install          # 의존성 설치 (최초 1회)
pnpm dev              # 개발 서버 (http://localhost:5173, /api → :8080 proxy)
pnpm build            # tsc -b && vite build → web/dist/
pnpm preview          # 빌드 결과 미리보기
pnpm lint             # ESLint
```

리포 루트 Makefile에서도 호출 가능합니다.

```bash
make web-install
make web-dev
make web-build
```

## 디렉터리

```
web/
├─ src/
│  ├─ App.tsx                  # Router + QueryClient 결선
│  ├─ main.tsx                 # React 19 진입
│  ├─ routes/                  # TanStack Router file-based
│  │   ├─ __root.tsx           # 공유 레이아웃 (Header + Sidebar + Outlet)
│  │   └─ index.tsx            # 임시 Welcome (Stage C에서 Overview 대체)
│  ├─ components/
│  │   ├─ layout/              # Header, Sidebar
│  │   └─ ui/                  # shadcn/ui (전신 복사, import 경로만 정리)
│  ├─ lib/utils.ts             # cn() helper
│  ├─ api/                     # Stage B에서 openapi-fetch 클라이언트 추가
│  └─ styles/globals.css       # Tailwind v4 + 디자인 토큰
├─ index.html
├─ vite.config.ts
├─ tailwind.config.ts
├─ postcss.config.js
└─ tsconfig*.json
```

## 진행 단계

- **Stage A (현재)**: 모노레포 부트스트랩 + UI 컴포넌트 복사 + 골격 레이아웃.
- Stage B: openapi-fetch 기반 API 클라이언트 결선, localStorage 토큰.
- Stage C: 4 페이지(Login/Robots/Scans/Reports) 구현.
- Stage D: Vitest 테스트 + Go `embed.FS`로 단일 바이너리 결합.
