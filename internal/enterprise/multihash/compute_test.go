//go:build rosshield_enterprise

// compute_test.go — Compute 단위 테스트.

package multihash

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"testing"

	"lukechampine.com/blake3"
)

// 알려진 입력의 sha256 / blake3 (외부 도구 cross-check 기준).
const (
	helloSHA256 = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
)

func TestCompute_default_sha256_only(t *testing.T) {
	mh, err := Compute([]byte("hello"), Option{})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if mh.Algorithms[AlgoSHA256] != helloSHA256 {
		t.Errorf("sha256 = %s, want %s", mh.Algorithms[AlgoSHA256], helloSHA256)
	}
	if _, has := mh.Algorithms[AlgoBLAKE3]; has {
		t.Errorf("blake3 should not be present in default mode")
	}
	if mh.EvidenceSize != 5 {
		t.Errorf("size = %d, want 5", mh.EvidenceSize)
	}
	if len(mh.SubHashes) != 0 {
		t.Errorf("sub-hashes should be empty, got %d", len(mh.SubHashes))
	}
}

func TestCompute_blake3_deterministic(t *testing.T) {
	mh, err := Compute([]byte("hello"), Option{
		Algorithms: []Algorithm{AlgoBLAKE3},
	})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	// blake3("hello")는 외부 도구와 cross-check.
	expected := blake3.Sum256([]byte("hello"))
	if mh.Algorithms[AlgoBLAKE3] != hex.EncodeToString(expected[:]) {
		t.Errorf("blake3 mismatch: got %s", mh.Algorithms[AlgoBLAKE3])
	}
	// blake3 단독 — sha256 부재.
	if _, has := mh.Algorithms[AlgoSHA256]; has {
		t.Errorf("sha256 unexpectedly present")
	}
}

func TestCompute_both_algorithms(t *testing.T) {
	ev := []byte("multi-hash evidence")
	mh, err := Compute(ev, Option{
		Algorithms: []Algorithm{AlgoSHA256, AlgoBLAKE3},
	})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if len(mh.Algorithms) != 2 {
		t.Fatalf("expected 2 algorithms, got %d", len(mh.Algorithms))
	}
	gotSHA := mh.Algorithms[AlgoSHA256]
	gotB3 := mh.Algorithms[AlgoBLAKE3]
	wantSHA := sha256.Sum256(ev)
	wantB3 := blake3.Sum256(ev)
	if gotSHA != hex.EncodeToString(wantSHA[:]) {
		t.Errorf("sha256 mismatch")
	}
	if gotB3 != hex.EncodeToString(wantB3[:]) {
		t.Errorf("blake3 mismatch")
	}
}

func TestCompute_empty_input(t *testing.T) {
	mh, err := Compute(nil, Option{
		Algorithms: []Algorithm{AlgoSHA256, AlgoBLAKE3},
	})
	if err != nil {
		t.Fatalf("Compute(nil): %v", err)
	}
	emptySHA := sha256.Sum256(nil)
	emptyB3 := blake3.Sum256(nil)
	if mh.Algorithms[AlgoSHA256] != hex.EncodeToString(emptySHA[:]) {
		t.Errorf("empty sha256 mismatch")
	}
	if mh.Algorithms[AlgoBLAKE3] != hex.EncodeToString(emptyB3[:]) {
		t.Errorf("empty blake3 mismatch")
	}
	if mh.EvidenceSize != 0 {
		t.Errorf("size = %d, want 0", mh.EvidenceSize)
	}
}

func TestCompute_unsupported_algorithm(t *testing.T) {
	_, err := Compute([]byte("x"), Option{
		Algorithms: []Algorithm{"md5"},
	})
	if err == nil {
		t.Fatal("expected ErrUnsupportedAlgorithm, got nil")
	}
	if !errors.Is(err, ErrUnsupportedAlgorithm) {
		t.Errorf("err = %v, want wraps ErrUnsupportedAlgorithm", err)
	}
}

