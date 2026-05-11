# Quickstart — 30분 내 첫 admin login + 첫 scan

> **목표**: 처음 접하는 customer가 30분 내에 (1) 다운로드 → (2) 첫 부팅 → (3) admin login → (4) 첫 robot 등록 → (5) 첫 scan → (6) 첫 PDF report 까지 완주.
> **전제**: 인터넷 연결 가능 환경(GitHub release 다운로드용). 에어갭 환경은 부록 A 참고.
> **버전**: rosshield v0.2.0 (2026-05-08 release).

각 섹션 끝에는 **체크포인트** — 진행 상태 확인용 화면·예상 응답·실패 시 대응. 막히면 [README의 Support 채널](./README.md#support-채널-sla-phase-5-best-effort)로 즉시 문의.

---

## 0. 사전 준비 (5분)

### 0.1 시스템 요구사항

| 항목 | 최소 | 권장 |
|---|---|---|
| **CPU 아키텍처** | amd64 또는 arm64 | amd64 권장 (검증 더 두터움) |
| **OS** | Linux (kernel 5.x+) / macOS 12+ / Windows 10+ | Linux (Ubuntu 22.04 LTS) |
| **RAM** | 4 GB | 8 GB |
| **Disk** | 10 GB free | 50 GB (evidence·report 누적) |
| **네트워크** | localhost 8080 + 로봇 SSH 22 | TLS 인증서가 있으면 HTTPS 권장 |
| **추가 도구** | (없음 — 단일 바이너리) | `cosign` v2.x (release 검증용) |

> **에어갭 환경**: 다운로드만 별 머신에서 받아 USB로 이전. 첫 부팅·로그인 흐름은 동일.

### 0.2 다운로드

GitHub release 페이지에서 OS·아키텍처에 맞는 바이너리를 받습니다.

```bash
# Linux amd64 예시 (다른 OS는 파일 이름만 교체)
curl -LO https://github.com/ssabro/rosshield/releases/download/v0.2.0/rosshield-server_v0.2.0_linux_amd64.tar.gz
curl -LO https://github.com/ssabro/rosshield/releases/download/v0.2.0/rosshield-server_v0.2.0_linux_amd64.tar.gz.cert
curl -LO https://github.com/ssabro/rosshield/releases/download/v0.2.0/rosshield-server_v0.2.0_linux_amd64.tar.gz.sig
curl -LO https://github.com/ssabro/rosshield/releases/download/v0.2.0/checksums.sha256
```

### 0.3 검증 (선택이지만 강력 권장)

Sigstore cosign keyless 서명으로 무결성 확인 — 자세한 절차는 repo 루트 [`README.md`의 §"Release 검증"](../../README.md#release-검증-r30-4--e26)를 참조하세요. 핵심 명령:

```bash
# 1) cosign 서명
cosign verify-blob \
  --certificate rosshield-server_v0.2.0_linux_amd64.tar.gz.cert \
  --signature rosshield-server_v0.2.0_linux_amd64.tar.gz.sig \
  --certificate-identity-regexp 'https://github.com/ssabro/rosshield/.github/workflows/release.yml@refs/tags/.*' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  rosshield-server_v0.2.0_linux_amd64.tar.gz

# 2) checksum
sha256sum -c checksums.sha256 --ignore-missing

# 3) 압축 해제
tar -xzf rosshield-server_v0.2.0_linux_amd64.tar.gz
chmod +x rosshield-server
./rosshield-server version
```

**체크포인트 0**:
- [ ] `cosign verify-blob` → `Verified OK`
- [ ] `sha256sum -c` → `OK`
- [ ] `./rosshield-server version` → `rosshield-server vX.Y.Z (commit=... built=... go=...)` 한 줄 출력
- [ ] 실패 시 → release 페이지 다시 다운로드 또는 SHA 불일치면 사고 신고(P0)

> **(스크린샷 placeholder — `screenshots/00-version-output.png`)** — `version` 명령 정상 출력.

---

## 1. 첫 부팅 + admin 시드 (5분)

### 1.1 admin 계정 시드

첫 부팅 전에 admin 계정을 시드합니다. 한 명령으로 tenant + admin user + 시스템 역할(admin/auditor/operator) 모두 생성됩니다.

```bash
# 작업 디렉터리 생성
mkdir -p ./data
cd ./data
mkdir -p var keys evidence

# 패스워드는 stdin으로 전달 — 명령행 노출 방지
echo 'StrongPassword!2026' | ../rosshield-server seed admin \
  --email admin@acme.test \
  --password-stdin \
  --display-name "Acme Admin" \
  --data-dir ./var
```

stdout은 JSON 한 줄:

```json
{"tenantId":"01HV...","tenantName":"default","userId":"01HV...","email":"admin@acme.test","seededAt":"2026-05-11T..."}
```

### 1.2 서버 부팅

```bash
../rosshield-server --addr 127.0.0.1:8080 --data-dir ./var
```

기본은 SQLite + localhost bind. PostgreSQL을 사용하려면(intake에서 선택한 경우):

```bash
../rosshield-server \
  --addr 127.0.0.1:8080 \
  --data-dir ./var \
  --storage postgres \
  --storage-dsn "postgres://rosshield:secret@db.internal:5432/rosshield?sslmode=disable"
```

License token이 있다면(enterprise/pro):

```bash
ROSSHIELD_LICENSE_TOKEN='eyJ...' ../rosshield-server --addr 127.0.0.1:8080 --data-dir ./var
```

### 1.3 health check

다른 터미널에서:

```bash
curl http://127.0.0.1:8080/healthz
# → {"status":"ok"}
```

### 1.4 첫 로그인

브라우저에서 `http://127.0.0.1:8080`을 엽니다. 로그인 페이지가 나타납니다.

- **이메일**: `admin@acme.test` (위 시드와 동일)
- **패스워드**: `StrongPassword!2026`

로그인 성공 시 `/overview` 대시보드(robots·insights·profiles·latest score 4 카드)로 자동 이동.

**체크포인트 1**:
- [ ] `seed admin` → JSON 한 줄 출력 + exit 0
- [ ] `curl /healthz` → `{"status":"ok"}`
- [ ] 브라우저 로그인 성공 → `/overview` 4 카드 표시
- [ ] Header 우상단에 `Acme Admin` 표시
- [ ] 좌측 Sidebar에 9개 메뉴(overview·robots·scans·findings·compliance·advisor·integrations·audit·settings) 표시

> **(스크린샷 placeholder — `screenshots/01-overview-empty.png`)** — 빈 대시보드(robots=0, insights=0).

**실패 시**:
- 시드 중 `duplicate seed` (exit 3) → 이미 시드된 디렉터리. `--data-dir`을 새 디렉터리로 변경하거나 기존 admin으로 로그인.
- 로그인 실패 (`이메일 또는 패스워드가 올바르지 않습니다`) → 시드 시 사용한 정확한 값 재확인.
- 서버 부팅 시 `bind: address already in use` → `--addr`을 다른 포트로 변경.

---

## 2. 첫 robot 등록 (10분)

### 2.1 등록 폼 진입

좌측 Sidebar에서 **Robots** 클릭 → `/robots` 페이지 → 우상단 **등록** 버튼 클릭 → 모달 또는 폼 표출.

### 2.2 자격증명 정보 입력

| 필드 | 설명 | 예시 |
|---|---|---|
| **Robot 이름** | UI 표시용 별칭 | `arm-01-warehouse-A` |
| **Fleet** | 그룹핑(없으면 default fleet 자동 선택) | `warehouse-fleet` |
| **호스트/IP** | SSH 접근 가능 주소 | `10.0.1.42` 또는 `robot01.lan` |
| **SSH 포트** | 기본 22 | `22` |
| **사용자명** | SSH 계정 | `ros` 또는 `ubuntu` |
| **인증 방식** | password 또는 SSH key | (선택) |
| **자격증명 값** | password 문자열 또는 private key PEM | (입력) |

> **보안 설명** — 입력한 자격증명은 즉시 **KEK→DEK envelope 암호화**(per-tenant DEK + master KEK)로 wrap되어 저장되며, **평문은 영속 저장소에 절대 기록되지 않습니다**. 메모리 내 평문은 SSH 연결 사용 시점에만 일시적으로 unwrap됩니다. 외부 audit이 자격증명 평문을 추출할 수 있는 경로는 없습니다(R10-7 키 분리 + 키 회전 절차 §13).

> **인증 방식 권장** — production은 **SSH key** 방식. password는 PoC·데스크톱 환경에서만 권장.

### 2.3 저장

**저장** 버튼 클릭 → 통합 onboarding diagnostic이 즉시 SSH 시범 연결을 시도하고 OS·ROS2·Docker 버전을 수집합니다(`02 §2.5 시나리오 B`). 결과:

- **성공** → robot이 `/robots` 테이블에 한 행 추가, 우측 상태 컬럼 `online (Ubuntu 22.04, ROS2 humble)` 등.
- **실패** → 에러 토스트 + robot 행은 `unreachable` 상태로 등록(자격증명·네트워크 수정 후 재시도).

**체크포인트 2**:
- [ ] `/robots` 테이블에 1행 등록
- [ ] OS·ROS2 버전 자동 감지 표시
- [ ] 자격증명을 다시 보려고 해도 평문 표시 없음(마스킹) → KEK wrap 정상

> **(스크린샷 placeholder — `screenshots/02-robot-registered.png`)** — 1대 등록된 robot 테이블.

**실패 시**:
- `connection refused` → SSH 데몬·방화벽 확인.
- `permission denied (publickey)` → SSH key 권한·user 매칭 확인.
- `unknown host` → DNS·`/etc/hosts` 확인.

---

## 3. 첫 scan 실행 (5분)

### 3.1 스캔 폼 진입

좌측 Sidebar **Scans** → `/scans` → **새 스캔** 버튼.

### 3.2 스캔 파라미터 선택

| 필드 | 설명 | 첫 시연 권장값 |
|---|---|---|
| **Fleet** | 스캔 대상 fleet | (위에서 등록한 fleet) |
| **Robot 선택** | 단일 robot 또는 fleet 전체 | (위에서 등록한 robot 1대) |
| **Pack 선택** | 적용할 벤치마크 팩 | `cis-ubuntu-22.04` (또는 `ros2-humble-baseline`) |
| **Trigger** | manual / scheduled | `manual` |

> **번들된 팩** — v0.2.0은 CIS Ubuntu 22.04 + ROS2 humble baseline + 내부 demo seed pack을 기본 번들합니다. 추가 팩(ISMS-P 컨텍스트 매핑 등)은 Phase 5 pack mirror에서.

### 3.3 실행

**실행** 버튼 클릭 → scan session ID가 생성되고 진행 상태가 **WebSocket live progress**로 실시간 표시됩니다(`/api/v1/scans/{id}/progress` 구독, C1 carryover).

```
[12/47] Running: ensure_no_world_writable_files ........... PASS
[13/47] Running: rsyslog_logging_configured ............... PASS
[14/47] Running: ros2_discovery_server_authentication ..... FAIL
...
[47/47] Done. PASS=39  FAIL=5  WARN=2  N/A=1
```

진행 막대 + 카운터가 실시간 갱신. 보통 robot 1대·47 check 기준 30초~2분 소요.

### 3.4 완료 후 결과

스캔이 끝나면 결과 카드가 표출:

- **Overall score**: `0.83` (PASS / total)
- **Pass / Fail / Warn / N/A** breakdown
- **High severity finding 수**: 예 `2건` → `/findings`에서 상세 확인 가능

**체크포인트 3**:
- [ ] WebSocket progress 정상 동작 (live 카운터 갱신)
- [ ] scan session 완료 → 결과 카드 표시
- [ ] FAIL이 1건 이상 발생 (대부분의 robot은 첫 스캔에서 FAIL이 나옵니다 — 정상)

> **(스크린샷 placeholder — `screenshots/03-scan-progress.png`)** — live progress 진행 중.
> **(스크린샷 placeholder — `screenshots/04-scan-result.png`)** — 완료 후 결과 카드.

**실패 시**:
- WebSocket 연결 실패 → 프록시·방화벽이 WS 업그레이드를 차단하는지 확인. polling fallback도 동작.
- 모든 check가 N/A → robot OS·ROS2 버전이 pack과 안 맞음. 다른 pack 선택.

---

## 4. 첫 finding 처리 + 첫 PDF report 발행 (5분)

### 4.1 Findings 페이지

좌측 Sidebar **Findings** → `/findings` → 위 scan에서 나온 finding 목록 표출.

각 finding에 대해:
- **상세 보기** — check ID·증거(stdout/stderr)·remediation 가이드
- **Dismiss** — 사유 입력 후 dismiss(audit log에 기록됨)
- **Severity 필터** — High / Medium / Low

> **결정론성** — finding은 동일 scan을 다시 돌리면 정확히 같은 결과가 나옵니다(rule-based evaluation, P1 결정성 원칙). LLM advisor가 활성이면 자연어 설명이 추가되지만, 판정 자체는 rule이 결정.

### 4.2 Report 생성

`/findings` → 우상단 **Report 생성** 버튼 (또는 `/scans` 결과 카드의 **Report 생성** 버튼) → 형식 선택:

- **Session report (PDF)** — 단일 scan의 상세 리포트. 페이지1 메타+요약 카드, 페이지2~N check rows, footer audit anchor + sha256.
- **Framework report (PDF)** — 컴플라이언스 프레임워크 단위 리포트(2주차 활성화 후).

**Session report 생성**을 선택 → 30초 내외에 PDF 생성 + 다운로드 버튼 활성.

### 4.3 다운로드 + 검증

```bash
# 다운로드된 bundle (예: scan-01HV....tar.gz)
tar -tzf scan-01HV....tar.gz
# → report.pdf · evidence/* · manifest.json · signature.sig · checkpoint.json
```

bundle 무결성을 외부 감사인 도구(E30, standalone)로 검증:

```bash
./rosshield-audit-verify --bundle scan-01HV....tar.gz
# → bundle: OK
#   manifest sha256: <hex>
#   signature: VERIFIED (key=tenant:01HV...)
#   audit chain anchor: VERIFIED (head=<hex>, generated=2026-05-11T...)
```

`rosshield-audit-verify`는 v0.2.0 release 자산에 포함된 별도 250 LoC 단일 바이너리 — **rosshield-server 없이도** 외부 감사인이 독립 검증 가능(R30-4).

**체크포인트 4**:
- [ ] `/findings`에 finding 1건 이상 표시
- [ ] PDF 다운로드 성공
- [ ] PDF가 올바르게 열림 (페이지1 메타 + 카드 / 페이지N에 footer audit anchor)
- [ ] `rosshield-audit-verify` → `bundle: OK` + `signature: VERIFIED`

> **(스크린샷 placeholder — `screenshots/05-findings-list.png`)** — finding 목록 테이블.
> **(스크린샷 placeholder — `screenshots/06-pdf-page1.png`)** — PDF 페이지1(점수 카드 + 메타).
> **(스크린샷 placeholder — `screenshots/07-audit-verify-output.png`)** — verify 도구 stdout.

---

## 5. 다음 단계 (1주차 잔여)

축하합니다 — 첫 사이클 완료. 1주차 내 권장 다음 작업:

1. **추가 robot 등록** — fleet 전체로 확장(`/robots` → 등록 반복).
2. **정기 스캔 스케줄 등록** — `/scans` → schedule 탭(weekly 권장 시작값).
3. **사용자 초대** — `/users` 또는 `/invitations` → 추가 admin·auditor·operator 초대.
4. **SSO 결선** (선택) — `/sso` → OIDC 또는 SAML provider 등록 → `/users` 자동 프로비저닝 활성.
5. **컴플라이언스 프레임워크** — `/compliance` → ISMS-P 또는 ISO27001 활성 → 첫 framework snapshot 생성.
6. **Webhook 결선** (선택) — `/integrations` → SIEM·Slack·webhook endpoint 등록 → scan completion 자동 통보.

각 항목 step-by-step은 [`README.md`의 2주차 섹션](./README.md#2주차-t8--t14일--운영-안착) 참조.

---

## 부록 A: 에어갭 환경

인터넷 연결이 없는 환경:

1. **다운로드** — 별 머신(인터넷 가능)에서 release tarball + cosign cert/sig + checksums + LICENSE 다운로드 → USB로 이전.
2. **검증** — 같은 머신(에어갭)에서도 cosign verify-blob 가능 (Rekor public log 검증을 skip하려면 `--insecure-ignore-tlog` 추가, 단 신뢰도 감소).
3. **시드 + 부팅** — §1과 동일.
4. **pack 업데이트** — Phase 5 후반의 pack mirror offline bundle 사용(USB 서명 번들 → 내부 mirror, §02 §2.7).
5. **LLM** — cloud provider 강제 off, Ollama local 모델만 사용 가능(`--llm-provider=ollama` + 로컬 endpoint).
6. **Telemetry** — 강제 off (default).

---

## 부록 B: 트러블슈팅 빠른 표

| 증상 | 원인 후보 | 대응 |
|---|---|---|
| `bind: address already in use` | 포트 점유 | `--addr` 변경 또는 점유 프로세스 종료 |
| `seed admin` exit 3 | 이미 시드됨 | 새 `--data-dir` 또는 기존 admin 사용 |
| 로그인 401 | 패스워드 오타·시드 안 됨 | 시드 다시 (다른 디렉터리) |
| robot `unreachable` | SSH·방화벽·자격증명 | 명령행에서 `ssh user@host` 직접 시도해 검증 |
| WS progress 미동작 | 프록시 WS upgrade 차단 | 직접 접속 또는 polling fallback 사용 |
| PDF 다운로드 실패 | reporting 도메인 미초기화 | 서버 로그(stderr) → `reporting` 패키지 에러 확인 |
| `rosshield-audit-verify` `signature INVALID` | bundle 손상 또는 키 변경 | bundle 재생성 또는 키 fingerprint 비교 |
| `/healthz` 응답 없음 | 서버 부팅 실패 | stderr 로그 확인 — 마이그레이션 실패·DSN 오타 빈도 높음 |

**그 외 막힐 때**: 서버 로그 마지막 200줄 + 재현 절차를 [README의 Support 채널](./README.md#support-채널-sla-phase-5-best-effort)로 송부.

---

## 변경 이력

| 날짜 | 변경 | 작성자 |
|---|---|---|
| 2026-05-11 | 초판 (E38 사전 준비, v0.2.0 기준) | rosshield core team |
