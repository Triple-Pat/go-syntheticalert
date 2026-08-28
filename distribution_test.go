package syntheticalert

import (
	"math"
	"slices"
	"testing"
	"time"
)

const samples = 10_000

// ksCritical is the Kolmogorov-Smirnov critical value at alpha = 0.01 for large N.
var ksCritical = 1.628 / math.Sqrt(samples)

func TestEveryGapLiesWithinBounds(t *testing.T) {
	alert, err := New(WithMeanInterval(100*time.Second), WithMinInterval(50*time.Second),
		WithMaxInterval(150*time.Second), WithFiringDuration(time.Second))
	noError(t, err)
	for range samples {
		if g := alert.gap(); g < 50*time.Second || g > 150*time.Second {
			t.Fatalf("gap %v outside [50s, 150s]", g)
		}
	}
}

func TestZeroWidthWindowIsPeriodic(t *testing.T) {
	alert := deterministic(t, time.Minute, time.Second)
	for range samples {
		equal(t, time.Minute, alert.gap())
	}
}

func TestGapSurvivesAMaxFarBeyondTheMean(t *testing.T) {
	// exp(-max/mean) goes subnormal past about 708 means and is exactly 0
	// beyond about 745; a CDF-space draw would hand log() a zero for the
	// largest random value. Survival-space sampling must stay finite and in
	// bounds in both regions.
	for _, ratio := range []int{730, 1_000_000} {
		maxGap := time.Duration(ratio) * time.Second
		alert, err := New(WithMeanInterval(time.Second), WithMinInterval(time.Second),
			WithMaxInterval(maxGap), WithFiringDuration(time.Second/2))
		noError(t, err)
		for range samples {
			if g := alert.gap(); g < time.Second || g > maxGap {
				t.Fatalf("ratio %d: gap %v outside bounds", ratio, g)
			}
		}
	}
}

// --- Kolmogorov-Smirnov against the truncated exponential ---

func survival(x, mean float64) float64 { return math.Exp(-x / mean) }

// truncatedCDF is the CDF of Exp(mean) truncated to [lo, hi].
func truncatedCDF(x, mean, lo, hi float64) float64 {
	return (survival(lo, mean) - survival(x, mean)) / (survival(lo, mean) - survival(hi, mean))
}

// truncatedQuantile inverts truncatedCDF.
func truncatedQuantile(u, mean, lo, hi float64) float64 {
	s := survival(lo, mean) - u*(survival(lo, mean)-survival(hi, mean))
	return -mean * math.Log(s)
}

// ksStatistic is the largest distance between the empirical CDF of xs and the
// truncated exponential CDF.
func ksStatistic(xs []float64, mean, lo, hi float64) float64 {
	xs = slices.Clone(xs)
	slices.Sort(xs)
	n := float64(len(xs))
	d := 0.0
	for i, x := range xs {
		f := truncatedCDF(x, mean, lo, hi)
		d = max(d, math.Abs(float64(i+1)/n-f), math.Abs(float64(i)/n-f))
	}
	return d
}

// window is an alert's gap distribution parameters in seconds.
type window struct{ mean, lo, hi float64 }

func windowOf(alert *SyntheticAlert) window {
	return window{alert.mean.Seconds(), alert.minGap.Seconds(), alert.maxGap.Seconds()}
}

// A correct sampler is rejected about 1% of the time by construction; three
// attempts bring the false-failure rate to ~1e-6. The wrong samplers in
// TestKSRejectsWrongDistributions are rejected every time.
func TestGapsAreMemoryless(t *testing.T) {
	alert, err := New()
	noError(t, err)
	w := windowOf(alert)
	var last float64
	for attempt := range 3 {
		xs := make([]float64, samples)
		for i := range xs {
			xs[i] = alert.gap().Seconds()
		}
		last = ksStatistic(xs, w.mean, w.lo, w.hi)
		if last <= ksCritical {
			return
		}
		t.Logf("attempt %d: K-S statistic %.4f exceeds %.4f", attempt+1, last, ksCritical)
	}
	t.Fatalf("K-S rejected the sampler on all attempts; last statistic %.4f", last)
}

// quantileGrid is a noise-free stand-in for a sample: evenly spaced quantiles.
func quantileGrid(f func(u float64) float64) []float64 {
	xs := make([]float64, samples)
	for i := range xs {
		xs[i] = f((float64(i) + 0.5) / samples)
	}
	return xs
}

func TestKSRejectsWrongDistributions(t *testing.T) {
	alert, err := New()
	noError(t, err)
	w := windowOf(alert)
	wrong := map[string]func(u float64) float64{
		// A sampler that clamps instead of truncating piles mass on the bounds.
		"clamped":  func(u float64) float64 { return min(max(-w.mean*math.Log(1-u), w.lo), w.hi) },
		"uniform":  func(u float64) float64 { return w.lo + u*(w.hi-w.lo) },
		"mean+25%": func(u float64) float64 { return truncatedQuantile(u, w.mean*1.25, w.lo, w.hi) },
	}
	for name, f := range wrong {
		t.Run(name, func(t *testing.T) {
			if d := ksStatistic(quantileGrid(f), w.mean, w.lo, w.hi); d <= ksCritical {
				t.Fatalf("K-S failed to reject %s: statistic %.4f", name, d)
			}
		})
	}
}

func TestKSAcceptsTheRightDistribution(t *testing.T) {
	alert, err := New()
	noError(t, err)
	w := windowOf(alert)
	right := func(u float64) float64 { return truncatedQuantile(u, w.mean, w.lo, w.hi) }
	if d := ksStatistic(quantileGrid(right), w.mean, w.lo, w.hi); d > ksCritical {
		t.Fatalf("K-S rejected the right distribution: statistic %.4f", d)
	}
}
