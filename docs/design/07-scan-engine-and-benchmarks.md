# 07. 스캔 엔진 및 벤치마크

## 7.1 목표

- 로봇 플릿에 대해 **대량·병렬·결정론적** 감사를 실행.
- 증거(Evidence) 원본을 **해시 주소**로 영속화.
- **차분 스캔** 지원 — 이전 세션 대비 변한 것만 재실행.
- 벤치마크는 **서명된 팩**으로 관리, 제품과 분리된 라이프사이클.
- 모든 체크는 **Self-Test 가능**해야 한다.

## 7.2 스캔 실행 아키텍처

```
┌─────────────────────────────────────────────────┐
│               Scan Orchestrator                 │
│  ─ 세션 생성 · 큐 관리 · 진행 상태 브로드캐스트   │
└───────┬─────────────────────────────────────────┘
        │
        ├──▶ Check Executor (per robot)
        │       ├─ pre-run: connection check
        │       ├─ for each check:
        │       │    ├─ SSH exec via SSH Pool
        │       │    ├─ capture stdout/stderr/exit
        │       │    ├─ redact sensitive patterns
        │       │    ├─ Evidence Store: dedupe & store blob
        │       │    ├─ Evaluator: apply rule → outcome
        │       │    └─ Result DB write + Event publish
        │       └─ post-run: summary, metrics
        │
        └──▶ SSH Pool (shared across robots)
                ├─ connection limit per host
                ├─ connection limit per tenant
                ├─ retry with backoff
                └─ host key pinning
```

### 실행 파이프라인 단계

1. **Session 생성** — `POST /scans`로 트리거. scope(로봇·팩·레벨·차분 여부) 기록.
2. **큐 적재** — robot × check의 카티전 곱을 큐에 쌓음. 우선순위는 criticality.
3. **Executor pool** — goroutine/worker가 큐에서 pull.
4. **SSH exec** — SSH Pool이 세션 재사용(keepalive). command timeout 적용.
5. **Redaction** — stdout/stderr에서 비밀 패턴 제거 후 Evidence Store로.
6. **Evaluation** — CheckDefinition의 `evaluationRule`에 따라 pass/fail/error/NA/manual.
7. **Insight 트리거** — drift·anomaly·root-cause가 enable인 경우 후처리 파이프라인으로.
8. **Audit append** — 세션 완료 시 `ScanCompleted` 엔트리.

## 7.3 벤치마크 팩 포맷

### 디렉터리 구조

```
cis-ubuntu-24.04-v1.2.3.pack/
  ├─ pack.yaml                    # 메타데이터 (id, version, vendor, signature)
  ├─ checks/
  │   ├─ 1.1.1.1-fs-cramfs.yaml
  │   ├─ 1.1.1.2-fs-freevxfs.yaml
  │   └─ ...
  ├─ mappings/
  │   ├─ iso27001.yaml
  │   ├─ nist-800-53.yaml
  │   └─ isms-p.yaml
  ├─ templates/                   # 리포트 템플릿 오버라이드 (옵션)
  ├─ selftest/
  │   ├─ fixtures/                # known-good/bad 시스템 샘플 출력
  │   └─ cases.yaml               # 각 check를 어떻게 self-test 할지
  └─ SIGNATURE                    # pack.yaml + 모든 파일의 manifest 서명
```

### `pack.yaml`

```yaml
apiVersion: fleetguard.dev/pack/v1
kind: BenchmarkPack
metadata:
  id: cis-ubuntu-24.04
  version: 1.2.3
  vendor: "CIS"
  name: { ko: "CIS Ubuntu 24.04 Benchmark", en: "CIS Ubuntu 24.04 Benchmark" }
  compatibility:
    os: ["ubuntu-24.04"]
    rosDistro: ["jazzy", "any"]
  license: "CC-BY-NC-ND-4.0"
  publisher: "Fleetguard, Inc."
  publishedAt: "2026-03-01T00:00:00Z"
signature:
  algorithm: "ed25519"
  signerKeyId: "fleetguard-pack-2026"
  signature: "base64..."
```

