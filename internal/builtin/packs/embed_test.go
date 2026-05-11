package builtinpacks_test

import (
	"crypto/ed25519"
	"encoding/hex"
	"sort"
	"testing"

	builtinpacks "github.com/ssabro/rosshield/internal/builtin/packs"
)

func TestBuiltinsReturnsEmbeddedPacks(t *testing.T) {
	packs, err := builtinpacks.Builtins()
	if err != nil {
		t.Fatalf("Builtins: unexpected error %v (run 'make pack-archive' first)", err)
	}
	if len(packs) < 2 {
		t.Fatalf("Builtins: got %d packs, want at least 2 (cis + ros2)", len(packs))
	}

	wantFilenames := []string{"cis-ubuntu-2404.tar.gz", "ros2-jazzy-baseline.tar.gz"}
	got := make([]string, len(packs))
	for i, p := range packs {
		got[i] = p.Filename
	}
	sort.Strings(got)
	for _, want := range wantFilenames {
		found := false
		for _, g := range got {
			if g == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Builtins: missing pack %q (got %v)", want, got)
		}
	}
}

func TestBuiltinsTrustBundleHasDevAndRelease(t *testing.T) {
	packs, err := builtinpacks.Builtins()
	if err != nil {
		t.Fatalf("Builtins: %v", err)
	}
	for _, p := range packs {
		if len(p.TarGz) == 0 {
			t.Errorf("pack %q: empty TarGz", p.Filename)
		}
		if len(p.TrustBundle) != 2 {
			t.Errorf("pack %q: TrustBundle len = %d, want 2 (dev + release)", p.Filename, len(p.TrustBundle))
			continue
		}
		// 순서: dev 먼저, release 다음 (caller가 dev 머신에서 dev signer로 archive한 경우 첫 시도 통과).
		if p.TrustBundle[0].SignerKeyID != builtinpacks.DevSignerKeyID {
			t.Errorf("TrustBundle[0].SignerKeyID = %q, want %q",
				p.TrustBundle[0].SignerKeyID, builtinpacks.DevSignerKeyID)
		}
		if p.TrustBundle[1].SignerKeyID != builtinpacks.ReleaseSignerKeyID {
			t.Errorf("TrustBundle[1].SignerKeyID = %q, want %q",
				p.TrustBundle[1].SignerKeyID, builtinpacks.ReleaseSignerKeyID)
		}
		for i, te := range p.TrustBundle {
			if len(te.PublicKey) != ed25519.PublicKeySize {
				t.Errorf("pack %q TrustBundle[%d]: PublicKey size %d, want %d",
					p.Filename, i, len(te.PublicKey), ed25519.PublicKeySize)
			}
		}
	}
}

func TestBuiltinsSortedByFilename(t *testing.T) {
	packs, err := builtinpacks.Builtins()
	if err != nil {
		t.Fatalf("Builtins: %v", err)
	}
	for i := 1; i < len(packs); i++ {
		if packs[i-1].Filename >= packs[i].Filename {
			t.Errorf("Builtins not sorted: %q >= %q", packs[i-1].Filename, packs[i].Filename)
		}
	}
}

func TestPublicKeyHexConstantsValid(t *testing.T) {
	packs, err := builtinpacks.Builtins()
	if err != nil {
		t.Fatalf("Builtins: %v", err)
	}
	if len(packs) == 0 {
		t.Skip("no packs embedded")
	}
	for i, te := range packs[0].TrustBundle {
		encoded := hex.EncodeToString(te.PublicKey)
		if len(encoded) != ed25519.PublicKeySize*2 {
			t.Errorf("TrustBundle[%d] hex len %d, want %d", i, len(encoded), ed25519.PublicKeySize*2)
		}
	}
}

func TestDevAndReleasePublicKeysAreDistinct(t *testing.T) {
	packs, err := builtinpacks.Builtins()
	if err != nil {
		t.Fatalf("Builtins: %v", err)
	}
	if len(packs) == 0 {
		t.Skip("no packs embedded")
	}
	tb := packs[0].TrustBundle
	if len(tb) < 2 {
		t.Skip("trust bundle has fewer than 2 entries")
	}
	if hex.EncodeToString(tb[0].PublicKey) == hex.EncodeToString(tb[1].PublicKey) {
		t.Error("dev signer pubKey == release signer pubKey (rotation gone wrong)")
	}
}
