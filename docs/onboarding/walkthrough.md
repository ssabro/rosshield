# PoC walkthrough — 단계별 실행 시퀀스 + 검증

> **대상**: 첫 paying customer 또는 PoC 파트너의 운영자(rosshield team) + customer 측 관리자.
> **사용 시점**: T+0 kickoff부터 T+7일 1주차 종료까지 동행 가이드. `quickstart.md`(30분 단발 흐름)와 짝을 이루는 **단계별 PoC 진행 시퀀스**입니다.
> **차별점**: `quickstart.md`는 "30분 안에 첫 cycle 완주" 단발 흐름, 본 문서는 "단계별 명령 → 예상 결과 → 검증 → 트러블슈팅" 시퀀스로 **부분 실패 시 진단 가이드** 포함.
> **버전**: rosshield v0.2.0 (`2026-05-08` release) 기준.
> **참조**: design doc `docs/design/notes/customer-onboarding-design.md` §3.2 영역 2 + §6.2 R2 + D-CUSTONB-3 권장 default(bash + e2e CI 통합).

---

## Pre-flight checklist (kickoff 24시간 전)

운영자 + customer 양측이 kickoff *전*에 다음을 모두 OK 표시한 뒤 진행합니다. 한 항목이라도 미충족이면 kickoff 일정 조정.

### customer 환경

- [ ] **OS** — Linux(Ubuntu 22.04 LTS 권장) 또는 macOS 12+ 또는 Windows 10+ (Windows는 부록 W 별 노트 참고).
- [ ] **CPU 아키텍처** — amd64 또는 arm64.
- [ ] **RAM ≥ 4 GB** (권장 8 GB), **Disk ≥ 10 GB free** (권장 50 GB — evidence·report 누적).
- [ ] **네트워크** — `localhost:8080` 가용 + 등록할 robot의 SSH 22 포트 reach 가능. TLS reverse proxy 사용 시 별도 합의.
- [ ] **외부 도구** — `curl`, `tar`, `sha256sum` 또는 `shasum -a 256` (Linux/macOS 기본 포함). `cosign` v2.x 권장(release 검증용 — 미설치 시 sha256만으로도 진행 가능, 보안 등급은 감소).
- [ ] **에어갭 환경 여부 명시** — intake yaml `network.airgap` 값과 일치 (true 시 부록 A 참고).

### customer 자격증명

- [ ] **첫 robot 1대 SSH 접근** — 호스트/IP + SSH 사용자명 + 인증 방식(password 또는 private key PEM) 사전 합의. PoC 단계는 **단일 robot 1대**부터 시작 권장.
- [ ] **admin 이메일·표시명** — intake yaml `customer.contact_admin.{email,name}`와 일치.
- [ ] **admin 패스워드 정책** — 최소 12자 + 영숫자·특수문자 포함. 본 walkthrough는 customer가 직접 설정.

### rosshield team 측

- [ ] **license token 발급 완료** (intake yaml의 `license.edition`·`expected_quota`에 맞춰 Ed25519 서명) + secure 채널로 전달 준비. 평문 token은 본 walkthrough 안에 기재 X.
- [ ] **invite token 또는 admin seed 절차 확정** — 본 PoC는 R1 intake API 마감 *전*이라 운영자가 `seed admin` 명령으로 직접 시드 (R1 마감 후엔 intake API로 자동화).
- [ ] **연락 채널** — `README.md` §"Support 채널 SLA" + §연락처 customer 측 1차 contact 사전 공유.

**Pre-flight 검증 명령** (customer 측 단일 명령):

```bash
# Linux/macOS
uname -sm && free -h 2>/dev/null | head -2 && df -h / | tail -1 && \
  command -v curl tar sha256sum cosign 2>&1 | sed 's/^/found: /' || echo "missing tool above"
```

**예상 결과** (Ubuntu 22.04 amd64 예시):

```
Linux x86_64
              total        used        free      shared  buff/cache   available
Mem:           7.7Gi       1.2Gi       3.0Gi        ...
/dev/sda1     50G   12G   36G  25%  /
found: /usr/bin/curl
found: /bin/tar
found: /usr/bin/sha256sum
found: /usr/local/bin/cosign
```

`cosign`이 missing이면 권장 설치(Linux):

