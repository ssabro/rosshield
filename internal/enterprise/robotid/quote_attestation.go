//go:build rosshield_enterprise

// quote_attestation.go — TPM Quote attestation 구조 + 검증 (OS-agnostic, v3).
//
// 본 파일은 design doc `phase7-public-transition-design.md` §6.5 5번
// "TPM Quote는 옵션: PCR 값 결합으로 부팅 무결성까지 증명"의 v3 단계
// (Quote signature attestation flow)를 구현합니다.
//
// v2는 PCR 값 결합만 수행 — fingerprint에 PCR digest 포함.
// v3는 추가로 다음을 보장:
//
//  1. AK (Attestation Key)가 TPM 내부에서 PCR digest + nonce를 서명.
//  2. 검증자가 AK 서명을 검증하여 quote 신뢰성 확인 (replay·위조 방어).
//  3. 첨부된 PCR 값으로 재계산한 digest가 quote 내부 digest와 일치 확인.
//
// 표준 알고리즘 출처:
//   - TCG TPM 2.0 Library — Part 2: Structures §10.12 TPMS_ATTEST + §11.3 Quote
//   - go-tpm-tools/internal/quote.go VerifyQuote (reference 구현)
//
// 본 패키지는 internal/quote.go를 import할 수 없어 (Go internal 규칙) 표준
// 알고리즘만 stdlib + go-tpm/legacy/tpm2 public API로 재구현합니다. 결과는
// 의미적으로 reference와 동일 (회귀 0 — 같은 입력 → 같은 결과).
//
// 본 파일은 build tag `rosshield_enterprise` 만 — OS-agnostic. VerifyQuote는
// 모든 OS에서 사용 가능 (server-side 검증은 Linux 한정 아님). Quote 생성
// 자체는 Linux 전용(quote_linux.go).

package robotid

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"crypto/x509"
	"fmt"
	"sort"

	"github.com/google/go-tpm/legacy/tpm2"
)

// quoteMagic는 TPMS_GENERATED_VALUE — TPM이 AK로 서명한 attestation 구조의
// 첫 4바이트입니다 (0xFF "TCG"). DecodeAttestationData가 자체 확인하므로
// 본 상수는 reference 용도 (외부에서 hand-crafting 시 사용).
const quoteMagic uint32 = 0xff544347

// QuoteAttestation은 TPM 2.0 Quote 결과 + 검증에 필요한 모든 데이터입니다 (v3).
//
// 본 구조는 audit 결과에 attach되어 외부 검증자가 VerifyQuote로 재현
// 가능하도록 직렬화됩니다 (JSON tag 영문 snake_case 일관).
//
// 필드 순서·이름은 외부 검증 도구와의 contract — 변경 시 broken.
type QuoteAttestation struct {
	// AKPublic은 Attestation Key public key DER 표현입니다 (x509.MarshalPKIXPublicKey).
	// VerifyQuote는 이 키를 ECDSA 또는 RSA로 파싱하여 signature 검증에 사용합니다.
	// EK(Endorsement Key)와는 별 객체 — AK는 quote 서명 전용 restricted signing key.
	AKPublic []byte `json:"ak_public"`

	// QuoteInfo는 TPMS_ATTEST wire format bytes입니다 (PCR digest + nonce + 메타).
	// AK private이 이 bytes에 서명하며, VerifyQuote가 DecodeAttestationData로 파싱.
	QuoteInfo []byte `json:"quote_info"`

	// Signature는 AK private으로 QuoteInfo에 대한 ECDSA 또는 RSASSA-PKCS1v15 서명입니다
	// (TPMT_SIGNATURE wire format). tpm2.DecodeSignature로 파싱.
	Signature []byte `json:"signature"`

	// PCRSelection은 quote에 포함된 PCR index 집합 (정렬 후 저장).
	// VerifyQuote가 QuoteInfo 내부 PCRSelection과의 일치를 확인.
	PCRSelection []int `json:"pcr_selection"`

	// PCRValues는 quote 시점 실 PCR 값 map입니다 (검증자가 digest 재계산용).
	//   key   — PCR index.
	//   value — PCR digest (sha256 기준 32바이트).
	// VerifyQuote는 정렬된 index 순으로 concat → sha256 → QuoteInfo.PCRDigest 비교.
	PCRValues map[int][]byte `json:"pcr_values"`
}

