package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"rate-limit-tester/internal/tui"
)

func main() {
	program := tea.NewProgram(
		tui.New(),
		tea.WithAltScreen(),
		tea.WithoutSignalHandler(),
		tea.WithFilter(func(model tea.Model, msg tea.Msg) tea.Msg {
			if _, quit := msg.(tea.QuitMsg); quit {
				if screen, ok := model.(tui.Model); ok && screen.Running() {
					return tui.CancelMsg{}
				}
			}
			return msg
		}),
	)
	if _, err := program.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
