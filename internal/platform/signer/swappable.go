// Package signer — SwappableSigner는 hot-swap 가능한 Signer wrapper입니다 (E33 / Phase 10.D-4).
//
// 설계: docs/design/notes/audit-chain-rotation-automation-design.md §6.4 + §12.1.
//
// 동작:
//   - Sign 호출은 RLock 으로 직렬화 — 동시 sign 다수 OK, in-flight 시 Swap 은 모든 RLock
//     해제까지 대기 (queue 패턴, D-P10D-3 결정).
//   - Swap 은 Lock — 새 Signer 교체 + 새 epoch 기록. RWMutex 의도상 in-flight Sign 이
//     완료된 후에만 교체 수행.
//
// Signer interface 호환성 (옵션 A 채택):
//   - 본 wrapper 는 signer.Signer interface 를 그대로 implement — 기존 사용처 변경 0.
//   - audit chain 서명에 epoch 가 필요한 경우 SignWithEpoch / CurrentEpoch / CurrentKeyID 추가
//     메서드를 별도로 호출. 다른 사용처(jwt·report·license)는 기존 Sign 패턴 유지.
package signer

import "sync"

// SwappableSigner 는 hot-swap 가능한 Signer wrapper 입니다.
//
// 단일 활성 Signer + 현재 epoch 보존. atomic swap 시 in-flight Sign 은 자연 직렬화 (queue).
type SwappableSigner struct {
	mu     sync.RWMutex
	active Signer
	epoch  int64
}

// NewSwappableSigner 는 initial signer 와 초기 epoch 로 SwappableSigner 를 만듭니다.
//
// initial 은 non-nil 필수. initialEpoch 는 ≥ 1 권장 (epoch=1 부트스트랩 일관).
func NewSwappableSigner(initial Signer, initialEpoch int64) *SwappableSigner {
	if initial == nil {
		panic("signer: NewSwappableSigner requires non-nil initial signer")
	}
	return &SwappableSigner{active: initial, epoch: initialEpoch}
}

// Sign 은 활성 Signer 로 payload 를 서명합니다 (Signer interface).
//
// 동시 다수 호출 OK. Swap 시 모든 in-flight Sign 이 완료된 후에 교체.
func (s *SwappableSigner) Sign(payload []byte) ([]byte, string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.active.Sign(payload)
}

// SignWithEpoch 는 활성 Signer 로 payload 를 서명하고 현재 epoch 도 함께 반환합니다.
//
// audit chain Append 가 entry 당 epoch 기록할 때 사용. 다른 사용처(jwt·report·license)는
// 기본 Sign 사용 — 기존 호환.
func (s *SwappableSigner) SignWithEpoch(payload []byte) (sig []byte, epoch int64, keyID string, err error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sig, keyID, err = s.active.Sign(payload)
	epoch = s.epoch
	return
}

// Verify 는 활성 Signer 로 payload·sig 무결성을 검증합니다 (Signer interface).
//
// 다른 epoch 의 서명을 검증할 때는 본 wrapper 가 아닌 epoch 별 public key 로 외부 검증 필요.
func (s *SwappableSigner) Verify(payload, sig []byte) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.active.Verify(payload, sig)
}

// PublicKey 는 활성 Signer 의 public key raw bytes 를 반환합니다.
func (s *SwappableSigner) PublicKey() []byte {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.active.PublicKey()
}

// KeyID 는 활성 Signer 의 KeyID 를 반환합니다 (Signer interface).
func (s *SwappableSigner) KeyID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.active.KeyID()
}

// CurrentEpoch 는 현재 활성 epoch 를 반환합니다.
func (s *SwappableSigner) CurrentEpoch() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.epoch
}

// CurrentKeyID 는 KeyID 동등 메서드입니다 (도메인 호환 명시).
func (s *SwappableSigner) CurrentKeyID() string {
	return s.KeyID()
}

// Swap 은 활성 Signer 를 교체합니다 (hot-swap, queue 패턴).
//
// RWMutex Lock 으로 in-flight Sign 완료까지 대기 + atomic 교체. newSigner non-nil 필수.
// newEpoch 는 단조 증가 권장 (도메인 layer 가 enforce, 본 wrapper 는 값을 그대로 저장).
func (s *SwappableSigner) Swap(newSigner Signer, newEpoch int64) {
	if newSigner == nil {
		panic("signer: SwappableSigner.Swap requires non-nil signer")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.active = newSigner
	s.epoch = newEpoch
}

// 컴파일 시점 인터페이스 매칭 보증.
var _ Signer = (*SwappableSigner)(nil)
