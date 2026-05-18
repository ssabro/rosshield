# ROS2 Baseline Pack 설계 — 솔루션 핵심 차별화 영역 carryover 마감

> **상태**: Design draft (Phase 6 후속 / `scanrun-ssh-integration-design.md` §6.1 옵션 D 후반부 carryover, D-ROS2 결정 대기).
> **작성일**: 2026-05-18.
> **범위**: `packs/ros2-jazzy/` 신규 pack 도입 + `cmd/pack-tools/converter/ros2.go` 보강(기존 framework JSON 변환기 재사용) + `internal/app/scanrun/` SSH executor의 ROS2 환경 source 결선(env injection · ROS_DISTRO 가드). 본 doc 자체는 **코드 변경 0 / 마이그레이션 0 / pack 변경 0**.
> **참조**:
> - `docs/design/notes/scanrun-ssh-integration-design.md` §6.1 옵션 D — "ROS2 specific cmd library 동시" + 본 doc carryover 명시(line 225).
> - `docs/design/00-mission-and-positioning.md` §0.1 미션 — "ROS2 로봇 플릿이 정의된 보안 기준을 지속적으로 지키고 있음을 ... 증명한다."
> - `docs/design/07-scan-engine-and-benchmarks.md` §7.2~§7.3 — pack 구조, `rosDistro` compatibility 필드, `ros2_topic_audit` 플러그인 check type 힌트, §7.6 Self-Test.
> - `docs/design/11-tech-stack-and-roadmap.md` D7 결정(2026-04-23): "초기 타깃 벤치마크 = CIS Ubuntu 24.04 + ROS2 Jazzy baseline".
> - 기존 pack 패턴: `packs/cis-ubuntu-2404/{pack.yaml,checks/*.yaml,selftest/*.yaml,MANIFEST,SIGNATURE}` (313 checks · 자동 변환률 94.6% · Manual 17).
> - 기존 ROS2 framework JSON 변환기: `cmd/pack-tools/converter/ros2.go` (E12 Stage B 결선, nrobotcheck 전신 한국어 baseline 변환용).
> - 도메인 entity: `internal/domain/scan/scan.go::CheckDef.AuditCommand` (현재 ROS2-specific 0).
> **R 식별자**: R-ROS2-1 (본 doc 전체) — 결정 항목은 D-ROS2-1 ~ D-ROS2-9.
> **본 worktree**: `agent-ad9689a85f79ff04f`, main(head `693b005`)에서 분기. 단독 sub-agent.
> **비목표**:
> - **에이전트 자율 공격 / fuzz** — CAI 영토(설계서 §00 포지셔닝 위반).
> - **DDS 패킷 직접 inspection / pcap** — 본 pack은 SSH 위 결정론적 cmd output만. wire-level은 별 epic.
> - **ROS2 source 코드 정적 분석** — paying customer 요구 후 별 pack.
> - **새 transport 도입** (rmw direct, gRPC) — scanrun §6.1 옵션 C 또는 별 phase.
> - **자체 ROS2 distro 빌드 / package mirror** — customer 환경 그대로 가정.

---

## 1. 상태 / 배경

### 1.1 본 doc 위치 — 솔루션 핵심 차별화 영역 carryover 마감

본 doc은 `scanrun-ssh-integration-design.md` §6.1 옵션 D 후반부의 **명시적 carryover**입니다. 옵션 D 본문 line 225 인용:

> "본 doc(scanrun deep dive)은 **옵션 A 단독**을 마무리합니다. ROS2 specific pack(옵션 D의 후반부)은 본 doc 종료 후 별 design doc(`ros2-baseline-pack-design.md`)로 분리."

scanrun SSH 결선(E29~E32)이 production-quality에 도달했고, customer onboarding(E38) docs까지 마감되었지만, **본 솔루션이 "ROS2 로봇 플릿 보안 감사" 제품인 한 ROS2-specific check가 0이면 차별화 미진척** 상태입니다. CIS Ubuntu 24.04 pack(313 checks)만으로는 본 제품이 일반 Linux compliance 도구와 구분되지 않습니다.

설계서 §00 미션을 다시 인용합니다:

> "**ROS2 로봇 플릿이** 정의된 보안 기준을 지속적으로 지키고 있음을, 감사인이 받아들이는 형태의 증거와 함께 증명한다."

미션 첫 단어가 "ROS2 로봇 플릿". 본 doc은 그 첫 단어를 코드 산출물로 진입시키는 carryover 마감입니다.

### 1.2 본 design doc 마감 목표

memory `feedback_design_doc_first.md` 일관 — 코드 진입 *전* design doc에서 옵션 비교 + Stage 분해 + 결정 항목 권장 default 모두 명시. 다음 세션 즉시 Stage 1 진입 부담 0.

본 design doc 자체는:
- 코드 변경 **0**
- 마이그레이션 **0**
- pack/converter 변경 **0**
- API 변경 **0**

산출물: 본 markdown 1개(~500~600줄) + commit 1건.

### 1.3 현 상태 진단 — 자산 매트릭스

| 영역 | 현 상태 | 한계 | 본 doc 영향 |
|---|---|---|---|
| **Pack 인프라** | `cmd/pack-tools/converter/` + `packs/cis-ubuntu-2404/`(313 checks) — yaml + Self-Test + MANIFEST + Ed25519 SIGNATURE 패턴 정착 | ROS2 specific check 0 | 패턴 재사용 — `packs/ros2-jazzy/` 신규 |
| **ROS2 변환기** | `converter/ros2.go` 320줄 — nrobotcheck 전신의 한국어 framework JSON(4계층 audit_command + pass_logic 트리)을 single-bash로 합치는 변환기 | 입력 데이터(전신 JSON)는 nrobotcheck 자산이며 본 리포에 없음 / "공개 표준 ROS2 baseline" 부재 — converter는 있으나 변환할 source 미확정 | 변환기 재사용 + source 결정(D-ROS2-3) |
| **Scan engine ROS2 통합** | `scan.CheckDef.AuditCommand`는 string 그대로 SSH로 송신 — `ros2 node list` 같은 cmd는 ROS2 env(`source /opt/ros/jazzy/setup.bash`) 없이 not found | env source 결선 0 / `ROS_DISTRO` 가드 0 / sudo vs ros user 정책 0 | scanrun executor env injection (D-ROS2-5·D-ROS2-7) |
| **Pack compatibility** | `pack.yaml`에 `compatibility.rosDistro`(예: `["jazzy", "any"]`) 설계서 §7.2에 명시 | 실 pack에서 사용 0 — `cis-ubuntu-2404/pack.yaml`는 `rosDistro` 필드 없음 | `packs/ros2-jazzy/pack.yaml`에서 `rosDistro: ["jazzy"]` 정착 |
| **Plugin check type** | 설계서 §7.3에 `type: ros2_topic_audit` 힌트만 — `internal/domain/scan/`에 type registry 부재 | 본 doc 범위 초과 — 별 epic(D-ROS2-8) | 본 doc Round에서는 일반 SSH cmd만 사용, plugin type은 backlog |
| **Self-Test fixture** | CIS pack: 313 checks 중 313 selftest entry(자동 마커 + 17 manual fixture, Manual 21 design 완료) | ROS2는 fixture source가 더 부족 — 실 robot 부재 시 mock stdout 작성 필요 | D-ROS2-9에서 mock 우선 정착 |

