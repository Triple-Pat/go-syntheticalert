package syntheticalert

import (
	"testing"
	"time"
)

func noError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func equal[T comparable](t *testing.T, want, got T) {
	t.Helper()
	if want != got {
		t.Fatalf("want %v, got %v", want, got)
	}
}

// deterministic returns an alert whose gaps are all exactly gap and whose
// firings last exactly firing: min == mean == max makes the schedule periodic.
func deterministic(t *testing.T, gap, firing time.Duration) *SyntheticAlert {
	t.Helper()
	alert, err := New(
		WithMeanInterval(gap), WithMinInterval(gap), WithMaxInterval(gap),
		WithFiringDuration(firing),
	)
	noError(t, err)
	return alert
}
