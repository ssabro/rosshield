//go:build rosshield_enterprise && linux && tpm_integration

// quote_simulator_test.go — go-tpm-tools/simulator 통합 테스트 (D-3 v3).
//
// 빌드 태그 `tpm_integration`은 일반 `go test ./...`에서 본 파일이 빌드되지 않게
// 합니다 (simulator는 cgo + ms-tpm-20-ref C 라이브러리 + libssl-dev 의존). CI의
// 별 job `tpm-integration`에서만 실행:
//
//	go test -tags="tpm_integration rosshield_enterprise" -count=1 \
//	  ./internal/enterprise/robotid/...
//
// simulator.Get()은 in-process Microsoft TPM2 reference. 기존 hook
// (defaultTPMOpener + defaultAKLoader)은 그대로 활용 — simulator를 io.ReadWriter
// 로 주입하여 production 경로 (client.AttestationKeyECC + ak.Quote)가 simulator
// 위에서 실제 round-trip을 수행합니다.
//
// 본 round의 검증 항목:
//
//  1. 정상 round-trip — QuoteLinux로 발급한 attestation을 VerifyQuote가 nil 반환.
//  2. nonce mismatch — 다른 nonce로 verify → ErrQuoteNonceMismatch.
//  3. PCR tamper — Quote 발급 후 PCR 16 (debug PCR) extend → 재발급 quote의
//     PCRValues가 첫 quote의 PCRDigest와 mismatch (PCR digest mismatch 분기).
//  4. signature tamper — Signature 마지막 byte 1bit 변조 → ErrQuoteSignatureInvalid
//     또는 ErrQuoteInfoParse (둘 다 reject 결과).
//  5. 결정론 — 같은 attestation + 같은 nonce → 두 번 verify 결과 동일.
//
// PCR 선택: debug PCR(16)을 포함하여 tamper 시나리오를 직접 PCR_Extend로 트리거.
// production DefaultPCRSelection (0,2,4,7)은 simulator default에서 모두 0x00…00
// 이라 fingerprint v2 path는 별 round trip만 검증 가능.

package robotid

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"io"
	"testing"

	"github.com/google/go-tpm-tools/simulator"
	"github.com/google/go-tpm/legacy/tpm2"
	"github.com/google/go-tpm/tpmutil"
)

// debugPCR는 simulator에서 자유롭게 PCR_Extend로 변조 가능한 PCR입니다.
// PCR 16은 TPM 2.0 spec상 debug용으로 OS extend가 허용됩니다 (store_linux_test.go
// 와 동일 상수).
const debugPCRSim = 16

// integrationPCRSelection — debug PCR(16)을 포함하여 tamper 시나리오 직접 트리거.
var integrationPCRSelection = []int{0, 7, debugPCRSim}

// withSimulator는 simulator를 default*Hook에 wire하여 QuoteLinux production
// 경로가 simulator 위에서 실행되게 합니다. 반환된 sim은 PCR_Extend 등 직접
// manipulation 용도. t.Cleanup으로 sim.Close + hook 복원 보장.
//
// 주의: simulator.Get()은 global lock — 한 번에 하나의 simulator만 살아있어야 함.
// 본 helper 사용 test들은 t.Parallel을 사용하지 않습니다.
func withSimulator(t *testing.T) *simulator.Simulator {
	t.Helper()
	sim, err := simulator.Get()
	if err != nil {
		t.Fatalf("simulator.Get: %v", err)
	}

	prevOpener := defaultTPMOpener
	defaultTPMOpener = func() (io.ReadWriteCloser, error) {
		// simulator를 직접 반환하되 Close는 t.Cleanup에서 1회만 호출 — wrapper로
		// Close를 noop 처리 (QuoteLinux의 defer rwc.Close()가 sim을 닫지 않게).
		return noopCloser{sim}, nil
	}
	t.Cleanup(func() {
		defaultTPMOpener = prevOpener
		_ = sim.Close()
	})
	return sim
}

// noopCloser는 io.ReadWriteCloser wrapper — Close는 noop. QuoteLinux는 호출당
// rwc를 한 번 열고 닫는 흐름인데, 같은 simulator를 여러 호출에서 재사용하려면
// Close가 underlying simulator를 닫으면 안 됩니다.
type noopCloser struct {
	rw io.ReadWriter
}

func (n noopCloser) Read(p []byte) (int, error)  { return n.rw.Read(p) }
func (n noopCloser) Write(p []byte) (int, error) { return n.rw.Write(p) }
func (noopCloser) Close() error                  { return nil }