func TestCompute_duplicate_algorithms_dedup(t *testing.T) {
	mh, err := Compute([]byte("x"), Option{
		Algorithms: []Algorithm{AlgoSHA256, AlgoSHA256, AlgoBLAKE3, AlgoBLAKE3},
	})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if len(mh.Algorithms) != 2 {
		t.Errorf("expected 2 unique algorithms, got %d", len(mh.Algorithms))
	}
}

func TestCompute_determinism(t *testing.T) {
	ev := []byte(`{"foo":"bar","baz":[1,2,3]}`)
	opt := Option{
		Algorithms: []Algorithm{AlgoSHA256, AlgoBLAKE3},
		JSONPaths:  []string{"$.foo", "$.baz[0]"},
		LineHash:   true,
	}
	mh1, err := Compute(ev, opt)
	if err != nil {
		t.Fatalf("Compute1: %v", err)
	}
	mh2, err := Compute(ev, opt)
	if err != nil {
		t.Fatalf("Compute2: %v", err)
	}
	if mh1.Algorithms[AlgoSHA256] != mh2.Algorithms[AlgoSHA256] {
		t.Errorf("sha256 not deterministic")
	}
	if len(mh1.SubHashes) != len(mh2.SubHashes) {
		t.Fatalf("sub-hash count not deterministic: %d vs %d", len(mh1.SubHashes), len(mh2.SubHashes))
	}
	for i := range mh1.SubHashes {
		if mh1.SubHashes[i] != mh2.SubHashes[i] {
			t.Errorf("sub-hash[%d] not deterministic: %+v vs %+v", i, mh1.SubHashes[i], mh2.SubHashes[i])
		}
	}
}

func TestCompute_subhashes_sorted_by_path(t *testing.T) {
	ev := []byte(`{"z":1,"a":2,"m":3}`)
	mh, err := Compute(ev, Option{
		JSONPaths: []string{"$.z", "$.a", "$.m"},
	})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if len(mh.SubHashes) != 3 {
		t.Fatalf("expected 3 sub-hashes, got %d", len(mh.SubHashes))
	}
	paths := make([]string, len(mh.SubHashes))
	for i, s := range mh.SubHashes {
		paths[i] = s.Path
	}
	if !sort.StringsAreSorted(paths) {
		t.Errorf("sub-hash paths not sorted: %v", paths)
	}
}

func TestCompute_subhash_invariant_under_input_path_reorder(t *testing.T) {
	ev := []byte(`{"a":1,"b":2,"c":3}`)
	mh1, err := Compute(ev, Option{JSONPaths: []string{"$.a", "$.b", "$.c"}})
	if err != nil {
		t.Fatalf("Compute1: %v", err)
	}
	mh2, err := Compute(ev, Option{JSONPaths: []string{"$.c", "$.a", "$.b"}})
	if err != nil {
		t.Fatalf("Compute2: %v", err)
	}
	if len(mh1.SubHashes) != len(mh2.SubHashes) {
		t.Fatalf("count mismatch")
	}
	for i := range mh1.SubHashes {
		if mh1.SubHashes[i] != mh2.SubHashes[i] {
			t.Errorf("sub-hash[%d] differs after path reorder: %+v vs %+v", i, mh1.SubHashes[i], mh2.SubHashes[i])
		}
	}
}

func TestCompute_line_hash_basic(t *testing.T) {
	ev := []byte("line1\nline2\nline3\n")
	mh, err := Compute(ev, Option{LineHash: true})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if len(mh.SubHashes) != 3 {
		t.Fatalf("expected 3 line sub-hashes (trailing \\n ignored), got %d", len(mh.SubHashes))
	}
	wantPaths := []string{"line:1", "line:2", "line:3"}
	for i, want := range wantPaths {
		if mh.SubHashes[i].Path != want {
			t.Errorf("sub-hash[%d].Path = %q, want %q", i, mh.SubHashes[i].Path, want)
		}
	}
	// 각 line hash가 sha256(line) 과 일치.
	wantLine1 := sha256.Sum256([]byte("line1"))
	if mh.SubHashes[0].Hash != hex.EncodeToString(wantLine1[:]) {
		t.Errorf("line:1 hash mismatch")
	}
}