### Check 정의 (`checks/1.1.1.1-fs-cramfs.yaml`)

```yaml
apiVersion: fleetguard.dev/check/v1
kind: CheckDefinition
metadata:
  code: CIS-1.1.1.1
  title:
    ko: "cramfs 파일시스템 모듈 로드 금지"
    en: "Ensure mounting of cramfs filesystems is disabled"
  severity: medium
  required: true
  automated: true
  levels: [L1]
spec:
  auditCommand:
    argv: ["bash", "-c", "modprobe -n -v cramfs; lsmod | grep cramfs"]
    timeoutSec: 10
  evaluationRule:
    type: expression
    expression: |
      stdout.includes("install /bin/true") || stdout.includes("install /bin/false")
      && !stdout.match(/^cramfs\s/m)
  remediationCommand:
    argv: ["bash", "-c",
      "echo 'install cramfs /bin/false' | sudo tee /etc/modprobe.d/cramfs.conf"]
    requiresSudo: true
  controlMappings:
    - framework: cis
      control: "1.1.1.1"
    - framework: iso27001
      control: "A.8.9"
    - framework: nist-800-53
      control: "CM-7"
  references:
    - { label: "CIS Benchmark 1.1.1.1", url: "https://..." }
```

### 평가 규칙 (`evaluationRule`) 유형

| type | 설명 | 사용 예 |
|---|---|---|
| `exit_zero` | exit code == 0이면 pass | 가장 단순한 체크 |
| `stdout_match` | 정규식/문자열 매치 | 설정 값 존재 여부 |
| `stdout_equals` | trim 후 정확히 일치 | 버전 확인 |
| `stdout_json_path` | JSONPath로 값 추출 후 비교 | `systemctl show --output=json` |
| `expression` | 안전한 표현식 언어 | 복합 조건(AND/OR) |
| `custom_evaluator` | 팩 제공 WASM 모듈 (v2) | 복잡한 로직 |

**`expression` 언어**: 화이트리스트 함수만 (includes/match/length/trim/json.parse/semver.gte 등). eval 계열 금지.

### 플러그인 체크 타입 (v1.1+)

SSH 명령 외 체크 유형을 추가할 수 있는 확장 포인트:

```yaml
kind: CheckDefinition
spec:
  type: ros2_topic_audit        # SSH 명령 대신 커스텀 검사기
  params:
    topic: "/cmd_vel"
    expect:
      - field: "qos.reliability"
        equals: "RELIABLE"
```

팩이 선언한 `type`은 Core의 CheckType 레지스트리에서 해석. 외부 플러그인은 서명 필수.

## 7.4 팩 생명주기

```
[Install] ──▶ [Staged] ──activate──▶ [Active] ──deactivate──▶ [Archived]
                │                                                │
                └──────────────── remove ────────────────────────┘
```

- **Staged**: 서명 검증·Self-Test 통과, 아직 스캔에 사용되지 않음.
- **Active**: 기본 스캔에 사용됨.
- **Archived**: 이전 버전으로 롤백 가능, 새 스캔에 사용 안 됨.
- **Remove**: 소프트 삭제(감사 로그 유지용 참조 남음).

## 7.5 팩 서명 · 검증

### 서명 과정 (팩 제공자)

1. 모든 파일의 `sha256` 생성 → `manifest.txt`.
2. `manifest.txt`를 개인키로 Ed25519 서명 → `SIGNATURE`.
3. tar.gz으로 번들.

### 검증 과정 (제품)

1. tar 해제 후 `manifest.txt` 재계산, 기록된 해시와 대조.
2. `SIGNATURE`를 신뢰하는 공개키 번들로 검증.
3. 기기 내 설치 위치에 복사, 메타데이터를 DB에 기록.
4. 실패 시 설치 거부 + 감사 엔트리.

### 신뢰 키 관리

- Core에 **내장 공개키 번들**(시스템 팩용) + 고객이 추가한 공개키(고객 내부 팩).
- 키 회전: 새 키로 서명된 업데이트 팩 배포 후 구 키 expiration.

## 7.6 Self-Test 프레임워크

### 목적

