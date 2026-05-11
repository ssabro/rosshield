# Secure Boot Enrollment 가이드 (E34 Phase 5)

> **상태**: E34 Stage 1+2 완료 시점 운영 가이드. Stage 3 (bootstrap 결선) 완료
> 후 본 절차로 어플라이언스 양산 전 1회 점검을 수행합니다.
> **R40-1**: `core22` (Ubuntu Core 22), **R40-2**: `swtpm` (CI 시뮬레이터),
> **R41-1**: B (TPM Keystore + soft Signer), **R41-2**: `google/go-tpm-tools`,
> **R41-3**: PCR `[0, 2, 4, 7]`.
> **설계 출처**: `docs/design/notes/e34-tpm-design.md` §8 — 본 가이드는 운영 docs로 풀어 씀.

---

## 1. 개요

### 1.1 Ubuntu Core 22의 측정 부팅(measured boot)

Ubuntu Core 22는 부팅 단계마다 측정값(SHA-256)을 TPM 2.0 칩의 PCR
(Platform Configuration Register)에 누적 extend합니다. 단계는 다음과 같습니다:

```
UEFI firmware (PCR 0)
   ↓
OPROM — GPU·NIC 등 옵션 ROM (PCR 2)
   ↓
shim — Microsoft cross-signed 1차 부트로더 (PCR 4)
   ↓
grub — Canonical 서명 (PCR 4·8 누적)
   ↓
kernel snap — Canonical 서명 (PCR 11·12)
   ↓
rosshield-server (snap service) — TPM unseal 시도
```

### 1.2 PCR 7이 측정하는 것

PCR 7은 다른 PCR과 달리 **Secure Boot 정책 자체**(활성/비활성, 사용된 키 db
내용, 쉽게 변경되는 KEK·PK 변경 이벤트)를 측정합니다. 누군가 BIOS 진입 후
Secure Boot를 비활성화하거나 다른 OS 키를 등록하면 PCR 7이 즉시 변하므로,
PCR 7에 묶인 sealed blob은 unseal 불가가 됩니다. 이로써 **부트 체인 신뢰 근간**이
수학적으로 보장됩니다.

### 1.3 E34 sealing의 신뢰 모델

E34 Stage 2 구현은 **Microsoft + Canonical 기본 키만** 신뢰합니다 (Ubuntu Core 22
출고 상태 그대로). 사용자 자체 서명 키(rosshield PK·KEK·db 등) enrollment는
**옵션** — 본 가이드 §4 — 으로 두며 Phase 5 후속 또는 enterprise customer가
strict deployment를 명시 요청할 때만 수행합니다.

---

## 2. 사전 준비

### 2.1 UEFI Secure Boot 활성 확인

```bash
sudo mokutil --sb-state
# 정상: SecureBoot enabled
# 비정상: SecureBoot disabled  → BIOS 진입 후 활성화 (§7 트러블슈팅)
```

### 2.2 Ubuntu Core 22 install 완료

```bash
sudo snap version
# core 22  ← 이 줄이 있어야 함

cat /etc/os-release | grep VERSION
# VERSION="22 (Core)"
```

### 2.3 TPM 2.0 칩 정상 동작 확인

`tpm2-tools`는 Ubuntu Core에 기본 포함되지 않으므로 별도 설치합니다:

```bash
sudo snap install tpm2-tools --dangerous   # store 미발행 시
# 또는 strict 환경: sudo apt-get install tpm2-tools (Classic snap deployment)

sudo tpm2_pcrread sha256:0,2,4,7
# 정상: 4개 PCR 각각 64자 hex 출력 (0이 아닌 값 — 이미 부팅 측정됨)
# 비정상: "ERROR ... TPM device not available" → §7 트러블슈팅
```

### 2.4 BIOS UEFI mode 확인

Legacy BIOS / CSM이 활성이면 측정 부팅이 동작하지 않습니다:

```bash
[ -d /sys/firmware/efi ] && echo "UEFI mode" || echo "Legacy mode"
# 정상: UEFI mode
# 비정상: Legacy mode → BIOS setup → Boot mode → UEFI only
```

---

## 3. Microsoft + Canonical 기본 키 (사용자 액션 0)

Ubuntu Core 22는 install 시점에 다음 키 chain을 자동으로 신뢰합니다.
**운영자가 추가로 할 일은 없습니다** — 본 절은 무엇이 신뢰되는지를
이해하기 위한 참고입니다.