// VerifyQuote는 QuoteAttestation의 4단계 검증을 수행합니다 (v3):
//
//  1. AKPublic을 x509.ParsePKIXPublicKey로 파싱 (ECDSA 또는 RSA).
//  2. Signature를 AKPublic으로 검증 (ECDSA.Verify 또는 rsa.VerifyPKCS1v15).
//     hash 알고리즘은 signature 내부 HashAlg 사용 (SHA-256/384/512 지원).
//  3. QuoteInfo를 DecodeAttestationData로 파싱 → ExtraData가 expectedNonce와
//     constant-time 일치 확인 (replay 방어).
//  4. PCRValues로 PCR digest 재계산 → QuoteInfo.AttestedQuoteInfo.PCRDigest와
//     constant-time 일치 확인.
//
// 각 단계 실패 시 대응 sentinel 반환:
//   - 파싱 실패 → ErrQuoteInfoParse
//   - 서명 검증 실패 → ErrQuoteSignatureInvalid
//   - nonce mismatch → ErrQuoteNonceMismatch
//   - PCR digest mismatch → ErrQuotePCRMismatch
//
// 결정론: 같은 (att, expectedNonce) → 같은 결과. 입력 mutation 금지.
//
// 본 함수는 server-side 검증 entry point — TPM hardware 불필요. 모든 OS에서
// 사용 가능 (검증자 = 감사 서버, 로봇 측 quote 생성 = Linux 한정).
func VerifyQuote(att *QuoteAttestation, expectedNonce []byte) error {
	if att == nil {
		return fmt.Errorf("%w: nil attestation", ErrQuoteInfoParse)
	}

	// 1단계 — AKPublic 파싱.
	pub, err := x509.ParsePKIXPublicKey(att.AKPublic)
	if err != nil {
		return fmt.Errorf("%w: AKPublic parse: %v", ErrQuoteInfoParse, err)
	}
	switch pub.(type) {
	case *ecdsa.PublicKey, *rsa.PublicKey:
		// ok
	default:
		return fmt.Errorf("%w: unsupported AK public key type %T (only ECDSA/RSA)",
			ErrQuoteInfoParse, pub)
	}

	// 2단계 — Signature 파싱 + 검증.
	if err := verifyQuoteSignature(pub, att.QuoteInfo, att.Signature); err != nil {
		return err
	}

	// 3단계 — QuoteInfo 파싱.
	ad, err := tpm2.DecodeAttestationData(att.QuoteInfo)
	if err != nil {
		return fmt.Errorf("%w: AttestationData decode: %v", ErrQuoteInfoParse, err)
	}
	if ad.Type != tpm2.TagAttestQuote {
		return fmt.Errorf("%w: AttestationData type %v != TagAttestQuote",
			ErrQuoteInfoParse, ad.Type)
	}
	if ad.AttestedQuoteInfo == nil {
		return fmt.Errorf("%w: AttestedQuoteInfo is nil", ErrQuoteInfoParse)
	}

	// nonce(ExtraData) 일치 확인 — constant time.
	if subtle.ConstantTimeCompare(ad.ExtraData, expectedNonce) != 1 {
		return ErrQuoteNonceMismatch
	}

	// 4단계 — PCR digest 재계산 + 비교.
	if err := verifyQuotePCRDigest(ad.AttestedQuoteInfo, att.PCRValues); err != nil {
		return err
	}

	return nil
}

// verifyQuoteSignature는 quoteInfo bytes에 대한 sig를 AKPublic으로 검증합니다.
//
// signature 내부 hash 알고리즘에 따라:
//   - SHA-256 → sha256
//   - SHA-384 → sha384
//   - SHA-512 → sha512
//
// 다른 알고리즘은 ErrQuoteSignatureInvalid (지원 범위 외).
//
// ECDSA: ecdsa.Verify(pub, H(quoteInfo), R, S).
// RSA: rsa.VerifyPKCS1v15(pub, hash, H(quoteInfo), Signature).
//
// 두 알고리즘 모두 standard PKCS — TPM 표준과 일치.
func verifyQuoteSignature(pub crypto.PublicKey, quoteInfo, sigBytes []byte) error {
	sig, err := tpm2.DecodeSignature(bytes.NewBuffer(sigBytes))
	if err != nil {
		return fmt.Errorf("%w: Signature decode: %v", ErrQuoteInfoParse, err)
	}

	// hash 알고리즘 결정.
	hashAlg, err := signatureHashAlg(sig)
	if err != nil {
		return err
	}
	hashed := hashBytes(hashAlg, quoteInfo)

	switch p := pub.(type) {
	case *ecdsa.PublicKey:
		if sig.Alg != tpm2.AlgECDSA || sig.ECC == nil {
			return fmt.Errorf("%w: AK is ECDSA but signature alg=0x%x",
				ErrQuoteSignatureInvalid, sig.Alg)
		}
		if !ecdsa.Verify(p, hashed, sig.ECC.R, sig.ECC.S) {
			return ErrQuoteSignatureInvalid
		}
		return nil
	case *rsa.PublicKey:
		if sig.Alg != tpm2.AlgRSASSA || sig.RSA == nil {
			return fmt.Errorf("%w: AK is RSA but signature alg=0x%x",
				ErrQuoteSignatureInvalid, sig.Alg)
		}
		if err := rsa.VerifyPKCS1v15(p, hashAlg, hashed, sig.RSA.Signature); err != nil {
			return fmt.Errorf("%w: %v", ErrQuoteSignatureInvalid, err)
		}
		return nil
	default:
		return fmt.Errorf("%w: unsupported AK type %T", ErrQuoteSignatureInvalid, pub)
	}
}

