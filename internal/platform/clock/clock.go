// Package clock는 시간 의존성을 주입 가능한 형태로 추상화합니다.
// 운영 코드는 Clock 인터페이스를 받고, 테스트는 FakeClock으로 결정론적 시간을 주입합니다.
package clock

import (
	"sync"
	"time"
)

type Clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

func System() Clock { return systemClock{} }

type FakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func NewFake(t time.Time) *FakeClock {
	return &FakeClock{now: t}
}

func (f *FakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *FakeClock) Set(t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = t
}

func (f *FakeClock) Advance(d time.Duration) {
	if d < 0 {
		panic("clock: FakeClock.Advance with negative duration")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}