```bash
curl -sLO https://github.com/sigstore/cosign/releases/download/v2.4.1/cosign-linux-amd64
sudo install cosign-linux-amd64 /usr/local/bin/cosign
cosign version
```

---

## 단계 0: 사전 준비 — rosshield 다운로드 + 검증 (5분)

### 명령

```bash
# 작업 디렉터리 생성 (customer 측 host)
mkdir -p ~/rosshield-poc && cd ~/rosshield-poc

# v0.2.0 release 자산 4종 다운로드 (Linux amd64 예시 — 다른 OS는 파일 이름만 교체)
VERSION=v0.2.0
ARCH=linux_amd64
BASE=https://github.com/ssabro/rosshield/releases/download/${VERSION}

curl -LO ${BASE}/rosshield-server_${VERSION}_${ARCH}.tar.gz
curl -LO ${BASE}/rosshield-server_${VERSION}_${ARCH}.tar.gz.cert
curl -LO ${BASE}/rosshield-server_${VERSION}_${ARCH}.tar.gz.sig
curl -LO ${BASE}/checksums.sha256

# checksum 검증 (필수)
sha256sum -c checksums.sha256 --ignore-missing

# cosign keyless 검증 (강력 권장)
cosign verify-blob \
  --certificate rosshield-server_${VERSION}_${ARCH}.tar.gz.cert \
  --signature rosshield-server_${VERSION}_${ARCH}.tar.gz.sig \
  --certificate-identity-regexp 'https://github.com/ssabro/rosshield/.github/workflows/release.yml@refs/tags/.*' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  rosshield-server_${VERSION}_${ARCH}.tar.gz

# 압축 해제 + 실행 권한
tar -xzf rosshield-server_${VERSION}_${ARCH}.tar.gz
chmod +x rosshield-server rosshield-audit-verify
./rosshield-server version
```

### 예상 결과

```
rosshield-server_v0.2.0_linux_amd64.tar.gz: OK
Verified OK
rosshield-server v0.2.0 (commit=abcdef0 built=2026-05-08T... go=go1.22.x)
```

### 검증 방법

- **성공 판정**: `rosshield-server version` 한 줄이 `vX.Y.Z` + `commit=` + `built=` + `go=` 4 필드 출력. exit code 0.
- **다음 단계 진입 조건**: `cosign verify-blob`이 `Verified OK` 1줄 출력 + `sha256sum -c`가 `OK` 출력.

### 트러블슈팅 링크

- `cosign verify-blob` 실패 또는 `sha256sum: ... FAILED` → §트러블슈팅 T1.
- `version` 명령이 권한 거부 (Windows에서 추출한 binary 등) → 부록 W 참고 + `chmod +x rosshield-server rosshield-audit-verify` 적용 후 재시도.

---

## 단계 1: 첫 admin 시드 + 부팅 + 첫 로그인 (5분)

### 명령

```bash
# 작업 디렉터리 안에서
mkdir -p ./var ./evidence

# admin 시드 (패스워드는 stdin으로 전달 — 명령행 노출 방지)
echo 'StrongPassword!2026' | ./rosshield-server seed admin \
  --email admin@acme.test \
  --password-stdin \
  --display-name "Acme Admin" \
  --data-dir ./var

# 서버 부팅 (foreground — 별 터미널에서 실행, 또는 nohup)
./rosshield-server --addr 127.0.0.1:8080 --data-dir ./var
```

License token이 있다면(enterprise/pro):

```bash
ROSSHIELD_LICENSE_TOKEN='<rosshield team이 secure 채널로 전달한 token>' \
  ./rosshield-server --addr 127.0.0.1:8080 --data-dir ./var
```

다른 터미널에서 health check:

```bash
curl http://127.0.0.1:8080/healthz
```

브라우저에서 `http://127.0.0.1:8080` 진입 → 이메일·패스워드(위 시드 값) 입력.

### 예상 결과

`seed admin` stdout:

```json
{"tenantId":"01HV...","tenantName":"default","userId":"01HV...","email":"admin@acme.test","seededAt":"2026-05-11T..."}
```

`curl /healthz`:

```json
{"status":"ok"}
```

브라우저 로그인 성공 → `/overview` 4 카드 대시보드(robots=0, insights=0, profiles=0, latest score=N/A).

