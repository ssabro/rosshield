// Package builtinpacks는 binary에 embed된 built-in 벤치마크 팩 자산을 제공합니다 (E12 Stage E).
//
// First-boot seed loader가 본 패키지의 Builtins()를 호출해 trust bundle 안의 pubKey
// 한 개로 InstallPack한다. 사용자는 별도 pack upload 없이 즉시 스캔 가능.
//
// 운영 모델 (dev + release 두 trust):
//   - dev signer (DevSignerKeyID, scripts/dev-pack-signer.pub.hex와 동기화):
//     본 repo에서 빌드된 binary가 신뢰. 키 회전 시 본 상수도 갱신.
//   - release signer (ReleaseSignerKeyID, scripts/release-pack-signer.pub.hex):
//     GitHub Actions release-pipeline workflow가 ROSSHIELD_PACK_SIGNER_KEY secret으로
//     archive — production binary는 dev + release 두 trust 모두 보유.
//
// caller(seed_packs.go)는 SeedPack.TrustBundle을 차례로 시도 — 첫 통과 키로 install.
// 모두 ErrSignatureInvalid면 archive가 어떤 trust로도 검증 안 되는 의심 archive.
//
// 빌드 흐름:
//   make pack-archive  → packs/*.tar.gz 생성 (dev signer) + cp internal/builtin/packs/_archives/
//   release-pipeline   → ROSSHIELD_PACK_SIGNER_KEY → packs/*.tar.gz (release signer) + cp _archives/
//   go build           → //go:embed가 _archives/*.tar.gz를 binary에 포함
//
// _archives/ 디렉터리는 _ prefix로 Go 패키지 스캔에서 제외 — 같은 디렉터리에 두지만
// 별도 source tree로 취급. .gitignore가 _archives/*.tar.gz를 제외 — build artifact.
package builtinpacks

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"
)

// SignerKeyID 식별자.
const (
	DevSignerKeyID     = "rosshield-dev-pack-signer-2026"
	ReleaseSignerKeyID = "rosshield-release-pack-signer-2026"
)

// Public key hex 상수 — scripts/{dev,release}-pack-signer.pub.hex와 동기화.
//
// 키 회전 시 두 위치 모두 갱신:
//   1. bin/pack-tools keygen -out scripts/<role>-pack-signer.key -pub-out scripts/<role>-pack-signer.pub.hex -force
//   2. 본 상수를 새 .pub.hex 내용으로 교체
const (
	devSignerPublicKeyHex     = "f074a51dac239cc2496b927b1d8a363cd2ca8db59b6e29fec6123bc4929d6478"
	releaseSignerPublicKeyHex = "482ea7964e48fee1e25ff0a907cb3861c9d2a2d1688f935cb8d643d6920760fb"
)

//go:embed _archives/*.tar.gz
var archives embed.FS

// TrustEntry는 한 개 신뢰 키입니다.
type TrustEntry struct {
	PublicKey   []byte // 32 bytes ed25519 public key
	SignerKeyID string // benchmark.InstallPack의 signerKeyID 인자에 전달
}

// SeedPack은 first-boot seed loader가 InstallPack에 전달할 단일 built-in 팩입니다.
type SeedPack struct {
	// Filename은 packs/<filename>의 원본 파일명입니다 (예: "cis-ubuntu-2404.tar.gz").
	// 디버그·로그용 — InstallPack은 TarGz만 사용.
	Filename string

	// TarGz는 archive 내용(MANIFEST + SIGNATURE 포함)입니다.
	TarGz []byte

	// TrustBundle은 archive 검증을 시도할 trust 키 list입니다.
	// 호출 순서: dev → release. 첫 통과 키로 install + 종료.
	TrustBundle []TrustEntry
}

