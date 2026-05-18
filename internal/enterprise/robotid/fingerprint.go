//go:build rosshield_enterprise

// fingerprint.go — D-3 robot identity fingerprint 산출 본체 (enterprise edition).
//
// 본 패키지는 design doc `phase7-public-transition-design.md` §6.5의 알고리즘을
// 구현합니다:
//
//  1. 로봇 측 agent가 TPM EK certificate · 네트워크 MAC 주소 · CPU serial 수집.
//  2. fingerprint = sha256( EK_cert ‖ "|" ‖ sorted_macs ‖ "|" ‖ cpu_serial ‖ "|" ‖ salt )
//  3. salt는 tenant-level 고정 (cross-tenant fingerprint 누출 방지).
//  4. 감사 결과에 fingerprint binding — 다른 로봇이 결과 위조 시 fingerprint
//     불일치로 즉시 검출.
//
// 본 round는 TPM Quote (PCR 결합) 미포함 — EK certificate만으로 binding.
// PCR 결합은 후속 round (E36 burn-in 단계 또는 D-3 v2)에서 추가 예정.
//
// 참조:
//   - docs/design/notes/phase7-public-transition-design.md §6.5
//   - docs/design/13-patent-strategy.md §13.5 1순위 결합 청구항 D-3

package robotid

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
)

// HashHexLength는 sha256 hash의 hex 표현 길이입니다 (32바이트 → 64자).
const HashHexLength = sha256.Size * 2

// 오류 정의 — Compute가 입력 검증 실패 시 반환.
var (
	// ErrSaltRequired는 Inputs.Salt가 nil 또는 빈 slice일 때 반환됩니다.
	//
	// salt는 tenant 분리의 핵심 — 빈 salt는 cross-tenant fingerprint 누출
	// 보호가 불가하므로 거부합니다 (design doc §6.5 3).
	ErrSaltRequired = errors.New("robotid: salt is required (tenant cross-leak protection)")
)

// Fingerprint는 robot identity fingerprint 산출 결과입니다.
//
// 모든 필드는 audit 결과에 attach되어 외부 검증자가 같은 inputs로 재현
// 가능하도록 직렬화됩니다 (JSON tag 한국어 직접 노출 금지 — 영문 snake_case).
type Fingerprint struct {
	// Hash는 sha256 hex 소문자 64자입니다 (canonical 표현).
	Hash string `json:"hash"`
	// Length는 hash byte 길이입니다 (현재 sha256 → 32, 후속 multihash 호환용).
	Length int `json:"length"`
	// HasTPM은 EKCert가 non-nil & non-empty일 때 true입니다 (TPM 부착 로봇 식별).
	HasTPM bool `json:"has_tpm"`
	// MACCount는 입력 MACs slice 길이입니다 (dedupe 책임은 collector — Compute는 입력 보존).
	MACCount int `json:"mac_count"`
}

// Inputs는 fingerprint 산출에 사용할 robot 식별자 묶음입니다.
//
// 각 필드는 OS별 Collector가 채워서 전달합니다 (Linux: /sys, /proc; non-Linux:
// stub). 빈 값 허용 — TPM 없는 로봇·CPU serial 부재 환경에서도 fingerprint
// 산출 가능 (salt만 필수).
type Inputs struct {
	// EKCert는 TPM Endorsement Key public key DER (x509.MarshalPKIXPublicKey 결과).
	// nil 또는 빈 slice 허용 — HasTPM=false flag만 차이.
	EKCert []byte

	// MACs는 네트워크 인터페이스 MAC 주소 slice. 입력 순서 무관 — Compute가
	// 사본 정렬 후 사용 (입력 mutation 금지). 빈 slice 허용.
	MACs []string

	// CPUSerial은 CPU 또는 시스템 serial number. trim + lowercase로 정규화.
	// 빈 string 허용 — 환경에 따라 dmi/cpuinfo 부재 가능.
	CPUSerial string

	// Salt는 tenant-level 고정 salt. 필수 — 빈 salt는 ErrSaltRequired.
	// cross-tenant fingerprint 누출 방지 (다른 tenant가 같은 robot에 다른
	// fingerprint를 산출하도록).
	Salt []byte
}

// Compute는 Inputs으로부터 결정론적 robot fingerprint를 산출합니다.
//
// 알고리즘 (design doc §6.5 2):
//
//	hash = sha256( EKCert ‖ "|" ‖ sorted_macs ‖ "|" ‖ normalized_cpu ‖ "|" ‖ salt )
//
// 결정론 보장:
//   - MACs는 lex sort 후 "," join (입력 순서 무관).
//   - CPUSerial은 strings.TrimSpace + strings.ToLower로 정규화.
//   - empty field도 separator "|"는 보존 (빈 EKCert + 빈 MACs + 빈 CPUSerial,
//     salt="S" → "|||S"의 sha256).
//   - Inputs slice 필드 mutation 금지 (사본 정렬).
//
// 입력 검증: Salt가 nil 또는 length 0이면 ErrSaltRequired.
//
// 본 함수는 audit chain entry attach에 그대로 사용 가능한 hex 표현 + meta를
// Fingerprint 구조로 반환합니다. binding 저장·검증은 호출 측 (audit 도메인의
// 어댑터)에서 담당합니다 (E32 후속 통합).
func Compute(in Inputs) (Fingerprint, error) {
	if len(in.Salt) == 0 {
		return Fingerprint{}, ErrSaltRequired
	}

	// MACs 정렬 — 입력 mutation 방지 위해 사본 사용.
	var sortedMACs string
	macCount := len(in.MACs)
	if macCount > 0 {
		macsCopy := make([]string, macCount)
		copy(macsCopy, in.MACs)
		sort.Strings(macsCopy)
		sortedMACs = strings.Join(macsCopy, ",")
	}

	// CPUSerial 정규화 — trim + lowercase.
	normCPU := strings.ToLower(strings.TrimSpace(in.CPUSerial))

	// sha256 누적 — EKCert ‖ "|" ‖ sortedMACs ‖ "|" ‖ normCPU ‖ "|" ‖ Salt.
	// EKCert가 nil이라도 separator는 보존 (Write(nil)은 no-op, 다음 separator만 기록).
	h := sha256.New()
	h.Write(in.EKCert)
	h.Write([]byte{'|'})
	h.Write([]byte(sortedMACs))
	h.Write([]byte{'|'})
	h.Write([]byte(normCPU))
	h.Write([]byte{'|'})
	h.Write(in.Salt)

	sum := h.Sum(nil)

	return Fingerprint{
		Hash:     hex.EncodeToString(sum),
		Length:   sha256.Size,
		HasTPM:   len(in.EKCert) > 0,
		MACCount: macCount,
	}, nil
}