체크의 `auditCommand` + `evaluationRule`이 실제로 pass/fail을 올바르게 구분하는지 **팩 제공자가 증명**하고, **제품은 그 증명을 재실행**할 수 있게.

### Self-Test 케이스

```yaml
apiVersion: fleetguard.dev/selftest/v1
kind: CheckSelfTestCase
metadata:
  checkCode: CIS-1.1.1.1
spec:
  cases:
    - name: "disabled via /etc/modprobe.d — pass"
      fixture:
        stdout: "install /bin/true\n"
        stderr: ""
        exitCode: 0
      expect:
        outcome: pass
    - name: "loaded module — fail"
      fixture:
        stdout: "cramfs 12345 0\n"
        stderr: ""
        exitCode: 0
      expect:
        outcome: fail
    - name: "timeout simulation — error"
      fixture:
        error: timeout
      expect:
        outcome: error
```

### 실행

- 팩 설치 시 자동 실행 — 실패한 체크는 `degraded` 상태로 표시, 사용 여부는 사용자 선택.
- CI에서 팩 변경 시 자동 실행.
- 커버리지 목표: Self-Test가 있는 체크 비율 **80%+**.

## 7.7 SSH 실행 세부사항

### 연결 풀

- **키당 최대 N 연결** (기본 3), **호스트당 최대 M** (기본 5), **테넌트당 최대 K** (기본 50).
- 유휴 연결은 keepalive 30초, 유휴 5분 후 종료.
- 재시도: `error.Transient == true`인 경우만 (network timeout, `EHOSTUNREACH` 등).

### 명령 실행

```
exec(robot, argv, options) →
  if not connected: establish()
  channel = session.open_channel()
  start = now()
  channel.run(argv, { timeout, env, cwd })
  stdout, stderr = channel.read()
  return { exitCode, stdout, stderr, durationMs }
```

- **argv 기반만** — 쉘 문자열 파싱 없음. 벤치마크가 `bash -c "..."`를 요청하면 `argv: ["bash", "-c", "..."]`로 선언.
- 출력 크기 제한 도달 시 잘라내고 `truncated: true` 플래그.

### 진단 모드 (온보딩)

- 첫 연결 시 OS/ROS distro/Docker/네트워크 구성 등을 자동 수집하여 **추천 팩·레벨·criticality**를 반환.
- 결과는 `nrobotcheck` 온보딩(F19) 경험을 승계.

## 7.8 Evidence Store

### 저장 모델

- **컨텐츠 해시 주소**: `ev_<sha256-hex>` 또는 `ev_<ulid>` + `sha256` 필드.
- **blob 저장**: 로컬 파일시스템 or MinIO/S3.
- **중복 제거**: 같은 해시는 blob 1개, 참조 카운터 증가.
- **압축**: zstd. 텍스트 계열은 80~95% 압축률.

### 레덕션 파이프라인

```
raw bytes
  → 패턴 매칭 (정규식 + 엔트로피)
  → 위치 기록 + 대체
  → 결과 바이트 저장
  → RedactionMark 레코드에 { offset, length, type } 저장
```

- 패턴 예시: PEM 키 블록, AWS access key, GitHub token, 비밀번호=... 형식.
- 레덕션 off 설정 존재하나 **기본값 on**, 감사 로그에 on/off 변경 기록.

### 접근 제어

- Evidence 다운로드는 `scan.read` + 리소스 소유자 규칙.
- 다운로드 시 사전 서명 URL 짧은 만료(5분).

## 7.9 차분 스캔 (Differential Scan)

### 동기

- 180개 체크 × 50대 로봇 = 9000 실행. 매일 돌리면 운영 부담.
- 대부분 상태는 변하지 않음 — 변한 것만 재실행하면 충분.

### 구현

1. 지난 N일 최신 결과의 `evidence.sha256` 기록.
2. 새 세션 시작 시 **빠른 프리플라이트**로 증거 해시만 재수집(가능한 체크에 한해).
3. 해시 동일 → 이전 결과 재사용 (세션에 `reused: true` 플래그).
4. 해시 다름 → 전체 평가 실행.

### 효과 목표

