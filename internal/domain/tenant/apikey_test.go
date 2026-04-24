package tenant_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ssabro/rosshield/internal/domain/tenant"
)

func TestGenerateApiKeyTokenFormat(t *testing.T) {
	t.Parallel()

	token, prefix, err := tenant.GenerateApiKeyToken()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if len(token) != 40 {
		t.Errorf("token length = %d, want 40", len(token))
	}
	if !strings.HasPrefix(token, "fg_live_") {
		t.Errorf("token does not start with fg_live_: %q", token)
	}
	if len(prefix) != 12 {
		t.Errorf("prefix length = %d, want 12", len(prefix))
	}
	if prefix != token[:12] {
		t.Errorf("prefix = %q, want token[:12] = %q", prefix, token[:12])
	}
}

func TestGenerateApiKeyTokenRandomness(t *testing.T) {
	t.Parallel()

	const N = 100
	seen := make(map[string]struct{}, N)
	for i := 0; i < N; i++ {
		token, _, err := tenant.GenerateApiKeyToken()
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		if _, ok := seen[token]; ok {
			t.Fatalf("duplicate token within %d samples: %q", N, token)
		}
		seen[token] = struct{}{}
	}
}

func TestExtractApiKeyPrefixHappyAndError(t *testing.T) {
	t.Parallel()

	token, prefix, err := tenant.GenerateApiKeyToken()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	gotPrefix, err := tenant.ExtractApiKeyPrefix(token)
	if err != nil {
		t.Errorf("Extract(valid): %v", err)
	}
	if gotPrefix != prefix {
		t.Errorf("prefix = %q, want %q", gotPrefix, prefix)
	}

	cases := map[string]string{
		"empty":          "",
		"wrong prefix":   "ak_live_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"too short":      "fg_live_TOO_SHORT",
		"missing prefix": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
	}
	for name, bad := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := tenant.ExtractApiKeyPrefix(bad)
			if !errors.Is(err, tenant.ErrInvalidApiKeyFormat) {
				t.Errorf("Extract(%q): err = %v, want ErrInvalidApiKeyFormat", bad, err)
			}
		})
	}
}
