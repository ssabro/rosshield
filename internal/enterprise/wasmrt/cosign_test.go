//go:build rosshield_enterprise

package wasmrt

import (
	"context"
	"crypto"
	"crypto/dsa"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"testing"
)

// --- helpers: in-memory key generation ---------------------------------------

func mustGenECDSAP256(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa gen: %v", err)
	}
	return k
}

func mustGenEd25519(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519 gen: %v", err)
	}
	return pub, priv
}

func mustGenRSA2048(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa gen: %v", err)
	}
	return k
}

func mustSignECDSA(t *testing.T, k *ecdsa.PrivateKey, policy []byte) []byte {
	t.Helper()
	digest := sha256.Sum256(policy)
	sig, err := ecdsa.SignASN1(rand.Reader, k, digest[:])
	if err != nil {
		t.Fatalf("ecdsa sign: %v", err)
	}
	return sig
}

func mustSignEd25519(t *testing.T, k ed25519.PrivateKey, policy []byte) []byte {
	t.Helper()
	return ed25519.Sign(k, policy)
}

func mustSignRSA(t *testing.T, k *rsa.PrivateKey, policy []byte) []byte {
	t.Helper()
	digest := sha256.Sum256(policy)
	sig, err := rsa.SignPKCS1v15(rand.Reader, k, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("rsa sign: %v", err)
	}
	return sig
}

// --- ECDSA P-256 keyed verify -----------------------------------------------

func TestCosignKeyedVerifier_ecdsa_p256_valid_signature_통과(t *testing.T) {
	priv := mustGenECDSAP256(t)
	policy := []byte("policy bytes for ecdsa")
	sig := mustSignECDSA(t, priv, policy)

	v := &CosignKeyedVerifier{PublicKey: &priv.PublicKey, HashFn: crypto.SHA256}
	if err := v.Verify(policy, sig); err != nil {
		t.Errorf("valid ecdsa sig 거부: %v", err)
	}
}

func TestCosignKeyedVerifier_ecdsa_default_hash는_sha256(t *testing.T) {
	priv := mustGenECDSAP256(t)
	policy := []byte("default hash test")
	sig := mustSignECDSA(t, priv, policy)

	// HashFn 0 (zero value) → 자동 sha256.
	v := &CosignKeyedVerifier{PublicKey: &priv.PublicKey}
	if err := v.Verify(policy, sig); err != nil {
		t.Errorf("default hash sha256 가정 실패: %v", err)
	}
}

func TestCosignKeyedVerifier_ecdsa_변조된_policy_거부(t *testing.T) {
	priv := mustGenECDSAP256(t)
	policy := []byte("original policy bytes")
	sig := mustSignECDSA(t, priv, policy)

	tampered := append([]byte{}, policy...)
	tampered[0] ^= 0x01 // 1 byte 변경

	v := &CosignKeyedVerifier{PublicKey: &priv.PublicKey, HashFn: crypto.SHA256}
	err := v.Verify(tampered, sig)
	if !errors.Is(err, ErrPolicySignatureInvalid) {
		t.Errorf("변조 policy: got %v, want ErrPolicySignatureInvalid", err)
	}
}

func TestCosignKeyedVerifier_ecdsa_잘못된_signature_거부(t *testing.T) {
	priv := mustGenECDSAP256(t)
	policy := []byte("policy")
	badSig := []byte{0x30, 0x44, 0x02, 0x20, 0x00, 0x00, 0x00, 0x00} // 형식만 ASN.1 비슷.

	v := &CosignKeyedVerifier{PublicKey: &priv.PublicKey, HashFn: crypto.SHA256}
	err := v.Verify(policy, badSig)
	if !errors.Is(err, ErrPolicySignatureInvalid) {
		t.Errorf("잘못된 sig: got %v, want ErrPolicySignatureInvalid", err)
	}
}

func TestCosignKeyedVerifier_ecdsa_다른_key_거부(t *testing.T) {
	priv1 := mustGenECDSAP256(t)
	priv2 := mustGenECDSAP256(t)
	policy := []byte("cross-key test")
	sig := mustSignECDSA(t, priv1, policy)

	v := &CosignKeyedVerifier{PublicKey: &priv2.PublicKey, HashFn: crypto.SHA256}
	err := v.Verify(policy, sig)
	if !errors.Is(err, ErrPolicySignatureInvalid) {
		t.Errorf("다른 key: got %v, want ErrPolicySignatureInvalid", err)
	}
}