`<screenshot: docs/onboarding/screenshots/01-overview-empty.png — 빈 /overview 대시보드 4 카드>`

### 검증 방법

- **성공 판정**: `seed admin` exit 0 + JSON 한 줄 + `/healthz` `ok` + 브라우저 로그인 성공 + 우상단 `Acme Admin` 표시 + 좌측 9개 메뉴(overview·robots·scans·findings·compliance·advisor·integrations·audit·settings).
- **다음 단계 진입 조건**: `/overview` 4 카드 표시 + Sidebar 9 메뉴 모두 클릭 가능.

### 트러블슈팅 링크

- `seed admin` exit 3 (`duplicate seed`) → §트러블슈팅 T2.
- 서버 부팅 시 `bind: address already in use` → §트러블슈팅 T3.
- 브라우저 로그인 실패(`이메일 또는 패스워드가 올바르지 않습니다`) → §트러블슈팅 T4.
- `curl /healthz` 무응답 또는 connection refused → §트러블슈팅 T5.

---

## 단계 2: customer info 입력 + tenant 메타 확인 (5분)

> **R1 intake API 마감 전**: 본 단계는 운영자가 customer 측에서 회수한 yaml(`customer-info-template.md`)을 참고해 **수동 점검**합니다. R1 마감 후엔 `POST /api/v1/customers/intake` 자동화로 대체.

### 명령

운영자가 customer 측에서 회수한 intake yaml을 사전 검토:

```bash
# 운영자 측 host (customer 측 host 아님)
yq eval '.customer.organization, .deployment.sku, .license.edition, .network.airgap' acme-intake.yaml
```

또는 yq 미설치 시 텍스트로 확인:

```bash
grep -E '^(customer|deployment|license|network):' -A 5 acme-intake.yaml
```

customer 측 `/settings` 페이지(좌측 Sidebar 최하단) 진입 → tenant 표시명·timezone 확인.

### 예상 결과

`yq` 출력:

```
Acme Robotics
onprem
pro
false
```

`/settings` 페이지: tenant 이름·timezone·기본 storage(sqlite/postgres) 표시.

`<screenshot: docs/onboarding/screenshots/02-settings-tenant.png — /settings tenant 메타>`

### 검증 방법

- **성공 판정**: intake yaml의 `customer.organization`·`deployment.sku`·`license.edition`·`network.airgap` 4 필수 필드가 모두 비어 있지 않음(`TBD` 포함). UI `/settings`에서 timezone이 `customer.contact_admin.timezone`과 일치.
- **다음 단계 진입 조건**: SKU·storage·license edition 합의 완료. 누락 항목은 메일/Slack로 즉시 회수.

### 트러블슈팅 링크

- yaml 파싱 에러 → 운영자 측 yaml lint 사전 확인 (`yamllint acme-intake.yaml` — indentation 탭/스페이스 혼용·따옴표 누락이 흔함).
- `network.airgap=true` && `network.smtp.enabled=true` 같은 모순 → customer에 재확인. R1 intake API 마감 후엔 422 응답으로 자동 검출.

---

## 단계 3: 첫 robot 등록 + connection test (10분)

### 명령

좌측 Sidebar **Robots** 클릭 → `/robots` 페이지 → 우상단 **등록** 버튼.

폼 필드 입력 (intake yaml에서 합의한 첫 robot 1대):

| 필드 | 예시값 |
|---|---|
| Robot 이름 | `arm-01-warehouse-A` |
| Fleet | `warehouse-fleet` (없으면 default fleet 자동 선택) |
| 호스트/IP | `10.0.1.42` 또는 `robot01.lan` |
| SSH 포트 | `22` |
| 사용자명 | `ros` 또는 `ubuntu` |
| 인증 방식 | **SSH key** (production 권장) 또는 password (PoC 한정) |
| 자격증명 값 | private key PEM 붙여넣기 또는 password 입력 |

**저장** 버튼 클릭 → 통합 onboarding diagnostic이 즉시 SSH 시범 연결 + OS·ROS2·Docker 버전 수집.

사전 SSH 단독 검증(권장):

```bash
# customer 측 host에서 수동으로 SSH 연결 가능한지 사전 확인
ssh -p 22 ros@10.0.1.42 'uname -a && lsb_release -d 2>/dev/null'
```

### 예상 결과

`ssh ... uname -a` 출력:

```
Linux robot01 5.15.0-... #... SMP ... x86_64 GNU/Linux
Description:    Ubuntu 22.04.4 LTS
```

`/robots` 테이블에 1행 등록:

| 이름 | Fleet | OS | ROS2 | 상태 |
|---|---|---|---|---|
| arm-01-warehouse-A | warehouse-fleet | Ubuntu 22.04 | humble | online |

`<screenshot: docs/onboarding/screenshots/03-robot-registered.png — 1대 등록된 /robots 테이블>`

### 검증 방법

- **성공 판정**: `/robots` 테이블에 1행 추가 + 상태 컬럼 `online` + OS/ROS2 버전 자동 감지 표시. 자격증명 다시 보기 시도 → 평문 표시 없음(마스킹).
- **다음 단계 진입 조건**: 상태 `online`. `unreachable`이면 §트러블슈팅 T6/T7로 진단 후 재등록.

### 트러블슈팅 링크

- SSH `connection refused` 또는 `unknown host` → §트러블슈팅 T6.
- SSH `permission denied (publickey)` → §트러블슈팅 T7.

---

## 단계 4: 첫 scan 실행 + progress 모니터링 (5분)

### 명령

좌측 Sidebar **Scans** → `/scans` → **새 스캔** 버튼.

폼 입력:

| 필드 | 첫 시연 권장값 |
|---|---|
| Fleet | (단계 3에서 등록한 fleet) |
| Robot 선택 | (단계 3에서 등록한 robot 1대) |
| Pack 선택 | `cis-ubuntu-22.04` (또는 `ros2-humble-baseline`) |
| Trigger | `manual` |

**실행** 버튼 → scan session ID 생성 + URL `?session=<id>` 자동 이동 + WebSocket live progress 표출.

CLI에서 별도 모니터링(옵션):

```bash
# 운영자 측 — scan 진행 상태 polling (커뮤니티 인증 토큰 사용)
curl -s -H "Authorization: Bearer <admin token>" \
  http://127.0.0.1:8080/api/v1/scans/<session-id> | yq -P
```

### 예상 결과

UI live progress:

```
[12/47] Running: ensure_no_world_writable_files ........... PASS
[13/47] Running: rsyslog_logging_configured ............... PASS
[14/47] Running: ros2_discovery_server_authentication ..... FAIL
...
[47/47] Done. PASS=39  FAIL=5  WARN=2  N/A=1
```

완료 후 결과 카드:

- **Overall score**: `0.83` (PASS / total)
- **Pass / Fail / Warn / N/A**: `39 / 5 / 2 / 1`
- **High severity finding 수**: 예 `2건` → `/findings`에서 상세

`<screenshot: docs/onboarding/screenshots/04-scan-progress.png — live progress 진행 중>`
`<screenshot: docs/onboarding/screenshots/05-scan-result.png — 완료 결과 카드>`

소요: robot 1대·47 check 기준 약 30초~2분.

### 검증 방법

- **성공 판정**: WebSocket Live Badge 표출 → progress 카운터 실시간 갱신 → 완료 시 결과 카드 + Pass/Fail/Warn/N/A 4 카운터 + Overall score. FAIL이 1건 이상 발생(첫 스캔에서 FAIL 0은 오히려 의심 — pack/robot 매칭 확인).
- **다음 단계 진입 조건**: scan session 상태 `completed` (UI 또는 `/api/v1/scans/<id>` JSON `status`).

### 트러블슈팅 링크

- WebSocket 연결 실패 (Polling Badge로 자동 fallback이지만 둘 다 실패 시) → §트러블슈팅 T8.
- 모든 check가 N/A → §트러블슈팅 T9.
- scan이 시작 직후 `failed` 상태 → §트러블슈팅 T10.

---

## 단계 5: 첫 PDF report 생성 + 외부 검증 (5분)

### 명령

`/findings` 페이지 → 우상단 **Report 생성** 버튼 (또는 `/scans` 결과 카드에서 동일).

형식 선택: **Session report (PDF)** → 30초 내외 PDF 생성 + 다운로드 버튼 활성.

다운로드된 bundle 검증:

```bash
# 다운로드 폴더에서
ls -la scan-01HV*.tar.gz
tar -tzf scan-01HV*.tar.gz

# 외부 검증 (감사인 시뮬레이션 — rosshield-server 없이 standalone 동작)
./rosshield-audit-verify --bundle scan-01HV*.tar.gz
```

