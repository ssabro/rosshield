package benchmark

import (
	"crypto/ed25519"
	"fmt"
	"sort"
	"strings"
)

// LoadPackFromTar는 tar.gz 바이트와 publicKey로 Pack 메타 + 모든 Check를 검증·반환합니다.
//
// 검증된 Pack은 sqliterepo가 INSERT 합니다 (Stage E에서 추가).
// Stage B에서는 검증 + 메모리 Pack 구성까지.
func LoadPackFromTar(data []byte, publicKey ed25519.PublicKey) (Pack, error) {
	if len(publicKey) != ed25519.PublicKeySize {
		return Pack{}, fmt.Errorf("benchmark: publicKey size = %d, want %d", len(publicKey), ed25519.PublicKeySize)
	}

	arc, manifest, err := VerifyArchive(data, publicKey)
	if err != nil {
		return Pack{}, err
	}

	// pack.yaml 필수.
	packBytes, ok := arc.files[packYAMLFile]
	if !ok {
		return Pack{}, ErrMissingPackYAML
	}
	pack, err := ParsePackYAML(packBytes)
	if err != nil {
		return Pack{}, err
	}
	// schema 추가 검증 (jsonschema — type/pattern 강제).
	if err := ValidatePackYAMLBytes(packBytes); err != nil {
		return Pack{}, err
	}
	// MANIFEST의 PackKey와 pack.yaml의 PackKey 일치 강제.
	if manifest.PackKey != pack.PackKey {
		return Pack{}, fmt.Errorf("%w: manifest.packKey=%q != pack.yaml=%q",
			ErrSchemaViolation, manifest.PackKey, pack.PackKey)
	}

	// MANIFEST hash를 Pack에 채움.
	manifestBytes := arc.files[manifestFile]
	pack.ManifestHash = ManifestHashOf(manifestBytes)

	// checks/*.yaml 파싱 — manifest에 listed된 것만, ordered.
	checkPaths := make([]string, 0)
	for _, entry := range manifest.Files {
		if strings.HasPrefix(entry.Path, checksDir) && strings.HasSuffix(entry.Path, ".yaml") {
			checkPaths = append(checkPaths, entry.Path)
		}
	}
	sort.Strings(checkPaths)

	checks := make([]Check, 0, len(checkPaths))
	seenIDs := make(map[string]struct{}, len(checkPaths))
	for _, p := range checkPaths {
		body := arc.files[p]
		if err := ValidateCheckYAMLBytes(body); err != nil {
			return Pack{}, fmt.Errorf("benchmark: check %q: %w", p, err)
		}
		check, err := ParseCheckYAML(body)
		if err != nil {
			return Pack{}, fmt.Errorf("benchmark: check %q: %w", p, err)
		}
		if _, dup := seenIDs[check.CheckID]; dup {
			return Pack{}, fmt.Errorf("%w: %q", ErrDuplicateCheckID, check.CheckID)
		}
		seenIDs[check.CheckID] = struct{}{}
		checks = append(checks, check)
	}
	pack.Checks = checks

	return pack, nil
}
