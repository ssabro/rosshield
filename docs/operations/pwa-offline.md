# rosshield PWA / 오프라인 운영 가이드 (PWA epic Stage 4 마감)

> **상태**: PWA Stage 1~4 (manifest + SW + offline UX + mutation 가드) 머지 완료.
> **참조**: `docs/design/notes/pwa-offline-design.md` (Phase 5 epic 본문 + D-PWA-1~8 결정 로그).
> **대상**: 어플라이언스 운영자 / 감사인 / 데모 진행자.

본 문서는 rosshield Web Console의 **PWA(Progressive Web App)** 기능을 운영하는
가이드입니다. 에어갭 §3 정책에 따라 백엔드 단절 시 셸 진입 + 마지막 데이터 표시 +
mutation 차단 경로를 제공합니다.

---

## 1. PWA 설치 절차

rosshield Console은 **manifest.webmanifest + service worker** 기반 PWA로 빌드되며
별 install 동작 없이 모든 사용자 브라우저에서 자동 동작합니다.

### 1.1 데스크톱 (Chrome / Edge)

1. Console URL 접속 후 첫 로드 완료까지 대기 (약 3~5초, 정적 자산 SW precache).
2. 주소창 우측 끝의 **install 아이콘**(컴퓨터 + 다운로드 화살표)을 클릭.
   - 노출되지 않는 경우: ⋮ 메뉴 → "rosshield 설치…" 또는 "앱 설치".
3. "설치" 버튼 클릭 → 별도 창 + 작업 표시줄 + 시작 메뉴에 등록.
4. 설치된 앱은 SW를 공유하므로 추가 다운로드 0.

### 1.2 모바일 (iOS Safari / Android Chrome)

- **iOS Safari**: 공유 버튼(▭↑) → "홈 화면에 추가" → 이름 확인 → "추가".
  - 주의: iOS는 manifest 표준 미흡으로 `apple-touch-icon.png`이 우선 사용됨.
- **Android Chrome**: 주소창 하단 또는 ⋮ 메뉴 → "홈 화면에 추가" → "설치".
  - 자동 install banner는 Lighthouse "Installable" 기준 충족 시 1회 노출.

### 1.3 어플라이언스 환경

- on-prem 어플라이언스(NUC/OptiPlex)에서 LAN 단절을 대비해 사용자 노트북에
  **Console PWA install 권장** — 단절 후에도 마지막 화면 + 메뉴 진입 OK.
- 단, 신규 데이터 fetch는 백엔드 복구 후에만 가능 (아래 §2 참조).

---

## 2. 오프라인 동작 범위

design doc §3.4 시나리오 표 + Stage 1~4 구현 결과 정리.

### 2.1 OK — 오프라인에서 동작

| 동작 | 동작 이유 |
|---|---|
| Console 셸 진입 (URL 직접 접속 / install된 앱 실행) | SW가 HTML/JS/CSS/fonts/icons 자산 precache. |
| 좌측 메뉴 + 라우팅 | TanStack Router 클라이언트 사이드 라우팅 + 자산 precache. |
| 마지막 페이지의 메모리 잔존 데이터 read | react-query 메모리 캐시 (페이지 reload 전까지 유효, persist는 옵션 C 후속). |
| 인증 상태 표시 (로그인 사용자 정보) | Zustand persist → localStorage `rosshield-auth`. |
| 오프라인 banner + UpdatePrompt UI | OfflineIndicator + UpdatePrompt 컴포넌트 (Stage 3). |

### 2.2 차단 — 오프라인에서 차단되는 동작 (Stage 4 mutation 가드)

| 페이지 | 차단되는 mutation 버튼 |
|---|---|
| `/sso` | Provider 추가 / 수정 / 삭제 / 폼 submit |
| `/users` | 초대 생성 / 초대 취소 |
| `/integrations` | Webhook endpoint 추가 / 삭제 / Test 송출 |
| `/robots` | Robot 추가 폼 submit |
| `/scans` | 새 스캔 시작 / Cancel |
| `/compliance` | Profile 추가 / Snapshot 생성 |
| `/findings` | Insight dismiss |

차단 UX:
- 버튼이 `disabled` 상태로 회색 처리.
- 버튼 hover 시 native `title` tooltip으로 **"오프라인 모드에서는 변경 작업을 수행할 수 없습니다"** 표시 (영어: "Offline mode — changes cannot be saved right now").
- 동시에 화면 상단의 **OfflineIndicator banner**가 노출되어 사용자가 상황을 인지 가능.

### 2.3 한계 — 오프라인에서 동작하지 않음

| 동작 | 한계 이유 |
|---|---|
| 신규 GET API fetch (페이지 reload 후) | react-query persist 미구현 (옵션 C 후속). 메모리 캐시는 reload 시 휘발. |
| WebSocket scan progress | SW는 fetch event만 intercept. WebSocket과 직교 — 단절 시 자동 polling fallback도 실패. |
| LLM advisor / explain | 옵트인 + 외부 LLM endpoint 의존. 옵트인 자체가 별 트랙. |
| Audit chain 검증 (offline) | `report verify` CLI 또는 Tauri 데스크톱 별 트랙 권장. |
| 오프라인 mutation queueing 후 자동 재시도 | 의도적 비목표 — audit chain leader epoch + RBAC 정합성 위험. |

---

## 3. UpdatePrompt 동작 + 정책

### 3.1 SW 갱신 prompt 모드 (D-PWA-6)

