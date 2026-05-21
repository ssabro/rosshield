# fg-verify (rosshield-audit-verify) — 외부 감사인 가이드

`rosshield-audit-verify`(통칭 fg-verify)는 외부 감사인이 rosshield-server 의존 없이
단독으로 audit chain · report bundle · audit export bundle 의 무결성·진위를 검증할 수
있는 standalone binary 입니다. Phase 10.D-5 (v0.10.0+) 부터 **audit chain key rotation
aware** 검증을 지원합니다 — epoch 별 public key 를 자동 사용해 과거·현재 entry 모두
검증.

## 검증 가능한 번들 종류

| 서브커맨드 | 입력 형식 | 산출 | 의도 |
|---|---|---|---|
| (default) `--bundle` | report tar.gz | PDF + Ed25519 signature + anchor JSON | 보고서 외부 검증 (E30) |
| `rotation` | segment tar.gz | manifest + entries NDJSON | cold archive 무결성 (E32) |
| `export` | NDJSON+gzip | entries + signature + chainKeyEpochs | audit chain entries 외부 검증 (Phase 10.D-5) |

본 문서는 **`export` 서브커맨드** 와 **rotation aware 검증** 절차를 다룹니다. 다른
서브커맨드는 README 참조.

## v1 vs v2 bundle

audit export bundle 의 wire 형식은 v1·v2 두 가지가 공존합니다. fg-verify 는 양쪽 모두를
자동 판별·검증합니다 — backward compatibility 는 v0.10.0+ 에서도 엄격 유지됩니다.

### v1 (~v0.9.0)

- signature line 에 `_bundleVersion` **부재**.
- entry line 에 `keyEpoch` **부재** (또는 nil).
- signature line 의 단일 `_publicKey` 로 전체 entry stream 검증.
- 단일 epoch (=1) 가정 — rotation 적용 이전 bundle.

### v2 (v0.10.0+)

- signature line 에 `_bundleVersion: "v2"` **명시**.
- signature line 에 `_chainKeyEpochs[]` 배열 포함 — `audit_chain_keys` 테이블 snapshot:
  - `epoch` (1 부터 단조 증가)
  - `keyId` (Ed25519 public key 의 short fingerprint)
  - `publicKeyHex` (32B Ed25519 public key 의 hex)
  - `createdAt` · `revokedAt` (RFC3339Nano UTC; 활성 epoch 는 `revokedAt` omit)
- 각 entry line 에 `keyEpoch` 필드 — INSERT 시점의 활성 epoch.
- signature line 의 `_keyId` 가 `_chainKeyEpochs` 안 한 row 의 `keyId` 와 매칭 — 해당
  epoch 의 `publicKeyHex` 로 검증.

### 자동 판별

fg-verify 는 signature line 의 `_bundleVersion` 필드 유무로 자동 분기합니다 — 호출자가
`--bundle-version` 같은 옵션을 지정할 필요 없음. v1 bundle 도 변경 없이 PASS.

## 사용법

### 기본 검증

```bash
rosshield-audit-verify export \
    --bundle /path/to/audit-export.ndjson.gz
```

출력 예 (v2 bundle, 모두 PASS):

```
RESULT           PASS
bundle           /path/to/audit-export.ndjson.gz
bundleSha256     <hex64>
bundleVersion    v2
entryCount       5
epochCount       3
signingKeyId     key_e<8hex>
fromSeq          1
toSeq            5
rotationEntries  2

STEPS:
  fetch              PASS  1057 bytes
  gunzip             PASS  1893 ndjson bytes
  parse              PASS  bundleVersion=v2 keyId=key_e... from=1 to=5 epochs=3
  digestRecompute    PASS  sha256(entries) == _signedDigest
  signature          PASS  ed25519.Verify OK (key=key_e...)
  chain              PASS  5 entries hash-linked
  epochTransition    PASS  2 rotation entries verified

PASS — audit export bundle verification successful.
```

### JSON 출력

```bash
rosshield-audit-verify export \
    --bundle /path/to/audit-export.ndjson.gz \
    --format json
```

CI 또는 자동화 환경에서 machine-readable 결과가 필요한 경우 사용. exit code 0=PASS,
1=FAIL, 2=ARG.

## 외부 감사인이 epoch 별 public key 를 신뢰하는 절차

v2 bundle 의 `_chainKeyEpochs[]` 가 belong 신뢰 chain 의 출발점입니다. 다음 두 절차 중
하나 (또는 둘 다) 를 권장합니다.

### 절차 A — bundle 안 chainKeyEpochs 를 그대로 신뢰

v2 bundle 자체가 signing key 의 epoch transition history 를 self-contained 로 보존합니다.
fg-verify 는 다음을 자동 검증합니다:

1. signature line 의 `_signature` 가 `_chainKeyEpochs[k where k.keyId == _keyId].publicKeyHex`
   로 `_signedDigest` 검증을 통과.
