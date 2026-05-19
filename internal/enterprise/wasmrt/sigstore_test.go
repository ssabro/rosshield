//go:build rosshield_enterprise

// sigstore_test.go — CosignSigstoreVerifier 단위 테스트 (C-1 v3).
//
// 본 테스트 는 sigstore-go 의 VirtualSigstore (in-memory Fulcio/Rekor mock) 를
// 사용 하여 외부 네트워크 호출 0 + 결정 론 fixture 로 keyless 검증 의 happy/sad
// path 를 모두 cover 합니다.

package wasmrt

import (
	"errors"
	"strings"
	"testing"

	sgca "github.com/sigstore/sigstore-go/pkg/testing/ca"
	sgverify "github.com/sigstore/sigstore-go/pkg/verify"
)

// --- helpers -----------------------------------------------------------------

// newVirtualSigstore 는 in-memory Sigstore (TrustedMaterial + Fulcio CA + Rekor
// + TSA) 를 한 번에 생성 합니다.
func newVirtualSigstore(t *testing.T) *sgca.VirtualSigstore {
	t.Helper()
	vs, err := sgca.NewVirtualSigstore()
	if err != nil {
		t.Fatalf("new virtual sigstore: %v", err)
	}
	return vs
}

// signWithVS 는 VirtualSigstore 로 artifact (= WASM policy) 를 서명하여 testing
// 전용 SignedEntity 를 반환 합니다.
func signWithVS(t *testing.T, vs *sgca.VirtualSigstore, identity, issuer string, policy []byte) sgverify.SignedEntity {
	t.Helper()
	entity, err := vs.Sign(identity, issuer, policy)
	if err != nil {
		t.Fatalf("vs.Sign: %v", err)
	}
	return entity
}

// --- Verify (bundle bytes path) ----------------------------------------------

func TestCosignSigstoreVerifier_nil_receiver_미지원(t *testing.T) {
	var v *CosignSigstoreVerifier
	err := v.Verify([]byte("p"), []byte("b"))
	if !errors.Is(err, ErrUnsupportedSignatureAlgorithm) {
		t.Errorf("nil receiver: got %v, want ErrUnsupportedSignatureAlgorithm", err)
	}
}

func TestCosignSigstoreVerifier_nil_trusted_material_미지원(t *testing.T) {
	v := &CosignSigstoreVerifier{
		TrustedMaterial: nil,
		ExpectedIdentities: []SigstoreIdentity{
			{SubjectRegex: ".*", Issuer: "i"},
		},
	}
	err := v.Verify([]byte("p"), []byte("b"))
	if !errors.Is(err, ErrUnsupportedSignatureAlgorithm) {
		t.Errorf("nil trusted material: got %v, want ErrUnsupportedSignatureAlgorithm", err)
	}
}

func TestCosignSigstoreVerifier_empty_identities_거부(t *testing.T) {
	vs := newVirtualSigstore(t)
	v := &CosignSigstoreVerifier{
		TrustedMaterial:    vs,
		ExpectedIdentities: nil,
	}
	err := v.Verify([]byte("policy"), []byte("{}"))
	if !errors.Is(err, ErrSigstoreIdentityMismatch) {
		t.Errorf("empty identities: got %v, want ErrSigstoreIdentityMismatch", err)
	}
}

func TestCosignSigstoreVerifier_empty_bundle_거부(t *testing.T) {
	vs := newVirtualSigstore(t)
	v := &CosignSigstoreVerifier{
		TrustedMaterial: vs,
		ExpectedIdentities: []SigstoreIdentity{
			{SubjectRegex: ".*", Issuer: "issuer"},
		},
	}
	err := v.Verify([]byte("policy"), nil)
	if !errors.Is(err, ErrSigstoreBundleInvalid) {
		t.Errorf("empty bundle: got %v, want ErrSigstoreBundleInvalid", err)
	}
}

func TestCosignSigstoreVerifier_invalid_bundle_bytes_거부(t *testing.T) {
	vs := newVirtualSigstore(t)
	v := &CosignSigstoreVerifier{
		TrustedMaterial: vs,
		ExpectedIdentities: []SigstoreIdentity{
			{SubjectRegex: ".*", Issuer: "issuer"},
		},
	}
	// Bundle 은 protobuf JSON — 임의 garbage bytes 거부.
	err := v.Verify([]byte("policy"), []byte("not a sigstore bundle"))
	if !errors.Is(err, ErrSigstoreBundleInvalid) {
		t.Errorf("garbage bundle: got %v, want ErrSigstoreBundleInvalid", err)
	}
}

