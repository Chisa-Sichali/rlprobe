package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"rate-limit-tester/internal/probe"
)

func (m Model) viewLive() string {
	var b strings.Builder
	b.WriteString(bold.Render("Running"))
	b.WriteString("\n\n")
	fmt.Fprintf(&b, "%-16s%s\n", "Mode:", m.mode)
	fmt.Fprintf(&b, "%-16s%s\n", "Rate:", accent.Render(probe.FormatRate(m.live.rate)+" req/s"))
	if m.width > 14 {
		m.progress.Width = m.width - 4
	}
	b.WriteString(m.progress.ViewAs(m.fraction()))
	b.WriteString("\n\n")
	if !m.live.gotFirst {
		b.WriteString(m.spinner.View())
		b.WriteString(" waiting for the first response\n")
	}
	fmt.Fprintf(&b, "%-16s%d\n", "Requests:", m.live.sent)
	limited := strconv.Itoa(m.live.limited)
	if m.live.limited > 0 {
		limited = warn.Render(limited)
	}
	fmt.Fprintf(&b, "%-16s%s\n", "Limited:", limited)
	fmt.Fprintf(&b, "%-16s%d\n", "Errors:", m.live.errors)
	if m.live.lastLatency > 0 {
		fmt.Fprintf(&b, "%-16s%s\n", "Last latency:", m.live.lastLatency.Round(time.Millisecond).String())
	}
	b.WriteString("\nStatus codes:\n")
	b.WriteString(formatStatus(m.live.status))
	return b.String()
}

func (m Model) viewResults() string {
	var b strings.Builder
	b.WriteString(bold.Render("Results"))
	b.WriteString("\n\n")
	b.WriteString(m.report.Text())
	if m.errMsg != "" {
		b.WriteString("\n")
		b.WriteString(warn.Render(m.errMsg))
		b.WriteString("\n")
	}
	return b.String()
}

func (m Model) fraction() float64 {
	if m.mode == probe.ModeFixed {
		if m.window <= 0 {
			return 0
		}
		portion := float64(time.Since(m.live.started)) / float64(m.window)
		return clamp(portion)
	}
	if m.maxRate <= m.startRate {
		if m.window <= 0 {
			return 0
		}
		return clamp(float64(time.Since(m.live.started)) / float64(m.window))
	}
	return clamp((m.live.rate - m.startRate) / (m.maxRate - m.startRate))
}

func clamp(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func formatStatus(counts map[int]int) string {
	if len(counts) == 0 {
		return "  (none yet)\n"
	}
	codes := make([]int, 0, len(counts))
	for code := range counts {
		codes = append(codes, code)
	}
	sort.Ints(codes)
	var b strings.Builder
	for _, code := range codes {
		fmt.Fprintf(&b, "  %d  %d\n", code, counts[code])
	}
	return b.String()
}

func (m Model) apply(event probe.Event) Model {
	switch event.Kind {
	case probe.EventStep:
		m.live.rate = event.StepRate
	case probe.EventResult:
		m.live.gotFirst = true
		if event.Result.Err != nil {
			m.live.errors++
			return m
		}
		m.live.sent++
		m.live.lastLatency = event.Result.Latency
		if m.live.status == nil {
			m.live.status = make(map[int]int)
		}
		m.live.status[event.Result.Status]++
		if event.Result.Limited {
			m.live.limited++
		}
	case probe.EventDone:
		m.screen = screenResults
		if event.Report != nil {
			m.report = *event.Report
		}
		if event.Err != nil {
			m.errMsg = event.Err.Error()
		}
		m.stop()
	}
	return m
}
