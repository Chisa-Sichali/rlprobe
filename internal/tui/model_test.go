package tui

import (
	"strings"
	"testing"

	"rate-limit-tester/internal/probe"
)

func TestResultsViewShowsCeiling(t *testing.T) {
	model := New()
	model.screen = screenResults
	model.report = probe.Report{
		Mode:        probe.ModeRamp,
		CeilingRate: 15,
		Sent:        20,
		Limited:     3,
		Status:      map[int]int{200: 17, 429: 3},
	}
	got := model.View()
	if !strings.Contains(got, "15 req/s") {
		t.Fatalf("results view missing ceiling:\n%s", got)
	}
}
