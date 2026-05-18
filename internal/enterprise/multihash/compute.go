//go:build rosshield_enterprise

// compute.go — B-1 multi-hash evidence 산출 본체 (enterprise edition).
//
// 본 파일은 evidence 바이트 열에 대해 다음을 동시에 산출합니다:
//
//   - 전체 algorithm hash (sha256 / blake3 / 둘 다)
//   - JSONPath 단위 sub-hash (각 path 위치 값을 canonical JSON으로 직렬화 후 hash)
//   - line 단위 sub-hash (텍스트 evidence: 개행 분리, 마지막 빈 line은 산출 안 함)
//
// 결정론 (spec 의도):
//   - 같은 evidence + 같은 Option → 같은 MultiHash (sub-hash 정렬 포함).
//   - SubHashes 슬라이스는 `Path` 사전식 오름차순 정렬 (외부 검증 일관성).
//
// 알고리즘 선택 정책:
//   - 코어 verify CLI: sha256 단일 (Compute 결과의 Algorithms[AlgoSHA256]만 사용).
//   - enterprise verify CLI: sha256 + blake3 cross-check + sub-hash partial verify
//     (Verify 함수 참조).
//
// 참조:
//   - docs/design/notes/phase7-public-transition-design.md §6.3 D8-B1
//   - docs/design/13-patent-strategy.md §13.3 청구권 B-1

package multihash

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"

	"lukechampine.com/blake3"
)

// Algorithm은 multi-hash가 지원하는 hash 알고리즘 식별자입니다.
type Algorithm string

const (
	// AlgoSHA256은 SHA-256 (코어 호환). 코어 verify CLI가 단독으로 사용.
	AlgoSHA256 Algorithm = "sha256"
	// AlgoBLAKE3은 BLAKE3 256-bit. enterprise verify CLI가 sha256과 cross-check.
	AlgoBLAKE3 Algorithm = "blake3"
)

// SubHashScheme은 sub-hash 식별자 prefix (Path 직렬화 규칙).
const (
	subHashSchemeJSONPath = "jsonpath:" // 예: "jsonpath:$.foo.bar"
	subHashSchemeLine     = "line:"     // 예: "line:3" (1-based)
)

// 오류 정의.
var (
	// ErrUnsupportedAlgorithm은 Algorithms에 본 패키지가 모르는 알고리즘이 포함될 때 반환됩니다.
	ErrUnsupportedAlgorithm = errors.New("multihash: unsupported algorithm")

	// ErrSHA256Mismatch는 Verify 시 expected.Algorithms[AlgoSHA256]와 재계산 결과가 어긋날 때 반환됩니다.
	ErrSHA256Mismatch = errors.New("multihash: sha256 mismatch")

	// ErrBLAKE3Mismatch는 Verify 시 expected.Algorithms[AlgoBLAKE3]와 재계산 결과가 어긋날 때 반환됩니다.
	ErrBLAKE3Mismatch = errors.New("multihash: blake3 mismatch")

	// ErrSubHashMismatch는 Verify 시 expected.SubHashes 중 일부가 재계산 결과와 어긋날 때
	// 반환됩니다. Path는 fmt.Errorf로 wrap됨.
	ErrSubHashMismatch = errors.New("multihash: sub-hash mismatch")

	// ErrEvidenceSizeMismatch는 Verify 시 expected.EvidenceSize와 입력 evidence 길이가 다를 때 반환됩니다.
	ErrEvidenceSizeMismatch = errors.New("multihash: evidence size mismatch")
)

// Option은 Compute / Verify가 어떤 algorithm·sub-hash 모드로 동작할지 제어합니다.
type Option struct {
	// Algorithms는 산출할 전체 algorithm 집합입니다. 빈 슬라이스이면 sha256 단독으로 처리.
	// 같은 algorithm이 중복 지정되어도 한 번만 산출됩니다.
	Algorithms []Algorithm

	// JSONPaths는 sub-hash를 산출할 JSONPath 식 목록입니다. 빈 슬라이스이면 jsonpath sub-hash 없음.
	// evidence가 valid JSON이 아니면 Compute는 ErrInvalidJSONPath wrap 반환.
	JSONPaths []string

	// LineHash가 true이면 evidence를 '\n'으로 분리하여 각 line에 대해 line:N sub-hash를 산출합니다 (1-based).
	// 마지막 '\n' 뒤의 빈 fragment는 산출 대상 아닙니다 (text 표준 동작).
	// JSONPaths 와 동시 활성도 가능 — 둘 다 SubHashes에 누적됩니다.
	LineHash bool

	// SubHashAlgorithm은 sub-hash 단위에 적용할 algorithm (단일). 빈 값이면 AlgoSHA256.
	SubHashAlgorithm Algorithm
}

// SubHash는 evidence 내 한 단위(JSONPath value 또는 line)의 hash입니다.
//
// Path 표기:
//   - JSONPath: "jsonpath:$.foo.bar"
//   - Line:     "line:N" (1-based 정수)
type SubHash struct {
	// Path는 sub-hash 식별자 ("jsonpath:..." 또는 "line:N").
	Path string
	// Hash는 hex 소문자 (sha256 64자 / blake3 256-bit 64자).
	Hash string
	// Algo는 본 sub-hash를 산출한 algorithm (현재 단일 — Option.SubHashAlgorithm).
	Algo Algorithm
}