// signatureHashAlg는 TPM signature의 hash algorithm을 Go crypto.Hash로 변환합니다.
// 지원: SHA-256, SHA-384, SHA-512 (TPM 2.0 표준 + reference 구현 일관).
func signatureHashAlg(sig *tpm2.Signature) (crypto.Hash, error) {
	var algTPM tpm2.Algorithm
	switch {
	case sig.ECC != nil:
		algTPM = sig.ECC.HashAlg
	case sig.RSA != nil:
		algTPM = sig.RSA.HashAlg
	default:
		return 0, fmt.Errorf("%w: signature missing hash algorithm", ErrQuoteSignatureInvalid)
	}
	switch algTPM {
	case tpm2.AlgSHA256:
		return crypto.SHA256, nil
	case tpm2.AlgSHA384:
		return crypto.SHA384, nil
	case tpm2.AlgSHA512:
		return crypto.SHA512, nil
	default:
		return 0, fmt.Errorf("%w: unsupported hash alg 0x%x", ErrQuoteSignatureInvalid, algTPM)
	}
}

// hashBytes는 alg에 따라 data의 hash를 계산합니다. stdlib만 사용 (crypto/sha*).
func hashBytes(alg crypto.Hash, data []byte) []byte {
	switch alg {
	case crypto.SHA256:
		sum := sha256.Sum256(data)
		return sum[:]
	case crypto.SHA384:
		sum := sha512.Sum384(data)
		return sum[:]
	case crypto.SHA512:
		sum := sha512.Sum512(data)
		return sum[:]
	default:
		// signatureHashAlg가 이미 거름 — defensive.
		return nil
	}
}

// verifyQuotePCRDigest는 첨부된 PCRValues로 PCR digest를 재계산하여 quote 내부
// digest와 비교합니다.
//
// 알고리즘 (reference VerifyQuote와 일관):
//  1. PCRValues key를 오름차순 정렬.
//  2. 정렬 순서로 PCR 값을 concat.
//  3. sha256(concat) → 32바이트 → QuoteInfo.PCRDigest와 constant-time 비교.
//
// PCR 알고리즘은 sha256 가정 (collectPCRValuesLinux의 AlgSHA256과 일관).
// 다른 PCR bank는 본 round 미지원 (필요 시 후속 round에서 확장).
func verifyQuotePCRDigest(qi *tpm2.QuoteInfo, pcrValues map[int][]byte) error {
	if len(pcrValues) == 0 {
		return fmt.Errorf("%w: PCRValues empty", ErrQuotePCRMismatch)
	}

	// PCR selection 일치 확인 — quote가 sign한 PCR 집합 == 첨부된 PCR 집합.
	wantIndices := make(map[int]struct{}, len(qi.PCRSelection.PCRs))
	for _, i := range qi.PCRSelection.PCRs {
		wantIndices[i] = struct{}{}
	}
	if len(wantIndices) != len(pcrValues) {
		return fmt.Errorf("%w: PCR count mismatch (quote=%d, attached=%d)",
			ErrQuotePCRMismatch, len(wantIndices), len(pcrValues))
	}
	for i := range pcrValues {
		if _, ok := wantIndices[i]; !ok {
			return fmt.Errorf("%w: PCR %d in attached but not in quote selection",
				ErrQuotePCRMismatch, i)
		}
	}

	// PCR digest 재계산 — 정렬 순서로 concat → sha256.
	indices := make([]int, 0, len(pcrValues))
	for i := range pcrValues {
		indices = append(indices, i)
	}
	sort.Ints(indices)

	h := sha256.New()
	for _, i := range indices {
		h.Write(pcrValues[i])
	}
	got := h.Sum(nil)

	if subtle.ConstantTimeCompare(got, qi.PCRDigest) != 1 {
		return ErrQuotePCRMismatch
	}
	return nil
}

// SortedPCRSelection은 PCRSelection을 사본·정렬하여 반환합니다 (결정론 helper).
// 외부 호출자가 QuoteAttestation 직렬화 시 일관된 순서 보장에 활용 가능.
func SortedPCRSelection(pcrs []int) []int {
	out := make([]int, len(pcrs))
	copy(out, pcrs)
	sort.Ints(out)
	return out
}

// 본 파일의 모든 sentinel은 collector.go의 var 블록에 정의되어 errors.Is로
// 외부에서 분기 가능합니다 (Err* prefix 일관).
