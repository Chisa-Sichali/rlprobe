# Tests

A test is a function in a file whose name ends in `_test.go`, in the same package as the code, with the signature `func TestSomething(t *testing.T)`.

## Run them

From the project root:

```text
go test ./...
```

To run one test:

```text
go test ./internal/probe -run TestRampStopsWhenLimited -v
```

`-v` prints the name of each case. `-run` takes a regular expression. `TestRamp` would also match.

## A local server

`TestFixedRecordsLimits` and `TestRampStopsWhenLimited` do not call the internet. `httptest.NewServer` listens on `127.0.0.1` and serves the handler you pass.

`newLimitServer` counts requests with `atomic.Int64`. The first `allow` requests get `200`. The rest get `429`, `Retry-After: 1`, and `X-RateLimit-Remaining: 0`. `atomic` matters because the probe's workers call the server at the same time, and a plain `int` increment would race.

`t.Cleanup(server.Close)` shuts the listener down when the test function returns, including when the test fails.

`collect` calls the real `probe.Run`, reads until `EventDone`, and returns the `Report`. The fixed-mode test asserts three things that should survive small timing changes: at least one request was sent, at least one was limited, and `Retry-After` was stored. It does not assert an exact request count, because a 500 millisecond window can land on either side of a timer tick.

## Table-driven ramp checks

`TestRampStopsWhenLimited` is one function with two cases. The cases live in a slice of structs:

```go
tests := []struct {
	name        string
	allow       int
	alwaysOK    bool
	wantCeiling float64
	wantStopped float64
}{
	{name: "stops after a clean step", allow: 12, wantCeiling: 5, wantStopped: 100},
	{name: "keeps the top rate when nothing is limited", alwaysOK: true, wantCeiling: 100},
}
```

`t.Run(tt.name, func(t *testing.T) { ... })` runs each row as its own subtest. The first server starts returning `429` after 12 requests. The ramp's first pace is 5 requests per second, so that step stays under the allowance and is clean. The next pace is 100 requests per second, crosses the 5% threshold, and stops. The ceiling is `5`. The second row never returns `429`, so the ceiling is the top of the plan, `100`, and `StoppedRate` stays `0`.

Sharing the loop keeps the setup in one place. Adding a case means adding a row, not copying the whole test.

`TestReportTextIncludesLimitedCount` does not start a server. It fills a `Report` and checks `Text` and `JSON`. That is the right size of test for formatting.

## The TUI test

`internal/tui/model_test.go` builds a results model and calls `View()`. Bubble Tea's `View` is a pure function of the model, so the test does not need a terminal. It checks that a ramp ceiling of 15 renders as `15 req/s`. The interactive screens are not driven in `go test`.

## Try this

Add a third row to the ramp table that uses `alwaysOK: true` and expects the ceiling you think `ratePlan(5, 95, 100)` produces. The existing "nothing is limited" row already expects `100`. Run:

```text
go test ./internal/probe -run TestRampStopsWhenLimited -v
```

If your new row expects a different ceiling, the failure output includes `report.Text()`, which prints every step. Read the step lines, fix the expected number, and run the test again. Remove the extra row if you were only exploring.
