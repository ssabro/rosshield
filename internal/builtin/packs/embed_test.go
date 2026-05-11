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

func TestBuiltinsResultIsConsistent(t *testing.T) {
	packs, err := builtinpacks.Builtins()
	if err != nil {
		t.Fatalf("Builtins: %v", err)
	}
	for _, p := range packs {
		if len(p.TarGz) == 0 {
			t.Errorf("pack %q: empty TarGz", p.Filename)
		}
		if len(p.PublicKey) != ed25519.PublicKeySize {
			t.Errorf("pack %q: PublicKey size %d, want %d", p.Filename, len(p.PublicKey), ed25519.PublicKeySize)
		}
		if p.SignerKeyID != builtinpacks.DevSignerKeyID {
			t.Errorf("pack %q: SignerKeyID = %q, want %q", p.Filename, p.SignerKeyID, builtinpacks.DevSignerKeyID)
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

func TestDevSignerPublicKeyHexValid(t *testing.T) {
	packs, err := builtinpacks.Builtins()
	if err != nil {
		t.Fatalf("Builtins: %v", err)
	}
	if len(packs) == 0 {
		t.Skip("no packs embedded")
	}
	encoded := hex.EncodeToString(packs[0].PublicKey)
	if len(encoded) != ed25519.PublicKeySize*2 {
		t.Errorf("encoded pubKey hex len %d, want %d", len(encoded), ed25519.PublicKeySize*2)
	}
}
