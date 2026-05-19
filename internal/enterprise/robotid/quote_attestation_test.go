//go:build rosshield_enterprise

// quote_attestation_test.go — VerifyQuote 검증 단위 테스트 (OS-agnostic, v3).
//
// 본 test는 TPM hardware 의존을 회피 — 모든 quote는 in-process로 hand-craft
// 합니다 (tpm2.AttestationData.Encode + crypto/ecdsa + crypto/rsa stdlib 서명).
// 결과는 실 TPM이 발급한 quote와 wire-compatible — VerifyQuote 알고리즘
// 자체의 결정론 + 분기 정확성을 충분히 cover합니다.
//
// 실 TPM(simulator 포함) 통합은 별 round (`tpm_integration` build tag) 예정.

package robotid

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"errors"
	"math/big"
	"testing"

	"github.com/google/go-tpm/legacy/tpm2"
	"github.com/google/go-tpm/tpmutil"
)

// fixedPCR은 test 전반에서 재사용하는 결정론적 PCR 값 (sha256 32바이트).
var fixedPCR = map[int][]byte{
	0: bytes.Repeat([]byte{0x01}, 32),
	2: bytes.Repeat([]byte{0x02}, 32),
	4: bytes.Repeat([]byte{0x03}, 32),
	7: bytes.Repeat([]byte{0x04}, 32),
}

var fixedPCRSelection = []int{0, 2, 4, 7}

// computePCRDigestSHA256은 test helper — VerifyQuote의 알고리즘과 동일하게
// PCR index 정렬 후 sha256(concat)을 계산합니다.
func computePCRDigestSHA256(pcrs map[int][]byte, sel []int) []byte {
	h := sha256.New()
	// sel 순서를 그대로 사용 — 호출자가 정렬 책임.
	for _, i := range sel {
		h.Write(pcrs[i])
	}
	return h.Sum(nil)
}

// buildQuoteInfoBytes는 TPMS_ATTEST wire bytes를 생성합니다.
// PCRSelection.Hash = SHA-256 가정.
func buildQuoteInfoBytes(t *testing.T, nonce []byte, sel []int, pcrDigest []byte) []byte {
	t.Helper()
	ad := tpm2.AttestationData{
		Magic:           0xff544347,
		Type:            tpm2.TagAttestQuote,
		QualifiedSigner: tpm2.Name{}, // 빈 Name — 본 검증은 signer 사용 안 함.
		ExtraData:       tpmutil.U16Bytes(nonce),
		ClockInfo: tpm2.ClockInfo{
			Clock: 0, ResetCount: 0, RestartCount: 0, Safe: 1,
		},
		FirmwareVersion: 0,
		AttestedQuoteInfo: &tpm2.QuoteInfo{
			PCRSelection: tpm2.PCRSelection{
				Hash: tpm2.AlgSHA256,
				PCRs: sel,
			},
			PCRDigest: tpmutil.U16Bytes(pcrDigest),
		},
	}
	bs, err := ad.Encode()
	if err != nil {
		t.Fatalf("AttestationData.Encode: %v", err)
	}
	return bs
}

// signECDSAQuote는 quoteInfo bytes에 ECDSA P-256 서명을 생성하여 TPMT_SIGNATURE
// wire bytes로 반환합니다. AK private key는 stdlib ECDSA 키 — 실 TPM의 AK와
// 알고리즘은 동일.
func signECDSAQuote(t *testing.T, priv *ecdsa.PrivateKey, quoteInfo []byte) []byte {
	t.Helper()
	sum := sha256.Sum256(quoteInfo)
	r, s, err := ecdsa.Sign(rand.Reader, priv, sum[:])
	if err != nil {
		t.Fatalf("ecdsa.Sign: %v", err)
	}
	sig := tpm2.Signature{
		Alg: tpm2.AlgECDSA,
		ECC: &tpm2.SignatureECC{
			HashAlg: tpm2.AlgSHA256,
			R:       r,
			S:       s,
		},
	}
	bs, err := sig.Encode()
	if err != nil {
		t.Fatalf("Signature.Encode: %v", err)
	}
	return bs
}

