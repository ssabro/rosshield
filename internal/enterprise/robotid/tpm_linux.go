//go:build rosshield_enterprise && linux

// tpm_linux.go — Linux 전용 TPM EK certificate 수집기.
//
// 본 파일은 `go-tpm-tools/client`로 TPM 2.0 Endorsement Key를 로드하여 public
// key를 DER (x509.MarshalPKIXPublicKey) 형태로 반환합니다.
//
// design doc §6.5 TPM Quote는 본 round 미포함 — EK 자체만으로 robot identity
// 결합. PCR + Quote는 후속 round (E36 burn-in 또는 D-3 v2).
//
// 디바이스 경로: /dev/tpmrm0 우선 fallback /dev/tpm0 (keystore/tpm/store_linux.go
// 패턴 일관 — tpm2.OpenTPM() 기본 동작이 동일 fallback 적용).
//
// 디바이스 부재·권한 부족·TPM 2.0 미장착 시 ErrTPMNotAvailable로 묶어 반환.

package robotid

import (
	"crypto/x509"
	"errors"
	"fmt"
	"io"

	"github.com/google/go-tpm-tools/client"
	"github.com/google/go-tpm/legacy/tpm2"
)

// tpmOpener는 TPM 디바이스 open hook입니다 — test seam (mock 주입).
// nil이면 production 경로(/dev/tpmrm0 → /dev/tpm0).
type tpmOpener func() (io.ReadWriteCloser, error)

// ekLoader는 rwc로부터 client.Key (EK)를 로드하는 hook입니다 — test seam.
// nil이면 production 경로(client.EndorsementKeyECC).
//
// go-tpm-tools/client.EndorsementKeyECC signature와 일치 (io.ReadWriter).
type ekLoader func(rw io.ReadWriter) (*client.Key, error)

// 본 함수 변수는 test에서만 override됩니다 (tpm_test.go의 newWith*Hook).
// production 경로는 nil (default 분기).
var (
	defaultTPMOpener tpmOpener
	defaultEKLoader  ekLoader
)

// collectEKCertLinux는 TPM EK public key를 DER 형태로 수집합니다.
//
// 흐름:
//  1. tpm2.OpenTPM() — /dev/tpmrm0 → /dev/tpm0 fallback.
//  2. client.EndorsementKeyECC(rwc) — EK ECC 핸들 로드.
//  3. x509.MarshalPKIXPublicKey(ek.PublicKey()) — DER 직렬화.
//
// 어느 단계라도 실패 시 ErrTPMNotAvailable로 wrap. 호출자는 errors.Is로
// 분기 후 EKCert nil로 Compute 진행 가능.
func collectEKCertLinux() ([]byte, error) {
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

	loader := defaultEKLoader
	if loader == nil {
		loader = client.EndorsementKeyECC
	}
	ek, err := loader(rwc)
	if err != nil {
		return nil, fmt.Errorf("%w: load EK: %v", ErrTPMNotAvailable, err)
	}
	defer ek.Close()

	pub := ek.PublicKey()
	if pub == nil {
		return nil, fmt.Errorf("%w: EK public key is nil", ErrTPMNotAvailable)
	}

	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal EK public: %v", ErrTPMNotAvailable, err)
	}
	if len(der) == 0 {
		return nil, errors.New("robotid: EK DER is empty")
	}
	return der, nil
}

// openTPMDevice는 production TPM open 경로 — go-tpm legacy/tpm2.OpenTPM 위임.
// 인자 없으면 /dev/tpmrm0 → /dev/tpm0 자동 fallback (go-tpm 내부 동작).
func openTPMDevice() (io.ReadWriteCloser, error) {
	return tpm2.OpenTPM()
}
