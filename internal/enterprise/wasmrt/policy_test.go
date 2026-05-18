//go:build rosshield_enterprise

package wasmrt

import (
	"errors"
	"testing"
)

func TestNopVerifier_항상_통과(t *testing.T) {
	v := NopVerifier{}
	if err := v.Verify(nil, nil); err != nil {
		t.Errorf("NopVerifier nil/nil: %v", err)
	}
	if err := v.Verify([]byte("policy"), []byte("sig")); err != nil {
		t.Errorf("NopVerifier bytes: %v", err)
	}
	if err := v.Verify([]byte("policy"), nil); err != nil {
		t.Errorf("NopVerifier no sig: %v", err)
	}
}

func TestValidatePolicyBytes_정상_magic_통과(t *testing.T) {
	policy := append([]byte{}, wasmMagic...)
	if err := validatePolicyBytes(policy); err != nil {
		t.Errorf("valid magic 거부: %v", err)
	}
}

func TestValidatePolicyBytes_정상_magic_뒤에_section_있음_통과(t *testing.T) {
	policy := append([]byte{}, wasmMagic...)
	policy = append(policy, 0x00, 0x01, 0x02) // dummy section bytes
	if err := validatePolicyBytes(policy); err != nil {
		t.Errorf("magic + 임의 section 거부: %v", err)
	}
}

func TestValidatePolicyBytes_빈_입력_거부(t *testing.T) {
	err := validatePolicyBytes(nil)
	if !errors.Is(err, ErrInvalidPolicy) {
		t.Errorf("빈 입력 → ErrInvalidPolicy 아님: %v", err)
	}
}

func TestValidatePolicyBytes_짧은_입력_거부(t *testing.T) {
	err := validatePolicyBytes([]byte{0x00, 0x61, 0x73})
	if !errors.Is(err, ErrInvalidPolicy) {
		t.Errorf("짧은 입력 → ErrInvalidPolicy 아님: %v", err)
	}
}

func TestValidatePolicyBytes_잘못된_magic_거부(t *testing.T) {
	policy := []byte{0xff, 0xff, 0xff, 0xff, 0x01, 0x00, 0x00, 0x00}
	err := validatePolicyBytes(policy)
	if !errors.Is(err, ErrInvalidPolicy) {
		t.Errorf("잘못된 magic → ErrInvalidPolicy 아님: %v", err)
	}
}

func TestValidatePolicyBytes_잘못된_version_거부(t *testing.T) {
	// magic은 맞으나 version이 2 (현재 wasm은 1만 표준).
	policy := []byte{0x00, 0x61, 0x73, 0x6d, 0x02, 0x00, 0x00, 0x00}
	err := validatePolicyBytes(policy)
	if !errors.Is(err, ErrInvalidPolicy) {
		t.Errorf("잘못된 version → ErrInvalidPolicy 아님: %v", err)
	}
}

// errVerifier 는 항상 거부하는 verifier (테스트 전용).
type errVerifier struct{}

func (errVerifier) Verify(_, _ []byte) error {
	return ErrPolicySignatureInvalid
}

func TestPolicyVerifier_거부_경로_sentinel_검출(t *testing.T) {
	v := errVerifier{}
	err := v.Verify([]byte("x"), []byte("y"))
	if !errors.Is(err, ErrPolicySignatureInvalid) {
		t.Errorf("errVerifier가 ErrPolicySignatureInvalid를 반환하지 않음: %v", err)
	}
}

func TestErrors_sentinel_고유성(t *testing.T) {
	all := []error{
		ErrInvalidPolicy,
		ErrCPUTimeout,
		ErrMemoryExceeded,
		ErrStdoutTruncated,
		ErrInvalidOutput,
		ErrPolicySignatureInvalid,
		ErrRuntimeClosed,
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
