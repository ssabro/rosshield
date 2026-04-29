# E8 PDF 리포트 서명·검증 + Audit Chain Anchor 통합 — 리서치 노트

> 작성일: 2026-04-29 · Phase 1 · Epic E8 · 스택 결정 사전 조사
> 범위: 단일 PDF 리포트의 서명 형식 결정 + audit 체인 anchor 매립 패턴 + `rosshield report verify` CLI 검증 알고리즘
> 산출 형식: 권장 R10-X(서명 형식)·R11-X(anchor) 식별자 + 검증 의사코드
> 외부 의존: **0** (pure Go stdlib + crypto/ed25519, 이미 `internal/platform/signer/soft` 보유)

---

## 0. TL;DR

- 권장 서명 형식: **R10-T = 옵션 D (PDF EOF 뒤 trailing block, JSON+sig)** + 보조로 옵션 C (`.sig` detached 파일도 같이 출력).
- Audit chain anchor: **R11-A = signed payload 안에 `audit_anchor` JSON 객체로 매립** (`tenant_id`, `seq`, `head_hash`, `head_signed_at`, `signer_key_id`).
- 검증 CLI: `rosshield report verify <pdf> [--audit-export <path>]` — 5단계 (서명 추출 → 본문 hash → Ed25519.Verify → anchor 추출 → optional cross-check).
- 결정성: **mandatory** — `/CreationDate`, `/ID`, `/Producer`는 입력 기반(테넌트·세션·report 메타에서 파생)으로 강제 고정. 라이브러리가 random ID/Now()를 쓰면 후처리로 덮어쓰기.
- 테스트: 6개 변조 케이스(페이지 추가/삭제/텍스트 수정/메타 변조/멀티 서명/키 회전 후 검증) + 1개 결정성 회귀.

---

## 1. 서명 형식 비교

### 1.1 4가지 옵션 평가

| 옵션 | 형식 | 외부 검증 가능 | 구현 복잡도 | 단일 파일 | 생태계 호환 | self-signed Ed25519 | 결정성 위험 | 평가 |
|---|---|---|---|---|---|---|---|---|
| **A. PAdES** (ISO 32000-2 + ETSI EN 319 142) | PDF `/Sig` Dict + `/ByteRange` + PKCS#7/CAdES container | Adobe Reader, EU eIDAS 도구 | **높음** (PKCS#7 ASN.1, byte range 계산, CMS SignedData 빌더) | ✅ | 표준 (Adobe·정부·금융) | ❌ X.509 chain 필수, raw Ed25519는 RFC 8419로 가능하나 Adobe Reader 미지원 (2026 시점) | 중 (signing time, Adobe Reader render에 따라 byte 변동) | **부적합** — 개발 비용 ≥ 4주, ROI 음수 |
| **B. PDF Info Dict 사용자 키** | trailer `/Info` dict에 `/X-Signature-KeyId (...)` `/X-Signature-Base64 (...)` | 텍스트 grep 가능 (하지만 PDF reader에는 안 보임) | **중** (PDF writer가 trailer dict 갱신 지원해야 함) | ✅ | 비표준, 사용자 정의 키 | ✅ raw bytes OK | 중 (Info dict의 다른 필드도 결정적이어야) | **차순위** — 라이브러리 의존 큼, body hash 정의 모호 (sig 자신이 body 안에 있어 self-reference 회피 절차 필요) |
| **C. Detached `.sig` 파일** | `report.pdf` + `report.pdf.sig` (signify·minisign style) | ✅ 매우 쉬움 (별도 파일을 그냥 sha256+ed25519.Verify) | **낮음** (문자 그대로 sig·keyID 두 줄) | ❌ 두 파일 운반 | minisign·signify와 호환 가능 (포맷 합의 시) | ✅ 그 자체 | 낮음 (PDF 내용 무관) | **보조 권장** — 단순성·외부 검증 강력 |
| **D. PDF EOF 뒤 trailing block** | `%%EOF\n` + `\n%%FG-SIG\n<base64-JSON-with-sig>\n%%FG-SIG-END\n` | ✅ 텍스트 marker로 추출, PDF reader는 EOF에서 읽기 중지 → 무시 | **낮음** (PDF writer 끝낸 뒤 append만) | ✅ 단일 파일 | 비표준 (Cloudflare PDF append, GnuPG cleartext signature 영향) | ✅ raw bytes OK | **매우 낮음** (sig 추가 전 body는 PDF writer 산물 그대로) | **최선** — 단순 + 단일파일 + 외부 검증 가능 |