// --- ed25519 keyed verify ----------------------------------------------------

func TestCosignKeyedVerifier_ed25519_valid_signature_통과(t *testing.T) {
	pub, priv := mustGenEd25519(t)
	policy := []byte("ed25519 policy")
	sig := mustSignEd25519(t, priv, policy)

	v := &CosignKeyedVerifier{PublicKey: pub}
	if err := v.Verify(policy, sig); err != nil {
		t.Errorf("valid ed25519 sig 거부: %v", err)
	}
}

func TestCosignKeyedVerifier_ed25519_변조된_policy_거부(t *testing.T) {
	pub, priv := mustGenEd25519(t)
	policy := []byte("ed25519 original")
	sig := mustSignEd25519(t, priv, policy)

	tampered := append([]byte{}, policy...)
	tampered[0] ^= 0x01

	v := &CosignKeyedVerifier{PublicKey: pub}
	err := v.Verify(tampered, sig)
	if !errors.Is(err, ErrPolicySignatureInvalid) {
		t.Errorf("변조 policy: got %v, want ErrPolicySignatureInvalid", err)
	}
}

func TestCosignKeyedVerifier_ed25519_잘못된_signature_size_거부(t *testing.T) {
	pub, _ := mustGenEd25519(t)
	policy := []byte("ed25519")
	badSig := []byte{0x01, 0x02, 0x03} // 64 byte 아님

	v := &CosignKeyedVerifier{PublicKey: pub}
	err := v.Verify(policy, badSig)
	if !errors.Is(err, ErrPolicySignatureInvalid) {
		t.Errorf("잘못된 sig size: got %v, want ErrPolicySignatureInvalid", err)
	}
}

func TestCosignKeyedVerifier_ed25519_다른_key_거부(t *testing.T) {
	pub1, _ := mustGenEd25519(t)
	_, priv2 := mustGenEd25519(t)
	policy := []byte("cross key ed25519")
	sig := mustSignEd25519(t, priv2, policy)

	v := &CosignKeyedVerifier{PublicKey: pub1}
	err := v.Verify(policy, sig)
	if !errors.Is(err, ErrPolicySignatureInvalid) {
		t.Errorf("다른 key: got %v, want ErrPolicySignatureInvalid", err)
	}
}

// --- RSA PKCS#1 v1.5 keyed verify --------------------------------------------

func TestCosignKeyedVerifier_rsa_pkcs1v15_valid_signature_통과(t *testing.T) {
	priv := mustGenRSA2048(t)
	policy := []byte("rsa policy")
	sig := mustSignRSA(t, priv, policy)

	v := &CosignKeyedVerifier{PublicKey: &priv.PublicKey, HashFn: crypto.SHA256}
	if err := v.Verify(policy, sig); err != nil {
		t.Errorf("valid rsa sig 거부: %v", err)
	}
}

func TestCosignKeyedVerifier_rsa_변조된_policy_거부(t *testing.T) {
	priv := mustGenRSA2048(t)
	policy := []byte("rsa original")
	sig := mustSignRSA(t, priv, policy)

	tampered := append([]byte{}, policy...)
	tampered[0] ^= 0x01

	v := &CosignKeyedVerifier{PublicKey: &priv.PublicKey, HashFn: crypto.SHA256}
	err := v.Verify(tampered, sig)
	if !errors.Is(err, ErrPolicySignatureInvalid) {
		t.Errorf("변조 policy: got %v, want ErrPolicySignatureInvalid", err)
	}
}

// --- 기본 입력 validation ----------------------------------------------------

func TestCosignKeyedVerifier_nil_receiver_미지원(t *testing.T) {
	var v *CosignKeyedVerifier
	err := v.Verify([]byte("x"), []byte("y"))
	if !errors.Is(err, ErrUnsupportedSignatureAlgorithm) {
		t.Errorf("nil receiver: got %v, want ErrUnsupportedSignatureAlgorithm", err)
	}
}