### 1.4 진단 종합

본 카테고리는 다른 epic(scanrun · customer onboarding · audit chain)과 결합도가 낮지만, **MVP 차별화 가치의 모든 무게가 여기에 실려** 있습니다. carryover 마감을 더 미루면:

- 첫 paying customer demo에서 "ROS2 robot 보안 감사"라고 부르기 어색.
- D7(2026-04-23) 결정 "초기 타깃 = CIS Ubuntu 24.04 + ROS2 Jazzy baseline" 절반만 cover된 상태로 v0.3.0 release.
- enterprise 협상 시점에 ROS2 check 카탈로그 0 → SI/컨설팅 세그먼트(설계서 §0.4) 진입 불가.

본 doc은 위 carryover의 **합성 전략 + Stage 분해 + 결정 항목 권장 default**까지 마감합니다.

---

## 2. ROS2 보안 카테고리 분류 (C1~C8)

본 절은 ROS2 로봇 fleet에서 audit 대상이 되는 보안 영역을 카테고리화합니다. SROS2(`design.ros2.org/articles/ros2_dds_security.html`) + DDS Security Spec v1.1 + Open Source Robotics Foundation(OSRF) 권장 + ROS2 Survey 학술 문헌(Mayoral-Vilches et al., "SROS2: Usable Cyber Security Tools for ROS 2") + 운영 경험 기반.

### C1 — 노드 ID · 인증 (SROS2 / DDS Security)

**위협**: 인증 없이 join한 악의적 노드가 `/cmd_vel` publish → 로봇 임의 조작.

**check 후보** (5~7건 예상):
- `sros2.policy_files_exist` — `~/.ros/keystore/enclaves/` 존재 + 정책 파일 매핑.
- `sros2.ca_cert_validity` — CA 인증서 만료까지 ≥ 30일.
- `sros2.enclave_per_node` — `/talker` 등 활성 노드 모두 enclave 매핑.
- `sros2.security_enabled_env` — `ROS_SECURITY_ENABLE=true` + `ROS_SECURITY_STRATEGY=Enforce`.
- `sros2.identity_pkey_perms` — `identity/key.pem` 권한 `0600` + owner ros-user.
- `sros2.governance_signature` — `governance.p7s` 서명 유효성 (openssl smime verify).
- `sros2.permissions_signature` — `permissions.p7s` 동일.

**audit cmd 예**: `ros2 security list_keystores`, `openssl x509 -in <ca>.pem -checkend 2592000`, `env | grep ROS_SECURITY_`.

**예상 PASS/FAIL/INDETERMINATE 매핑**:
- PASS: `ros2 security` cmd exit 0 + 출력에 활성 enclave 1+.
- FAIL: enclave 0 또는 만료 임박 인증서 검출.
- INDETERMINATE: SROS2 미설치 (no-op robot) — site policy 의존.

**manual 비율 추정**: 30% (CA 발급 절차·키 저장 위치는 customer 환경별).

### C2 — 토픽 권한 (publisher/subscriber ACL · 민감 토픽 발견)

**위협**: ACL 부재 시 임의 subscriber가 `/camera/image_raw` 도청, 임의 publisher가 `/cmd_vel` 명령 발사.

**check 후보** (6~9건 예상):
- `topic.cmd_vel_publisher_count` — `/cmd_vel` publisher 수가 정의된 화이트리스트 내(보통 1).
- `topic.cmd_vel_acl_enforced` — `permissions.xml`에 `<publish><topic>cmd_vel</topic></publish>` 명시.
- `topic.sensitive_topics_subscribed` — `/joint_states`·`/tf`·`/odom`·`/scan` subscribe 노드가 화이트리스트 내.
- `topic.image_topics_acl` — `/camera/*` 모두 ACL 적용.
- `topic.parameter_topics_acl` — `/parameter_events`·`/rosout` ACL 적용.
- `topic.qos_reliability_for_safety` — `/cmd_vel` QoS = RELIABLE + KEEP_LAST.
- `topic.no_anonymous_subscribers` — 익명 subscriber 0 (DDS Security activation 시 자동).
- `topic.unauthorized_topics` — site 정책 외 토픽 0 (`ros2 topic list` 결과를 expected allowlist와 diff).
- `topic.lifecycle_state_secure` — managed node lifecycle 상태 = `active` (idle 노드 정리).

**audit cmd 예**: `ros2 topic info /cmd_vel --verbose`, `ros2 topic list -t`, `cat ~/.ros/keystore/enclaves/*/permissions.xml`.

**manual 비율 추정**: 40% (site policy 의존 — 허용 토픽 allowlist는 customer 정의 필요).

### C3 — ROS_DOMAIN_ID 격리

**위협**: 기본 ROS_DOMAIN_ID=0 사용 시 인접 fleet과 토픽 충돌 + cross-fleet 도청.

**check 후보** (3~5건 예상):
- `domain.id_set_explicit` — `ROS_DOMAIN_ID` 환경변수 정의 + ≠ 0.
- `domain.id_range_valid` — 0 ≤ id ≤ 232 (DDS spec).
- `domain.id_documented` — fleet docs(site별)에 매핑 기록.
- `domain.id_unique_per_fleet` — `ros2 daemon status --domain-id` 결과 단일 fleet만 응답.
- `domain.localhost_only_when_dev` — `ROS_LOCALHOST_ONLY=1` (개발 환경) 또는 명시적으로 0(운영).

**audit cmd 예**: `env | grep ROS_DOMAIN_ID`, `ros2 daemon status`.

**manual 비율 추정**: 20% (대부분 단순 env 검사).

### C4 — 노드 binary 무결성 (apt source · `colcon build` 산출 검증)

**위협**: 비신뢰 binary가 노드로 실행 → 백도어.

