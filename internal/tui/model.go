package tui

import (
	"context"
	"os"
	"os/signal"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"rate-limit-tester/internal/probe"
)

type screen int

const (
	screenSetup screen = iota
	screenLive
	screenResults
)

// CancelMsg is sent when Ctrl+C arrives, including from the OS signal.
type CancelMsg struct{}

type eventsClosedMsg struct{}

type liveView struct {
	sent        int
	limited     int
	errors      int
	status      map[int]int
	lastLatency time.Duration
	rate        float64
	gotFirst    bool
	started     time.Time
}

type keyMap struct {
	next   key.Binding
	prev   key.Binding
	mode   key.Binding
	start  key.Binding
	quit   key.Binding
	cancel key.Binding
	again  key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		next:   key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next")),
		prev:   key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "previous")),
		mode:   key.NewBinding(key.WithKeys("left", "right"), key.WithHelp("←/→", "mode")),
		start:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "start")),
		quit:   key.NewBinding(key.WithKeys("esc", "q"), key.WithHelp("esc/q", "quit")),
		cancel: key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "cancel")),
		again:  key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "again")),
	}
}

type setupHelp struct{ keys keyMap }
type liveHelp struct{ keys keyMap }
type resultHelp struct{ keys keyMap }

func (h setupHelp) ShortHelp() []key.Binding {
	return []key.Binding{h.keys.next, h.keys.mode, h.keys.start, h.keys.quit}
}
func (h setupHelp) FullHelp() [][]key.Binding { return [][]key.Binding{h.ShortHelp()} }

func (h liveHelp) ShortHelp() []key.Binding  { return []key.Binding{h.keys.cancel} }
func (h liveHelp) FullHelp() [][]key.Binding { return [][]key.Binding{h.ShortHelp()} }

func (h resultHelp) ShortHelp() []key.Binding {
	return []key.Binding{h.keys.again, h.keys.quit}
}
func (h resultHelp) FullHelp() [][]key.Binding { return [][]key.Binding{h.ShortHelp()} }

// Model is the Bubble Tea program: setup, a live run, then results.
type Model struct {
	screen   screen
	mode     probe.Mode
	inputs   []textinput.Model
	labels   []string
	focus    int
	errMsg   string
	spinner  spinner.Model
	progress progress.Model
	help     help.Model
	keys     keyMap
	startup  tea.Cmd

	width  int
	height int

	cancel    context.CancelFunc
	events    <-chan probe.Event
	report    probe.Report
	live      liveView
	window    time.Duration
	startRate float64
	maxRate   float64
}

// New returns the setup screen with the same defaults as the CLI.
func New() Model {
	inputs, labels := buildInputs(probe.ModeFixed, defaultDraft())
	spin := spinner.New()
	spin.Spinner = spinner.Dot
	bar := progress.New(progress.WithSolidFill("6"))
	bar.Width = 40
	helpModel := help.New()
	helpModel.Width = 72
	model := Model{
		screen:   screenSetup,
		mode:     probe.ModeFixed,
		inputs:   inputs,
		labels:   labels,
		spinner:  spin,
		progress: bar,
		help:     helpModel,
		keys:     newKeyMap(),
		width:    80,
	}
	var cmd tea.Cmd
	model, cmd = model.focusIndex(1)
	model.startup = cmd
	return model
}

// Running reports whether a probe is in flight. The program filter uses it
// so Ctrl+C cancels the run instead of closing the TUI immediately.
func (m Model) Running() bool {
	return m.screen == screenLive
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.startup, listenInterrupt())
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.help.Width = msg.Width
		inner := msg.Width - 4
		if inner < 10 {
			inner = 10
		}
		m.progress.Width = inner
		field := msg.Width - 18
		if field < 12 {
			field = 12
		}
		if field > 60 {
			field = 60
		}
		for i := range m.inputs {
			m.inputs[i].Width = field
		}
		return m, nil
	case CancelMsg:
		return m.onCancel()
	case probe.Event:
		if m.screen != screenLive {
			return m, nil
		}
		m = m.apply(msg)
		if m.screen != screenLive {
			return m, nil
		}
		return m, waitEvents(m.events)
	case eventsClosedMsg:
		if m.screen == screenLive {
			m.screen = screenResults
			m.stop()
		}
		return m, nil
	case spinner.TickMsg:
		if m.screen == screenLive && !m.live.gotFirst {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil
	case tea.KeyMsg:
		return m.onKey(msg)
	default:
		if m.screen == screenSetup && m.focus > 0 && m.focus-1 < len(m.inputs) {
			var cmd tea.Cmd
			index := m.focus - 1
			m.inputs[index], cmd = m.inputs[index].Update(msg)
			return m, cmd
		}
		return m, nil
	}
}