### 1.2 옵션 D의 근거 (왜 trailing block인가)

**PDF spec(ISO 32000-1 §7.5.5) 관점**: PDF reader는 파일 끝에서 역방향으로 `%%EOF` 마커를 찾고, 그 직전 `startxref` 오프셋으로 cross-reference table을 읽는다. **`%%EOF` 뒤의 모든 바이트는 read-time에 무시**된다. (Linearization 등 일부 reader 변종이 forward parse 모드를 쓸 수 있으나, 표준 reader는 trailing 데이터에 영향받지 않음 — Foxit·Acrobat·Chrome·Edge·SumatraPDF·pdf.js 검증됨.)

**구체적 wire format 제안**:

```
... PDF 본체 ...
%%EOF
\n
%%FG-SIG-V1\n
<base64-no-newline of signed_payload JSON>\n
%%FG-SIG-V1-END\n
```

여기서 `signed_payload`는:

```json
{
  "alg": "Ed25519",
  "key_id": "key_a3f1c9b27e840a2c",
  "body_sha256": "<hex of sha256 of bytes [0 .. start of trailing block])>",
  "signature_b64": "<base64 of ed25519.Sign(canonical_signing_input)>",
  "signed_at": "2026-04-29T12:34:56Z",
  "report": {
    "tenant_id": "tn_...",
    "session_id": "ss_...",
    "report_id": "rp_...",
    "schema_version": 1
  },
  "audit_anchor": { ...§3 참조... }
}
```

**서명 입력**(`canonical_signing_input`)은 `signature_b64`를 제외한 나머지 필드를 **canonical JSON**(키 정렬, 공백 없음, UTF-8)으로 직렬화한 바이트열.

> 구현 메모: `body_sha256`은 **trailing block 직전까지의 PDF 바이트** 해시. 서명 입력에 `body_sha256`이 들어가므로, PDF 본체 변조 시 `body_sha256` 재계산값과 불일치 → 서명 검증 실패. 즉 sig 자체가 PDF 본체에 종속되면서도 sig가 본체 안에 자기-참조로 들어가는 자기-순환을 회피한다.

### 1.3 옵션 D + C 보조 (권장 동시 출력)

`rosshield report build`는 기본으로 `report.pdf`(D 형식, 단일 파일) + `report.pdf.sig`(C 형식, detached) **둘 다 출력**.

- D 단독으로 충분하지만, C도 같이 두면 (a) PDF reader가 trailing 바이트를 잘라내는 미래 이슈 대비 (b) GPG·minisign 익숙한 감사인이 손쉽게 검증.
- 두 파일은 **같은 signing input**을 쓰므로 서명 1회로 두 파일 동시 생성. 비용 무시 가능.

---

## 2. 권장값 R10-T (단일 결정)

**R10-T**: 리포트 PDF는 표준 PDF 본체 + 마커 둘러싼 trailing JSON 블록(옵션 D)로 서명한다. 동시에 동일 서명을 담은 `report.pdf.sig` detached 파일을 부수 생성한다(옵션 C 보조).

**3줄 이유**:

1. **단일 바이너리 원칙(§01-7) + 외부 의존 0** — PAdES/CMS 의존을 도입하지 않고 stdlib `crypto/ed25519` + `encoding/base64` + `encoding/json`만으로 완결.
2. **결정성(§01-1)** — trailing block은 PDF writer 산물 뒤에 append만 하므로, body 결정성만 보장하면 sig도 결정적. PAdES의 `/ByteRange` self-reference 계산은 결정성·구현 모두 까다로움.
3. **검증 단순성** — 외부 감사인이 Python·shell 스크립트 30줄로 검증 가능 (marker 스캔 → JSON parse → body 해시 → ed25519 verify). C 보조 파일이 있으면 minisign-style 검증도 OK.

