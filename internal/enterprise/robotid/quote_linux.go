//go:build rosshield_enterprise && linux

// quote_linux.go — Linux 전용 TPM Quote 생성 (v3 attestation flow).
//
// 본 파일은 design doc §6.5 5번 v3의 production 측면 — 로봇 측 agent가 TPM
// AK를 사용하여 PCR + nonce에 대한 서명된 quote를 생성합니다.
//
// 흐름:
//  1. TPM device open (/dev/tpmrm0 → /dev/tpm0 fallback).
//  2. AK 생성/load — 기본 ECDSA P-256 (RSA 2048 fallback 옵션, 별 함수).
//  3. AK.Quote(PCRSelection, nonce) → pb.Quote {Quote, RawSig, Pcrs}.
//  4. AK.PublicKey() → x509.MarshalPKIXPublicKey 후 DER.
//  5. QuoteAttestation 구성하여 반환.
//
// 검증 측 (VerifyQuote)은 quote_attestation.go에 OS-agnostic으로 정의.
//
// EK collector (tpm_linux.go)와 분리 — AK는 별 핸들 + 서명 전용.

package robotid

import (
	"crypto/x509"
	"fmt"
	"io"

	"github.com/google/go-tpm-tools/client"
	pb "github.com/google/go-tpm-tools/proto/tpm"
	"github.com/google/go-tpm/legacy/tpm2"
)

// akLoader는 rwc로부터 client.Key (AK)를 로드하는 hook입니다 — test seam.
// nil이면 production 경로(client.AttestationKeyECC).
type akLoader func(rw io.ReadWriter) (*client.Key, error)

// quoter는 AK · PCR selection · nonce로 pb.Quote를 생성하는 hook입니다 — test seam.
// nil이면 production 경로(client.Key.Quote 메서드 호출).
type quoter func(ak *client.Key, sel tpm2.PCRSelection, nonce []byte) (*pb.Quote, error)

// akPubExtractor는 AK로부터 public key를 DER로 추출하는 hook입니다 — test seam.
// nil이면 production 경로(x509.MarshalPKIXPublicKey ∘ ak.PublicKey()).
type akPubExtractor func(ak *client.Key) ([]byte, error)

// 본 함수 변수는 test에서만 override됩니다 (quote_linux_test.go의 withQuoteHooks).
// production 경로는 nil (default 분기).
var (
	defaultAKLoader       akLoader
	defaultQuoter         quoter
	defaultAKPubExtractor akPubExtractor
)

// QuoteLinux는 TPM AK를 사용하여 nonce + PCR selection에 대한 서명된 quote를
// 생성합니다 (Linux 전용, v3).
//
// 호출 패턴:
//
//	att, err := robotid.QuoteLinux(nonce, robotid.DefaultPCRSelection)
//	if err != nil { /* ErrTPMNotAvailable fallback or reject */ }
//	// att를 audit 결과에 attach → 검증자는 VerifyQuote(att, nonce)로 검증.
//
// 흐름:
//  1. 빈 pcrs → ErrTPMNotAvailable (의미 없는 quote 거부).
//  2. nil/empty nonce → ErrTPMNotAvailable (replay 방어 안 됨).
//  3. TPM open → AK load → Quote → AK public extract → QuoteAttestation 구성.
//
// 어느 단계라도 실패 시 ErrTPMNotAvailable로 wrap. 호출자는 errors.Is로
// 분기하여 v2 fingerprint만으로 fallback 가능 (v3 attestation 옵션 원칙 일관).
//
// AK 알고리즘은 ECC (P-256) 기본 — RSA 2048 옵션은 별 함수(QuoteLinuxRSA, 후속).
// 본 round는 ECC만 — 표준 + 짧은 서명 (~70바이트 vs RSA 256바이트).
func QuoteLinux(nonce []byte, pcrs []int) (*QuoteAttestation, error) {
	if len(pcrs) == 0 {
		return nil, fmt.Errorf("%w: empty PCR selection", ErrTPMNotAvailable)
	}
	if len(nonce) == 0 {
		return nil, fmt.Errorf("%w: empty nonce (replay protection requires fresh nonce)",
			ErrTPMNotAvailable)
	}

	opener := defaultTPMOpener
	if opener == nil {
		opener = openTPMDevice
	}
	rwc, err := opener()
	if err != nil {
		return nil, fmt.Errorf("%w: open: %v", ErrTPMNotAvailable, err)
	}
	defer func() {
		_ = rwc.Close()
	}()

	loader := defaultAKLoader
	if loader == nil {
		loader = client.AttestationKeyECC
	}
	ak, err := loader(rwc)
	if err != nil {
		return nil, fmt.Errorf("%w: load AK: %v", ErrTPMNotAvailable, err)
	}
	defer ak.Close()

	sel := tpm2.PCRSelection{
		Hash: tpm2.AlgSHA256,
		PCRs: SortedPCRSelection(pcrs),
	}

	q := defaultQuoter
	if q == nil {
		q = func(ak *client.Key, s tpm2.PCRSelection, n []byte) (*pb.Quote, error) {
			return ak.Quote(s, n)
		}
	}
	quote, err := q(ak, sel, nonce)
	if err != nil {
		return nil, fmt.Errorf("%w: quote: %v", ErrTPMNotAvailable, err)
	}
	if quote == nil {
		return nil, fmt.Errorf("%w: quote returned nil", ErrTPMNotAvailable)
	}
	if len(quote.Quote) == 0 || len(quote.RawSig) == 0 {
		return nil, fmt.Errorf("%w: quote missing data (info=%d, sig=%d)",
			ErrTPMNotAvailable, len(quote.Quote), len(quote.RawSig))
	}

	extractor := defaultAKPubExtractor
	if extractor == nil {
		extractor = extractAKPublicDER
	}
	akDER, err := extractor(ak)
	if err != nil {
		return nil, fmt.Errorf("%w: AK public: %v", ErrTPMNotAvailable, err)
	}

	// pb.Quote.Pcrs는 quote 시점 실 PCR 값 (HashAlgo + map[uint32][]byte).
	// 본 구조의 PCRValues는 map[int][]byte — uint32 → int 변환.
	pcrValues := make(map[int][]byte, len(quote.Pcrs.GetPcrs()))
	for k, v := range quote.Pcrs.GetPcrs() {
		// PCR 값 사본 — 입력 mutation 방지.
		cp := make([]byte, len(v))
		copy(cp, v)
		pcrValues[int(k)] = cp
	}

	return &QuoteAttestation{
		AKPublic:     akDER,
		QuoteInfo:    append([]byte(nil), quote.Quote...),
		Signature:    append([]byte(nil), quote.RawSig...),
		PCRSelection: SortedPCRSelection(pcrs),
		PCRValues:    pcrValues,
	}, nil
}

// extractAKPublicDER는 AK의 public key를 x509.MarshalPKIXPublicKey DER로 추출합니다.
// production 경로 — test seam(defaultAKPubExtractor)이 nil일 때 사용.
func extractAKPublicDER(ak *client.Key) ([]byte, error) {
	pub := ak.PublicKey()
	if pub == nil {
		return nil, fmt.Errorf("AK PublicKey is nil")
	}
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, fmt.Errorf("marshal: %v", err)
	}
	if len(der) == 0 {
		return nil, fmt.Errorf("DER is empty")
	}
	return der, nil
}
