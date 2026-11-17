package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestReportTextIncludesLimitedCount(t *testing.T) {
	report := Report{
		Mode:    ModeFixed,
		Sent:    10,
		Limited: 4,
		Status:  map[int]int{200: 6, 429: 4},
	}
	text := report.Text()
	want := fmt.Sprintf("%-16s%d", "Limited:", 4)
	if !strings.Contains(text, want) {
		t.Fatalf("text missing limited count %q:\n%s", want, text)
	}

	raw, err := report.JSON()
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["limited"].(float64) != 4 {
		t.Fatalf("json limited = %v", payload["limited"])
	}
}

func TestValidateRejectsUnsafeCaps(t *testing.T) {
	fixed := Config{
		URL:      "https://example.com/health",
		Mode:     ModeFixed,
		Rate:     MaxRate + 1,
		Duration: time.Second,
	}
	if err := fixed.Validate(); err == nil {
		t.Fatal("expected high rate to be rejected")
	}

	ramp := Config{
		URL:          "http://127.0.0.1/health",
		Mode:         ModeRamp,
		StartRate:    1,
		Step:         1,
		MaxRate:      40,
		StepDuration: 5 * time.Second,
	}
	if err := ramp.Validate(); err == nil {
		t.Fatal("expected a long ramp to be rejected")
	}
}

func TestRatePlanIncludesMax(t *testing.T) {
	got := ratePlan(5, 10, 20)
	want := []float64{5, 15, 20}
	if len(got) != len(want) {
		t.Fatalf("plan = %v, want %v", got, want)
	}
	for i := range want {
		if math.Abs(got[i]-want[i]) > 0.001 {
			t.Fatalf("plan = %v, want %v", got, want)
		}
	}
}

func TestParseHeader(t *testing.T) {
	name, value, err := ParseHeader("Authorization: Bearer TOKEN")
	if err != nil {
		t.Fatal(err)
	}
	if name != "Authorization" || value != "Bearer TOKEN" {
		t.Fatalf("got %s %s", name, value)
	}
	if _, _, err := ParseHeader("not a header"); err == nil {
		t.Fatal("expected an error")
	}
}

func TestFixedRecordsLimits(t *testing.T) {
	server := newLimitServer(t, 3, false)
	cfg := Config{
		URL:         server.URL,
		Mode:        ModeFixed,
		Rate:        40,
		Duration:    500 * time.Millisecond,
		Concurrency: 10,
		Timeout:     2 * time.Second,
	}
	report := collect(t, cfg)
	if report.Sent == 0 {
		t.Fatal("expected requests to be sent")
	}
	if report.Limited == 0 {
		t.Fatalf("expected limited responses:\n%s", report.Text())
	}
	if len(report.RetryAfter) == 0 {
		t.Fatal("expected Retry-After to be recorded")
	}
	if _, ok := report.RateHeaders["X-Ratelimit-Remaining"]; !ok {
		t.Fatalf("headers = %#v", report.RateHeaders)
	}
	text := report.Text()
	want := fmt.Sprintf("%-16s%d", "Limited:", report.Limited)
	if !strings.Contains(text, want) {
		t.Fatalf("text missing limited count:\n%s", text)
	}
}

func TestExtraStatusCountsAsLimited(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "busy", http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)

	cfg := Config{
		URL:           server.URL,
		Mode:          ModeFixed,
		Rate:          10,
		Duration:      400 * time.Millisecond,
		Concurrency:   4,
		Timeout:       2 * time.Second,
		LimitStatuses: map[int]bool{http.StatusServiceUnavailable: true},
	}
	report := collect(t, cfg)
	if report.Limited == 0 || report.Status[http.StatusServiceUnavailable] == 0 {
		t.Fatalf("expected 503 to count as limited:\n%s", report.Text())
	}
}

func TestErrorsDoNotConsumeRequestCount(t *testing.T) {
	var hits atomic.Int64
	var okCount atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) <= 2 {
			hijacker, ok := w.(http.Hijacker)
			if !ok {
				t.Errorf("server does not support hijack")
				return
			}
			conn, _, err := hijacker.Hijack()
			if err != nil {
				return
			}
			conn.Close()
			return
		}
		okCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	report := collect(t, Config{
		URL:         server.URL,
		Mode:        ModeFixed,
		Rate:        20,
		Duration:    400 * time.Millisecond,
		Concurrency: 4,
		Timeout:     time.Second,
	})
	if report.Errors < 1 {
		t.Fatalf("expected a dropped connection to be recorded:\n%s", report.Text())
	}
	if report.Sent != int(okCount.Load()) {
		t.Fatalf("sent = %d, completed responses = %d\n%s", report.Sent, okCount.Load(), report.Text())
	}
	if report.Sent == 0 {
		t.Fatal("expected the replacement request to be counted")
	}
	if hits.Load() <= int64(report.Sent) {
		t.Fatalf("hits = %d, sent = %d; dropped attempts should be extra", hits.Load(), report.Sent)
	}
}

func TestRampStopsWhenLimited(t *testing.T) {
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
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newLimitServer(t, tt.allow, tt.alwaysOK)
			cfg := Config{
				URL:            server.URL,
				Mode:           ModeRamp,
				StartRate:      5,
				Step:           95,
				MaxRate:        100,
				StepDuration:   600 * time.Millisecond,
				Concurrency:    20,
				Timeout:        2 * time.Second,
				LimitThreshold: 0.05,
			}
			report := collect(t, cfg)
			if math.Abs(report.CeilingRate-tt.wantCeiling) > 0.01 {
				t.Fatalf("ceiling = %v, want %v\n%s", report.CeilingRate, tt.wantCeiling, report.Text())
			}
			if math.Abs(report.StoppedRate-tt.wantStopped) > 0.01 {
				t.Fatalf("stopped = %v, want %v\n%s", report.StoppedRate, tt.wantStopped, report.Text())
			}
		})
	}
}

func newLimitServer(t *testing.T, allow int, alwaysOK bool) *httptest.Server {
	t.Helper()
	var seen atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := seen.Add(1)
		if !alwaysOK && n > int64(allow) {
			w.Header().Set("Retry-After", "1")
			w.Header().Set("X-RateLimit-Remaining", "0")
			http.Error(w, "slow down", http.StatusTooManyRequests)
			return
		}
		w.Header().Set("X-RateLimit-Remaining", "5")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(server.Close)
	return server
}

func collect(t *testing.T, cfg Config) Report {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	events, err := Run(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	var report Report
	sawDone := false
	for event := range events {
		if event.Kind == EventDone {
			if event.Report == nil {
				t.Fatal("missing report")
			}
			report = *event.Report
			sawDone = true
		}
	}
	if !sawDone {
		t.Fatal("run ended without a report")
	}
	return report
}