func TestCosignSigstoreVerifier_invalid_bundle_json_garbage_거부(t *testing.T) {
	vs := newVirtualSigstore(t)
	v := &CosignSigstoreVerifier{
		TrustedMaterial: vs,
		ExpectedIdentities: []SigstoreIdentity{
			{SubjectRegex: ".*", Issuer: "issuer"},
		},
	}
	// 유효 JSON 이지만 Bundle proto 가 아닌 객체.
	err := v.Verify([]byte("policy"), []byte(`{"foo":"bar","missing":"required fields"}`))
	if !errors.Is(err, ErrSigstoreBundleInvalid) {
		t.Errorf("non-bundle JSON: got %v, want ErrSigstoreBundleInvalid", err)
	}
}

// --- verifyEntity (signed entity 직접 검증 path) -----------------------------
// VirtualSigstore.Sign 이 반환 하는 TestEntity 는 SignedEntity 인터페이스 를
// 구현 — bundle bytes 경유 없이 직접 verify 가능. 테스트 의 결정론 + 단순성.

func TestCosignSigstoreVerifier_정상_정책_정상_identity_통과(t *testing.T) {
	vs := newVirtualSigstore(t)
	policy := []byte("policy bytes for sigstore")
	identity := "foo@example.com"
	issuer := "issuer-x"

	entity := signWithVS(t, vs, identity, issuer, policy)

	v := &CosignSigstoreVerifier{
		TrustedMaterial: vs,
		ExpectedIdentities: []SigstoreIdentity{
			{SubjectRegex: "^foo@.*$", Issuer: issuer},
		},
	}
	if err := v.verifyEntity(policy, entity); err != nil {
		t.Errorf("정상 verify 거부: %v", err)
	}
}

func TestCosignSigstoreVerifier_변조된_정책_거부(t *testing.T) {
	vs := newVirtualSigstore(t)
	policy := []byte("original policy")
	tampered := append([]byte{}, policy...)
	tampered[0] ^= 0x01

	entity := signWithVS(t, vs, "foo@example.com", "issuer", policy)

	v := &CosignSigstoreVerifier{
		TrustedMaterial: vs,
		ExpectedIdentities: []SigstoreIdentity{
			{SubjectRegex: ".*", Issuer: "issuer"},
		},
	}
	err := v.verifyEntity(tampered, entity)
	if err == nil {
		t.Fatal("변조 정책 통과 — 검증 누락")
	}
	// artifact mismatch → Fulcio/signature 분류 (verify 단계 일반 실패).
	if !errors.Is(err, ErrSigstoreFulcioInvalid) && !errors.Is(err, ErrPolicySignatureInvalid) {
		t.Errorf("변조 정책: got %v, want ErrSigstoreFulcioInvalid 또는 ErrPolicySignatureInvalid", err)
	}
}

func TestCosignSigstoreVerifier_identity_subject_불일치_거부(t *testing.T) {
	vs := newVirtualSigstore(t)
	policy := []byte("policy")
	entity := signWithVS(t, vs, "foo@example.com", "issuer-a", policy)

	v := &CosignSigstoreVerifier{
		TrustedMaterial: vs,
		ExpectedIdentities: []SigstoreIdentity{
			{SubjectRegex: "^bar@.*$", Issuer: "issuer-a"}, // subject 정규 식 다름
		},
	}
	err := v.verifyEntity(policy, entity)
	if !errors.Is(err, ErrSigstoreIdentityMismatch) {
		t.Errorf("subject 불일치: got %v, want ErrSigstoreIdentityMismatch", err)
	}
}

func TestCosignSigstoreVerifier_identity_issuer_불일치_거부(t *testing.T) {
	vs := newVirtualSigstore(t)
	policy := []byte("policy")
	entity := signWithVS(t, vs, "foo@example.com", "issuer-a", policy)

	v := &CosignSigstoreVerifier{
		TrustedMaterial: vs,
		ExpectedIdentities: []SigstoreIdentity{
			{SubjectRegex: ".*", Issuer: "issuer-different"}, // issuer 다름
		},
	}
	err := v.verifyEntity(policy, entity)
	if !errors.Is(err, ErrSigstoreIdentityMismatch) {
		t.Errorf("issuer 불일치: got %v, want ErrSigstoreIdentityMismatch", err)
	}
}

