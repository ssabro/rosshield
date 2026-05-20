# Audit Rotation cosign keyless 서명 가이드

> **대상**: 운영자 (Lodestar 서버 관리) + 외부 감사인 (서명 검증).
> **목표**: rotated audit segment archive를 cosign keyless(Sigstore)로 서명하여, customer/감사인이
> 단독으로 archive 무결성 + 작성 주체(OIDC identity)를 외부 verify CLI로 검증할 수 있도록 한다.
> **선행**: `audit-rotation-verify.md` (archive 구조 + 무결성 검증 기본).
> **결정**: D-AR-4 — 옵션 A (외부 `cosign` CLI 채택). 옵션 B(sigstore-go SDK 임베드)는 별 epic.

---

## 1. cosign keyless가 의미하는 것

전통적 ed25519/RSA 서명은 long-lived private key를 관리해야 하지만, Sigstore keyless는:

- **Fulcio CA**가 OIDC identity(`admin@example.com` 등)에 대해 단기(10분) X.509 인증서를 발급.
- **Rekor** 투명성 로그에 서명 metadata가 append-only로 기록.
- private key 보관 불요 — identity + Rekor inclusion proof가 검증의 근거.

서명 결과(bundle)는 self-contained — 인증서 + 서명 + Rekor SET(Signed Entry Timestamp) 묶음으로
verify 시 외부 네트워크 의존 없이 검증 가능(public Fulcio root + Rekor public key는 binary embed).

---

## 2. Lodestar 적용 모델

- rotation 발생 시 archive tar.gz bytes를 `cosign sign-blob`로 서명.
- bundle bytes는 `audit_rotation_segments.cosign_bundle BYTEA` 컬럼(마이그레이션 0032에서 reserve)에 저장.
- bundle 길이로 활성 여부를 판단하지 않음 — config(`COSIGN_ENABLED`) 별도.
- 비활성(에어갭 customer default)이면 bundle은 NULL — `segment_hash` + `archive_sha256`만으로 결정론적
  검증 유지(audit-rotation-verify.md).

---

## 3. cosign binary 설치

리눅스(amd64):

```bash
curl -L https://github.com/sigstore/cosign/releases/latest/download/cosign-linux-amd64 \
  -o /usr/local/bin/cosign
chmod +x /usr/local/bin/cosign
cosign version
```

macOS:

```bash
brew install cosign
```

윈도우:

```powershell
$dest = "C:\Program Files\cosign\cosign.exe"
New-Item -ItemType Directory -Force "C:\Program Files\cosign" | Out-Null
Invoke-WebRequest `
  -Uri "https://github.com/sigstore/cosign/releases/latest/download/cosign-windows-amd64.exe" `
  -OutFile $dest
& $dest version
```

에어갭 환경:

- public Fulcio/Rekor 도달 불가 → `COSIGN_ENABLED=false` 권장(default).
- 내부 Fulcio/Rekor 운영이 가능하면 `COSIGN_FULCIO_URL` / `COSIGN_REKOR_URL`로 가리키되,
  내부 OIDC IdP까지 함께 운영해야 함(별 epic).

---

## 4. 환경 변수 설정

`/etc/rosshield/rosshield.env` (예):

```
ROSSHIELD_COSIGN_ENABLED=true
ROSSHIELD_COSIGN_BINARY=/usr/local/bin/cosign
ROSSHIELD_COSIGN_IDENTITY=admin@example.com
# 빈 값이면 Sigstore public Fulcio/Rekor 사용 — 일반 운영자는 기본값 권장.
# ROSSHIELD_COSIGN_FULCIO_URL=
# ROSSHIELD_COSIGN_REKOR_URL=
```

또는 flag로 (env가 더 권장 — secret 누출 방지):

```bash
rosshield-server \
  --cosign-enabled \
  --cosign-binary=/usr/local/bin/cosign \
  --cosign-identity=admin@example.com \
  --audit-rotation-schedule="0 0 1 * *"
```

우선순위: flag → env. 둘 다 비면 default 비활성.

---

## 5. OIDC token 공급 방식

cosign keyless는 서명 시 OIDC identity token이 필요. 다음 중 하나:

