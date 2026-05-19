//go:build rosshield_enterprise

// sigstore.go — WASM 정책의 cosign Sigstore keyless 서명 검증 (C-1 v3).
//
// 본 파일은 keyless (short-lived Fulcio cert + Rekor transparency log + OIDC
// identity) 검증 을 구현 합니다. Keyed (장기 keypair) 모드 는 cosign.go 의
// CosignKeyedVerifier 로 별 도 처리 합니다 — 둘 다 PolicyVerifier 인터 페이스 를
// 구현 하므로 EvaluateWithVerifier 에 호환 됩니다.
//
// 본 round 의 범위 (design doc §6.4):
//   - Sigstore Bundle (cosign sign-blob --bundle 출력 — JSON) 입력.
//   - Fulcio cert chain : trusted root 의 Fulcio CA 와 매칭.
//   - Rekor entry       : transparency log inclusion proof + log ID 검증.
//   - OIDC identity     : Fulcio cert 의 SubjectAlternativeName + issuer OID
//                         가 호출자 제공 SigstoreIdentity 리스트 중 하나와 매칭.
//
// 결정론 보장:
//   - 같은 (policy bytes, bundle bytes, trusted material) → 같은 verify 결과.
//   - sigstore-go verify 는 외부 네트워크 호출 없음 (trusted material 주입 모델).
//   - cert validity window 는 bundle 내 RFC3161 또는 Rekor integrated timestamp
//     를 사용하므로 wall-clock 의존 0 (재현 가능).
//
// 알고리즘 (Verify):
//   1. bundle parse (protobuf JSON) — 실패 시 ErrSigstoreBundleInvalid.
//   2. sigstore-go Verifier 구성 (TrustedMaterial + transparency log threshold=1
//      + integrated timestamp threshold=1).
//   3. PolicyBuilder 구성:
//        - WithArtifact(policy bytes) — Fulcio cert + signature 가 본 artifact 를 cover.
//        - WithCertificateIdentity(...) — 각 SigstoreIdentity 를 등록.
//   4. Verify 호출 — 결과 분류:
//        - ErrNoMatchingCertificateIdentity 또는 메시지 에 "identity"/"SAN"/"issuer"
//          포함 → ErrSigstoreIdentityMismatch.
//        - "rekor"/"transparency log"/"inclusion proof" 포함 → ErrSigstoreRekorInvalid.
//        - "fulcio"/"certificate"/"verification" 포함 → ErrSigstoreFulcioInvalid.
//        - 그 외 → ErrSigstoreFulcioInvalid (보수적 default — verify 실패 전반).
//
// 본 type 의 한계:
//   - TUF root 자동 fetch 미지원 — 호출자 가 root.TrustedMaterial 을 주입.
//     production 사용 시 호출자 측에서 sigstore-go pkg/tuf 로 fresh root 를 load.
//   - timestamping authority (TSA) 와 Rekor SCT (SignedCertificateTimestamp)
//     검증 은 기본 옵션 으로 비활성 — 필요 시 BuildOptions 확장 (v4+).
//
// 참조:
//   - docs/design/notes/phase7-public-transition-design.md §6.4 C-1 v3
//   - sigstore-go pkg/verify : https://pkg.go.dev/github.com/sigstore/sigstore-go/pkg/verify

package wasmrt

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	sgbundle "github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	sgverify "github.com/sigstore/sigstore-go/pkg/verify"
)

// SigstoreIdentity 는 Fulcio cert 의 OIDC identity 매칭 조건 입니다.
//
// 매칭 규칙 (둘 다 정규 표현식 — Go regexp 문법):
//   - SubjectRegex : Fulcio cert 의 SubjectAlternativeName (보통 GitHub Actions
//     workflow URL 또는 email) 과 정규 매칭. 빈 문자열 이면 SAN 매칭 skip.
//   - Issuer       : Fulcio OID 1.3.6.1.4.1.57264.1.1 (OIDC issuer URL) 와 정확
//     문자열 비교 (예: "https://token.actions.githubusercontent.com").
//
// 다중 SigstoreIdentity 는 OR 매칭 — 하나라도 매칭되면 통과.
//
// 본 type 은 immutable 합니다.
type SigstoreIdentity struct {
	SubjectRegex string
	Issuer       string
}

