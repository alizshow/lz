package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// RelativeTime formats a time as a compact relative string.
func RelativeTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dw", int(d.Hours()/(24*7)))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo", int(d.Hours()/(24*30)))
	default:
		return fmt.Sprintf("%dy", int(d.Hours()/(24*365)))
	}
}

// DotFill returns a dot-leader string " ···" of the given width (minimum 2).
func DotFill(width int) string {
	width = max(width, 2)
	return " " + strings.Repeat("·", width-1)
}

// Tab bar styles.
var (
	TabActive = lipgloss.NewStyle().Bold(true).Padding(0, 1).Foreground(lipgloss.Color("4")).Underline(true)
	Tab       = lipgloss.NewStyle().Padding(0, 1)
)

// RenderTabBar renders a horizontal tab bar with the active tab highlighted.
func RenderTabBar(labels []string, active int) string {
	parts := make([]string, len(labels))
	for i, label := range labels {
		if i == active {
			parts[i] = TabActive.Render(label)
		} else {
			parts[i] = Tab.Render(label)
		}
	}
	return strings.Join(parts, " ")
}

// Truncate shortens s to max display cells, appending "…" if truncated.
// Unicode-safe via runewidth.
func Truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	w := runewidth.StringWidth(s)
	if w <= max {
		return s
	}
	result := []rune{}
	cur := 0
	for _, r := range s {
		rw := runewidth.RuneWidth(r)
		if cur+rw > max-1 { // leave room for …
			break
		}
		result = append(result, r)
		cur += rw
	}
	return string(result) + "…"
}

// RenderHelp formats a help bar string in faint style.
func RenderHelp(parts ...string) string {
	return Faint.Render(strings.Join(parts, " · "))
}