func TestCosignKeyedVerifier_nil_publickey_미지원(t *testing.T) {
	v := &CosignKeyedVerifier{PublicKey: nil}
	err := v.Verify([]byte("x"), []byte("y"))
	if !errors.Is(err, ErrUnsupportedSignatureAlgorithm) {
		t.Errorf("nil key: got %v, want ErrUnsupportedSignatureAlgorithm", err)
	}
}

func TestCosignKeyedVerifier_empty_signature_거부(t *testing.T) {
	priv := mustGenECDSAP256(t)
	v := &CosignKeyedVerifier{PublicKey: &priv.PublicKey, HashFn: crypto.SHA256}
	err := v.Verify([]byte("policy"), nil)
	if !errors.Is(err, ErrPolicySignatureInvalid) {
		t.Errorf("empty sig: got %v, want ErrPolicySignatureInvalid", err)
	}
}

func TestCosignKeyedVerifier_unsupported_key_type_미지원(t *testing.T) {
	// DSA 키 — 본 패키지가 지원하지 않는 type.
	dsaKey := &dsa.PublicKey{}
	v := &CosignKeyedVerifier{PublicKey: dsaKey, HashFn: crypto.SHA256}
	err := v.Verify([]byte("policy"), []byte("sig"))
	if !errors.Is(err, ErrUnsupportedSignatureAlgorithm) {
		t.Errorf("DSA key: got %v, want ErrUnsupportedSignatureAlgorithm", err)
	}
}

func TestCosignKeyedVerifier_unsupported_hash_미지원(t *testing.T) {
	priv := mustGenECDSAP256(t)
	v := &CosignKeyedVerifier{PublicKey: &priv.PublicKey, HashFn: crypto.MD5}
	err := v.Verify([]byte("policy"), []byte("sig-non-empty"))
	if !errors.Is(err, ErrUnsupportedSignatureAlgorithm) {
		t.Errorf("md5 hash: got %v, want ErrUnsupportedSignatureAlgorithm", err)
	}
}

// --- 결정론: 같은 입력 같은 결과 ---------------------------------------------

func TestCosignKeyedVerifier_결정론_ecdsa_같은_입력_같은_결과(t *testing.T) {
	priv := mustGenECDSAP256(t)
	policy := []byte("determinism")
	sig := mustSignECDSA(t, priv, policy)
	v := &CosignKeyedVerifier{PublicKey: &priv.PublicKey, HashFn: crypto.SHA256}

	err1 := v.Verify(policy, sig)
	err2 := v.Verify(policy, sig)
	if err1 != err2 {
		t.Errorf("결정론 위반: %v vs %v", err1, err2)
	}
}

// --- ParsePublicKeyPEM -------------------------------------------------------

func TestParsePublicKeyPEM_ecdsa_pkix_정상(t *testing.T) {
	priv := mustGenECDSAP256(t)
	derBytes, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatalf("marshal pkix: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: derBytes})

	pk, err := ParsePublicKeyPEM(pemBytes)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, ok := pk.(*ecdsa.PublicKey); !ok {
		t.Errorf("type: got %T, want *ecdsa.PublicKey", pk)
	}
}

func TestParsePublicKeyPEM_ed25519_pkix_정상(t *testing.T) {
	pub, _ := mustGenEd25519(t)
	derBytes, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatalf("marshal pkix: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: derBytes})

	pk, err := ParsePublicKeyPEM(pemBytes)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, ok := pk.(ed25519.PublicKey); !ok {
		t.Errorf("type: got %T, want ed25519.PublicKey", pk)
	}
}

func TestParsePublicKeyPEM_rsa_pkcs1_정상(t *testing.T) {
	priv := mustGenRSA2048(t)
	derBytes := x509.MarshalPKCS1PublicKey(&priv.PublicKey)
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: derBytes})

	pk, err := ParsePublicKeyPEM(pemBytes)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, ok := pk.(*rsa.PublicKey); !ok {
		t.Errorf("type: got %T, want *rsa.PublicKey", pk)
	}
}

func TestParsePublicKeyPEM_invalid_pem_거부(t *testing.T) {
	_, err := ParsePublicKeyPEM([]byte("not a pem block"))
	if !errors.Is(err, ErrInvalidPublicKey) {
		t.Errorf("invalid pem: got %v, want ErrInvalidPublicKey", err)
	}
}

