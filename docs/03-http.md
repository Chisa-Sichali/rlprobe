# HTTP

The probe speaks ordinary HTTP. The standard library package is `net/http`. You do not need a third-party client for this tool.

## One request

Open `perform` in `internal/probe/client.go`. Each call builds a request, sends it, and records what came back.

```go
req, err := http.NewRequestWithContext(reqCtx, cfg.Method, cfg.URL, body)
```

`NewRequestWithContext` attaches a `context`. When the run is cancelled, or when the per-request timeout fires, the client stops waiting. The method comes from the CLI flag or the TUI field. The default filled in by `Validate` is `GET`.

Headers are a map of names to values. The loop copies `cfg.Headers` onto the request. If the user did not set a user agent, the probe sets one:

```go
if req.Header.Get("User-Agent") == "" {
	req.Header.Set("User-Agent", "rlprobe/1.0")
}
```

`Get` reads one header. `Set` replaces it. `Add` would append another value under the same name. The CLI uses `Add`, so repeating `--header` can send two values for one name.

Then:

```go
resp, err := client.Do(req)
```

`err` here means the call never completed: DNS failed, the connection was refused, or the timeout won. A `429` is not an error in this sense. It is a finished response with a status code. The code records that on `result.Status` and still returns a `Result` the report can count.

The body is drained and closed:

```go
defer resp.Body.Close()
_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
```

`defer` runs when `perform` returns. Closing the body lets the connection be reused. The copy throws the bytes away, but it stops after 1 mebibyte so a huge response cannot sit in memory. `1<<20` is `2` to the power of `20`.

## What "limited" means

```go
result.Limited = resp.StatusCode == http.StatusTooManyRequests || retry != "" || cfg.LimitStatuses[resp.StatusCode]
```

`http.StatusTooManyRequests` is `429`. A `Retry-After` header also counts, even on another status, because servers use it to say "come back later". `--limit-status 503` puts `503` in `cfg.LimitStatuses`, for APIs that signal overload that way.

`pickRateHeaders` keeps `Retry-After`, headers starting with `X-RateLimit-`, and headers starting with `RateLimit-`. Go's `http.Header` stores names in canonical form, so `X-RateLimit-Remaining` is saved as `X-Ratelimit-Remaining`. The report printer uses whatever key the map holds.

## Status codes you will see

- `200` means the request was accepted.
- `429` means the client is being told to slow down.
- `503` sometimes means the server is too busy. It counts as a limit only if you opted in with `--limit-status`.
- A non-nil `result.Err` means there was no status at all.

The histogram in the report is `Report.Status`, a `map[int]int` from status code to how many times it appeared.

## Try this

Run a two-second check and read the status block at the bottom:

```text
go run ./cmd/rlprobe fixed --url https://example.com --rate 1 --duration 2s
```

You should see `200` in the status list. Then open `perform` and temporarily change the user agent string to `rlprobe-learning/1.0`. The example server ignores it, but you can see that the string you send is just a header. Change it back to `rlprobe/1.0` afterward.
