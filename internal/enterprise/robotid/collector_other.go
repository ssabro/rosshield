//go:build rosshield_enterprise && !linux

// collector_other.go — non-Linux stub Collector.
//
// Windows·macOS·기타 OS에서는 robot 식별자 수집을 지원하지 않습니다.
// 모든 Collect* 메서드가 ErrCollectorNotSupported를 반환하여 빌드 가능성과
// 코어 테스트는 보존하되, 실 robot 식별자 수집은 Linux 한정으로 제한합니다
// (실 운영 robot은 Linux 가정 — desktop은 audit 콘솔만).

package robotid

// NewCollector는 non-Linux 환경에서 stub Collector를 반환합니다.
//
// 모든 Collect* 호출이 zero value + ErrCollectorNotSupported를 반환합니다.
// 호출자가 ErrCollectorNotSupported를 인지하고 fingerprint를 산출하지 않거나
// 빈 inputs (salt만 채운)으로 Compute 가능 — 정책 선택.
func NewCollector() Collector {
	return stubCollector{}
}

// stubCollector는 non-Linux OS용 zero-value 반환 collector입니다.
type stubCollector struct{}

func (stubCollector) CollectEKCert() ([]byte, error) {
	return nil, ErrCollectorNotSupported
}

func (stubCollector) CollectMACs() ([]string, error) {
	return nil, ErrCollectorNotSupported
}

func (stubCollector) CollectCPUSerial() (string, error) {
	return "", ErrCollectorNotSupported
}

// CollectPCRValues는 non-Linux stub — TPM 자체가 미지원이므로 nil + sentinel.
// 호출자는 nil map을 Inputs.PCRValues에 전달하여 v1 path로 fallback 가능.
func (stubCollector) CollectPCRValues(_ []int) (map[int][]byte, error) {
	return nil, ErrCollectorNotSupported
}
