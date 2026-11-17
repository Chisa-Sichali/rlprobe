package tui

import "github.com/charmbracelet/lipgloss"

var (
	accent = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	warn   = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true)
	bold   = lipgloss.NewStyle().Bold(true)
	label  = lipgloss.NewStyle().Width(12)
)