// signRSAQuote는 quoteInfo bytes에 RSASSA-PKCS1v15 서명을 생성합니다.
func signRSAQuote(t *testing.T, priv *rsa.PrivateKey, quoteInfo []byte) []byte {
	t.Helper()
	sum := sha256.Sum256(quoteInfo)
	raw, err := rsa.SignPKCS1v15(rand.Reader, priv, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatalf("rsa.SignPKCS1v15: %v", err)
	}
	sig := tpm2.Signature{
		Alg: tpm2.AlgRSASSA,
		RSA: &tpm2.SignatureRSA{
			HashAlg:   tpm2.AlgSHA256,
			Signature: tpmutil.U16Bytes(raw),
		},
	}
	bs, err := sig.Encode()
	if err != nil {
		t.Fatalf("Signature.Encode: %v", err)
	}
	return bs
}

// newECDSAQuoteAttestation은 결정론적 ECDSA quote attestation을 합성합니다.
// 반환된 private key는 test에서 signature 변조 등 추가 manipulation에 사용 가능.
func newECDSAQuoteAttestation(t *testing.T, nonce []byte) (*QuoteAttestation, *ecdsa.PrivateKey) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	akDER, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey: %v", err)
	}
	digest := computePCRDigestSHA256(fixedPCR, fixedPCRSelection)
	qi := buildQuoteInfoBytes(t, nonce, fixedPCRSelection, digest)
	sig := signECDSAQuote(t, priv, qi)
	pcrCopy := make(map[int][]byte, len(fixedPCR))
	for k, v := range fixedPCR {
		pcrCopy[k] = append([]byte(nil), v...)
	}
	return &QuoteAttestation{
		AKPublic:     akDER,
		QuoteInfo:    qi,
		Signature:    sig,
		PCRSelection: append([]int(nil), fixedPCRSelection...),
		PCRValues:    pcrCopy,
	}, priv
}

// newRSAQuoteAttestation은 결정론적 RSA quote attestation을 합성합니다.
func newRSAQuoteAttestation(t *testing.T, nonce []byte) (*QuoteAttestation, *rsa.PrivateKey) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	akDER, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey: %v", err)
	}
	digest := computePCRDigestSHA256(fixedPCR, fixedPCRSelection)
	qi := buildQuoteInfoBytes(t, nonce, fixedPCRSelection, digest)
	sig := signRSAQuote(t, priv, qi)
	pcrCopy := make(map[int][]byte, len(fixedPCR))
	for k, v := range fixedPCR {
		pcrCopy[k] = append([]byte(nil), v...)
	}
	return &QuoteAttestation{
		AKPublic:     akDER,
		QuoteInfo:    qi,
		Signature:    sig,
		PCRSelection: append([]int(nil), fixedPCRSelection...),
		PCRValues:    pcrCopy,
	}, priv
}

// =============================================================================
// VerifyQuote — 정상 path
// =============================================================================

// TestVerifyQuote_ECDSA_round_trip_nil
//
// ECDSA AK + 정상 quote → VerifyQuote가 nil 반환.
func TestVerifyQuote_ECDSA_round_trip_nil(t *testing.T) {
	nonce := []byte("nonce-ecdsa-001")
	att, _ := newECDSAQuoteAttestation(t, nonce)
	if err := VerifyQuote(att, nonce); err != nil {
		t.Errorf("ECDSA round-trip: VerifyQuote = %v, want nil", err)
	}
}

// TestVerifyQuote_RSA_round_trip_nil
//
// RSA AK + 정상 quote → VerifyQuote가 nil 반환.
func TestVerifyQuote_RSA_round_trip_nil(t *testing.T) {
	nonce := []byte("nonce-rsa-001")
	att, _ := newRSAQuoteAttestation(t, nonce)
	if err := VerifyQuote(att, nonce); err != nil {
		t.Errorf("RSA round-trip: VerifyQuote = %v, want nil", err)
	}
}

