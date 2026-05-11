// Package builtinpacks는 binary에 embed된 built-in 벤치마크 팩 자산을 제공합니다 (E12 Stage E).
//
// First-boot seed loader가 본 패키지의 Builtins()를 호출해 dev signer 신뢰 하의
// pack을 자동 InstallPack한다. 사용자는 별도 pack upload 없이 즉시 스캔 가능.
//
// 운영 모델:
//   - dev signer (본 패키지에 pubKey 상수): 본 repo에서 빌드된 binary가 신뢰
//     scripts/dev-pack-signer.pub.hex와 동기화 — 키 회전 시 본 상수도 갱신
//   - release signer (별 epic): GitHub Actions secret으로 release 시 별도 archive
//     production binary는 release pubKey도 trust bundle에 포함 (별 commit)
//
// 빌드 흐름:
//   make pack-archive  → packs/*.tar.gz 생성 + cp internal/builtin/packs/_archives/
//   go build           → //go:embed가 _archives/*.tar.gz를 binary에 포함
//
// _archives/ 디렉터리는 _ prefix로 Go 패키지 스캔에서 제외 — 같은 디렉터리에 두지만
// 별도 source tree로 취급. .gitignore가 _archives/*.tar.gz를 제외 — build artifact.
package builtinpacks

import (
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
)

// DevSignerKeyID는 dev signer의 keyID 식별자입니다.
//
// benchmark.InstallPack의 signerKeyID 인자로 전달 — audit log·pack metadata에 기록.
const DevSignerKeyID = "rosshield-dev-pack-signer-2026"

// devSignerPublicKeyHex는 scripts/dev-pack-signer.pub.hex와 동기화된 hex 문자열입니다.
//
// 키 회전 시 두 위치 모두 갱신:
//   1. bin/pack-tools keygen -out scripts/dev-pack-signer.key -pub-out scripts/dev-pack-signer.pub.hex -force
//   2. 본 상수를 새 .pub.hex 내용으로 교체
const devSignerPublicKeyHex = "f074a51dac239cc2496b927b1d8a363cd2ca8db59b6e29fec6123bc4929d6478"

//go:embed _archives/*.tar.gz
var archives embed.FS

// SeedPack은 first-boot seed loader가 InstallPack에 전달할 단일 built-in 팩입니다.
type SeedPack struct {
	// Filename은 packs/<filename>의 원본 파일명입니다 (예: "cis-ubuntu-2404.tar.gz").
	// 디버그·로그용 — InstallPack은 Bytes만 사용.
	Filename string

	// TarGz는 archive 내용(Ed25519 서명 검증된 MANIFEST + SIGNATURE 포함)입니다.
	TarGz []byte

	// PublicKey는 archive 서명 검증에 쓰는 32-byte ed25519 public key입니다.
	// 본 Stage는 모든 built-in pack이 dev signer로 서명 → 동일 키 반복.
	PublicKey []byte

	// SignerKeyID는 InstallPack(signerKeyID) 인자로 전달됩니다.
	SignerKeyID string
}

// Builtins는 binary에 embed된 모든 built-in pack을 반환합니다.
//
// 결정성: 파일명 알파벳순 정렬. 빈 결과면 nil + ErrNoBuiltinsEmbedded
// (build flow에서 make pack-archive 누락이거나, dist 누락 빌드).
func Builtins() ([]SeedPack, error) {
	pubKey, err := hex.DecodeString(devSignerPublicKeyHex)
	if err != nil {
		return nil, fmt.Errorf("builtinpacks: decode dev signer pubkey: %w", err)
	}

	entries, err := fs.ReadDir(archives, "_archives")
	if err != nil {
		return nil, fmt.Errorf("builtinpacks: read _archives: %w", err)
	}

	var out []SeedPack
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".tar.gz") {
			continue
		}
		data, err := fs.ReadFile(archives, path.Join("_archives", e.Name()))
		if err != nil {
			return nil, fmt.Errorf("builtinpacks: read %s: %w", e.Name(), err)
		}
		out = append(out, SeedPack{
			Filename:    e.Name(),
			TarGz:       data,
			PublicKey:   pubKey,
			SignerKeyID: DevSignerKeyID,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Filename < out[j].Filename })

	if len(out) == 0 {
		return nil, ErrNoBuiltinsEmbedded
	}
	return out, nil
}

// ErrNoBuiltinsEmbedded는 _archives/ 가 비었을 때 반환됩니다.
//
// 원인: make pack-archive 미실행, 또는 .gitignore 회피 빌드.
// bootstrap은 본 에러를 warn 로그로 처리하고 seed 단계 skip — 운영자가 명시적으로
// pack upload 하면 됨 (degraded mode, 비-fatal).
var ErrNoBuiltinsEmbedded = errors.New("builtinpacks: no archives embedded (run 'make pack-archive' before 'go build')")
