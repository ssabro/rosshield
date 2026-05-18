//go:build rosshield_enterprise

// verify_test.go — Verify 단위 테스트.

package multihash

import (
	"errors"
	"strings"
	"testing"
)

// helper — Compute + Verify round-trip.
func roundTrip(t *testing.T, ev []byte, opt Option, mode VerifyMode) {
	t.Helper()
	mh, err := Compute(ev, opt)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if err := Verify(ev, mh, opt, mode); err != nil {
		t.Fatalf("Verify(unchanged): %v", err)
	}
}

func TestVerify_core_sha256_round_trip(t *testing.T) {
	roundTrip(t, []byte("hello"), Option{}, ModeCoreSHA256)
}

func TestVerify_enterprise_full_round_trip(t *testing.T) {
	ev := []byte(`{"a":1,"b":2}`)
	opt := Option{
		Algorithms: []Algorithm{AlgoSHA256, AlgoBLAKE3},
		JSONPaths:  []string{"$.a"},
		LineHash:   true,
	}
	roundTrip(t, ev, opt, ModeEnterpriseFull)
}

func TestVerify_sha256_mismatch(t *testing.T) {
	mh, err := Compute([]byte("hello"), Option{})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	// evidence 변경 → sha256 어긋남. EvidenceSize도 같이 바꿔서 size pre-check를 통과시킴.
	tampered := []byte("HELLO")
	mh.EvidenceSize = int64(len(tampered))
	err = Verify(tampered, mh, Option{}, ModeCoreSHA256)
	if err == nil {
		t.Fatal("expected ErrSHA256Mismatch, got nil")
	}
	if !errors.Is(err, ErrSHA256Mismatch) {
		t.Errorf("err = %v, want wraps ErrSHA256Mismatch", err)
	}
}

func TestVerify_evidence_size_mismatch(t *testing.T) {
	mh, err := Compute([]byte("hello"), Option{})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	err = Verify([]byte("hello world"), mh, Option{}, ModeCoreSHA256)
	if err == nil {
		t.Fatal("expected ErrEvidenceSizeMismatch, got nil")
	}
	if !errors.Is(err, ErrEvidenceSizeMismatch) {
		t.Errorf("err = %v, want wraps ErrEvidenceSizeMismatch", err)
	}
}

func TestVerify_blake3_mismatch_enterprise_only(t *testing.T) {
	ev := []byte("payload")
	mh, err := Compute(ev, Option{Algorithms: []Algorithm{AlgoSHA256, AlgoBLAKE3}})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	// blake3 hash만 조작.
	mh.Algorithms[AlgoBLAKE3] = strings.Repeat("0", 64)
	err = Verify(ev, mh, Option{Algorithms: []Algorithm{AlgoSHA256, AlgoBLAKE3}}, ModeEnterpriseFull)
	if err == nil {
		t.Fatal("expected ErrBLAKE3Mismatch, got nil")
	}
	if !errors.Is(err, ErrBLAKE3Mismatch) {
		t.Errorf("err = %v, want wraps ErrBLAKE3Mismatch", err)
	}
}

func TestVerify_blake3_mismatch_ignored_in_core_mode(t *testing.T) {
	// 코어 모드는 sha256 단독 — blake3 변조 무시.
	ev := []byte("payload")
	mh, err := Compute(ev, Option{Algorithms: []Algorithm{AlgoSHA256, AlgoBLAKE3}})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	mh.Algorithms[AlgoBLAKE3] = strings.Repeat("f", 64)
	// 코어 mode: sha256만 검증 → PASS.
	if err := Verify(ev, mh, Option{}, ModeCoreSHA256); err != nil {
		t.Errorf("core mode should ignore blake3 tamper, got err: %v", err)
	}
}

func TestVerify_subhash_mismatch_path_in_error(t *testing.T) {
	ev := []byte(`{"foo":1,"bar":2}`)
	opt := Option{JSONPaths: []string{"$.foo", "$.bar"}}
	mh, err := Compute(ev, opt)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	// $.foo의 sub-hash만 조작.
	for i := range mh.SubHashes {
		if mh.SubHashes[i].Path == subHashSchemeJSONPath+"$.foo" {
			mh.SubHashes[i].Hash = strings.Repeat("a", 64)
		}
	}
	err = Verify(ev, mh, opt, ModeEnterpriseFull)
	if err == nil {
		t.Fatal("expected ErrSubHashMismatch, got nil")
	}
	if !errors.Is(err, ErrSubHashMismatch) {
		t.Errorf("err = %v, want wraps ErrSubHashMismatch", err)
	}
	if !strings.Contains(err.Error(), "$.foo") {
		t.Errorf("err should mention path, got %q", err.Error())
	}
}