**check 후보** (4~6건 예상):
- `binary.apt_source_official` — `/etc/apt/sources.list.d/ros2.list`가 공식 packages.ros.org 또는 distributor.
- `binary.apt_key_valid` — ROS2 GPG key 유효 + 만료 ≥ 90일.
- `binary.colcon_install_hash` — `install/setup.bash`의 dist 디렉토리 sha256이 `pack/baselines/` 기준값과 일치 (or customer-supplied digest list).
- `binary.no_world_writable_libs` — `ament_index/*/lib/` world-writable 0.
- `binary.signed_packages_only` — `apt-cache policy ros-jazzy-*` 결과에 알 수 없는 origin 0.
- `binary.systemd_unit_perms` — `/etc/systemd/system/ros2-*.service` owner=root, perms ≤ 0644.

**audit cmd 예**: `apt-cache policy ros-jazzy-ros-core`, `find ~/ros2_ws/install -name '*.so' -perm /o+w`.

**manual 비율 추정**: 35% (customer-supplied baseline digest 필요).

### C5 — launch 파일 안전 (parameter 보안 / 외부 yaml 노출 / launch arg injection)

**위협**: launch arg를 통해 외부 yaml 로드 → parameter injection으로 노드 동작 변경.

**check 후보** (4~6건 예상):
- `launch.param_files_owner` — launch가 참조하는 모든 param yaml owner=ros-user, perms ≤ 0644.
- `launch.no_world_writable_yaml` — `find <launch-yaml-dir> -perm /o+w` 결과 0.
- `launch.argv_no_remote_url` — launch arg에 `http://` `https://` 외부 URL 없음(grep으로 시작 점).
- `launch.parameter_no_secret_inline` — yaml 안에 `password|secret|token` 키 inline value 없음(env 또는 vault 참조만).
- `launch.no_shell_exec` — launch 안 `ExecuteProcess(shell=True)` 0(launch.py 정적 분석).
- `launch.lifecycle_node_used` — safety-critical 노드는 managed lifecycle 사용.

**audit cmd 예**: `grep -r "ExecuteProcess.*shell=True" ~/ros2_ws/src/`, `find /etc/ros2/params -perm /o+w`.

**manual 비율 추정**: 50% (launch.py 정적 분석은 본 doc 범위 초과 — manual 권장).

### C6 — ROS2 distro 버전 (Jazzy LTS vs Iron EOL · 보안 patch)

**위협**: EOL distro 사용 시 보안 patch 미반영.

**check 후보** (3~4건 예상):
- `distro.is_lts` — `ros2 --version` 또는 `dpkg -l ros-*-ros-core` 결과 = `jazzy` 또는 `humble` (LTS).
- `distro.not_eol` — `iron`·`foxy`·`galactic`·`dashing`·`eloquent` 검출 시 FAIL.
- `distro.security_patch_age` — `apt-get changelog ros-jazzy-ros-core` 최신 patch ≤ 90일.
- `distro.unattended_upgrades_enabled` — `/etc/apt/apt.conf.d/50unattended-upgrades`에 ROS2 origin 포함.

**audit cmd 예**: `ros2 --version`, `dpkg -l 'ros-*-ros-core' | awk '$1=="ii"{print $2}'`.

**manual 비율 추정**: 10% (env detection 대부분 자동).

### C7 — rmw_implementation (Cyclone DDS vs Fast DDS · 알려진 CVE)

**위협**: 미패치 rmw 구현체로 RCE / DoS.

**check 후보** (3~5건 예상):
- `rmw.implementation_set` — `RMW_IMPLEMENTATION` env 명시 (default 의존 회피).
- `rmw.implementation_supported` — set이 `rmw_cyclonedds_cpp` 또는 `rmw_fastrtps_cpp` 중.
- `rmw.fastdds_version_min` — Fast DDS ≥ baseline 버전 (CVE-2024-XXXX 패치 포함).
- `rmw.cyclonedds_version_min` — Cyclone DDS ≥ baseline.
- `rmw.discovery_protocol_safe` — `CYCLONEDDS_URI`에 `<Discovery><AllowMulticast>false</AllowMulticast>` 또는 unicast 강제.

**audit cmd 예**: `env | grep RMW_IMPLEMENTATION`, `dpkg -l ros-jazzy-rmw-*`.

**manual 비율 추정**: 25% (CVE-version 매핑은 매 quarter 갱신 필요).

### C8 — transport encryption (DDS Security 활성화 · TLS for ROS2 services)

**위협**: 평문 DDS traffic 도청 / man-in-the-middle.

**check 후보** (3~5건 예상):
- `transport.dds_security_enabled` — `ROS_SECURITY_ENABLE=true` (C1과 중복 아님 — 여기서는 transport plugin 활성).
- `transport.governance_encrypt_topics` — `governance.xml`의 `<topic_security_kind>ENCRYPT</topic_security_kind>` 명시 토픽 ≥ allowlist.
- `transport.tls_for_ros_bridge` — `rosbridge_server` 사용 시 TLS cert + key 설정.
- `transport.no_unencrypted_services` — `ros2 service list -t` 결과 중 평문 service 0(governance 매핑 검증).
- `transport.discovery_server_secure` — `ROS_DISCOVERY_SERVER` 설정 시 TLS endpoint 사용.

**audit cmd 예**: `cat ~/.ros/keystore/enclaves/*/governance.xml | grep topic_security_kind`.

**manual 비율 추정**: 40% (governance.xml 분석 일부 수동).

### 카테고리 요약

| 카테고리 | check 추정 | manual 비율 | 1순위 진입 |
|---|---|---|---|
| C1 노드 ID·인증 | 5~7 | 30% | ✅ MVP |
| C2 토픽 권한 | 6~9 | 40% | ✅ MVP |
| C3 ROS_DOMAIN_ID | 3~5 | 20% | ✅ MVP (가장 쉬움) |
| C4 binary 무결성 | 4~6 | 35% | 옵션 B(권장)에서 후속 |
| C5 launch 안전 | 4~6 | 50% | 옵션 B 후속 |
| C6 distro 버전 | 3~4 | 10% | ✅ MVP (가장 쉬움) |
| C7 rmw_implementation | 3~5 | 25% | ✅ MVP |
| C8 transport encryption | 3~5 | 40% | ✅ MVP (C1과 결합) |
| **총** | **31~47** | **~32%** 평균 | 옵션 B Round 1 = C1·C2·C3·C6·C7·C8 (24~35건), Round 2 = C4·C5 (8~12건) |

memory `feedback_design_doc_conservative.md` 일관 — manual 비율은 site policy 의존 항목을 모두 manual로 보수 잡음. 실제 작성 시 일부는 fixture 패턴 재사용으로 자동 변환 가능.

---

## 3. Pack 구조 — `packs/ros2-jazzy/`

