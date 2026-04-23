package clock_test

import (
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/platform/clock"
)

func TestSystemClockReturnsCurrentTime(t *testing.T) {
	t.Parallel()

	c := clock.System()

	before := time.Now()
	got := c.Now()
	after := time.Now()

	if got.Before(before) || got.After(after) {
		t.Errorf("System().Now() = %v, want within [%v, %v]", got, before, after)
	}
}

func TestClockInjectableFake(t *testing.T) {
	t.Parallel()

	fixed := time.Date(2026, 4, 23, 12, 0, 0, 0, time.UTC)
	fake := clock.NewFake(fixed)

	var c clock.Clock = fake

	if got := c.Now(); !got.Equal(fixed) {
		t.Errorf("FakeClock.Now() = %v, want %v", got, fixed)
	}
}

func TestFakeClockSetReplacesTime(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 4, 23, 12, 0, 0, 0, time.UTC)
	fake := clock.NewFake(start)

	next := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	fake.Set(next)

	if got := fake.Now(); !got.Equal(next) {
		t.Errorf("after Set, Now() = %v, want %v", got, next)
	}
}

func TestFakeClockAdvanceMovesForward(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 4, 23, 12, 0, 0, 0, time.UTC)
	fake := clock.NewFake(start)

	fake.Advance(90 * time.Second)

	want := start.Add(90 * time.Second)
	if got := fake.Now(); !got.Equal(want) {
		t.Errorf("after Advance(90s), Now() = %v, want %v", got, want)
	}

	fake.Advance(2 * time.Hour)
	want = start.Add(90*time.Second + 2*time.Hour)
	if got := fake.Now(); !got.Equal(want) {
		t.Errorf("after second Advance, Now() = %v, want %v", got, want)
	}
}

func TestFakeClockAdvanceRejectsNegative(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Error("Advance with negative duration should panic")
		}
	}()

	fake := clock.NewFake(time.Now())
	fake.Advance(-1 * time.Second)
}

func TestFakeClockIsConcurrencySafe(t *testing.T) {
	t.Parallel()

	fake := clock.NewFake(time.Date(2026, 4, 23, 12, 0, 0, 0, time.UTC))

	const goroutines = 50
	const iterations = 100

	done := make(chan struct{})
	for i := 0; i < goroutines; i++ {
		go func() {
			for j := 0; j < iterations; j++ {
				fake.Advance(time.Millisecond)
				_ = fake.Now()
			}
			done <- struct{}{}
		}()
	}
	for i := 0; i < goroutines; i++ {
		<-done
	}

	want := time.Date(2026, 4, 23, 12, 0, 0, 0, time.UTC).
		Add(time.Duration(goroutines*iterations) * time.Millisecond)
	if got := fake.Now(); !got.Equal(want) {
		t.Errorf("after concurrent Advance, Now() = %v, want %v", got, want)
	}
}