### 3.1 출고 상태에서 enroll된 키 (UEFI db / dbx)

| 키 | 용도 | 출처 |
|---|---|---|
| **Microsoft Corporation UEFI CA 2011** | shim 검증 | Microsoft third-party UEFI CA — Linux distro shim cross-sign |
| **Microsoft Windows Production PCA 2011** | Windows boot 호환 | Windows dual-boot 시나리오 (rosshield 어플라이언스에서는 미사용) |
| **Canonical Ltd. Master Certificate Authority** | grub·kernel·snap 검증 | Canonical 자체 키 — Ubuntu Core 모든 컴포넌트 서명 근간 |

### 3.2 검증 방법

```bash
sudo mokutil --list-enrolled
# Microsoft Corporation UEFI CA 2011 출력 확인

sudo mokutil --pk
sudo mokutil --kek
sudo mokutil --db   # 신뢰 db에 위 3개 키가 모두 있어야 함
```

### 3.3 snap kernel 서명 체크

```bash
sudo snap info pc-kernel
# publisher: canonical** (verified)  ← Canonical 공식 서명

sudo snap known model | grep authority-id
# authority-id: canonical
```

### 3.4 결론

기본 enrollment 상태에서 PCR 7에는 **"Microsoft + Canonical 키로 검증된
정상 부트"** 사실이 측정됩니다. E34 Stage 2 sealing 정책(`[0, 2, 4, 7]`)은
이 PCR 7 값에 자동으로 묶이므로, **운영자 액션 0**으로 §1 위협 모델 3종
(디스크 도난·부트 체인 변조·user-space 침해)을 모두 차단합니다.

---

## 4. 사용자 키 enrollment (옵션 — enterprise)

이 절은 **enterprise customer가 자체 서명 부트로더·kernel을 사용하는
시나리오**에만 해당합니다. Phase 5 1차 범위는 §3 기본 키만 사용 — 본 절은
후속 enterprise build 또는 strict deployment 요청 시에만 수행하세요.

### 4.1 시나리오

- 보안 정책상 Microsoft cross-signed shim을 신뢰할 수 없음 (정부·국방)
- 자체 서명 kernel을 사용하여 Canonical 공식 chain을 우회해야 함
- 부트 체인 전체에 customer 자체 PK·KEK·db만 enroll

### 4.2 키 생성 (build host에서 1회)

```bash
# Platform Key (PK) — 최상위
openssl req -new -x509 -newkey rsa:2048 -nodes \
  -keyout rosshield-PK.key -out rosshield-PK.crt \
  -days 3650 -subj "/CN=rosshield Platform Key"

# Key Exchange Key (KEK) — db 갱신 권한
openssl req -new -x509 -newkey rsa:2048 -nodes \
  -keyout rosshield-KEK.key -out rosshield-KEK.crt \
  -days 3650 -subj "/CN=rosshield KEK"

# Signature Database (db) — 부트로더·kernel 서명 키
openssl req -new -x509 -newkey rsa:2048 -nodes \
  -keyout rosshield-db.key -out rosshield-db.crt \
  -days 3650 -subj "/CN=rosshield db"

# UEFI 등록 형식으로 변환 (DER)
openssl x509 -in rosshield-PK.crt  -outform DER -out rosshield-PK.der
openssl x509 -in rosshield-KEK.crt -outform DER -out rosshield-KEK.der
openssl x509 -in rosshield-db.crt  -outform DER -out rosshield-db.der
```

`*.key`는 build host의 안전한 vault(HSM·sealed backup)에 보관. 분실 시 키
재생성 + 모든 어플라이언스 재enrollment 필요.

### 4.3 어플라이언스 enrollment

```bash
# 1. db 키를 MOK(Machine Owner Key)에 import
sudo mokutil --import rosshield-db.der

# 2. enroll password 설정 (재부팅 후 MOK Manager TUI 입력용)
# Input password:
# Input password again:

# 3. 재부팅
sudo reboot
```

### 4.4 재부팅 후 MOK Manager TUI

부팅 직후 파란 MOK Manager 화면이 자동으로 뜸. **10초 안에 키보드를
누르지 않으면 enrollment 취소되고 일반 부팅** 진행. 절차:

```
Perform MOK management → Enroll MOK → View key 0 → Continue
   → Enroll the key(s)? → Yes → 위에서 설정한 password 입력
   → Reboot
```

### 4.5 shim·kernel 자체 서명

