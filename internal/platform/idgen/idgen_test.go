package idgen_test

import (
	"strings"
	"sync"
	"testing"

	"github.com/ssabro/rosshield/internal/platform/idgen"
)

// crockfordAlphabet은 ULID Crockford base32 알파벳입니다 (I, L, O, U 제외).
const crockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

func TestIDGenPrefixAndLength(t *testing.T) {
	t.Parallel()

	g := idgen.NewULID()

	cases := []string{"ro", "ss", "au", "tn", "fl"}
	for _, prefix := range cases {
		got := g.New(prefix)

		want := prefix + "_"
		if !strings.HasPrefix(got, want) {
			t.Errorf("New(%q) = %q, want prefix %q", prefix, got, want)
			continue
		}

		body := strings.TrimPrefix(got, want)
		if len(body) != 26 {
			t.Errorf("New(%q) body length = %d, want 26 (id=%q)", prefix, len(body), got)
		}
	}
}

func TestIDGenUsesCrockfordAlphabet(t *testing.T) {
	t.Parallel()

	g := idgen.NewULID()
	id := g.New("ro")
	body := strings.TrimPrefix(id, "ro_")

	for i, r := range body {
		if !strings.ContainsRune(crockfordAlphabet, r) {
			t.Errorf("char at %d = %q, not in Crockford alphabet (id=%q)", i, r, id)
		}
	}
}

func TestIDGenWithoutPrefix(t *testing.T) {
	t.Parallel()

	g := idgen.NewULID()
	got := g.New("")

	if len(got) != 26 {
		t.Errorf("New(\"\") length = %d, want 26 (id=%q)", len(got), got)
	}
	if strings.Contains(got, "_") {
		t.Errorf("New(\"\") = %q, want no underscore", got)
	}
}

func TestIDGenUniqueAcrossCalls(t *testing.T) {
	t.Parallel()

	g := idgen.NewULID()
	const n = 1000

	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		id := g.New("ro")
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate id at iter %d: %q", i, id)
		}
		seen[id] = struct{}{}
	}
}

func TestIDGenIsConcurrencySafe(t *testing.T) {
	t.Parallel()

	g := idgen.NewULID()

	const goroutines = 50
	const iterations = 100

	var mu sync.Mutex
	seen := make(map[string]struct{}, goroutines*iterations)

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				id := g.New("ro")
				mu.Lock()
				seen[id] = struct{}{}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if got, want := len(seen), goroutines*iterations; got != want {
		t.Errorf("unique ids = %d, want %d (collisions = %d)", got, want, want-got)
	}
}