// TestVerifyQuote_결정론_같은_입력_같은_결과
//
// 같은 attestation + 같은 nonce → 두 번 호출 결과 동일 (결정론).
func TestVerifyQuote_결정론_같은_입력_같은_결과(t *testing.T) {
	nonce := []byte("nonce-determinism")
	att, _ := newECDSAQuoteAttestation(t, nonce)
	err1 := VerifyQuote(att, nonce)
	err2 := VerifyQuote(att, nonce)
	if err1 != err2 {
		t.Errorf("결정론 위반: err1=%v err2=%v", err1, err2)
	}
}

// =============================================================================
// VerifyQuote — nonce mismatch
// =============================================================================

// TestVerifyQuote_nonce_다름_ErrQuoteNonceMismatch
//
// 검증자가 다른 nonce를 기대하면 replay 차단 — ErrQuoteNonceMismatch.
func TestVerifyQuote_nonce_다름_ErrQuoteNonceMismatch(t *testing.T) {
	att, _ := newECDSAQuoteAttestation(t, []byte("nonce-signed"))
	err := VerifyQuote(att, []byte("nonce-expected-different"))
	if !errors.Is(err, ErrQuoteNonceMismatch) {
		t.Errorf("want ErrQuoteNonceMismatch, got %v", err)
	}
}

// TestVerifyQuote_빈_expectedNonce_signed에_빈_nonce_round_trip
//
// 빈 nonce도 일관성만 맞으면 통과 — 호출자가 nonce 정책으로 강제.
func TestVerifyQuote_빈_expectedNonce_signed에_빈_nonce_round_trip(t *testing.T) {
	att, _ := newECDSAQuoteAttestation(t, []byte{})
	if err := VerifyQuote(att, []byte{}); err != nil {
		t.Errorf("빈 nonce round-trip: %v", err)
	}
}

// =============================================================================
// VerifyQuote — PCR mismatch
// =============================================================================

// TestVerifyQuote_PCR_값_변경_ErrQuotePCRMismatch
//
// quote 발급 후 PCRValues 1바이트 변조 → ErrQuotePCRMismatch.
func TestVerifyQuote_PCR_값_변경_ErrQuotePCRMismatch(t *testing.T) {
	nonce := []byte("nonce-pcr")
	att, _ := newECDSAQuoteAttestation(t, nonce)
	// PCR 0 값 1바이트 변조.
	att.PCRValues[0] = append([]byte(nil), att.PCRValues[0]...)
	att.PCRValues[0][0] ^= 0xFF
	err := VerifyQuote(att, nonce)
	if !errors.Is(err, ErrQuotePCRMismatch) {
		t.Errorf("want ErrQuotePCRMismatch, got %v", err)
	}
}

// TestVerifyQuote_PCR_빈_map_ErrQuotePCRMismatch
//
// PCRValues 빈 map → digest 재계산 불가, ErrQuotePCRMismatch.
func TestVerifyQuote_PCR_빈_map_ErrQuotePCRMismatch(t *testing.T) {
	nonce := []byte("nonce-pcr-empty")
	att, _ := newECDSAQuoteAttestation(t, nonce)
	att.PCRValues = map[int][]byte{}
	err := VerifyQuote(att, nonce)
	if !errors.Is(err, ErrQuotePCRMismatch) {
		t.Errorf("want ErrQuotePCRMismatch, got %v", err)
	}
}

// TestVerifyQuote_PCR_index_불일치_ErrQuotePCRMismatch
//
// PCRValues에 quote selection 외 index 주입 → ErrQuotePCRMismatch.
func TestVerifyQuote_PCR_index_불일치_ErrQuotePCRMismatch(t *testing.T) {
	nonce := []byte("nonce-pcr-index")
	att, _ := newECDSAQuoteAttestation(t, nonce)
	// PCR 7을 PCR 8로 교체 — 길이는 동일, index는 quote selection 밖.
	delete(att.PCRValues, 7)
	att.PCRValues[8] = bytes.Repeat([]byte{0x99}, 32)
	err := VerifyQuote(att, nonce)
	if !errors.Is(err, ErrQuotePCRMismatch) {
		t.Errorf("want ErrQuotePCRMismatch, got %v", err)
	}
}

