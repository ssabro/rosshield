//go:build rosshield_enterprise

// verify.go — B-1 multi-hash evidence 검증 (enterprise edition).
//
// 본 파일은 expected MultiHash가 주어졌을 때 evidence를 재계산하여 다음을 확인합니다:
//
//   - 전체 algorithm hash (expected.Algorithms의 모든 key)
//   - sub-hash 무결성 (expected.SubHashes의 각 항목, 같은 Path → 같은 Hash)
//   - evidence size (expected.EvidenceSize와 일치)
//
// 알고리즘 선택 (코어 vs enterprise):
//   - 코어 verify CLI: sha256 단일 — VerifyMode=ModeCoreSHA256만 호출, sub-hash 검증 생략.
//   - enterprise verify CLI: sha256 + blake3 cross-check + sub-hash partial verify.
//
// 검증 옵션은 Option 구조체를 재사용 (Compute와 같은 구조 — opt.JSONPaths /
// opt.LineHash가 expected가 어떤 sub-hash를 가진다고 가정하는지 결정).

package multihash

import (
	"errors"
	"fmt"
)

// VerifyMode는 Verify가 어떤 깊이로 검증할지 결정합니다.
type VerifyMode int

const (
	// ModeCoreSHA256은 sha256 단일 hash + evidence size만 검증합니다 (코어 verify CLI).
	// expected.Algorithms[AlgoSHA256]가 없으면 ErrSHA256Mismatch (선언 자체 어긋남).
	ModeCoreSHA256 VerifyMode = iota

	// ModeEnterpriseFull은 expected가 가진 모든 algorithm + 모든 sub-hash + size를 검증합니다.
	// expected.Algorithms[AlgoBLAKE3]가 있으면 ErrBLAKE3Mismatch도 강제.
	ModeEnterpriseFull
)

// Verify는 evidence를 재계산하여 expected MultiHash와 일치하는지 확인합니다.
//
// opt는 Compute에 전달된 것과 같은 Option이어야 합니다 (JSONPaths · LineHash ·
// SubHashAlgorithm). mode가 ModeCoreSHA256이면 sha256만 검증 (sub-hash 무시).
//
// 반환 sentinel:
//   - ErrEvidenceSizeMismatch : evidence 크기 다름.
//   - ErrSHA256Mismatch      : sha256 hash 다름.
//   - ErrBLAKE3Mismatch      : blake3 hash 다름 (enterprise mode + expected에 blake3 있을 때).
//   - ErrSubHashMismatch     : sub-hash 일부 다름 (Path가 wrap된 error).
//   - ErrUnsupportedAlgorithm: expected.Algorithms에 미지원 algorithm.
func Verify(evidence []byte, expected MultiHash, opt Option, mode VerifyMode) error {
	// 1. evidence size pre-check (cheap).
	if expected.EvidenceSize != int64(len(evidence)) {
		return fmt.Errorf("%w: expected=%d actual=%d", ErrEvidenceSizeMismatch, expected.EvidenceSize, len(evidence))
	}

	// 2. 재계산용 Option 구성.
	//    mode == ModeCoreSHA256 인 경우는 sha256 단일 + sub-hash 0 옵션으로 축소.
	recomputeOpt := opt
	if mode == ModeCoreSHA256 {
		recomputeOpt = Option{
			Algorithms:       []Algorithm{AlgoSHA256},
			JSONPaths:        nil,
			LineHash:         false,
			SubHashAlgorithm: AlgoSHA256,
		}
	} else {
		// ModeEnterpriseFull: expected가 가진 algorithm 집합을 재계산 대상으로.
		if len(expected.Algorithms) > 0 {
			algos := make([]Algorithm, 0, len(expected.Algorithms))
			for a := range expected.Algorithms {
				algos = append(algos, a)
			}
			recomputeOpt.Algorithms = algos
		}
	}

	recomputed, err := Compute(evidence, recomputeOpt)
	if err != nil {
		return fmt.Errorf("multihash: recompute: %w", err)
	}

	// 3. sha256 (양 mode 공통 강제).
	expSHA, hasSHA := expected.Algorithms[AlgoSHA256]
	if !hasSHA {
		return fmt.Errorf("%w: expected.Algorithms missing sha256", ErrSHA256Mismatch)
	}
	if recomputed.Algorithms[AlgoSHA256] != expSHA {
		return fmt.Errorf("%w: expected=%s actual=%s", ErrSHA256Mismatch, expSHA, recomputed.Algorithms[AlgoSHA256])
	}

	if mode == ModeCoreSHA256 {
		// 코어 모드는 sub-hash · blake3 미검증.
		return nil
	}

	// 4. enterprise mode: blake3 (있으면 강제).
	if expBlake, has := expected.Algorithms[AlgoBLAKE3]; has {
		if recomputed.Algorithms[AlgoBLAKE3] != expBlake {
			return fmt.Errorf("%w: expected=%s actual=%s", ErrBLAKE3Mismatch, expBlake, recomputed.Algorithms[AlgoBLAKE3])
		}
	}

	// 5. enterprise mode: sub-hash partial verify.
	//    expected.SubHashes에 있는 항목 모두가 recomputed에 같은 Hash로 존재해야 함.
	if len(expected.SubHashes) > 0 {
		index := make(map[string]SubHash, len(recomputed.SubHashes))
		for _, s := range recomputed.SubHashes {
			index[s.Path] = s
		}
		for _, want := range expected.SubHashes {
			got, ok := index[want.Path]
			if !ok {
				return fmt.Errorf("%w: path %q missing in recompute", ErrSubHashMismatch, want.Path)
			}
			if got.Hash != want.Hash {
				return fmt.Errorf("%w: path %q expected=%s actual=%s", ErrSubHashMismatch, want.Path, want.Hash, got.Hash)
			}
			if want.Algo != "" && got.Algo != want.Algo {
				return fmt.Errorf("%w: path %q algo expected=%q actual=%q", ErrSubHashMismatch, want.Path, want.Algo, got.Algo)
			}
		}
	}

	return nil
}

// IsMismatch는 err가 multi-hash mismatch sentinel 계열인지 빠르게 판정합니다 (호출 측 편의).
func IsMismatch(err error) bool {
	return errors.Is(err, ErrSHA256Mismatch) ||
		errors.Is(err, ErrBLAKE3Mismatch) ||
		errors.Is(err, ErrSubHashMismatch) ||
		errors.Is(err, ErrEvidenceSizeMismatch)
}
