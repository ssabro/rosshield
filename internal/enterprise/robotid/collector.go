//go:build rosshield_enterprise

// collector.go — robot 식별자 수집 OS-agnostic 인터페이스 + dispatcher.
//
// 본 파일은 build tag 분리된 collector_linux.go / collector_other.go 중
// 적절한 한쪽이 OS별 본체를 제공합니다 (NewCollector).
//
// 사용 패턴:
//
//	c := robotid.NewCollector()
//	ek, _ := c.CollectEKCert()       // TPM 없으면 ErrTPMNotAvailable + nil
//	macs, _ := c.CollectMACs()       // 빈 slice 허용
//	cpu, _ := c.CollectCPUSerial()   // 부재 시 ErrCPUSerialNotAvailable + ""
//
//	fp, err := robotid.Compute(robotid.Inputs{
//	    EKCert: ek, MACs: macs, CPUSerial: cpu, Salt: tenantSalt,
//	})
//
// 부재 sentinel은 fatal이 아닙니다 — Compute는 빈 값으로 진행 가능 (HasTPM·
// MACCount flag로 식별). 호출자가 운영 정책에 따라 강도 결정 (TPM 필수 모드
// 운영자는 ErrTPMNotAvailable을 reject로 격상 가능).

package robotid

import "errors"

// 오류 sentinel — 환경에 따른 부재를 표현 (fatal이 아님).
var (
	// ErrTPMNotAvailable는 TPM 디바이스 부재·권한 부족·TPM 2.0 미장착 시 반환됩니다.
	// 호출자는 EKCert를 nil로 둬도 fingerprint 산출 가능 (HasTPM=false).
	ErrTPMNotAvailable = errors.New("robotid: TPM not available on this host")

	// ErrCollectorNotSupported는 본 OS (Linux 외)에서 stub Collector가 반환합니다.
	// non-Linux desktop·CI 환경에서 빌드 + 코어 테스트는 통과시키되 실 robot
	// 식별자 수집은 Linux 한정으로 제한.
	ErrCollectorNotSupported = errors.New("robotid: collector not supported on this OS")

	// ErrCPUSerialNotAvailable은 dmi/cpuinfo 부재·권한 부족 시 반환됩니다.
	// 호출자는 빈 string으로 Compute 가능 (CPUSerial 0-length normalize).
	ErrCPUSerialNotAvailable = errors.New("robotid: CPU serial not available")
)

// DefaultPCRSelection는 v2 부팅 무결성 결합 시 권장 PCR index 집합입니다.
//
// PCR 0 — UEFI/BIOS firmware 코드 측정 (CRTM).
// PCR 2 — UEFI driver 및 application 측정.
// PCR 4 — boot loader 측정 (Linux 환경 grub/shim 등).
// PCR 7 — Secure Boot 정책 및 인증 키 측정.
//
// 이 4개는 boot integrity 핵심 — boot loader/kernel/driver/secure boot 변조를
// 검출하기 위해 충분 (TCG PC Client Platform Firmware Profile spec 기준).
// 호출자는 운영 정책에 따라 다른 PCR 집합을 전달 가능 (예: PCR 8-9 ima 측정,
// PCR 14 MOK list 등).
var DefaultPCRSelection = []int{0, 2, 4, 7}

// Collector는 OS별 robot 식별자 수집 인터페이스입니다.
//
// 각 메서드는 부재 sentinel을 반환할 수 있으며 — 호출자는 빈 값과 sentinel
// 양쪽을 처리해야 합니다 (Compute는 빈 값 그대로 수용).
//
// 구현체는 OS별 build tag로 분기:
//   - linux: realCollector (collector_linux.go)
//   - else:  stubCollector (collector_other.go)
type Collector interface {
	// CollectEKCert는 TPM EK certificate (public key DER)를 수집합니다.
	// TPM 부재 시 (nil, ErrTPMNotAvailable).
	CollectEKCert() ([]byte, error)

	// CollectMACs는 활성 네트워크 인터페이스 MAC 주소 배열을 수집합니다.
	// loopback 제외. 빈 slice도 정상 (no-NIC container 등).
	CollectMACs() ([]string, error)

	// CollectCPUSerial은 CPU/시스템 serial을 수집합니다.
	// 부재 시 ("", ErrCPUSerialNotAvailable).
	CollectCPUSerial() (string, error)

	// CollectPCRValues는 지정된 PCR index 집합의 값을 수집합니다 (v2 부팅
	// 무결성 결합용). pcrs는 sort 불필요 (구현이 PCRSelection 구성 시 사용).
	// TPM 부재·접근 실패 시 (nil, ErrTPMNotAvailable). 호출자는 nil map을
	// 그대로 Inputs.PCRValues에 전달하여 v1 path로 fallback 가능.
	CollectPCRValues(pcrs []int) (map[int][]byte, error)
}