1. **CI 환경(GitHub Actions / GitLab CI)**: ambient token 자동 사용. `ROSSHIELD_COSIGN_IDENTITY`에 워크플로
   identity(e.g. `https://github.com/ssabro/rosshield/.github/workflows/release.yml@refs/tags/v0.6.8`) 기록.
2. **수동 서명/배치**: `cosign login` 또는 `COSIGN_OIDC_TOKEN` env로 미리 token 발급. 일반 customer 운영
   서버는 cron rotation을 service account로 돌리므로 이 방식 권장.
3. **interactive (개발 환경 only)**: 브라우저 popup. 서버 배포에서는 금지 — `--yes` flag로 confirmation
   skip되지만 OIDC 흐름 자체는 token 부재 시 fail.

---

## 6. bundle 검증 (감사인 단독 절차)

archive와 bundle 두 파일 확보 후 두 가지 방법:

### 6.1 권장 — `rosshield-audit-verify` 통합 (segment + cosign 한 번에)

```bash
# rosshield-audit-verify는 release page에서 standalone binary로 배포 (R30-4 외부 검증).
rosshield-audit-verify rotation \
  --archive-uri file://$PWD/seg-000005.tar.gz \
  --expected-segment-hash <hex64> \
  --cosign-bundle seg-000005.bundle \
  --cosign-identity admin@example.com \
  --cosign-oidc-issuer https://accounts.google.com

# chain mode — 여러 segment + bundle 일괄 검증:
rosshield-audit-verify rotation chain \
  --backend file:///var/lib/rosshield/audit-archives/tn_acme/ \
  --from-segment 1 --to-segment 10 \
  --cosign-bundle-dir ./bundles/ \
  --cosign-identity admin@example.com \
  --cosign-oidc-issuer https://accounts.google.com
```

verify CLI는 segment_hash + prev_segment_hash chain + cosign verify-blob을 한 번에 수행하고
JSON/table 양쪽 출력으로 stepResult를 노출 — `cosignVerifyMatch: true` 필드로 자동 처리 가능.

### 6.2 기본 `cosign verify-blob` 직접 (외부 인프라 + binary만)

```bash
# 1) hot DB에서 segment row 추출 (운영자 협조 또는 backup dump).
psql -c "SELECT encode(cosign_bundle, 'base64'), archive_uri \
         FROM audit_rotation_segments \
         WHERE tenant_id = 'tn_acme' AND segment_number = 5;" \
  | base64 -d > seg-000005.bundle

# 2) archive 다운로드 (file:// 또는 s3://).
cp /var/lib/rosshield/audit-archives/tn_acme/seg-000005.tar.gz .

# 3) cosign verify-blob.
cosign verify-blob \
  --bundle seg-000005.bundle \
  --certificate-identity admin@example.com \
  --certificate-oidc-issuer https://accounts.google.com \
  seg-000005.tar.gz
```

성공 시 `Verified OK` 출력. 실패는 두 경우:

- archive 변조 → SHA mismatch.
- identity/issuer 불일치 → 다른 주체가 서명. 운영자에게 expected identity 확인.

---

## 7. 운영 권장

- `ROSSHIELD_COSIGN_IDENTITY`는 단일 service account로 고정 — rotation 주체가 바뀌면 감사인 측에서
  identity 추적 어려움.
- 첫 enterprise customer 도입 전 internal Sigstore(또는 BYO Fulcio/Rekor)로 전환 검토 — public Sigstore는
  rate limit + privacy(public transparency log 노출) 제약.
- bundle column 부재(NULL) segment는 v0.6.7 이전 rotation — 별 epic에서 후행 backfill 도구 필요.

---

## 8. 한계 (본 round)

- `rosshield-server`는 `cosign sign-blob` 외부 CLI execution. binary 부재 시 rotation Tx rollback →
  운영 모니터링 권장.
- `rosshield-audit-verify` CLI도 `cosign verify-blob`을 외부 호출 — verify 도구는 stdlib + 도메인 hash
  만 의존하는 원칙(E30) 유지를 위해 sigstore-go SDK 임베드 회피. cosign binary는 감사인 환경에 별도 설치 필요.
- e2e 자동 test는 build tag `cosign_e2e`로 격리(CI default skip).
- in-process sigstore-go SDK 임베드(옵션 B)는 future round — binary 의존 0이 강하게 요구되면 검토.