shim과의 통합은 `sbsign` (Secure Boot Signing) 도구로 수행:

```bash
sudo apt-get install sbsigntool

# rosshield 자체 빌드 shim 서명
sbsign --key rosshield-db.key --cert rosshield-db.crt \
  --output shim-rosshield-signed.efi shim-original.efi

# kernel snap 서명 (Ubuntu Core build 파이프라인에 통합)
sbsign --key rosshield-db.key --cert rosshield-db.crt \
  --output pc-kernel-signed.snap pc-kernel-unsigned.snap

# 검증
sbverify --cert rosshield-db.crt shim-rosshield-signed.efi
# Signature verification OK
```

### 4.6 PCR 재봉인 (필수)

> ⚠️ **중요**: enrollment 직후 PCR 7이 변경됩니다. 기존 sealed blob은 unseal
> 실패 → rosshield-server가 부팅하지 못합니다. 즉시 §5 재봉인 절차 실행.

```bash
# rosshield 임시 fallback (file keystore)
sudo snap set rosshield keystore=file
sudo snap restart rosshield.server

# 새 PCR 측정값 확인
sudo tpm2_pcrread sha256:7
# 이전 값과 다른 hex → enrollment 반영 확인

# TPM keystore로 재봉인
sudo rm /var/snap/rosshield/common/keys/tpm/*.sealed
sudo rm /var/snap/rosshield/common/keys/tpm/*.sealed.meta.json
sudo snap set rosshield keystore=tpm
sudo snap restart rosshield.server

# unseal 정상 동작 확인
curl http://localhost:8080/healthz | jq .components.signer
# {"status":"ok","keyID":"...","keystore":"tpm"}
```

---

## 5. PCR 검증 + 봉인 절차

본 절은 **모든 어플라이언스 양산 전 1회** 수행하는 표준 절차입니다.

### 5.1 봉인 직전 PCR 스냅샷 기록

운영 중 PCR 변조 진단의 baseline으로 사용:

```bash
sudo mkdir -p /var/snap/rosshield/common/keys/tpm
sudo tpm2_pcrread sha256:0,2,4,7 \
  | sudo tee /var/snap/rosshield/common/keys/tpm/pcr-snapshot.txt
sudo cat /var/snap/rosshield/common/keys/tpm/pcr-snapshot.txt
# sha256:
#   0 : 0xa3f1c9...
#   2 : 0x000000...   (OPROM 없으면 0)
#   4 : 0x1b2e7a...
#   7 : 0x8c4d3f...
```

이 파일은 비밀이 아닙니다 — PCR 값은 ROOT of trust의 측정 결과일 뿐 키
추출에 사용 불가합니다.

### 5.2 snap install + 첫 부팅 자동 봉인

E34 Stage 2 구현이 keystore=tpm에서 자동으로 동작합니다:

```bash
# snap install (snap-deployment.md §3 참조)
sudo snap install rosshield_<version>_amd64.snap --dangerous

# keystore=tpm 활성 (기본 file → tpm 전환)
sudo snap set rosshield keystore=tpm
sudo snap restart rosshield.server

# 봉인 로그 확인
sudo snap logs rosshield.server -n 50 | grep -E "tpm:|keystore"
# 예시:
# tpm: sealed key handle=audit-chain pcr=[0,2,4,7]
# keystore: type=tpm sealed_dir=/var/snap/rosshield/common/keys/tpm
```

### 5.3 sealed blob 파일 확인

```bash
sudo ls -la /var/snap/rosshield/common/keys/tpm/
# audit-chain.sealed              (sealed blob — TPM 외부 사용 불가)
# audit-chain.sealed.meta.json    (PCR 정책 스냅샷 — 진단용 메타)
# pcr-snapshot.txt                (5.1에서 기록한 baseline)

sudo cat /var/snap/rosshield/common/keys/tpm/audit-chain.sealed.meta.json
# {
#   "pcrSelection": [0, 2, 4, 7],
#   "pcrSnapshot": { "0": "a3f1c9...", "2": "00000...", "4": "1b2e7a...", "7": "8c4d3f..." },
#   "sealedAt": "2026-05-15T10:00:00Z",
#   "tpmFwVersion": "STM_3.71",
#   "rosshield-server-version": "0.5.0"
# }
```

### 5.4 재부팅 후 unseal 검증

