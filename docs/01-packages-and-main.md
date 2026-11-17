# Packages and main

A Go program is a set of packages. One of them is special: `package main`, with a function named `main`. That function is where the process starts.

## Two programs, one engine

This repo builds two commands:

| Folder | Package | What starts |
| --- | --- | --- |
| `cmd/rlprobe` | `main` | the CLI |
| `cmd/rlprobe-tui` | `main` | the Bubble Tea UI |
| `internal/probe` | `probe` | HTTP pacing, shared by both |
| `internal/tui` | `tui` | screens, used only by the TUI |

`cmd/` is the usual place for programs. `internal/` means another module cannot import these packages. That is a Go rule, not a style choice. It keeps the probe private to this tool.

Both mains import the probe:

```go
"rate-limit-tester/internal/probe"
```

The CLI in `cmd/rlprobe/main.go` reads flags, calls `probe.Run`, and prints the report. The TUI in `cmd/rlprobe-tui/main.go` only starts Bubble Tea. The screen code lives in `internal/tui` and calls the same `probe.Run`.

If the pacing rules lived inside each `main`, a bug fix would have to be made twice. Sharing `internal/probe` means a fixed run and a ramp behave the same in the CLI and the TUI.

## What `main` is allowed to do

Open `cmd/rlprobe/main.go`. The process begins here:

```go
func main() {
	if err := run(os.Args[1:]); err != nil {
```

`os.Args[1:]` is everything the user typed after the program name. `main` itself stays small: it prints an error and picks an exit code. The rest of the CLI is the `run` function and `runMode`.

A package named `main` cannot be imported. Every other package in this repo (`probe`, `tui`) can.

## Files in one package

`internal/probe` has several files: `client.go`, `runner.go`, `report.go`, and `runner_test.go`. They all say `package probe`. The compiler glues those files into one package. A function in `client.go` can call a function in `runner.go` without an import. Tests in `runner_test.go` are part of the same package, so they can call helpers that other packages cannot see, such as `ratePlan`.

Names that start with a capital letter are exported. `probe.Run` and `probe.Config` are visible to the CLI. `perform` and `ratePlan` start with a lowercase letter, so only `package probe` can call them.

## Try this

From the project root, run:

```text
go run ./cmd/rlprobe help
```

Then open `usageText` near the bottom of `cmd/rlprobe/main.go`. Change one word in that string, run the command again, and confirm the new word appears. Change it back when you are done so the docs still match the tool.
