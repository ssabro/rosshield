# ROS2 Humble Pack 운영자 가이드

> **대상**: ROS2 Humble Hawksbill LTS 분기 customer (Ubuntu 22.04 Jammy baseline) — Lodestar v0.11.0 이상.
> **참조 release**: [v0.11.0](../releases/v0.11.0.md) — Phase 10 옵션 E 마감.
> **참조 design**: [`docs/design/notes/ros2-humble-dds-sros2-design.md`](../design/notes/ros2-humble-dds-sros2-design.md) — Stage 분해 + D-P10E-1·2·3·4 결정.
> **EOL 주의**: ROS2 Humble은 **2027-05** EOL — `distro_not_eol` check가 2027-05 이후 customer 진입 시 FAIL 처리. Jazzy (2029-05 EOL) 마이그레이션 별 carryover.

---

## 1. 개요

### 1.1 무엇이 cover되었나

`packs/ros2-humble/` (v0.1.0) — 29 check, C1~C8 8/8 카테고리 cover. `packs/ros2-jazzy/` (22 → 29 check 자동 확장)와 동일한 깊이.

- **C1 SROS2 / 인증** (5 check): keystore exists · security enable · cert expiry ≥90일 · CA SHA-256 fingerprint trust · CRL nextUpdate ≥90일.
- **C2 토픽 권한** (6 check): `/cmd_vel` ACL/publisher count + `/scan` · `/odom` · `/tf+/tf_static` · `/joint_states` ACL.
- **C3 ROS_DOMAIN_ID** (1 check): domain_id_set.
- **C4 binary 무결성** (6 check): apt key · apt source 공식 · colcon install hash · world-writable libs · signed packages · systemd unit perms.
- **C5 launch 안전** (6 check): argv remote URL · lifecycle node · shell exec · world-writable yaml · parameter secret inline · param files owner.
- **C6 distro lifecycle** (3 check): LTS · not EOL · ros2 CLI.
- **C7 RMW** (1 check): rmw_implementation_set.
- **C8 governance encryption** (1 check): governance_encrypt_topics.

### 1.2 Jazzy customer 병행 가능

본 humble pack은 jazzy pack과 독립 install — 한 tenant 안에서 두 pack 동시 활성 가능 (Humble fleet + Jazzy fleet 혼합 운영 customer cover). robot별 적용 pack은 fleet metadata 또는 tenant policy로 분기.

---

## 2. humble pack 활성 절차

### 2.1 source-only 빌드 (default — v0.11.0 carryover)

본 release는 humble pack archive를 `internal/builtin/packs/_archives/` 자동 embed에 포함하지 않습니다. customer 환경에서 직접 archive + 서명 후 install:

```bash
# 1. pack-tools 빌드.
make pack-tools-build

# 2. (첫 사용) dev signer key 생성 — release-level signer key는 별 secret store (CI).
bin/pack-tools keygen \
  -out scripts/dev-pack-signer.key \
  -pub-out scripts/dev-pack-signer.pub.hex

# 3. humble pack archive + 서명.
bin/pack-tools archive \
  -input packs/ros2-humble \
  -signer-key scripts/dev-pack-signer.key \
  -output packs/ros2-humble.tar.gz \
  -force

# 기대 출력:
# ✓ Archived packs/ros2-humble → packs/ros2-humble.tar.gz (29 check, signed Ed25519)
```

### 2.2 tenant pack 등록

```bash
# admin 권한으로 rosshield CLI.
rosshield pack install packs/ros2-humble.tar.gz \
  --tenant <tenant_id>

# 기대 출력:
# Pack ros2-humble v0.1.0 installed (29 check, signature verified).
```

### 2.3 fleet metadata 매칭 (ROS_DISTRO=humble)

robot agent가 ROS_DISTRO 환경 변수를 보고 적합 pack을 자동 선택하도록 fleet metadata에 `ros_distro: humble` 명시. policy 분기는 tenant rule 측 책임 (별 epic).

---

