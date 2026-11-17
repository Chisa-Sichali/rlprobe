package probe

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	// MaxRate is the highest pace a run is allowed to ask for.
	MaxRate = 500
	// MaxDuration is the longest a fixed run, or a whole ramp, may last.
	MaxDuration = 2 * time.Minute
	// MaxConcurrency bounds the worker pool.
	MaxConcurrency = 100

	DefaultRate        = 10
	DefaultDuration    = 10 * time.Second
	DefaultConcurrency = 10
	DefaultTimeout     = 30 * time.Second
	// attemptsPerSlot is the original try plus one replacement. A transport
	// failure does not count as one of the requests the run set out to finish.
	attemptsPerSlot     = 2
	DefaultStartRate    = 1
	DefaultStep         = 5
	DefaultStepDuration = 5 * time.Second
	DefaultMaxRate      = 50
	DefaultThreshold    = 0.05
)

// Mode selects a fixed pace or a ramp that speeds up until limits appear.
type Mode string

const (
	ModeFixed Mode = "fixed"
	ModeRamp  Mode = "ramp"
)

const (
	EventResult = "result"
	EventStep   = "step"
	EventDone   = "done"
)

// Config is everything the runner needs. Validate fills zero defaults and
// rejects values outside the safety caps.
type Config struct {
	URL            string
	Method         string
	Headers        http.Header
	Body           []byte
	Timeout        time.Duration
	Concurrency    int
	Insecure       bool
	LimitStatuses  map[int]bool
	LimitThreshold float64
	Mode           Mode
	Rate           float64
	Duration       time.Duration
	StartRate      float64
	Step           float64
	StepDuration   time.Duration
	MaxRate        float64
}

// Result is one finished HTTP call.
type Result struct {
	Status      int
	Latency     time.Duration
	Limited     bool
	Err         error
	At          time.Time
	RetryAfter  string
	RateHeaders map[string]string
	TargetRate  float64
}

// Event is streamed to the CLI and the TUI while a run is in progress.
type Event struct {
	Kind     string
	Result   Result
	StepRate float64
	Report   *Report
	Err      error
}

// Validate checks cfg and fills defaults for timeout, concurrency, and the
// limit threshold when those fields are zero.
func (c *Config) Validate() error {
	if c.Mode != ModeFixed && c.Mode != ModeRamp {
		return fmt.Errorf("unknown mode %q", c.Mode)
	}
	parsed, err := url.ParseRequestURI(c.URL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return errors.New("url must be an absolute http or https URL")
	}
	if strings.TrimSpace(c.Method) == "" {
		c.Method = http.MethodGet
	}
	c.Method = strings.ToUpper(strings.TrimSpace(c.Method))
	if c.Timeout == 0 {
		c.Timeout = DefaultTimeout
	}
	if c.Timeout < 0 {
		return errors.New("timeout must be greater than 0")
	}
	if c.Concurrency == 0 {
		c.Concurrency = DefaultConcurrency
	}
	if c.Concurrency < 1 || c.Concurrency > MaxConcurrency {
		return fmt.Errorf("concurrency must be from 1 to %d", MaxConcurrency)
	}
	if c.LimitThreshold == 0 {
		c.LimitThreshold = DefaultThreshold
	}
	if c.LimitThreshold < 0 || c.LimitThreshold > 1 {
		return errors.New("limit threshold must be between 0 and 1")
	}
	if c.Headers == nil {
		c.Headers = make(http.Header)
	}

	switch c.Mode {
	case ModeFixed:
		if c.Rate <= 0 || c.Rate > MaxRate {
			return fmt.Errorf("rate must be greater than 0 and at most %d req/s", MaxRate)
		}
		if c.Duration <= 0 || c.Duration > MaxDuration {
			return fmt.Errorf("duration must be greater than 0 and at most %s", MaxDuration)
		}
	case ModeRamp:
		if c.StartRate <= 0 || c.StartRate > MaxRate {
			return fmt.Errorf("start rate must be greater than 0 and at most %d req/s", MaxRate)
		}
		if c.Step <= 0 {
			return errors.New("step must be greater than 0")
		}
		if c.MaxRate <= 0 || c.MaxRate > MaxRate {
			return fmt.Errorf("max rate must be greater than 0 and at most %d req/s", MaxRate)
		}
		if c.StartRate > c.MaxRate {
			return errors.New("start rate must be less than or equal to max rate")
		}
		if c.StepDuration <= 0 || c.StepDuration > MaxDuration {
			return fmt.Errorf("step duration must be greater than 0 and at most %s", MaxDuration)
		}
		plan := ratePlan(c.StartRate, c.Step, c.MaxRate)
		total := c.StepDuration * time.Duration(len(plan))
		if total > MaxDuration {
			return fmt.Errorf("ramp would run for %s, which is over the %s cap; raise the step or shorten each step", total.Round(time.Second), MaxDuration)
		}
	}
	return nil
}

