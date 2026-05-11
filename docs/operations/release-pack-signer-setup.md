# Release Pack Signer 설정

> **대상**: rosshield repo 운영자(release를 publish하는 maintainer).
> **목적**: production binary가 GitHub Actions release-pipeline workflow에서 ed25519 키로 built-in pack을 archive하도록 GitHub Actions secret을 등록.

## 배경

`internal/builtin/packs/embed.go`는 두 trust 키를 보유합니다:

| Trust | KeyID | 용도 |
|---|---|---|
| dev | `rosshield-dev-pack-signer-2026` | 본 repo dev 머신에서 `make pack-archive`로 archive한 pack 검증 |
| release | `rosshield-release-pack-signer-2026` | GitHub Actions release-pipeline에서 archive한 pack 검증 |

dev signer는 `scripts/dev-pack-signer.{key gitignore, pub.hex commit}`. private key는 dev 머신에 머무름.

release signer는 `scripts/release-pack-signer.{key gitignore, pub.hex commit}`. private key는 GitHub Actions secret으로 등록.

## 1회 등록 절차

### Step 1: release signer keypair 생성 (이미 완료)

본 repo는 첫 commit 시 release signer keypair를 생성한 상태:
- `scripts/release-pack-signer.key` — 64 bytes raw ed25519 private (gitignore)
- `scripts/release-pack-signer.pub.hex` — 64 hex chars + LF (commit)

`internal/builtin/packs/embed.go:devSignerPublicKeyHex`·`releaseSignerPublicKeyHex` 상수가 두 .pub.hex와 동기화.

키 회전 시 (드물게):
```bash
make pack-tools-build
bin/pack-tools keygen \
  -out scripts/release-pack-signer.key \
  -pub-out scripts/release-pack-signer.pub.hex \
  -force
# embed.go의 releaseSignerPublicKeyHex 상수도 새 .pub.hex 내용으로 교체
```

### Step 2: GitHub Actions secret으로 등록

```bash
# private key를 base64로 encode (single-line):
base64 -w0 scripts/release-pack-signer.key
```

GitHub UI: `Settings → Secrets and variables → Actions → New repository secret`
- Name: `ROSSHIELD_PACK_SIGNER_KEY`
- Value: 위 명령 출력 base64 string

또는 gh CLI:
```bash
gh secret set ROSSHIELD_PACK_SIGNER_KEY --body "$(base64 -w0 scripts/release-pack-signer.key)"
```

### Step 3: 첫 release 검증

tag push 후 release-pipeline workflow 로그 확인:
- `Decode release pack signer key` step에서 "Release signer key decoded (64 bytes)." 메시지
- `Build built-in pack archives` step에서 archive 성공 + `_archives/` 두 .tar.gz 파일

production binary 부팅 시 `seedBuiltinPacks` 로그:
```
seedBuiltinPacks: installed filename=cis-ubuntu-2404.tar.gz signerKeyId=rosshield-release-pack-signer-2026
```

## 누락 시 동작

`ROSSHIELD_PACK_SIGNER_KEY` secret 미등록 → workflow가 임시 dev signer를 새로 생성해 fallback. 결과:
- 새 keypair → pubkey가 binary embed된 dev pubkey 상수와 mismatch
- seed loader가 dev trust 시도 실패 → release trust 시도 실패 (release secret 없음)
- built-in pack 0개로 부팅 (`degraded mode` warn 로그)

이 경우 사용자가 별도 pack을 수동 install해야 스캔 가능. 첫 production release 전에 secret 등록 필수.

## 보안 주의

- `scripts/release-pack-signer.key` 절대 commit 금지 (.gitignore가 *.key 차단)
- 키 노출 시 새 keypair 생성 + embed.go 상수 + secret 모두 교체 + 이전 release 모두 재발행
- private key는 빌드 머신과 GitHub Actions runner에만 존재해야 함
- 대안 (장기): release signer를 cosign keyless + Sigstore Rekor로 이전(별 epic)

## 관련 파일

- `.github/workflows/release-pipeline.yml` — release secret 사용 단계
- `internal/builtin/packs/embed.go` — pubKey 상수 + trust bundle
- `cmd/pack-tools/main.go keygen` — keypair 생성 도구
- `cmd/rosshield-server/seed_packs.go` — first-boot trust bundle 시도 logic
