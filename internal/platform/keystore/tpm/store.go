// Package tpm은 KeyStore의 TPM 2.0 PCR-sealed 어댑터입니다 (E34 Stage 2-B).
//
// 모델 (R41-1=B): TPM이 ed25519 raw private key를 PCR-sealed blob으로 보관합니다.
// 부팅 시 unseal하여 메모리로 로드 → bootstrap이 soft.WrapPrivateKey로 signer 생성.
// TPM 자체가 ed25519 서명을 수행하지는 않습니다 (호환성 우선). enterprise 차별화로
// "TPM Signer 어댑터" (모델 A)는 후속 epic.
//
// 라이브러리 (R41-2): google/go-tpm-tools v0.4.8 (`client.StorageRootKeyECC` +
// `Key.Seal`/`Key.Unseal` 사용). simulator는 in-process Microsoft TPM2 reference
// (`simulator.Get()`), CGO 필수이며 통합 테스트는 `//go:build tpm_integration`
// 태그로 분리하여 일반 `go test ./...`에서 제외 (Windows 개발 환경 보호).
//
// PCR set (R41-3): 기본 [0, 2, 4, 7] — BIOS·OPROM·EFI·Secure Boot policy.
// strict mode([0,2,4,7,11,12])는 Phase 5 carryover.
//
// 플랫폼 분리:
//   - Linux: store_linux.go — 실제 TPM 호출 (/dev/tpmrm0 → /dev/tpm0 또는 명시 path)
//   - Windows/macOS: store_other.go — ErrTpmDeviceNotAvailable 반환 (조용한 fallback 금지)
//
// 설계: docs/design/notes/e34-tpm-design.md (R40-2 = swtpm 결정 + R41 확정).
package tpm

import (
	"errors"
)

// Options는 TPM Store 생성 옵션입니다.
type Options struct {
	// DevicePath는 TPM 디바이스 경로입니다.
	// 빈 값이면 OS 기본 (Linux: /dev/tpmrm0 → /dev/tpm0 fallback).
	// swtpm/integration 테스트에서는 unix socket 경로를 직접 지정.
	DevicePath string

	// PCRSelection은 sealing policy에 사용할 PCR 인덱스입니다.
	// 빈 값이면 R41-3 기본 [0, 2, 4, 7].
	PCRSelection []int

	// SealingDir는 sealed blob을 저장할 디렉터리입니다 (예: $DataDir/keys/tpm/).
	// handle별로 SealingDir/<handle>.sealed 파일이 생성됩니다.
	// 비어 있으면 ErrSealingDirRequired.
	SealingDir string
}

// 공통 sentinel.
var (
	// ErrTpmDeviceNotAvailable는 TPM 디바이스가 사용 불가일 때 반환됩니다.
	// Windows·macOS는 항상 본 에러를 반환 (조용한 fallback 금지, 원칙 §11).
	// Linux에서도 /dev/tpm* 부재·권한 부족·TPM 2.0 미장착 시 동일.
	ErrTpmDeviceNotAvailable = errors.New("keystore/tpm: TPM 2.0 device not available (Linux only with /dev/tpm*)")

	// ErrPcrMismatch는 sealed blob의 PCR policy가 unseal 시점의 PCR 값과 일치하지
	// 않을 때 반환됩니다 (부팅 환경 변조 신호).
	ErrPcrMismatch = errors.New("keystore/tpm: PCR values changed since seal (boot environment tampered)")

	// ErrSealingDirRequired는 Options.SealingDir이 비었을 때 반환됩니다.
	ErrSealingDirRequired = errors.New("keystore/tpm: SealingDir is required")
)

// defaultPCRSelection은 R41-3 결정에 따른 권장 PCR set을 반환합니다.
//
// PCR 0: UEFI firmware (BIOS) — 펌웨어 변조 차단.
// PCR 2: OPROM (GPU·NIC 옵션 ROM) — 옵션 ROM 변조 차단.
// PCR 4: MBR/EFI 첫 코드 — 부트로더 변조 차단.
// PCR 7: Secure Boot policy + 사용 키 — 부트 체인 신뢰 근간.
func defaultPCRSelection() []int { return []int{0, 2, 4, 7} }

// resolvePCRSelection은 Options.PCRSelection이 비었으면 기본값을 반환합니다.
func resolvePCRSelection(opts Options) []int {
	if len(opts.PCRSelection) == 0 {
		return defaultPCRSelection()
	}
	out := make([]int, len(opts.PCRSelection))
	copy(out, opts.PCRSelection)
	return out
}