// TestSimulator_QuoteLinux_VerifyQuote_round_trip
//
// simulator 위에서 production QuoteLinux + VerifyQuote 정상 round-trip 검증.
// AK 생성·Quote·PCR collect·Signature 모두 실 TPM 알고리즘 (simulator 구현).
func TestSimulator_QuoteLinux_VerifyQuote_round_trip(t *testing.T) {
	_ = withSimulator(t)

	nonce := []byte("nonce-sim-roundtrip-001")
	att, err := QuoteLinux(nonce, integrationPCRSelection)
	if err != nil {
		t.Fatalf("QuoteLinux: %v", err)
	}
	if att == nil {
		t.Fatal("QuoteLinux returned nil attestation")
	}
	if len(att.AKPublic) == 0 || len(att.QuoteInfo) == 0 || len(att.Signature) == 0 {
		t.Errorf("attestation missing data: AK=%d QI=%d Sig=%d",
			len(att.AKPublic), len(att.QuoteInfo), len(att.Signature))
	}
	if len(att.PCRValues) != len(integrationPCRSelection) {
		t.Errorf("PCRValues count = %d, want %d",
			len(att.PCRValues), len(integrationPCRSelection))
	}

	if err := VerifyQuote(att, nonce); err != nil {
		t.Errorf("VerifyQuote round-trip: %v, want nil", err)
	}
}

// TestSimulator_VerifyQuote_nonce_mismatch
//
// simulator로 발급한 정상 attestation을 다른 nonce로 verify → ErrQuoteNonceMismatch.
func TestSimulator_VerifyQuote_nonce_mismatch(t *testing.T) {
	_ = withSimulator(t)

	signed := []byte("nonce-signed-by-tpm")
	att, err := QuoteLinux(signed, integrationPCRSelection)
	if err != nil {
		t.Fatalf("QuoteLinux: %v", err)
	}

	verifyErr := VerifyQuote(att, []byte("nonce-expected-different"))
	if !errors.Is(verifyErr, ErrQuoteNonceMismatch) {
		t.Errorf("want ErrQuoteNonceMismatch, got %v", verifyErr)
	}
}

// TestSimulator_VerifyQuote_PCR_tamper_after_quote
//
// 첫 quote 발급 후 PCR 16을 extend → 두 번째 발급 quote는 새 PCR 값을 반영하나,
// 첫 quote의 PCRValues를 새 quote에 swap-in하면 PCR digest mismatch.
//
// 본 시나리오는 실 attack scenario — quote 발급 시점과 verify 시점 사이에 PCR이
// 변하면 quote가 reject되어야. 첫 attestation의 PCRDigest는 첫 시점 PCR 값에
// 기반 — 검증자가 새 시점 PCR 값을 attach하면 mismatch 검출.
func TestSimulator_VerifyQuote_PCR_tamper_after_quote(t *testing.T) {
	sim := withSimulator(t)

	nonce := []byte("nonce-pcr-tamper")
	att, err := QuoteLinux(nonce, integrationPCRSelection)
	if err != nil {
		t.Fatalf("QuoteLinux #1: %v", err)
	}

	// PCR 16을 임의 값으로 extend — simulator 내부 PCR 상태가 변경됨.
	extension := bytes.Repeat([]byte{0xAA}, sha256.Size)
	if err := tpm2.PCRExtend(sim, tpmutil.Handle(debugPCRSim), tpm2.AlgSHA256, extension, ""); err != nil {
		t.Fatalf("PCRExtend: %v", err)
	}

	// 두 번째 quote — 새 PCR 값을 반영하나 첫 quote의 PCRDigest와는 mismatch.
	att2, err := QuoteLinux(nonce, integrationPCRSelection)
	if err != nil {
		t.Fatalf("QuoteLinux #2: %v", err)
	}

	// attack 모의 — 첫 quote의 sig/info에 두 번째 quote의 PCRValues를 swap-in.
	// 첫 quote의 PCRDigest는 첫 PCR 값으로 sign되었으므로, 두 번째 PCR 값으로
	// 재계산하면 mismatch → ErrQuotePCRMismatch.
	att.PCRValues = att2.PCRValues
	verifyErr := VerifyQuote(att, nonce)
	if !errors.Is(verifyErr, ErrQuotePCRMismatch) {
		t.Errorf("want ErrQuotePCRMismatch after PCR tamper, got %v", verifyErr)
	}

	// 정상 case 확인 — att2 자체는 새 PCR + 새 quote으로 일관, VerifyQuote nil.
	if err := VerifyQuote(att2, nonce); err != nil {
		t.Errorf("att2 자체 VerifyQuote: %v, want nil", err)
	}
}

