# E34 TPM 키 봉인 + Secure Boot 설계 — Keystore 추상화 + swtpm 검증

> **상태**: Design draft (Phase 5, R41-1·R41-2·R41-3 결정 대기)
> **작성일**: 2026-05-11
> **범위**: 어플라이언스(Ubuntu Core 22 + TPM 2.0)에서 KEK·signing key를 디스크 평문으로 두지 않는다. TPM 2.0의 PCR 봉인으로 부팅 무결성을 강제하고, soft signer가 다루는 ed25519 private key를 TPM이 sealing object로 보관한다. CI는 swtpm 시뮬레이터로 검증 (R40-2 결정 — 2026-05-11).
> **참조**: `phase5-backlog.md` §E34, `13-patent-strategy.md` §13.3 D-3 robot identity binding, `internal/platform/signer/` (현재 soft 구현), `notes/e25-ha-design.md` (design doc 패턴).
> **비목표**: TPM EK 기반 robot identity binding(D-3, 별 epic E32). HSM(PKCS#11) 어댑터. Windows TPM 직접 검증(개발 환경은 Linux/swtpm). full disk encryption(LUKS+TPM, OS 영역).
> **코드 변경**: 0건. 본 문서는 docs only — 실제 구현은 R41-1·R41-2·R41-3 결정 후 별도 PR.

---

## 1. 목적·배경

### 왜 TPM 봉인이 필요한가

현재 `internal/platform/signer/soft/`는 ed25519 private key를 디스크 평문 파일(`0600`)로 저장합니다. 데스크톱·소규모 온프렘에서는 충분하지만, 어플라이언스 deployment에서는 다음 위협이 현실화됩니다:

- **디스크 도난** — 어플라이언스(NUC/OptiPlex급 mini-PC)는 물리 노출 환경에 놓이는 경우가 많습니다. SATA·NVMe를 빼서 다른 머신에 마운트하면 keys 디렉토리의 ed25519 private key가 그대로 추출됩니다. 그 키로 audit chain checkpoint·report PDF를 위조하면 외부 검증자가 진위 판별 불가.
- **부트 체인 변조** — 부트로더·커널·initramfs를 갈아끼우면 키 파일에 접근 가능한 user-space 코드가 임의로 실행됩니다. 정상 운영 중인 머신에서도 BIOS·Secure Boot가 비활성이면 자유롭게 부팅 매체를 바꿀 수 있습니다.
- **컴퓨팅 환경 침해** — root 권한 탈취 또는 supply-chain 침투로 일시적으로 user-space 접근권을 얻은 공격자가 키 파일을 외부로 유출할 수 있습니다.

이 세 위협 모델은 모두 **"키가 디스크에 평문으로 존재한다"**는 단일 가정에서 출발합니다.

### TPM 2.0 PCR 봉인의 핵심 아이디어

TPM 2.0 칩은 다음 두 기능을 제공합니다:

1. **PCR (Platform Configuration Registers)** — BIOS·OPROM·부트로더·커널 등 부팅 단계마다 측정값(SHA-256 해시)을 누적 extend. PCR 값은 부팅 체인이 1바이트라도 다르면 달라집니다.
2. **Sealing** — 임의 데이터를 PCR 값들의 정책(policy)에 묶어 TPM 안에 보관. 봉인 시점의 PCR 값과 unseal 시점의 PCR 값이 일치해야만 데이터 반환.

두 기능을 결합하면 **"정상 부팅 체인을 거친 직후의 ed25519 key는 unseal 가능, 변조된 부팅 체인에서는 절대 unseal 불가"**가 됩니다. 디스크를 통째로 빼서 다른 머신에 꽂아도, sealed object 자체는 TPM 칩 안에 묶여 있어 의미가 없습니다.

Phase 5 E34는 이 메커니즘을 rosshield-server에 결선하는 작업입니다. R40-2(2026-05-11) 결정대로 CI 검증은 swtpm 시뮬레이터로 진행하고, 실 TPM 검증은 E36 레퍼런스 HW 단계에서 수행합니다.

---

## 2. 두 모델 비교 (Signer 레벨 vs Keystore 레벨)

E34 결선 위치를 두 갈래로 나눌 수 있습니다.

| 모델 | 설명 | 장점 | 단점 |
|---|---|---|---|
| **A. TPM Signer 어댑터** | `Signer` 인터페이스 구현체를 TPM 직접 호출로 대체. `Sign()`이 TPM2_Sign 명령어로 ed25519 서명. private key는 평생 TPM 칩 밖으로 나오지 않음 | 최강 보안 — 메모리 덤프로도 키 추출 불가. 키 사용 감사 추적 가능(TPM 자체 audit log) | TPM 2.0 ed25519 지원 칩이 필요. 일부 구형 dTPM(2.0 초창기 펌웨어)은 RSA·ECC P-256만 지원. 서명 latency가 200~500ms 수준(TPM은 일반적으로 느림) — audit chain INSERT 빈도가 낮으면 무관하지만 PDF 보고서 대량 생성 시 병목 |
| **B. TPM Keystore + soft Signer** | TPM은 ed25519 raw key를 PCR-sealed blob으로 보관. 부팅 시 unseal하여 메모리에 로드 → 기존 soft signer가 in-memory로 사용 | 호환 칩 모두 OK (sealing은 TPM 2.0 spec 필수). 서명 latency는 soft와 동일(수 µs). 코드 변경 최소 — Signer는 그대로, Keystore만 추가 | private key가 일시적으로 user-space 메모리에 존재 — 메모리 덤프(LiME·avml) 또는 process injection 공격 시 노출 위험 |

### 권고: B (TPM Keystore + soft Signer)

Phase 5 첫 어플라이언스 PoC는 **호환성 우선**입니다. NUC/OptiPlex 2종에서 무조건 동작해야 하는 E36 Exit 조건과, 1.5주 추정에 들어가야 하는 코드 변경량을 고려하면 B가 합리적입니다.

A는 D5 enterprise build tag 안에 `signer/tpm/` 옵션으로 후속 epic에서 추가합니다(예: E40 또는 별 enterprise feature). 1순위 결합 청구항(D8-2)의 "키 추출 공격 시 서명 자동 무효화" 보강에 A-2 청구항(§13.4)이 매핑되며, 이쪽이 enterprise 차별화에도 부합합니다.

본 문서 §3 이하는 모델 B 전제로 진행합니다.

---

## 3. Go TPM 라이브러리 비교

Pure Go(CGO=0) 유지가 필수 제약(현재 `make ci` 기준)입니다. 세 후보 모두 cgo-free.

| 라이브러리 | 메인테이너 | API 수준 | 마지막 release | 평가 |
|---|---|---|---|---|
| `github.com/google/go-tpm` | Google | low-level — TPM 2.0 raw command(TPM2_StartAuthSession 등) 1:1 매핑 | 활발 | 유연하지만 sealing 한 번에도 50+ 줄 boilerplate. seal/unseal·PCR 정책 빌더를 직접 작성해야 함 |
| `github.com/google/go-tpm-tools` | Google | high-level — `client.Seal()`·`client.Unseal()`·attestation 헬퍼 제공 | 활발 (Confidential VM 등에서 적극 사용) | seal/unseal·attest를 한 함수 호출로 처리. 내부적으로 `go-tpm`을 사용. **추천** |
| `github.com/canonical/go-tpm2` | Canonical | medium-level — Object 단위 추상(SealedObject 타입 등) | 활발 (snap·Ubuntu Core 자체에서 사용) | snap·Ubuntu Core와 친화. snapd가 이 라이브러리로 FDE를 구현. 어플라이언스 OS와 동일 라이브러리를 쓰면 디버깅·이슈 추적 유리 |

### 권고: google/go-tpm-tools

이유:

1. **boilerplate 최소** — `client.NewKey(rwc, tpm2.HandleEndorsement, ...)` + `key.Seal(secret, sealOpts)` 두 줄로 끝. 1.5주 추정에 부합.
2. **공식 deprecation 위험 낮음** — Google이 GCP Confidential VM·Shielded VM에서 직접 사용 중. 메인테이너 자원 안정적.
3. **swtpm 호환** — 내부 구현이 `go-tpm` raw transport이므로 `/dev/tpm0`·swtpm Unix socket 모두 동일 코드 경로.

대안 평가:
- **canonical/go-tpm2** — snapd와 같은 라이브러리를 쓰는 친화성은 매력적. 단, API가 더 verbose(Object 직접 핸들링)이고 high-level seal/unseal 헬퍼가 약함. snap 환경에서 FDE 키와 충돌 가능성을 추후 점검할 가치는 있음 — 본 epic 비목표로 두되 Phase 5 carryover 후보로 메모.
- **google/go-tpm low-level** — 봉인 정책을 매우 세밀하게 제어할 수 있지만, 우리는 표준 PCR sealing(§5)만 쓰므로 과도한 유연성. 디버깅 시에만 임시로 사용.

---

## 4. Keystore 추상화 설계

### 4.1 패키지 구조

신규 패키지 `internal/platform/keystore/`를 추가하고, 현재 `signer/soft/LoadOrCreatePrivateKey`가 직접 수행하는 디스크 I/O를 추상화합니다.

```
internal/platform/keystore/
├─ keystore.go         # KeyStore interface
├─ file/
│   ├─ file.go         # 현재 동작과 동등 (디스크 평문 ed25519)
│   └─ file_test.go
└─ tpm/
    ├─ tpm.go          # go-tpm-tools 기반 PCR-sealed
    ├─ pcr_policy.go   # PCR set·정책 빌더
    ├─ tpm_test.go             # 단위 테스트 (mock transport)
    └─ tpm_integration_test.go # //go:build tpm_integration
```

### 4.2 인터페이스

```go
// 의사 코드 (실 구현은 R41-1·R41-2 결정 후)
package keystore

type KeyStore interface {
    // LoadOrCreatePrivateKey는 handle에 해당하는 ed25519 private key를 반환합니다.
    // 키가 없으면 새로 생성하여 영속 저장 후 반환.
    // file 구현은 path = handle, tpm 구현은 NV index 또는 sealed object 파일 경로.
    LoadOrCreatePrivateKey(handle string) (ed25519.PrivateKey, error)

    // Close는 keystore가 보유한 자원(TPM 세션, file handle 등)을 해제합니다.
    Close() error
}
```

### 4.3 file 구현 (backward compatible)

```go
type fileKeyStore struct {
    rootDir string  // bootstrap이 결정 (기본 ~/.rosshield/keys)
}

func (f *fileKeyStore) LoadOrCreatePrivateKey(handle string) (ed25519.PrivateKey, error) {
    // 현재 signer/soft.LoadOrCreatePrivateKey와 동등 동작 — handle을 파일명으로 매핑
    return softLoadOrCreatePrivateKey(filepath.Join(f.rootDir, handle))
}

func (f *fileKeyStore) Close() error { return nil }
```

기존 `signer/soft.LoadOrCreatePrivateKey`는 내부적으로 `fileKeyStore.LoadOrCreatePrivateKey`로 위임하거나, 그대로 두고 bootstrap이 keystore.New(...)를 거쳐 soft signer로 wrap합니다. 후자가 변경 폭 작음 — `signer/soft` 파일은 손대지 않음.

### 4.4 tpm 구현

```go
type tpmKeyStore struct {
    rwc          io.ReadWriteCloser  // /dev/tpm0 또는 swtpm Unix socket
    pcrSelection []int               // 보통 [0, 2, 4, 7] — §5
    parentHandle tpmutil.Handle      // EK(Endorsement Key) 또는 SRK(Storage Root Key)
    sealedDir    string              // sealed object 영속 경로 (TPM NV 또는 일반 파일)
}

func (t *tpmKeyStore) LoadOrCreatePrivateKey(handle string) (ed25519.PrivateKey, error) {
    sealedPath := filepath.Join(t.sealedDir, handle+".sealed")
    if _, err := os.Stat(sealedPath); err == nil {
        return t.unsealKey(sealedPath)  // PCR 검증 후 raw key 반환
    }
    // 신규 — ed25519 키 생성 → seal → 디스크에 sealed blob 저장
    _, priv, _ := ed25519.GenerateKey(rand.Reader)
    if err := t.sealKey(sealedPath, priv); err != nil {
        return nil, err
    }
    return priv, nil
}
```

sealed blob은 디스크에 둬도 안전합니다(다른 TPM에서 unseal 불가, 같은 TPM이라도 PCR 변조 시 unseal 불가).

### 4.5 bootstrap 결선

```
--keystore=file (기본)  → fileKeyStore
--keystore=tpm          → tpmKeyStore — TPM 미장착 시 부팅 실패 (조용한 fallback 금지, §11)
```

bootstrap이 KeyStore에서 `ed25519.PrivateKey`를 받아 `signer/soft.NewFromPrivateKey(priv)`로 wrap (현재 `wrapPrivateKey`를 export). signer 인터페이스 자체는 변경 없음 — audit chain·report PDF·pack manifest 등 기존 호출처 영향 0.

---

## 5. PCR 정책

### 5.1 권장 PCR set

Ubuntu Core 22의 측정 부팅(measured boot) 기준으로 다음 PCR을 sealing 정책에 포함합니다:

| PCR | 측정 대상 | 변경 빈도 | 포함 권장 |
|---|---|---|---|
| **0** | UEFI firmware (BIOS) | 펌웨어 업데이트 시 | ✅ |
| 1 | UEFI configuration · boot order | UEFI 설정 변경 시 | ❌ (사용자가 BIOS 진입만 해도 변경 가능) |
| **2** | OPROM (옵션 ROM, GPU·NIC 등) | HW 변경 시 | ✅ |
| 3 | OPROM configuration | 빈번 변경 | ❌ |
| **4** | MBR/EFI (부트 매체 첫 코드) | 부트로더 업데이트 시 | ✅ |
| 5 | GPT partition table | 파티션 변경 시 | ❌ (디스크 확장 등으로 변경) |
| 6 | resume from S4 등 platform-specific | 잡음 많음 | ❌ |
| **7** | Secure Boot policy + 사용한 키 | Secure Boot 정책 변경 시 | ✅ |
| 8~9 | OS loader (grub 등) | OS 업데이트 시 | ❌ (옵션 — §5.3 strict mode) |
| **11** | snap base | snap update 시 | △ (옵션 — §5.3 strict mode) |
| **12** | snap kernel | kernel snap update 시 | △ (옵션 — §5.3 strict mode) |
| 14 | shim (mokutil) | MOK 변경 시 | ❌ |

### 5.2 기본 정책: `[0, 2, 4, 7]`

- 0: BIOS 자체가 바뀌면 unseal 불가 — 펌웨어 attack 차단
- 2: GPU·NIC 펌웨어 변조 차단
- 4: 부트로더(grub·shim 첫 stage) 변조 차단
- 7: Secure Boot 비활성화·키 변경 시 unseal 불가 — 부트 체인 신뢰 근간

이 4개만으로 §1의 세 위협(디스크 도난·부트 체인 변조·user-space 침해 후 디스크 추출)을 모두 차단합니다. 일상 운영 중(rosshield 자체 업데이트·sysctl 변경 등)에는 PCR이 그대로이므로 unseal 정상.

### 5.3 strict mode: `[0, 2, 4, 7, 11, 12]`

`--keystore-pcr-strict` 플래그로 활성. snap base·kernel까지 묶음. 장점은 OS 깊이의 변조까지 차단, 단점은 snap kernel update가 발생하면 PCR 11·12가 바뀌어 unseal 실패 → 운영자가 수동 reseal 또는 자동 reseal hook 필요.

**자동 reseal hook** — snap configure hook에서 새 PCR 측정값으로 다시 seal. 단, 새 kernel이 정상이라는 가정이 필요(공격자가 만든 악성 kernel snap이라면 reseal도 함께 변조됨). 따라서 strict mode는 enterprise customer가 수동 검증을 동반할 때만 권고.

기본은 **non-strict (`[0, 2, 4, 7]`)**, strict는 옵션.

### 5.4 봉인 메타데이터

sealed blob 옆에 정책 스냅샷을 함께 저장합니다:

```jsonc
// {handle}.sealed.meta.json
{
  "pcrSelection": [0, 2, 4, 7],
  "pcrSnapshot": {
    "0": "a3f1c9...",
    "2": "00000...",  // OPROM 없으면 0
    "4": "1b2e7a...",
    "7": "8c4d3f..."
  },
  "sealedAt": "2026-05-15T10:00:00Z",
  "tpmFwVersion": "STM_3.71",
  "rosshield-server-version": "0.5.0"
}
```

unseal 실패 시 운영자가 메타를 보고 "어느 PCR이 어떻게 바뀌었는지" 진단 가능. 메타 자체는 비밀이 아님 (PCR 값은 ROOT of trust의 측정 결과일 뿐 키 추출에 사용 불가).

---

## 6. TDD 태스크

| ID | 테스트 | 인프라 | 대상 |
|---|---|---|---|
| **E34.T1** | `TestTpmKeySealRoundTrip` | swtpm 시뮬레이터 (testcontainers 또는 docker exec) | `tpmKeyStore.LoadOrCreatePrivateKey` — seal → 재시작 시뮬레이션 → unseal → 동일 keyID |
| **E34.T2** | `TestTpmUnsealFailsWhenPcrChanged` | swtpm | seal 후 swtpm CLI로 PCR_Extend 호출하여 PCR 변조 → unseal 시 정책 mismatch 에러 |
| **E34.T3** | `TestKeystoreTpmRefusesIfNoTpmDevice` | 단위 테스트 (mock transport) | `--keystore=tpm` + `/dev/tpm0` 부재 → bootstrap이 명시적 에러로 종료 (조용한 fallback 금지) |
| **E34.T4** | `TestKeystoreFileBackwardCompatible` | 단위 테스트 | `fileKeyStore`가 기존 `signer/soft.LoadOrCreatePrivateKey`와 동일 포맷·동일 권한(0600)·동일 keyID 산출 — 기존 사용자의 keys 파일 그대로 로드 가능 |
| **E34.T5** | `TestKeystoreTpmRoundTripWithGoTpmTools` | swtpm | go-tpm-tools `client.Seal/Unseal` API 직접 호출 — 라이브러리 자체 동작 확인 (라이브러리 업데이트 시 회귀 감지) |

### 테스트 인프라 상세

- **swtpm 기동** — `swtpm socket --tpm2 --tpmstate dir=$TMPDIR/swtpm --ctrl type=unixio,path=$TMPDIR/swtpm.ctrl --server type=unixio,path=$TMPDIR/swtpm.srv &`. Go 테스트는 `TPM_DEVICE` env로 socket 경로 받음.
- **PCR 변조** — `tpm2-tools`의 `tpm2_pcrextend 0:sha256=<random>`. swtpm은 진짜 칩처럼 PCR_Extend를 받아 누적.
- **build tag** — `//go:build tpm_integration` — 일반 `go test ./...`에서는 제외 (Windows 개발 환경 보호).
- **Windows 환경 대응** — 메인 개발기는 Windows. swtpm은 Linux 전용. T1·T2·T5는 GitHub Actions Linux runner에서만 실행. T3·T4는 Windows에서도 실행 가능 (mock transport 또는 file-only).

### 의존 그래프

T4 → T3 → T1 → T2 → T5 (대략 순서). T4는 가장 먼저 (기존 동작 보전 확인), T5는 가장 마지막 (라이브러리 회귀 가드).

---

## 7. CI 검증

`.github/workflows/ci.yml`에 신규 job `tpm-integration` 추가. 기존 job들과 병렬, 실패해도 main job은 영향 없음(옵션):

```yaml
tpm-integration:
  runs-on: ubuntu-22.04
  steps:
    - uses: actions/checkout@v4
    - name: Install swtpm
      run: sudo apt-get update && sudo apt-get install -y swtpm swtpm-tools tpm2-tools
    - name: Setup Go
      uses: actions/setup-go@v5
      with: { go-version: '1.23' }
    - name: Start swtpm
      run: |
        mkdir -p /tmp/swtpm
        swtpm socket --tpm2 --tpmstate dir=/tmp/swtpm \
          --ctrl type=unixio,path=/tmp/swtpm.ctrl \
          --server type=unixio,path=/tmp/swtpm.srv \
          --flags startup-clear --daemon
    - name: Run TPM integration tests
      env:
        TPM_DEVICE: /tmp/swtpm.srv
      run: go test -tags=tpm_integration -count=1 ./internal/platform/keystore/tpm/...
```

### 점진적 도입

1. **Stage 1** — job 추가 후 `continue-on-error: true`로 마킹. 결과만 수집, blocking 안 함.
2. **Stage 2** — T1·T2·T5가 안정화되면 blocking 전환.
3. **E36 단계** — 실 TPM 2.0 칩(NUC + OptiPlex)에서 self-hosted runner 또는 수동 검증 (CI 자동화 X).

### swtpm vs 실 TPM 차이

swtpm은 software emulation이라 실제 칩과 다음 차이가 있습니다:
- 펌웨어 quirk 없음 (Infineon·STMicro·Nuvoton 칩별 미묘한 동작 차이 재현 불가)
- attestation 인증서가 swtpm 자체 서명 (vendor CA 체인 검증 불가)
- 성능이 호스트 CPU 속도에 비례 (실 칩은 100~500ms로 일정)

따라서 swtpm은 **로직 회귀 방지** 목적이고, 실 칩 호환성은 E36 레퍼런스 HW 단계에서 검증합니다(phase5-backlog.md §리스크 표 참조).

---

## 8. Secure Boot enrollment

TPM 봉인이 의미를 가지려면 부트 체인 자체가 변조되지 않았다는 보장이 필요합니다. 그것이 Secure Boot의 역할입니다.

### 8.1 Ubuntu Core 22 Secure Boot 기본 설정

Ubuntu Core 22는 Canonical이 서명한 shim → grub → kernel snap 체인을 사용하며, 다음 키가 UEFI에 enroll되어 있습니다:

- **Microsoft UEFI CA** — shim 서명 (Microsoft가 Linux distro shim에 cross-sign)
- **Canonical Master Signing Key** — Canonical이 서명한 grub·kernel 검증

기본 enrollment에서 PCR 7에는 "Microsoft + Canonical 키로 검증된 정상 부트" 사실이 측정됩니다. rosshield는 이 PCR 7 값에 sealing 정책을 묶음으로써, **누군가 Secure Boot를 비활성화하거나 다른 OS를 부팅하면 PCR 7이 바뀌어 unseal 실패** → audit chain·report 서명 불가능.

### 8.2 Phase 5 1차 범위

**Microsoft + Canonical 기본 키만 사용**. 별도의 rosshield 사용자 키 등록은 **옵션**(enterprise customer가 strict deployment를 원할 때).

이유:
- 사용자 키를 등록하려면 BIOS 진입 후 mokutil로 PK·KEK·db 갱신 필요 — 운영자 부담 큼
- Canonical이 이미 검증된 부트 체인을 제공하므로 1차 범위에서 추가 키 불필요
- 사용자 키 추가는 §5.3 strict mode와 함께 enterprise 옵션으로 후속 가능

### 8.3 enrollment 절차 (운영자 1회 실행)

E34 시점에 추가될 운영자 가이드 (`docs/appliance/secure-boot-setup.md`, E34 stage 4에서 작성):

```bash
# 1. UEFI Secure Boot 활성화 확인
sudo mokutil --sb-state
# 출력: SecureBoot enabled

# 2. PCR 7 측정 확인
sudo tpm2_pcrread sha256:7

# 3. 측정 값을 sealing 정책에 사용 — rosshield-server가 자동
sudo systemctl restart rosshield-server
sudo journalctl -u rosshield-server | grep "tpm: sealed key"
# 출력 예: tpm: sealed key handle=audit-chain pcr=[0,2,4,7]

# 4. 검증 — 재부팅 후 unseal 성공 확인
sudo reboot
# 부팅 후
sudo systemctl status rosshield-server
# active (running) — unseal 성공
```

### 8.4 사용자 키 등록 (옵션, enterprise)

Phase 5 1차 비목표지만 후속 참조용:

```bash
# rosshield 자체 서명 키 생성 (build host에서 1회)
openssl req -new -x509 -newkey rsa:2048 -keyout rosshield-PK.key \
  -out rosshield-PK.crt -days 3650 -nodes -subj "/CN=rosshield PK"

# 어플라이언스에서 mokutil로 등록
sudo mokutil --import rosshield-PK.crt
sudo reboot  # MOK Manager 화면에서 enrollment 완료
```

이 경우 PCR 7에 추가 키 측정값이 들어가므로 sealing 정책도 다시 잡아야 합니다 — strict deployment 가이드에서 다룸.

---

## 9. Stage 분해

총 **약 6.5일** (1인 작업 기준, Phase 5 backlog의 1.5주 추정에 부합):

| Stage | 작업 | 산출 | 추정 |
|---|---|---|---|
| **Stage 1** | design doc(본 문서) + keystore interface scaffolding + file 구현 + T4 단위 테스트 | `internal/platform/keystore/keystore.go`·`file/file.go`·`file/file_test.go`·본 문서 | **1일** |
| **Stage 2** | TPM 어댑터 본체 — go-tpm-tools seal/unseal + PCR 정책 빌더 + T1·T2·T5 integration 테스트 | `tpm/tpm.go`·`tpm/pcr_policy.go`·`tpm/tpm_integration_test.go` | **3일** |
| **Stage 3** | bootstrap 결선 — `--keystore` flag + TPM 미장착 거부 + T3 단위 테스트 | `cmd/rosshield-server/bootstrap.go` 갱신, 설정 docs 1 페이지 | **0.5일** |
| **Stage 4** | swtpm CI integration job + Secure Boot enrollment 가이드 + 운영자 docs | `.github/workflows/ci.yml` 갱신, `docs/appliance/secure-boot-setup.md` 신규 | **2일** |
| **Stage 5** | E36 레퍼런스 HW에서 실 TPM 2.0 검증 + 결과 표 작성 | E36 epic으로 위임 (`docs/appliance/reference-hardware.md`에 TPM 섹션) | (E36 일부) |

### 병렬 작업 가능성

- Stage 1 + Stage 4 docs 일부를 병렬로 진행 가능 (다른 사람이 Secure Boot 가이드 초안 작성)
- Stage 2의 T1·T2·T5는 의존이 있어 순차

memory `feedback_parallel_agents.md`에 따라 Stage 4의 docs 작성은 별 agent에게 분담 권고.

---

## 10. 결정 요청 항목 → ✅ 모두 결정 완료 (2026-05-11)

사용자가 본 design doc 권고 3종 모두 채택. Stage 2 (실 PCR seal/unseal 본체)
sub-agent worktree dispatch 시작.

### R41-1: KeyStore 모델 — ✅ B (TPM Keystore + soft Signer)

- ~~A) TPM Signer 어댑터~~ — D5 enterprise build tag로 후속 epic 분리 (R41-1.후속 후보)
- **B) TPM Keystore + soft Signer** ✅ **결정** (2026-05-11) — 호환성·코드 변경 최소
- ~~C) 두 모드 모두 지원~~ — Phase 5 1차 over-engineering, 2 customer 이상 명시 요청 시 추가

