# rosshield-audit-verify

외부 감사인용 standalone 검증 binary (E30, R30-4).

rosshield-server·rosshield CLI 없이 단일 binary만 다운로드하여 서명된 report
tar.gz 번들의 무결성·진위를 검증합니다.

## 빌드

```bash
make audit-verify-build
# → bin/rosshield-audit-verify
```

stdlib + `crypto/ed25519`만 사용 — 외부 의존 0.

## 사용법

### Bundle 검증 (E30 본체)

```bash
rosshield-audit-verify --bundle <path.tar.gz> [--format json|table] [--strict]
```

### Rotation segment 검증 (E32 Stage 5)

```bash
# 단일 segment archive
rosshield-audit-verify rotation \
    --archive-uri file:///path/to/seg-000005.tar.gz \
    [--expected-sha256 <hex64>] \
    [--expected-segment-hash <hex64>] \
    [--prev-segment-hash <hex64>] \
    [--format table|json]

# chain batch (segment 간 prev_segment_hash 일관성)
rosshield-audit-verify rotation chain \
    --backend file:///path/to/audit-archives/<tenant>/ \
    --from-segment <N> [--to-segment <M>] \
    [--format table|json]
```

### Rotation + cosign keyless 통합 검증

archive 무결성과 cosign keyless 서명(Sigstore Fulcio + Rekor)을 한 번에 검증합니다. cosign
binary가 PATH에 있어야 하며, 검증 자체는 외부 `cosign verify-blob`을 위임 호출합니다.

```bash
# 단일 segment + bundle
rosshield-audit-verify rotation \
    --archive-uri file:///path/to/seg-000005.tar.gz \
    --cosign-bundle /path/to/seg-000005.cosign.bundle \
    --cosign-identity ci@example.com \
    --cosign-oidc-issuer https://accounts.google.com

# chain mode (각 segment 옆 seg-NNNNNN.cosign.bundle 자동 검색)
rosshield-audit-verify rotation chain \
    --backend file:///path/to/audit-archives/<tenant>/ \
    --from-segment 1 --to-segment 10 \
    --cosign-bundle-dir /path/to/bundles/ \
    --cosign-identity ci@example.com \
    --cosign-oidc-issuer https://accounts.google.com
```

cosign flag가 모두 비어 있으면 verify는 skip되고 `cosignVerifyMatch=true(skipped)`로 표시됩니다.

상세 가이드:
- [`docs/onboarding/audit-rotation-verify.md`](../../docs/onboarding/audit-rotation-verify.md) — segment 무결성
- [`docs/onboarding/audit-rotation-cosign.md`](../../docs/onboarding/audit-rotation-cosign.md) — cosign keyless

옵션:

- `--bundle`  검증할 report tar.gz 번들 경로 (필수)
- `--format`  출력 포맷 (`table` 기본 / `json`)
- `--strict`  warning을 fail로 처리 (현 단계 no-op, 미래 확장)

## exit code

| code | 의미 |
|------|------|
| 0 | PASS — 모든 단계 통과 |
| 1 | FAIL — 검증 실패 (서명·entry 부재·tar.gz 손상·anchor) |
| 2 | ARG  — invalid CLI args |

## 검증 단계

1. `read`      — tar.gz 파일 읽기
2. `extract`   — 번들 entry 4종 (`report.pdf`·`report.pdf.sig`·`audit-chain-head.json`·`public-key.pem`) 모두 존재
3. `publicKey` — PEM PKIX → Ed25519 PublicKey 디코드
4. `signature` — `ed25519.Verify(pub, pdf, sig)` 통과
5. `anchor`    — `audit-chain-head.json`의 `chainHeadSeq`·`chainHeadHash`·`signerKeyId` 추출
6. `evidence`  — PDF sha256 + size 노출 (감사인이 별도 채널의 hash와 cross-check 가능)

## 예시

```bash
$ rosshield-audit-verify --bundle report-2026.tar.gz
RESULT       PASS
bundle       report-2026.tar.gz
pdfSize      48217
pdfSha256    9f2c...
signerKeyId  rosshield-prod-2026-q2
chainHeadSeq 12031
...
PASS — bundle verification successful.
```