기존 CIS pack 구조를 그대로 차용합니다. memory `feedback_go_commit_pipeline.md` 일관(파일·net layer 변경 없음).

### 3.1 디렉토리 layout

```
packs/ros2-jazzy/
├── pack.yaml                  # 메타 + compatibility.rosDistro=["jazzy"]
├── MANIFEST                   # sha256 list (build 시 생성)
├── SIGNATURE                  # Ed25519 (build 시 생성)
├── checks/
│   ├── C1-sros2-keystore.yaml
│   ├── C1-sros2-security-enable.yaml
│   ├── ...
│   ├── C8-transport-tls-rosbridge.yaml
│   └── ...
└── selftest/
    ├── C1-sros2-keystore.yaml
    ├── ...
```

### 3.2 `pack.yaml` 예

```yaml
apiVersion: rosshield.io/v1
kind: Pack
metadata:
  name: ros2-jazzy
  version: 0.1.0
  vendor: rosshield
  description: ROS2 Jazzy LTS Baseline Security Pack
spec:
  schemaVersion: 1
  compatibility:
    os: ["ubuntu-24.04"]
    rosDistro: ["jazzy"]
  preflight:
    requiredEnv: ["ROS_DISTRO", "AMENT_PREFIX_PATH"]
    requiredBinaries: ["ros2"]
    sourceScript: "/opt/ros/jazzy/setup.bash"
```

`preflight` 블록은 본 doc에서 신규 도입. scanrun executor가 check 실행 직전 robot에서 `[ -f /opt/ros/jazzy/setup.bash ] && source /opt/ros/jazzy/setup.bash` 한 줄을 prepend. 누락 시 모든 ROS2 check가 INDETERMINATE 마킹 + 진단 메시지 "ROS2 환경 미설치 (preflight)".

### 3.3 Check 정의 예 (`checks/C3-domain-id-set.yaml`)

```yaml
apiVersion: rosshield.io/v1
kind: Check
metadata:
  id: ros2.C3.domain_id_set
  title: ROS_DOMAIN_ID is explicitly set and non-zero
  description: |-
    Default ROS_DOMAIN_ID=0 leaks discovery traffic to adjacent fleets on
    the same network. Explicit assignment per fleet is required for isolation
    and reduces cross-fleet topic collision.
  severity: medium
spec:
  auditCommand: |-
    bash -c '
    set -o pipefail;
    [ -f /opt/ros/jazzy/setup.bash ] && source /opt/ros/jazzy/setup.bash;
    DOM="${ROS_DOMAIN_ID:-unset}";
    if [ "$DOM" = "unset" ] || [ "$DOM" = "0" ]; then
      echo "** FAIL ** ROS_DOMAIN_ID=$DOM";
    else
      echo "** PASS ** ROS_DOMAIN_ID=$DOM";
    fi
    '
  evaluationRule:
    op: contains
    value: '** PASS **'
  rationale: |-
    DDS discovery broadcasts use ROS_DOMAIN_ID for namespacing. ID 0 (default)
    is widely used by tutorials and demo code, so any neighbor on the same
    network is likely to be reachable. Assigning a per-fleet ID also prevents
    accidental cross-fleet teleop or topic loops.
  fixGuidance: |-
    1. Choose a per-fleet ID in [1, 232].
    2. Add `export ROS_DOMAIN_ID=<id>` to /etc/profile.d/ros2.sh.
    3. Document the assignment in fleet inventory (per-site mapping).
    4. Verify via `ros2 daemon status --domain-id <id>`.
```

### 3.4 Self-Test 예 (`selftest/C3-domain-id-set.yaml`)

```yaml
apiVersion: rosshield.io/v1
kind: SelfTest
metadata:
  checkId: ros2.C3.domain_id_set
spec:
  cases:
    - name: passes when domain id is 42
      input:
        stdout: '** PASS ** ROS_DOMAIN_ID=42'
        stderr: ''
        exitCode: 0
      expectedOutcome: PASS
    - name: fails when domain id is 0
      input:
        stdout: '** FAIL ** ROS_DOMAIN_ID=0'
        stderr: ''
        exitCode: 0
      expectedOutcome: FAIL
    - name: fails when domain id unset
      input:
        stdout: '** FAIL ** ROS_DOMAIN_ID=unset'
        stderr: ''
        exitCode: 0
      expectedOutcome: FAIL
```

### 3.5 ROS2-specific 필드 (CIS와 차이)

| 필드 | CIS pack | ROS2 pack | 비고 |
|---|---|---|---|
| `compatibility.rosDistro` | 없음 | `["jazzy"]` | 설계서 §7.2 정의 처음 사용 |
| `preflight.sourceScript` | 없음 | `/opt/ros/jazzy/setup.bash` | 신규 — scanrun에서 한 줄 prepend |
| `preflight.requiredBinaries` | 없음 | `["ros2"]` | preflight 실패 시 모두 INDETERMINATE |
| audit cmd 시작 | `bash -c '#!/usr/bin/env bash ...'` | `bash -c 'set -o pipefail; [ -f ... ] && source ...; ...'` | env source 패턴 표준화 |
| evaluationRule | 동일 (`contains '** PASS **'`) | 동일 | 호환 |
| Self-Test | 마커 + 17 manual fixture | 마커 + ROS2-specific fixture (실 robot 부재 시 mock stdout 작성) | D-ROS2-9 |

---

## 4. 자동 변환 가능성

### 4.1 ROS2 공식 baseline source 부재

CIS Ubuntu 24.04는 CIS-CAT (Center for Internet Security) 공식 PDF + XCCDF XML이 존재해 313 checks 중 94.6%를 converter로 자동 변환 가능했습니다. ROS2는 이런 표준이 없습니다.

기존에 본 리포에 들어온 자산:
- `cmd/pack-tools/converter/ros2.go` 320줄 — nrobotcheck 전신 한국어 framework JSON 변환기. **입력 JSON 자체는 본 리포에 없음** (nrobotcheck `resources/baselines/ros2_*_security_baseline_framework_*.json`).

후보 source:
1. **nrobotcheck 전신 framework JSON 재사용** — 한국어 baseline 30~60 items, converter는 이미 존재. 단 nrobotcheck 자산은 paid customer 1과 함께 협상해 가져온 자산 → 라이선스 검토 필요(D-ROS2-3).
2. **SROS2 공식 docs 수동 변환** — `design.ros2.org/articles/ros2_dds_security.html` + ROS2 design repo. 표준이지만 check 형식 변환은 수동.
3. **OSRF + 학술 권장 수동 합성** — Mayoral-Vilches et al. SROS2 paper, ROS2 Survey, DDS Security Spec v1.1.
4. **paying customer 환경 reverse engineering** — 첫 customer 환경 cmd 결과로 baseline 합성. customer 0인 현 시점 불가.

