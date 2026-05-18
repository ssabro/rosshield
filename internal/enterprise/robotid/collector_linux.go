//go:build rosshield_enterprise && linux

// collector_linux.go — Linux 전용 robot 식별자 수집기.
//
// 본 파일은 다음 수집기를 stdlib만으로 구현합니다 (외부 dep 0 — `jaypipes/ghw`
// 미사용; design doc spec 그대로 stdlib + 직접 file read 노선):
//
//   - MAC: `net.Interfaces()` (stdlib) — loopback 제외, FlagUp 또는 down 무관
//     (사후 audit 시점에는 link 상태 무관하게 MAC 자체가 식별자).
//   - CPU serial: `/sys/devices/virtual/dmi/id/product_serial` (root 필요) →
//     fallback `/proc/cpuinfo` "Serial : ..." (ARM Raspberry Pi 등).
//
// EKCert는 tpm_linux.go가 별 책임 — collector_linux.go는 tpm_linux.go의
// CollectEKCertLinux 함수를 위임 호출합니다.

package robotid

import (
	"bufio"
	"bytes"
	"errors"
	"net"
	"os"
	"strings"
)

// dmiProductSerialPath는 시스템 SMBIOS product serial number 경로입니다.
// 일반적으로 root 권한 필요 — 부재 시 fallback.
const dmiProductSerialPath = "/sys/devices/virtual/dmi/id/product_serial"

// cpuinfoPath는 `/proc/cpuinfo` 경로 — ARM SBC (Raspberry Pi 등)는 "Serial : ..."
// 라인을 가집니다.
const cpuinfoPath = "/proc/cpuinfo"

// NewCollector는 Linux 환경에서 realCollector를 반환합니다.
func NewCollector() Collector {
	return realCollector{}
}

// realCollector는 Linux용 production 수집기입니다.
type realCollector struct{}

// CollectEKCert는 TPM EK certificate (public key DER)를 수집합니다.
// 본체는 tpm_linux.go의 collectEKCertLinux에 위임 — TPM 미장착·권한 부족 시
// ErrTPMNotAvailable.
func (realCollector) CollectEKCert() ([]byte, error) {
	return collectEKCertLinux()
}

// CollectMACs는 활성·비활성 네트워크 인터페이스의 MAC 주소를 수집합니다.
// 정렬·dedupe는 Compute가 담당 — 본 함수는 stdlib 호출 순서 그대로 반환.
//
// 필터:
//   - HardwareAddr 빈 인터페이스 제외 (loopback `lo`, tun/tap, bridge 일부).
//   - flag FlagLoopback 명시 제외.
//
// link state(FlagUp)은 무관 — robot이 idle 상태에서도 fingerprint 산출
// 가능해야.
func (realCollector) CollectMACs() ([]string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	macs := make([]string, 0, len(ifaces))
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if len(iface.HardwareAddr) == 0 {
			continue
		}
		macs = append(macs, iface.HardwareAddr.String())
	}
	return macs, nil
}

// CollectCPUSerial은 시스템 또는 CPU serial을 수집합니다.
//
// 시도 순서:
//  1. /sys/devices/virtual/dmi/id/product_serial — SMBIOS product serial (x86).
//  2. /proc/cpuinfo "Serial : ..." 라인 — ARM Raspberry Pi 등.
//
// 둘 다 부재·읽기 실패 시 ("", ErrCPUSerialNotAvailable). 호출자는 빈 string
// 으로 Compute 가능.
func (realCollector) CollectCPUSerial() (string, error) {
	if s, err := readDMIProductSerial(dmiProductSerialPath); err == nil && s != "" {
		return s, nil
	}
	if s, err := readCPUInfoSerial(cpuinfoPath); err == nil && s != "" {
		return s, nil
	}
	return "", ErrCPUSerialNotAvailable
}

// readDMIProductSerial은 SMBIOS product serial 파일을 읽습니다.
// 일반적으로 root 권한 필요. 빈 string · "To be filled by O.E.M." 등 placeholder는
// 호출자가 fallback하도록 정상 반환 (Compute가 lowercase normalize).
func readDMIProductSerial(path string) (string, error) {
	data, err := os.ReadFile(path) // #nosec G304 — path는 상수 const dmiProductSerialPath.
	if err != nil {
		return "", err
	}
	s := strings.TrimSpace(string(data))
	if s == "" {
		return "", errors.New("robotid: dmi product_serial is empty")
	}
	return s, nil
}

// readCPUInfoSerial은 /proc/cpuinfo의 "Serial : ..." 라인을 파싱합니다.
// ARM (Raspberry Pi, Jetson 등) 에서 가용. x86은 보통 부재 — 그 경우 0 반환.
func readCPUInfoSerial(path string) (string, error) {
	f, err := os.Open(path) // #nosec G304 — path는 상수 const cpuinfoPath.
	if err != nil {
		return "", err
	}
	defer func() {
		_ = f.Close()
	}()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		// "Serial" prefix 라인을 찾음. case-sensitive (kernel 표기).
		if !bytes.HasPrefix(line, []byte("Serial")) {
			continue
		}
		idx := bytes.IndexByte(line, ':')
		if idx < 0 || idx+1 >= len(line) {
			continue
		}
		val := strings.TrimSpace(string(line[idx+1:]))
		if val == "" {
			continue
		}
		return val, nil
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", errors.New("robotid: cpuinfo has no Serial line")
}