### 예상 결과

`tar -tzf` 출력:

```
report.pdf
evidence/check-001.json
evidence/check-002.json
...
manifest.json
signature.sig
checkpoint.json
```

`rosshield-audit-verify` 출력:

```
bundle: OK
manifest sha256: 3a4f2e1b8c9d...
signature: VERIFIED (key=tenant:01HV...)
audit chain anchor: VERIFIED (head=7f8e6d5c..., generated=2026-05-11T...)
```

`<screenshot: docs/onboarding/screenshots/06-pdf-page1.png — PDF 페이지1: 점수 카드 + 메타>`
`<screenshot: docs/onboarding/screenshots/07-audit-verify-output.png — verify 도구 stdout>`

### 검증 방법

- **성공 판정**: bundle tar.gz가 `report.pdf`·`manifest.json`·`signature.sig`·`checkpoint.json`·`evidence/` 디렉터리 5종 모두 포함. `rosshield-audit-verify`가 4 줄 모두 `OK`/`VERIFIED` 출력. exit 0.
- **다음 단계 진입 조건**: `signature: VERIFIED` + `audit chain anchor: VERIFIED` 둘 다 PASS.

### 트러블슈팅 링크

- PDF 다운로드 실패 → 서버 stderr 로그 `tail -100 server.log | grep -i 'reporting\|pdf'` + 디스크 공간 확인 (`df -h ./var`). reporting 도메인 미초기화 또는 한글 폰트 누락 가능 — 후자는 PDF 자체는 생성 가능, 한글 일부 누락만 발생.
- `rosshield-audit-verify` `signature INVALID` 또는 `chain MISMATCH` → §트러블슈팅 T11.

---

## 단계 6: license 사용량 확인 + /system 페이지 점검 (3분)

### 명령

좌측 Sidebar **Settings** 또는 직접 URL — `/system` 페이지 진입.

`/license` 페이지에서 quota 사용률 확인:

```
브라우저 주소창: http://127.0.0.1:8080/license
```

CLI에서 동일 정보 확인(옵션):

```bash
# admin 토큰 필요
curl -s -H "Authorization: Bearer <admin token>" \
  http://127.0.0.1:8080/api/v1/license | yq -P
```

### 예상 결과

`/system` 4 카드:

- **Health**: server + database + audit chain head 표시 (`head=7f8e6d5c...`).
- **HA**: single instance 안내 (또는 leader/follower 활성).
- **License**: edition + quota (robots used/limit, scans_per_day used/limit, llm tokens used/limit).
- **Backups**: 최근 5개 + Download 버튼.

`/license` 상세:

```
Edition: pro
Status: ACTIVE (만료 2027-05-11, 365일 남음)
Quota:
  robots:           1 / 50    (2%)
  scans_per_day:    1 / 100   (1%)
  llm_tokens_per_day: 0 / 0   (LLM 미사용)
```

`<screenshot: docs/onboarding/screenshots/08-system-overview.png — /system 4 카드>`
`<screenshot: docs/onboarding/screenshots/09-license-detail.png — /license 상세>`

### 검증 방법

- **성공 판정**: License `Status: ACTIVE` + 만료일이 미래 + quota 사용률 < 80%. audit chain head가 단계 5 verify 결과와 일치(`head=7f8e6d5c...`).
- **다음 단계 진입 조건**: License ACTIVE + audit chain head 일치 → 1주차 baseline 완료.

### 트러블슈팅 링크

- License `Status: GRACE` 또는 `EXPIRED` → §트러블슈팅 T12.
- audit chain head가 verify 출력과 불일치 → §트러블슈팅 T11 (단계 5와 동일 진단).

---

## 트러블슈팅 시나리오 (12개)

흔한 에러 우선. 각 시나리오는 **증상 → 원인 → 대응** 3단계.

### T1. cosign verify-blob 실패 또는 sha256sum FAILED

