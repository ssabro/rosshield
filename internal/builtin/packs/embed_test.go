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

func TestFilenameForPackKey(t *testing.T) {
	cases := []struct {
		packKey    string
		wantSubstr string
		wantErr    bool
	}{
		{packKey: "rosshield-cis-ubuntu-2404-1.0.0", wantSubstr: "cis-ubuntu-2404", wantErr: false},
		{packKey: "rosshield-ros2-jazzy-baseline-1.1.0", wantSubstr: "ros2-jazzy-baseline", wantErr: false},
		{packKey: "tenant-custom-pack-1.0", wantErr: true},               // non-rosshield prefix
		{packKey: "rosshield-nonexistent-pack-1.0.0", wantErr: true},     // archive missing
		{packKey: "rosshield", wantErr: true},                            // no version
	}
	for _, tc := range cases {
		t.Run(tc.packKey, func(t *testing.T) {
			got, err := builtinpacks.FilenameForPackKey(tc.packKey)
			if tc.wantErr {
				if err == nil {
					t.Errorf("got %q, want error", got)
				}
				return
			}
			if err != nil {
				t.Skipf("FilenameForPackKey: %v (likely _archives empty — run 'make pack-archive')", err)
			}
			if !contains(got, tc.wantSubstr) {
				t.Errorf("got %q, want substring %q", got, tc.wantSubstr)
			}
		})
	}
}

func TestSelftestYAML(t *testing.T) {
	filename, err := builtinpacks.FilenameForPackKey("rosshield-cis-ubuntu-2404-1.0.0")
	if err != nil {
		t.Skipf("no built-in packs embedded: %v", err)
	}
	// 1.5.1은 selftest 보유 (변환 stage A에서 자동 변환된 fixture).
	yaml, err := builtinpacks.SelftestYAML(filename, "1.5.1")
	if err != nil {
		t.Fatalf("SelftestYAML(1.5.1): %v", err)
	}
	if len(yaml) == 0 {
		t.Error("returned empty selftest yaml")
	}
	// degraded check (selftest 부재) — ErrSelftestNotFound 회귀.
	_, err = builtinpacks.SelftestYAML(filename, "nonexistent-check-id-9999")
	if err == nil {
		t.Error("expected ErrSelftestNotFound, got nil")
	}
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
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
