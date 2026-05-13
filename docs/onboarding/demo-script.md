# Demo Script — 첫 customer 시연 시나리오

> **대상**: rosshield Phase 5(E38) 첫 paying customer 또는 PoC 파트너 시연.
> **사용 시점**: T+14일 run-through 또는 영업·sales pitch 시연.
> **소요**: 30~45분 (Q&A 포함 60분).
> **버전**: v0.2.0 release(`2026-05-08`) 기준.

---

## 시연 목표

이 시나리오는 다음 5가지 가치를 30분 안에 시각적으로 증명한다:

1. **결정론적 audit chain** — 모든 변경이 해시 체인으로 기록되고, 외부 검증 SDK로 위변조 탐지 가능
2. **multi-tenancy 격리** — 같은 인스턴스에서 두 customer가 서로의 데이터를 절대 볼 수 없음
3. **자동 CIS 변환** — 312 items 중 77.6% 자동 변환된 pack을 즉시 사용
4. **에어갭 동작** — 인터넷 연결 없이도 모든 기능 동작
5. **외부 검증** — 감사인이 별 PC에서 release binary + cosign로 무결성 확인 가능

---

## 사전 준비 (시연 1시간 전)

| 항목 | 상태 확인 |
|---|---|
| `rosshield-server` v0.2.0 binary 다운로드 + cosign 검증 완료 | `cosign verify-blob ...` |
| Demo tenant 2개 시드 (`acme-robotics`, `globex-ros`) | seed 스크립트 실행 |
| Robot 5대 등록 (acme 3대 + globex 2대), 각각 fakesshd 또는 실 SSH 서버 | `/robots` 페이지 확인 |
| CIS Ubuntu 24.04 pack install 완료 | `/packs` 페이지에 "cis-ubuntu-2404" 표시 |
| Grafana 11.x + Prometheus 데몬 가동 + dashboard import | `deploy/grafana/README.md` §4 |
| 시연용 admin user + auditor user 각각 활성 (RBAC 시연용) | `/users` 페이지 |
| 화면 공유 도구 + 마이크 + 카메라 | run-through 5분 전 점검 |

---

## 시나리오 (30분)

### 0. 컨텍스트 설정 (2분)

> "지금부터 rosshield의 30분 시연을 시작합니다. 우리가 증명할 5가지는 결정론적 audit chain, 멀티테넌시, 자동 CIS 변환, 에어갭, 외부 검증입니다. 시연 환경은 v0.2.0 release binary로, 인터넷 연결 없이 동작합니다."