- **증상**: `Error: error verifying blob ... no matching signatures` 또는 `rosshield-server_vX.Y.Z_..._.tar.gz: FAILED`.
- **원인 후보**: cosign 버전 < 2.0 / 인증서 OIDC issuer mismatch / Rekor public log unreachable / 다운로드 중단.
- **대응**:
  ```bash
  cosign version           # v2.0.0+ 확인. 미만이면 업그레이드.
  # 재다운로드 (변조·중단 의심 시)
  rm rosshield-server_*.tar.gz checksums.sha256
  curl -LO ${BASE}/rosshield-server_${VERSION}_${ARCH}.tar.gz
  curl -LO ${BASE}/checksums.sha256
  sha256sum -c checksums.sha256 --ignore-missing
  # Rekor unreachable(에어갭) 시 — tlog 검증 skip (보안 등급 감소)
  cosign verify-blob --insecure-ignore-tlog ...
  ```
  재발생 시 **P0 사고 신고**(README §Support 채널) — supply chain 오염 가능성.

### T2. seed admin exit 3 (duplicate seed)

- **증상**: `Error: tenant already seeded` (exit code 3).
- **원인 후보**: 같은 `--data-dir`로 이미 시드함.
- **대응**:
  - 새 PoC를 시작하려면 `--data-dir`을 새 디렉터리로 변경 (예: `./var-poc-2`).
  - 기존 admin으로 진행하려면 시드 절차 skip → 단계 1 부팅으로 바로 진입.
  - **데이터 삭제는 권장하지 않음** (P9 불변성). 새 디렉터리가 안전.

### T3. bind: address already in use

- **증상**: `listen tcp 127.0.0.1:8080: bind: address already in use`.
- **원인 후보**: 8080 포트 점유 (다른 rosshield 인스턴스 또는 다른 앱).
- **대응**:
  ```bash
  # 점유 프로세스 확인 (Linux)
  sudo lsof -i :8080
  # 또는 다른 포트 사용
  ./rosshield-server --addr 127.0.0.1:18080 --data-dir ./var
  ```
  그 후 브라우저는 `http://127.0.0.1:18080`로 접속.

### T4. 로그인 401 — 이메일 또는 패스워드가 올바르지 않습니다

- **증상**: `/login` 폼 제출 → "이메일 또는 패스워드가 올바르지 않습니다".
- **원인 후보**: 시드 시 사용한 이메일·패스워드와 입력 값 불일치 / Caps Lock / 시드 안 된 디렉터리에 부팅.
- **대응**:
  - 시드 시 사용한 정확한 이메일·패스워드 재확인 (시드 stdout JSON의 `email` 필드).
  - 시드 안 된 디렉터리면 단계 1 시드 명령 다시 실행.
  - 패스워드 잊은 경우 — 별 디렉터리에 새 시드 (현재 PoC는 패스워드 reset endpoint 미제공, R1 후속).

### T5. /healthz 무응답 또는 connection refused

- **증상**: `curl: (7) Failed to connect to 127.0.0.1 port 8080`.
- **원인 후보**: 서버 부팅 실패 (마이그레이션 실패·DSN 오타·license token 무효 등) / 다른 IP에 bind.
- **대응**:
  ```bash
  # 서버 stderr 마지막 50줄 확인
  ./rosshield-server --addr 127.0.0.1:8080 --data-dir ./var 2>&1 | tail -50
  ```
  - `migration failed: ...` → SQLite 파일 권한 또는 corrupt. `rm ./var/rosshield.db` 후 재시드 (PoC 환경 한정).
  - `license token: invalid signature` → license token 재발급 요청.
  - `bind 0.0.0.0:8080` 등 다른 IP면 firewall 확인.

### T6. SSH connection refused 또는 unknown host

- **증상**: `ssh: connect to host 10.0.1.42 port 22: Connection refused` 또는 `ssh: Could not resolve hostname robot01.lan`.
- **원인 후보**: 대상 robot SSH 데몬 미실행 / 방화벽 22 포트 차단 / DNS 미등록.
- **대응**:
  ```bash
  # robot 측에서 (콘솔 또는 KVM):
  systemctl status ssh           # active 확인
  sudo systemctl start ssh
  sudo ufw allow 22/tcp           # firewall 허용
  # customer 측에서:
  nc -zv 10.0.1.42 22             # TCP reachability 확인
  # DNS 미등록 시 IP 직접 사용 또는 /etc/hosts 추가:
  echo "10.0.1.42 robot01.lan" | sudo tee -a /etc/hosts
  ```