func TestCosignSigstoreVerifier_identity_다중_OR_매칭_통과(t *testing.T) {
	vs := newVirtualSigstore(t)
	policy := []byte("policy multi-identity")
	entity := signWithVS(t, vs, "bob@example.com", "issuer-b", policy)

	v := &CosignSigstoreVerifier{
		TrustedMaterial: vs,
		ExpectedIdentities: []SigstoreIdentity{
			// 첫 번째 — 매칭 실패 예상.
			{SubjectRegex: "^alice@.*$", Issuer: "issuer-a"},
			// 두 번째 — 매칭 성공 예상.
			{SubjectRegex: "^bob@.*$", Issuer: "issuer-b"},
		},
	}
	if err := v.verifyEntity(policy, entity); err != nil {
		t.Errorf("OR 매칭 실패: %v", err)
	}
}

func TestCosignSigstoreVerifier_identity_다중_모두_불일치_거부(t *testing.T) {
	vs := newVirtualSigstore(t)
	policy := []byte("policy")
	entity := signWithVS(t, vs, "carol@example.com", "issuer-c", policy)

	v := &CosignSigstoreVerifier{
		TrustedMaterial: vs,
		ExpectedIdentities: []SigstoreIdentity{
			{SubjectRegex: "^alice@.*$", Issuer: "issuer-a"},
			{SubjectRegex: "^bob@.*$", Issuer: "issuer-b"},
		},
	}
	err := v.verifyEntity(policy, entity)
	if !errors.Is(err, ErrSigstoreIdentityMismatch) {
		t.Errorf("모두 불일치: got %v, want ErrSigstoreIdentityMismatch", err)
	}
}

func TestCosignSigstoreVerifier_invalid_subject_regex_거부(t *testing.T) {
	vs := newVirtualSigstore(t)
	policy := []byte("policy")
	entity := signWithVS(t, vs, "foo@example.com", "issuer", policy)

	v := &CosignSigstoreVerifier{
		TrustedMaterial: vs,
		ExpectedIdentities: []SigstoreIdentity{
			{SubjectRegex: "[invalid(regex", Issuer: "issuer"}, // 잘못된 regex.
		},
	}
	err := v.verifyEntity(policy, entity)
	if !errors.Is(err, ErrSigstoreIdentityMismatch) {
		t.Errorf("invalid regex: got %v, want ErrSigstoreIdentityMismatch", err)
	}
}

// --- 결정 론: 같은 입력 → 같은 결과 -----------------------------------------

func TestCosignSigstoreVerifier_결정론_같은_entity_같은_결과(t *testing.T) {
	vs := newVirtualSigstore(t)
	policy := []byte("determinism test")
	entity := signWithVS(t, vs, "foo@example.com", "issuer", policy)

	v := &CosignSigstoreVerifier{
		TrustedMaterial: vs,
		ExpectedIdentities: []SigstoreIdentity{
			{SubjectRegex: "^foo@.*$", Issuer: "issuer"},
		},
	}
	err1 := v.verifyEntity(policy, entity)
	err2 := v.verifyEntity(policy, entity)
	// nil 둘 또는 같은 error type 둘 모두 결정론.
	if (err1 == nil) != (err2 == nil) {
		t.Errorf("결정 론 위반: %v vs %v", err1, err2)
	}
}

// --- classifySigstoreVerifyError 단위 ---------------------------------------

func TestClassifySigstoreVerifyError_nil_pass_through(t *testing.T) {
	if got := classifySigstoreVerifyError(nil); got != nil {
		t.Errorf("nil: got %v, want nil", got)
	}
}

func TestClassifySigstoreVerifyError_identity_keyword_매핑(t *testing.T) {
	cases := []string{
		"no matching identity found",
		"SAN value mismatch",
		"issuer mismatch detected",
		"oid extension does not match",
	}
	for _, msg := range cases {
		err := classifySigstoreVerifyError(errors.New(msg))
		if !errors.Is(err, ErrSigstoreIdentityMismatch) {
			t.Errorf("%q: got %v, want ErrSigstoreIdentityMismatch", msg, err)
		}
	}
}

