# Concurrency

A fixed run has to keep a steady pace and also wait on network calls that finish at different times. Go's tools for that are goroutines, channels, and `context`.

## The runner publishes events

`probe.Run` does not return the final report directly. It returns a channel:

```go
func Run(ctx context.Context, cfg Config) (<-chan Event, error) {
```

`<-chan Event` means "a channel you can only receive from". The CLI ranges over it until it closes. The TUI receives one event at a time and redraws. Both see the same `Event` values: a step change, one result, or the final report (`EventDone`).

Inside `Run`, a goroutine does the work and closes the channel when it finishes. This is the real function, shortened only by leaving the `switch` to your reading of the file:

```go
events := make(chan Event, 256)
go func() {
	defer close(events)
	client := newClient(cfg)
	stats := newStats(cfg)
	switch cfg.Mode {
	case ModeFixed:
		runWindow(ctx, client, cfg, cfg.Rate, cfg.Duration, stats, events)
	case ModeRamp:
		runRamp(ctx, client, cfg, stats, events)
	}
	report := stats.finish(ctx.Err() != nil)
	events <- Event{Kind: EventDone, Report: &report}
}()
```

`go` starts that function on another goroutine. `main` can keep reading while requests are still in flight. `defer close(events)` runs when the function returns, which tells the reader that nothing else is coming.

## Workers and a job channel

Open `runWindow` in `internal/probe/runner.go`. One goroutine, the scheduler, decides when a request may start. A pool of workers performs them.

```go
jobs := make(chan struct{})
var wg sync.WaitGroup
for i := 0; i < cfg.Concurrency; i++ {
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range jobs {
			result := perform(ctx, client, cfg, target)
```

`make(chan struct{})` is a channel that carries no data. Sending on it means "do one request". The channel is unbuffered, so a send waits until a worker is ready to receive. That caps how many requests are in flight at `cfg.Concurrency`.

`WaitGroup` counts live workers. `Add(1)` before each `go`, `Done()` when that goroutine returns. After the window, `close(jobs)` makes `for range jobs` end, and `wg.Wait()` blocks until every worker has finished. The ramp uses that so a step's totals are complete before the next pace starts.

## The pace

`golang.org/x/time/rate` hands out permission to send. Burst is `1`, so the probe does not fire a clump of requests at the start of a window.

```go
limiter := rate.NewLimiter(rate.Limit(target), 1)
reservation := limiter.Reserve()
delay := reservation.Delay()
```

`delay` is how long to sleep before the next request. If that sleep would end after the window, the reservation is cancelled and the scheduler stops. `sleepCtx` uses a timer and also watches `ctx.Done()`, so Ctrl+C does not wait out the full delay.

Workers skip a result when the error is `context.Canceled`. Those calls were aborted because you stopped the run. They are not counted as API errors.

## Shared totals

Several workers call `stats.add` at once. `stats` holds a `sync.Mutex`. `add` locks, updates the counts, and unlocks. Without the lock, two goroutines writing `Sent` at the same time would be a data race. `go test` does not always catch that. `go test -race ./internal/probe` runs the tests with the race detector and is worth using when you change `stats`.

## The ramp

`runRamp` walks the paces from `ratePlan`. After each window it calls `crossed`. The default threshold is `0.05`. If the limited share of that step is at least 5%, the ramp stops. The ceiling is the previous pace that still had real traffic and stayed under the line. If the first step is already over the line, the ceiling is none.

## Try this

In `runWindow`, the worker count is `cfg.Concurrency`. Run the tests with the race detector:

```text
go test -race ./internal/probe
```

The first race-enabled build is slower because the compiler instruments memory access. A passing run means the tests did not observe a conflicting write.