### T7. SSH permission denied (publickey)

- **증상**: `ros@10.0.1.42: Permission denied (publickey).`.
- **원인 후보**: private key 파일 mismatch / public key가 robot의 `~/.ssh/authorized_keys`에 없음 / SSH user 잘못.
- **대응**:
  ```bash
  # private key fingerprint 확인 (운영자 측)
  ssh-keygen -lf ~/path/to/private.key
  # robot 측 authorized_keys 확인 (콘솔 접근)
  cat ~ros/.ssh/authorized_keys | ssh-keygen -lf -
  # 두 fingerprint 일치 여부 비교
  ```
  불일치 시 robot에 새 public key append:
  ```bash
  echo "<public-key-content>" >> ~ros/.ssh/authorized_keys
  chmod 600 ~ros/.ssh/authorized_keys
  ```
  password 인증으로 변경(PoC 한정) — robot 측 `/etc/ssh/sshd_config`에서 `PasswordAuthentication yes` + `systemctl reload ssh`.

### T8. WebSocket + polling 둘 다 실패

- **증상**: scan progress 카드에서 카운터가 갱신 안 됨 + Live Badge·Polling Badge 모두 빨강.
- **원인 후보**: 인증 토큰 만료 / 프록시가 WS upgrade + polling 둘 다 차단 / 서버 측 progress emit 미동작.
- **대응**:
  - 페이지 새로고침 → 토큰 재발급.
  - 직접 접속 (`http://127.0.0.1:8080`) — 프록시 우회.
  - 서버 stderr 로그에서 `progress event emitted: session=...` 라인 확인 (없으면 server-side 문제, P1 사고).

### T9. 모든 check가 N/A

- **증상**: scan 완료 후 `PASS=0 FAIL=0 WARN=0 N/A=47`.
- **원인 후보**: pack과 robot OS/ROS2 버전 mismatch (예: CIS Ubuntu 22.04 pack을 Ubuntu 20.04 robot에 적용).
- **대응**:
  - `/robots/<id>` 상세 페이지에서 감지된 OS·ROS2 버전 확인.
  - 매칭 pack 선택 (예: `cis-ubuntu-20.04`가 있으면 그걸로, 없으면 baseline pack 사용).
  - pack 가용 목록은 `/packs` 페이지 확인.

### T10. scan 시작 직후 failed 상태

- **증상**: scan session이 1초 안에 `status: failed` + `failureReason` 표출.
- **원인 후보**: SSH 연결이 단계 3과 다른 시점에 끊김 / robot 디스크 가득 / pack 자산 누락.
- **대응**:
  - `/robots/<id>` → 상태가 `online`인지 재확인 + 단계 3 사전 SSH 단독 검증 다시 실행.
  - failureReason "Show more" expand → 상세 stderr 확인. `disk full`·`permission denied` 등 패턴별 대응.
  - robot 디스크 점검: `ssh ros@... df -h`.

### T11. rosshield-audit-verify signature INVALID 또는 chain MISMATCH

- **증상**: `signature: INVALID` 또는 `audit chain anchor: MISMATCH at entry N`.
- **원인 후보**: bundle 다운로드 중 손상 / 의도적 변조 / 키 회전 후 옛 키로 verify (드뭄).
- **대응**:
  - bundle 재다운로드 (네트워크 transient 가능).
  - 재발생 시 **즉시 P0 사고 신고**(README §Support 채널) — 데이터 무결성 위반 가능성.
  - 키 fingerprint 비교: `rosshield-audit-verify --print-key-fingerprint` 출력과 운영자 측 키 fingerprint 일치 여부.

### T12. License Status: GRACE 또는 EXPIRED

- **증상**: `/license` 페이지 `Status: GRACE` (만료 후 7일 grace) 또는 `EXPIRED`.
- **원인 후보**: license token 만료 / 시스템 시계 오류.
- **대응**:
  - 시스템 시계 확인: `date -u` (UTC) — 큰 편차면 `sudo timedatectl set-ntp true`.
  - license 만료 임박이면 운영자에 갱신 요청 (README §Support 채널 — 본 epic R3 마감 후 license-lifecycle docs 별도 가이드).
  - GRACE 기간 동안 enterprise 기능(SSO·MT·webhook)은 게이트 차단. 코어 기능(scan·report·audit)은 정상 동작 (P3 에어갭 1급).

