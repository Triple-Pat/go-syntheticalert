// Package syntheticalert emits a synthetic alert measurement so you can prove,
// continuously and in production, that your alerting pipeline works.
//
// A SyntheticAlert's Value method is 1 while the synthetic alert should be
// firing and 0 otherwise, on a memoryless schedule. Hand Value to whichever
// metrics client you already use, alert on the resulting gauge, and route that
// alert to a Triple Pat check-in timer (https://triplepat.com); every delivered
// alert becomes a check-in, and the timer raises an alarm if the alerts stop
// arriving — the one failure your alerting system cannot report about itself.
//
// Prometheus:
//
//	alert, err := syntheticalert.New()
//	promauto.NewGaugeFunc(prometheus.GaugeOpts{Name: "triplepat_synthetic_alert", Help: "..."}, alert.Value)
//
// OpenTelemetry:
//
//	gauge, _ := meter.Float64ObservableGauge("triplepat.synthetic.alert")
//	meter.RegisterCallback(func(_ context.Context, o metric.Observer) error {
//		o.ObserveFloat64(gauge, alert.Value())
//		return nil
//	}, gauge)
//
// The library owns no metric, starts no goroutine, and has no dependencies.
package syntheticalert

import (
	"fmt"
	"math"
	"math/rand/v2"
	"sync"
	"time"
)

// SyntheticAlert is a measurement callback for a synthetic alert.
//
// Each firing holds Value at 1 for exactly the firing duration. The silent
// gap between firings, from the end of one to the start of the next, is
// exponentially distributed (memoryless) with the configured mean: an attempt
// at a Poisson process, which cannot synchronize with cron jobs or scrape
// cycles. As a nod to practicality the gap is truncated to the configured min
// and max, which makes the process only roughly Poisson; widen the bounds to
// get closer.
//
// The schedule advances lazily: nothing happens until Value is called, at
// which point every transition up to time.Now() is replayed. Value is safe
// for concurrent use.
type SyntheticAlert struct {
	next           time.Time
	mean           time.Duration
	minGap         time.Duration
	maxGap         time.Duration
	firingDuration time.Duration
	mu             sync.Mutex
	firing         bool
}

// New returns a SyntheticAlert with the given options applied to the
// defaults. It returns an error if an option is invalid.
func New(opts ...Option) (*SyntheticAlert, error) {
	cfg := defaultConfig()
	for _, opt := range opts {
		err := opt(&cfg)
		if err != nil {
			return nil, fmt.Errorf("applying option: %w", err)
		}
	}
	err := cfg.validate()
	if err != nil {
		return nil, fmt.Errorf("validating options: %w", err)
	}
	a := &SyntheticAlert{
		mean:           cfg.meanInterval,
		minGap:         cfg.minInterval,
		maxGap:         cfg.maxInterval,
		firingDuration: cfg.firingDuration,
	}
	a.next = time.Now().Add(a.gap())
	return a, nil
}

// Value returns 1 if the synthetic alert should be firing right now and 0
// otherwise. It replays every schedule transition between the last call and
// now, so the realized schedule is the same whatever the scrape cadence.
func (a *SyntheticAlert) Value() float64 {
	// Why carry state and replay transitions, rather than compute the state
	// from the clock alone?
	//
	// A stateless answer to "is a firing in progress?" needs the firing times
	// to be a pure function of wall-clock time. That is possible for a plain
	// Poisson process, because it has independent increments: chop time into
	// epochs, seed a PRNG from the epoch index, draw that epoch's arrivals, and
	// check whether one falls within the last firing duration. It has a real
	// attraction, too: every replica of a service would compute the same
	// schedule and raise one alert instead of N.
	//
	// But the min and max bounds on the silent gap make each gap depend on
	// where the previous firing ended, which destroys independent increments;
	// epochs can no longer be generated in isolation. Thinning and back-filling
	// a plain Poisson stream to fake the bounds would have to peek across epoch
	// boundaries and would no longer have a distribution the tests can name.
	// The bounds exist for practical reasons (the alert must visibly resolve;
	// the check-in timer must not false-alarm), so we honor them exactly with
	// an alternating renewal process: fixed firings, i.i.d. truncated-
	// exponential gaps, and a few words of state.
	//
	// Replaying every missed transition, rather than jumping to the current
	// state, keeps the realized schedule identical whatever the scrape cadence.
	// It costs one loop iteration per elapsed transition, about fifty a day at
	// the defaults, so even a scrape after a week of silence is trivial.
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	for !now.Before(a.next) {
		a.firing = !a.firing
		if a.firing {
			a.next = a.next.Add(a.firingDuration)
		} else {
			a.next = a.next.Add(a.gap())
		}
	}
	if a.firing {
		return 1
	}
	return 0
}

// gap draws one silent gap from the exponential distribution with mean
// a.mean, truncated to [a.minGap, a.maxGap], by inverse-CDF sampling: pick a
// uniform point within the probability mass the exponential puts on the
// window, then map it back through the exponential's quantile function. One
// draw, exact shape, and the bounds hold literally.
//
// This is deliberately bespoke and single-use, per the series philosophy of
// reimplementing the memoryless sampler in each library.
func (a *SyntheticAlert) gap() time.Duration {
	// Work with the survival function S(x) = exp(-x / mean), which is strictly
	// positive at minGap (minGap <= mean, so the exponent is at least -1) but
	// underflows to exactly 0 when maxGap is hundreds of means away.
	// rand.Float64 is in [0, 1), so 1 - rand.Float64() is in (0, 1] and u lands
	// in (sMax, sMin]: never equal to sMax, so math.Log never sees 0.
	sMax := math.Exp(-float64(a.maxGap) / float64(a.mean))
	sMin := math.Exp(-float64(a.minGap) / float64(a.mean))
	//nolint:gosec // Statistical sampling, not crypto: math/rand/v2 is the right tool.
	u := sMax + (1-rand.Float64())*(sMin-sMax)
	d := time.Duration(-float64(a.mean) * math.Log(u))
	// Mathematically d is already in [minGap, maxGap): this is not clamping a
	// distribution, it corrects the few ulps by which exp followed by log can
	// miss a round trip, so the bounds hold literally rather than to within
	// floating-point rounding.
	return min(max(d, a.minGap), a.maxGap)
}