### 4.2 변환기 재사용 가능 범위

`converter/ros2.go`는 입력이 4계층 framework JSON일 때만 동작합니다. SROS2 docs(markdown) → check yaml 변환은 별 converter 필요.

- **재사용 부분** (가능): `buildBashCombine` + `wrapBash` + `ConditionToBashTest` 함수 — bash 합성 + escape 로직. 수동 작성한 yaml 안에서도 동일 escape 패턴 사용.
- **재사용 불가** (수동): condition extraction (markdown → structured yaml). SROS2 docs는 prose. LLM 도움 가능하지만 결정론적 변환 아님.

### 4.3 결론 — 수동 작성 우선

자동 변환률 추정: **0~20%** (전신 framework JSON 라이선스 OK 시 일부 재사용 가능, 그 외 수동).

본 pack은 **수동 작성을 default**로 하고, converter는 보조 도구로만 사용. CIS pack의 94.6% 변환률은 본 pack에서 재현되지 않습니다. → 일정 추정 매우 보수적으로 잡아야 합니다.

---

## 5. 합성 전략 옵션 (4종)

### 옵션 A — 일괄 전 카테고리 (C1~C8) 30~50 check 작성

전 8 카테고리를 1 epic으로 작성. 4~6주 추정.

**Pros**:
- baseline 완전 cover — demo에서 "8 카테고리 ROS2 보안" 풀스코프.
- pack 하나로 release 깔끔.

**Cons**:
- 일정 long — paying customer 진입 timing 압박 충돌.
- review 부담 큼 — single PR 또는 4~6주 누적 commit.
- C4·C5(binary·launch) manual 비율 높아 가설 검증 부재 상태로 작성하면 회귀 위험.

**회귀 위험**: 중 (pack 신규 + scanrun preflight 결선 모두 한꺼번에).

**추정**: **4~6주** (보수).

### 옵션 B — Round 1 MVP 6 카테고리 (C1·C2·C3·C6·C7·C8) + Round 2 C4·C5 분리

Round 1에 24~35 check (manual ~30%), Round 2에 8~12 check.

**Pros**:
- MVP 가치 즉시(2~3주) demo 가능.
- C4·C5는 launch.py 정적 분석 등 별 epic 의존 → Round 2로 미루기 자연.
- 회귀 위험 Round별 분리.
- memory `feedback_design_doc_conservative.md` 일관 — 가설 격리.

**Cons**:
- 사용자 round 2회 필요.
- pack 버전 0.1.0(Round 1) → 0.2.0(Round 2) 단계적 release — release note 2회.

**회귀 위험**: 낮음~중 (Round별).

**추정**: Round 1 = **2~3주**, Round 2 = **1~1.5주**, 누적 **3~4.5주**.

### 옵션 C — ROS2 community baseline 정립 후 진입

ROS-Industrial security WG 또는 OSRF가 표준 baseline 발표 후 변환만 수행.

**Pros**:
- 표준 부합 — 감사인 인정도 최대.
- 자동 변환률 80%+ 기대.

**Cons**:
- **표준 부재** — ROS-I security WG 자체가 활동 저조. 1년+ 대기.
- 그동안 본 솔루션은 ROS2-specific check 0 — 차별화 미진척 지속.

**회귀 위험**: 0 (대기).

**추정**: **1년+** (가설).

### 옵션 D — 보류 (paying customer 요구 후)

첫 paying customer가 명시적으로 ROS2 check 요청할 때까지 보류.

**Pros**:
- 가설 작업 0.
- customer 요구 정확히 맞춤.

**Cons**:
- 본 솔루션 미션(§00 "ROS2 로봇 플릿")과 충돌 — customer가 "ROS2 check 없는 ROS2 audit 도구"를 보고 진입 거부 가능.
- D7 결정(2026-04-23) 미이행.
- demo에서 "ROS2 robot" 마케팅 부담.

**회귀 위험**: 0.

**추정**: 0주 (보류).

### 옵션 비교 매트릭스

| 옵션 | 카테고리 cover | 추정 | 회귀 위험 | round 수 | MVP demo | 권장 |
|---|---|---|---|---|---|---|
| A 일괄 | C1~C8 (8) | 4~6주 | 중 | 1 | ✅ | 우선순위 2 |
| **B 분리** | Round 1: 6 / Round 2: 2 | 3~4.5주 | 낮음~중 | 2 | ✅ (Round 1 후) | **권장** |
| C 표준 대기 | 8 (변환) | 1년+ | 0 | 1 | ❌ | 우선순위 4 |
| D 보류 | 0 | 0주 | 0 | 0 | ❌ | 우선순위 3 |

---

## 6. 권장 옵션 + 근거

### 6.1 권장 = 옵션 B (Round 1 MVP 6 카테고리 우선)

**근거**:

1. **MVP 가치 즉시 회수** — Round 1(C1·C2·C3·C6·C7·C8) 2~3주로 demo·marketing·D7 결정 모두 cover. C4·C5는 launch.py 정적 분석 같은 별 영역 의존이라 별 round로 미루는 게 자연.
2. **회귀 위험 분리** — Round 1은 단순 SSH cmd만 사용. Round 2는 launch.py 정적 분석 plugin check type 도입 가능성 — 별 R 식별자로 격리.
3. **memory `feedback_design_doc_conservative.md` 일관** — manual 비율 32% 평균이지만 실제로는 일부 fixture 패턴 재사용으로 줄어들 가능성. Round 1 마감 후 회수율을 측정해 Round 2 추정 보정.
4. **paying customer 진입 timing** — customer onboarding(E38) 결선이 paying customer 1명 진입 시점을 trigger. Round 1만 마감해도 "8 카테고리 중 6"라고 말할 수 있어 차별화 메시지 작동.
5. **D5 open-core 정합** — Round 1은 코어 pack(Apache-2.0). Round 2는 enterprise tier 후보(BSL 또는 commercial) — D5 분리 시점에 자연스럽게 그룹화.
6. **사용자 round 2회 acceptable** — Phase 5에서 epic당 평균 3~5 round였음. 본 epic 2 round는 단순.

### 6.2 옵션 A를 *지금* 채택하지 않는 이유

- 4~6주 long epic이 paying customer 진입 timing과 충돌.
- C4·C5는 plugin check type(설계서 §7.3 `ros2_topic_audit`) 결정 필요 → 별 epic으로 분리하는 게 자연.
- review 부담 큼 — Phase 5 patterns 위반.

### 6.3 옵션 C·D를 *지금* 채택하지 않는 이유