// CosignSigstoreVerifier 는 cosign keyless (Sigstore) 검증 을 수행 합니다.
//
// 필드:
//   - TrustedMaterial    : sigstore-go root.TrustedMaterial — Fulcio root/intermediate
//   - Rekor public key + (선택) CT log key 의 집합. 호출자 가 TUF 또는 embedded
//     bytes 로 부터 load.
//   - ExpectedIdentities : 허용 할 OIDC identity 리스트 (OR 매칭). 빈 리스트 면
//     검증 단계 에서 항상 거부 (false-confidence 방지) — design 원칙: identity
//     강제 default-on.
//   - RequireTlog        : true (default) 면 Rekor inclusion proof 강제.
//     false 면 Rekor 검증 skip (TSA-only 또는 private deployment).
//   - TlogThreshold      : 최소 verified Rekor entry 수 (default 1). 0 → 1 로
//     설정.
//
// 본 type 은 Verify 호출 동안 mutation 없음 — concurrent 안전.
type CosignSigstoreVerifier struct {
	TrustedMaterial    root.TrustedMaterial
	ExpectedIdentities []SigstoreIdentity
	RequireTlog        bool
	TlogThreshold      int
}

// Verify 는 cosign Sigstore bundle 을 검증 합니다.
//
// 매개 변수:
//   - policy : 검증 대상 WASM 바이트 (Fulcio cert + signature 가 본 artifact 를 cover).
//   - bundle : cosign sign-blob --bundle 출력 (Sigstore bundle protobuf JSON).
//
// 반환:
//   - nil                          : 검증 통과 (cert chain + tlog + identity 모두 OK).
//   - ErrSigstoreBundleInvalid     : bundle parse 실패.
//   - ErrSigstoreFulcioInvalid     : Fulcio cert chain 검증 실패.
//   - ErrSigstoreRekorInvalid      : Rekor entry 검증 실패.
//   - ErrSigstoreIdentityMismatch  : SAN/issuer 매칭 실패.
//   - ErrUnsupportedSignatureAlgorithm : TrustedMaterial nil 등 설정 오류.
//   - ErrPolicySignatureInvalid    : 위 분류 어느 곳 에도 속하지 않는 일반 verify 실패.
func (v *CosignSigstoreVerifier) Verify(policy, bundleBytes []byte) error {
	if v == nil {
		return fmt.Errorf("%w: nil verifier", ErrUnsupportedSignatureAlgorithm)
	}
	if v.TrustedMaterial == nil {
		return fmt.Errorf("%w: nil trusted material", ErrUnsupportedSignatureAlgorithm)
	}
	if len(v.ExpectedIdentities) == 0 {
		// false-confidence 방지: identity 미지정 시 즉시 거부.
		return fmt.Errorf("%w: no expected identities configured", ErrSigstoreIdentityMismatch)
	}
	if len(bundleBytes) == 0 {
		return fmt.Errorf("%w: empty bundle", ErrSigstoreBundleInvalid)
	}

	// 1. bundle parse.
	bdl := &sgbundle.Bundle{}
	if err := bdl.UnmarshalJSON(bundleBytes); err != nil {
		return fmt.Errorf("%w: %v", ErrSigstoreBundleInvalid, err)
	}

	return v.verifyEntity(policy, bdl)
}

