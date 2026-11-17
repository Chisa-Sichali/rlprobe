package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"

	"rate-limit-tester/internal/probe"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, errHelp) {
			return
		}
		if errors.Is(err, errUsage) && err.Error() == errUsage.Error() {
			os.Exit(2)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(exitCode(err))
	}
}

func run(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprint(os.Stdout, usageText)
		if len(args) == 0 {
			return errUsage
		}
		return errHelp
	}
	switch args[0] {
	case "fixed", "ramp":
		return runMode(probe.Mode(args[0]), args[1:])
	default:
		return fmt.Errorf("%w: unknown command %q\n\n%s", errUsage, args[0], usageText)
	}
}

func runMode(mode probe.Mode, args []string) error {
	fs := flag.NewFlagSet(string(mode), flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var headers stringList
	var limitStatuses intList
	urlFlag := fs.String("url", "", "absolute http or https URL to probe")
	method := fs.String("method", http.MethodGet, "HTTP method")
	body := fs.String("body", "", "request body")
	bodyFile := fs.String("body-file", "", "file to send as the request body")
	timeout := fs.Duration("timeout", probe.DefaultTimeout, "per-request timeout")
	concurrency := fs.Int("concurrency", probe.DefaultConcurrency, "maximum in-flight requests")
	insecure := fs.Bool("insecure", false, "skip TLS certificate checks for local certificates")
	asJSON := fs.Bool("json", false, "print the report as JSON")
	fs.Var(&headers, "header", "request header, as Name: Value (repeatable)")
	fs.Var(&limitStatuses, "limit-status", "extra status code that counts as rate limited (repeatable)")

	rateFlag := fs.Float64("rate", probe.DefaultRate, "requests per second for a fixed run")
	duration := fs.Duration("duration", probe.DefaultDuration, "how long a fixed run lasts")
	start := fs.Float64("start", probe.DefaultStartRate, "first ramp rate, in requests per second")
	step := fs.Float64("step", probe.DefaultStep, "how much to add to the rate after each ramp step")
	stepDuration := fs.Duration("step-duration", probe.DefaultStepDuration, "how long each ramp step lasts")
	maxRate := fs.Float64("max-rate", probe.DefaultMaxRate, "highest ramp rate, in requests per second")

	fs.Usage = func() {
		fmt.Fprint(os.Stdout, commandUsage(mode))
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return errHelp
		}
		return fmt.Errorf("%w: %v\n\n%s", errUsage, err, commandUsage(mode))
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("%w: unexpected argument %q\n\n%s", errUsage, fs.Arg(0), commandUsage(mode))
	}
	if *urlFlag == "" {
		return fmt.Errorf("%w: --url is required\n\n%s", errUsage, commandUsage(mode))
	}
	if *body != "" && *bodyFile != "" {
		return fmt.Errorf("%w: pass only one of --body or --body-file", errUsage)
	}

	cfg := probe.Config{
		URL:            *urlFlag,
		Method:         *method,
		Timeout:        *timeout,
		Concurrency:    *concurrency,
		Insecure:       *insecure,
		Mode:           mode,
		Rate:           *rateFlag,
		Duration:       *duration,
		StartRate:      *start,
		Step:           *step,
		StepDuration:   *stepDuration,
		MaxRate:        *maxRate,
		LimitThreshold: probe.DefaultThreshold,
		Headers:        make(http.Header),
		LimitStatuses:  make(map[int]bool),
	}
	for _, line := range headers {
		name, value, err := probe.ParseHeader(line)
		if err != nil {
			return fmt.Errorf("%w: %v", errUsage, err)
		}
		if name != "" {
			cfg.Headers.Add(name, value)
		}
	}
	for _, code := range limitStatuses {
		cfg.LimitStatuses[code] = true
	}
	if *bodyFile != "" {
		payload, err := os.ReadFile(*bodyFile)
		if err != nil {
			return err
		}
		cfg.Body = payload
	} else if *body != "" {
		cfg.Body = []byte(*body)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	events, err := probe.Run(ctx, cfg)
	if err != nil {
		return err
	}
	var report probe.Report
	for event := range events {
		if event.Kind == probe.EventDone && event.Report != nil {
			report = *event.Report
		}
	}
	if *asJSON {
		raw, err := report.JSON()
		if err != nil {
			return err
		}
		fmt.Println(string(raw))
		return nil
	}
	fmt.Print(report.Text())
	return nil
}

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ", ") }

func (s *stringList) Set(value string) error {
	*s = append(*s, value)
	return nil
}

type intList []int

func (s *intList) String() string {
	parts := make([]string, len(*s))
	for i, n := range *s {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ",")
}

func (s *intList) Set(value string) error {
	n, err := strconv.Atoi(value)
	if err != nil || n < 100 || n > 599 {
		return fmt.Errorf("limit status must be an HTTP status code")
	}
	*s = append(*s, n)
	return nil
}

var (
	errHelp  = errors.New("help")
	errUsage = errors.New("usage")
)

func exitCode(err error) int {
	if errors.Is(err, errUsage) {
		return 2
	}
	return 1
}

const usageText = `rlprobe probes an HTTP API you are allowed to test and reports when rate limits appear.

Use only APIs you own or have permission to test. The tool caps a run at 500 requests
per second and 2 minutes.

Usage:
  rlprobe fixed --url URL [flags]
  rlprobe ramp  --url URL [flags]

Examples:
  rlprobe fixed --url https://api.example.com/health --rate 20 --duration 15s --header "Authorization: Bearer TOKEN"
  rlprobe ramp  --url https://api.example.com/health --start 5 --step 5 --step-duration 5s --max-rate 80

Run rlprobe fixed -h or rlprobe ramp -h for the flags of that mode.
`

func commandUsage(mode probe.Mode) string {
	shared := `
Shared flags:
  --url            absolute http or https URL (required)
  --method         HTTP method (default GET)
  --header         Name: Value header (repeatable)
  --body           request body
  --body-file      file to send as the request body
  --timeout        per-request timeout (default 30s)
  --concurrency    maximum in-flight requests (default 10, max 100)
  --insecure       skip TLS certificate checks
  --limit-status   extra status code that counts as limited (repeatable)
  --json           print the report as JSON
`
	if mode == probe.ModeRamp {
		return `rlprobe ramp raises the request rate until limited responses cross 5% of a step.
The last clean rate is the observed ceiling. The whole ramp must finish within 2 minutes,
and no step may exceed 500 req/s.

` + shared + `
Ramp flags:
  --start          first rate in requests per second (default 1)
  --step           rate added after each step (default 5)
  --step-duration  length of each step (default 5s)
  --max-rate       highest rate to try (default 50)
`
	}
	return `rlprobe fixed sends at one rate for a duration, then reports limited responses.
Duration is capped at 2 minutes and the rate at 500 req/s.

` + shared + `
Fixed flags:
  --rate       requests per second (default 10)
  --duration   how long to send (default 10s)
`
}