- **C**: ROS-I security WG 표준 부재가 1년+ 지속 — 솔루션 차별화 미진척 carryover 상태로 v0.4.0~v1.0.0 release.
- **D**: 본 솔루션 미션과 충돌. customer가 "ROS2 check 없는 ROS2 도구"를 보고 진입 거부 가능 — 가장 risky.

---

## 7. 변경 사항 outline (옵션 B 채택 시)

본 절은 다음 세션이 즉시 Stage 1에 진입할 수 있는 정밀도로 기술합니다. memory `feedback_design_doc_first.md` 일관.

### 7.1 R1 — MVP 6 카테고리 (24~35 check)

**마이그레이션**: 0.

**신규 파일**:
- `packs/ros2-jazzy/pack.yaml` (~25줄).
- `packs/ros2-jazzy/checks/C1-*.yaml` (5~7개, 각 ~30~80줄).
- `packs/ros2-jazzy/checks/C2-*.yaml` (6~9개).
- `packs/ros2-jazzy/checks/C3-*.yaml` (3~5개).
- `packs/ros2-jazzy/checks/C6-*.yaml` (3~4개).
- `packs/ros2-jazzy/checks/C7-*.yaml` (3~5개).
- `packs/ros2-jazzy/checks/C8-*.yaml` (3~5개).
- `packs/ros2-jazzy/selftest/*.yaml` (각 check별 mock fixture 2~3 case).
- `packs/ros2-jazzy/MANIFEST` + `SIGNATURE` (build 시 생성).

**수정 site**:
- `cmd/pack-tools/converter/ros2.go` — `buildBashCombine`에 preflight prepend 옵션 추가 (~30줄 보강, 기존 동작 default-off).
- `internal/app/scanrun/scanrun.go::executeOne` — pack의 `preflight.sourceScript` 처리 (~15줄, optional env injection).
- `internal/domain/scan/scan.go::CheckDef` — `Preflight` 필드 추가 (옵션, R2에서 본격 정착 가능).
- `cmd/pack-tools/builder/` — manifest + signature 빌드 (기존 CIS pack 빌드 절차 재사용).
- `Makefile` — `pack-ros2-jazzy` 타겟 추가.

**테스트**:
- 단위 — `converter/ros2_test.go`에 preflight prepend 케이스 3종.
- pack-tools selftest 통합 — 모든 check가 selftest fixture PASS/FAIL 정확 분기.
- 통합 — `scanrun/integration_test.go`에 ROS2 pack fakesshd 시나리오 1건 (env source 결선 검증).

### 7.2 R2 — C4·C5 (8~12 check)

**마이그레이션**: 0 (또는 plugin check type registry — D-ROS2-8 결정에 따라).

**신규 파일**:
- `packs/ros2-jazzy/checks/C4-*.yaml` (4~6개).
- `packs/ros2-jazzy/checks/C5-*.yaml` (4~6개).
- (옵션) `internal/domain/scan/checktype/` plugin registry — `ros2_launch_audit` type.

**수정 site**:
- `packs/ros2-jazzy/pack.yaml` — version 0.2.0.
- (옵션) `internal/app/scanrun/` — plugin dispatch.

**테스트**:
- 단위 — launch.py 정적 분석 (옵션) 또는 단순 grep 패턴 검증.
- 통합 — C4 binary 무결성 check fakesshd 시나리오.

### 7.3 사용자 합의 시점

- **R1 진입 합의** = 본 design doc commit 직후.
- **R2 진입 합의** = R1 마감 후 (실제 manual 비율 측정 후 R2 추정 보정).

---

## 8. TDD Stage 분해 (R1 = 5 commit + R2 = 3 commit = 8 commit)

memory `feedback_parallel_agents.md` — 매 Stage 시작 시 병렬 가능성 재평가. R1 Stage 2~6은 카테고리별 sub-agent worktree 병렬 가능(같은 round 안 6 worktree, 각각 1 카테고리).

### R1 Stage 분해 (5 commit)

**Stage 1**: pack scaffold + preflight 인프라
- `packs/ros2-jazzy/pack.yaml` + 빈 `checks/` `selftest/` 디렉토리.
- `converter/ros2.go::buildBashCombine` preflight 옵션 보강 + 단위 test (TDD red).
- `scanrun/executeOne` env injection 결선 + integration test (fakesshd).
- 커밋: `feat(packs): ros2-jazzy pack scaffold + preflight source 결선`.

**Stage 2**: C3 (ROS_DOMAIN_ID) + C6 (distro 버전) — 가장 쉬운 영역
- 3~5 + 3~4 = 6~9 check yaml + selftest fixture.
- check 작성 → selftest fixture로 PASS/FAIL/INDETERMINATE 검증.
- 커밋: `feat(packs): ros2-jazzy C3·C6 — 6~9 check (domain_id · distro_version)`.

**Stage 3**: C7 (rmw_implementation) + C8 (transport)
- 3~5 + 3~5 = 6~10 check.
- 커밋: `feat(packs): ros2-jazzy C7·C8 — 6~10 check (rmw · transport)`.

**Stage 4**: C1 (SROS2 / 노드 ID)
- 5~7 check. 가장 manual 비율 높음(30%) — fixture 작성 시간 보수 예상.
- 커밋: `feat(packs): ros2-jazzy C1 — 5~7 check (sros2 키스토어 · 인증)`.

**Stage 5**: C2 (토픽 권한) + R1 마감 통합
- 6~9 check.
- pack MANIFEST + SIGNATURE 빌드.
- `SESSION_HANDOFF.md` 갱신.
- 커밋: `feat(packs): ros2-jazzy C2 + R1 마감 — 6~9 check + sign · handoff 갱신`.

### R2 Stage 분해 (3 commit)

**Stage 1**: C4 binary 무결성 — 4~6 check.
**Stage 2**: C5 launch 안전 — 4~6 check (plugin check type 도입 시 별 마이그레이션).
**Stage 3**: pack version 0.2.0 bump + handoff + README 갱신.

### 누적 추정

- R1: Stage 1(0.3주) + Stage 2~5(각 0.4~0.6주) = **2~3주** (memory `feedback_design_doc_conservative.md` 일관, manual fixture 작성 부담 반영).
- R2: **1~1.5주**.
- 누적: **3~4.5주**.

---

## 9. 결정 항목 (D-ROS2-1 ~ D-ROS2-9)

memory `feedback_design_doc_first.md` 일관 — 모든 결정에 권장 default 명시. 다음 세션 즉시 Stage 1 진입 부담 0.

### D-ROS2-1 — 본 design doc 채택 + 옵션 B 진입

