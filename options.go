package syntheticalert

import (
	"errors"
	"fmt"
	"time"
)

// Defaults for the memoryless schedule, overridable with WithMeanInterval,
// WithMinInterval, WithMaxInterval, and WithFiringDuration. The max bound
// exists because an unbounded exponential distribution occasionally produces
// gaps long enough to false-alarm the check-in timer this library exists to
// feed; the min bound guarantees the alert visibly resolves between firings.
const (
	DefaultMeanInterval   = time.Hour
	DefaultMinInterval    = 10 * time.Minute
	DefaultMaxInterval    = 2 * time.Hour
	DefaultFiringDuration = 10 * time.Minute
)

var errBadOption = errors.New("bad syntheticalert option")

// Option configures New.
type Option func(*config) error

type config struct {
	meanInterval   time.Duration
	minInterval    time.Duration
	maxInterval    time.Duration
	firingDuration time.Duration
}

func defaultConfig() config {
	return config{
		meanInterval:   DefaultMeanInterval,
		minInterval:    DefaultMinInterval,
		maxInterval:    DefaultMaxInterval,
		firingDuration: DefaultFiringDuration,
	}
}

// WithMeanInterval sets the mean of the exponential distribution the silent
// gaps are drawn from, before truncation to the min and max bounds. The gap is
// measured from the end of one firing to the start of the next. Truncation
// pulls the realized average toward the window's interior: with the defaults
// it is about 49 minutes, not an hour.
func WithMeanInterval(d time.Duration) Option {
	return func(c *config) error {
		if d <= 0 {
			return fmt.Errorf("%w: mean interval must be positive, got %v", errBadOption, d)
		}
		c.meanInterval = d
		return nil
	}
}

// WithMinInterval sets a lower bound on the silent gap between firings,
// guaranteeing the alert stays resolved at least that long. It must not exceed
// the mean interval. Setting min, mean, and max all equal is allowed: every gap
// is then exactly that long and the schedule is periodic, which is pointless
// in production but handy for deterministic debugging.
func WithMinInterval(d time.Duration) Option {
	return func(c *config) error {
		if d <= 0 {
			return fmt.Errorf("%w: min interval must be positive, got %v", errBadOption, d)
		}
		c.minInterval = d
		return nil
	}
}

// WithMaxInterval sets an upper bound on the silent gap between firings. It
// must be at least the mean interval. See WithMinInterval for the all-equal
// case.
func WithMaxInterval(d time.Duration) Option {
	return func(c *config) error {
		if d <= 0 {
			return fmt.Errorf("%w: max interval must be positive, got %v", errBadOption, d)
		}
		c.maxInterval = d
		return nil
	}
}

// WithFiringDuration sets how long Value reports 1 during each firing. It must
// be shorter than the mean interval between firings.
func WithFiringDuration(d time.Duration) Option {
	return func(c *config) error {
		if d <= 0 {
			return fmt.Errorf("%w: firing duration must be positive, got %v", errBadOption, d)
		}
		c.firingDuration = d
		return nil
	}
}

func (c *config) validate() error {
	if c.firingDuration >= c.meanInterval {
		return fmt.Errorf("%w: firing duration (%v) must be less than the mean interval (%v)",
			errBadOption, c.firingDuration, c.meanInterval)
	}
	if c.minInterval > c.meanInterval || c.meanInterval > c.maxInterval {
		return fmt.Errorf("%w: min interval (%v) and max interval (%v) must bracket the mean interval (%v)",
			errBadOption, c.minInterval, c.maxInterval, c.meanInterval)
	}
	return nil
}