// MultiHash는 Compute 결과 — 전체 algorithm hash + sub-hash 묶음 + evidence 길이.
type MultiHash struct {
	// Algorithms는 algorithm 식별자 → hex 소문자 hash 매핑입니다.
	// 호출자가 Option.Algorithms를 비웠으면 sha256만 채워집니다.
	Algorithms map[Algorithm]string

	// SubHashes는 Path 사전식 오름차순 정렬된 sub-hash 슬라이스입니다.
	SubHashes []SubHash

	// EvidenceSize는 evidence 바이트 길이 (sanity 검증용).
	EvidenceSize int64
}

// Compute는 evidence + Option으로부터 MultiHash를 산출합니다.
//
// 빈 evidence(nil 또는 빈 slice)도 유효 입력 — sha256("") / blake3("")는 정의된 값을 가짐.
// 단, LineHash가 활성화이고 evidence가 빈 슬라이스이면 line sub-hash는 0건.
//
// 결정론: 같은 입력 → 같은 출력 (SubHashes 정렬 포함). 호출자가 같은 Option 슬라이스를
// 다른 순서로 넘겨도 결과 SubHashes는 같은 정렬 순서가 됩니다.
func Compute(evidence []byte, opt Option) (MultiHash, error) {
	algos, err := normalizeAlgorithms(opt.Algorithms)
	if err != nil {
		return MultiHash{}, err
	}
	subAlgo := opt.SubHashAlgorithm
	if subAlgo == "" {
		subAlgo = AlgoSHA256
	}
	if _, ok := supportedAlgorithms()[subAlgo]; !ok {
		return MultiHash{}, fmt.Errorf("%w: sub-hash algorithm %q", ErrUnsupportedAlgorithm, subAlgo)
	}

	out := MultiHash{
		Algorithms:   make(map[Algorithm]string, len(algos)),
		SubHashes:    nil,
		EvidenceSize: int64(len(evidence)),
	}

	for _, a := range algos {
		h, hashErr := hashBytes(evidence, a)
		if hashErr != nil {
			return MultiHash{}, hashErr
		}
		out.Algorithms[a] = h
	}

	subHashes := make([]SubHash, 0)

	// JSONPath sub-hash.
	for _, p := range opt.JSONPaths {
		raw, extractErr := extractByPath(evidence, p)
		if extractErr != nil {
			return MultiHash{}, fmt.Errorf("multihash: jsonpath %q: %w", p, extractErr)
		}
		h, hashErr := hashBytes(raw, subAlgo)
		if hashErr != nil {
			return MultiHash{}, hashErr
		}
		subHashes = append(subHashes, SubHash{
			Path: subHashSchemeJSONPath + p,
			Hash: h,
			Algo: subAlgo,
		})
	}

	// Line sub-hash.
	if opt.LineHash {
		lines := splitLines(evidence)
		for i, ln := range lines {
			h, hashErr := hashBytes(ln, subAlgo)
			if hashErr != nil {
				return MultiHash{}, hashErr
			}
			subHashes = append(subHashes, SubHash{
				Path: fmt.Sprintf("%s%d", subHashSchemeLine, i+1),
				Hash: h,
				Algo: subAlgo,
			})
		}
	}

	// Path 사전식 정렬.
	sort.SliceStable(subHashes, func(i, j int) bool {
		return subHashes[i].Path < subHashes[j].Path
	})
	out.SubHashes = subHashes

	return out, nil
}

// supportedAlgorithms는 본 패키지가 지원하는 algorithm 집합을 반환합니다.
func supportedAlgorithms() map[Algorithm]struct{} {
	return map[Algorithm]struct{}{
		AlgoSHA256: {},
		AlgoBLAKE3: {},
	}
}

// normalizeAlgorithms는 입력 Algorithms slice를 검증·중복 제거·정렬합니다.
// 빈 입력이면 sha256 단독을 반환합니다.
func normalizeAlgorithms(in []Algorithm) ([]Algorithm, error) {
	supported := supportedAlgorithms()
	if len(in) == 0 {
		return []Algorithm{AlgoSHA256}, nil
	}
	seen := make(map[Algorithm]struct{}, len(in))
	dedup := make([]Algorithm, 0, len(in))
	for _, a := range in {
		if _, ok := supported[a]; !ok {
			return nil, fmt.Errorf("%w: %q", ErrUnsupportedAlgorithm, a)
		}
		if _, dup := seen[a]; dup {
			continue
		}
		seen[a] = struct{}{}
		dedup = append(dedup, a)
	}
	sort.Slice(dedup, func(i, j int) bool { return dedup[i] < dedup[j] })
	return dedup, nil
}

// hashBytes는 algorithm에 따라 data의 hash를 hex 소문자로 반환합니다.
func hashBytes(data []byte, algo Algorithm) (string, error) {
	switch algo {
	case AlgoSHA256:
		sum := sha256.Sum256(data)
		return hex.EncodeToString(sum[:]), nil
	case AlgoBLAKE3:
		sum := blake3.Sum256(data)
		return hex.EncodeToString(sum[:]), nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnsupportedAlgorithm, algo)
	}
}

// splitLines는 evidence를 '\n'으로 분리합니다. trailing '\n' 뒤의 빈 fragment는 제거.
// CRLF는 line의 trailing '\r'로 보존됩니다 (호출자가 normalize 필요시 직접 수행).
func splitLines(evidence []byte) [][]byte {
	if len(evidence) == 0 {
		return nil
	}
	parts := bytes.Split(evidence, []byte{'\n'})
	// trailing '\n'이 있으면 마지막은 빈 슬라이스 — 제거.
	if len(parts) > 0 && len(parts[len(parts)-1]) == 0 {
		parts = parts[:len(parts)-1]
	}
	return parts
}
