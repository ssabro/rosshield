//go:build rosshield_enterprise

// fold.go — A-1 cross-witness fold-in 본체 (enterprise edition).
//
// 본 패키지는 spec-candidate-A-draft.md [0019]~[0020-4]의 알고리즘을 구현합니다:
//
//   - 멀티 테넌트 audit chain에서 정기 fold-in 시점이 되면, 다른 모든 활성 테넌트의
//     최신 checkpoint(tenantId, seq, hash, signedAt) 집합을 자기 체인의 새 entry에 fold-in.
//   - fold-in 엔트리의 hash는 (prevHash ‖ payloadDigest ‖ canonicalMeta ‖ canonicalCrossWitness)
//     를 sha256으로 묶어 산출. canonicalCrossWitness는 tenantId 사전식 정렬 + RFC 8785
//     준수 결정론적 JSON.
//   - 운영자가 한 테넌트만 사후 위조 → 다른 테넌트의 fold-in 엔트리 안 그 테넌트
//     checkpoint hash와 어긋남 → 외부 검증자가 CROSS_WITNESS_MISMATCH로 검출.
//
// 운영자가 모든 테넌트를 동시 재작성하는 시나리오는 본 패키지 밖의 외부 anchoring
// (TSA, public transparency log, webhook, 외부 저장소)으로 보강합니다 — A-1 cross-witness
// 만으로는 불충분 (spec-A [0020-3] 명시).
//
// 참조:
//   - docs/ip/spec-candidate-A-draft.md §실시례 4 [0019]~[0020-4], [0026] canonical 규칙
//   - docs/design/13-patent-strategy.md §13.5 1순위 결합 청구항 A-1
//   - docs/design/notes/phase7-public-transition-design.md §6.2 A-1 알고리즘

package crosswitness

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

// HashSize는 sha256 출력 크기입니다 (32바이트, 코어 audit과 일관).
const HashSize = 32

// Hash는 32바이트 sha256 출력입니다 (코어 audit.Hash와 같은 표현).
type Hash [HashSize]byte

// TenantCheckpoint는 다른 테넌트의 최신 체크포인트 1건을 나타냅니다 (spec-A [0019]).
//
// fold-in 시점에 운영자는 자기 외 활성 테넌트들의 최신 (TenantID, Seq, Hash, SignedAt) 4-tuple을
// 수집하여 새 fold-in 엔트리의 CrossWitness 배열에 함께 담습니다. canonical 직렬화 시
// tenantId 사전식 정렬이 강제됩니다 ([0020-1] (ii)).
type TenantCheckpoint struct {
	TenantID string    // 다른 테넌트 식별자
	Seq      int64     // 그 테넌트 안 단조 증가 시퀀스
	Hash     Hash      // 해당 시퀀스의 chain hash
	SignedAt time.Time // 체크포인트 서명 시각 (canonical은 UTC RFC3339Nano)
}

// 오류 정의 (spec-A [0020-2], [0022-2] FailureCode 일관).
var (
	// ErrCrossWitnessMismatch는 fold-in 엔트리가 보유한 TenantCheckpoint와 검증자
	// 입력 chain 재계산 결과가 어긋날 때 반환됩니다 (CROSS_WITNESS_MISMATCH).
	ErrCrossWitnessMismatch = errors.New("crosswitness: cross-witness mismatch")

	// ErrDuplicateTenant는 CrossWitness 배열 안 동일 tenantId가 두 번 이상
	// 등장할 때 반환됩니다 ([0020-1] (ii) "동일 tenantId가 중복되지 않으며").
	ErrDuplicateTenant = errors.New("crosswitness: duplicate tenant in witness set")

	// ErrEmptyTenantID는 TenantCheckpoint.TenantID가 빈 문자열일 때 반환됩니다.
	ErrEmptyTenantID = errors.New("crosswitness: tenant id is empty")
)