2. 모든 `audit.chain.key_rotated` entry 의 `keyEpoch` 가 `_chainKeyEpochs` 안에 존재 +
   직전 entry 의 `keyEpoch` 보다 큼.
3. 모든 entry 의 hash chain (`prev_hash → hash`) 가 self-consistent.

전제: bundle 이 변조되지 않았다는 외부 채널 검증 (예: bundle 파일의 sha256 을
release page · email · 별도 secure channel 로 받음).

### 절차 B — epoch 별 public key 를 사전 등록

엔터프라이즈 환경에서 외부 감사인 또는 SOC2 감사 절차가 더 엄격한 key custody 요구:

1. rosshield 운영자가 rotation 발생 시 매번 새 epoch 의 `publicKeyHex` 를 외부 secure
   channel (예: company key escrow · Hardware Security Module · 감사 organization 의
   key store) 로 broadcast.
2. 감사인이 자신의 신뢰 store 에서 `publicKeyHex` 를 조회 → bundle 안 `_chainKeyEpochs`
   해당 row 의 값과 byte-equal 비교.
3. equal 이면 절차 A 의 fg-verify run 결과 신뢰. 불일치하면 즉시 FAIL.

본 절차는 fg-verify 본체 외부에서 수행됩니다 — 감사 organization 의 정책에 따름.

### 절차 C — fg-verify integration 향후 확장 (v0.11+ 후보)

`--trusted-keys <dir>` 옵션 — `epoch_N_pub.hex` 파일에서 epoch 별 public key 를 로드해
bundle 안 `_chainKeyEpochs` 와 byte-equal 비교를 fg-verify 자체가 강제. 현 round 미구현
(R30-4 일관 — 단순 binary 우선).

## rotation entry transition 검증

v2 bundle 의 `audit.chain.key_rotated` entry 는 rotation event 의 audit trail 자체입니다.
fg-verify 는 다음을 검증합니다:

- entry 의 `keyEpoch` 가 `_chainKeyEpochs` 에 존재.
- 직전 entry 의 `keyEpoch` 가 `_chainKeyEpochs` 에 존재.
- `entry.keyEpoch > prev_entry.keyEpoch` (단조 증가).

위반 시 `epochTransition` step 이 FAIL — 운영자 또는 감사인이 chain 변조 또는 epoch
재사용을 즉시 식별합니다.

## 일반 FAIL 패턴

| 단계 | 원인 | 진단 |
|---|---|---|
| `signature` | `_signature` 또는 `_publicKey` 변조; signing key 가 `_chainKeyEpochs` 에 부재 (v2) | bundle 송신 채널 신뢰 확인. 별 채널로 sha256 비교. |
| `chain` | entry 의 `payloadDigest` 또는 `hash` 변조 | DB 손상·의도적 변조 의심. 직전 backup·archive 비교. |
| `epochTransition` | rotation entry 의 `keyEpoch` 가 chainKeyEpochs 에 부재 | rosshield-server 가 v2 bundle 생성 시 `_chainKeyEpochs` 누락 — server 버그. |
| `digestRecompute` | entry stream 손상 (gzip 부분 손상 등) | bundle 재다운로드. |

## v0.9.0 customer 호환성

기존 v0.9.0 bundle 에 변경 0:

- `_bundleVersion` 부재 → fg-verify 가 자동으로 v1 mode 진입.
- 단일 `_publicKey` 로 검증 — 기존과 byte-identical 결과.
- `_chainKeyEpochs` 부재 → fg-verify 가 epoch=1 default 로 처리.

업그레이드 절차: customer 는 fg-verify binary 만 v0.10.0+ 로 교체 → 기존 v1 bundle ·
신규 v2 bundle 양쪽 모두 변경 없이 검증.

## 보안 고려사항

- bundle 자체는 **signed**. 즉, bundle 안 `_chainKeyEpochs` 는 signing key 가 보증.
  단, **bundle 변조 + signing key 도 같이 침해** 시나리오는 bundle level 검증으로는
  방어 불가 — 절차 B (epoch 별 public key 사전 등록) 또는 multi-sig (별 epic) 필요.
- v2 bundle 의 `_chainKeyEpochs` 안 `revokedAt` 은 informational. fg-verify 는 revoked
  epoch 의 signing 도 검증 PASS — revoked 라도 bundle 생성 시점에는 유효했을 수 있음.
- 외부 secure channel 로 bundle 의 sha256 · `_keyId` · epoch 마지막 row 의 createdAt 을
  cross-verify 하는 것이 권장.

## 참조

- `cmd/rosshield-audit-verify/` — 본 CLI 소스.
- `internal/domain/audit/export.go` — v1·v2 wire 정의.
- `internal/domain/audit/key_epoch.go` — ChainKeyEpoch 도메인 + Repository.
- `docs/design/notes/audit-chain-rotation-automation-design.md` §6.5·§8.2 — design.
- `internal/platform/storage/postgres/migrations/0037_audit_chain_keys.up.sql` — 마이그레이션.