func TestParsePublicKeyPEM_empty_거부(t *testing.T) {
	_, err := ParsePublicKeyPEM(nil)
	if !errors.Is(err, ErrInvalidPublicKey) {
		t.Errorf("empty: got %v, want ErrInvalidPublicKey", err)
	}
}

func TestParsePublicKeyPEM_unsupported_block_type_거부(t *testing.T) {
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte{0x00}})
	_, err := ParsePublicKeyPEM(pemBytes)
	if !errors.Is(err, ErrInvalidPublicKey) {
		t.Errorf("PRIVATE KEY block: got %v, want ErrInvalidPublicKey", err)
	}
}

func TestParsePublicKeyPEM_손상된_pkix_바이트_거부(t *testing.T) {
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: []byte{0xff, 0xff, 0xff}})
	_, err := ParsePublicKeyPEM(pemBytes)
	if !errors.Is(err, ErrInvalidPublicKey) {
		t.Errorf("손상된 pkix: got %v, want ErrInvalidPublicKey", err)
	}
}

func TestParsePublicKeyPEM_round_trip_verify_통과(t *testing.T) {
	// PEM 마샬 → 파싱 → verify 가 정상 동작하는 end-to-end.
	priv := mustGenECDSAP256(t)
	derBytes, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: derBytes})

	pk, err := ParsePublicKeyPEM(pemBytes)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	policy := []byte("round-trip e2e")
	sig := mustSignECDSA(t, priv, policy)

	v := &CosignKeyedVerifier{PublicKey: pk, HashFn: crypto.SHA256}
	if err := v.Verify(policy, sig); err != nil {
		t.Errorf("round-trip verify: %v", err)
	}
}

// --- Sentinel 고유성 (cosign 신규 sentinel 포함) ------------------------------

func TestCosign_sentinel_고유성(t *testing.T) {
	all := []error{
		ErrInvalidPolicy,
		ErrCPUTimeout,
		ErrMemoryExceeded,
		ErrStdoutTruncated,
		ErrInvalidOutput,
		ErrPolicySignatureInvalid,
		ErrRuntimeClosed,
		ErrUnsupportedSignatureAlgorithm,
		ErrInvalidPublicKey,
	}
	for i, a := range all {
		for j, b := range all {
			if i == j {
				continue
			}
			if errors.Is(a, b) {
				t.Errorf("sentinel %v == %v (구분 불가)", a, b)
			}
		}
	}
}

// --- Runtime 통합: CosignKeyedVerifier 가 Runtime 에서 동작 ------------------

func TestEvaluateWithVerifier_cosign_keyed_정상_정책_통과(t *testing.T) {
	rt := newTestRuntime(t)
	priv := mustGenECDSAP256(t)
	sig := mustSignECDSA(t, priv, wasmFdWriteJSON)

	v := &CosignKeyedVerifier{PublicKey: &priv.PublicKey, HashFn: crypto.SHA256}
	res, err := rt.EvaluateWithVerifier(context.Background(), wasmFdWriteJSON, sig, nil, Limits{}, v)
	if err != nil {
		t.Fatalf("cosign keyed verify + evaluate: %v", err)
	}
	if res.Status != StatusPass {
		t.Errorf("Status: %q", res.Status)
	}
}

func TestEvaluateWithVerifier_cosign_keyed_변조된_정책_거부(t *testing.T) {
	rt := newTestRuntime(t)
	priv := mustGenECDSAP256(t)
	sig := mustSignECDSA(t, priv, wasmFdWriteJSON)

	tampered := append([]byte{}, wasmFdWriteJSON...)
	tampered[len(tampered)-1] ^= 0x01

	v := &CosignKeyedVerifier{PublicKey: &priv.PublicKey, HashFn: crypto.SHA256}
	_, err := rt.EvaluateWithVerifier(context.Background(), tampered, sig, nil, Limits{}, v)
	if !errors.Is(err, ErrPolicySignatureInvalid) {
		t.Errorf("변조 정책: got %v, want ErrPolicySignatureInvalid", err)
	}
}
