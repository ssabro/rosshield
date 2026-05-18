//go:build rosshield_enterprise

// policy.go — WASM 정책 로딩 + 서명 검증 인터페이스 (C-1 sandboxed evaluator).
//
// 본 파일은 WASM 정책 바이트의 사전 검증과 서명 검증 인터페이스를 정의합니다.
// 실제 cosign 등 외부 서명 검증은 PolicyVerifier 구현체로 교체 가능합니다.
//
// 본 round 본체에는 다음 두 구현이 포함됩니다:
//   - NopVerifier  : 모든 서명을 통과시킵니다 (개발/테스트 환경).
//   - allowlistVerifier(테스트): blob → bool 매핑으로 특정 서명만 통과 (TDD 용).
//
// 실 cosign 검증은 후속 round 에서 별 어댑터로 주입됩니다 (R-D8 4.5 통합 전).
//
// 참조:
//   - docs/design/notes/phase7-public-transition-design.md §6.4 C-1 알고리즘 4단계
//   - sigstore/cosign 통합은 후속 — 본 round 는 인터페이스만.

package wasmrt

import (
	"bytes"
	"errors"
	"fmt"
)

// wasmMagic 은 WASM 바이너리의 magic + version (8바이트) prefix입니다.
//
//	magic  = 0x00 0x61 0x73 0x6d ('\0asm')
//	version = 0x01 0x00 0x00 0x00 (Wasm Core 1.0)
//
// 모든 유효한 .wasm 파일은 이 8바이트로 시작합니다 (WebAssembly 1.0 spec §5.1.1).
var wasmMagic = []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}

// 오류 정의 — 모든 sentinel 는 errors.Is 로 식별 가능합니다.
var (
	// ErrInvalidPolicy 는 WASM 바이트가 magic+version prefix 를 갖지 않거나
	// wazero 컴파일 단계에서 거부될 때 반환됩니다.
	ErrInvalidPolicy = errors.New("wasmrt: invalid wasm policy")

	// ErrCPUTimeout 는 Evaluate 호출이 Limits.CPUTimeout 안 종료되지 않았을 때
	// 반환됩니다. context.DeadlineExceeded 를 wrap 합니다 (errors.Is 가능).
	ErrCPUTimeout = errors.New("wasmrt: cpu timeout exceeded")

	// ErrMemoryExceeded 는 WASM 모듈이 Limits.MemoryPages 한도를 초과해 memory.grow
	// 또는 access trap 을 일으켰을 때 반환됩니다.
	ErrMemoryExceeded = errors.New("wasmrt: memory limit exceeded")

	// ErrStdoutTruncated 는 WASM 모듈이 Limits.StdoutBytes 초과 분량을 stdout에
	// 쓰려고 시도했음을 알립니다. 누적 바이트는 한도까지 truncate 되며, JSON
	// 파싱 가능성은 사용자에게 위임됩니다 (대부분 ErrInvalidOutput 동반).
	ErrStdoutTruncated = errors.New("wasmrt: stdout truncated by limit")

	// ErrInvalidOutput 는 WASM 모듈이 stdout에 쓴 바이트가 Result JSON 으로
	// 파싱되지 않을 때 반환됩니다 (JSON syntax 오류 또는 status 필드 누락).
	ErrInvalidOutput = errors.New("wasmrt: invalid result output")

	// ErrPolicySignatureInvalid 는 PolicyVerifier.Verify 가 서명을 거부했을 때
	// 반환됩니다 (cosign 실패 등).
	ErrPolicySignatureInvalid = errors.New("wasmrt: policy signature invalid")

	// ErrRuntimeClosed 는 닫힌 Runtime에 Evaluate 가 호출됐을 때 반환됩니다.
	ErrRuntimeClosed = errors.New("wasmrt: runtime closed")
)

// PolicyVerifier 는 WASM 정책 바이트 + 서명 바이트의 무결성/출처를 검증하는 인터페이스입니다.
//
// 본 round 에는 nopVerifier (항상 통과) 만 제공됩니다. 후속 round 에서 cosign 어댑터로 교체.
//
// 호출 규약:
//   - policy : 검증 대상 WASM 바이트 전체 (magic 포함).
//   - sig    : 정책에 부착된 detached signature 바이트 (cosign DSSE 또는 bare sig).
//   - 반환   : nil = 통과, 그 외 = ErrPolicySignatureInvalid 를 wrap 한 오류.
type PolicyVerifier interface {
	Verify(policy, sig []byte) error
}

// NopVerifier 는 모든 입력을 통과시키는 PolicyVerifier 입니다 (개발/테스트).
//
// 실 운영에서는 cosign 어댑터로 반드시 교체해야 합니다. 본 type 은 export 되어 있어
// 호출자가 명시적으로 "검증 없음" 을 표현할 수 있습니다 (false-confidence 방지).
type NopVerifier struct{}

// Verify 는 항상 nil 을 반환합니다.
func (NopVerifier) Verify(_, _ []byte) error { return nil }

// validatePolicyBytes 는 WASM 바이트가 최소한의 magic+version prefix 를 갖는지 확인합니다.
//
// 본 함수는 wazero CompileModule 호출 전 빠른 사전 거부를 위한 게이트입니다.
// CompileModule 도 같은 검증을 수행하나, 명확한 sentinel 오류를 위해 분리.
func validatePolicyBytes(policy []byte) error {
	if len(policy) < len(wasmMagic) {
		return fmt.Errorf("%w: too short (%d bytes)", ErrInvalidPolicy, len(policy))
	}
	if !bytes.Equal(policy[:len(wasmMagic)], wasmMagic) {
		return fmt.Errorf("%w: bad magic/version prefix", ErrInvalidPolicy)
	}
	return nil
}
