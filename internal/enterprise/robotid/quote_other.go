//go:build rosshield_enterprise && !linux

// quote_other.go — non-Linux TPM Quote stub (v3).
//
// Linux 외에서는 go-tpm-tools/client AK 발급 + Quote가 동작하지 않습니다.
// 본 stub은 ErrTPMNotAvailable을 즉시 반환하여 호출자가 v2 fingerprint
// path로 fallback할 수 있게 합니다 (옵션 원칙 일관).
//
// VerifyQuote는 quote_attestation.go가 OS-agnostic이므로 모든 OS에서 사용
// 가능 — 즉 비-Linux 검증자 서버도 정상 동작.

package robotid

// QuoteLinux는 non-Linux stub — ErrTPMNotAvailable 즉시 반환.
// 본 함수가 호출되는 일은 운영 흐름상 없음 (CollectEKCertLinux와 패턴 일관 —
// non-Linux는 collector가 미리 ErrCollectorNotSupported 반환).
func QuoteLinux(_ []byte, _ []int) (*QuoteAttestation, error) {
	return nil, ErrTPMNotAvailable
}
