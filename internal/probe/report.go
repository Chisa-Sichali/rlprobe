package probe

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// StepStat is one ramp window.
type StepStat struct {
	Rate    float64 `json:"rate"`
	Sent    int     `json:"sent"`
	Limited int     `json:"limited"`
	Errors  int     `json:"errors,omitempty"`
}

// Report is the summary printed by the CLI and the TUI results screen.
type Report struct {
	Mode         Mode              `json:"mode"`
	Sent         int               `json:"sent"`
	Limited      int               `json:"limited"`
	Errors       int               `json:"errors"`
	Status       map[int]int       `json:"status"`
	P50          time.Duration     `json:"-"`
	P95          time.Duration     `json:"-"`
	P99          time.Duration     `json:"-"`
	LatencyP50MS int64             `json:"latency_p50_ms"`
	LatencyP95MS int64             `json:"latency_p95_ms"`
	LatencyP99MS int64             `json:"latency_p99_ms"`
	FirstLimited *time.Time        `json:"first_limited,omitempty"`
	Started      time.Time         `json:"started"`
	Ended        time.Time         `json:"ended"`
	Duration     time.Duration     `json:"-"`
	DurationMS   int64             `json:"duration_ms"`
	RetryAfter   []string          `json:"retry_after,omitempty"`
	RateHeaders  map[string]string `json:"rate_headers,omitempty"`
	CeilingRate  float64           `json:"ceiling_rate,omitempty"`
	StoppedRate  float64           `json:"stopped_rate,omitempty"`
	Cancelled    bool              `json:"cancelled"`
	LastError    string            `json:"last_error,omitempty"`
	Steps        []StepStat        `json:"steps,omitempty"`
}

// FormatRate prints a requests-per-second value without trailing zeros.
func FormatRate(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

// Text renders the human-readable summary.
func (r Report) Text() string {
	var b strings.Builder
	writeLine(&b, "Mode:", string(r.Mode))
	writeLine(&b, "Requests:", strconv.Itoa(r.Sent))
	writeLine(&b, "Limited:", strconv.Itoa(r.Limited))
	writeLine(&b, "Errors:", strconv.Itoa(r.Errors))
	writeLine(&b, "Duration:", r.Duration.Round(time.Millisecond).String())
	writeLine(&b, "Latency p50:", r.P50.Round(time.Millisecond).String())
	writeLine(&b, "Latency p95:", r.P95.Round(time.Millisecond).String())
	writeLine(&b, "Latency p99:", r.P99.Round(time.Millisecond).String())
	if r.FirstLimited != nil && !r.Started.IsZero() {
		writeLine(&b, "First limited:", r.FirstLimited.Sub(r.Started).Round(time.Millisecond).String()+" after start")
	} else {
		writeLine(&b, "First limited:", "none")
	}
	if r.Mode == ModeRamp {
		if r.CeilingRate > 0 {
			writeLine(&b, "Ceiling:", FormatRate(r.CeilingRate)+" req/s")
		} else {
			writeLine(&b, "Ceiling:", "none")
		}
		if r.StoppedRate > 0 {
			writeLine(&b, "Stopped at:", FormatRate(r.StoppedRate)+" req/s")
		}
		for _, step := range r.Steps {
			fmt.Fprintf(&b, "  step %s req/s  requests %d, limited %d\n", FormatRate(step.Rate), step.Sent, step.Limited)
		}
	}
	if r.Cancelled {
		writeLine(&b, "Cancelled:", "yes")
	}
	if r.LastError != "" {
		writeLine(&b, "Last error:", r.LastError)
	}

	b.WriteString("\nStatus codes:\n")
	codes := make([]int, 0, len(r.Status))
	for code := range r.Status {
		codes = append(codes, code)
	}
	sort.Ints(codes)
	if len(codes) == 0 {
		b.WriteString("  (none)\n")
	}
	for _, code := range codes {
		fmt.Fprintf(&b, "  %d  %d\n", code, r.Status[code])
	}

	if len(r.RetryAfter) > 0 || len(r.RateHeaders) > 0 {
		b.WriteString("\nRate-limit headers:\n")
		if len(r.RetryAfter) > 0 {
			fmt.Fprintf(&b, "  Retry-After: %s\n", strings.Join(r.RetryAfter, ", "))
		}
		keys := make([]string, 0, len(r.RateHeaders))
		for key := range r.RateHeaders {
			if key == "Retry-After" {
				continue
			}
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			fmt.Fprintf(&b, "  %s: %s\n", key, r.RateHeaders[key])
		}
	}
	return b.String()
}

// JSON renders the same summary as one indented JSON object.
func (r Report) JSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

func writeLine(b *strings.Builder, label, value string) {
	fmt.Fprintf(b, "%-16s%s\n", label, value)
}

func percentile(samples []time.Duration, p float64) time.Duration {
	if len(samples) == 0 {
		return 0
	}
	idx := int(math.Ceil(p*float64(len(samples)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(samples) {
		idx = len(samples) - 1
	}
	return samples[idx]
}
