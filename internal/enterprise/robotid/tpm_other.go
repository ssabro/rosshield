//go:build rosshield_enterprise && !linux

// tpm_other.go — non-Linux TPM stub.
//
// Windows·macOS·기타 OS에서는 TPM EK 수집을 지원하지 않습니다. 본 round의
// go-tpm-tools dep은 Linux 한정 (keystore/tpm/store_linux.go 패턴 일관).
//
// 본 파일은 type alias만 정의 — 실제 collector_other.go의 stubCollector가
// ErrCollectorNotSupported를 반환하므로 이 함수는 호출 경로에 들어오지 않습니다.

package robotid

// collectEKCertLinux는 non-Linux stub — 실제 호출 경로 없음 (stubCollector가
// 먼저 ErrCollectorNotSupported 반환). 본 함수는 build symmetry 용도.
func collectEKCertLinux() ([]byte, error) {
	return nil, ErrTPMNotAvailable
}