func TestClassifySigstoreVerifyError_rekor_keyword_매핑(t *testing.T) {
	cases := []string{
		"rekor entry not found",
		"transparency log threshold not met",
		"tlog inclusion proof invalid",
		"inclusion proof missing",
		"signed entry timestamp invalid",
		"integrated timestamp before cert NotBefore",
	}
	for _, msg := range cases {
		err := classifySigstoreVerifyError(errors.New(msg))
		if !errors.Is(err, ErrSigstoreRekorInvalid) {
			t.Errorf("%q: got %v, want ErrSigstoreRekorInvalid", msg, err)
		}
	}
}

func TestClassifySigstoreVerifyError_fulcio_keyword_매핑(t *testing.T) {
	cases := []string{
		"fulcio certificate verify failed",
		"certificate chain invalid",
		"x509: signature verification failed",
		"artifact digest mismatch",
	}
	for _, msg := range cases {
		err := classifySigstoreVerifyError(errors.New(msg))
		if !errors.Is(err, ErrSigstoreFulcioInvalid) {
			t.Errorf("%q: got %v, want ErrSigstoreFulcioInvalid", msg, err)
		}
	}
}

func TestClassifySigstoreVerifyError_unknown_은_policy_signature(t *testing.T) {
	err := classifySigstoreVerifyError(errors.New("totally unexpected message blob"))
	if !errors.Is(err, ErrPolicySignatureInvalid) {
		t.Errorf("unknown: got %v, want ErrPolicySignatureInvalid", err)
	}
}

func TestClassifySigstoreVerifyError_message_포함_원문(t *testing.T) {
	original := errors.New("fulcio specific message detail")
	err := classifySigstoreVerifyError(original)
	if !strings.Contains(err.Error(), "fulcio specific message detail") {
		t.Errorf("원문 손실: %v", err)
	}
}

// --- ErrNoMatchingCertificateIdentity 타입 매칭 ------------------------------
// sigstore-go 의 ErrNoMatchingCertificateIdentity 가 정확히 ErrSigstoreIdentityMismatch
// 로 매핑되는지 검증 — 메시지 분류 보다 type 분류 우선.

func TestClassifySigstoreVerifyError_ErrNoMatching_타입_매칭(t *testing.T) {
	// VirtualSigstore + 잘못된 identity 로 실제 ErrNoMatchingCertificateIdentity
	// 를 발생 시켜 검증.
	vs := newVirtualSigstore(t)
	policy := []byte("policy")
	entity := signWithVS(t, vs, "foo@example.com", "issuer-a", policy)

	v := &CosignSigstoreVerifier{
		TrustedMaterial: vs,
		ExpectedIdentities: []SigstoreIdentity{
			{SubjectRegex: "^baz@.*$", Issuer: "issuer-z"},
		},
	}
	err := v.verifyEntity(policy, entity)
	if !errors.Is(err, ErrSigstoreIdentityMismatch) {
		t.Errorf("ErrNoMatching: got %v, want ErrSigstoreIdentityMismatch", err)
	}
}

// --- sentinel 고유성 + PolicyVerifier 인터페이스 구현 ------------------------

func TestCosignSigstoreVerifier_implements_PolicyVerifier(t *testing.T) {
	var _ PolicyVerifier = (*CosignSigstoreVerifier)(nil)
}

func TestSigstore_sentinel_고유성(t *testing.T) {
	all := []error{
		ErrInvalidPolicy,
		ErrCPUTimeout,
		ErrMemoryExceeded,
		ErrStdoutTruncated,
		ErrInvalidOutput,
		ErrPolicySignatureInvalid,
		ErrRuntimeClosed,
		ErrUnsupportedSignatureAlgorithm,
		ErrInvalidPublicKey,
		ErrSigstoreBundleInvalid,
		ErrSigstoreFulcioInvalid,
		ErrSigstoreRekorInvalid,
		ErrSigstoreIdentityMismatch,
	}
	for i, a := range all {
		for j, b := range all {
			if i == j {
				continue
			}
			if errors.Is(a, b) {
				t.Errorf("sentinel %v == %v (구분 불가)", a, b)
			}
		}
	}
}