// =============================================================================
// VerifyQuote — Signature 검증 실패
// =============================================================================

// TestVerifyQuote_Signature_변조_ErrQuoteSignatureInvalid
//
// Signature 마지막 byte 1bit 변조 → 검증 실패.
func TestVerifyQuote_Signature_변조_ErrQuoteSignatureInvalid(t *testing.T) {
	nonce := []byte("nonce-sig-tamper")
	att, _ := newECDSAQuoteAttestation(t, nonce)
	tampered := append([]byte(nil), att.Signature...)
	tampered[len(tampered)-1] ^= 0x01
	att.Signature = tampered
	err := VerifyQuote(att, nonce)
	// ECDSA 변조는 보통 verify 실패로 → ErrQuoteSignatureInvalid.
	// 단 1bit가 R/S 인코딩을 깨면 decode 실패로 ErrQuoteInfoParse 가능.
	// 두 sentinel 중 하나여야 (둘 다 reject 결과).
	if !errors.Is(err, ErrQuoteSignatureInvalid) && !errors.Is(err, ErrQuoteInfoParse) {
		t.Errorf("want ErrQuoteSignatureInvalid or ErrQuoteInfoParse, got %v", err)
	}
}

// TestVerifyQuote_다른_AK_ErrQuoteSignatureInvalid
//
// quote는 정상이나 AKPublic을 다른 키로 교체 → 서명 검증 실패.
func TestVerifyQuote_다른_AK_ErrQuoteSignatureInvalid(t *testing.T) {
	nonce := []byte("nonce-wrong-ak")
	att, _ := newECDSAQuoteAttestation(t, nonce)
	otherPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	otherDER, err := x509.MarshalPKIXPublicKey(&otherPriv.PublicKey)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	att.AKPublic = otherDER
	err = VerifyQuote(att, nonce)
	if !errors.Is(err, ErrQuoteSignatureInvalid) {
		t.Errorf("want ErrQuoteSignatureInvalid, got %v", err)
	}
}

// TestVerifyQuote_QuoteInfo_변조_ErrQuoteSignatureInvalid
//
// QuoteInfo bytes 변조 → signature가 더 이상 일치 안 함 → ErrQuoteSignatureInvalid.
// (QuoteInfo가 valid AttestationData인 채로 1바이트만 swap)
func TestVerifyQuote_QuoteInfo_변조_ErrQuoteSignatureInvalid(t *testing.T) {
	nonce := []byte("nonce-info-tamper")
	att, _ := newECDSAQuoteAttestation(t, nonce)
	tampered := append([]byte(nil), att.QuoteInfo...)
	// 끝 부분의 PCR digest byte 변조 — AttestationData 자체는 여전히 parse 가능
	// 하나 hash가 달라져 signature 검증 실패.
	tampered[len(tampered)-1] ^= 0x01
	att.QuoteInfo = tampered
	err := VerifyQuote(att, nonce)
	if !errors.Is(err, ErrQuoteSignatureInvalid) {
		t.Errorf("want ErrQuoteSignatureInvalid, got %v", err)
	}
}

// =============================================================================
// VerifyQuote — 파싱 실패
// =============================================================================

// TestVerifyQuote_nil_attestation_ErrQuoteInfoParse
func TestVerifyQuote_nil_attestation_ErrQuoteInfoParse(t *testing.T) {
	err := VerifyQuote(nil, []byte("n"))
	if !errors.Is(err, ErrQuoteInfoParse) {
		t.Errorf("want ErrQuoteInfoParse, got %v", err)
	}
}

// TestVerifyQuote_AKPublic_손상_ErrQuoteInfoParse
//
// AKPublic이 잘못된 DER → ParsePKIXPublicKey 실패 → ErrQuoteInfoParse.
func TestVerifyQuote_AKPublic_손상_ErrQuoteInfoParse(t *testing.T) {
	nonce := []byte("nonce-bad-ak")
	att, _ := newECDSAQuoteAttestation(t, nonce)
	att.AKPublic = []byte("not a DER public key")
	err := VerifyQuote(att, nonce)
	if !errors.Is(err, ErrQuoteInfoParse) {
		t.Errorf("want ErrQuoteInfoParse, got %v", err)
	}
}

