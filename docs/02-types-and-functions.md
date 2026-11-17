# Types and functions

Go groups data in structs and gives behavior to those structs with methods. Errors are normal return values, not a separate control-flow feature.

## Config is the input

Open `internal/probe/runner.go` and find `Config`. It is the form both programs fill in: URL, method, headers, pace, and how long to run.

```go
type Mode string

const (
	ModeFixed Mode = "fixed"
	ModeRamp  Mode = "ramp"
)
```

`Mode` is a string with a new name. `ModeFixed` and `ModeRamp` are the only values `Validate` accepts. A named type stops the rest of the program from passing an arbitrary string without you noticing at the call site.

`Validate` is a method because the receiver is written before the name:

```go
func (c *Config) Validate() error {
```

The `*` means the method can update the struct. When timeout, concurrency, or the limit threshold is zero, `Validate` fills the default. It returns an `error` when the URL is missing or a cap is exceeded. `nil` means success. Callers write:

```go
if err := cfg.Validate(); err != nil {
	return nil, err
}
```

That pattern is the normal way Go reports failure. `probe.Run` does this before it starts any requests, which is why a bad flag fails immediately instead of halfway through a run.

## Report is the output

Open `internal/probe/report.go`. `Report` holds the totals: sent, limited, errors, latency percentiles, headers, and, for a ramp, the ceiling.

`Text` and `JSON` are methods with a value receiver:

```go
func (r Report) Text() string {
```

A value receiver gets a copy. That is a good fit here because printing must not change the numbers that were collected. The CLI calls `report.Text()` unless `--json` is set, in which case it calls `report.JSON()`.

The TUI results screen prints `m.report.Text()` as well, in `internal/tui/run.go`. One method, two programs.

## Functions that are not methods

`FormatRate` is a plain function in the same file. It does not need a `Report` to do its job, so it has no receiver. `crossed` in `runner.go` is the same kind of function: given a sent count, a limited count, and a threshold, it returns true or false.

```go
func crossed(sent, limited int, threshold float64) bool {
	if sent == 0 {
		return false
	}
	return float64(limited)/float64(sent) >= threshold
}
```

`float64(limited)` converts an int to a decimal so the division is not integer division. In integer math, `1/20` is `0`. As decimals, it is `0.05`.

## Try this

In `TestReportTextIncludesLimitedCount` inside `internal/probe/runner_test.go`, the test builds a `Report` by setting fields directly and then calls `Text()`. Add a line that sets `Errors: 2` on that struct and assert that the text contains `Errors:`. Run:

```text
go test ./internal/probe -run TestReportTextIncludesLimitedCount
```

`-run` chooses one test by name. Put the file back if you do not want to keep the extra assertion.
