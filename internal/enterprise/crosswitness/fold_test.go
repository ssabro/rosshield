//go:build rosshield_enterprise

// fold_test.go — A-1 cross-witness fold-in 단위 테스트.
//
// 본 테스트는 spec-candidate-A-draft.md [0019]~[0020-4]의 정확한 명세를 검증합니다:
//   - fold-in 엔트리는 다른 테넌트의 최신 checkpoint(tenantId, seq, hash, signedAt) 집합을
//     RFC 8785 canonical JSON으로 직렬화하여 새 entry hash 입력에 포함.
//   - crossWitness 배열은 tenantId의 lexicographic 정렬, 중복 금지.
//   - 검증자는 각 TenantCheckpoint가 해당 테넌트 체인 재계산 결과와 일치하는지 확인.
//   - 일치 안 하면 CROSS_WITNESS_MISMATCH 반환.

package crosswitness

import (
	"crypto/sha256"
	"errors"
	"testing"
	"time"
)

// helper — Hash literal 생성기.
func mkHash(b byte) Hash {
	var h Hash
	for i := range h {
		h[i] = b
	}
	return h
}

func TestFoldHeads_빈_witness_set_은_빈_canonical_serialization_을_낸다(t *testing.T) {
	got, err := SerializeCrossWitness(nil)
	if err != nil {
		t.Fatalf("SerializeCrossWitness(nil) 에러: %v", err)
	}
	if string(got) != "[]" {
		t.Errorf("빈 witness set serialization = %q, want %q", string(got), "[]")
	}

	got2, err := SerializeCrossWitness([]TenantCheckpoint{})
	if err != nil {
		t.Fatalf("SerializeCrossWitness([]) 에러: %v", err)
	}
	if string(got2) != "[]" {
		t.Errorf("빈 slice serialization = %q, want %q", string(got2), "[]")
	}
}

func TestSerializeCrossWitness_tenantId_lexicographic_정렬(t *testing.T) {
	// 입력은 무작위 순서 — 결과는 tenantId 사전식 정렬이어야 한다.
	ts := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	in := []TenantCheckpoint{
		{TenantID: "tenant-c", Seq: 3, Hash: mkHash(0xCC), SignedAt: ts},
		{TenantID: "tenant-a", Seq: 1, Hash: mkHash(0xAA), SignedAt: ts},
		{TenantID: "tenant-b", Seq: 2, Hash: mkHash(0xBB), SignedAt: ts},
	}
	out, err := SerializeCrossWitness(in)
	if err != nil {
		t.Fatalf("SerializeCrossWitness 에러: %v", err)
	}
	// canonical JSON에 'tenant-a'가 'tenant-b'보다, 'tenant-b'가 'tenant-c'보다 앞에 와야 한다.
	s := string(out)
	posA, posB, posC := indexOf(s, "tenant-a"), indexOf(s, "tenant-b"), indexOf(s, "tenant-c")
	if posA < 0 || posB < 0 || posC < 0 {
		t.Fatalf("serialization에 tenant id 누락: %q", s)
	}
	if !(posA < posB && posB < posC) {
		t.Errorf("정렬 어긋남: posA=%d posB=%d posC=%d (out=%q)", posA, posB, posC, s)
	}
}

func TestSerializeCrossWitness_결정론적_재현(t *testing.T) {
	// 같은 입력 → 같은 출력 (시점 무관).
	ts := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	in := []TenantCheckpoint{
		{TenantID: "tenant-x", Seq: 1, Hash: mkHash(0x11), SignedAt: ts},
		{TenantID: "tenant-y", Seq: 2, Hash: mkHash(0x22), SignedAt: ts},
	}
	out1, err := SerializeCrossWitness(in)
	if err != nil {
		t.Fatalf("1차 serialize 에러: %v", err)
	}
	out2, err := SerializeCrossWitness(in)
	if err != nil {
		t.Fatalf("2차 serialize 에러: %v", err)
	}
	if string(out1) != string(out2) {
		t.Errorf("결정론 위반:\n  1차=%q\n  2차=%q", out1, out2)
	}
}

