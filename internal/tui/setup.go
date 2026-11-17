package tui

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"rate-limit-tester/internal/probe"
)

func defaultDraft() map[string]string {
	return map[string]string{
		"URL":         "",
		"Method":      "GET",
		"Header":      "",
		"Rate":        strconv.Itoa(probe.DefaultRate),
		"Duration":    probe.DefaultDuration.String(),
		"Concurrency": strconv.Itoa(probe.DefaultConcurrency),
		"Start":       strconv.Itoa(probe.DefaultStartRate),
		"Step":        strconv.Itoa(probe.DefaultStep),
		"Step time":   probe.DefaultStepDuration.String(),
		"Max rate":    strconv.Itoa(probe.DefaultMaxRate),
	}
}

func fieldLabels(mode probe.Mode) []string {
	labels := []string{"URL", "Method", "Header"}
	if mode == probe.ModeRamp {
		return append(labels, "Start", "Step", "Step time", "Max rate", "Concurrency")
	}
	return append(labels, "Rate", "Duration", "Concurrency")
}

func buildInputs(mode probe.Mode, draft map[string]string) ([]textinput.Model, []string) {
	labels := fieldLabels(mode)
	inputs := make([]textinput.Model, len(labels))
	for i, name := range labels {
		field := textinput.New()
		field.SetValue(draft[name])
		field.Width = 42
		field.CharLimit = 512
		field.Prompt = "  "
		switch name {
		case "URL":
			field.Placeholder = "https://api.example.com/health"
		case "Header":
			field.Placeholder = "Authorization: Bearer TOKEN"
		}
		inputs[i] = field
	}
	return inputs, labels
}

func (m Model) readDraft() map[string]string {
	draft := defaultDraft()
	for i, name := range m.labels {
		if i < len(m.inputs) {
			draft[name] = m.inputs[i].Value()
		}
	}
	return draft
}

func (m Model) setMode(mode probe.Mode) (Model, tea.Cmd) {
	if mode == m.mode {
		return m, nil
	}
	draft := m.readDraft()
	m.mode = mode
	m.inputs, m.labels = buildInputs(mode, draft)
	return m.focusIndex(0)
}

func (m Model) focusIndex(index int) (Model, tea.Cmd) {
	count := 1 + len(m.inputs)
	if count == 1 {
		m.focus = 0
		return m, nil
	}
	for index < 0 {
		index += count
	}
	m.focus = index % count
	var cmd tea.Cmd
	for i := range m.inputs {
		if m.focus == i+1 {
			cmd = m.inputs[i].Focus()
			m.inputs[i].Prompt = "> "
		} else {
			m.inputs[i].Blur()
			m.inputs[i].Prompt = "  "
		}
	}
	return m, cmd
}

func (m Model) config() (probe.Config, error) {
	draft := m.readDraft()
	cfg := probe.Config{
		URL:            strings.TrimSpace(draft["URL"]),
		Method:         strings.TrimSpace(draft["Method"]),
		Mode:           m.mode,
		LimitThreshold: probe.DefaultThreshold,
		Headers:        make(http.Header),
	}
	header := strings.TrimSpace(draft["Header"])
	if header != "" {
		name, value, err := probe.ParseHeader(header)
		if err != nil {
			return cfg, err
		}
		cfg.Headers.Set(name, value)
	}
	concurrency, err := strconv.Atoi(strings.TrimSpace(draft["Concurrency"]))
	if err != nil {
		return cfg, fmt.Errorf("concurrency must be a whole number")
	}
	cfg.Concurrency = concurrency
	if m.mode == probe.ModeRamp {
		cfg.StartRate, err = parseRate(draft["Start"], "start")
		if err != nil {
			return cfg, err
		}
		cfg.Step, err = parseRate(draft["Step"], "step")
		if err != nil {
			return cfg, err
		}
		cfg.MaxRate, err = parseRate(draft["Max rate"], "max rate")
		if err != nil {
			return cfg, err
		}
		cfg.StepDuration, err = time.ParseDuration(strings.TrimSpace(draft["Step time"]))
		if err != nil {
			return cfg, fmt.Errorf("step time must look like 5s or 1m")
		}
	} else {
		cfg.Rate, err = parseRate(draft["Rate"], "rate")
		if err != nil {
			return cfg, err
		}
		cfg.Duration, err = time.ParseDuration(strings.TrimSpace(draft["Duration"]))
		if err != nil {
			return cfg, fmt.Errorf("duration must look like 10s or 1m")
		}
	}
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func parseRate(raw, name string) (float64, error) {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be a number", name)
	}
	return value, nil
}

func (m Model) viewSetup() string {
	var b strings.Builder
	b.WriteString(bold.Render("rlprobe"))
	b.WriteString("\nProbe an API you are allowed to test.\n\n")

	fixed := "fixed"
	ramp := "ramp"
	if m.mode == probe.ModeFixed {
		fixed = bold.Render(fixed)
	} else {
		ramp = bold.Render(ramp)
	}
	pointer := "  "
	if m.focus == 0 {
		pointer = "> "
	}
	fmt.Fprintf(&b, "%sMode  %s   %s\n", pointer, fixed, ramp)
	if m.focus == 0 {
		b.WriteString("left/right switches mode. q quits.\n")
	}
	b.WriteString("\n")
	for i, input := range m.inputs {
		name := ""
		if i < len(m.labels) {
			name = m.labels[i]
		}
		fmt.Fprintf(&b, "%s %s\n", label.Render(name), input.View())
	}
	if m.errMsg != "" {
		b.WriteString("\n")
		b.WriteString(warn.Render(m.errMsg))
		b.WriteString("\n")
	}
	return b.String()
}
