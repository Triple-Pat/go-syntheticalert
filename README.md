[![Go Reference](https://pkg.go.dev/badge/github.com/triple-pat/go-syntheticalert.svg)](https://pkg.go.dev/github.com/triple-pat/go-syntheticalert) [![Lint and Test](https://github.com/Triple-Pat/go-syntheticalert/actions/workflows/ci.yml/badge.svg)](https://github.com/Triple-Pat/go-syntheticalert/actions/workflows/ci.yml) [![Coverage Status](https://coveralls.io/repos/github/Triple-Pat/go-syntheticalert/badge.svg?branch=main)](https://coveralls.io/github/Triple-Pat/go-syntheticalert?branch=main)

# go-syntheticalert

Drive a synthetic alert metric from Go, so a
[Triple Pat](https://triplepat.com) check-in timer can verify your alerting
pipeline end to end. Works with Prometheus and OpenTelemetry.

## Why

A broken alerting pipeline looks exactly like a healthy system. No alerts
might mean nothing is wrong, or it might mean your alerting is down, and
your alerting system is the one thing that cannot alert you about itself.

This library provides a time-based callback to drive a synthetic alert
metric. You register the callback as a gauge in your existing metrics
setup, alert on the gauge like any other metric, and route the alert to a
Triple Pat check-in timer. Every delivered alert then becomes a check-in,
and every firing is another fire drill for the whole path from metric to
notification. If the check-ins ever stop, your alerting pipeline is
broken, and the Triple Pat app raises an alarm through a separate channel
to tell you so. An example alert rule and Alertmanager route are below.

## Usage

```sh
go get github.com/triple-pat/go-syntheticalert
```

The library has no dependencies and starts no goroutines. It is a single
value that answers the question "should the synthetic alert be firing
right now?", and you hand its `Value` method to your metrics client as a
gauge callback.

### Prometheus

Alongside your existing Prometheus setup:

```go
alert, err := syntheticalert.New()
if err != nil {
	log.Fatal(err) // only an invalid option can fail
}
promauto.NewGaugeFunc(prometheus.GaugeOpts{
	Name: "triplepat_synthetic_alert",
	Help: "Set to 1 when the synthetic alert should fire and 0 otherwise. " +
		"Alert on this metric and route the alert to a Triple Pat check-in " +
		"timer to continuously test your alerting pipeline.",
}, alert.Value)
```

### OpenTelemetry

The same `alert` value works with OpenTelemetry. The OTel-to-Prometheus
exporter turns the dotted metric name into `triplepat_synthetic_alert`:

```go
gauge, err := meter.Float64ObservableGauge("triplepat.synthetic.alert",
	metric.WithDescription("Set to 1 when the synthetic alert should fire and 0 otherwise."))
if err != nil {
	log.Fatal(err)
}
_, err = meter.RegisterCallback(func(_ context.Context, o metric.Observer) error {
	o.ObserveFloat64(gauge, alert.Value())
	return nil
}, gauge)
```

### The schedule

Each firing holds the gauge at 1 for exactly 10 minutes. The silent gap
between firings, from the end of one to the start of the next, is
memoryless: exponentially distributed with a mean of one hour.

Memoryless gaps make the firings an attempt at a Poisson process, which
cannot synchronize with cron jobs or scrape cycles, and which by the
[PASTA theorem](https://en.wikipedia.org/wiki/Arrival_theorem#Theorem_for_arrivals_governed_by_a_Poisson_process)
sees your pipeline as it typically is rather than at some special moment.

As a nod to practicality the gap is truncated. It is never less than 10
minutes, so the alert visibly resolves between firings, and never more
than two hours, so the check-in timer can be sized. The truncation pulls
the realized mean gap down to about 49 minutes and makes the process only
roughly Poisson. If you need the PASTA property and can tolerate wider
variation in start times, set a lower min and a higher max, then size the
timer for the larger max. That recovers most of the Poisson behavior; for
the last few percent, use a mean much longer than the firing duration,
since the interval between firing starts is the firing plus the gap.

The schedule advances lazily, at scrape time, from the monotonic clock. If
nobody scrapes for a while, the next scrape replays every transition it
missed, so the process stays honest whatever your scrape interval.

There is no magic here: a handful of lines is a serviceable substitute,
firing for the first ten minutes of every hour:

```go
promauto.NewGaugeFunc(prometheus.GaugeOpts{
	Name: "triplepat_synthetic_alert",
	Help: "A synthetic alert metric, firing ten minutes per hour.",
}, func() float64 {
	if time.Now().Minute() < 10 {
		return 1
	}
	return 0
})
```

But that version fires at the top of every hour, exactly when your cron
jobs are doing something interesting. The memoryless schedule cannot
synchronize with anything, and that is the point of the library. If you
want a deterministic schedule anyway, the snippet above is all you need.

### Options

| Option | Effect | Default |
|---|---|---|
| `WithMeanInterval(d)` | Mean silent gap between firings | `1h` |
| `WithMinInterval(d)` | Lower bound on the silent gap | `10m` |
| `WithMaxInterval(d)` | Upper bound on the silent gap | `2h` |
| `WithFiringDuration(d)` | How long each firing holds the gauge at 1 | `10m` |

The firing duration must be shorter than the mean interval, and the min and
max intervals must bracket the mean. Setting all three intervals equal is
allowed: every gap is then exactly that long and the schedule is periodic,
which is pointless in production but handy for deterministic debugging.

## Alert on the metric

```yaml
groups:
  - name: synthetic
    rules:
      - alert: SyntheticAlert
        expr: triplepat_synthetic_alert == 1
        labels:
          severity: synthetic
        annotations:
          summary: Synthetic alert exercising the alerting pipeline.
```

## Route the alert to a check-in timer

Create a check-in timer at [Triple Pat](https://triplepat.com), then point
the alert at it. Prefer email delivery: mail transfer agents queue, retry,
and try every backend listed in DNS, so a check-in email is more likely to
arrive than a single webhook request to a single destination. Send to the
same timer at both the `.com` and `.net` addresses for good measure; extra
check-ins are harmless. Merge this into your existing Alertmanager config
(the fragment assumes you already have a default receiver and working
`smtp_*` defaults):

```yaml
route:
  routes:
    - matchers:
        - alertname="SyntheticAlert"
      receiver: triplepat
      group_wait: 0s
receivers:
  - name: triplepat
    email_configs:
      - to: YOUR-TIMER-UUID@checkin.triplepat.com
        send_resolved: false
      - to: YOUR-TIMER-UUID@checkin.triplepat.net
        send_resolved: false
```

`send_resolved: false` keeps the resolve notification from counting as an
extra check-in, so each firing checks in when it starts and not again when
it resolves.

If you cannot send email, deliver the alert as a webhook instead:

```yaml
receivers:
  - name: triplepat
    webhook_configs:
      - url: https://triplepat.com/api/v1/checkin/YOUR-TIMER-UUID
        send_resolved: false
```

## Sizing the timer

Set the check-in timer's interval to at least
`max interval + firing duration + your alerting pipeline's latency`. With
the defaults (silent gaps of at most two hours, plus 10 minutes of
firing), a three-hour timer is comfortable.

## License

Apache-2.0. See [LICENSE](LICENSE).