func TestSerializeCrossWitness_순서_무관(t *testing.T) {
	// 입력 순서만 다르고 내용은 같으면 같은 결과가 나와야 한다.
	ts := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	a := []TenantCheckpoint{
		{TenantID: "alpha", Seq: 10, Hash: mkHash(0x11), SignedAt: ts},
		{TenantID: "bravo", Seq: 20, Hash: mkHash(0x22), SignedAt: ts},
	}
	b := []TenantCheckpoint{
		{TenantID: "bravo", Seq: 20, Hash: mkHash(0x22), SignedAt: ts},
		{TenantID: "alpha", Seq: 10, Hash: mkHash(0x11), SignedAt: ts},
	}
	outA, _ := SerializeCrossWitness(a)
	outB, _ := SerializeCrossWitness(b)
	if string(outA) != string(outB) {
		t.Errorf("순서 invariant 위반:\n  A=%q\n  B=%q", outA, outB)
	}
}

func TestSerializeCrossWitness_중복_tenant_거부(t *testing.T) {
	// 동일 tenantId가 두 번 들어가면 ErrDuplicateTenant.
	ts := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	in := []TenantCheckpoint{
		{TenantID: "dup", Seq: 1, Hash: mkHash(0x01), SignedAt: ts},
		{TenantID: "dup", Seq: 2, Hash: mkHash(0x02), SignedAt: ts},
	}
	_, err := SerializeCrossWitness(in)
	if !errors.Is(err, ErrDuplicateTenant) {
		t.Errorf("err = %v, want ErrDuplicateTenant", err)
	}
}

func TestSerializeCrossWitness_빈_tenantId_거부(t *testing.T) {
	ts := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	in := []TenantCheckpoint{
		{TenantID: "", Seq: 1, Hash: mkHash(0x01), SignedAt: ts},
	}
	_, err := SerializeCrossWitness(in)
	if !errors.Is(err, ErrEmptyTenantID) {
		t.Errorf("err = %v, want ErrEmptyTenantID", err)
	}
}

func TestComputeFoldInHash_입력_invariant(t *testing.T) {
	// 같은 prev/payload/meta/witness → 같은 hash.
	prev := mkHash(0xAA)
	payload := mkHash(0xBB)
	meta := []byte(`{"action":"fold","tenantId":"self"}`)
	ts := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	w := []TenantCheckpoint{
		{TenantID: "t1", Seq: 1, Hash: mkHash(0xCC), SignedAt: ts},
	}

	h1, err := ComputeFoldInHash(prev, payload, meta, w)
	if err != nil {
		t.Fatalf("1차 fold-in hash 에러: %v", err)
	}
	h2, err := ComputeFoldInHash(prev, payload, meta, w)
	if err != nil {
		t.Fatalf("2차 fold-in hash 에러: %v", err)
	}
	if h1 != h2 {
		t.Errorf("결정론 위반: %x vs %x", h1, h2)
	}
}

func TestComputeFoldInHash_witness_변경_시_다른_hash(t *testing.T) {
	// 다른 witness set은 다른 hash를 내야 한다 (위조 검출 기반).
	prev := mkHash(0xAA)
	payload := mkHash(0xBB)
	meta := []byte(`{"a":1}`)
	ts := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)

	w1 := []TenantCheckpoint{
		{TenantID: "t1", Seq: 1, Hash: mkHash(0x01), SignedAt: ts},
	}
	w2 := []TenantCheckpoint{
		{TenantID: "t1", Seq: 1, Hash: mkHash(0x02), SignedAt: ts}, // hash만 다름
	}
	h1, _ := ComputeFoldInHash(prev, payload, meta, w1)
	h2, _ := ComputeFoldInHash(prev, payload, meta, w2)
	if h1 == h2 {
		t.Error("witness hash 변경에도 같은 fold-in hash 발생 — 위조 검출 불가")
	}
}

func TestComputeFoldInHash_빈_witness_도_정의된_hash_를_낸다(t *testing.T) {
	// [0020-4]: 대상 0개도 valid — 외부 anchoring 동반 의무는 본 fold 함수 밖.
	prev := mkHash(0xAA)
	payload := mkHash(0xBB)
	meta := []byte(`{"a":1}`)

	h, err := ComputeFoldInHash(prev, payload, meta, nil)
	if err != nil {
		t.Fatalf("빈 witness fold-in 에러: %v", err)
	}
	if h == (Hash{}) {
		t.Error("빈 witness fold-in hash가 zero — 결정론 위반")
	}

	// 동일한 입력 재실행해도 같은 결과.
	h2, _ := ComputeFoldInHash(prev, payload, meta, []TenantCheckpoint{})
	if h != h2 {
		t.Errorf("nil vs empty slice 결과 차이: %x vs %x", h, h2)
	}
}