// TestSimulator_VerifyQuote_signature_tamper
//
// simulator로 발급한 정상 signature를 1 byte 변조 → 검증 실패.
// 결과 sentinel은 ErrQuoteSignatureInvalid 또는 ErrQuoteInfoParse (R/S 인코딩이
// 깨지면 decode 실패 — 둘 다 reject).
func TestSimulator_VerifyQuote_signature_tamper(t *testing.T) {
	_ = withSimulator(t)

	nonce := []byte("nonce-sig-tamper")
	att, err := QuoteLinux(nonce, integrationPCRSelection)
	if err != nil {
		t.Fatalf("QuoteLinux: %v", err)
	}
	if len(att.Signature) == 0 {
		t.Fatal("signature empty — cannot tamper")
	}

	tampered := append([]byte(nil), att.Signature...)
	tampered[len(tampered)-1] ^= 0x01
	att.Signature = tampered

	verifyErr := VerifyQuote(att, nonce)
	if !errors.Is(verifyErr, ErrQuoteSignatureInvalid) &&
		!errors.Is(verifyErr, ErrQuoteInfoParse) {
		t.Errorf("want ErrQuoteSignatureInvalid or ErrQuoteInfoParse, got %v", verifyErr)
	}
}

// TestSimulator_VerifyQuote_결정론_같은_입력_같은_결과
//
// 같은 simulator + 같은 attestation + 같은 nonce → 두 번 verify 결과 동일.
func TestSimulator_VerifyQuote_결정론_같은_입력_같은_결과(t *testing.T) {
	_ = withSimulator(t)

	nonce := []byte("nonce-sim-determinism")
	att, err := QuoteLinux(nonce, integrationPCRSelection)
	if err != nil {
		t.Fatalf("QuoteLinux: %v", err)
	}
	err1 := VerifyQuote(att, nonce)
	err2 := VerifyQuote(att, nonce)
	if (err1 == nil) != (err2 == nil) {
		t.Errorf("결정론 위반: err1=%v err2=%v", err1, err2)
	}
	if err1 != nil {
		t.Errorf("VerifyQuote: %v, want nil", err1)
	}
}

// TestSimulator_QuoteLinux_같은_AK_같은_nonce_signature_재현성
//
// 같은 simulator 상태 + 같은 nonce + 같은 PCR selection → 두 번 QuoteLinux 결과
// 의 QuoteInfo 내부 PCRDigest가 동일 (PCR 변화 없으면). Signature는 ECDSA
// nonce(k) 무작위라 매번 다름 — 본 test는 QuoteInfo만 비교.
//
// (PCR 변경 없는 한 quote 내부 PCRDigest 결정론은 fingerprint v2와 같은
// 보장이며 quote sign에도 직접 영향.)
func TestSimulator_QuoteLinux_같은_입력_PCRDigest_결정론(t *testing.T) {
	_ = withSimulator(t)

	nonce := []byte("nonce-sim-pcrdigest")
	att1, err := QuoteLinux(nonce, integrationPCRSelection)
	if err != nil {
		t.Fatalf("QuoteLinux #1: %v", err)
	}
	att2, err := QuoteLinux(nonce, integrationPCRSelection)
	if err != nil {
		t.Fatalf("QuoteLinux #2: %v", err)
	}

	ad1, err := tpm2.DecodeAttestationData(att1.QuoteInfo)
	if err != nil {
		t.Fatalf("decode #1: %v", err)
	}
	ad2, err := tpm2.DecodeAttestationData(att2.QuoteInfo)
	if err != nil {
		t.Fatalf("decode #2: %v", err)
	}
	if ad1.AttestedQuoteInfo == nil || ad2.AttestedQuoteInfo == nil {
		t.Fatal("AttestedQuoteInfo nil")
	}
	if !bytes.Equal(ad1.AttestedQuoteInfo.PCRDigest, ad2.AttestedQuoteInfo.PCRDigest) {
		t.Errorf("PCRDigest 결정론 위반: #1=%x #2=%x",
			ad1.AttestedQuoteInfo.PCRDigest, ad2.AttestedQuoteInfo.PCRDigest)
	}
}