- 동일 플릿 재스캔 시 **실행 시간 70% 이상 감소**.
- 감사 무결성은 유지 (Evidence와 Result는 여전히 해당 세션에 귀속).

### 한계

- 프리플라이트 불가능한 체크(결과가 랜덤·시간 의존): 차분 대상에서 제외 플래그.

## 7.10 스케줄링

- **Cron 표현식**(tenant 로컬 타임존) + **one-off 예약** + **이벤트 트리거**(새 로봇 등록 시 등).
- 배포 분리 모드에서는 분산 락 + leader election.
- 실행 결과는 `ScanSession.trigger = 'schedule'`로 기록.
- 스케줄 실패 재시도 정책 설정 가능(없음/지수 백오프/다음 주기).

## 7.11 벤치마크 업데이트 흐름

```
Vendor (우리 or 3rd)
  ├─ 신규 체크 추가 / 기존 체크 수정
  ├─ Self-Test fixture 갱신
  ├─ 팩 서명
  └─ Pack Mirror에 게시

고객 환경
  ├─ 온라인 모드: 주기 확인 → 다운로드 → 서명 검증 → Staged
  ├─ 오프라인 모드: 관리자가 서명 번들을 USB로 반입 → 설치
  │
  └─ Staged 팩 수동 Activate (고객 정책에 따라 자동화 가능)
     └─ 차분 스캔 → 새 체크만 추가 실행
```

## 7.12 체크 카탈로그 관리 (내부)

- 제품 팀이 유지하는 **체크 소스(YAML)** 저장소는 별도 git 리포.
- 빌드: YAML → 서명 → 팩 번들.
- QA: known-good/bad VM에 전체 Self-Test 자동화.
- 변경 리뷰: LLM이 PR 리뷰 보조(영향받는 체크·매핑·설명 일관성 검사) — 옵션, 인간 승인 필수.

## 7.13 기존 nrobotcheck 자산 승계

| 자산 | 승계 |
|---|---|
| `rawdata/ros2_jazzy_robot_benchmark.csv` | 팩으로 변환 (pack-tools) |
| `rawdata/ros2_humble_robot_benchmark.csv` | 팩으로 변환 |
| `resources/baselines/cis_ubuntu_2404_benchmark.json` | 팩으로 변환 |
| `PassLogic` 트리 평가 로직 | `evaluationRule.expression` 언어로 이전 |
| `audit_command` 구조 | `spec.auditCommand.argv`로 정규화 |
| `check_conditions` 사용자 오버라이드 | `ControlCustomization`으로 |

자세한 재활용 전략은 `12-migration-and-non-goals.md`.

## 7.14 성능 목표

| 시나리오 | 목표 |
|---|---|
| 단일 로봇 × 180 체크 | < 3분 (평균) |
| 플릿 50대 × 180 체크 × 병렬 10 | < 15분 |
| 차분 스캔 (변화 < 5%) | 기존 시간의 30% 이하 |
| Evidence 저장 처리량 | 1000 events/s (단일 노드) |
| 동시 세션 | 100 (enterprise) |

## 7.15 실패·복구

- SSH 실패 → `outcome=error`, `evidence`에 오류 메시지 저장.
- 중간 실패한 세션 → `resumeFrom` 지원으로 남은 체크만 재실행.
- 세션 취소 → 진행 중 체크는 완료까지 대기(원격 작업 중단 위험), 이후 체크는 skip.
- 네트워크 전체 장애 → Scheduler가 다음 창에 재시도.

## 7.16 이 문서의 핵심 결정

1. **벤치마크는 서명된 팩** + **Self-Test 프레임워크**가 품질의 전부.
2. **SSH 명령은 argv**, 쉘 파싱 없음, 타임아웃·크기 제한 필수.
3. **Evidence는 해시 주소 저장 + 레덕션**.
4. **차분 스캔**으로 일상 운영 부담 절감.
5. **벤치마크 업데이트 사이클**이 수익 모델의 근간 — 팩 배포 체계를 제품보다 더 정성껏.

다음 문서: [08-intelligence-and-compliance.md](./08-intelligence-and-compliance.md)