## 3. SROS2 keystore 사전 등록 (권장)

신규 C1 SROS2 cert chain 3 check는 site policy 의존 default — env 미설정 시 PASS skip (false positive 회피). 깊이 cover 활성을 원하면 robot agent 환경에 다음 env 사전 등록:

### 3.1 `ROSSHIELD_SROS2_KEYSTORE`

SROS2 keystore 절대 경로. default `$HOME/sros2_keystore`.

```bash
# robot agent systemd unit 또는 launch profile에 export.
export ROSSHIELD_SROS2_KEYSTORE=/opt/ros/keystore
```

keystore 디렉터리 구조 (SROS2 표준):

```
/opt/ros/keystore/
├── ca.cert.pem            # Trust anchor (self-signed CA)
├── ca.key.pem             # CA private key (root signer)
├── ca.crl.pem             # Certificate Revocation List
├── enclaves/
│   ├── /talker/cert.pem   # per-node identity cert
│   ├── /talker/key.pem
│   ├── /listener/cert.pem
│   └── ...
└── permissions.xml        # DDS topic ACL (C2 check 입력)
```

### 3.2 `ROSSHIELD_SROS2_CA_FINGERPRINT_SHA256`

CA `ca.cert.pem`의 SHA-256 fingerprint hex 64자 (대문자, `:` 없는 형태).

#### 3.2.1 fingerprint 계산

```bash
openssl x509 -in /opt/ros/keystore/ca.cert.pem -sha256 -fingerprint -noout \
  | cut -d= -f2 \
  | tr -d : \
  | tr a-f A-F

# 기대 출력 (예시):
# 5C8B3E4D9F2A1B6C7E0D3F2A1B6C7E0D3F2A1B6C7E0D3F2A1B6C7E0D3F2A1B6C
```

#### 3.2.2 env 등록

```bash
export ROSSHIELD_SROS2_CA_FINGERPRINT_SHA256=5C8B3E4D9F2A1B6C7E0D3F2A1B6C7E0D3F2A1B6C7E0D3F2A1B6C7E0D3F2A1B6C
```

미설정 시 `sros2_ca_trust` check는 PASS skip — site policy 의존 default 일관 (false positive 회피).

---

## 4. DDS topic ACL whitelist 정책

### 4.1 현재 cover (5 topic)

| Topic | C2 check | 위협 |
|---|---|---|
| `/cmd_vel` | `cmd_vel_acl_enforced` + `cmd_vel_publisher_count` | 외부 침입자 명령 주입 (mobile platform 충돌) |
| `/scan` | `scan_acl_enforced` | LiDAR raw 데이터 유출 (실내 평면도 추출) |
| `/odom` | `odom_acl_enforced` | 로봇 위치 추적 (nav stack frame_id) |
| `/tf` + `/tf_static` | `tf_acl_enforced` (통합 1 check) | Transform tree 변조 (좌표계 spoof) |
| `/joint_states` | `joint_states_acl_enforced` | manipulator joint position 추적 (산업 IP) |

### 4.2 permissions.xml 예시

```xml
<?xml version="1.0" encoding="UTF-8"?>
<permissions xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
  <grant name="/scan_node">
    <subject_name>CN=/scan_node</subject_name>
    <validity>
      <not_before>2026-05-21T00:00:00</not_before>
      <not_after>2027-05-21T00:00:00</not_after>
    </validity>
    <allow_rule>
      <domains><id>0</id></domains>
      <publish>
        <topics>
          <topic>rt/scan</topic>
        </topics>
      </publish>
    </allow_rule>
  </grant>
</permissions>
```

### 4.3 whitelist expansion (carryover)

추가 topic (`/diagnostics` · `/rosout` · custom payload 등) cover는 별 epic (v0.11.0 carryover). 본 release는 5 topic baseline만.

---

## 5. cert/CRL rotation 절차

### 5.1 baseline (≥90일, quarterly cadence)

D-P10E-4 권장 default — cert expiry threshold 90일. quarterly cert rotation cadence와 정합.

