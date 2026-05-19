//go:build rosshield_enterprise

// cosign.go — WASM 정책의 cosign 호환 keyed 서명 검증 (C-1 v2).
//
// 본 파일은 stdlib (crypto/ecdsa + crypto/ed25519 + crypto/rsa + crypto/x509 +
// encoding/pem) 만으로 동작하는 Keyed 서명 검증 구현을 제공합니다. 외부 의존성 0.
//
// 본 round 의 범위 (design doc §6.4):
//   - Keyed verification : 호출자가 공개 키를 직접 보유 (PEM 또는 crypto.PublicKey).
//     서명은 raw bytes (ECDSA: ASN.1 DER, ed25519: 64 byte raw, RSA: PKCS#1 v1.5).
//
// 본 round 의 비목표:
//   - Sigstore keyless (Fulcio cert chain + Rekor entry + OIDC identity) — 후속 round.
//     이유: sigstore/cosign Go 모듈은 무거운 dep + cgo (rekor client). customer 별
//     환경 차이가 커 별 어댑터 패턴으로 분리.
//
// 결정론 보장:
//   - 같은 (policy bytes, signature bytes, public key) → 같은 verify 결과.
//   - ECDSA verify 는 결정론 (verify 자체는 random 사용하지 않음, sign 만 사용).
//   - ed25519 verify 는 결정론 (ed25519 자체가 deterministic).
//   - RSA verify 는 결정론 (PKCS#1 v1.5).
//
// 알고리즘 선택:
//   - cosign default 는 ECDSA P-256 + SHA-256 (cosign generate-key-pair 출력).
//   - ed25519 도 지원 (cosign 에서 옵션, 일부 환경).
//   - RSA 는 옵션 — 일부 enterprise PKI 통합 (PKCS#1 v1.5 + SHA-256).
//
// 참조:
//   - docs/design/notes/phase7-public-transition-design.md §6.4 C-1
//   - sigstore/cosign Verifier interface — https://pkg.go.dev/github.com/sigstore/sigstore

package wasmrt

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
)

// 추가 sentinel — 본 파일 전용.
var (
	// ErrUnsupportedSignatureAlgorithm 는 PublicKey 가 지원되지 않는 type 일 때
	// 반환됩니다 (현재 지원: ECDSA, ed25519, RSA — 그 외 DSA/X25519 등은 거부).
	ErrUnsupportedSignatureAlgorithm = errors.New("wasmrt: unsupported signature algorithm")

	// ErrInvalidPublicKey 는 PEM bytes 의 디코드 또는 x509 파싱 실패 시 반환됩니다.
	ErrInvalidPublicKey = errors.New("wasmrt: invalid public key")
)

// CosignKeyedVerifier 는 직접 보유한 공개 키로 정책 서명을 검증합니다.
//
// cosign 의 "keyed" mode 와 호환됩니다 — Fulcio/Rekor 미사용.
//
// 필드:
//   - PublicKey : *ecdsa.PublicKey | ed25519.PublicKey | *rsa.PublicKey 중 하나.
//   - HashFn    : 정책 바이트에 적용할 해시. 보통 crypto.SHA256.
//     ed25519 인 경우 HashFn 은 무시됨 (ed25519 은 자체 hash 포함).
//     zero 값(0) 은 crypto.SHA256 으로 기본 설정.
//
// 서명 형식:
//   - ECDSA   : ASN.1 DER encoded (R, S) — cosign 표준 출력.
//   - ed25519 : raw 64 byte signature.
//   - RSA     : PKCS#1 v1.5.
//
// 본 type 은 immutable — Verify 호출은 mutation 없습니다 (스레드-safe).
type CosignKeyedVerifier struct {
	PublicKey crypto.PublicKey
	HashFn    crypto.Hash
}

// Verify 는 policy bytes 의 서명을 PublicKey 로 검증합니다.
//
// 절차:
//  1. 입력 validation : nil public key / empty signature 거부.
//  2. policy hash 계산 (ed25519 제외).
//  3. key type 분기 : ecdsa.VerifyASN1 / ed25519.Verify / rsa.VerifyPKCS1v15.
//  4. 실패 시 ErrPolicySignatureInvalid 반환 (wrap).
//
// 오류 분류:
//   - PublicKey nil          → ErrUnsupportedSignatureAlgorithm wrap.
//   - PublicKey unknown type → ErrUnsupportedSignatureAlgorithm wrap.
//   - signature empty        → ErrPolicySignatureInvalid wrap.
//   - verify 실패            → ErrPolicySignatureInvalid wrap.
func (v *CosignKeyedVerifier) Verify(policy, signature []byte) error {
	if v == nil || v.PublicKey == nil {
		return fmt.Errorf("%w: nil public key", ErrUnsupportedSignatureAlgorithm)
	}
	if len(signature) == 0 {
		return fmt.Errorf("%w: empty signature", ErrPolicySignatureInvalid)
	}

	hashFn := v.HashFn
	if hashFn == 0 {
		hashFn = crypto.SHA256
	}

	switch pk := v.PublicKey.(type) {
	case *ecdsa.PublicKey:
		return verifyECDSA(pk, hashFn, policy, signature)
	case ed25519.PublicKey:
		return verifyEd25519(pk, policy, signature)
	case *rsa.PublicKey:
		return verifyRSAPKCS1v15(pk, hashFn, policy, signature)
	default:
		return fmt.Errorf("%w: %T", ErrUnsupportedSignatureAlgorithm, v.PublicKey)
	}
}