func TestVerifyFoldIn_정확한_witness_set_은_OK(t *testing.T) {
	// 시나리오: fold-in 엔트리 생성 → VerifyFoldIn에 같은 witness 전달 → ok.
	prev := mkHash(0xAA)
	payload := mkHash(0xBB)
	meta := []byte(`{"a":1}`)
	ts := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)

	w := []TenantCheckpoint{
		{TenantID: "t1", Seq: 1, Hash: mkHash(0x01), SignedAt: ts},
		{TenantID: "t2", Seq: 2, Hash: mkHash(0x02), SignedAt: ts},
	}
	foldHash, err := ComputeFoldInHash(prev, payload, meta, w)
	if err != nil {
		t.Fatalf("fold-in hash 에러: %v", err)
	}
	// 검증자가 같은 witness set으로 재계산 → fold-in hash가 일치해야 한다.
	if err := VerifyFoldIn(prev, payload, meta, w, foldHash); err != nil {
		t.Errorf("정확한 witness인데 검증 실패: %v", err)
	}
}

func TestVerifyFoldIn_witness_한_건_변조_시_MISMATCH(t *testing.T) {
	// 운영자가 t1 chain을 사후 위조 → 검증자가 fold-in 엔트리 안 t1.hash와 어긋남을 검출.
	prev := mkHash(0xAA)
	payload := mkHash(0xBB)
	meta := []byte(`{"a":1}`)
	ts := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)

	original := []TenantCheckpoint{
		{TenantID: "t1", Seq: 1, Hash: mkHash(0x01), SignedAt: ts},
		{TenantID: "t2", Seq: 2, Hash: mkHash(0x02), SignedAt: ts},
	}
	foldHash, _ := ComputeFoldInHash(prev, payload, meta, original)

	// 사후 위조: t1의 hash가 바뀐 chain 재계산 결과.
	tampered := []TenantCheckpoint{
		{TenantID: "t1", Seq: 1, Hash: mkHash(0xFF), SignedAt: ts}, // 위조!
		{TenantID: "t2", Seq: 2, Hash: mkHash(0x02), SignedAt: ts},
	}
	err := VerifyFoldIn(prev, payload, meta, tampered, foldHash)
	if !errors.Is(err, ErrCrossWitnessMismatch) {
		t.Errorf("err = %v, want ErrCrossWitnessMismatch", err)
	}
}

func TestVerifyFoldIn_witness_누락_시_MISMATCH(t *testing.T) {
	prev := mkHash(0xAA)
	payload := mkHash(0xBB)
	meta := []byte(`{"a":1}`)
	ts := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)

	w := []TenantCheckpoint{
		{TenantID: "t1", Seq: 1, Hash: mkHash(0x01), SignedAt: ts},
		{TenantID: "t2", Seq: 2, Hash: mkHash(0x02), SignedAt: ts},
	}
	foldHash, _ := ComputeFoldInHash(prev, payload, meta, w)

	// 검증 시 한 건이 사라진 set 전달 → MISMATCH.
	partial := []TenantCheckpoint{
		{TenantID: "t1", Seq: 1, Hash: mkHash(0x01), SignedAt: ts},
	}
	err := VerifyFoldIn(prev, payload, meta, partial, foldHash)
	if !errors.Is(err, ErrCrossWitnessMismatch) {
		t.Errorf("err = %v, want ErrCrossWitnessMismatch", err)
	}
}

