//go:build rosshield_enterprise

package robotid

import (
	"errors"
	"runtime"
	"testing"
)

// TestNewCollector_OS별_분기
//
// build tag 분기 — Linux → realCollector, 그외 → stubCollector. test가 build
// 양쪽에서 통과되도록 sentinel 확인 (Compute가 빈 inputs으로 진행 가능).
func TestNewCollector_OS별_분기(t *testing.T) {
	c := NewCollector()
	if c == nil {
		t.Fatal("NewCollector returned nil")
	}

	// 본 테스트 OS에 따라 기대값 분기. stubCollector는 모든 메서드가
	// ErrCollectorNotSupported를 반환 — realCollector는 OS 자체의 답을 반환.
	switch runtime.GOOS {
	case "linux":
		// realCollector는 호출 가능 — 결과는 환경에 의존하므로 nil-check만 수행.
		// EKCert는 TPM 부재 환경(CI)에서 ErrTPMNotAvailable 정상.
		_, ekErr := c.CollectEKCert()
		if ekErr != nil && !errors.Is(ekErr, ErrTPMNotAvailable) {
			t.Errorf("CollectEKCert: 예상 외 에러 %v", ekErr)
		}

		// MAC은 어느 호스트나 1개 이상 (loopback 외) 기대 가능하나 CI 환경
		// 격리 시 0개도 가능 — 에러만 없으면 통과.
		_, macErr := c.CollectMACs()
		if macErr != nil {
			t.Errorf("CollectMACs: 예상 외 에러 %v", macErr)
		}

		// CPU serial은 권한 부족·환경에 따라 ErrCPUSerialNotAvailable 정상.
		_, cpuErr := c.CollectCPUSerial()
		if cpuErr != nil && !errors.Is(cpuErr, ErrCPUSerialNotAvailable) {
			t.Errorf("CollectCPUSerial: 예상 외 에러 %v", cpuErr)
		}

	default:
		// non-Linux (Windows·macOS): 모든 메서드가 ErrCollectorNotSupported.
		_, ekErr := c.CollectEKCert()
		if !errors.Is(ekErr, ErrCollectorNotSupported) {
			t.Errorf("stub CollectEKCert: want ErrCollectorNotSupported, got %v", ekErr)
		}
		_, macErr := c.CollectMACs()
		if !errors.Is(macErr, ErrCollectorNotSupported) {
			t.Errorf("stub CollectMACs: want ErrCollectorNotSupported, got %v", macErr)
		}
		_, cpuErr := c.CollectCPUSerial()
		if !errors.Is(cpuErr, ErrCollectorNotSupported) {
			t.Errorf("stub CollectCPUSerial: want ErrCollectorNotSupported, got %v", cpuErr)
		}
	}
}

// TestCollector_Compute_통합_빈_inputs_OK
//
// stub OS에서도 Collector 결과 (모두 zero) + salt만 채워서 Compute 정상.
// 운영 정책에 따라 빈 inputs으로 fingerprint 산출 가능함을 보장.
func TestCollector_Compute_통합_빈_inputs_OK(t *testing.T) {
	c := NewCollector()

	// 결과 일부 sentinel은 빈 값으로 변환 (Compute 입력은 raw 값 그대로 받음).
	ek, _ := c.CollectEKCert()
	macs, _ := c.CollectMACs()
	cpu, _ := c.CollectCPUSerial()

	fp, err := Compute(Inputs{
		EKCert:    ek,
		MACs:      macs,
		CPUSerial: cpu,
		Salt:      []byte("tenant-fixed-salt"),
	})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if fp.Hash == "" {
		t.Error("fingerprint hash 비어있음")
	}
	if fp.Length == 0 {
		t.Error("fingerprint length 0")
	}
}

// TestCollector_OS_sentinel_분리
//
// non-Linux OS에서 stubCollector의 sentinel은 ErrTPMNotAvailable·
// ErrCPUSerialNotAvailable과 분리 — 호출자는 ErrCollectorNotSupported로
// 운영자에게 "본 OS 미지원" 명확히 전달 가능.
func TestCollector_OS_sentinel_분리(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("Linux는 realCollector — stub sentinel 검증 skip")
	}

	c := NewCollector()
	_, err := c.CollectEKCert()
	if errors.Is(err, ErrTPMNotAvailable) {
		t.Errorf("non-Linux stub은 ErrTPMNotAvailable이 아닌 ErrCollectorNotSupported 반환해야 (got %v)", err)
	}
	if !errors.Is(err, ErrCollectorNotSupported) {
		t.Errorf("want ErrCollectorNotSupported, got %v", err)
	}
}