func (m Model) onCancel() (tea.Model, tea.Cmd) {
	if m.screen == screenLive {
		next, cmd := m.cancelRun()
		return next, tea.Batch(cmd, listenInterrupt())
	}
	m.stop()
	return m, tea.Quit
}

func (m Model) onKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC || msg.String() == "ctrl+c" {
		return m.onCancel()
	}
	switch m.screen {
	case screenLive:
		return m, nil
	case screenResults:
		if msg.String() == "q" || msg.String() == "esc" {
			m.stop()
			return m, tea.Quit
		}
		if msg.String() == "r" {
			m.screen = screenSetup
			m.errMsg = ""
			return m.focusIndex(1)
		}
		return m, nil
	default:
		if msg.String() == "q" && m.focus == 0 {
			m.stop()
			return m, tea.Quit
		}
		if msg.String() == "esc" {
			m.stop()
			return m, tea.Quit
		}
		if m.focus == 0 && (msg.String() == "left" || msg.String() == "right") {
			if msg.String() == "left" {
				return m.setMode(probe.ModeFixed)
			}
			return m.setMode(probe.ModeRamp)
		}
		if key.Matches(msg, m.keys.next) {
			return m.focusIndex(m.focus + 1)
		}
		if key.Matches(msg, m.keys.prev) {
			return m.focusIndex(m.focus - 1)
		}
		if key.Matches(msg, m.keys.start) {
			return m.start()
		}
		if m.focus > 0 && m.focus-1 < len(m.inputs) {
			var cmd tea.Cmd
			index := m.focus - 1
			m.inputs[index], cmd = m.inputs[index].Update(msg)
			return m, cmd
		}
		return m, nil
	}
}

func (m Model) start() (Model, tea.Cmd) {
	cfg, err := m.config()
	if err != nil {
		m.errMsg = err.Error()
		return m, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	events, err := probe.Run(ctx, cfg)
	if err != nil {
		cancel()
		m.errMsg = err.Error()
		return m, nil
	}
	m.errMsg = ""
	m.cancel = cancel
	m.events = events
	m.screen = screenLive
	m.window = cfg.Duration
	m.startRate = cfg.StartRate
	m.maxRate = cfg.MaxRate
	rateNow := cfg.Rate
	if cfg.Mode == probe.ModeRamp {
		m.window = cfg.StepDuration
		rateNow = cfg.StartRate
	}
	m.live = liveView{
		status:  make(map[int]int),
		rate:    rateNow,
		started: time.Now(),
	}
	return m, tea.Batch(m.spinner.Tick, waitEvents(events))
}

func (m Model) cancelRun() (Model, tea.Cmd) {
	m.stop()
	return m, nil
}

func (m *Model) stop() {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
}

func (m Model) View() string {
	var body string
	switch m.screen {
	case screenLive:
		body = m.viewLive()
	case screenResults:
		body = m.viewResults()
	default:
		body = m.viewSetup()
	}
	return body + "\n" + m.help.View(m.helpMap())
}

func (m Model) helpMap() help.KeyMap {
	switch m.screen {
	case screenLive:
		return liveHelp{keys: m.keys}
	case screenResults:
		return resultHelp{keys: m.keys}
	default:
		return setupHelp{keys: m.keys}
	}
}

func waitEvents(events <-chan probe.Event) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-events
		if !ok {
			return eventsClosedMsg{}
		}
		return event
	}
}

func listenInterrupt() tea.Cmd {
	return func() tea.Msg {
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, os.Interrupt)
		<-signals
		signal.Stop(signals)
		return CancelMsg{}
	}
}
