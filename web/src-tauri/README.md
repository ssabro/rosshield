# rosshield Desktop (Tauri 2.x)

> Phase 1 Carryover **C2** 회수 — D3 결정대로 Tauri 2.x로 데스크톱 셸 구축.

## 개요

`rosshield-server` 백엔드와 동일한 Web Console을 Tauri WebView2 셸에 패키징한 데스크톱 앱입니다. Phase 1까지의 단일 바이너리(서버+API+SPA)는 그대로 유지되고, 본 Tauri 셸은 별도 산출물(.exe / .msi / .app / .dmg)로 OS-native 진입점을 제공합니다.

설계 정합:

- **D3** (2026-04-23) — 데스크톱 셸 = Tauri 2.x. Electron은 fallback 보류.
- **R12-9** (E10 Stage A) — Phase 1에서 deferred → Phase 2 carryover로 회수.
- **P3** (에어갭 1급) — 데스크톱은 단일 머신 로컬 백엔드 호출. 외부 통신 0.
- **P7** (단일 바이너리·다중 껍질) — 코어는 동일, 셸만 다름.

## 디렉터리 구조

```
web/                  # Vite + React + TanStack 프론트엔드 (E10 Stage A~D)
  ├─ src/             # React 코드
  ├─ src-tauri/       # ◀ 본 폴더 (Tauri 2.x Rust 셸)
  │   ├─ Cargo.toml
  │   ├─ tauri.conf.json
  │   ├─ src/
  │   │   ├─ main.rs
  │   │   └─ lib.rs
  │   ├─ icons/       # placeholder Tauri 기본
  │   └─ capabilities/
  └─ package.json     # `tauri:dev`, `tauri:build` scripts
```

## 사전 요구사항

### 개발 머신
- **Rust 1.77.2+** (`rustup` 설치 → `rustup default stable`)
- **Node.js 22.x** + **pnpm 10.x**
- **Tauri OS 의존성**:
  - **Windows**: WebView2 (Edge에 자동 동봉, Win 11/Win 10 1803+ 기본 설치)
  - **macOS**: Xcode CLT (`xcode-select --install`)
  - **Linux**: webkit2gtk + libssl + libgtk-3 — `https://tauri.app/start/prerequisites/` 참조

### 빌드 산출물 사이즈 가이드
- 첫 `cargo build` (의존성 컴파일): ~10분 (Windows·1회)
- 이후 incremental: <30초
- release MSI: ~10MB (WebView2 미포함 — system 자동)

## 빠른 시작

### 1. 프로젝트 의존성 설치

```bash
cd D:/robot/dev/fleetguard/web
pnpm install      # Node 의존성 + @tauri-apps/cli
```

### 2. 개발 모드 (hot reload)

```bash
cd D:/robot/dev/fleetguard/web
pnpm tauri:dev
```

흐름:
1. Tauri CLI가 `pnpm dev`로 vite dev server 부팅 (http://localhost:5173)
2. Rust 빌드 + WebView 윈도우 오픈 → vite dev URL 로드
3. React 코드 수정 시 hot reload, Rust 코드 수정 시 자동 재빌드

### 3. 프로덕션 빌드

```bash
cd D:/robot/dev/fleetguard/web
pnpm tauri:build
```

흐름:
1. `pnpm build`로 정적 자산을 `../internal/web/dist`에 출력
2. Tauri가 그 폴더를 `frontendDist`로 임베드
3. 산출물:
   - **Windows**: `src-tauri/target/release/rosshield.exe` + MSI/NSIS installer
   - **macOS**: `.app` + `.dmg`
   - **Linux**: `.AppImage` + `.deb`

## 백엔드 연결

데스크톱 셸은 별도 백엔드를 spawn하지 않습니다 (Phase 1 단순화). 사용자가 미리 `rosshield-server -addr 127.0.0.1:8080`을 실행한 상태에서 셸이 같은 origin으로 API/WebSocket 호출.

후속 (Phase 3+):
- **자동 spawn**: Tauri 셸이 백엔드 바이너리를 child process로 자동 시작·종료
- **System tray**: 트레이 아이콘 + show/hide
- **Auto-update**: tauri-plugin-updater (서명된 매니페스트)

## 주요 설정

### tauri.conf.json
- `productName`: rosshield
- `identifier`: io.rosshield.desktop
- `frontendDist`: `../../internal/web/dist` (vite outDir과 일치)
- `devUrl`: http://localhost:5173 (vite dev)
- `windows[0]`: 1280×800 (min 960×600)

### 보안
- `csp: null` 임시 — Phase 1 단순화. Phase 2 production 전 strict CSP 적용 필요 (Tauri 권장값).
- 외부 origin 호출 0 (P3).

## 알려진 한계

- **Hot reload는 frontend만**: Rust 코드 변경은 cargo 재빌드 (긴 경우 30s).
- **Bundle 크기**: WebView2 의존이라 자체 brower runtime 미포함 → 사용자 OS 환경에 의존.
- **macOS 코드 서명 미설정**: 배포 시 `signingIdentity` + 공증 필요 (Phase 3).
- **자동 업데이트 미구성**: tauri-plugin-updater + 서명된 매니페스트 별도 epic.

## 다음 단계 (Phase 3+)

| 항목 | 추정 |
|---|---|
| 백엔드 자동 spawn (rosshield-server child process) | 1주 |
| System tray + 백그라운드 모드 | 3일 |
| tauri-plugin-updater 결합 | 1주 |
| MSI/DMG 코드 서명 + 공증 | 인프라 의존 |
| custom 아이콘·스플래시 | 디자인 의존 |