| check | 조건 | 실패 시 |
|---|---|---|
| `sros2_cert_expiry` | enclave cert가 ≥90일 미래 만료 | rotation 지침 출력 |
| `sros2_ca_trust` | CA fingerprint env와 일치 | 침입 의심 — 즉시 알람 |
| `sros2_cert_revocation` | CRL `nextUpdate`가 ≥90일 미래 | CRL 갱신 절차 트리거 |

### 5.2 cert rotation 절차 (예시)

```bash
# 1. 새 enclave cert 생성.
ros2 security create_enclave \
  $ROSSHIELD_SROS2_KEYSTORE \
  /<node_name> \
  --force

# 2. 기존 cert 백업.
mv $ROSSHIELD_SROS2_KEYSTORE/enclaves/<node_name>/cert.pem \
   $ROSSHIELD_SROS2_KEYSTORE/enclaves/<node_name>/cert.pem.bak.$(date +%Y%m%d)

# 3. 새 cert 활성 + node restart.
systemctl restart <node_service>

# 4. Lodestar scan 재실행 — sros2_cert_expiry 다시 PASS 확인.
rosshield scan --pack ros2-humble --robot <robot_id>
```

### 5.3 CRL 갱신

```bash
# CA signer 환경에서 (별 보안 host).
openssl ca -gencrl -crldays 90 \
  -keyfile /secure/ca.key.pem \
  -cert /secure/ca.cert.pem \
  -out /tmp/ca.crl.pem

# robot agent에 배포.
scp /tmp/ca.crl.pem robot:/opt/ros/keystore/ca.crl.pem
```

---

## 6. 한계 + carryover

### 6.1 v0.11.0 한계

- **Humble distro 실 환경 검증 부재** — 본 pack 22 check + 깊이 확장 7 check 모두 jazzy fixture cargo cult + distro 4 영역 갱신만으로 확정. 첫 Humble customer 진입 시 false positive feedback 수집 + selftest fixture 보완 round 필요.
- **humble pack archive auto-embed 미진행** — source-only 노출. customer가 `bin/pack-tools archive` 직접 호출 또는 `Makefile PACKS_SOURCE` 별 patch 등록.
- **SROS2 cert chain 다단계 미cover** — `openssl verify` 1단 검증만. intermediate CA 다단계 + OCSP responder는 별 carryover.
- **SROS2 keystore 자동 enrollment 미구현** — 운영자 manual 사전 등록. 자동 enrollment workflow는 Phase 11+ 위임.
- **DDS topic whitelist expansion** — `/diagnostics` · `/rosout` · custom payload topic은 별 epic.

### 6.2 EOL 카운트다운

ROS2 Humble은 **2027-05 EOL** — Jazzy (2029-05 EOL) 대비 2년 짧음. 2027-05 이후 customer는 `distro_not_eol` check FAIL — Jazzy 마이그레이션 가이드는 별 carryover (Phase 11+).

### 6.3 customer feedback 경로

Humble 환경 false positive 또는 missing check는 GitHub issue (`label: pack:ros2-humble`) 또는 customer 채널로 보고. 첫 enterprise customer 진입 시 별 patch round 계획.

---

## 7. 참조

- [v0.11.0 release notes](../releases/v0.11.0.md) — Phase 10 옵션 E 마감 전체 요약.
- [ros2-humble-dds-sros2-design.md](../design/notes/ros2-humble-dds-sros2-design.md) — design doc (§4 옵션 비교 + §6 Stage 분해 + §7 결정 항목).
- [ros2-baseline-pack-design.md](../design/notes/ros2-baseline-pack-design.md) — Jazzy 1차 design doc (Round 1+2+3 결선).
- [SROS2 Tutorial](https://design.ros2.org/articles/ros2_dds_security.html) — DDS Security Spec v1.1 인터페이스.
- [ROS2 Humble Docs](https://docs.ros.org/en/humble/) — 공식 distro 문서.
