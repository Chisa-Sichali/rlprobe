# Bubble Tea

The TUI is a loop with three jobs: start things, react to messages, and draw. Bubble Tea names them `Init`, `Update`, and `View`. The type that has those methods is the model, in `internal/tui/model.go`.

## The loop

`cmd/rlprobe-tui/main.go` builds a program around `tui.New()` and calls `Run`. You do not write the loop yourself. Bubble Tea calls your methods.

```go
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.startup, listenInterrupt())
}
```

`Init` runs once. A `tea.Cmd` is a function that does work and later returns a message. `m.startup` blinks the cursor in the focused field. `listenInterrupt` waits for Ctrl+C and returns `CancelMsg`. `tea.Batch` starts both.

`Update` receives one message and returns the next model plus the next command. The event case is the bridge from the probe to the screen:

```go
case probe.Event:
	if m.screen != screenLive {
		return m, nil
	}
	m = m.apply(msg)
	if m.screen != screenLive {
		return m, nil
	}
	return m, waitEvents(m.events)
```

`msg := msg.(type)` above that case is a type switch. Each `case` is one kind of message. `tea.KeyMsg` is a keypress. `probe.Event` is one result from the probe. `spinner.TickMsg` is the spinner asking to advance a frame.

`View` only builds a string. It must not send HTTP, sleep, or read the event channel. If it did, the terminal would freeze every time Bubble Tea asked for a new frame, and a slow server would stall the UI. The live screen in `viewLive` reads fields that `Update` already stored on the model.

## Screens

`screen` is an int with names: `screenSetup`, `screenLive`, `screenResults`. `View` switches on it and calls `viewSetup`, `viewLive`, or `viewResults`. The help footer uses the same switch, so the keys shown match the screen.

Setup stores text in `textinput` models from the Bubbles package. Tab changes `m.focus`. Index `0` is the mode row, not a text field, which is why left and right can change fixed/ramp without moving a cursor inside the URL.

Enter calls `start`. That builds a `probe.Config`, calls `probe.Run`, and returns `waitEvents`:

```go
func waitEvents(events <-chan probe.Event) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-events
		if !ok {
			return eventsClosedMsg{}
		}
		return event
	}
}
```

The receive happens inside the command, on Bubble Tea's command goroutine, not inside `View`. When a value arrives, `Update` sees a `probe.Event`, stores the new counts, and starts `waitEvents` again for the next one. One receive at a time keeps the events in order.

## Cancel

Ctrl+C is an OS signal. `listenInterrupt` turns it into `CancelMsg`. During a run, `onCancel` calls `stop`, which cancels the `context` passed to `probe.Run`. The scheduler notices `ctx.Done()`, stops handing out jobs, and still sends `EventDone`. `Update` moves to the results screen with the partial report. From setup or results, the same message quits.

`Running()` exists so the program can tell a quit message from a cancel. The filter in `main` checks it.

## Color

`internal/tui/styles.go` defines three lipgloss styles. The current rate uses `accent`. Limited counts and the setup error line use `warn`. Everything else stays the terminal's normal foreground. `bold` is weight only, not a new color.

## Try this

Open `viewLive` in `internal/tui/run.go`. The rate line uses `accent.Render`. Change the label `"Rate:"` to `"Pace:"`, run `go test ./internal/tui`, then start `go run ./cmd/rlprobe-tui` and confirm the live screen still compiles. The results test does not look at that label, so it should stay green. Change the label back when you are finished looking.