// ratePlan lists every pace the ramp will try. The last value is max when
// start plus whole steps would otherwise stop short of it.
func ratePlan(start, step, max float64) []float64 {
	if step <= 0 {
		return nil
	}
	var plan []float64
	current := start
	for len(plan) < 100000 {
		if current > max+1e-6 {
			break
		}
		plan = append(plan, current)
		next := current + step
		if next > max+1e-6 {
			if max-current > 1e-6 {
				plan = append(plan, max)
			}
			break
		}
		current = next
	}
	return plan
}

// Run starts the probe and returns a channel of events. The channel is closed
// after a final EventDone. Cancel ctx to stop scheduling more requests.
func Run(ctx context.Context, cfg Config) (<-chan Event, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
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
	return events, nil
}

func runRamp(ctx context.Context, client *http.Client, cfg Config, stats *stats, events chan<- Event) {
	var lastClean float64
	foundClean := false
	for _, pace := range ratePlan(cfg.StartRate, cfg.Step, cfg.MaxRate) {
		if ctx.Err() != nil {
			return
		}
		emit(ctx, events, Event{Kind: EventStep, StepRate: pace})
		stats.beginStep(pace)
		runWindow(ctx, client, cfg, pace, cfg.StepDuration, stats, events)
		step := stats.currentStep()
		if crossed(step.Sent, step.Limited, step.Errors, cfg.LimitThreshold) {
			if foundClean {
				stats.setCeiling(lastClean)
			} else {
				stats.setCeiling(0)
			}
			stats.setStopped(pace)
			return
		}
		if step.Sent > 0 {
			lastClean = pace
			foundClean = true
			stats.setCeiling(pace)
		}
	}
}

func crossed(sent, limited, errors int, threshold float64) bool {
	if sent == 0 {
		return errors > 0
	}
	return float64(limited)/float64(sent) >= threshold
}

func runWindow(ctx context.Context, client *http.Client, cfg Config, target float64, window time.Duration, stats *stats, events chan<- Event) {
	jobs := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < cfg.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range jobs {
				deliver(ctx, client, cfg, target, stats, events)
			}
		}()
	}
	defer func() {
		close(jobs)
		wg.Wait()
	}()

	limiter := rate.NewLimiter(rate.Limit(target), 1)
	deadline := time.Now().Add(window)
	for {
		if ctx.Err() != nil || !time.Now().Before(deadline) {
			return
		}
		reservation := limiter.Reserve()
		if !reservation.OK() {
			return
		}
		delay := reservation.Delay()
		if !time.Now().Add(delay).Before(deadline) {
			reservation.Cancel()
			return
		}
		if delay > 0 && !sleepCtx(ctx, delay) {
			reservation.Cancel()
			return
		}
		if !submitJob(ctx, deadline, jobs) {
			return
		}
	}
}

// deliver tries a slot until it gets an HTTP response. A timeout or connection
// error is recorded, then replaced, so it does not take the place of a request
// the run was meant to finish.
func deliver(ctx context.Context, client *http.Client, cfg Config, target float64, stats *stats, events chan<- Event) {
	for attempt := 1; attempt <= attemptsPerSlot; attempt++ {
		if ctx.Err() != nil {
			return
		}
		result := perform(ctx, client, cfg, target)
		if result.Err != nil && errors.Is(result.Err, context.Canceled) {
			return
		}
		if result.Err != nil {
			stats.noteError(result)
			emit(ctx, events, Event{Kind: EventResult, Result: result})
			continue
		}
		stats.add(result)
		emit(ctx, events, Event{Kind: EventResult, Result: result})
		return
	}
}

