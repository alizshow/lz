package ui

import "github.com/charmbracelet/lipgloss"

// Shared lipgloss styles used across commands.
var (
	Bold  = lipgloss.NewStyle().Bold(true)
	Faint = lipgloss.NewStyle().Faint(true)

	Yellow  = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	Green   = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	Red     = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	Cyan    = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	Magenta = lipgloss.NewStyle().Foreground(lipgloss.Color("5"))
	Blue    = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))

	Cursor     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("5"))
	FaintGreen = lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color("2"))
)