---

## 3. Audit Chain Anchor 통합 설계 (R11-A)

### 3.1 매립 위치 — 두 군데 (가시화 + 기계검증)

| 위치 | 형태 | 목적 |
|---|---|---|
| **PDF 마지막 페이지 footer** (가시화) | "Audit Chain Anchor: tn_..., seq=12345, head=a3f1c9b2... (시점 2026-04-29 12:34:56Z)" 한 줄 | 인쇄·스캔된 사본에서도 anchor 식별, 사람이 읽음 |
| **trailing block JSON `audit_anchor`** (기계검증) | 구조화 JSON | `rosshield report verify`가 cross-check 사용 |

두 군데가 **불일치하면 검증 실패**(R10-T body 변조로 간주). 가시화는 서명 입력의 일부이므로 문서 텍스트를 변경하면 hash 깨짐.

### 3.2 audit_anchor JSON 스키마

```json
{
  "tenant_id": "tn_acme",
  "seq": 12345,
  "head_hash": "sha256:a3f1c9b27e840a2c...",
  "head_signed_at": "2026-04-29T12:34:50Z",
  "signer_key_id": "key_b9d2...",
  "checkpoint": {
    "seq": 12300,
    "hash": "sha256:78ee...",
    "signed_at": "2026-04-29T12:00:00Z",
    "signature_b64": "<ed25519 sig of (hash||seq||tenant_id) by signer_key_id>"
  }
}
```

- `seq`/`head_hash`: 리포트 서명 시점에 audit chain HEAD 스냅샷.
- `checkpoint`: 가장 가까운 §10.5 checkpoint 서명 (없을 수도 있음; 옵셔널).
- `signer_key_id`: audit 체크포인트 서명에 쓰인 키 (리포트 서명 키와 다를 수 있음 — 분리된 키 운영 권장).
- 모든 hash는 `sha256:` 접두사로 알고리즘 고정.

### 3.3 cross-check 시나리오

검증자가 가진 자료 조합별 가능한 검증 강도:

| 검증자 보유 자료 | 가능 검증 |
|---|---|
| `report.pdf` 만 | (1) 서명 자체 유효성 (2) anchor가 박혀있다는 사실 + (3) anchor의 checkpoint sig가 유효 (공개키만 있으면) |
| 위 + audit `chain.csv` export | 추가로 (4) `chain.csv`에서 `seq=12345` row의 hash가 `head_hash`와 일치 |
| 위 + audit DB(라이브 시스템) | 추가로 (5) `seq` 이후 chain이 끊김 없이 이어졌는지 |

### 3.4 멀티 키 운영 분리 권장

- **report signer key**: 리포트 서명 전용 (회전 주기 90일 권장).
- **audit checkpoint signer key**: §10.5 체크포인트 전용 (회전 주기 365일, 더 엄격).
- 분리 이유: report key가 노출돼도 audit chain 무결성은 안 무너짐. Phase 1은 두 키 모두 `signer/soft`로 메모리/파일 보관, Phase 3 SKU에서 TPM 봉인.

---

## 4. 검증 알고리즘 의사코드 (`rosshield report verify`)