// Builtins는 binary에 embed된 모든 built-in pack을 반환합니다.
//
// 결정성: 파일명 알파벳순 정렬. 빈 결과면 nil + ErrNoBuiltinsEmbedded
// (build flow에서 make pack-archive 누락이거나, dist 누락 빌드).
func Builtins() ([]SeedPack, error) {
	devPub, err := hex.DecodeString(devSignerPublicKeyHex)
	if err != nil {
		return nil, fmt.Errorf("builtinpacks: decode dev signer pubkey: %w", err)
	}
	relPub, err := hex.DecodeString(releaseSignerPublicKeyHex)
	if err != nil {
		return nil, fmt.Errorf("builtinpacks: decode release signer pubkey: %w", err)
	}
	bundle := []TrustEntry{
		{PublicKey: devPub, SignerKeyID: DevSignerKeyID},
		{PublicKey: relPub, SignerKeyID: ReleaseSignerKeyID},
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
			TrustBundle: bundle,
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

// ErrSelftestNotFound는 archive에 해당 checkId의 selftest yaml이 없을 때 반환됩니다.
//
// pack converter가 selftest를 만들 수 있는 check만 selftest 디렉터리에 출력 — degraded
// (manual·no-marker) check는 selftest 미보유.
var ErrSelftestNotFound = errors.New("builtinpacks: selftest yaml not found")

// SelftestYAML은 archive의 selftest/<checkId>.yaml raw bytes를 반환합니다.
//
// builtin pack scope 한정 — tenant 임포트 pack은 InstallPack 시점에 selftest 정보가
// 버려짐(현재 도메인 모델). 호출자가 yaml.Unmarshal로 cases 추출.
//
// packFilename은 SeedPack.Filename(예: "cis-ubuntu-2404.tar.gz"). checkId는 packMeta
// 내 식별자(예: "1.1.1.1"). 두 값이 정확히 일치해야 추출 — pack converter가 만든
// "selftest/<checkId>.yaml" 경로를 in-memory tar walk으로 찾음.
func SelftestYAML(packFilename, checkID string) ([]byte, error) {
	if packFilename == "" || checkID == "" {
		return nil, fmt.Errorf("builtinpacks: SelftestYAML requires non-empty packFilename and checkID")
	}
	data, err := fs.ReadFile(archives, path.Join("_archives", packFilename))
	if err != nil {
		return nil, fmt.Errorf("builtinpacks: read %s: %w", packFilename, err)
	}
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("builtinpacks: gzip %s: %w", packFilename, err)
	}
	defer func() { _ = gr.Close() }()
	tr := tar.NewReader(gr)
	target := path.Join("selftest", checkID+".yaml")
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("builtinpacks: tar walk: %w", err)
		}
		if h.Name == target {
			buf, err := io.ReadAll(tr)
			if err != nil {
				return nil, fmt.Errorf("builtinpacks: read selftest entry: %w", err)
			}
			return buf, nil
		}
	}
	return nil, ErrSelftestNotFound
}

// FilenameForPackKey는 packKey(예: "rosshield-cis-ubuntu-2404-1.0.0")로 builtin pack
// archive 파일명("cis-ubuntu-2404.tar.gz" 등)을 매핑합니다.
//
// 실제 매핑은 archive 안 pack.yaml의 metadata.name과 packKey의 매핑 규칙에 의존하지만,
// 본 helper는 archive 파일명에서 vendor prefix와 version suffix를 stripping해 비교하는
// 단순 휴리스틱: builtin packKey "rosshield-<name>-<version>" → archive "<name>.tar.gz" 매핑 시도.
//
// 매칭 실패 시 빈 string + ErrSelftestNotFound (caller가 unsupported로 처리).
func FilenameForPackKey(packKey string) (string, error) {
	if !strings.HasPrefix(packKey, "rosshield-") {
		return "", ErrSelftestNotFound
	}
	stripped := strings.TrimPrefix(packKey, "rosshield-")
	// version suffix는 마지막 dash 뒤 — 최소 1번의 dash 필요(name-version 분리).
	idx := strings.LastIndex(stripped, "-")
	if idx <= 0 {
		return "", ErrSelftestNotFound
	}
	name := stripped[:idx]

	// archive 디렉터리에 <name>.tar.gz 존재 확인.
	candidate := name + ".tar.gz"
	entries, err := fs.ReadDir(archives, "_archives")
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if e.Name() == candidate {
			return candidate, nil
		}
	}
	return "", ErrSelftestNotFound
}