// verifyECDSA 는 ECDSA + ASN.1 DER signature 를 검증합니다 (cosign 표준).
func verifyECDSA(pk *ecdsa.PublicKey, hashFn crypto.Hash, policy, sig []byte) error {
	digest, err := computeDigest(hashFn, policy)
	if err != nil {
		return err
	}
	if !ecdsa.VerifyASN1(pk, digest, sig) {
		return fmt.Errorf("%w: ecdsa verify failed", ErrPolicySignatureInvalid)
	}
	return nil
}

// verifyEd25519 는 ed25519 + raw 64 byte signature 를 검증합니다.
//
// ed25519 는 자체 내부 hash (SHA-512) 를 가지므로 HashFn 은 사용하지 않습니다.
func verifyEd25519(pk ed25519.PublicKey, policy, sig []byte) error {
	if len(pk) != ed25519.PublicKeySize {
		return fmt.Errorf("%w: ed25519 public key size %d", ErrUnsupportedSignatureAlgorithm, len(pk))
	}
	if len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("%w: ed25519 signature size %d", ErrPolicySignatureInvalid, len(sig))
	}
	if !ed25519.Verify(pk, policy, sig) {
		return fmt.Errorf("%w: ed25519 verify failed", ErrPolicySignatureInvalid)
	}
	return nil
}

// verifyRSAPKCS1v15 는 RSA PKCS#1 v1.5 signature 를 검증합니다.
func verifyRSAPKCS1v15(pk *rsa.PublicKey, hashFn crypto.Hash, policy, sig []byte) error {
	digest, err := computeDigest(hashFn, policy)
	if err != nil {
		return err
	}
	if err := rsa.VerifyPKCS1v15(pk, hashFn, digest, sig); err != nil {
		return fmt.Errorf("%w: rsa verify: %v", ErrPolicySignatureInvalid, err)
	}
	return nil
}

// computeDigest 는 지원되는 hash 알고리즘 으로 policy bytes 의 digest 를 계산합니다.
//
// 지원: SHA-256, SHA-384, SHA-512.
// 그 외는 ErrUnsupportedSignatureAlgorithm.
func computeDigest(hashFn crypto.Hash, policy []byte) ([]byte, error) {
	switch hashFn {
	case crypto.SHA256:
		sum := sha256.Sum256(policy)
		return sum[:], nil
	case crypto.SHA384:
		sum := sha512.Sum384(policy)
		return sum[:], nil
	case crypto.SHA512:
		sum := sha512.Sum512(policy)
		return sum[:], nil
	default:
		return nil, fmt.Errorf("%w: hash %v", ErrUnsupportedSignatureAlgorithm, hashFn)
	}
}

// ParsePublicKeyPEM 은 PEM-encoded 공개 키 bytes 를 crypto.PublicKey 로 파싱합니다.
//
// 지원 PEM block type:
//   - "PUBLIC KEY"      : PKIX 형식 (ECDSA / ed25519 / RSA 모두) — cosign 표준 출력.
//   - "RSA PUBLIC KEY"  : PKCS#1 RSA only.
//   - "CERTIFICATE"     : x509 cert — cert.PublicKey 추출.
//
// 반환된 PublicKey 는 CosignKeyedVerifier.PublicKey 에 직접 설정 가능합니다.
//
// 오류:
//   - PEM decode 실패         → ErrInvalidPublicKey wrap.
//   - block type 미지원       → ErrInvalidPublicKey wrap.
//   - x509 parse 실패         → ErrInvalidPublicKey wrap.
//   - PublicKey type 미지원   → ErrUnsupportedSignatureAlgorithm wrap.
func ParsePublicKeyPEM(pemBytes []byte) (crypto.PublicKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("%w: no PEM block", ErrInvalidPublicKey)
	}
	switch block.Type {
	case "PUBLIC KEY":
		pk, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("%w: pkix parse: %v", ErrInvalidPublicKey, err)
		}
		return validatePublicKey(pk)
	case "RSA PUBLIC KEY":
		pk, err := x509.ParsePKCS1PublicKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("%w: pkcs1 parse: %v", ErrInvalidPublicKey, err)
		}
		return pk, nil
	case "CERTIFICATE":
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("%w: x509 cert parse: %v", ErrInvalidPublicKey, err)
		}
		return validatePublicKey(cert.PublicKey)
	default:
		return nil, fmt.Errorf("%w: unsupported PEM type %q", ErrInvalidPublicKey, block.Type)
	}
}

// validatePublicKey 는 PublicKey 가 본 패키지가 지원하는 type 인지 확인합니다.
func validatePublicKey(pk crypto.PublicKey) (crypto.PublicKey, error) {
	switch pk.(type) {
	case *ecdsa.PublicKey, ed25519.PublicKey, *rsa.PublicKey:
		return pk, nil
	default:
		return nil, fmt.Errorf("%w: %T", ErrUnsupportedSignatureAlgorithm, pk)
	}
}