```
func ReportVerify(pdfPath string, opts VerifyOptions) (Result, error):
    // 1) 파일 읽기
    raw := os.ReadFile(pdfPath)

    // 2) trailing block 추출
    startMarker := []byte("\n%%FG-SIG-V1\n")
    endMarker   := []byte("\n%%FG-SIG-V1-END\n")
    sIdx := bytes.LastIndex(raw, startMarker)
    eIdx := bytes.LastIndex(raw, endMarker)
    if sIdx < 0 || eIdx < 0 || eIdx <= sIdx:
        return ErrNoSignature
    bodyBytes := raw[:sIdx]                    // PDF 본체 (trailing block 직전까지)
    blockB64  := raw[sIdx+len(startMarker) : eIdx]
    blockJSON := base64.Decode(blockB64)
    var sp SignedPayload; json.Unmarshal(blockJSON, &sp)

    // 3) PDF body hash 재계산
    bodyHash := sha256.Sum256(bodyBytes)
    if hex(bodyHash) != strip("sha256:", sp.BodySHA256):
        return ErrBodyHashMismatch          // 본체 변조 (페이지 추가/삭제/텍스트 수정)

    // 4) Ed25519 검증
    publicKey := opts.TrustStore.Lookup(sp.KeyID)  // 또는 --pubkey 플래그
    if publicKey == nil:
        return ErrUnknownKey
    signingInput := canonicalJSON(sp without "signature_b64")
    sig := base64.Decode(sp.SignatureB64)
    if !ed25519.Verify(publicKey, signingInput, sig):
        return ErrSignatureInvalid

    // 5) audit_anchor 추출 + 가시화 일치 확인
    anchor := sp.AuditAnchor
    visibleFooter := extractAnchorFooterText(bodyBytes)   // PDF text extraction
    if !anchorTextMatches(visibleFooter, anchor):
        return ErrAnchorVisibleMismatch     // 본체 가시화와 JSON 불일치

    // 6) (옵셔널) checkpoint 서명 자체 검증
    if anchor.Checkpoint != nil:
        cpInput := concat(anchor.Checkpoint.Hash, anchor.Checkpoint.Seq, anchor.TenantID)
        cpKey := opts.TrustStore.Lookup(anchor.SignerKeyID)
        if !ed25519.Verify(cpKey, cpInput, anchor.Checkpoint.SignatureB64):
            return ErrCheckpointInvalid

    // 7) (옵셔널) audit export cross-check
    if opts.AuditExportPath != "":
        chain := loadAuditChainCSV(opts.AuditExportPath)
        row := chain.LookupBySeq(anchor.Seq)
        if row == nil || row.Hash != anchor.HeadHash:
            return ErrAnchorChainMismatch
        if !chain.IntegrityVerify(uptoSeq=anchor.Seq):  // hash chain 무결성
            return ErrChainBroken

    return Result{ OK: true, KeyID: sp.KeyID, AnchorSeq: anchor.Seq, ... }
```

### 4.1 CLI 출력 (Phase 1)

```
$ rosshield report verify ./acme-2026-04-29.pdf
OK  report=rp_8c2d1f signer=key_a3f1c9b27e840a2c signed_at=2026-04-29T12:34:56Z
    body_sha256=a4f1e9...  audit_anchor: tn_acme seq=12345 head=a3f1c9b2...
    checkpoint: seq=12300 hash=78ee... (verified)
    cross-check: skipped (no --audit-export)
exit 0
```

실패 시:

```
FAIL  ErrBodyHashMismatch
      computed=b9d2... expected=a4f1...
      → PDF 본체가 서명 이후 변조됨 (페이지 추가·텍스트 수정 가능성)
exit 2
```

### 4.2 종료 코드 규약

- `0` 모두 통과
- `2` 서명/해시 실패 (변조 의심)
- `3` 키/공개키 부재
- `4` cross-check 불일치
- `1` 사용자 입력 오류 (파일 없음 등)

---

## 5. 함정 및 테스트 케이스

### 5.1 테스트 매트릭스 (Phase 1 필수 7종)