func TestCompute_line_hash_no_trailing_newline(t *testing.T) {
	ev := []byte("a\nb")
	mh, err := Compute(ev, Option{LineHash: true})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if len(mh.SubHashes) != 2 {
		t.Errorf("expected 2 sub-hashes, got %d", len(mh.SubHashes))
	}
}

func TestCompute_line_hash_empty_input(t *testing.T) {
	mh, err := Compute(nil, Option{LineHash: true})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if len(mh.SubHashes) != 0 {
		t.Errorf("empty input should yield 0 line sub-hashes, got %d", len(mh.SubHashes))
	}
}

func TestCompute_jsonpath_and_linehash_combined(t *testing.T) {
	ev := []byte(`{"x":1}` + "\n" + `{"y":2}` + "\n")
	mh, err := Compute(ev, Option{
		LineHash:  true,
		JSONPaths: nil, // JSON 전체가 multi-line이라 path 추출은 별 ev에서.
	})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	// 2 line sub-hashes만 — JSONPaths 없음.
	if len(mh.SubHashes) != 2 {
		t.Errorf("expected 2 line sub-hashes, got %d", len(mh.SubHashes))
	}
}

func TestCompute_jsonpath_invalid_path_returns_invalidpath_err(t *testing.T) {
	ev := []byte(`{"foo":1}`)
	_, err := Compute(ev, Option{JSONPaths: []string{"foo.bar"}})
	if err == nil {
		t.Fatal("expected ErrInvalidJSONPath, got nil")
	}
	if !errors.Is(err, ErrInvalidJSONPath) {
		t.Errorf("err = %v, want wraps ErrInvalidJSONPath", err)
	}
}

func TestCompute_jsonpath_absent_returns_notfound_err(t *testing.T) {
	ev := []byte(`{"foo":1}`)
	_, err := Compute(ev, Option{JSONPaths: []string{"$.absent"}})
	if err == nil {
		t.Fatal("expected ErrPathNotFound, got nil")
	}
	if !errors.Is(err, ErrPathNotFound) {
		t.Errorf("err = %v, want wraps ErrPathNotFound", err)
	}
}

func TestCompute_subhash_algorithm_override(t *testing.T) {
	ev := []byte(`{"a":1}`)
	mh, err := Compute(ev, Option{
		JSONPaths:        []string{"$.a"},
		SubHashAlgorithm: AlgoBLAKE3,
	})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if len(mh.SubHashes) != 1 {
		t.Fatalf("expected 1 sub-hash, got %d", len(mh.SubHashes))
	}
	if mh.SubHashes[0].Algo != AlgoBLAKE3 {
		t.Errorf("sub-hash algo = %q, want blake3", mh.SubHashes[0].Algo)
	}
	// 값 cross-check: blake3("1").
	wantB3 := blake3.Sum256([]byte("1"))
	if mh.SubHashes[0].Hash != hex.EncodeToString(wantB3[:]) {
		t.Errorf("sub-hash blake3 mismatch")
	}
}

func TestCompute_subhash_algorithm_unsupported(t *testing.T) {
	_, err := Compute([]byte("x"), Option{
		JSONPaths:        []string{"$"},
		SubHashAlgorithm: "md5",
	})
	if err == nil {
		t.Fatal("expected ErrUnsupportedAlgorithm, got nil")
	}
	if !errors.Is(err, ErrUnsupportedAlgorithm) {
		t.Errorf("err = %v, want wraps ErrUnsupportedAlgorithm", err)
	}
}

func TestCompute_hex_lowercase(t *testing.T) {
	mh, err := Compute([]byte("X"), Option{Algorithms: []Algorithm{AlgoSHA256, AlgoBLAKE3}})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	for a, h := range mh.Algorithms {
		for _, c := range h {
			if c >= 'A' && c <= 'F' {
				t.Errorf("algorithm %q hash %q contains uppercase hex", a, h)
				break
			}
		}
	}
}