- (1) **채택 + 옵션 B 진입** (Round 1 MVP 6 카테고리 우선) (권장 default).
- (2) 옵션 A 일괄 — 4~6주 long epic.
- (3) 옵션 C 표준 대기 — 1년+ 보류.
- (4) 옵션 D 보류 — paying customer 요구까지.

**근거**: 옵션 B는 MVP 가치 즉시(2~3주) + 회귀 위험 분리 + customer 진입 timing 정합. D7 결정 이행을 미루지 않으면서 long epic 부담 회피.

### D-ROS2-2 — ROS2 distro 우선순위

- (1) **Jazzy LTS 단독 (v0.1.0)** (권장 default) — LTS + 가장 새로운 active. pack 이름 `ros2-jazzy`.
- (2) Jazzy + Humble 동시 (v0.1.0) — 두 LTS cover, pack 2개.
- (3) Iron 포함 — Iron은 2025-11 EOL이라 본 doc 시점(2026-05) EOL.
- (4) Humble 우선 — 더 오래된 LTS, 안정.

**근거**: Jazzy 단독은 Round 1 범위 단순화. Humble pack은 Jazzy pack의 변형으로 R3에서 추가 가능(yaml 90% 동일, 변경은 distro 이름·setup.bash 경로·LTS 버전 string만). EOL distro(Iron·Foxy)는 C6 check가 FAIL 처리 — pack 자체에서 cover 안 함.

### D-ROS2-3 — Baseline source (자동 변환 source)

- (1) **수동 작성 우선 + SROS2 docs + OSRF 권장 합성** (권장 default) — converter 자동 변환률 0~20%, 수동 작성으로 회피.
- (2) nrobotcheck 전신 framework JSON 재사용 — 한국어 baseline 30~60 items 즉시 변환 가능, 단 라이선스 검토 필요.
- (3) 두 옵션 hybrid — 옵션 1로 골격, 옵션 2를 보조 fixture source.
- (4) 외부 ROS2 표준 대기 (옵션 C와 동일).

**근거**: 옵션 1은 표준 source(SROS2 design articles + DDS Security Spec)로 작성하므로 감사인 인용 가능 + 라이선스 깨끗. 옵션 2는 nrobotcheck 자산 라이선스가 paid customer 1과의 협상 결과 → 다시 검토 필요. memory `feedback_naming_verification.md`와 같은 외부 의존 검증이 필요한 작업이라 본 doc 외부 의존 0 default 선호.

### D-ROS2-4 — rmw 구현체 우선 매트릭스 (C7)

- (1) **Cyclone DDS + Fast DDS 동시 cover** (권장 default) — Jazzy 기본은 Fast DDS이지만 Cyclone DDS는 ROS-I·industry 광범위 사용.
- (2) Fast DDS 단독 (Jazzy 기본).
- (3) Cyclone DDS 단독 (성능·안정성 우위 인용 가능).
- (4) RMW agnostic — 모든 implementation을 INDETERMINATE.

**근거**: 양쪽 cover로 customer 환경 다양성 흡수. 추가 check는 implementation별 2~3건 (총 3~5건 추정의 핵심 차이).

### D-ROS2-5 — sudo vs ros user 실행 권한

- (1) **ros user 권한 default + sudo 필요 check는 별 마킹** (권장 default) — `requiresSudo: true` 필드(설계서 §7.3에 이미 존재) 활용.
- (2) sudo 전제 — 모든 check가 sudo로 실행.
- (3) ros user 전제 — sudo 불필요 check만 cover.

**근거**: ROS2 cmd 대부분(`ros2 node list`·`ros2 topic info`·`env`)은 ros user로 충분. binary 무결성(C4)·일부 파일 perms는 sudo 필요 — 별 마킹으로 site 정책 분리. 옵션 2는 sudo 정책 제한 customer(보안 강한 환경)에서 모든 check 실행 불가.

### D-ROS2-6 — 인증 정책 baseline (C1)

- (1) **SROS2 활성 + DDS Security Spec v1.1 fully signed** (권장 default) — `ROS_SECURITY_ENABLE=true` + `ROS_SECURITY_STRATEGY=Enforce`.
- (2) SROS2 활성 + Permissive 모드 허용 — 점진적 도입 환경.
- (3) SROS2 미활성 환경 default — check가 INDETERMINATE.

**근거**: Enforce 모드는 spec 권장. Permissive는 enterprise 점진 전환 시 사용 가능하나, MVP pack은 strict baseline으로 작성하고 customer가 정책 변경 시 자체 fork 또는 별 pack 선택.

### D-ROS2-7 — launch 파일 검증 깊이 (C5, R2 영역)

- (1) **grep 패턴 검증만** (권장 default) — `ExecuteProcess(shell=True)`·`http://` URL·world-writable yaml 등 단순 패턴.
- (2) launch.py 정적 분석 — Python AST 파싱, plugin check type(`ros2_launch_audit`) 도입.
- (3) launch 실행 추적 — launch 실제 dry-run + 노드 spawn 차단 감시.
- (4) C5 보류 — R2에서 제외, R3 또는 plugin epic으로 미루기.

**근거**: 옵션 1은 R2 범위 단순화 + 결정론적. 옵션 2는 별 epic(plugin check type 도입)으로 분리하는 게 자연 — 본 doc R2는 옵션 1 default, 옵션 2는 backlog. 옵션 3은 customer 환경 의존도 높아 본 pack 범위 초과.

### D-ROS2-8 — Plugin check type (`ros2_topic_audit` · `ros2_launch_audit`)

- (1) **본 doc 범위 외 — Round 1·2 모두 SSH cmd만 사용** (권장 default).
- (2) R2 Stage 2에서 `ros2_launch_audit` plugin 도입.
- (3) R1 Stage 1에서 plugin registry 인프라 먼저 도입.

**근거**: 설계서 §7.3에 plugin check type 힌트만 존재 — registry 인프라 자체가 부재. 본 doc은 일반 SSH cmd path만 사용하고 plugin은 별 epic으로 분리. 옵션 2는 R2 추정 1주 추가 — 일정 압박.

### D-ROS2-9 — Selftest fixture 전략 (실 ROS2 환경 의존)

- (1) **mock stdout/stderr/exitCode 작성 default** (권장 default) — CIS pack 패턴과 동일. 실 robot 부재 시에도 selftest 가능.
- (2) 실 ROS2 환경 docker 컨테이너에서 cmd 실행 후 stdout capture — 정확도 높지만 CI 인프라 추가.
- (3) hybrid — 단순 check는 mock, 복잡한 check는 docker capture.