(브라우저 열기 → http://demo.local:8080)

### 1. 첫 admin login + /system overview (3분)

```
1. /login 페이지 → admin@acme-robotics 로그인
2. /system 페이지로 자동 이동
3. 4 카드 설명:
   - Health: server + database + audit chain head 표시
   - HA: single instance 안내 (또는 leader/follower 활성)
   - License: edition + quota (robots/scans/llm tokens)
   - Backups: 최근 5개 + Download 버튼
4. 우측 상단 user menu → Roles: admin
```

> "여기가 운영 dashboard입니다. 4 카드 모두 백엔드 변경 없이 단일 페이지에 통합되어 있어, 시작 직후 운영자가 한곳에서 헬스 체크할 수 있습니다."

### 2. Robot fleet 등록 + drill-down (5분)

```
1. /fleets → "production" fleet 클릭 → drill-down 페이지
2. 소속 robot 3대 표시 (Badge: 로봇 3대)
3. robot "warehouse-bot-01" 클릭 → /robots/<id> 상세 페이지
4. Breadcrumbs: Fleets > production > Robots > warehouse-bot-01
5. 메타 카드 + 진단 이력 카드 (최근 10 sessions)
6. SessionGroup expand/collapse 시연 (localStorage 보존)
   - 세션 헤더: status Badge (cancelled = warning 노랑)
   - failed 시 destructive alert + failureReason expand 토글
7. ResultRow check 클릭 → /packs/cis-ubuntu-2404/checks/<check-id> 진입
8. CheckDetail 페이지: severity / audit cmd / evaluation rule / rationale / fix guidance / selftest fixture
```

> "drill-down 모든 페이지에 Breadcrumbs로 컨텍스트가 유지됩니다. failureReason은 stack trace 같은 긴 텍스트도 'Show more'로 expand해 monospace로 가독성 높게 표시됩니다."

### 3. Credential rotate + SSH fingerprint preview (3분)

```
1. /robots/<id> 상세에서 RotateCredentialCard 펼침 (admin only)
2. authType: privateKey 선택
3. PEM textarea에 ed25519 키 붙여넣기
4. "또는 .pub 직접 입력으로 사전 확인" 토글 → 운영자가 가진 .pub 붙여넣기
5. 두 fingerprint(SHA256:...) 일치 시각 비교
6. 회전 적용 → 새 credential ID 표시
```

> "PEM 입력은 backend ssh.ParsePrivateKey로 fingerprint를 계산하고, .pub 직접 입력은 client-side SubtleCrypto로 즉시 계산합니다 — .pub 평문이 server로 전송되지 않습니다. 두 fingerprint를 운영자가 시각 비교해 잘못된 키 입력을 사전 차단합니다."

### 4. 첫 scan 실행 + live progress (3분)

```
1. /scans → "새 스캔" form
2. fleet: "production" 선택, baseline: "cis-ubuntu-2404" 선택
3. Submit → URL ?session=<id>로 자동 이동
4. SessionProgressCardById live update:
   - WebSocket 연결 (Live Badge)
   - WS 실패 시 polling fallback (Polling Badge, 2s 간격)
   - status / completed / total / percent / duration
5. ~30초 후 completed 전이 → totalDuration "1m 23s" 표시
6. RecentSessionsCard에 새 session 즉시 반영 (5s polling)
```

> "WebSocket으로 실시간 progress를 받고, 연결 실패 시 자동으로 polling으로 fallback합니다. URL의 session 파라미터는 새로고침이나 다른 탭에서도 동일 progress 페이지를 보존합니다."

### 5. Audit chain 검증 (외부 SDK) (4분)

```
1. /system → Backups 카드에서 가장 최근 backup tar.gz Download
2. 별 터미널 (감사인 시뮬레이션):
   $ ./rosshield-audit-verify --bundle backup.tar.gz
   verifying audit chain...
   ✓ chain head matches signed checkpoint
   ✓ all entries hash-linked (12 entries)
   ✓ Ed25519 signature valid (key id: ROSSHIELD_PROD_2025)
3. backup tar.gz를 텍스트 에디터로 열어 1바이트 변조 → 다시 verify
4. 결과:
   ✗ chain head MISMATCH at entry 7
   ✗ verification FAILED
```

> "이게 결정론적 audit의 핵심입니다. 감사인이 release binary와 backup만 받아 별 PC에서 위변조 탐지 가능. SaaS 의존 없음."

### 6. 컴플라이언스 매핑 + framework snapshot (3분)

```
1. /compliance → "Create Profile" → ISMS-P 선택
2. 첫 framework snapshot 생성 (Generate Snapshot 버튼)
3. snapshot 표:
   - 통제 항목별 PASS/FAIL/Manual 비율
   - 미충족 항목 → finding 자동 등록
   - JSON / PDF export
4. /findings 페이지로 이동 → severity 필터 (high/critical)
5. finding 1건 클릭 → 상세 + Dismiss/Accept 버튼 (admin only)
```

> "CIS check 결과가 ISMS-P / ISO27001 / NIST 800-53 통제 항목과 자동 매핑됩니다. 컴플라이언스 보고서를 별도 작성할 필요 없이 스냅샷이 곧 보고서입니다."

### 7. Webhook 결선 시연 (옵션, 3분)

```
1. /integrations → Webhooks → "Create"
2. URL: https://hooks.slack.com/services/TBD (또는 webhook.site로 시연)
3. Event: scan_completed, finding_created
4. Test webhook 클릭 → 즉시 발송 + 응답 표시
5. /scans에서 새 scan 실행 → webhook 자동 발송
6. webhook.site에서 payload 확인 (signature header 포함)
```

> "Webhook은 SIEM·Slack·custom endpoint 어떤 것에든 결선 가능. payload는 X-Rosshield-Signature 헤더로 HMAC-SHA256 서명되어 위변조 탐지 가능합니다."

### 8. 멀티테넌시 격리 시연 (3분)

```
1. logout → globex-ros admin으로 login
2. /robots → globex의 robot 2대만 표시 (acme의 3대는 절대 안 보임)
3. /scans → globex sessions만
4. /system → globex license info만 (acme 사용량 안 보임)
5. URL 직접 입력으로 acme robot id 시도:
   /robots/<acme-warehouse-bot-01-id> → 404 Not Found
6. logout → 다시 acme admin으로 login → 정상 표시
```

> "모든 테이블이 tenant_id로 row-level isolation됩니다. URL 추측이나 API 직접 호출로도 cross-tenant 접근 불가능 — 보안 경계는 단일 middleware가 강제합니다."

### 9. RBAC + audit log 시연 (옵션, 3분)

```
1. /users → "Invite" 클릭
2. role: auditor (read-only) 선택, 이메일 입력
3. invitation token 발급 → 별 브라우저(또는 incognito)에서 accept
4. auditor user 로그인 → /robots 페이지 (read OK)
5. RotateCredentialCard 시도 → button disabled + tooltip "admin only"
6. /audit 페이지 (admin only) → audit log entries 표시:
   - admin@acme rotated credential ...
   - auditor@acme attempted rotate (denied: insufficient role)
```

> "admin/auditor 2-tier RBAC. 모든 mutation은 admin 한정이고, 모든 시도는 audit log에 기록됩니다."

### 10. Grafana operations dashboard (3분)

```
1. 별 탭에서 Grafana → "rosshield-ops" dashboard
2. 5 row 표시:
   - Overview: scan rate / webhook delivery / invitation accept rate
   - Audit chain: head sequence / checkpoint emit rate
   - Performance: API latency p50/p95/p99
   - Resources: memory / CPU / DB connections
   - HA: leader role / leader epoch / failover total (HA 활성 시)
3. 시연 중 발생한 scan event가 ~30초 안에 dashboard에 반영
```

> "rosshield_*_total 메트릭 6/6 + HA 메트릭 3종이 즉시 import 가능한 dashboard로 제공됩니다. customer는 자신의 Prometheus + Grafana로 5분 안에 결선 가능."

### 11. Q&A (남은 시간)

자주 받는 질문 + 답변 미리 준비:

| 질문 | 답변 핵심 |
|---|---|
| 에어갭 환경에서 LLM 어떻게? | 옵트인. 미설정 시 결정론적 fallback만. 에어갭은 LLM 없이도 풀 동작 |
| Postgres vs SQLite? | 단일 인스턴스는 SQLite, HA는 Postgres 필수. 마이그레이션은 driver-aware sqliterepo가 동일 코드베이스로 양 driver 지원 |
| 어플라이언스 OS 무엇? | Ubuntu Core 24 (R40-1 core22 base) + snap strict confined. TPM Secure Boot enrollment 가이드 제공 |
| 첫 customer 비용? | Phase 5 = founder-led, best-effort SLA, community price (정식 enterprise SLA 후속) |
| 위변조 탐지 어떻게? | Ed25519 audit chain + cosign keyless release 서명 + Sigstore Rekor public log 등록 |

---

## 시연 후 자료 송부 (T+0 24시간 내)

```
1. 회의록 (의사결정 + Action Item)
2. 시연에서 사용한 v0.2.0 binary 다운로드 링크
3. quickstart.md PDF 변환본
4. 30일차 마일스톤 합의서 draft
5. 첫 customer info template (yaml) — 채워서 회수
```

---

## 시연 실패 시나리오 + 복구

| 증상 | 즉시 복구 |
|---|---|
| WebSocket 연결 실패 | "polling fallback 동작 중" 안내 → 자연스럽게 다음 단계 |
| audit-verify가 chain mismatch (의도된 변조 아닌데) | backup 새로 받아 재실행 (네트워크 transient 가능성) |
| Grafana datasource 연결 실패 | scrape config 확인 → `prometheus.yml` reload + 30초 대기 |
| robot SSH 연결 실패 | fakesshd로 즉시 fallback (시연 환경 가정) |
| login 자체 실패 | logger.Error 확인 → 환경 변수 reset → 재시도 |

**모든 실패는 정직하게 인정**하고 다음 단계로 넘어가는 것이 신뢰 형성에 유리. "Phase 5 best-effort"임을 명시.

---

## 변경 이력

| 날짜 | 변경 | 작성자 |
|---|---|---|
| 2026-05-13 | 초판 (E38 demo script — `quickstart.md`와 짝, 11 단계 30분 시나리오) | rosshield core team |