근거: 모든 TPM 2.0 칩 호환 + 성능 OK + Stage 1 keystore 추상에 자연스럽게 fit.

### R41-2: Go TPM 라이브러리 — ✅ google/go-tpm-tools

- **A) google/go-tpm-tools** ✅ **결정** (2026-05-11) — high-level seal/unseal 헬퍼
- ~~B) canonical/go-tpm2~~ — snap FDE 키와 충돌 가능성 점검 필요 (Phase 5 carryover 후보)
- ~~C) google/go-tpm low-level~~ — boilerplate 폭증, 1.5주 추정 위험

근거: GCP Confidential VM 메인 사용처, Pure Go (CGO=0 유지), boilerplate 최소.

### R41-3: 기본 PCR set — ✅ [0, 2, 4, 7]

- **A) `[0, 2, 4, 7]`** ✅ **결정** (2026-05-11) — BIOS·OPROM·EFI·Secure Boot policy
- ~~B) `[0, 2, 4, 7, 11, 12]` (strict)~~ — enterprise strict 옵션으로 후속 분리 (snap update reseal hook 필요)
- ~~C) custom~~ — 미래 옵션 (운영자 사고 위험으로 Phase 5 1차 비활성)

근거: 일상 OS·snap 업데이트 무관 + §1 세 위협(디스크 도난·부트 변조·환경 침해) 모두 차단.

---

## 11. 참조

- `docs/design/phase5-backlog.md` §E34 정의, §리스크 표 (TPM 시뮬레이터 vs 실 TPM)
- `docs/design/13-patent-strategy.md` §13.3 D-3 robot identity binding (TPM EK 활용 — 별 epic E32 영역, 본 epic은 sealing만)
- `docs/design/notes/e25-ha-design.md` design doc 패턴
- `internal/platform/signer/signer.go` — 변경 없음. KeyStore가 `ed25519.PrivateKey` 반환 → `signer/soft.NewFromPrivateKey`로 wrap
- `internal/platform/signer/soft/signer.go` — `wrapPrivateKey`를 export하거나, `LoadOrCreatePrivateKey` 호출처를 keystore 경유로 교체
- TPM 2.0 Library Specification, Part 1 (Architecture) §11 — Sealing
- Ubuntu Core 22 Measured Boot — Canonical 공식 docs
- `swtpm` — IBM 시작, 현재 stefanberger/swtpm GitHub 메인 (Ubuntu apt 패키지)
- `github.com/google/go-tpm-tools` — `client.SealOpts`·`client.Unseal` API 문서
- 참조 문헌: "A Practical Guide to TPM 2.0" (Will Arthur, David Challener) — sealing chapter