// TestVerifyQuote_Signature_손상_ErrQuoteInfoParse
//
// Signature가 잘못된 wire format → DecodeSignature 실패 → ErrQuoteInfoParse.
func TestVerifyQuote_Signature_손상_ErrQuoteInfoParse(t *testing.T) {
	nonce := []byte("nonce-bad-sig")
	att, _ := newECDSAQuoteAttestation(t, nonce)
	att.Signature = []byte{0x00, 0x01} // 너무 짧음.
	err := VerifyQuote(att, nonce)
	if !errors.Is(err, ErrQuoteInfoParse) {
		t.Errorf("want ErrQuoteInfoParse, got %v", err)
	}
}

// TestVerifyQuote_QuoteInfo_손상_ErrQuoteInfoParse
//
// QuoteInfo가 valid AttestationData가 아닌 raw bytes → 우선 signature 검증
// 자체가 실패할 가능성이 큼 (signature는 원본 QuoteInfo에 대한 hash).
// 본 test는 손상된 QuoteInfo + 동시에 sig을 그 손상 bytes에 대해 새로 만들지
// 않으므로 → ErrQuoteSignatureInvalid가 먼저 발생할 수도 있음.
// signed 후 손상 시나리오는 위의 QuoteInfo_변조 test가 cover.
//
// 본 test는 sig + info를 함께 만들되 info가 invalid attestation magic을
// 갖도록 → signature는 valid하나 DecodeAttestationData가 거부.
func TestVerifyQuote_QuoteInfo_잘못된_magic_ErrQuoteInfoParse(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	akDER, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	// 잘못된 magic이 prefix인 bytes — sig은 이 bytes에 대해 valid하게 생성.
	invalidInfo := []byte("ROSSHIELD-NOT-A-VALID-TPMS-ATTEST-EVER")
	sig := signECDSAQuote(t, priv, invalidInfo)
	att := &QuoteAttestation{
		AKPublic:     akDER,
		QuoteInfo:    invalidInfo,
		Signature:    sig,
		PCRSelection: fixedPCRSelection,
		PCRValues:    fixedPCR,
	}
	err = VerifyQuote(att, []byte("any-nonce"))
	if !errors.Is(err, ErrQuoteInfoParse) {
		t.Errorf("want ErrQuoteInfoParse, got %v", err)
	}
}

// =============================================================================
// SortedPCRSelection — helper 결정론
// =============================================================================

// TestSortedPCRSelection_결정론
//
// 입력 순서 무관하게 정렬 결과 동일 + 입력 mutation 안 함.
func TestSortedPCRSelection_결정론(t *testing.T) {
	orig := []int{7, 0, 4, 2}
	origCopy := append([]int(nil), orig...)
	out1 := SortedPCRSelection(orig)
	out2 := SortedPCRSelection([]int{4, 2, 7, 0})

	if !bytes.Equal(intsToBytes(out1), intsToBytes(out2)) {
		t.Errorf("같은 집합 다른 순서: out1=%v out2=%v", out1, out2)
	}
	want := []int{0, 2, 4, 7}
	if !bytes.Equal(intsToBytes(out1), intsToBytes(want)) {
		t.Errorf("정렬 결과 mismatch: got %v want %v", out1, want)
	}
	// 입력 mutation 검증.
	if !bytes.Equal(intsToBytes(orig), intsToBytes(origCopy)) {
		t.Errorf("입력 mutation 발생: orig=%v copy=%v", orig, origCopy)
	}
}

// intsToBytes는 test 비교용 — []int를 결정론적 bytes로 직렬화.
func intsToBytes(xs []int) []byte {
	buf := make([]byte, 0, len(xs)*8)
	for _, x := range xs {
		bi := big.NewInt(int64(x))
		buf = append(buf, bi.Bytes()...)
		buf = append(buf, '|')
	}
	return buf
}