func sleepCtx(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func submitJob(ctx context.Context, deadline time.Time, jobs chan<- struct{}) bool {
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return false
	case jobs <- struct{}{}:
		return true
	}
}

func emit(ctx context.Context, events chan<- Event, event Event) {
	select {
	case events <- event:
	case <-ctx.Done():
	}
}

type stats struct {
	mu        sync.Mutex
	report    Report
	latencies []time.Duration
	stepIndex int
}

func newStats(cfg Config) *stats {
	return &stats{
		stepIndex: -1,
		report: Report{
			Mode:        cfg.Mode,
			Started:     time.Now(),
			Status:      make(map[int]int),
			RateHeaders: make(map[string]string),
		},
	}
}

func (s *stats) beginStep(pace float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.report.Steps = append(s.report.Steps, StepStat{Rate: pace})
	s.stepIndex = len(s.report.Steps) - 1
}

func (s *stats) currentStep() StepStat {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stepIndex < 0 || s.stepIndex >= len(s.report.Steps) {
		return StepStat{}
	}
	return s.report.Steps[s.stepIndex]
}

func (s *stats) setCeiling(pace float64) {
	s.mu.Lock()
	s.report.CeilingRate = pace
	s.mu.Unlock()
}

func (s *stats) setStopped(pace float64) {
	s.mu.Lock()
	s.report.StoppedRate = pace
	s.mu.Unlock()
}

func (s *stats) noteError(result Result) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.report.Errors++
	if result.Err != nil {
		s.report.LastError = result.Err.Error()
	}
	if s.stepIndex >= 0 && s.stepIndex < len(s.report.Steps) {
		s.report.Steps[s.stepIndex].Errors++
	}
}

func (s *stats) add(result Result) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.report.Sent++
	if s.stepIndex >= 0 && s.stepIndex < len(s.report.Steps) {
		s.report.Steps[s.stepIndex].Sent++
	}
	s.latencies = append(s.latencies, result.Latency)
	s.report.Status[result.Status]++
	if result.Limited {
		s.report.Limited++
		if s.stepIndex >= 0 && s.stepIndex < len(s.report.Steps) {
			s.report.Steps[s.stepIndex].Limited++
		}
		if s.report.FirstLimited == nil {
			seen := result.At
			s.report.FirstLimited = &seen
		}
	}
	if result.RetryAfter != "" {
		s.report.RetryAfter = appendUnique(s.report.RetryAfter, result.RetryAfter)
	}
	for key, value := range result.RateHeaders {
		s.report.RateHeaders[key] = value
	}
}

func (s *stats) finish(cancelled bool) Report {
	s.mu.Lock()
	defer s.mu.Unlock()
	report := s.report
	report.Ended = time.Now()
	report.Cancelled = cancelled
	report.Duration = report.Ended.Sub(report.Started)
	report.DurationMS = report.Duration.Milliseconds()
	report.Status = copyStatus(s.report.Status)
	report.RateHeaders = copyStrings(s.report.RateHeaders)
	report.RetryAfter = append([]string(nil), s.report.RetryAfter...)
	report.Steps = append([]StepStat(nil), s.report.Steps...)

	samples := append([]time.Duration(nil), s.latencies...)
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	report.P50 = percentile(samples, 0.50)
	report.P95 = percentile(samples, 0.95)
	report.P99 = percentile(samples, 0.99)
	report.LatencyP50MS = report.P50.Milliseconds()
	report.LatencyP95MS = report.P95.Milliseconds()
	report.LatencyP99MS = report.P99.Milliseconds()
	return report
}

func copyStatus(in map[int]int) map[int]int {
	out := make(map[int]int, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func copyStrings(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func appendUnique(existing []string, value string) []string {
	for _, item := range existing {
		if item == value {
			return existing
		}
	}
	if len(existing) >= 8 {
		return existing
	}
	return append(existing, value)
}