```bash
sudo reboot
# 부팅 후
sudo systemctl status snap.rosshield.server
# active (running) ← unseal 성공

curl http://localhost:8080/healthz | jq .
# {
#   "status": "ok",
#   "components": {
#     "storage": "ok",
#     "signer": { "keyID": "ed25519:Hf...", "keystore": "tpm" }
#   }
# }
```

같은 keyID가 재부팅 전후 동일해야 정상입니다.

### 5.5 BIOS update 후 시나리오 (PCR 0 변조)

운영 중 BIOS update가 발생하면 PCR 0이 즉시 변경되어 unseal 실패합니다.
journal에서 다음 패턴을 발견하면 §6 복구 절차로 이동:

```
tpm: unseal failed: TPM_RC_POLICY_FAIL — PCR mismatch
tpm: expected pcr[0]=a3f1c9... actual=4e7d2b...
keystore: failed to load handle=audit-chain
rosshield.server: bootstrap failed: signer init error
```

---

## 6. PCR 변조 시나리오와 복구

### 6.1 변조 원인별 영향 표

| 변조 원인 | 변경되는 PCR | 복구 방향 |
|---|---|---|
| BIOS firmware update | PCR 0 | file fallback → BIOS 안정화 후 재봉인 |
| Secure Boot 키 변경 (PK·KEK·db) | PCR 7 | file fallback → 키 변경 확정 후 재봉인 |
| 부트로더(shim·grub) 업그레이드 | PCR 4 | file fallback → 부트로더 안정화 후 재봉인 |
| OPROM 변경 (GPU·NIC 카드 add/remove) | PCR 2 | file fallback → HW 변경 확정 후 재봉인 |
| (정상 운영) snap update·sysctl 변경 | PCR 변경 없음 | 정상 동작, 복구 불필요 |

### 6.2 표준 복구 절차 (모든 PCR 변조 공통)

```bash
# 1. 임시 fallback (TPM 우회, 디스크 평문 키 사용)
sudo snap set rosshield keystore=file
sudo snap restart rosshield.server

# 2. 정상 동작 확인
curl http://localhost:8080/healthz | jq .components.signer
# {"keystore":"file"} ← 임시 모드

# 3. 변조 원인 안정화 (BIOS update 완료, 카드 변경 완료 등)

# 4. 새 PCR 값 기록 (baseline 갱신)
sudo tpm2_pcrread sha256:0,2,4,7 \
  | sudo tee /var/snap/rosshield/common/keys/tpm/pcr-snapshot.txt

# 5. 기존 sealed blob 삭제 (unseal 불가능하므로 안전)
sudo rm /var/snap/rosshield/common/keys/tpm/*.sealed
sudo rm /var/snap/rosshield/common/keys/tpm/*.sealed.meta.json

# 6. TPM keystore 재활성 + 자동 재봉인
sudo snap set rosshield keystore=tpm
sudo snap restart rosshield.server

# 7. 재봉인 결과 검증
sudo snap logs rosshield.server -n 20 | grep "tpm: sealed key"
curl http://localhost:8080/healthz | jq .components.signer
# {"keystore":"tpm","keyID":"ed25519:..."}
```

### 6.3 keyID 변경의 audit chain 영향

> ⚠️ 재봉인은 **새 ed25519 키 페어를 생성**합니다 (§7 한계 참조). 기존 keyID로
> 서명된 audit chain checkpoint는 새 keyID와 단절됩니다. 외부 감사인 검증
> 시 **두 keyID를 모두 신뢰 chain에 포함**해야 합니다.

```bash
# 새 keyID를 audit log에 기록
curl -X POST http://localhost:8080/api/v1/audit/key-rotation \
  -H "Authorization: Bearer <admin-token>" \
  -d '{"reason":"tpm-reseal-after-bios-update","oldKeyID":"...","newKeyID":"..."}'
```

이는 audit chain의 **자연스러운 키 회전** 이벤트로 처리되며, append-only
원칙(원칙 9)을 위반하지 않습니다.

---

## 7. 트러블슈팅 + 한계

### 7.1 트러블슈팅 표