func TestVerify_subhash_missing_path_returns_subhash_mismatch(t *testing.T) {
	// expected가 임의 path를 선언했는데 재계산에는 없음.
	ev := []byte(`{"x":1}`)
	opt := Option{JSONPaths: []string{"$.x"}}
	mh, err := Compute(ev, opt)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	// expected에 추가 경로 가짜 추가 (재계산 결과에는 없을 것).
	mh.SubHashes = append(mh.SubHashes, SubHash{
		Path: subHashSchemeJSONPath + "$.ghost",
		Hash: strings.Repeat("0", 64),
		Algo: AlgoSHA256,
	})
	// 재계산 시 Option은 원본 그대로 — $.ghost는 재계산에 등장 안 함.
	err = Verify(ev, mh, opt, ModeEnterpriseFull)
	if err == nil {
		t.Fatal("expected ErrSubHashMismatch, got nil")
	}
	if !errors.Is(err, ErrSubHashMismatch) {
		t.Errorf("err = %v, want wraps ErrSubHashMismatch", err)
	}
}

func TestVerify_expected_missing_sha256(t *testing.T) {
	// expected.Algorithms에 sha256 자체가 없는 변질된 입력 → sentinel.
	mh := MultiHash{
		Algorithms:   map[Algorithm]string{AlgoBLAKE3: strings.Repeat("0", 64)},
		EvidenceSize: 1,
	}
	err := Verify([]byte("x"), mh, Option{}, ModeCoreSHA256)
	if err == nil {
		t.Fatal("expected ErrSHA256Mismatch, got nil")
	}
	if !errors.Is(err, ErrSHA256Mismatch) {
		t.Errorf("err = %v, want wraps ErrSHA256Mismatch", err)
	}
}

func TestVerify_line_hash_change_detected(t *testing.T) {
	// 라인 N만 바꾸면 그 line:N sub-hash만 어긋남 + 전체 sha256도 어긋남.
	ev := []byte("alpha\nbeta\ngamma\n")
	opt := Option{LineHash: true}
	mh, err := Compute(ev, opt)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	tampered := []byte("alpha\nBETA\ngamma\n")
	mh.EvidenceSize = int64(len(tampered)) // 길이 같음 사실은 (소문자 4 → 대문자 4).
	err = Verify(tampered, mh, opt, ModeEnterpriseFull)
	if err == nil {
		t.Fatal("expected mismatch, got nil")
	}
	// sha256가 먼저 잡힘 — sentinel은 SHA256Mismatch.
	if !errors.Is(err, ErrSHA256Mismatch) {
		t.Errorf("err = %v, want sha256 mismatch first", err)
	}
}

func TestVerify_line_hash_isolation_subhash_diff(t *testing.T) {
	// expected는 원본 ev 기반. tampered evidence로 sub-hash 단위 검증 (sha256 우회 — algorithm 강제 제외).
	// 사용: expected의 sha256을 새 evidence 기준으로 재계산해서 sha256은 통과, line sub-hash만 어긋남.
	origEv := []byte("alpha\nbeta\ngamma\n")
	opt := Option{LineHash: true}
	expected, err := Compute(origEv, opt)
	if err != nil {
		t.Fatalf("Compute orig: %v", err)
	}
	tampered := []byte("alpha\nBETA\ngamma\n")
	// expected의 algorithm hash를 tampered 기준으로 갱신 (sha256/blake3 통과 시키기).
	tamperedMh, err := Compute(tampered, opt)
	if err != nil {
		t.Fatalf("Compute tampered: %v", err)
	}
	expected.Algorithms = tamperedMh.Algorithms
	expected.EvidenceSize = int64(len(tampered))
	// 이제 line:2 sub-hash만 어긋남.
	err = Verify(tampered, expected, opt, ModeEnterpriseFull)
	if err == nil {
		t.Fatal("expected sub-hash mismatch, got nil")
	}
	if !errors.Is(err, ErrSubHashMismatch) {
		t.Errorf("err = %v, want ErrSubHashMismatch", err)
	}
	if !strings.Contains(err.Error(), "line:2") {
		t.Errorf("err should mention line:2, got %q", err.Error())
	}
}

func TestIsMismatch(t *testing.T) {
	cases := []error{
		ErrSHA256Mismatch,
		ErrBLAKE3Mismatch,
		ErrSubHashMismatch,
		ErrEvidenceSizeMismatch,
	}
	for _, e := range cases {
		if !IsMismatch(e) {
			t.Errorf("IsMismatch(%v) = false, want true", e)
		}
	}
	if IsMismatch(errors.New("other")) {
		t.Errorf("IsMismatch(other) = true, want false")
	}
	if IsMismatch(nil) {
		t.Errorf("IsMismatch(nil) = true, want false")
	}
}
