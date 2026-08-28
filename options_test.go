package syntheticalert

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDefaultsMatchTheSeries(t *testing.T) {
	equal(t, time.Hour, DefaultMeanInterval)
	equal(t, 10*time.Minute, DefaultMinInterval)
	equal(t, 2*time.Hour, DefaultMaxInterval)
	equal(t, 10*time.Minute, DefaultFiringDuration)
	cfg := defaultConfig()
	noError(t, cfg.validate())
}

func TestOptionsSetConfig(t *testing.T) {
	cfg := defaultConfig()
	for _, opt := range []Option{
		WithMeanInterval(2 * time.Hour),
		WithMinInterval(30 * time.Minute),
		WithMaxInterval(3 * time.Hour),
		WithFiringDuration(20 * time.Minute),
	} {
		noError(t, opt(&cfg))
	}
	equal(t, 2*time.Hour, cfg.meanInterval)
	equal(t, 30*time.Minute, cfg.minInterval)
	equal(t, 3*time.Hour, cfg.maxInterval)
	equal(t, 20*time.Minute, cfg.firingDuration)
	noError(t, cfg.validate())
}

func TestBadOptions(t *testing.T) {
	cases := []struct {
		name string
		want string
		opts []Option
	}{
		{name: "zero mean", want: "mean interval must be positive", opts: []Option{WithMeanInterval(0)}},
		{name: "negative mean", want: "mean interval must be positive", opts: []Option{WithMeanInterval(-time.Second)}},
		{name: "zero min", want: "min interval must be positive", opts: []Option{WithMinInterval(0)}},
		{name: "zero max", want: "max interval must be positive", opts: []Option{WithMaxInterval(0)}},
		{name: "zero firing", want: "firing duration must be positive", opts: []Option{WithFiringDuration(0)}},
		{
			name: "firing not shorter than mean",
			want: "firing duration (1h0m0s) must be less than the mean interval (1h0m0s)",
			opts: []Option{WithFiringDuration(time.Hour)},
		},
		{
			name: "min above mean",
			want: "min interval (1h30m0s) and max interval (2h0m0s) must bracket the mean interval (1h0m0s)",
			opts: []Option{WithMinInterval(90 * time.Minute)},
		},
		{
			name: "max below mean",
			want: "min interval (10m0s) and max interval (30m0s) must bracket the mean interval (1h0m0s)",
			opts: []Option{WithMaxInterval(30 * time.Minute)},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(tc.opts...)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !errors.Is(err, errBadOption) {
				t.Fatalf("error %v is not errBadOption", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not contain %q", err, tc.want)
			}
		})
	}
}

func TestZeroWidthWindowIsLegal(t *testing.T) {
	_, err := New(WithMeanInterval(time.Minute), WithMinInterval(time.Minute),
		WithMaxInterval(time.Minute), WithFiringDuration(time.Second))
	noError(t, err)
}