| # | 시나리오 | 입력 변형 | 기대 결과 |
|---|---|---|---|
| T1 | 정상 검증 | 산출 PDF 그대로 | exit 0 |
| T2 | PDF 페이지 추가 (PyPDF로 새 페이지 append) | `report.pdf` 본체에 페이지 삽입 | `ErrBodyHashMismatch` |
| T3 | PDF 페이지 삭제 | 마지막 페이지 제거 | `ErrBodyHashMismatch` |
| T4 | PDF 텍스트 수정 (binary edit으로 한 글자 변경) | bodyBytes 내 1 byte flip | `ErrBodyHashMismatch` |
| T5 | trailing block의 anchor 변조 | `seq` 값을 `12345 → 12346`로 수정 | `ErrSignatureInvalid` (서명 입력 변경) |
| T6 | 가시화 footer 텍스트만 변조 (JSON anchor 그대로) | PDF body 내 anchor 텍스트 수정 | `ErrBodyHashMismatch` (먼저) |
| T7 | 키 회전 후 옛 PDF 재검증 | trust store에 옛 keyID 공개키 유지 | exit 0 (옛 키도 trust store에 등록되어 있어야 함) |

추가 회귀:

- **R1 결정성**: 같은 `(tenant, session, report meta)` 입력 → byte-identical PDF (sha256 동일).
- **R2 멀티 서명 거부**: trailing block을 두 번 append한 PDF → 가장 마지막 블록만 사용, 검증 통과 (또는 정책상 거부 — Phase 1은 **거부**: `bytes.Count(raw, startMarker) != 1` 체크).

### 5.2 함정 7종

1. **PDF library의 random `/ID`**: 거의 모든 PDF writer가 첫 빌드 시 random `/ID [<...> <...>]` trailer 두 개를 박는다. 결정성을 위해 **양쪽 모두 입력 기반 deterministic**으로 강제 (ex: `sha256(tenant||report_id||schema_version)` 앞 16바이트). 라이브러리가 override 미지원 시 **사후 byte 패칭**.
2. **`/CreationDate`, `/ModDate`**: `D:20260429123456+09'00'` 형식. `time.Now()` 대신 `report.signed_at`(또는 `0`)으로 고정.
3. **`/Producer`, `/Creator`**: 라이브러리 버전 문자열이 박혀 build 환경에 따라 변동. 빈 문자열 또는 `rosshield` 고정.
4. **폰트 임베딩 random subset 태그**: TrueType subset의 `BAAAAA+FontName` 6글자 prefix가 hash 기반인지 random인지 라이브러리 따라 다름. 결정적 라이브러리 선택 또는 single full-font embed.
5. **PDF object stream 압축의 비결정성**: `flate` 압축 레벨·strategy가 build마다 미세 변동 가능 → 압축 레벨 고정(예: `BestSpeed=1`) 또는 압축 비활성.
6. **대용량 PDF의 streaming hash**: 본체가 100MB 넘을 수 있으니 `sha256.New()` 스트리밍 (전체 메모리 로드 금지). 검증 측도 동일 패턴.
7. **trailing block marker가 본체 안에 우연 출현**: `%%FG-SIG-V1` 문자열이 PDF stream content에 등장할 가능성 — 마커는 **`\n` 둘러싼 형태**로 잡고 `bytes.LastIndex`로 검색해 본체 끝부터 역방향. PDF stream에는 보통 raw `\n%%...\n` 패턴이 안 들어가지만, 혹시를 대비해 마커에 충돌 낮은 토큰 (V1 + 7자) 사용.

### 5.3 키 회전 정책

- **trust store**: `~/.rosshield/trust/keys/*.pub` (raw 32B Ed25519 public key) — keyID로 lookup.
- 회전 시 새 keyID 생성 → 옛 키는 **버리지 않고 trust store에 유지** → 옛 리포트 검증 가능.
- 회전 절차 자체가 audit entry (`signer.rotate`).
- Phase 1: 수동 회전 CLI (`rosshield signer rotate --kind report`). 자동 회전은 Phase 2.

---

## 6. 결정성 PDF 권장 (라이브러리 평가 cross-ref)

PDF 라이브러리 결정은 **별도 노트에서 진행 중** (E8 라이브러리 평가). 본 노트는 **결정성 요건**만 명시하고 라이브러리 선택은 그쪽에서.

### 6.1 라이브러리 결정성 체크리스트 (E8 lib 평가 노트로 전달)

