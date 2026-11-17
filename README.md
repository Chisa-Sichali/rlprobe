# rlprobe

`rlprobe` sends HTTP requests at a controlled pace and reports when an API starts rate-limiting them. Use it only on APIs you own or have permission to test.

There are two programs, and they share one probe engine:

- `rlprobe` is the CLI, for a single run from the terminal
- `rlprobe-tui` is a [Bubble Tea](https://github.com/charmbracelet/bubbletea) terminal UI with a form, a live view, and a results screen

A response counts as limited when the status is `429`, when a `Retry-After` header is present, or when you pass an extra status such as `503`.

Safety caps are built in: at most 500 requests per second, at most 100 requests in flight, and at most 2 minutes for a fixed run or a whole ramp. The tool does not rotate proxies or try to get around a limit. It only observes one.

## Install

You need Go 1.26 or newer. From this directory:

```text
go build -o rlprobe.exe ./cmd/rlprobe
go build -o rlprobe-tui.exe ./cmd/rlprobe-tui
```

Or run without installing:

```text
go run ./cmd/rlprobe fixed --url https://example.com --rate 1 --duration 2s
go run ./cmd/rlprobe-tui
```

`https://example.com` is only a connectivity check. It will not show a real limit. Point the tool at an API you are allowed to test.

## CLI

Fixed pace:

```text
rlprobe fixed --url https://api.example.com/health --rate 20 --duration 15s --header "Authorization: Bearer TOKEN"
```

Ramp. The rate starts low and steps up. The first step where at least 5% of responses are limited stops the run. The ceiling is the last rate that stayed under that line.

```text
rlprobe ramp --url https://api.example.com/health --start 5 --step 5 --step-duration 5s --max-rate 80
```

`--max-rate` is the highest pace the ramp may use. If a full step would pass it, the last window uses the max itself.

Shared flags: `--method` (default `GET`), repeatable `--header`, `--body` or `--body-file`, `--timeout` (default `30s`), `--concurrency`, `--insecure` (skip TLS checks for a local certificate), repeatable `--limit-status`, and `--json`.

`Requests` counts calls that got an HTTP response. A timeout or a dropped connection is retried once and listed under `Errors`, so it does not take a slot away from that count.

`rlprobe fixed -h` and `rlprobe ramp -h` print the flags for that mode.

The text report includes requests sent, the limited count, latency p50/p95/p99, the first limited response, status codes, and any `Retry-After` or `X-RateLimit-*` / `RateLimit-*` headers. `--json` prints that same summary as one JSON object. A ramp report also includes the ceiling.

## TUI

```text
rlprobe-tui
```

1. Setup. Tab moves between the mode row and the fields. On the mode row, left and right switch between fixed and ramp, and `q` quits. Enter starts. Esc quits from a text field, so you can still type a `q` inside a URL.
2. Live run. You see the current rate, a progress bar, requests sent, limited responses, the latest latency, and status counts. Ctrl+C cancels and then shows whatever was collected.
3. Results. Same summary as the CLI. `r` returns to setup with your values kept. `q` quits.

## Tests

```text
go test ./...
```

## Learn Go with this repo

The lessons in `docs/` explain the code that is actually here. Read them in order:

1. [Start here](docs/00-start-here.md)
2. [Packages and main](docs/01-packages-and-main.md)
3. [Types and functions](docs/02-types-and-functions.md)
4. [HTTP](docs/03-http.md)
5. [Concurrency](docs/04-concurrency.md)
6. [Bubble Tea](docs/05-bubble-tea.md)
7. [Tests](docs/06-tests.md)