// SerializeCrossWitness는 TenantCheckpoint 배열을 결정론적 JSON으로 직렬화합니다.
//
// 규칙 (spec-A [0020-1], [0026]):
//   - tenantId 사전식(lexicographic) 정렬, 중복 거부.
//   - 각 항목 = (tenantId, seq, hash, signedAt) canonical JSON 객체.
//   - hash는 hex 소문자 64자 (canonical text 표현).
//   - signedAt은 RFC 3339 nanosecond UTC ("2026-05-18T12:00:00Z" 또는 ".123Z").
//   - 배열 전체는 공백 없는 JSON.
//   - 빈 입력(nil 또는 빈 slice) → "[]" 반환 (빈 배열도 유효 — 외부 anchoring 의무 동반).
//
// 본 함수가 산출한 바이트는 fold-in 엔트리 hash 입력에 그대로 포함됩니다
// (ComputeFoldInHash 참조).
func SerializeCrossWitness(witnesses []TenantCheckpoint) ([]byte, error) {
	if len(witnesses) == 0 {
		return []byte("[]"), nil
	}

	// 1. 입력 사본 + tenantId 사전식 정렬 (입력 mutation 금지 — 원칙 §11 불변성).
	sorted := make([]TenantCheckpoint, len(witnesses))
	copy(sorted, witnesses)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].TenantID < sorted[j].TenantID
	})

	// 2. validation: 빈 tenant id 거부 + 중복 거부.
	for i := range sorted {
		if sorted[i].TenantID == "" {
			return nil, ErrEmptyTenantID
		}
		if i > 0 && sorted[i].TenantID == sorted[i-1].TenantID {
			return nil, fmt.Errorf("%w: %q", ErrDuplicateTenant, sorted[i].TenantID)
		}
	}

	// 3. canonical JSON 직렬화 — 항목별 알파벳순 key, 공백 없는 출력.
	//    [0026] (iii): signedAt은 RFC 3339 Nano UTC로 정규화.
	//    [0026] (vi): hash는 hex 소문자 (외부 검증자가 byte 시퀀스를 재현 가능하도록).
	type itemJSON struct {
		Hash     string `json:"hash"`
		Seq      int64  `json:"seq"`
		SignedAt string `json:"signedAt"`
		TenantID string `json:"tenantId"`
	}

	items := make([]itemJSON, len(sorted))
	for i, w := range sorted {
		items[i] = itemJSON{
			Hash:     hashToHex(w.Hash),
			Seq:      w.Seq,
			SignedAt: w.SignedAt.UTC().Format(time.RFC3339Nano),
			TenantID: w.TenantID,
		}
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // canonical: HTML escape 비활성으로 결정론적 출력.
	if err := enc.Encode(items); err != nil {
		return nil, fmt.Errorf("crosswitness: marshal: %w", err)
	}

	// json.Encoder.Encode는 끝에 '\n'을 붙임 — canonical에서는 제거.
	out := bytes.TrimRight(buf.Bytes(), "\n")
	return out, nil
}

// ComputeFoldInHash는 fold-in 엔트리의 hash를 산출합니다 (spec-A [0019] 'hash' 필드).
//
//	hash = sha256( prevHash[32] ‖ payloadDigest[32] ‖ canonicalMeta ‖ canonicalCrossWitness )
//
// canonicalMeta는 호출자가 결정론적으로 직렬화한 entry 메타 (코어 audit
// canonicalMetaJSON과 동일 규칙). canonicalCrossWitness는 본 패키지의
// SerializeCrossWitness 결과 — 빈 witness이면 "[]"가 입력에 포함되어 결정론 유지.
//
// 본 함수는 hash 계산만 수행 — entry 추가 / 체인 저장 / scheduler trigger 는
// 호출 측(audit 도메인의 어댑터)에서 담당합니다 (E32 후속 통합).
func ComputeFoldInHash(prevHash, payloadDigest Hash, canonicalMeta []byte, witnesses []TenantCheckpoint) (Hash, error) {
	witnessJSON, err := SerializeCrossWitness(witnesses)
	if err != nil {
		return Hash{}, fmt.Errorf("crosswitness: serialize witness: %w", err)
	}

	h := sha256.New()
	h.Write(prevHash[:])
	h.Write(payloadDigest[:])
	h.Write(canonicalMeta)
	h.Write(witnessJSON)

	var out Hash
	copy(out[:], h.Sum(nil))
	return out, nil
}

// VerifyFoldIn은 외부 검증자가 fold-in 엔트리의 cross-witness 무결성을 확인하는 함수입니다
// (spec-A [0020-2]).
//
// 검증자는 다음을 입력으로 받습니다:
//   - prevHash, payloadDigest, canonicalMeta: fold-in 엔트리의 보유 필드
//   - witnesses: 검증자가 다른 테넌트 chain을 재계산해 얻은 (TenantID, Seq, Hash, SignedAt) 4-tuple
//   - expected: fold-in 엔트리에 기록된 hash 값
//
// 본 함수는 같은 입력으로 ComputeFoldInHash를 재계산하여 expected와 비교합니다.
// 한 건이라도 어긋나면 ErrCrossWitnessMismatch 반환 — 운영자의 사후 위조 검출.
//
// 본 함수는 witnesses 입력 순서에 영향받지 않습니다 (내부 정렬). 호출자가 다른 순서로
// 전달해도 같은 결과를 냅니다 (테스트 TestVerifyFoldIn_witness_순서_달라도_OK).
func VerifyFoldIn(prevHash, payloadDigest Hash, canonicalMeta []byte, witnesses []TenantCheckpoint, expected Hash) error {
	got, err := ComputeFoldInHash(prevHash, payloadDigest, canonicalMeta, witnesses)
	if err != nil {
		return fmt.Errorf("crosswitness: recompute: %w", err)
	}
	if got != expected {
		return ErrCrossWitnessMismatch
	}
	return nil
}

// hashToHex는 Hash를 hex 소문자 64자로 직렬화합니다 ([0026] (vi) canonical 텍스트 표현).
//
// encoding/hex.EncodeToString과 동치이나 의존을 최소화하기 위해 직접 구현 —
// hot path에서 alloc 최소화.
func hashToHex(h Hash) string {
	const hexchars = "0123456789abcdef"
	out := make([]byte, HashSize*2)
	for i, b := range h {
		out[i*2] = hexchars[b>>4]
		out[i*2+1] = hexchars[b&0x0F]
	}
	return string(out)
}