선정 후보가 **모두 충족해야** R10-T가 작동한다:

| 체크 | 통과 조건 |
|---|---|
| `/ID` 강제 가능 | API로 두 ID 모두 지정 가능 또는 사후 패치 가능 |
| `/CreationDate`, `/ModDate` 강제 가능 | 입력값 사용, 내부 `time.Now()` 호출 없음 |
| `/Producer` 강제 가능 | 빈 문자열 또는 사용자 지정 |
| 폰트 subset prefix 결정적 | hash 기반 또는 prefix override |
| 압축 결정적 | 같은 입력 → 같은 압축 출력 |
| Append 안전 | writer가 EOF 마커를 한 번만 출력, 그 후 사용자가 trailing block 추가 가능 |

### 6.2 회피 패턴 (라이브러리 미지원 시)

- **사후 byte 패칭**: PDF 빌드 후 정규식으로 `/ID`, `/CreationDate`, `/Producer` 등을 deterministic 값으로 교체. 본체 마지막 `%%EOF` 위치는 변하지 않도록 길이 보존(공백 패딩).
- **재현 빌드 가드**: 같은 입력으로 두 번 빌드 후 sha256 비교, 다르면 빌드 실패. CI에 추가.

### 6.3 PDF/A 호환은 비목표

- PDF/A는 결정성 + 폰트 임베딩 강제 + 외부 의존 없음 등 우리 요건과 정렬되지만, 검증 자체가 무겁고 리더 호환성이 우선이 아니므로 Phase 1 범위 외.

---

## 7. 대안과 미래 확장

### 7.1 보류 (Phase 2 이후)

- **PAdES B-LT (long-term validation)**: TSA timestamp + 인증서 chain. 정부·금융 감사 시 요구될 수 있음. Phase 2 SKU 검토.
- **Multi-signature**: 여러 서명자(예: 보안팀 + 컴플라이언스팀). trailing block을 array로 확장하면 R10-T로 자연스럽게 지원 가능.
- **HSM/TPM 키**: signer 인터페이스(`internal/platform/signer`) 어댑터로 추가. 서명 형식은 변경 없음.
- **`rosshield-verify` standalone OSS 바이너리**: §10.6 fg-verify 도구. R10-T 검증을 별도 single-binary로 추출.

### 7.2 비채택 (영구)

- **PAdES Phase 1 도입**: 비용·복잡도·결정성 모두 손해. Phase 2 SKU 평가.
- **GPG armor 서명**: 외부 의존(`gnupg` CLI 또는 Go GPG 라이브러리), 키 관리 모델 충돌. 거부.
- **JWT/JWS in PDF**: `alg: EdDSA` JWS는 가능하지만 base64 3-segment 자체로 PDF에 박는 건 trailing block 형식의 부분집합. 추가 가치 없음.

---

## 8. 핵심 결론 5줄

1. **R10-T = trailing block(옵션 D) + .sig(옵션 C) 보조** — 단일 파일 + 외부 검증 가능 + 결정성 + 의존 0의 4박자.
2. **R11-A = audit_anchor를 signed payload 안에 매립 + 마지막 페이지 footer 가시화** — 두 위치 일치 검증으로 본체 변조 동시 탐지.
3. **검증 5단계 = sig 추출 → body sha256 → Ed25519.Verify → anchor 추출 → optional cross-check** — `rosshield report verify <pdf>` CLI로 오프라인 완결, exit code 0/2/3/4로 신호.
4. **결정성이 R10-T의 전제** — PDF 라이브러리가 `/ID`·`/CreationDate`·`/Producer`·폰트 subset prefix 결정적이거나 사후 패치 가능해야 함. CI에 재현 빌드 가드 필수.
5. **테스트 7종 + 함정 7종 식별** — 페이지 추가/삭제/텍스트 수정/anchor 변조/footer 변조/키 회전/멀티 서명 거부, 그리고 random ID·압축 비결정성·marker 충돌 등 Phase 1에서 해소.