**근거**: mock fixture는 CIS pack에서 검증된 패턴. 본 pack도 동일 패턴으로 selftest 314→ROS2 N건 누적 가능. docker capture는 ROS2 distro별 (Jazzy·Humble) image 유지 부담 — 별 CI epic으로 분리. 옵션 3은 단계적 도입 가능하나 본 doc R1 단순화 우선.

---

## 10. 회귀 위험 / 운영 고려

### 10.1 회귀 위험 매트릭스

| 위험 | 발생 가능성 | 영향 | 완화 |
|---|---|---|---|
| ROS2 env source 결선이 non-ROS2 robot에 부작용 | 낮음 | 낮음 | preflight 실패 → INDETERMINATE 마킹. CIS pack에는 prepend 0(`preflight.sourceScript` 미정의). |
| `converter/ros2.go` preflight 옵션 추가가 기존 변환 회귀 | 낮음 | 중 | default-off + 기존 unit test 100% 유지. R1 Stage 1 TDD red→green. |
| selftest fixture가 실 robot 결과와 어긋남 | 중 | 중 | mock fixture는 paying customer 환경 reverse engineering으로 갱신. fixture diff는 customer feedback 채널로 회수. |
| C1·C2 manual 비율이 site policy 의존이라 false positive 다수 | 중 | 중 | check별 `requiresSiteContext: true` 필드(신규, R1 Stage 1에서 도입 가능)로 결과 dampening. UI에서 "site policy 의존" 배너. |
| Jazzy 외 distro(Humble·Iron) customer가 본 pack을 잘못 사용 | 중 | 낮음 | `compatibility.rosDistro: ["jazzy"]` strict 매칭 + scanrun preflight에서 mismatch 시 모두 INDETERMINATE. |
| paying customer rmw 구현체가 default 아닌 변형 | 중 | 낮음 | C7 check INDETERMINATE 마킹 + UI 진단 메시지로 customer 응답 유도. |
| pack 서명 키 회전 시 기존 customer 충격 | 낮음 | 중 | 설계서 §7.5 키 회전 절차 그대로 적용 — 본 pack도 동일. |

### 10.2 운영 고려

- **customer ROS2 distro 다양성**: D-ROS2-2에서 Jazzy 단독 default. Humble pack은 R3 또는 별 카드로 추가 — `packs/ros2-humble/`로 분기, 90% yaml 재사용.
- **scanrun executor preflight 결선**: ROS2 env source는 robot당 1회 (전 check 공통 prepend). 그러나 sshpool은 dial-on-acquire / close-on-release (scanrun-ssh-integration §1.4 G2 미해소) — preflight prepend가 check별 반복 실행되어 cost 누적. R2 시점에 sshpool idle 재사용(G2) 마감 후 한 번만 source하는 최적화 가능.
- **pack 자동 변환 한계**: CIS pack 94.6% 변환률은 본 pack에 적용 불가. 이는 paying customer 진입 마케팅 시점에 "ROS2 baseline은 OSRF·SROS2 표준 직접 합성"이라는 메시지로 활용 가능(자동 변환률 낮음이 단점이 아니라 결정론적 표준 인용의 결과).
- **D5 open-core 정합**: R1 6 카테고리는 코어 Apache-2.0. R2의 C4·C5는 enterprise tier 후보 — D5 분리 시점에 자연스럽게 그룹화.
- **pack release cadence**: ROS2 distro EOL(2년 cycle) + DDS Security Spec 갱신 + 새 CVE 발견 시 pack patch. baseline 갱신은 분기당 1회 추정.

### 10.3 D-ROS2-3 옵션 2(nrobotcheck 자산 재사용) 채택 시 추가 운영 고려

- nrobotcheck `resources/baselines/ros2_*_security_baseline_framework_*.json` 라이선스 재검토 필요.
- 한국어 baseline → 영어/한국어 dual title 결정 (converter에 `PreferEnglish` 옵션 이미 존재 — 양쪽 출력 가능).
- 변환된 check는 본 doc §3 패턴(preflight·rosDistro)으로 사후 수정 필요.

---

## 11. 참조

- `docs/design/notes/scanrun-ssh-integration-design.md` §6.1 옵션 D — 본 doc carryover 명시 (line 225).
- `docs/design/notes/customer-onboarding-design.md` — paying customer 진입 timing.
- `docs/design/00-mission-and-positioning.md` §0.1 미션 — "ROS2 로봇 플릿".
- `docs/design/07-scan-engine-and-benchmarks.md` §7.2~§7.3 — pack 구조 + `rosDistro` compatibility + plugin check type 힌트.
- `docs/design/11-tech-stack-and-roadmap.md` D7 결정 (2026-04-23) — "초기 타깃 = CIS Ubuntu 24.04 + ROS2 Jazzy baseline".
- 기존 pack: `packs/cis-ubuntu-2404/` (313 checks · 94.6% 자동 변환 · Manual 17 · Manual 21 design 완료).
- 기존 converter: `cmd/pack-tools/converter/ros2.go` (320줄, nrobotcheck framework JSON → rosshield pack).
- SROS2 official: `design.ros2.org/articles/ros2_dds_security.html`.
- DDS Security Spec v1.1: OMG.
- Mayoral-Vilches et al., "SROS2: Usable Cyber Security Tools for ROS 2" (학술 인용 후보).
- memory `feedback_design_doc_first.md` · `feedback_design_doc_conservative.md` · `feedback_parallel_agents.md` · `feedback_naming_verification.md`.

---

## 12. TL;DR

- **carryover 마감**: `scanrun-ssh-integration-design.md` §6.1 옵션 D 후반부 = ROS2 specific pack.
- **카테고리 8개** (C1~C8): 노드 ID·인증 / 토픽 권한 / ROS_DOMAIN_ID / binary 무결성 / launch 안전 / distro 버전 / rmw_implementation / transport encryption.
- **check 추정 31~47건**, 평균 manual 비율 ~32%.
- **권장 옵션 B**: Round 1 = MVP 6 카테고리(C1·C2·C3·C6·C7·C8, 24~35 check, 2~3주) + Round 2 = C4·C5(8~12 check, 1~1.5주). 누적 3~4.5주.
- **자동 변환률 매우 낮음 (0~20%)** — ROS2 표준 baseline 부재. 수동 작성이 default.
- **결정 항목 D-ROS2-1 ~ D-ROS2-9**, 각각 권장 default 명시.
- **본 doc 자체는 코드 0 / 마이그레이션 0 / pack 변경 0**.
- **솔루션 차별화 가치의 모든 무게**가 본 carryover에 실려 있음 — 미진척 지속 시 v0.4.0~v1.0.0 release까지 ROS2 check 0 상태로 진행됨.
