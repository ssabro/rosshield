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
}
