// Package idgen은 도메인 식별자 생성을 담당합니다.
// ULID(Crockford base32) 본체에 `<prefix>_` 접두사를 붙여 로그·UI에서 타입을 즉시 구분합니다.
//
// 예: `ro_01H8Z7C9R5T3MFXJEY8Q1A6P4V` (robot), `ss_01H...` (scan session).
package idgen

import (
	"crypto/rand"
	"sync"

	"github.com/oklog/ulid/v2"
)

type IDGen interface {
	New(prefix string) string
}

type ulidGen struct {
	mu      sync.Mutex
	entropy *ulid.MonotonicEntropy
}

// NewULID는 시스템 시간 + crypto/rand 엔트로피를 사용하는 IDGen을 반환합니다.
// 동일 millisecond 내에서 monotonic ordering을 보장합니다.
func NewULID() IDGen {
	return &ulidGen{
		entropy: ulid.Monotonic(rand.Reader, 0),
	}
}

func (g *ulidGen) New(prefix string) string {
	g.mu.Lock()
	id := ulid.MustNew(ulid.Now(), g.entropy)
	g.mu.Unlock()

	if prefix == "" {
		return id.String()
	}
	return prefix + "_" + id.String()
}