`vite-plugin-pwa`의 `registerType: 'prompt'` 정책에 따라 **사용자 클릭으로만 reload**
합니다. autoUpdate를 채택하지 않은 이유:
- 감사 입력 폼 작성 중 강제 reload → 입력 데이터 손실 위험.
- mutation 진행 중 reload → 트랜잭션 정합성 위험 (E20 audit chain leader epoch).

### 3.2 사용자가 보는 UX

1. 신규 SW 빌드가 백엔드에 배포되면 사용자 브라우저가 24h 또는 강제 reload 시
   새 SW를 fetch + install + activate 대기 상태로 진입.
2. UpdatePrompt 컴포넌트가 toast로 **"새 버전을 사용할 수 있습니다"** 표시
   + **"새로고침"** 버튼.
3. 사용자가 버튼 클릭 → `updateSW(true)` 호출 → skipWaiting + 페이지 reload.
4. 사용자가 무시하면 다음 진입 시까지 대기 (작업 보호 우선).

### 3.3 운영자 측 갱신 절차

1. 새 release 빌드 → `internal/web/dist/sw.js` + `workbox-*.js` + 자산 hash 갱신.
2. binary 배포 (snap refresh / appliance OTA / manual binary swap).
3. 사용자가 24h 내 자동으로 갱신 prompt 노출, 또는 강제 reload (Ctrl/Cmd+Shift+R).

---

## 4. 트러블슈팅

### 4.1 "새 빌드 배포 후에도 사용자 화면이 stale" 호소

**원인**: SW 캐시가 사용자 브라우저에 잔존 + UpdatePrompt 미클릭.

**해결**:
1. 사용자에게 **Ctrl/Cmd+Shift+R** 강제 reload 안내 → SW가 새 자산 fetch.
2. 또는 사용자에게 toast의 "새로고침" 버튼 클릭 안내.
3. 안 되면 DevTools 절차 (§4.2).

### 4.2 DevTools를 사용한 SW cache 강제 clear

1. Console 탭 열기 (F12).
2. **Application** 패널 → **Service Workers** 섹션.
3. 등록된 SW 목록에서 rosshield SW 확인 → **Unregister** 클릭.
4. 같은 패널 좌측의 **Storage** → **Clear site data** 클릭 → 모든 캐시 + IndexedDB +
   localStorage 초기화.
5. 페이지 reload → 새 SW install + 자산 fetch.

> **주의**: Clear site data는 localStorage `rosshield-auth`도 비우므로 **재로그인
> 필요**합니다. 운영 중 사용자에게는 재로그인 위험을 안내.

### 4.3 "SW 자체가 손상되어 모든 reload가 stale 자산만 표시" — 비상 절차

design doc §9.2 "rollback 비상 절차" — 빈 SW 배포로 사용자 브라우저에서 SW 자동
unregister 유도. 본 절차는 첫 paying customer 직전에만 사용 권장 (downstream
영향 큼).

운영자 측:
1. `web/src/sw-rollback.ts` (별 stage 추가 예정) 빈 SW 배포 또는 `internal/web/dist/sw.js`를
   임시로 unregister-only 코드로 교체:
   ```js
   self.addEventListener('install', () => self.skipWaiting())
   self.addEventListener('activate', (e) => {
     e.waitUntil(
       self.clients.claim().then(() => self.registration.unregister())
     )
   })
   ```
2. binary 재배포 → 사용자 브라우저가 다음 reload 시 SW 자동 제거 → 다음 빌드부터
   정상 SW 재install.

### 4.4 모바일 install이 안 되는 경우

- **iOS**: Safari만 지원. Chrome/Firefox 등 third-party 브라우저는 iOS 시스템 정책상
  PWA install 미지원.
- **Android**: Chrome 외 일부 브라우저(Samsung Internet 등)는 별 UX. 표준은 Chrome.
- HTTPS가 아니거나 manifest.webmanifest가 404이면 install prompt 미노출 →
  운영자 측에서 binary 빌드 산출물 검증 필요.

---

## 5. 한계 (의도적 비목표)

design doc §3.5 + §9.8 정책 발췌:

- **오프라인 mutation queueing 미구현** — 오프라인에서 사용자가 변경 작업 시도 →
  자동 큐잉 + 백엔드 복구 시 재시도 패턴은 audit chain leader epoch 정합성 + RBAC
  정합성 위험으로 비목표. Stage 4의 button-level disabled가 명확한 차단 UX 제공.
- **GET API persist (read 캐시) 미구현** — 옵션 C(react-query persist) trigger:
  첫 paying customer가 "오프라인 read 필수" 명시 또는 어플라이언스 PoC 30일에서
  사용성 이슈 보고 시 별 design doc.
- **Audit chain 검증 (offline)** — `report verify` CLI(E30 별 트랙) 또는 Tauri 데스크톱
  별 트랙 권장. PWA 본 epic 비대상.
- **LLM advisor (offline)** — LLM 자체가 옵트인 + 외부 endpoint 의존. PWA SW와 직교.
- **Push notification** — 외부 push 서비스(Firebase/web-push) 의존, 에어갭 §3 정책
  위반.

---

## 참조

- `docs/design/notes/pwa-offline-design.md` — PWA epic 본문 design doc + D-PWA-1~8
  결정 로그 + Stage 분해.
- `docs/design/01-principles.md` §3 (에어갭 1급) / §10 (프라이버시) / §11 (설명 가능성)
  / §12 (점진적 적용).
- `docs/operations/snap-deployment.md` — 어플라이언스 binary 배포 절차.
- `docs/operations/ha-deployment.md` — 멀티 노드 HA 운영.
- vite-plugin-pwa 공식: <https://vite-pwa-org.netlify.app/>.
- Workbox 7: <https://developer.chrome.com/docs/workbox>.
