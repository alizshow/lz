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

// Superscript converts a non-negative integer to Unicode superscript digits.
func Superscript(n int) string {
	sup := [10]rune{'⁰', '¹', '²', '³', '⁴', '⁵', '⁶', '⁷', '⁸', '⁹'}
	s := fmt.Sprintf("%d", n)
	var b strings.Builder
	for _, r := range s {
		b.WriteRune(sup[r-'0'])
	}
	return b.String()
}

// WrapLine soft-wraps a single string (which may contain ANSI escapes) into
// lines of at most width visible cells. ANSI state is carried across breaks.
func WrapLine(s string, width int) []string {
	if width <= 0 {
		return []string{s}
	}
	runes := []rune(s)
	var lines []string
	var cur strings.Builder
	var activeSeq string
	visW := 0

	for i := 0; i < len(runes); {
		// ANSI escape sequence: \x1b[ ... m
		if runes[i] == '\x1b' && i+1 < len(runes) && runes[i+1] == '[' {
			j := i + 2
			for j < len(runes) && runes[j] != 'm' {
				j++
			}
			if j < len(runes) {
				seq := string(runes[i : j+1])
				cur.WriteString(seq)
				if seq == "\x1b[0m" || seq == "\x1b[m" {
					activeSeq = ""
				} else {
					activeSeq = seq
				}
				i = j + 1
				continue
			}
		}

		rw := runewidth.RuneWidth(runes[i])
		if visW+rw > width {
			if activeSeq != "" {
				cur.WriteString("\x1b[0m")
			}
			lines = append(lines, cur.String())
			cur.Reset()
			if activeSeq != "" {
				cur.WriteString(activeSeq)
			}
			visW = 0
		}
		cur.WriteRune(runes[i])
		visW += rw
		i++
	}
	if cur.Len() > 0 {
		lines = append(lines, cur.String())
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

// RenderHelp formats a help bar string in faint style.
func RenderHelp(parts ...string) string {
	return Faint.Render(strings.Join(parts, " · "))
}
