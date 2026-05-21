package signer_test

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ssabro/rosshield/internal/platform/signer"
	"github.com/ssabro/rosshield/internal/platform/signer/soft"
)

func newSoftSigner(t *testing.T) signer.Signer {
	t.Helper()
	s, err := soft.New()
	if err != nil {
		t.Fatalf("soft.New: %v", err)
	}
	return s
}

func TestSwappableSignerBasicSignReturnsEpoch(t *testing.T) {
	t.Parallel()
	initial := newSoftSigner(t)
	ss := signer.NewSwappableSigner(initial, 1)

	payload := []byte("audit checkpoint payload")
	sig, epoch, keyID, err := ss.SignWithEpoch(payload)
	if err != nil {
		t.Fatalf("SignWithEpoch: %v", err)
	}
	if epoch != 1 {
		t.Errorf("epoch = %d, want 1", epoch)
	}
	if keyID != initial.KeyID() {
		t.Errorf("keyID = %q, want %q", keyID, initial.KeyID())
	}
	if err := ss.Verify(payload, sig); err != nil {
		t.Errorf("Verify: %v", err)
	}
}

func TestSwappableSignerSignWithoutEpochCompat(t *testing.T) {
	t.Parallel()
	initial := newSoftSigner(t)
	ss := signer.NewSwappableSigner(initial, 7)

	// Signer interface 호환 path — Sign 은 (sig, keyID, err) 만 반환.
	sig, keyID, err := ss.Sign([]byte("hello"))
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if keyID != initial.KeyID() {
		t.Errorf("keyID = %q, want %q", keyID, initial.KeyID())
	}
	if len(sig) == 0 {
		t.Error("sig empty")
	}
}

func TestSwappableSignerSwapReplacesEpochAndKey(t *testing.T) {
	t.Parallel()
	a := newSoftSigner(t)
	b := newSoftSigner(t)
	if a.KeyID() == b.KeyID() {
		t.Fatal("test setup: a and b should have distinct keyIDs")
	}

	ss := signer.NewSwappableSigner(a, 1)
	if ss.CurrentEpoch() != 1 {
		t.Fatalf("CurrentEpoch before swap = %d, want 1", ss.CurrentEpoch())
	}
	if ss.CurrentKeyID() != a.KeyID() {
		t.Errorf("CurrentKeyID before swap = %q, want %q", ss.CurrentKeyID(), a.KeyID())
	}

	ss.Swap(b, 2)
	if ss.CurrentEpoch() != 2 {
		t.Errorf("CurrentEpoch after swap = %d, want 2", ss.CurrentEpoch())
	}
	if ss.CurrentKeyID() != b.KeyID() {
		t.Errorf("CurrentKeyID after swap = %q, want %q", ss.CurrentKeyID(), b.KeyID())
	}

	// 새 Sign 은 b 의 keyID 반환.
	_, epoch, keyID, err := ss.SignWithEpoch([]byte("after swap"))
	if err != nil {
		t.Fatalf("SignWithEpoch: %v", err)
	}
	if epoch != 2 {
		t.Errorf("epoch after swap = %d, want 2", epoch)
	}
	if keyID != b.KeyID() {
		t.Errorf("keyID after swap = %q, want %q", keyID, b.KeyID())
	}
}

func TestSwappableSignerEpochMonotonicAfterMultipleSwaps(t *testing.T) {
	t.Parallel()
	ss := signer.NewSwappableSigner(newSoftSigner(t), 1)
	prev := ss.CurrentEpoch()
	for i := int64(2); i <= 5; i++ {
		ss.Swap(newSoftSigner(t), i)
		if got := ss.CurrentEpoch(); got != i {
			t.Errorf("epoch after Swap(%d) = %d, want %d", i, got, i)
		}
		if got := ss.CurrentEpoch(); got <= prev {
			t.Errorf("epoch not monotonic: %d <= prev %d", got, prev)
		}
		prev = ss.CurrentEpoch()
	}
}

// Race test: 100 goroutine 동시 Sign + 동시 Swap. race detector 활성 시 데이터 race 0 확인.
func TestSwappableSignerConcurrentSignAndSwapRace(t *testing.T) {
	t.Parallel()

	ss := signer.NewSwappableSigner(newSoftSigner(t), 1)

	var wg sync.WaitGroup
	const signersCount = 100
	const swapCount = 20

	// 다수의 signer 동시 sign.
	var signErrs atomic.Int64
	for i := 0; i < signersCount; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				if _, _, _, err := ss.SignWithEpoch([]byte{byte(i), byte(j)}); err != nil {
					signErrs.Add(1)
				}
			}
		}(i)
	}

	// 동시 swap.
	for i := int64(2); i < 2+swapCount; i++ {
		wg.Add(1)
		go func(epoch int64) {
			defer wg.Done()
			ss.Swap(newSoftSigner(t), epoch)
		}(i)
	}

	wg.Wait()

	if signErrs.Load() != 0 {
		t.Errorf("got %d sign errors during race test", signErrs.Load())
	}
	if got := ss.CurrentEpoch(); got < 2 {
		t.Errorf("final epoch = %d, want >= 2", got)
	}
}

func TestSwappableSignerSwapNilPanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Error("Swap(nil) should panic")
		}
	}()
	ss := signer.NewSwappableSigner(newSoftSigner(t), 1)
	ss.Swap(nil, 2)
}

func TestNewSwappableSignerNilInitialPanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewSwappableSigner(nil, ...) should panic")
		}
	}()
	_ = signer.NewSwappableSigner(nil, 1)
}

func TestSwappableSignerInterfaceCompat(t *testing.T) {
	t.Parallel()
	// 컴파일 시점 / 런타임 둘 다 interface 만족 확인.
	var s signer.Signer = signer.NewSwappableSigner(newSoftSigner(t), 1)
	if s.KeyID() == "" {
		t.Error("KeyID via Signer interface empty")
	}
}