func TestVerifyFoldIn_witness_순서_달라도_OK(t *testing.T) {
	// 검증자가 다른 순서로 witness를 전달해도 ComputeFoldInHash 내부에서 정렬되므로 OK여야 한다.
	prev := mkHash(0xAA)
	payload := mkHash(0xBB)
	meta := []byte(`{"a":1}`)
	ts := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)

	w := []TenantCheckpoint{
		{TenantID: "alpha", Seq: 1, Hash: mkHash(0xA1), SignedAt: ts},
		{TenantID: "bravo", Seq: 2, Hash: mkHash(0xB2), SignedAt: ts},
		{TenantID: "charlie", Seq: 3, Hash: mkHash(0xC3), SignedAt: ts},
	}
	foldHash, _ := ComputeFoldInHash(prev, payload, meta, w)

	shuffled := []TenantCheckpoint{
		{TenantID: "charlie", Seq: 3, Hash: mkHash(0xC3), SignedAt: ts},
		{TenantID: "alpha", Seq: 1, Hash: mkHash(0xA1), SignedAt: ts},
		{TenantID: "bravo", Seq: 2, Hash: mkHash(0xB2), SignedAt: ts},
	}
	if err := VerifyFoldIn(prev, payload, meta, shuffled, foldHash); err != nil {
		t.Errorf("순서 다른 동일 set인데 실패: %v", err)
	}
}

func TestVerifyFoldIn_빈_set_도_검증_가능(t *testing.T) {
	prev := mkHash(0xAA)
	payload := mkHash(0xBB)
	meta := []byte(`{"a":1}`)

	foldHash, _ := ComputeFoldInHash(prev, payload, meta, nil)
	if err := VerifyFoldIn(prev, payload, meta, nil, foldHash); err != nil {
		t.Errorf("빈 witness 검증 실패: %v", err)
	}
}

func TestVerifyFoldIn_prev_변경_시_MISMATCH(t *testing.T) {
	// prevHash가 다르면 검증 실패해야 함 — 체인 무결성 보장.
	payload := mkHash(0xBB)
	meta := []byte(`{"a":1}`)
	ts := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)

	w := []TenantCheckpoint{
		{TenantID: "t1", Seq: 1, Hash: mkHash(0x01), SignedAt: ts},
	}
	foldHash, _ := ComputeFoldInHash(mkHash(0xAA), payload, meta, w)

	err := VerifyFoldIn(mkHash(0xBB), payload, meta, w, foldHash)
	if !errors.Is(err, ErrCrossWitnessMismatch) {
		t.Errorf("err = %v, want ErrCrossWitnessMismatch", err)
	}
}

func TestSerializeCrossWitness_signedAt_RFC3339Nano_UTC(t *testing.T) {
	// canonical JSON에서 signedAt이 RFC3339Nano UTC 문자열로 직렬화되는지 확인.
	// 입력에 +09:00 timezone을 주더라도 UTC로 변환되어야 한다 (결정론 보장).
	loc, _ := time.LoadLocation("Asia/Seoul")
	tsKST := time.Date(2026, 5, 18, 21, 0, 0, 0, loc) // = 12:00 UTC
	tsUTC := tsKST.UTC()

	inKST := []TenantCheckpoint{
		{TenantID: "t1", Seq: 1, Hash: mkHash(0x01), SignedAt: tsKST},
	}
	inUTC := []TenantCheckpoint{
		{TenantID: "t1", Seq: 1, Hash: mkHash(0x01), SignedAt: tsUTC},
	}
	outKST, _ := SerializeCrossWitness(inKST)
	outUTC, _ := SerializeCrossWitness(inUTC)
	if string(outKST) != string(outUTC) {
		t.Errorf("UTC 정규화 실패:\n  KST 입력 → %q\n  UTC 입력 → %q", outKST, outUTC)
	}
}

func TestComputeFoldInHash_canonical_입력에서_sha256_을_사용(t *testing.T) {
	// 회귀 가드: 명세 변경 없는 한 알고리즘은 sha256. 단순 sanity.
	prev := mkHash(0x00)
	payload := mkHash(0x00)
	meta := []byte(`{}`)
	h, err := ComputeFoldInHash(prev, payload, meta, nil)
	if err != nil {
		t.Fatalf("계산 에러: %v", err)
	}
	// hash 길이 = 32바이트 (sha256).
	if len(h) != sha256.Size {
		t.Errorf("hash 길이 = %d, want %d (sha256)", len(h), sha256.Size)
	}
}

// indexOf는 strings.Index의 단순 사본 — test helper.
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
