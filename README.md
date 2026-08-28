# go-syntheticalert

Emit a synthetic alert metric from Go, so a
[Triple Pat](https://triplepat.com) check-in timer can verify your alerting
pipeline end to end. Works with Prometheus and OpenTelemetry.

## Why

A broken alerting pipeline looks exactly like a healthy system. No alerts
might mean nothing is wrong, or it might mean your alerting is down, and
your alerting system is the one thing that cannot alert you about itself.

This library gives your monitoring a synthetic alert metric that fires on a
schedule. You alert on that metric like any other (example alert rule and
Alertmanager route below) and route the alert to a Triple Pat check-in
timer, so every delivered alert becomes a check-in. If the check-ins stop
arriving, the timer raises an alarm through a separate channel. Your
alerting pipeline is broken. It is an automated fire drill for the whole
path from metric to notification.

## Usage

```sh
go get github.com/triple-pat/go-syntheticalert
```

The library has no dependencies and starts no goroutines. It is a single
value that answers "should the synthetic alert be firing right now?" and
you hand its `Value` method to your metrics client as a gauge callback.
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

Or with OpenTelemetry (the OTel-to-Prometheus exporter turns the dotted name
into `triplepat_synthetic_alert`):

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

Each firing holds the gauge at 1 for exactly 10 minutes. The silent gap
between firings — from the end of one to the start of the next — is
memoryless: exponentially distributed with a mean of one hour. That makes
the firings an attempt at a Poisson process, which cannot synchronize with
cron jobs or scrape cycles, and which by the
[PASTA theorem](https://en.wikipedia.org/wiki/Arrival_theorem#Theorem_for_arrivals_governed_by_a_Poisson_process)
sees your pipeline as it typically is rather than at some special moment.
As a nod to practicality the gap is truncated: never less than 10 minutes,
so the alert visibly resolves between firings, and never more than two
hours, so the check-in timer can be sized. The truncation pulls the
realized mean gap down to about 49 minutes and makes the process only
roughly Poisson. If you need the PASTA property and can tolerate wider
variation in start times, set a lower min and a higher max (and, for the
last few percent, a longer mean relative to the firing duration), then size
the timer for the larger max.

The schedule advances lazily, at scrape time. If nobody scrapes for a
while, the next scrape replays every transition it missed, so the process
stays honest whatever your scrape interval.

There is no magic here. A handful of lines is a serviceable substitute,
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
jobs are doing something interesting. The point of the library is the
memoryless schedule, which cannot synchronize with anything. If you want a
deterministic schedule anyway, the snippet above is all you need.

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
the alert at it. Merge this into your existing Alertmanager config (the
fragment assumes you already have a default receiver). As a webhook:

```yaml
route:
  routes:
    - matchers:
        - alertname="SyntheticAlert"
      receiver: triplepat
      group_wait: 0s
receivers:
  - name: triplepat
    webhook_configs:
      - url: https://triplepat.com/api/v1/checkin/YOUR-TIMER-UUID
        send_resolved: false
```

`send_resolved: false` keeps the resolve notification from counting as a
second check-in, so one firing is one check-in.

Alternatively, deliver the alert by email to
`YOUR-TIMER-UUID@checkin.triplepat.com`, with `send_resolved: false` on the
`email_configs` entry for the same reason.

## Sizing the timer

Set the check-in timer's interval to at least
`max interval + firing duration + your alerting pipeline's latency`. With
the defaults (silent gaps of at most two hours, plus 10 minutes of
firing), a three-hour timer is comfortable.

## License

Apache-2.0. See [LICENSE](LICENSE).
