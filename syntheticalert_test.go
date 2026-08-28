package syntheticalert

import (
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

const epsilon = time.Nanosecond

func TestStartsResolved(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		alert := deterministic(t, time.Minute, 10*time.Second)
		equal(t, 0.0, alert.Value())
	})
}

func TestFiresAfterOneGapThenResolves(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		alert := deterministic(t, time.Minute, 10*time.Second)
		for cycle := range 100 {
			time.Sleep(time.Minute - epsilon)
			if alert.Value() != 0 {
				t.Fatalf("cycle %d: firing just before the gap elapsed", cycle)
			}
			time.Sleep(epsilon)
			if alert.Value() != 1 {
				t.Fatalf("cycle %d: not firing at the end of the gap", cycle)
			}
			time.Sleep(10*time.Second - epsilon)
			if alert.Value() != 1 {
				t.Fatalf("cycle %d: resolved before the firing duration elapsed", cycle)
			}
			time.Sleep(epsilon)
			if alert.Value() != 0 {
				t.Fatalf("cycle %d: still firing after the firing duration", cycle)
			}
		}
	})
}

func TestGapIsMeasuredFromEndOfFiring(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		alert, err := New()
		noError(t, err)
		time.Sleep(time.Until(alert.next))
		equal(t, 1.0, alert.Value())
		resolvedAt := alert.next
		time.Sleep(time.Until(resolvedAt))
		equal(t, 0.0, alert.Value())
		gap := alert.next.Sub(resolvedAt)
		if gap < DefaultMinInterval || gap > DefaultMaxInterval {
			t.Fatalf("gap %v outside [%v, %v]", gap, DefaultMinInterval, DefaultMaxInterval)
		}
	})
}

func TestLongPauseReplaysToOneTransitionAhead(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		alert, err := New()
		noError(t, err)
		time.Sleep(10 * 24 * time.Hour)
		v := alert.Value()
		if v != 0 && v != 1 {
			t.Fatalf("value %v", v)
		}
		now := time.Now()
		if !alert.next.After(now) {
			t.Fatalf("next transition %v is not after now %v", alert.next, now)
		}
		if alert.next.Sub(now) > DefaultMaxInterval+DefaultFiringDuration {
			t.Fatalf("next transition %v is more than one cycle ahead of %v", alert.next, now)
		}
	})
}

func TestConcurrentValueCallsAgree(t *testing.T) {
	// Real clock, tiny durations, run under -race: the lock must keep every
	// goroutine's view consistent while the schedule churns.
	alert, err := New(WithMeanInterval(2*time.Millisecond), WithMinInterval(time.Millisecond),
		WithMaxInterval(4*time.Millisecond), WithFiringDuration(time.Millisecond))
	noError(t, err)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 1000 {
				if v := alert.Value(); v != 0 && v != 1 {
					t.Errorf("value %v", v)
				}
			}
		})
	}
	wg.Wait()
}
