//go:build rosshield_enterprise

// fingerprint.go — D-3 robot identity fingerprint 산출 본체 (enterprise edition).
//
// 본 패키지는 design doc `phase7-public-transition-design.md` §6.5의 알고리즘을
// 구현합니다:
//
//  1. 로봇 측 agent가 TPM EK certificate · 네트워크 MAC 주소 · CPU serial 수집.
//  2. v1 fingerprint = sha256( EK_cert ‖ "|" ‖ sorted_macs ‖ "|" ‖ cpu_serial ‖ "|" ‖ salt )
//  3. salt는 tenant-level 고정 (cross-tenant fingerprint 누출 방지).
//  4. 감사 결과에 fingerprint binding — 다른 로봇이 결과 위조 시 fingerprint
//     불일치로 즉시 검출.
//  5. v2 (옵션, design doc §6.5 5): TPM PCR 값 결합 — 부팅 무결성까지 증명.
//     v2 fingerprint = sha256( ... ‖ salt ‖ "|" ‖ pcr_digest_raw )
//     pcr_digest = sha256( concat(PCR[i] for sorted i in PCRValues) )
//
// PCR 결합은 옵션 — Inputs.PCRValues nil/empty → v1 알고리즘 그대로 (회귀 0).
// TPM Quote() 자체 (AK 서명 검증)는 별 round 예정 (attestation flow, scope 큼).
// 본 round는 PCR 값 결합까지만 — fingerprint에 PCR 포함하여 boot 변조 검출.
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
	// HasPCRQuote는 PCRValues가 1개 이상 있을 때 true입니다 (v2 부팅 무결성 결합).
	// false이면 v1 path (PCR 결합 없음).
	HasPCRQuote bool `json:"has_pcr_quote"`
	// PCRDigest는 sha256( concat(sorted PCR values) )의 hex 표현 (HashHexLength=64).
	// HasPCRQuote=false일 땐 빈 string (omitempty로 JSON에서 생략).
	// 외부 검증자가 같은 PCR 값으로 재현 가능하도록 노출.
	PCRDigest string `json:"pcr_digest,omitempty"`
	// MACCount는 입력 MACs slice 길이입니다 (dedupe 책임은 collector — Compute는 입력 보존).
	MACCount int `json:"mac_count"`
}

// Inputs는 fingerprint 산출에 사용할 robot 식별자 묶음입니다.
//
// 각 필드는 OS별 Collector가 채워서 전달합니다 (Linux: /sys, /proc, TPM;
// non-Linux: stub). 빈 값 허용 — TPM 없는 로봇·CPU serial 부재 환경에서도
// fingerprint 산출 가능 (salt만 필수).
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

	// PCRValues는 TPM Platform Configuration Register 값 map입니다 (v2 옵션).
	//   key   — PCR index (예: 0, 2, 4, 7 — boot loader · kernel · driver · secure boot).
	//   value — PCR digest 값 (sha256 알고리즘 기준 32바이트).
	//
	// nil 또는 빈 map → v1 알고리즘 (PCR 결합 없음, 회귀 0).
	// 1개 이상 → pcr_digest 계산 후 fingerprint에 결합 (부팅 무결성 증명).
	//
	// 결정론 위해 구현이 PCR index 정렬 후 ordered concat합니다 (map iteration
	// 순서 비결정 회피). 입력 map은 mutation되지 않습니다.
	PCRValues map[int][]byte
}

// Compute는 Inputs으로부터 결정론적 robot fingerprint를 산출합니다.
//
// v1 알고리즘 (design doc §6.5 2, PCRValues nil/empty 시):
//
//	hash = sha256( EKCert ‖ "|" ‖ sorted_macs ‖ "|" ‖ normalized_cpu ‖ "|" ‖ salt )
//
// v2 알고리즘 (design doc §6.5 5, PCRValues 채움 시):
//
//	pcr_digest_raw = sha256( concat(PCRValues[i] for sorted i) )
//	hash = sha256( EKCert ‖ "|" ‖ sorted_macs ‖ "|" ‖ normalized_cpu ‖ "|" ‖ salt
//	               ‖ "|" ‖ pcr_digest_raw )
//
// 결정론 보장:
//   - MACs는 lex sort 후 "," join (입력 순서 무관).
//   - CPUSerial은 strings.TrimSpace + strings.ToLower로 정규화.
//   - PCRValues는 PCR index sort 후 ordered concat (map 순서 invariant).
//   - empty field도 separator "|"는 보존 (빈 EKCert + 빈 MACs + 빈 CPUSerial,
//     salt="S" → "|||S"의 sha256).
//   - Inputs slice·map 필드 mutation 금지 (사본 사용).
//   - PCR digest는 raw 32바이트로 결합 (hex string 아님 — byte 효율 + 결정론).
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

	// PCR digest 산출 — PCRValues 비어있지 않으면 v2 path.
	// PCR index sort 후 ordered concat → sha256 → raw 32바이트.
	var (
		hasPCR       bool
		pcrDigestRaw []byte
		pcrDigestHex string
	)
	if len(in.PCRValues) > 0 {
		hasPCR = true
		pcrDigestRaw, pcrDigestHex = computePCRDigest(in.PCRValues)
	}

	// sha256 누적 — EKCert ‖ "|" ‖ sortedMACs ‖ "|" ‖ normCPU ‖ "|" ‖ Salt
	// (v2 시) ‖ "|" ‖ pcr_digest_raw.
	// EKCert가 nil이라도 separator는 보존 (Write(nil)은 no-op, 다음 separator만 기록).
	h := sha256.New()
	h.Write(in.EKCert)
	h.Write([]byte{'|'})
	h.Write([]byte(sortedMACs))
	h.Write([]byte{'|'})
	h.Write([]byte(normCPU))
	h.Write([]byte{'|'})
	h.Write(in.Salt)
	if hasPCR {
		h.Write([]byte{'|'})
		h.Write(pcrDigestRaw)
	}

	sum := h.Sum(nil)

	return Fingerprint{
		Hash:        hex.EncodeToString(sum),
		Length:      sha256.Size,
		HasTPM:      len(in.EKCert) > 0,
		HasPCRQuote: hasPCR,
		PCRDigest:   pcrDigestHex,
		MACCount:    macCount,
	}, nil
}

// computePCRDigest는 PCRValues로부터 결정론적 PCR digest를 산출합니다.
//
// 알고리즘:
//  1. PCR index 추출 후 오름차순 정렬 (map iteration 순서 비결정 회피).
//  2. 정렬된 순서대로 PCR 값을 concat (separator 없음 — 각 PCR 값은 고정
//     길이 32바이트 가정이라 ambiguity 없음. 가변 길이 PCR 입력 시에도
//     결정론은 정렬 순서로 보장).
//  3. sha256(concat) → 32바이트 raw + hex 64자 동시 반환.
//
// 반환: (raw 32바이트, hex 64자). raw는 fingerprint sha256 누적에 결합용,
// hex는 Fingerprint.PCRDigest 노출용.
//
// 입력 map은 mutation되지 않습니다 (사본 keys만 정렬).
func computePCRDigest(pcrs map[int][]byte) ([]byte, string) {
	indices := make([]int, 0, len(pcrs))
	for i := range pcrs {
		indices = append(indices, i)
	}
	sort.Ints(indices)

	h := sha256.New()
	for _, i := range indices {
		h.Write(pcrs[i])
	}
	raw := h.Sum(nil)
	return raw, hex.EncodeToString(raw)
}