---

## 다음 단계 안내

본 walkthrough(단계 0~6)가 모두 PASS하면 **1주차 Day-1 baseline 완료**입니다. 이후:

### 1주차 잔여 (T+1 ~ T+7)

`README.md` §"마일스톤 1주차" 표 참조. 우선순위:

1. **추가 robot 등록** — fleet 전체로 확장 (단계 3 반복).
2. **정기 스캔 스케줄 등록** — `/scans` → schedule 탭 (weekly 권장 시작값).
3. **사용자 초대** — `/users` 또는 `/invitations` (auditor·operator role 추가).
4. **First check-in 예약** — T+5 원격 15분, 막힌 지점·fail check 회수.

### 2주차 (T+8 ~ T+14)

`README.md` §"2주차" 표 참조:

1. **SSO 결선** (intake에서 활성 명시 시) — `/sso` → OIDC/SAML provider 등록.
2. **컴플라이언스 프레임워크** — `/compliance` → ISMS-P/ISO27001/NIST 800-53 중 선택.
3. **Webhook 결선** — `/integrations` → SIEM·Slack endpoint 등록.

### 30일차 정착 점검 (T+30)

`README.md` §"30일차" 표 참조 + retro 인터뷰 (NPS 대용).

### Production 배포 옵션

PoC 결과가 OK이고 정식 production 배포로 전환하려면:

| 옵션 | 내용 | 참조 |
|---|---|---|
| **storage Postgres 전환** | sqlite → postgres 마이그레이션 (기존 데이터 보존) | (Phase 5 후속 docs — TBD) |
| **HA 활성** | leader/follower 멀티 인스턴스 | `docs/design/notes/e25-ha-design.md` |
| **TLS reverse proxy** | nginx/Caddy 앞단 + TLS 인증서 | `docs/design/02-system-overview-and-deployment.md` |
| **Backup 자동화** | `rosshield-server backup` cron + S3/off-site | `README.md` §운영 |
| **Grafana ops dashboard** | Prometheus + Grafana import | `deploy/grafana/README.md` |
| **에어갭 pack mirror** | USB 서명 번들 → 내부 mirror | `quickstart.md` 부록 A |
| **SSO 자동 프로비저닝** | intake `sso.enabled=true` + email_domains 화이트리스트 | `/sso` 페이지 |

### customer-specific 옵션

| customer 요구 | 대응 |
|---|---|
| 멀티 fleet 분리 운영 | RBAC fleet 정밀화 (Phase 5 마감) — `/users` role binding |
| 감사인 자가 검증 | `rosshield-audit-verify` standalone binary 제공 (R30-4) |
| LLM advisor 사용 (옵트인) | `--llm-provider=openai` 또는 `ollama` (로컬). intake `license.expected_quota.llm_tokens_per_day` 양수 필수 |
| 데스크톱 단독 운영 | Tauri shell (D3 결정) — Phase 5 후속 release 대상 |
| 어플라이언스 OS 배포 | Ubuntu Core 24 (D4 가정) — Phase 3 exit 재확정 시점 |

---

## 부록 W: Windows 환경 별 노트

Windows 10/11에서 customer가 직접 운영하는 경우(데스크톱 SKU 가정):

- **PowerShell** 사용 — 본 문서의 bash 명령은 PowerShell equivalent로 변환 필요:
  - `curl -LO ...` → `Invoke-WebRequest -OutFile ...`
  - `sha256sum -c` → `Get-FileHash -Algorithm SHA256` + 수동 비교
  - `chmod +x` → 불필요 (Windows는 `.exe` 확장자로 실행 권한 자동)
  - `tar -xzf` → Windows 10 1803+ 기본 `tar` 명령 동일하게 사용 가능
- **release 자산** — `rosshield-server_v0.2.0_windows_amd64.zip` 사용 (tar.gz 대신).
- **에어갭** — Linux/macOS와 동일.

상세는 Phase 5 후속 별도 가이드(TBD).

---

## 변경 이력

| 날짜 | 변경 | 작성자 |
|---|---|---|
| 2026-05-15 | 초판 (Phase 6 후보 1 R2 — design doc §3.2/§6.2 R2 + D-CUSTONB-3 권장 default `bash + e2e CI 통합`) | rosshield core team |