| 증상 | 원인 | 해결 |
|---|---|---|
| `mokutil --sb-state` returns "disabled" | UEFI Secure Boot off | BIOS setup → Security → Secure Boot → Enable → save & reboot |
| `tpm2_pcrread` permission denied | tpm2-abrmd 미가동 또는 user가 tss 그룹 미포함 | `sudo systemctl start tpm2-abrmd` + `sudo usermod -aG tss <user>` + 재로그인 |
| snap에서 keystore=tpm 부팅 실패 with `ErrTpmDeviceNotAvailable` | `/dev/tpm*` 권한 또는 strict confined plug 미연결 | `sudo snap connect rosshield:tpm core:tpm` (slot 자동 연결되지 않은 경우) |
| Unseal 후 keyID가 매번 다름 | sealed blob 파일이 매번 새로 생성됨 (재봉인 버그) | `sudo snap logs rosshield.server` + sealed blob 경로 점검 (`ls -la /var/snap/rosshield/common/keys/tpm/`) |
| BIOS reset 후 모든 sealed blob 무효화 | PCR 0 reset (factory default) — TPM도 함께 reset된 경우 EK 변경 | §6 표준 복구 + audit chain key-rotation 이벤트 기록 |
| `mokutil --import` 후에도 PCR 7 그대로 | MOK Manager TUI에서 enroll 미완료 | 재부팅 → MOK 화면에서 keyboard 입력 (10초 timeout) → Enroll the key(s) → Yes |
| TPM lockout — `TPM_RC_LOCKOUT` 에러 | TPM 인증 실패 누적 (DA — Dictionary Attack) | `sudo tpm2_dictionarylockout --clear-lockout --auth=<lockout-pw>` (lockout 비밀번호 모르면 TPM clear 필요 — 모든 sealed blob 손실) |

### 7.2 한계 (Phase 5 1차)

1. **사용자 키 enrollment 자동화 미지원** — §4 절차는 `mokutil --import` +
   MOK Manager TUI 수동 조작 필수. snap configure hook으로 자동화하려면
   firstboot detection·MOK password 비대화 입력이 필요한데, snap strict
   confinement에서는 `/dev/tpm0` plug만 허용되고 mokutil에 필요한
   `/sys/firmware/efi/efivars` write는 제한됨. enterprise 후속 epic 영역.

2. **TPM 2.0 dictionary attack lockout 정책 미설정** — TPM 2.0 spec은
   인증 실패 횟수에 따라 칩을 lockout시키는 DA 방어 메커니즘을 정의하지만,
   E34 Stage 2 구현은 기본값(vendor 권장)에 의존. snap 자체 정책으로 추가
   설정하지 않습니다. enterprise 환경에서 매우 빈번한 unseal 시도가
   필요하다면 vendor tooling으로 별도 튜닝 필요.

3. **봉인된 키 backup·복구 메커니즘 미제공** — TPM 손상·HW 교체 시 sealed
   blob은 영구 unseal 불가. 새 ed25519 키를 생성하여 재봉인하면 keyID가
   바뀌므로 audit chain은 키 회전 이벤트(§6.3)로 단절 표시됩니다.
   Cross-TPM 키 마이그레이션(EK migration·duplication)은 enterprise epic
   후보 — TPM 2.0 spec의 `TPM2_Duplicate` 명령 사용 필요.

4. **arm64 + Secure Boot는 Phase 5 후속 검증** — E33 snap arm64 빌드 자체가
   carryover 상태(snap-deployment.md §9). arm64 어플라이언스의 측정 부팅
   체인(특히 ARM Trusted Firmware → U-Boot → grub 단계)은 amd64와 PCR 의미가
   다를 수 있어 별도 검증 단계 필요. Phase 5 1차는 amd64만 보증.

---

## 8. 참조

- `docs/design/notes/e34-tpm-design.md` §8 Secure Boot enrollment — 본 가이드의 design 출처
- `docs/operations/snap-deployment.md` §4 설정 — `snap set rosshield <key>=<value>` 패턴
- `docs/operations/ha-deployment.md` — HA + TPM 결합 시 leader/follower epoch 영향 (현재 epoch는 TPM과 무관 — keystore=file과 동일 동작)
- Ubuntu Core 22 Measured Boot 공식 docs: <https://ubuntu.com/core/docs/uc20/full-disk-encryption>
- TPM 2.0 Library Specification, Part 1 (Architecture) §11 — Sealing
- `mokutil(1)` man page — MOK 등록·조회·삭제 인터페이스
- `tpm2-tools` 공식: <https://tpm2-tools.readthedocs.io/>
- `sbsigntool` (Secure Boot signing): <https://manpages.ubuntu.com/manpages/jammy/man1/sbsign.1.html>
- "A Practical Guide to TPM 2.0" (Will Arthur, David Challener) — sealing chapter
