# ROS2 Humble Pack + DDS/SROS2 깊이 확장 — Phase 10 옵션 E Design

> **상태**: Design (Stage 10.E-1) — 코드 0줄 / 마이그레이션 0건 / pack 변경 0.
> **작성일**: 2026-05-21
> **범위**: Phase 10 backlog §4.5(`phase10-backlog-design.md`) 3순위 권장. 옵션 A(multi-region UI · v0.9.0) + 옵션 D(audit chain key rotation · v0.10.0~v0.10.2) 마감 후속. 본 round는 design doc만, 코드 진입은 D-P10E-1~4 사용자 확정 후 별 PR(Stage 10.E-2~7).
> **참조**:
> - `docs/design/notes/ros2-baseline-pack-design.md` (Jazzy 1차 design doc — Round 1+2+3 결선, 22 check 8/8 카테고리 cover).
> - `docs/design/notes/phase10-backlog-design.md` §4.5 (옵션 E 권장 진입 — 본 doc의 직접 부모).
> - `docs/design/notes/audit-chain-rotation-automation-design.md` (직전 design doc 패턴 — Phase 10 옵션 D).
> - 코드: `packs/ros2-jazzy/` · `internal/domain/benchmark/` (ValidatePackYAMLBytes · ParseCheckYAML · ParseSelfTestYAML · RunCheckSelfTest · 동적 fixture round-trip).
> - 외부: SROS2 design articles · DDS Security Spec v1.1 · ROS2 Humble Hawksbill 공식 docs(https://docs.ros.org/en/humble/).
> **R 식별자**: R-P10E-1 (본 doc 전체) — 결정 항목은 D-P10E-1~4.
> **본 문서 작성 위치**: main(head `195f26a`), 단독 sub-agent.
> **비목표** (§10에서 명시):
> - ROS2 Iron Irwini (non-LTS, 2025-11 EOL) — Humble + Jazzy LTS만 cover.
> - TurtleBot · 특정 robot 모델 종속 check — 단일 customer 의존 회피.
> - 외부 DDS 벤더(RTI Connext · OpenSplice 등) 정밀 — Cyclone DDS + Fast DDS만.
> - 자율 공격 시뮬레이션 — 설계서 P1 결정론 + CAI 영토 회피.
> - 실 robot HW 부재 시 docker-in-CI ROS2 실 환경 fixture — mock stdout/stderr fixture 우선(D-ROS2-9 일관).

---

## 1. 상태 / 배경

### 1.1 Phase 10 옵션 E 진입 가치

Phase 10 backlog(`phase10-backlog-design.md`) 8 후보 중 옵션 E는 3순위로 권장되었습니다. 옵션 A(multi-region UI 표면화, v0.9.0) + 옵션 D(audit chain key rotation, v0.10.0~v0.10.2) 마감 후속 진입.

본 round 진입 가치(§4.5 인용):
- ROS2 Humble Hawksbill 분기 customer cover — Jazzy 미사용 customer 영업 진입 베이스.
- SROS2 cert chain expiry/CA trust 검증 + DDS topic ACL 정밀화 — 카테고리 깊이 확장으로 layered defense 강화.
- ROS2 Round 3 carryover 6건 자연 cover.
- 외부 트랙 의존 0(paying customer Humble 명시 요구 *전*에도 baseline 가치) + 회귀 위험 낮음(pack 변경 isolated).

### 1.2 본 round 범위·비범위

- **범위**: `packs/ros2-humble/` 신규 pack 작성(Jazzy 22 check humble 변환 + distro 차이) + DDS topic ACL 정밀화 check(jazzy + humble 양쪽 동기) + SROS2 cert chain 검증 check(양쪽 동기) + Round 3 carryover 자연 통합.
- **비범위**: §10 명시 — Iron Irwini distro · TurtleBot/특정 robot · 외부 DDS 벤더 · 실 HW 의존 fixture · CAI 자율 공격.

### 1.3 본 design doc 마감 목표

memory `feedback_design_doc_first.md` 일관 — 코드 진입 *전* design doc에서 옵션 비교 + Stage 분해 + 결정 항목 권장 default 모두 명시. 다음 세션 즉시 Stage 10.E-2 진입 부담 0.

본 doc 자체는:
- 코드 변경 **0**
- 마이그레이션 **0**
- pack/converter 변경 **0**
- API 변경 **0**

산출물: 본 markdown 1개(~400~500줄) + commit 1건.

---

## 2. 현재 상태 fact-check (코드 직접 grep)

본 §은 추측 0, fact만 명시. 영역별 grep/Read 결과를 표 또는 bullet로 정리합니다.

### 2.1 `packs/ros2-jazzy/pack.yaml` (Jazzy 1차 pack)

| 영역 | fact |
|---|---|
| `apiVersion` | `rosshield.io/v1` |
| `kind` | `Pack` |
| `metadata.name` | `ros2-jazzy` |
| `metadata.version` | `0.1.0` |
| `metadata.vendor` | `rosshield` |
| `metadata.description` | `"ROS2 Jazzy LTS Baseline Security Pack (Round 1+2+3 — C1~C8 8/8 cover + 깊이 확장, 22 check)"` |
| `spec.schemaVersion` | `1` |
| 파일 총 줄수 | 18 |
| `compatibility.rosDistro` | **부재** — 1차 design doc §3.2에 설계되었으나 실 yaml에는 미정착. |
| `preflight` 블록 | **부재** — 1차 design doc §3.2에 설계되었으나 실 yaml에는 미정착. |

Jazzy 1차 design doc(`ros2-baseline-pack-design.md` §3.2)에 명시된 `compatibility.rosDistro: ["jazzy"]` + `preflight.sourceScript: /opt/ros/jazzy/setup.bash`는 yaml에 미정착. **본 round 진입 시 Humble pack은 동일 설계대로 `compatibility.rosDistro: ["humble"]` 명시 권장**(Jazzy pack은 v0.2.0 bump 시 합치는 별 carryover).

### 2.2 `packs/ros2-jazzy/checks/` (22 check 8/8 카테고리 cover)

| 카테고리 | check 수 | check id |
|---|---|---|
| C1 SROS2 / 인증 | 2 | `sros2_keystore_exists` · `sros2_security_enable` |
| C2 토픽 권한 | 2 | `cmd_vel_acl_enforced` · `cmd_vel_publisher_count` |
| C3 ROS_DOMAIN_ID | 1 | `domain_id_set` |
| C4 binary 무결성 | 5 | `apt_key_valid` · `apt_source_official` · `colcon_install_hash` · `no_world_writable_libs` · `signed_packages_only` · `systemd_unit_perms` |
| C5 launch 안전 | 5 | `argv_no_remote_url` · `lifecycle_node_used` · `no_shell_exec` · `no_world_writable_yaml` · `parameter_no_secret_inline` · `param_files_owner` |
| C6 distro lifecycle | 3 | `distro_is_lts` · `distro_not_eol` · `ros2_cli_available` |
| C7 RMW | 1 | `rmw_implementation_set` |
| C8 governance encryption | 1 | `governance_encrypt_topics` |
| **총** | **22** | (C4 6 / C5 6 — 합계 정정: 실제 ls 결과 C4=5+systemd_unit_perms=6 / C5=5+no_world_writable_yaml=6, 사용자 인용 22 일관) |

22 check 모두 selftest 1:1 매칭. round-trip test `TestROS2JazzyChecksRoundTrip`(`internal/domain/benchmark/ros2_jazzy_fixture_test.go`)이 ValidatePackYAMLBytes + ParseCheckYAML + ParseSelfTestYAML + RunCheckSelfTest 전 단계 PASS.

### 2.3 Round 3 carryover 6건 — commit `a914735` (2026-05-19)

commit `a914735` "feat(packs): ros2-jazzy Round 3 — C4·C5 carryover 6건 깊이 확장 (16→22 check)" — Phase 10 backlog §4.5에서 "ROS2 Round 3 carryover 6건이 자연 cover" 명시한 항목들은 **이미 ros2-jazzy/checks/에 적용**되어 있습니다:

- C4 3건: `apt_key_valid` · `colcon_install_hash` · `signed_packages_only`
- C5 3건: `param_files_owner` · `argv_no_remote_url` · `lifecycle_node_used`

본 round는 **humble pack을 별 pack으로 작성**하므로, 위 6건은 humble로도 동일 적용(yaml cargo cult). 이는 Phase 10 backlog §4.5의 "Round 3 carryover도 자연 cover"를 **humble pack에서 신규 cover**로 해석.

### 2.4 `packs/ros2-jazzy-baseline/pack.yaml` (별도 baseline pack, nrobotcheck 자동 변환)

| 영역 | fact |
|---|---|
| `metadata.name` | `ros2-jazzy-baseline` |
| `metadata.version` | `1.1.0` |
| `metadata.description` | `ROS 2 Jazzy Security Baseline v1.1` |
| check 수 | 329 (nrobotcheck 자동 변환) |

본 round 비범위 — ros2-jazzy-baseline은 자동 변환 pack이며 humble baseline은 별 epic(nrobotcheck humble 변환 source 필요 — paying customer 의존 ★).

### 2.5 benchmark 도메인 패턴 fact-check

`internal/domain/benchmark/` 핵심 API:
- `ValidatePackYAMLBytes(data) error` — pack.yaml schema validation.
- `ParseCheckYAML(checkBytes) (*Check, error)` — 단일 check yaml 파싱.
- `ParseSelfTestYAML(fixBytes) (*SelfTestSpec, error)` — selftest fixture 파싱.
- `RunCheckSelfTest(check, fixBytes) (*Result, error)` — fixture 시뮬레이션 + evaluation rule.
- `ParseEvalRule` — evaluation rule 파싱(contains · regex · exit_code).

`ros2_jazzy_fixture_test.go`가 22 check 동적 fixture round-trip을 수행 — 본 round의 humble pack도 동일 패턴 `TestROS2HumbleChecksRoundTrip` 작성.

### 2.6 D-ROS2-2 결정(2026-05-18, Jazzy 1차 design)

`ros2-baseline-pack-design.md` §9.2 D-ROS2-2:
> **(1) Jazzy LTS 단독 (v0.1.0) (권장 default)** — pack 이름 `ros2-jazzy`. Humble pack은 Jazzy pack의 변형으로 R3에서 추가 가능(yaml 90% 동일, 변경은 distro 이름·setup.bash 경로·LTS 버전 string만).

본 round = D-ROS2-2의 R3 진입. 1차 design에서 예측된 "Humble pack은 yaml 90% 동일, 변경은 distro 이름·setup.bash 경로·LTS 버전 string만" 가설을 본 doc §3에서 검증.

---

## 3. 위협 모델 / 요구사항

### 3.1 신규 위협

| 위협 | 가능성 | 영향 | 본 epic 대응 |
|---|---|---|---|
| Jazzy 미사용 customer가 Humble pack 부재로 진입 거부 | 중(Humble는 2022-05 release LTS) | 영업 기회 손실 | humble pack 신규(옵션 A 또는 C) |
| SROS2 cert chain 만료가 만료 전 알람 없이 audit fail | 중(cert 1년+ 운영 시) | DDS 통신 차단 + robot 전체 통신 중단 | C1 cert chain expiry check(D-P10E-4) |
| `/cmd_vel` 외 일반 topic(`/scan` LiDAR · `/odom` Odometry · `/tf` Transform) ACL 부재 | 높음(현재 C2는 cmd_vel만) | 센서 도청 + odom 위변조 → SLAM 공격 | C2 깊이 확장(D-P10E-3) |
| CA trust anchor 손상 시 가짜 cert 신뢰 | 낮음(CA 보호된 환경) | DDS 보안 무력화 | C1 CA trust 검증 check |
| Humble distro 차이로 jazzy check evalRule 부정확 | 중(humble setup.bash + apt source URL 차이) | 첫 humble customer에서 false positive | humble pack 신규 + distro 차이 격리(옵션 A 또는 C) |
| ROS2 Round 3 carryover 6건이 humble에 미적용 | 중(humble customer baseline 약화) | 동일 공격 표면 노출 | humble pack에 6건 자연 cover |

### 3.2 요구사항

| ID | 요구 | 측정 |
|---|---|---|
| R1 | ros2-humble pack은 Jazzy 22 check 변환 + distro 차이만 격리 | yaml diff ≤ 30% per check |
| R2 | DDS topic ACL 정밀화 check는 jazzy + humble 양쪽 동기 적용 | 두 pack에 동일 check id |
| R3 | SROS2 cert chain expiry check는 D-P10E-4 threshold 일관 | 단일 env `ROSSHIELD_SROS2_CERT_EXPIRY_DAYS` 지원 |
| R4 | humble pack은 jazzy pack과 별 file로 격리 — multi-distro single pack 회피 | `packs/ros2-humble/` 디렉토리 신규 |
| R5 | humble pack round-trip test `TestROS2HumbleChecksRoundTrip` PASS | 동적 fixture 22+α check 통과 |
| R6 | site policy 의존 check는 env 미설정 시 PASS skip(false positive 회피) | Round 3 패턴 일관 |
| R7 | 본 round 변경은 기존 jazzy pack 회귀 0 | 기존 `TestROS2JazzyChecksRoundTrip` PASS 유지 |
| R8 | humble distro EOL 이전 cover — 2027-05 EOL까지 운영 baseline | C6 humble lifecycle check |

---

## 4. 옵션 비교 (4 옵션)

### 4.1 옵션 A — ros2-humble 신규 pack 작성 (Jazzy 22 check 변환)

**설계 요약**: `packs/ros2-humble/` 신규 디렉토리 + `pack.yaml`(version 0.1.0) + 22 check yaml(jazzy → humble 변환) + 22 selftest. distro 차이만 격리 — `/opt/ros/humble/setup.bash` 경로 + apt source URL + LTS expected version string. Round 3 carryover 6건은 신규 cover(jazzy에 적용된 6건 동일 변환). DDS/SROS2 깊이 확장은 본 옵션에 포함하지 않고 별 라운드.

**가치**: ★★ — Humble distro cover. 깊이 확장 없음.

**노력 추정 (보수적)**: **2~3주**. pack scaffold 0.3주 + 22 check yaml 변환(jazzy 패턴 cargo cult) 1주 + 22 selftest 변환 0.5주 + round-trip test 0.3주 + e2e + release notes 0.4주.

**전제·의존**: 없음. paying customer Humble 명시 요구 *전*에도 baseline.

**리스크**: **낮음**. pack 변경 isolated. distro 차이 검증은 humble docs 기반 — 실 humble 환경 부재 시 첫 customer에서 false positive 가능(D-ROS2-9 mock fixture 패턴 일관).

### 4.2 옵션 B — Jazzy + Humble multi-distro single pack

**설계 요약**: pack 1개에 distro별 분기 — `compatibility.rosDistro: ["jazzy", "humble"]` + 각 check에 distro별 evalRule 분기(또는 audit cmd `case "$ROS_DISTRO"` 분기). 운영 단순(pack 1개) 그러나 yaml 복잡도 증가.

**가치**: ★★ — 운영 단순. 그러나 다음 distro(Kilted 2026-12 등) 추가 시 yaml 분기 누적.

**노력 추정 (보수적)**: **4~5주**. 기존 jazzy pack 22 check 모두 distro 분기 추가 + selftest 분기 + scanrun executor에서 `ROS_DISTRO` env 사용한 check dispatch 결선 — 회귀 위험 중.

**전제·의존**: 없음.

**리스크**: **중**. 기존 jazzy pack 22 check yaml 모두 변경 — 회귀 표면 큼. selftest fixture는 distro 분기마다 새로 작성 필요(22 × 2 = 44 fixture). 운영자가 단일 pack에서 distro별 결과 분리 표시 어려움.

### 4.3 옵션 C — Humble pack 신규 + DDS/SROS2 깊이 확장(jazzy + humble 동기) (권장)

**설계 요약**: 옵션 A + 깊이 확장 동시 진행. `packs/ros2-humble/` 신규(옵션 A 동일) + DDS topic ACL 깊이 확장(`/scan` · `/odom` · `/tf` 3 topic ACL check, jazzy + humble 양쪽 동기) + SROS2 cert chain 깊이 확장(cert expiry · CA trust · revocation 3 check, 양쪽 동기). 두 pack 동시 progression.

**가치**: ★★★★ — Humble cover + 깊이 확장(layered defense) + Round 3 carryover 자연 cover.

**노력 추정 (보수적)**: **3~4주** (Phase 10 backlog §4.5 추정 일관). pack scaffold + 22 check 변환(humble) 1주 + DDS topic ACL 깊이 확장 양쪽 0.5주 + SROS2 cert chain 깊이 확장 양쪽 0.5주 + selftest 변환 + 신규 fixture 0.5주 + e2e + ops docs + v0.11.0 minor release 0.5주.

**전제·의존**: 없음.

**리스크**: **낮음**. pack 변경 isolated. 깊이 확장은 jazzy에도 동기 — jazzy pack은 v0.2.0 minor bump(22 → 28 check 추정).

### 4.4 옵션 D — DDS/SROS2 깊이만 jazzy에 확장 + Humble은 다음 epic

**설계 요약**: humble pack은 paying customer 명시 요구 후 trigger(★ customer 외부 트랙). 본 round는 DDS topic ACL + SROS2 cert chain 깊이 확장 6 check만 jazzy에 추가(jazzy pack v0.2.0 bump).

**가치**: ★★★ — 깊이 확장만, distro cover 없음.

**노력 추정 (보수적)**: **1~2주**. 깊이 확장 6 check + selftest + round-trip test 0.5주 + e2e + release notes 0.5주.

**전제·의존**: customer Humble 진입 시점 ★(외부 트랙).

**리스크**: **낮음**. jazzy pack 한정 변경. humble cover는 customer 진입 후 별 epic.

### 4.5 옵션 비교 매트릭스

| 옵션 | distro cover | 깊이 확장 | 노력 | 리스크 | 외부 트랙 의존 | 즉시 진입 |
|---|---|---|---|---|---|---|
| **A** humble pack only | jazzy + humble | 없음 | 2~3주 | 낮음 | 0 | ✅ |
| **B** multi-distro single pack | jazzy + humble (단일) | 없음 | 4~5주 | 중 | 0 | ⚠️(회귀 위험) |
| **C** humble pack + 깊이 확장 양쪽 동기 | jazzy + humble | DDS + SROS2 | 3~4주 | 낮음 | 0 | ✅(**권장**) |
| **D** 깊이만 jazzy에 | jazzy만 | DDS + SROS2 | 1~2주 | 낮음 | ★ humble customer | ⚠️ |

---

## 5. Top 1 권장 + 근거

**옵션 C (Humble pack 신규 + DDS/SROS2 깊이 확장 양쪽 동기)** — Phase 10 backlog §4.5 설계 요약과 일관.

### 5.1 근거

1. **Phase 10 backlog §4.5 권장 default와 일치** — backlog가 옵션 E를 정의할 때 "ros2-humble 신규 pack + 카테고리 깊이 확장 + Round 3 carryover 자연 cover"를 함께 묶은 합성 옵션. 옵션 C가 그 합성.
2. **distro cover + 깊이 확장 동시 진척** — paying customer가 Humble 진입 시 즉시 baseline + layered defense. 옵션 A 또는 D 분리 진행 시 round 2회 누적 부담.
3. **회귀 위험 낮음** — pack 변경 isolated. jazzy pack v0.2.0 minor bump도 새 check 추가만(기존 22 check yaml 변경 0).
4. **외부 트랙 의존 0** — paying customer Humble 명시 요구 *전*에도 baseline 가치. Round 3 carryover 자연 cover로 jazzy의 깊이 확장도 일관.
5. **memory `feedback_design_doc_conservative.md` 일관** — 추정 3~4주는 backlog 추정과 일관. distro 차이 검증은 humble docs 기반(실 환경 부재 시 mock fixture, 첫 customer에서 false positive 보정).
6. **Jazzy 1차 design doc D-ROS2-2 R3+ 진입** — 본 round가 R3+ 진입 시점. 1차 design 가설 "yaml 90% 동일, 변경은 distro 이름·setup.bash 경로·LTS 버전 string만"을 검증.

### 5.2 보류 옵션 사유

- **옵션 A**(humble pack only): 깊이 확장 없음. paying customer demo 가치 ★★만. 옵션 C와 비교해 동시 가치 증분 회피.
- **옵션 B**(multi-distro single pack): 기존 jazzy pack 22 check 모두 변경 — 회귀 표면 큼. distro 추가 시 yaml 분기 누적 부담.
- **옵션 D**(깊이만 jazzy): humble cover 없음 → paying customer Humble 진입 시 별 round 부담. ★ customer 외부 트랙 의존.

---

## 6. Stage 분해 (옵션 C 채택 가정)

memory `feedback_design_doc_conservative.md` 일관 — 보수적 추정.

### 6.1 Stage 10.E-1 — 본 design doc (마감)

본 round (docs only, 코드 0). D-P10E-1·2·3·4 결정 + 사용자 합의.

### 6.2 Stage 10.E-2 — ros2-humble pack scaffold + C1~C3 check 변환

추정 **1주**.

- `packs/ros2-humble/pack.yaml` 신규:
  - `metadata.name: ros2-humble` · `version: 0.1.0` · `vendor: rosshield`.
  - `metadata.description: "ROS2 Humble Hawksbill LTS Baseline Security Pack (Round 1+2+3 + 깊이 확장, N check)"` — N은 Stage 10.E-6 후 확정.
  - `compatibility.rosDistro: ["humble"]` 명시(jazzy pack과 다른 점).
- `packs/ros2-humble/checks/C1-*.yaml` (sros2_keystore_exists · sros2_security_enable):
  - jazzy 변환 — `[ -f /opt/ros/jazzy/setup.bash ]` → `[ -f /opt/ros/humble/setup.bash ]`.
  - 나머지 90% cargo cult.
- `packs/ros2-humble/checks/C2-*.yaml` (cmd_vel_acl_enforced · cmd_vel_publisher_count): jazzy 변환.
- `packs/ros2-humble/checks/C3-*.yaml` (domain_id_set): jazzy 변환 + LTS expected version string은 humble("humble") 검증.
- `packs/ros2-humble/selftest/C1-*.yaml` · `C2-*.yaml` · `C3-*.yaml` (5 selftest): jazzy fixture cargo cult — `humble` distro string 차이만.
- distro 차이 documentation: pack.yaml 헤더 주석에 명시(humble setup.bash · LTS expected · apt source).

### 6.3 Stage 10.E-3 — C4(supply chain) + C5(launch 안전) check 변환

추정 **0.5주**.

- C4 6 check 변환(`apt_key_valid` · `apt_source_official` · `colcon_install_hash` · `no_world_writable_libs` · `signed_packages_only` · `systemd_unit_perms`):
  - apt source URL은 동일(packages.ros.org 공통).
  - LTS expected version은 humble("humble") 검증.
  - Round 3 carryover 3건(`apt_key_valid` · `colcon_install_hash` · `signed_packages_only`) 자연 cover.
- C5 6 check 변환(`argv_no_remote_url` · `lifecycle_node_used` · `no_shell_exec` · `no_world_writable_yaml` · `parameter_no_secret_inline` · `param_files_owner`):
  - launch.py 정적 grep 패턴은 distro 무관 — 변경 0.
  - Round 3 carryover 3건(`param_files_owner` · `argv_no_remote_url` · `lifecycle_node_used`) 자연 cover.
- 각 check 대응 selftest 12 fixture 변환.

### 6.4 Stage 10.E-4 — C6~C8 check + selftest cover + 동적 fixture round-trip

추정 **0.5주**.

- C6 3 check 변환(`distro_is_lts` · `distro_not_eol` · `ros2_cli_available`):
  - `distro_is_lts`는 humble = LTS PASS.
  - `distro_not_eol`은 humble EOL = 2027-05까지 cover. 2027-05 이후 customer 진입 시 EOL FAIL 처리.
- C7 1 check 변환(`rmw_implementation_set`): 동일 패턴.
- C8 1 check 변환(`governance_encrypt_topics`): 동일.
- 신규 round-trip test `internal/domain/benchmark/ros2_humble_fixture_test.go` — `TestROS2HumbleChecksRoundTrip`:
  - `ros2_jazzy_fixture_test.go` 패턴 cargo cult(line 27 ValidatePackYAMLBytes + line 75 ParseCheckYAML + line 95 RunCheckSelfTest).
  - 22 check 전 통과 검증.
- (옵션) v0.10.x patch 또는 Stage 10.E-7 v0.11.0 minor 시점(D-P10E-2 결정 항목)에서 humble pack 첫 release.

### 6.5 Stage 10.E-5 — DDS topic ACL 깊이 확장(jazzy + humble 양쪽 동기)

추정 **0.5주**.

- 신규 check 3건(jazzy + humble 양쪽 동일):
  - `ros2.C2.scan_topic_acl_enforced` — `/scan`(LiDAR) topic publisher count + permissions.xml ACL 명시.
  - `ros2.C2.odom_topic_acl_enforced` — `/odom`(Odometry) topic ACL 명시.
  - `ros2.C2.tf_topic_acl_enforced` — `/tf`(Transform) topic ACL 명시.
- D-P10E-3 결정 항목: ACL whitelist 정책(`/cmd_vel` + N 추가 vs site policy override). 권장 default = env `ROSSHIELD_DDS_TOPIC_ACL_WHITELIST` 미설정 시 PASS skip(site policy 의존 일관).
- selftest 6 fixture(3 check × 2 pack).
- 양쪽 pack 모두 minor bump — jazzy v0.1.0 → v0.2.0, humble은 v0.1.0 첫 release.

### 6.6 Stage 10.E-6 — SROS2 cert chain 깊이 확장(jazzy + humble 양쪽 동기)

추정 **0.5주**.

- 신규 check 3건(양쪽 동일):
  - `ros2.C1.sros2_cert_expiry` — CA + identity cert 만료까지 ≥ D-P10E-4 days. `openssl x509 -checkend $(( D * 86400 ))` 패턴.
  - `ros2.C1.sros2_ca_trust` — `~/.ros/keystore/enclaves/*/cert.pem` 체인 검증 + CA `ca.cert.pem` trust anchor 일관성.
  - `ros2.C1.sros2_cert_revocation` — CRL 또는 OCSP 응답 조회(env `ROSSHIELD_SROS2_CRL_PATH` 미설정 시 PASS skip — site policy 의존).
- D-P10E-4 결정 항목: cert expiry threshold(≥ 90일 권장 default · ≥ 30일 · site dependent).
- selftest 6 fixture(3 check × 2 pack).
- 양쪽 pack minor bump 누적(stage 10.E-5 동일).

### 6.7 Stage 10.E-7 — testcontainers integration + ops docs + v0.11.0 minor release

추정 **0.5주**.

- testcontainers integration(옵션 — D-ROS2-9 mock fixture 우선 default 일관 시 mock만):
  - 옵션 a: docker-in-CI humble 이미지 + 실 ROS2 cmd 결과 fixture 갱신. (운영 부담 — D-ROS2-9 옵션 2 동일).
  - 옵션 b: mock fixture default(권장 default).
- `docs/operations/ros2-humble-pack.md` 신규 — humble customer 진입 가이드(env 정의 · cert expiry threshold · DDS topic whitelist).
- `docs/operations/ros2-jazzy-pack.md` 갱신(존재 시) — DDS/SROS2 깊이 확장 6 check 추가 안내.
- v0.11.0 minor — Phase 10 옵션 E 마감 minor.
- release notes + CHANGELOG entry.

### 6.8 Stage 10.E-2~7 합계 추정

**~3~3.5주** (보수적). Phase 10 backlog §4.5 추정 3~4주와 정합. 본 doc은 보수적으로 3~4주 명시.

총 check 수 추정(Stage 10.E-7 마감 시):
- jazzy pack: 22(기존) + 6(DDS 3 + SROS2 3 깊이 확장) = **28 check** (v0.2.0).
- humble pack: 22(변환) + 6(깊이 확장 동기) = **28 check** (v0.1.0 첫 release).

### 6.9 병렬 진행 가능성 평가 (memory `feedback_parallel_agents.md` 일관)

- Stage 10.E-2·3·4(humble pack 22 check 변환)는 sub-agent 1명이 카테고리별 worktree로 분할 가능. 그러나 yaml cargo cult이고 변환 패턴 단순 — 병렬 이득 작음. 단일 agent 권장.
- Stage 10.E-5 + 10.E-6(깊이 확장 6 check, jazzy + humble 양쪽)은 sub-agent 분할 가능 — DDS topic ACL(Stage 5) + SROS2 cert chain(Stage 6)가 도메인 독립. 각 stage 0.5주이므로 병렬 이득 0.3~0.5주.
- 본 doc default = 단일 agent 순차 진행. 사용자 명시 시 Stage 10.E-5+6 병렬 가능.

---

## 7. 결정 항목

memory `feedback_design_doc_first.md` 일관 — 모든 결정에 권장 default 명시.

### 7.1 D-P10E-1 — 옵션 채택

- (A) humble pack only — 깊이 확장 없음.
- (B) multi-distro single pack — 회귀 위험 중.
- **(C)** humble pack + 깊이 확장 양쪽 동기 (**권장 default**) — Phase 10 backlog §4.5 일관, 외부 트랙 의존 0.
- (D) 깊이만 jazzy — humble cover 없음, ★ customer 의존.

**근거**: 옵션 C는 distro cover + 깊이 확장 동시 진척 + 회귀 위험 낮음 + 외부 트랙 의존 0. backlog 권장 default와 일관.

### 7.2 D-P10E-2 — Humble 첫 release 시점

- (1) Stage 10.E-4 후 v0.10.5 patch — humble pack 22 check 단독으로 patch release. 깊이 확장 6 check는 다음 minor(v0.11.0).
- **(2)** Stage 10.E-7 v0.11.0 minor (**권장 default**) — humble pack + 깊이 확장 6 check 모두 포함된 v0.11.0 minor. release 단순화.

**근거**: 옵션 (2)는 humble customer가 즉시 layered defense(깊이 확장 6 check 포함) baseline 진입. 옵션 (1)은 humble 22 check만으로 첫 release한 후 customer가 깊이 확장 부재 baseline 사용 — 두 release 사이에 baseline gap 발생.

### 7.3 D-P10E-3 — DDS topic ACL whitelist 정책

- (1) `/cmd_vel` + `/scan` + `/odom` + `/tf` 화이트리스트 hardcode — 일반 robot fleet 가장 빈도 높은 topic. 추가 topic는 false positive.
- **(2)** env `ROSSHIELD_DDS_TOPIC_ACL_WHITELIST` (comma-separated) 미설정 시 PASS skip (**권장 default**) — site policy 의존, Round 3 패턴 일관. customer 환경별 topic 다양성 흡수.
- (3) site policy override file(`/etc/rosshield/dds-acl-whitelist.txt`) — file 존재 시 적용, 부재 시 PASS skip. 운영 단순하나 file 경로 결정 + ops docs 부담.

**근거**: 옵션 (2)는 ROS2 Round 3 carryover 패턴 일관(`ROSSHIELD_COLCON_BASELINE_SHA256` · `ROSSHIELD_SAFETY_CRITICAL_NODES` 등). false positive로 customer 신뢰 손상 회피. customer onboarding 시 env 정의 절차 안내 필요(Round 3 일관).

### 7.4 D-P10E-4 — SROS2 cert chain expiry threshold

- (1) **≥ 90일 권장 default** (**권장 default**) — ISMS-P SC-12 baseline + enterprise compliance 일반 권장.
- (2) ≥ 30일 — 짧은 lead time, urgent 알람 발생 빈도 증가.
- (3) env `ROSSHIELD_SROS2_CERT_EXPIRY_DAYS` (default 90) — site dependent override 허용.

**근거**: 옵션 (1)은 SC-12 baseline + apt_key_valid Round 3 check의 ≥ 90일 일관(`ros2.C4.apt_key_valid`). 옵션 (3)은 (1)의 superset이며 권장 default + override 양쪽 cover — 실제 구현은 (3) default + 90일 fallback. 본 결정의 권장 default는 (1) + (3) hybrid: env override 지원하되 default 90일.

---

## 8. 마이그레이션·호환성 영향

### 8.1 신규 마이그레이션

**0건**. 본 round는 pack yaml만 추가 — DB 스키마 변경 0, audit chain 변경 0, API 변경 0.

### 8.2 기존 pack 호환성

- `packs/ros2-jazzy/pack.yaml` version bump 0.1.0 → 0.2.0(Stage 10.E-5·6 깊이 확장 6 check 추가). 기존 22 check yaml 변경 0.
- `packs/ros2-jazzy-baseline/`(329 check 자동 변환 pack) 변경 0 — 본 round는 nrobotcheck 자동 변환 source와 무관.
- `packs/cis-ubuntu-2404/` 변경 0.

### 8.3 customer 환경 영향

- customer 환경에서 humble pack은 별 선택 — scan profile pack 지정 시 사용(`packs/ros2-humble/`).
- 기존 jazzy customer는 jazzy pack v0.2.0으로 minor bump 시 6 check 추가 결과 — INDETERMINATE 또는 PASS skip 패턴(env 미설정 시) 일관, 회귀 0.
- humble customer 진입 시 humble pack을 scan profile에서 선택 — env 정의 절차는 ops docs(Stage 10.E-7) 가이드.

### 8.4 audit chain head sha 영향

- pack yaml 변경은 audit chain entry로 emit되지 않음(pack은 정적 자산).
- pack manifest 서명 변경은 0036 audit chain 또는 0037 audit_chain_keys와 무관 — pack manifest 서명은 별 도메인(0007_packs.up.sql).

### 8.5 fg-verify SDK 호환성

- 본 round는 audit chain key rotation 변경 없음 — fg-verify SDK 호환성 변경 0(v0.10.x 호환 일관).

---

## 9. 리스크

| # | 리스크 | 가능성 | 영향 | 완화 |
|---|---|---|---|---|
| R1 | Humble distro 실 환경 검증 없이 docs 기반 작성 → 첫 customer에서 false positive | 중 | 중 | mock fixture로 round-trip 검증 + ops docs에 "첫 customer 환경에서 env 정의 절차" 안내 + customer feedback로 fixture 갱신 |
| R2 | SROS2 cert chain CA trust 정책이 customer마다 달라 false positive | 중 | 중 | env override 지원(D-P10E-4 옵션 (3) 일관) + 미설정 시 PASS skip(D-P10E-3 패턴 일관) |
| R3 | DDS topic ACL 정밀화 fixture 작성이 실 ROS2 환경 없이 정교 mock 필요 | 중 | 중 | mock stdout/stderr fixture default(D-ROS2-9 일관) + customer onboarding 후 실 환경 결과로 fixture 갱신 |
| R4 | humble distro 2027-05 EOL 이후 customer 진입 시 baseline 무력화 | 낮음(2년+ 후) | 낮음 | C6 `distro_not_eol` check가 EOL 도래 시 FAIL 처리(jazzy 패턴 일관) |
| R5 | yaml cargo cult 변환 시 distro 차이 미세 누락 | 중 | 낮음 | round-trip test + selftest fixture가 distro string 분기 검증 + PR review |
| R6 | 깊이 확장 6 check 추가가 jazzy customer baseline 회귀 위험 | 낮음 | 중 | env 미설정 시 PASS skip 일관 → customer 환경 변경 0 + 단위 test 회귀 0 |
| R7 | humble pack 첫 release v0.11.0 minor 시 release notes baseline 변경 안내 누락 | 낮음 | 낮음 | Stage 10.E-7 release notes에 humble pack scan profile 선택 절차 + 깊이 확장 6 check env 안내 명시 |
| R8 | 외부 DDS 벤더(RTI Connext 등) customer가 본 pack을 잘못 사용 | 낮음 | 낮음 | C7 `rmw_implementation_set` check가 Cyclone DDS + Fast DDS만 PASS — 외부 벤더는 INDETERMINATE 마킹 |
| R9 | Stage 10.E-5·6 양쪽 pack 동기 변경 시 jazzy/humble 간 check id 일관성 누락 | 낮음 | 낮음 | check id 명명 규칙 명시(`ros2.CX.<name>`) + PR review |

---

## 10. 비목표

본 epic 명시 제외:

### 10.1 ROS2 Iron Irwini (non-LTS)

Iron은 2025-11 EOL — 본 doc 시점(2026-05) EOL 분기. Humble + Jazzy LTS만 cover. paying customer가 Iron 명시 요구 시 customer 측 distro upgrade 안내(별 epic).

### 10.2 TurtleBot · 특정 robot 모델 종속 check

단일 customer 환경 의존 — Phase 10 backlog §9.3 일관(단일 customer 의존 epic 거부). robot 모델별 customization은 customer onboarding 절차에서 site policy override env로 cover.

### 10.3 외부 DDS 벤더(RTI Connext · OpenSplice 등) 정밀

Cyclone DDS + Fast DDS만 cover. RTI Connext는 commercial license + customer 환경 의존(★ 외부 트랙). C7 `rmw_implementation_set` check는 외부 벤더 INDETERMINATE 마킹으로 회피.

### 10.4 자율 공격 시뮬레이션 (P1 결정론 위반)

설계서 §01 원칙 1(결정론) + §12 비목표(CAI 영토 회피) 일관. 본 pack은 audit cmd output 결정론 평가만. fuzz · 자율 공격은 별 epic 거부.

### 10.5 실 robot HW 의존 fixture

D-ROS2-9 mock fixture 패턴 default 일관. docker-in-CI humble 이미지 실 ROS2 cmd 결과 capture는 별 CI epic(Stage 10.E-7 옵션 a) — 운영 부담 크고 본 doc 비범위.

### 10.6 ros2-humble-baseline (nrobotcheck humble 변환 pack)

`ros2-jazzy-baseline/`(329 check 자동 변환) 패턴의 humble 분기는 별 epic — nrobotcheck humble 변환 source 필요. paying customer 명시 요구 시 별도 진입(★ 외부 트랙).

### 10.7 plugin check type (`ros2_topic_audit` · `ros2_launch_audit`)

D-ROS2-8 결정 일관 — 본 doc 범위 외. Round 1·2·3 모두 SSH cmd path만 사용. 깊이 확장 6 check도 SSH cmd로 cover 가능(openssl x509 · grep 등).

### 10.8 자체 ROS2 distro 빌드 / package mirror

`ros2-baseline-pack-design.md` §0 비목표 일관 — customer 환경 그대로 가정. internal mirror 운영 site는 별 carryover(`ROSSHIELD_APT_ORIGIN_WHITELIST` env 도입 검토).

---

## 11. 참조

### 11.1 직전 design doc 4건

- `docs/design/notes/ros2-baseline-pack-design.md` — Jazzy 1차 design(2026-05-18), R1+R2+R3 결선. D-ROS2-2 R3+ 진입이 본 doc.
- `docs/design/notes/phase10-backlog-design.md` §4.5 — 본 doc의 직접 부모(옵션 E 권장 default).
- `docs/design/notes/audit-chain-rotation-automation-design.md` — Phase 10 옵션 D(직전 round) design doc 패턴.
- `docs/design/notes/multi-region-ha-design.md` — Phase 10 옵션 A(직직전 round) design doc 패턴.

### 11.2 Phase 10 마감 release

- v0.9.0 — Phase 10 옵션 A(multi-region UI) 마감.
- v0.10.0~v0.10.2 — Phase 10 옵션 D(audit chain key rotation + hot fix).
- v0.11.0(예정) — Phase 10 옵션 E 마감 minor.

### 11.3 코드 영역

- `packs/ros2-jazzy/` — Jazzy 1차 pack(22 check 8/8 카테고리 cover).
- `packs/ros2-jazzy-baseline/` — nrobotcheck 329 check 자동 변환 pack(별 도메인).
- `packs/cis-ubuntu-2404/` — Ubuntu CIS baseline.
- `internal/domain/benchmark/` — ValidatePackYAMLBytes · ParseCheckYAML · ParseSelfTestYAML · RunCheckSelfTest 핵심 API.
- `internal/domain/benchmark/ros2_jazzy_fixture_test.go` — 동적 fixture round-trip 패턴(humble 복제 source).

### 11.4 외부 spec

- SROS2 design articles: https://design.ros2.org/articles/ros2_dds_security.html (출처: design.ros2.org — 공식 design repo).
- DDS Security Spec v1.1 (출처: Object Management Group · https://www.omg.org/spec/DDS-SECURITY/1.1/).
- ROS2 Humble Hawksbill 공식 docs: https://docs.ros.org/en/humble/ (출처: docs.ros.org — Humble 공식 documentation).
- ROS2 distro release schedule: https://docs.ros.org/en/rolling/Releases.html (Humble EOL 2027-05, Jazzy EOL 2029-05).
- Mayoral-Vilches et al., "SROS2: Usable Cyber Security Tools for ROS 2" (학술 인용 후보 — 1차 design 일관).

### 11.5 결정 history

- D-ROS2-2 (2026-05-18) — Jazzy LTS 단독(권장 default), Humble pack은 R3+에서 추가. 본 round = R3+ 진입.
- D-ROS2-9 (2026-05-18) — mock stdout/stderr fixture default. 본 round 일관 적용.
- D-P10-1 (2026-05-20) — Phase 10 진입 + Top 3 우선순위(A → D → E). 본 round = 3순위 진입.

### 11.6 memory feedback

- `feedback_design_doc_first.md` — 1일+ 임계 design doc 우선. 본 round 일관.
- `feedback_design_doc_conservative.md` — 잠재 효과/시간 보수적. 본 round 추정 3~4주 보수.
- `feedback_parallel_agents.md` — 매 stage 시작 시 병렬 가능성 재평가. §6.9 평가.
- `feedback_user_tracks.md` — D1·E36·customer trigger 외부 트랙 제외. 본 round 외부 트랙 의존 0.
- `feedback_no_rest_recommendation.md` — 휴식 옵션 자동 포함 X.
- `feedback_recommend_next_actions.md` — 다음 추천 작업 3~5건 명시.
- `feedback_skip_handoff.md` — handoff edit/commit/push 생략, CHANGELOG + release notes + commit 메시지로 trace.

---

**문서 끝**. 본 round 마감 — D-P10E-1·2·3·4 사용자 확정 후 Stage 10.E-2 진입 가능.