// verifyEntity 는 이미 파싱된 SignedEntity (bundle 또는 testing TestEntity) 를
// 검증 하는 내부 helper 입니다. Verify 가 bundle bytes 를 parse 한 뒤 호출 하며,
// 테스트 는 VirtualSigstore.Sign() 의 결과 (TestEntity, 같은 인터페이스) 를
// 직접 주입 합니다.
func (v *CosignSigstoreVerifier) verifyEntity(policy []byte, entity sgverify.SignedEntity) error {
	// 2. sigstore-go Verifier 구성.
	tlogThreshold := v.TlogThreshold
	if tlogThreshold <= 0 {
		tlogThreshold = 1
	}

	opts := []sgverify.VerifierOption{}
	if v.RequireTlog || tlogThreshold > 0 {
		opts = append(opts,
			sgverify.WithTransparencyLog(tlogThreshold),
			sgverify.WithIntegratedTimestamps(1),
		)
	} else {
		// Rekor 미강제 시 — current time 사용 (private deployment 또는 testing).
		opts = append(opts, sgverify.WithCurrentTime())
	}

	sgv, err := sgverify.NewVerifier(v.TrustedMaterial, opts...)
	if err != nil {
		return fmt.Errorf("%w: verifier construct: %v", ErrSigstoreFulcioInvalid, err)
	}

	// 3. PolicyBuilder 구성 — identity 옵션 등록.
	identOpts, err := buildIdentityOptions(v.ExpectedIdentities)
	if err != nil {
		return fmt.Errorf("%w: identity build: %v", ErrSigstoreIdentityMismatch, err)
	}

	policyBuilder := sgverify.NewPolicy(
		sgverify.WithArtifact(bytes.NewReader(policy)),
		identOpts...,
	)

	// 4. Verify 호출 + 결과 분류.
	if _, err := sgv.Verify(entity, policyBuilder); err != nil {
		return classifySigstoreVerifyError(err)
	}
	return nil
}

// buildIdentityOptions 는 SigstoreIdentity 리스트 를 sigstore-go PolicyOption 으로
// 변환 합니다. 각 항목 은 WithCertificateIdentity 로 등록 되어 OR 매칭 됩니다.
func buildIdentityOptions(ids []SigstoreIdentity) ([]sgverify.PolicyOption, error) {
	opts := make([]sgverify.PolicyOption, 0, len(ids))
	for i, id := range ids {
		certID, err := sgverify.NewShortCertificateIdentity(
			id.Issuer,       // issuer exact match (or empty + regex)
			"",              // issuer regex empty — 정확 매칭 만
			"",              // SAN exact value empty (regex 사용)
			id.SubjectRegex, // SAN 정규 표현식
		)
		if err != nil {
			return nil, fmt.Errorf("identity[%d]: %w", i, err)
		}
		opts = append(opts, sgverify.WithCertificateIdentity(certID))
	}
	return opts, nil
}

// classifySigstoreVerifyError 는 sigstore-go verify error 를 본 패키지 sentinel
// 로 wrap 합니다. sigstore-go 가 다양한 error type/메시지 를 반환 하므로 메시지
// 기반 분류 + type 매칭 을 병행.
func classifySigstoreVerifyError(err error) error {
	if err == nil {
		return nil
	}
	// 1. Identity mismatch — type-based 분류 (가장 명확).
	var idErr *sgverify.ErrNoMatchingCertificateIdentity
	if errors.As(err, &idErr) {
		return fmt.Errorf("%w: %v", ErrSigstoreIdentityMismatch, err)
	}

	// 2. 메시지 기반 분류 (sigstore-go 가 sentinel 노출 제한적).
	msg := strings.ToLower(err.Error())

	// Identity 관련 키워드.
	if containsAnyStr(msg, []string{
		"identity",
		"subjectalternativename",
		"san ",
		"issuer ",
		"oid extension",
	}) {
		return fmt.Errorf("%w: %v", ErrSigstoreIdentityMismatch, err)
	}

	// Rekor / transparency log 관련.
	if containsAnyStr(msg, []string{
		"rekor",
		"transparency log",
		"tlog",
		"inclusion proof",
		"inclusion promise",
		"signed entry timestamp",
		"integrated timestamp",
	}) {
		return fmt.Errorf("%w: %v", ErrSigstoreRekorInvalid, err)
	}

	// Fulcio / cert chain 관련 (default for verify failure).
	if containsAnyStr(msg, []string{
		"fulcio",
		"certificate",
		"x509",
		"cert chain",
		"signature",
		"verification",
		"artifact",
	}) {
		return fmt.Errorf("%w: %v", ErrSigstoreFulcioInvalid, err)
	}

	// 그 외 — 일반 policy signature 실패.
	return fmt.Errorf("%w: %v", ErrPolicySignatureInvalid, err)
}

// containsAnyStr 는 s 가 subs 중 어느 한 substring 이라도 포함 하는지 확인합니다.
// (cosign.go containsAny 는 [][]byte — 본 함수 는 string 변형).
func containsAnyStr(s string, subs []string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
