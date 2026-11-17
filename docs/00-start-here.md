# Start here

This lesson gets the project running and explains the one file Go uses to name the module.

## What you need

Install Go 1.26 or newer from [https://go.dev/dl/](https://go.dev/dl/). In a terminal:

```text
go version
```

You should see a version of at least `go1.26`. This repo was written with `go 1.26.4`, which is the first line of real configuration in `go.mod`.

Open a terminal in the project directory (the folder that contains `go.mod`).

## Run the CLI

This sends one request per second for two seconds to `https://example.com`. That host is a reserved example site, so the run is a connectivity check, not a limit test.

```text
go run ./cmd/rlprobe fixed --url https://example.com --rate 1 --duration 2s
```

`go run` compiles the program and starts it. `./cmd/rlprobe` is the folder that holds `main`. You should see a short report: mode, request count, limited count, errors, and latency. `example.com` answers `200`, so the limited count stays at 0. That is a successful run.

To open the terminal UI:

```text
go run ./cmd/rlprobe-tui
```

Esc quits. The next lessons explain the screens. You can close it now.

## Run the tests

```text
go test ./...
```

`./...` means this module and every package under it. A package that passes prints `ok`. The tests start a tiny local HTTP server, so they do not call the public internet.

## What a module is

Open `go.mod`. The first lines look like this:

```go
module rate-limit-tester

go 1.26.4
```

`module rate-limit-tester` is the import prefix. Code in `internal/probe` is imported as `rate-limit-tester/internal/probe`. The `go` line is the language version the module expects.

The `require` block lists other people's modules, such as Bubble Tea. You do not edit those versions by hand. When you add an import, run:

```text
go mod tidy
```

That updates `go.mod` and `go.sum`. `go.sum` is a list of checksums so the next download is the same code.

## Try this

Run the fixed command again with `--json` added at the end. The same facts come out as one JSON object. In `internal/probe/report.go`, `JSON` is the method that builds it, and `Text` is the method that builds the normal report.
